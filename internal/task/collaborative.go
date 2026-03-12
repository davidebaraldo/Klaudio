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
	// PHASE 3: Wait for workers, handle fix rounds
	// =============================================
	//
	// After all workers complete, the orchestrator sends ALL_WORKERS_DONE.
	// The manager can then either:
	//   a) Exit normally → orchestration ends
	//   b) Send [RESPAWN_WORKERS] with a list of subtask IDs and fix instructions
	//      → the orchestrator respawns those workers and waits again
	// This loop repeats indefinitely until the manager is satisfied and exits.
	round := 0
	var msgCursor int64 // tracks last seen message ID for respawn polling

	for {
		// --- Wait for all workers to finish (manager stays running) ---
		allWorkersDone := false
		for !allWorkersDone {
			runningMu.Lock()
			count := len(running)
			onlyManager := true
			for _, stID := range running {
				if stID != "manager" {
					onlyManager = false
					break
				}
			}
			runningMu.Unlock()

			if count == 0 {
				// Manager also exited — we're completely done
				goto done
			}

			if onlyManager {
				allWorkersDone = true
				break
			}

			select {
			case <-ctx.Done():
				logger.Info("collaborative orchestration cancelled")
				return ctx.Err()
			case ev := <-completionCh:
				o.handleCollaborativeCompletion(ctx, ev, task.ID, plan, absWorkspace, &runningMu, running, logger)
			}
		}

		// --- All workers finished — notify the manager ---
		round++
		logger.Info("all workers completed, sending ALL_WORKERS_DONE to manager", "round", round)
		o.sendSystemMessage(ctx, task.ID, "ALL_WORKERS_DONE",
			o.buildWorkersSummary(plan))

		o.recordEvent(ctx, task.ID, "fix_round.workers_done", map[string]string{
			"round": fmt.Sprintf("%d", round),
		})

		// --- Wait for manager decision: exit or RESPAWN_WORKERS ---
		respawnReqs := o.waitForManagerDecision(ctx, task.ID, completionCh, &runningMu, running, &msgCursor, logger)

		if respawnReqs == nil {
			// Manager exited (or context cancelled) — no more rounds
			goto done
		}

		// --- Manager requested respawns — launch fix workers ---
		logger.Info("manager requested worker respawn", "round", round+1, "count", len(respawnReqs))

		o.recordEvent(ctx, task.ID, "fix_round.respawn", map[string]string{
			"round":   fmt.Sprintf("%d", round+1),
			"workers": fmt.Sprintf("%d", len(respawnReqs)),
		})

		for _, req := range respawnReqs {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			subtask := o.findSubtask(plan, req.SubtaskID)
			if subtask == nil {
				logger.Warn("manager requested respawn of unknown subtask", "subtask_id", req.SubtaskID)
				continue
			}

			role := agent.AgentRole(subtask.AgentRole)
			if role == "" {
				role = agent.RoleDeveloper
			}

			prompt := BuildFixWorkerPrompt(*subtask, plan.Subtasks, plan.TaskPrompt, req.Instructions, round+1, CollaborativeWorkerOpts{
				Directive:      o.comms.ReadDirective(absWorkspace, subtask.ID),
				RolePromptHint: roleHints[subtask.AgentRole],
				APIURL:         apiURL,
				TaskID:         task.ID,
			})

			ag, spawnErr := o.pool.Spawn(ctx, agent.SpawnOpts{
				TaskID:       task.ID,
				SubtaskID:    subtask.ID,
				Role:         role,
				Prompt:       prompt,
				WorkspaceDir: absWorkspace,
			})
			if spawnErr != nil {
				logger.Error("failed to respawn worker", "subtask_id", req.SubtaskID, "error", spawnErr)
				o.sendSystemMessage(ctx, task.ID, "RESPAWN_FAILED",
					fmt.Sprintf("Failed to respawn %s: %s", req.SubtaskID, spawnErr))
				continue
			}

			// Update plan state
			for i := range plan.Subtasks {
				if plan.Subtasks[i].ID == subtask.ID {
					plan.Subtasks[i].Status = "running"
					plan.Subtasks[i].AgentID = ag.ID
					break
				}
			}
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

			o.recordEvent(ctx, task.ID, "subtask.respawned", map[string]string{
				"subtask_id": subtask.ID,
				"agent_id":   ag.ID,
				"round":      fmt.Sprintf("%d", round+1),
			})

			logger.Info("respawned fix worker", "subtask_id", subtask.ID, "agent_id", ag.ID, "round", round+1)
		}

		// Notify the manager that respawned workers are running
		o.sendSystemMessage(ctx, task.ID, "WORKERS_RESPAWNED",
			fmt.Sprintf("Respawned %d worker(s) for fix round %d. Monitoring their progress.", len(respawnReqs), round+1))

		// Loop back to wait for these new workers to complete
	}

done:
	// Save manager context
	o.comms.SaveSubtaskContext(ctx, task.ID, "manager", managerAg.ID, "Team Manager",
		fmt.Sprintf("Coordinated workers across %d round(s) and monitored execution to completion.", round), absWorkspace)

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

// waitForManagerDecision blocks until the manager either exits or sends a RESPAWN_WORKERS message.
// Returns nil if the manager exited (or context was cancelled), or the list of respawn requests.
func (o *Orchestrator) waitForManagerDecision(
	ctx context.Context,
	taskID string,
	completionCh chan completionEvent,
	runningMu *sync.Mutex,
	running map[string]string,
	msgCursor *int64,
	logger *slog.Logger,
) []RespawnRequest {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case ev := <-completionCh:
			// An agent exited — if it's the manager, we're done
			runningMu.Lock()
			delete(running, ev.AgentID)
			runningMu.Unlock()

			if ev.SubtaskID == "manager" {
				if ev.Result.ExitCode == 0 && ev.Result.Error == nil {
					logger.Info("team manager exited — no more fix rounds requested")
				} else {
					logger.Warn("team manager exited with error",
						"exit_code", ev.Result.ExitCode, "error", ev.Result.Error)
				}
				return nil
			}

		case <-ticker.C:
			// Poll for RESPAWN_WORKERS message from manager
			reqs, newCursor := o.comms.CheckRespawnRequests(ctx, taskID, *msgCursor)
			*msgCursor = newCursor
			if len(reqs) > 0 {
				logger.Info("manager sent RESPAWN_WORKERS", "count", len(reqs))
				return reqs
			}
		}
	}
}

// findSubtask returns a pointer to the subtask in the plan with the given ID, or nil.
func (o *Orchestrator) findSubtask(plan *ExecutionPlan, subtaskID string) *db.Subtask {
	for i := range plan.Subtasks {
		if plan.Subtasks[i].ID == subtaskID {
			return &plan.Subtasks[i]
		}
	}
	return nil
}

// BuildFixWorkerPrompt creates the prompt for a worker respawned for a fix round.
// It includes the original subtask context plus the manager's fix instructions.
func BuildFixWorkerPrompt(subtask db.Subtask, allSubtasks []db.Subtask, taskPrompt, fixInstructions string, round int, opts CollaborativeWorkerOpts) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("You are a worker agent respawned for **fix round %d**.\n\n", round))
	b.WriteString("The team already completed an initial round of work. The Team Manager reviewed the results ")
	b.WriteString("and determined that your subtask needs additional fixes.\n\n")

	b.WriteString("## FIX INSTRUCTIONS FROM MANAGER (DO THIS FIRST)\n\n")
	b.WriteString(fixInstructions)
	b.WriteString("\n\n")

	b.WriteString("## Overall Task\n")
	b.WriteString(taskPrompt)
	b.WriteString("\n\n")

	if opts.RolePromptHint != "" {
		b.WriteString("## Your Role\n")
		b.WriteString(opts.RolePromptHint)
		b.WriteString("\n\n")
	}

	if opts.Directive != "" {
		b.WriteString("## Original Directives from Team Manager\n")
		b.WriteString(opts.Directive)
		b.WriteString("\n\n")
	}

	b.WriteString("## Your Team\n")
	b.WriteString("Status of all subtasks after the previous round:\n")
	for _, st := range allSubtasks {
		marker := ""
		if st.ID == subtask.ID {
			marker = " ← THIS IS YOU (respawned)"
		}
		b.WriteString(fmt.Sprintf("- **%s** (ID: %s): %s%s\n", st.Name, st.ID, st.Status, marker))
	}
	b.WriteString("\n")

	b.WriteString("## Your Subtask\n")
	b.WriteString(fmt.Sprintf("**ID**: %s\n", subtask.ID))
	b.WriteString(fmt.Sprintf("**Name**: %s\n", subtask.Name))
	b.WriteString(fmt.Sprintf("**Description**: %s\n\n", subtask.Description))
	b.WriteString("## Original Instructions\n")
	b.WriteString(subtask.Prompt)
	b.WriteString("\n")

	if len(subtask.FilesInvolved) > 0 {
		b.WriteString("\n## Files to work with\n")
		for _, f := range subtask.FilesInvolved {
			b.WriteString("- " + f + "\n")
		}
	}

	b.WriteString("\n## Important\n")
	b.WriteString("- Focus on the FIX INSTRUCTIONS above — that is your primary objective this round\n")
	b.WriteString("- The codebase already has work from the previous round, so do NOT start from scratch\n")
	b.WriteString("- When done, follow the approval loop below — do NOT exit until the manager approves\n\n")

	b.WriteString(workerApprovalLoopInstructions(opts.APIURL, opts.TaskID, subtask.ID))
	b.WriteString(APICommsInstructions(opts.APIURL, opts.TaskID, subtask.ID))
	b.WriteString(workspaceFileRule)

	return b.String()
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
	b.WriteString("6. When you see ALL_WORKERS_DONE, **review the results** and decide:\n")
	b.WriteString("   - If everything looks good → write a final summary and **exit**\n")
	b.WriteString("   - If fixes are needed → send a **RESPAWN_WORKERS** message (see below) to relaunch specific workers\n\n")

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

	b.WriteString("## Respawning Workers for Fixes\n\n")
	b.WriteString("After receiving ALL_WORKERS_DONE, if you determine that some workers need to fix their work, ")
	b.WriteString("you can request the orchestrator to respawn them. Send a message with the exact format:\n\n")
	b.WriteString("```bash\n")
	b.WriteString(fmt.Sprintf("curl -s -X POST %s/api/tasks/%s/messages \\\n", apiURL, taskID))
	b.WriteString("  -H \"Content-Type: application/json\" \\\n")
	b.WriteString("  -d '{\"from\": \"manager\", \"content\": \"[RESPAWN_WORKERS]\\nsubtask-id-1: Description of what to fix\\nsubtask-id-2: Description of what to fix\"}'\n")
	b.WriteString("```\n\n")
	b.WriteString("The orchestrator will respawn those workers with your fix instructions and send you a ")
	b.WriteString("`[WORKERS_RESPAWNED]` confirmation. You then continue monitoring until those workers finish, ")
	b.WriteString("and you will receive another ALL_WORKERS_DONE. You can repeat this as many times as needed.\n\n")
	b.WriteString("### Inline fixes (workers stay alive)\n\n")
	b.WriteString("Workers do NOT exit when they finish — they wait for your approval. ")
	b.WriteString("When a worker sends `[WORK_DONE]`, you can:\n\n")
	b.WriteString("**Approve** (worker exits):\n")
	b.WriteString("```bash\n")
	b.WriteString(fmt.Sprintf("curl -s -X POST %s/api/tasks/%s/messages \\\n", apiURL, taskID))
	b.WriteString("  -H \"Content-Type: application/json\" \\\n")
	b.WriteString("  -d '{\"from\": \"manager\", \"to\": \"SUBTASK_ID\", \"content\": \"[WORKER_APPROVED] Good work!\"}'\n")
	b.WriteString("```\n\n")
	b.WriteString("**Request more work** (worker continues):\n")
	b.WriteString("```bash\n")
	b.WriteString(fmt.Sprintf("curl -s -X POST %s/api/tasks/%s/messages \\\n", apiURL, taskID))
	b.WriteString("  -H \"Content-Type: application/json\" \\\n")
	b.WriteString("  -d '{\"from\": \"manager\", \"to\": \"SUBTASK_ID\", \"content\": \"[CONTINUE_WORK] Please fix: description of what needs fixing\"}'\n")
	b.WriteString("```\n\n")
	b.WriteString("This is the **preferred** method for small fixes — it avoids respawning the worker entirely.\n\n")
	b.WriteString("### Full respawn (for larger rework)\n\n")
	b.WriteString("Use RESPAWN_WORKERS only when a worker has already exited or needs a complete redo.\n")
	b.WriteString("The inline CONTINUE_WORK approach above is faster and preserves the worker's context.\n\n")
	b.WriteString("### Review checklist\n")
	b.WriteString("- Inspect the code the workers produced (read the relevant files)\n")
	b.WriteString("- Run tests or build commands to verify correctness\n")
	b.WriteString("- Check if workers followed the shared contracts from your directives\n")
	b.WriteString("- Check for integration issues between what different workers produced\n")
	b.WriteString("- Only exit when you have approved ALL workers and are satisfied with the overall result\n\n")

	b.WriteString("## Important Rules\n")
	b.WriteString("- Do NOT write any production code. You only write directives and messages.\n")
	b.WriteString("- Be specific and concrete in your directives and fix instructions.\n")
	b.WriteString("- Use exact function signatures, types, and paths.\n")
	b.WriteString("- After writing directives, enter monitoring mode — poll messages and respond.\n")
	b.WriteString("- Do NOT exit until you have reviewed all results and are satisfied.\n")
	b.WriteString("- You may request as many fix rounds as needed — there is no limit.\n\n")

	b.WriteString("## Monitoring Loop\n\n")
	b.WriteString("After writing directives, use this loop to monitor workers:\n")
	b.WriteString("```bash\n")
	b.WriteString("# Poll for new messages (repeat every 10-15 seconds)\n")
	b.WriteString(fmt.Sprintf("curl -s %s/api/tasks/%s/messages | jq '.messages[] | select(.msg_type==\"message\" or .msg_type==\"system\")'\n", apiURL, taskID))
	b.WriteString("```\n\n")
	b.WriteString("When you see `[ALL_WORKERS_DONE]`, review the code and decide: exit if satisfied, or send RESPAWN_WORKERS.\n")
	b.WriteString("When you see `[WORKERS_RESPAWNED]`, continue monitoring — another ALL_WORKERS_DONE will follow.\n")
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
	b.WriteString(workerApprovalLoopInstructions(opts.APIURL, opts.TaskID, subtask.ID))
	b.WriteString(APICommsInstructions(opts.APIURL, opts.TaskID, subtask.ID))
	b.WriteString(workspaceFileRule)

	return b.String()
}

// workerApprovalLoopInstructions returns prompt text instructing workers to wait for
// manager approval before exiting. The worker stays alive, polling for messages,
// and can receive additional fix instructions without being respawned.
func workerApprovalLoopInstructions(apiURL, taskID, subtaskID string) string {
	return fmt.Sprintf(`## CRITICAL: Do NOT exit when done — Wait for Manager Approval

After completing your work, you MUST follow this protocol:

1. **Broadcast** a summary of what you did:
`+"```bash"+`
curl -s -X POST %s/api/tasks/%s/messages \
  -H "Content-Type: application/json" \
  -d '{"from": "%s", "content": "[WORK_DONE] Summary of completed work here"}'
`+"```"+`

2. **Enter an approval loop** — poll for messages from the manager every 10 seconds:
`+"```bash"+`
while true; do
  MSG=$(curl -s %s/api/tasks/%s/messages | jq -r '.messages[] | select(.to_subtask_id=="%s" or .to_subtask_id==null) | select(.from_subtask_id=="manager") | .content' | tail -1)
  if echo "$MSG" | grep -q '\[WORKER_APPROVED\]'; then
    echo "Manager approved — exiting."
    break
  fi
  if echo "$MSG" | grep -q '\[CONTINUE_WORK\]'; then
    echo "Manager sent additional instructions:"
    echo "$MSG"
    break
  fi
  echo "Waiting for manager approval..."
  sleep 10
done
`+"```"+`

3. If the manager sends **[CONTINUE_WORK]** with additional instructions:
   - Read the instructions from the message content
   - Execute the requested fixes
   - Broadcast [WORK_DONE] again with a summary of the new changes
   - Return to the approval loop (step 2)

4. If the manager sends **[WORKER_APPROVED]** — you can safely exit.

**Do NOT exit before receiving WORKER_APPROVED.** The manager needs to review your work first.

`, apiURL, taskID, subtaskID, apiURL, taskID, subtaskID)
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
