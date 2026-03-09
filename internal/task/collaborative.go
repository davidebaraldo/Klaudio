package task

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/klaudio-ai/klaudio/internal/agent"
	"github.com/klaudio-ai/klaudio/internal/db"
)

// RunCollaborative executes the plan in collaborative mode:
// 1. A team-manager agent spawns and writes initial coordination directives
// 2. All worker agents spawn simultaneously (manager keeps running)
// 3. The orchestrator notifies the manager as workers complete/fail via API messages
// 4. When all workers are done, the orchestrator sends ALL_WORKERS_DONE — manager wraps up
// 5. Reviewer runs after the manager exits
func (o *Orchestrator) RunCollaborative(ctx context.Context, task *db.Task, plan *ExecutionPlan) error {
	logger := slog.With("task_id", task.ID, "component", "collaborative")

	// Resolve workspace
	workspaceDir := filepath.Join(o.cfg.Storage.DataDir, "workspaces", task.ID)
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return fmt.Errorf("creating workspace directory: %w", err)
	}
	absWorkspace, err := filepath.Abs(workspaceDir)
	if err != nil {
		return fmt.Errorf("resolving workspace path: %w", err)
	}

	// Prepare .klaudio directories (context + directives only; messages go through API)
	for _, dir := range []string{"context", "directives"} {
		os.MkdirAll(filepath.Join(absWorkspace, ".klaudio", dir), 0o755)
	}

	apiURL := o.agentAPIURL()

	// Resolve team template role hints
	roleHints := o.resolveRoleHints(ctx, task)

	// completionCh receives events from both manager and workers
	completionCh := make(chan completionEvent, len(plan.Subtasks)+1)

	var runningMu sync.Mutex
	running := make(map[string]string) // agentID -> subtaskID

	// =============================================
	// PHASE 1: Spawn Team Manager (stays alive)
	// =============================================
	logger.Info("spawning team manager agent")

	managerPrompt := BuildManagerPrompt(plan.Subtasks, plan.TaskPrompt, roleHints, apiURL, task.ID)

	managerAg, err := o.pool.Spawn(ctx, agent.SpawnOpts{
		TaskID:       task.ID,
		SubtaskID:    "manager",
		Role:         "manager",
		Prompt:       managerPrompt,
		WorkspaceDir: absWorkspace,
	})
	if err != nil {
		return fmt.Errorf("spawning team manager: %w", err)
	}

	o.recordEvent(ctx, task.ID, "manager.started", map[string]string{
		"agent_id": managerAg.ID,
	})

	// Monitor manager in background — it runs concurrently with workers
	go func() {
		result := <-o.pool.Wait(managerAg.ID)
		completionCh <- completionEvent{
			AgentID:   managerAg.ID,
			SubtaskID: "manager",
			Result:    result,
		}
	}()

	runningMu.Lock()
	running[managerAg.ID] = "manager"
	runningMu.Unlock()

	// Workers are spawned immediately but their prompts instruct them to wait
	// for their directive file before starting actual work.
	logger.Info("spawning workers immediately — they will wait for manager directives")

	// =============================================
	// PHASE 2: Spawn all workers simultaneously
	// =============================================
	logger.Info("spawning all worker agents simultaneously", "count", len(plan.Subtasks))

	for i := range plan.Subtasks {
		subtask := &plan.Subtasks[i]

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		role := agent.AgentRole(subtask.AgentRole)
		if role == "" {
			role = agent.RoleDeveloper
		}

		// Read directives from manager (if written)
		directive := o.comms.ReadDirective(absWorkspace, subtask.ID)

		// Collect any broadcasts already sent (e.g., by manager)
		broadcastMsgs := o.comms.CollectBroadcastMessages(ctx, task.ID, subtask.ID)

		prompt := BuildCollaborativeWorkerPrompt(*subtask, plan.Subtasks, plan.TaskPrompt, CollaborativeWorkerOpts{
			Directive:         directive,
			BroadcastMessages: broadcastMsgs,
			RolePromptHint:    roleHints[subtask.AgentRole],
			APIURL:            apiURL,
			TaskID:            task.ID,
		})

		ag, err := o.pool.Spawn(ctx, agent.SpawnOpts{
			TaskID:       task.ID,
			SubtaskID:    subtask.ID,
			Role:         role,
			Prompt:       prompt,
			WorkspaceDir: absWorkspace,
		})
		if err == agent.ErrPoolFull || err == agent.ErrTaskLimitReached {
			logger.Warn("pool full, waiting for slot to open", "subtask_id", subtask.ID)
			ev := <-completionCh
			o.handleCollaborativeCompletion(ctx, ev, task.ID, plan, absWorkspace, &runningMu, running, logger)
			// Retry spawn
			ag, err = o.pool.Spawn(ctx, agent.SpawnOpts{
				TaskID:       task.ID,
				SubtaskID:    subtask.ID,
				Role:         role,
				Prompt:       prompt,
				WorkspaceDir: absWorkspace,
			})
		}
		if err != nil {
			return fmt.Errorf("spawning worker for subtask %s: %w", subtask.ID, err)
		}

		plan.Subtasks[i].Status = "running"
		plan.Subtasks[i].AgentID = ag.ID
		o.updatePlanSubtasks(ctx, plan)

		runningMu.Lock()
		running[ag.ID] = subtask.ID
		runningMu.Unlock()

		go func(agentID, stID string) {
			result := <-o.pool.Wait(agentID)
			completionCh <- completionEvent{
				AgentID:   agentID,
				SubtaskID: stID,
				Result:    result,
			}
		}(ag.ID, subtask.ID)

		o.recordEvent(ctx, task.ID, "subtask.started", map[string]string{
			"subtask_id": subtask.ID,
			"agent_id":   ag.ID,
			"mode":       "collaborative",
		})

		logger.Info("spawned collaborative worker", "subtask_id", subtask.ID, "agent_id", ag.ID)
	}

	// =============================================
	// PHASE 3: Wait for all agents to complete
	// =============================================
	allWorkersDoneNotified := false
	for {
		runningMu.Lock()
		count := len(running)
		runningMu.Unlock()
		if count == 0 {
			break
		}

		select {
		case <-ctx.Done():
			logger.Info("collaborative orchestration cancelled")
			return ctx.Err()
		case ev := <-completionCh:
			o.handleCollaborativeCompletion(ctx, ev, task.ID, plan, absWorkspace, &runningMu, running, logger)

			// After processing, check if all workers are done (manager may still be running)
			if !allWorkersDoneNotified {
				runningMu.Lock()
				onlyManager := true
				for _, stID := range running {
					if stID != "manager" {
						onlyManager = false
						break
					}
				}
				runningMu.Unlock()

				if onlyManager {
					// All workers finished — notify the manager
					allWorkersDoneNotified = true
					logger.Info("all workers completed, sending ALL_WORKERS_DONE to manager")
					o.sendSystemMessage(ctx, task.ID, "ALL_WORKERS_DONE",
						o.buildWorkersSummary(plan))
				}
			}
		}
	}

	// Save manager context
	o.comms.SaveSubtaskContext(ctx, task.ID, "manager", managerAg.ID, "Team Manager",
		"Coordinated workers and monitored execution to completion.", absWorkspace)

	o.recordEvent(ctx, task.ID, "manager.completed", map[string]string{
		"agent_id": managerAg.ID,
	})

	// =============================================
	// PHASE 4: Reviewer
	// =============================================
	if o.shouldRunReviewer(plan) {
		logger.Info("running reviewer agent")
		reviewer := NewReviewer(o.pool, o.db)
		reviewResult, err := reviewer.Review(ctx, task, plan, absWorkspace)
		if err != nil {
			logger.Warn("reviewer failed", "error", err)
		} else {
			logger.Info("review completed", "status", reviewResult.Status, "summary", reviewResult.Summary)
			o.recordEvent(ctx, task.ID, "review.completed", map[string]string{
				"status":  reviewResult.Status,
				"summary": reviewResult.Summary,
			})
		}
	}

	return nil
}

// waitForDirectives waits until the manager has written ALL directive files
// (coordination.md + one per subtask) or the timeout elapses.
func waitForDirectives(ctx context.Context, absWorkspace string, subtasks []db.Subtask, timeout time.Duration) {
	directivesDir := filepath.Join(absWorkspace, ".klaudio", "directives")

	// Build list of expected files: coordination.md + {subtaskID}.md for each subtask
	expected := []string{filepath.Join(directivesDir, "coordination.md")}
	for _, st := range subtasks {
		expected = append(expected, filepath.Join(directivesDir, st.ID+".md"))
	}

	deadline := time.After(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			// Log which files are still missing
			var missing []string
			for _, f := range expected {
				if _, err := os.Stat(f); err != nil {
					missing = append(missing, filepath.Base(f))
				}
			}
			slog.Info("directive wait timeout, proceeding with worker spawn",
				"missing_files", missing)
			return
		case <-ticker.C:
			allFound := true
			for _, f := range expected {
				if _, err := os.Stat(f); err != nil {
					allFound = false
					break
				}
			}
			if allFound {
				slog.Info("all directive files found, proceeding with worker spawn",
					"count", len(expected))
				return
			}
		}
	}
}

// sendSystemMessage sends a system/orchestrator message to a task via the comms DB.
func (o *Orchestrator) sendSystemMessage(ctx context.Context, taskID, signal, content string) {
	from := "orchestrator"
	msg := &db.AgentMessage{
		TaskID:        taskID,
		FromSubtaskID: &from,
		MsgType:       "system",
		Content:       fmt.Sprintf("[%s] %s", signal, content),
		CreatedAt:     time.Now().UTC(),
	}
	if err := o.db.CreateAgentMessage(ctx, msg); err != nil {
		slog.Warn("failed to send system message", "task_id", taskID, "signal", signal, "error", err)
	}
}

// buildWorkersSummary creates a summary of all worker results for the manager.
func (o *Orchestrator) buildWorkersSummary(plan *ExecutionPlan) string {
	var b strings.Builder
	b.WriteString("All workers have finished. Summary:\n\n")
	for _, st := range plan.Subtasks {
		b.WriteString(fmt.Sprintf("- %s (%s): %s\n", st.Name, st.ID, st.Status))
	}
	return b.String()
}

// handleCollaborativeCompletion processes a single agent completion in collaborative mode.
// For worker completions, it also notifies the manager via the messaging API.
func (o *Orchestrator) handleCollaborativeCompletion(
	ctx context.Context,
	ev completionEvent,
	taskID string,
	plan *ExecutionPlan,
	absWorkspace string,
	runningMu *sync.Mutex,
	running map[string]string,
	logger *slog.Logger,
) {
	runningMu.Lock()
	delete(running, ev.AgentID)
	runningMu.Unlock()

	// Manager completion — just log it
	if ev.SubtaskID == "manager" {
		if ev.Result.ExitCode == 0 && ev.Result.Error == nil {
			logger.Info("team manager exited successfully")
		} else {
			logger.Warn("team manager exited with error",
				"exit_code", ev.Result.ExitCode, "error", ev.Result.Error)
		}
		return
	}

	// Worker completion
	if ev.Result.ExitCode == 0 && ev.Result.Error == nil {
		var subtaskName, subtaskDesc string
		for _, st := range plan.Subtasks {
			if st.ID == ev.SubtaskID {
				subtaskName = st.Name
				subtaskDesc = st.Description
				if subtaskDesc == "" {
					subtaskDesc = st.Prompt
				}
				break
			}
		}

		summary := fmt.Sprintf("Completed successfully.\n\nTask: %s\nDescription: %s", subtaskName, subtaskDesc)
		if err := o.comms.SaveSubtaskContext(ctx, taskID, ev.SubtaskID, ev.AgentID, subtaskName, summary, absWorkspace); err != nil {
			logger.Warn("failed to save subtask context", "subtask_id", ev.SubtaskID, "error", err)
		}

		for i := range plan.Subtasks {
			if plan.Subtasks[i].ID == ev.SubtaskID {
				plan.Subtasks[i].Status = "completed"
				break
			}
		}
		o.updatePlanSubtasks(ctx, plan)

		o.recordEvent(ctx, taskID, "subtask.completed", map[string]string{
			"subtask_id": ev.SubtaskID,
			"agent_id":   ev.AgentID,
			"mode":       "collaborative",
		})

		// Notify the manager that this worker completed
		o.sendSystemMessage(ctx, taskID, "WORKER_COMPLETED",
			fmt.Sprintf("Worker %s (%s) completed successfully.", subtaskName, ev.SubtaskID))

		logger.Info("collaborative worker completed", "subtask_id", ev.SubtaskID)
	} else {
		for i := range plan.Subtasks {
			if plan.Subtasks[i].ID == ev.SubtaskID {
				plan.Subtasks[i].Status = "failed"
				break
			}
		}
		o.updatePlanSubtasks(ctx, plan)

		errMsg := ""
		if ev.Result.Error != nil {
			errMsg = ev.Result.Error.Error()
		}
		o.recordEvent(ctx, taskID, "subtask.failed", map[string]string{
			"subtask_id": ev.SubtaskID,
			"agent_id":   ev.AgentID,
			"error":      errMsg,
			"mode":       "collaborative",
		})

		// Notify the manager that this worker failed
		var subtaskName string
		for _, st := range plan.Subtasks {
			if st.ID == ev.SubtaskID {
				subtaskName = st.Name
				break
			}
		}
		o.sendSystemMessage(ctx, taskID, "WORKER_FAILED",
			fmt.Sprintf("Worker %s (%s) FAILED: %s (exit code %d)", subtaskName, ev.SubtaskID, errMsg, ev.Result.ExitCode))

		logger.Error("collaborative worker failed", "subtask_id", ev.SubtaskID,
			"exit_code", ev.Result.ExitCode, "error", ev.Result.Error)
	}
}

// resolveTeamMode looks up the team template for a task and returns its mode.
func (o *Orchestrator) resolveTeamMode(ctx context.Context, task *db.Task) string {
	if task.TeamTemplate == nil || *task.TeamTemplate == "" {
		return "sequential"
	}

	tt, err := o.db.GetTeamTemplate(ctx, *task.TeamTemplate)
	if err != nil || tt == nil {
		return "sequential"
	}

	if tt.Mode == "collaborative" {
		return "collaborative"
	}
	return "sequential"
}

// BuildManagerPrompt creates the prompt for the team manager agent.
// The manager runs concurrently with workers and monitors their progress via the API.
func BuildManagerPrompt(subtasks []db.Subtask, taskPrompt string, roleHints map[string]string, apiURL, taskID string) string {
	var b strings.Builder
	b.WriteString("You are the **Team Manager** agent. You coordinate a team of worker agents and stay alive throughout execution.\n\n")
	b.WriteString("## Task\n")
	b.WriteString(taskPrompt)
	b.WriteString("\n\n")

	b.WriteString("## Workers\n")
	b.WriteString("The following worker agents will start simultaneously shortly after you write your directives:\n\n")
	for _, st := range subtasks {
		b.WriteString(fmt.Sprintf("- **%s** (ID: %s, role: %s): %s\n", st.Name, st.ID, st.AgentRole, st.Description))
	}
	b.WriteString("\n")

	b.WriteString("## Your Lifecycle\n\n")
	b.WriteString("You operate in two phases:\n\n")
	b.WriteString("### Phase 1: Write Directives\n")
	b.WriteString("1. **Analyze** the task and understand how the subtasks relate to each other\n")
	b.WriteString("2. **Write coordination directives** for each worker agent\n")
	b.WriteString("3. **Define shared contracts** (APIs, data formats, naming conventions)\n")
	b.WriteString("4. **Identify potential conflicts** where workers might step on each other\n")
	b.WriteString("5. **Broadcast** a summary of the coordination plan\n\n")

	b.WriteString("### Phase 2: Monitor & Coordinate\n")
	b.WriteString("After writing directives, **keep running** and monitor workers:\n")
	b.WriteString("1. **Poll messages** from the API every 10-15 seconds to see worker updates\n")
	b.WriteString("2. **Respond** to questions or issues raised by workers\n")
	b.WriteString("3. **Coordinate** if conflicts arise (e.g., two workers editing same interface)\n")
	b.WriteString("4. **Send guidance** when workers report completion or problems\n")
	b.WriteString("5. **Wait for the ALL_WORKERS_DONE signal** — the orchestrator will send a system message with this signal when all workers have finished\n")
	b.WriteString("6. When you see ALL_WORKERS_DONE, write a final summary and **exit**\n\n")

	b.WriteString("## How to Write Directives\n\n")
	b.WriteString("For EACH worker, create a file at `.klaudio/directives/{subtask_id}.md` with:\n")
	b.WriteString("- What this worker should do first\n")
	b.WriteString("- Shared contracts and interfaces to follow\n")
	b.WriteString("- Files this worker should NOT modify (to avoid conflicts)\n")
	b.WriteString("- How to handle dependencies on other workers' output\n")
	b.WriteString("- Any specific instructions or constraints\n\n")

	b.WriteString("Also create `.klaudio/directives/coordination.md` with:\n")
	b.WriteString("- Shared API contracts\n")
	b.WriteString("- Naming conventions\n")
	b.WriteString("- File ownership (which worker owns which files)\n")
	b.WriteString("- Execution order recommendations\n\n")

	b.WriteString("## Important Rules\n")
	b.WriteString("- Do NOT write any production code. You only write directives and messages.\n")
	b.WriteString("- Be specific and concrete in your directives.\n")
	b.WriteString("- Use exact function signatures, types, and paths.\n")
	b.WriteString("- After writing directives, enter monitoring mode — poll messages and respond.\n")
	b.WriteString("- Do NOT exit until you see the ALL_WORKERS_DONE system message.\n\n")

	b.WriteString("## Monitoring Loop\n\n")
	b.WriteString("After writing directives, use this loop to monitor workers:\n")
	b.WriteString("```bash\n")
	b.WriteString("# Poll for new messages (repeat every 10-15 seconds)\n")
	b.WriteString(fmt.Sprintf("curl -s %s/api/tasks/%s/messages | jq '.messages[] | select(.msg_type==\"message\" or .msg_type==\"system\")'\n", apiURL, taskID))
	b.WriteString("```\n\n")
	b.WriteString("When you see `[ALL_WORKERS_DONE]` in a system message, write your final summary and exit.\n")
	b.WriteString("When you see `[WORKER_COMPLETED]` or `[WORKER_FAILED]`, you can send feedback or guidance.\n\n")

	b.WriteString(APICommsInstructions(apiURL, taskID, "manager"))

	return b.String()
}

// CollaborativeWorkerOpts contains options for building collaborative worker prompts.
type CollaborativeWorkerOpts struct {
	Directive         string
	BroadcastMessages string
	RolePromptHint    string
	APIURL            string
	TaskID            string
}

// BuildCollaborativeWorkerPrompt creates the prompt for a worker in collaborative mode.
func BuildCollaborativeWorkerPrompt(subtask db.Subtask, allSubtasks []db.Subtask, taskPrompt string, opts CollaborativeWorkerOpts) string {
	var b strings.Builder
	b.WriteString("You are a worker agent in a **collaborative team**. All workers run simultaneously.\n\n")

	b.WriteString("## Overall Task\n")
	b.WriteString(taskPrompt)
	b.WriteString("\n\n")

	if opts.RolePromptHint != "" {
		b.WriteString("## Your Role\n")
		b.WriteString(opts.RolePromptHint)
		b.WriteString("\n\n")
	}

	if opts.Directive != "" {
		b.WriteString("## Directives from Team Manager\n")
		b.WriteString(opts.Directive)
		b.WriteString("\n\n")
	}

	if opts.BroadcastMessages != "" {
		b.WriteString(opts.BroadcastMessages)
		b.WriteString("\n\n")
	}

	b.WriteString("## Your Team\n")
	b.WriteString("These agents are running at the same time as you:\n")
	for _, st := range allSubtasks {
		if st.ID == subtask.ID {
			b.WriteString(fmt.Sprintf("- **%s** (ID: %s) ← THIS IS YOU\n", st.Name, st.ID))
		} else {
			b.WriteString(fmt.Sprintf("- **%s** (ID: %s, role: %s): %s\n", st.Name, st.ID, st.AgentRole, st.Description))
		}
	}
	b.WriteString("\n")

	b.WriteString("## IMPORTANT: Wait for Manager Directives\n\n")
	b.WriteString("Before doing ANY work, you MUST wait for the Team Manager to write your directive file.\n\n")
	b.WriteString(fmt.Sprintf("**Your directive file**: `.klaudio/directives/%s.md`\n\n", subtask.ID))
	b.WriteString("Run this loop at the very start:\n")
	b.WriteString("```bash\n")
	b.WriteString(fmt.Sprintf("while [ ! -f .klaudio/directives/%s.md ]; do echo 'Waiting for manager directives...'; sleep 3; done\n", subtask.ID))
	b.WriteString("echo 'Directives received!'\n")
	b.WriteString(fmt.Sprintf("cat .klaudio/directives/%s.md\n", subtask.ID))
	b.WriteString("```\n\n")
	b.WriteString("Only after reading your directives should you begin working on your subtask.\n")
	b.WriteString("The directives contain important coordination rules, shared contracts, and file ownership info.\n\n")

	b.WriteString("## Your Subtask\n")
	b.WriteString(fmt.Sprintf("**ID**: %s\n", subtask.ID))
	b.WriteString(fmt.Sprintf("**Name**: %s\n", subtask.Name))
	b.WriteString(fmt.Sprintf("**Description**: %s\n\n", subtask.Description))
	b.WriteString("## Instructions\n")
	b.WriteString(subtask.Prompt)
	b.WriteString("\n")

	if len(subtask.FilesInvolved) > 0 {
		b.WriteString("\n## Files to work with\n")
		for _, f := range subtask.FilesInvolved {
			b.WriteString("- " + f + "\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(APICommsInstructions(opts.APIURL, opts.TaskID, subtask.ID))

	return b.String()
}

// ReadDirective reads a directive file for a specific subtask written by the manager.
func (cs *CommsService) ReadDirective(workspaceDir, subtaskID string) string {
	directiveFile := filepath.Join(workspaceDir, ".klaudio", "directives", subtaskID+".md")
	data, err := os.ReadFile(directiveFile)
	if err != nil {
		data = nil
	}

	coordFile := filepath.Join(workspaceDir, ".klaudio", "directives", "coordination.md")
	coordData, err := os.ReadFile(coordFile)
	if err != nil {
		coordData = nil
	}

	var parts []string
	if coordData != nil {
		parts = append(parts, "### Shared Coordination\n"+string(coordData))
	}
	if data != nil {
		parts = append(parts, "### Your Specific Directives\n"+string(data))
	}

	if len(parts) == 0 {
		return ""
	}

	return strings.Join(parts, "\n---\n")
}

// RecordDirective persists a manager directive to DB for UI visibility.
func (cs *CommsService) RecordDirective(ctx context.Context, taskID, managerAgentID, targetSubtaskID, content string) error {
	fromSubtask := "manager"
	msg := &db.AgentMessage{
		TaskID:        taskID,
		FromAgentID:   &managerAgentID,
		FromSubtaskID: &fromSubtask,
		ToSubtaskID:   &targetSubtaskID,
		MsgType:       "directive",
		Content:       content,
		CreatedAt:     time.Now().UTC(),
	}
	return cs.db.CreateAgentMessage(ctx, msg)
}
