package task

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/klaudio-ai/klaudio/internal/agent"
	"github.com/klaudio-ai/klaudio/internal/config"
	"github.com/klaudio-ai/klaudio/internal/db"
	"github.com/klaudio-ai/klaudio/internal/stream"
)

// Orchestrator coordinates parallel execution of subtasks across multiple agents.
type Orchestrator struct {
	pool      *agent.Pool
	db        *db.DB
	streamHub *stream.Hub
	cfg       *config.Config
	comms     *CommsService
}

// NewOrchestrator creates a new Orchestrator.
func NewOrchestrator(pool *agent.Pool, database *db.DB, hub *stream.Hub, cfg *config.Config) *Orchestrator {
	return &Orchestrator{
		pool:      pool,
		db:        database,
		streamHub: hub,
		cfg:       cfg,
		comms:     NewCommsService(database),
	}
}

// completionEvent is sent on the internal channel when an agent finishes.
type completionEvent struct {
	AgentID   string
	SubtaskID string
	Result    agent.AgentResult
}

// Run executes an orchestration loop for the given plan.
// If the plan's mode is "collaborative", it delegates to RunCollaborative.
func (o *Orchestrator) Run(ctx context.Context, task *db.Task, plan *ExecutionPlan) error {
	// Check for collaborative mode
	if plan.Mode == "collaborative" {
		return o.RunCollaborative(ctx, task, plan)
	}

	logger := slog.With("task_id", task.ID, "component", "orchestrator")

	graph := BuildGraph(plan)

	if graph.HasCycles() {
		return fmt.Errorf("plan contains circular dependencies")
	}

	// Resolve workspace
	workspaceDir := filepath.Join(o.cfg.Storage.DataDir, "workspaces", task.ID)
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return fmt.Errorf("creating workspace directory: %w", err)
	}
	absWorkspace, err := filepath.Abs(workspaceDir)
	if err != nil {
		return fmt.Errorf("resolving workspace path: %w", err)
	}

	// Prepare .klaudio context directory
	os.MkdirAll(filepath.Join(absWorkspace, ".klaudio", "context"), 0o755)

	// Resolve team template for role prompt hints
	roleHints := o.resolveRoleHints(ctx, task)
	isTeam := len(plan.Subtasks) > 1

	// File-level lock service to prevent concurrent modification
	fileLocks := NewFileLockService()

	// Channel for agent completion notifications
	completionCh := make(chan completionEvent, 16)

	// Track running agents to know what we're waiting on
	var runningMu sync.Mutex
	running := make(map[string]string) // agentID -> subtaskID

	// spawnReady finds ready subtasks and spawns agents for them
	spawnReady := func() error {
		ready := graph.GetReady()
		for _, subtask := range ready {
			// Check file locks — skip if any file is held by another agent
			if len(subtask.FilesInvolved) > 0 {
				if !fileLocks.TryLock(subtask.ID, subtask.FilesInvolved) {
					logger.Debug("subtask deferred due to file lock conflict",
						"subtask_id", subtask.ID, "files", subtask.FilesInvolved)
					continue
				}
			}

			role := agent.AgentRole(subtask.AgentRole)
			if role == "" {
				role = agent.RoleDeveloper
			}

			// Build enriched prompt with dependency context and comms
			depContext := o.comms.CollectDependencyContext(*subtask, plan.Subtasks, absWorkspace)
			broadcastMsgs := o.comms.CollectBroadcastMessages(ctx, task.ID, subtask.ID)

			prompt := BuildSubtaskPrompt(*subtask, plan.Subtasks, plan.TaskPrompt, SubtaskPromptOpts{
				DependencyContext: depContext,
				BroadcastMessages: broadcastMsgs,
				RolePromptHint:    roleHints[subtask.AgentRole],
				IsTeamExecution:   isTeam,
				APIURL:            o.agentAPIURL(),
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
				logger.Debug("pool limit reached, will retry when slot opens")
				break
			}
			if err != nil {
				return fmt.Errorf("spawning agent for subtask %s: %w", subtask.ID, err)
			}

			graph.MarkRunning(subtask.ID)

			// Update plan subtask status
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

			// Monitor this agent in a goroutine
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
			})

			logger.Info("spawned agent for subtask", "subtask_id", subtask.ID, "agent_id", ag.ID)
		}
		return nil
	}

	// Initial spawn
	if err := spawnReady(); err != nil {
		return err
	}

	// Main orchestration loop
	for {
		if graph.AllCompleted() {
			break
		}

		// Check if there's nothing running and nothing ready (deadlock or all failed)
		runningMu.Lock()
		runCount := len(running)
		runningMu.Unlock()
		if runCount == 0 {
			ready := graph.GetReady()
			if len(ready) == 0 {
				break // Nothing to do: all done/failed/skipped
			}
		}

		select {
		case <-ctx.Done():
			logger.Info("orchestration cancelled")
			return ctx.Err()

		case ev := <-completionCh:
			runningMu.Lock()
			delete(running, ev.AgentID)
			runningMu.Unlock()

			// Release file locks for this subtask
			fileLocks.Release(ev.SubtaskID)

			if ev.Result.ExitCode == 0 && ev.Result.Error == nil {
				// Success — save context for dependent subtasks
				graph.Complete(ev.SubtaskID)

				// Find the subtask info
				var subtaskName string
				for _, st := range plan.Subtasks {
					if st.ID == ev.SubtaskID {
						subtaskName = st.Name
						break
					}
				}

				// Build a summary from the subtask info (agent output is streamed,
				// not easily captured here). The context file serves as a marker
				// and basic info for dependent subtasks.
				var subtaskDesc string
				for _, st := range plan.Subtasks {
					if st.ID == ev.SubtaskID {
						subtaskDesc = st.Description
						if subtaskDesc == "" {
							subtaskDesc = st.Prompt
						}
						break
					}
				}
				summary := fmt.Sprintf("Completed successfully.\n\nTask: %s\nDescription: %s", subtaskName, subtaskDesc)
				if err := o.comms.SaveSubtaskContext(ctx, task.ID, ev.SubtaskID, ev.AgentID, subtaskName, summary, absWorkspace); err != nil {
					logger.Warn("failed to save subtask context", "subtask_id", ev.SubtaskID, "error", err)
				}

				for i := range plan.Subtasks {
					if plan.Subtasks[i].ID == ev.SubtaskID {
						plan.Subtasks[i].Status = "completed"
						break
					}
				}
				o.updatePlanSubtasks(ctx, plan)

				o.recordEvent(ctx, task.ID, "subtask.completed", map[string]string{
					"subtask_id": ev.SubtaskID,
					"agent_id":   ev.AgentID,
				})

				logger.Info("subtask completed", "subtask_id", ev.SubtaskID, "agent_id", ev.AgentID)
			} else {
				// Failure
				graph.Fail(ev.SubtaskID)

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
				o.recordEvent(ctx, task.ID, "subtask.failed", map[string]string{
					"subtask_id": ev.SubtaskID,
					"agent_id":   ev.AgentID,
					"error":      errMsg,
					"exit_code":  fmt.Sprintf("%d", ev.Result.ExitCode),
				})

				logger.Error("subtask failed", "subtask_id", ev.SubtaskID,
					"exit_code", ev.Result.ExitCode, "error", ev.Result.Error)
			}

			// Try to spawn more agents now that a slot opened
			if err := spawnReady(); err != nil {
				return err
			}
		}
	}

	// Run reviewer if appropriate
	if o.shouldRunReviewer(plan) {
		logger.Info("running reviewer agent")
		reviewer := NewReviewer(o.pool, o.db)
		reviewResult, err := reviewer.Review(ctx, task, plan, absWorkspace)
		if err != nil {
			logger.Warn("reviewer failed", "error", err)
			// Reviewer failure is not fatal
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

// agentAPIURL returns the klaudio API base URL accessible from inside Docker containers.
func (o *Orchestrator) agentAPIURL() string {
	port := o.cfg.Server.Port
	if port == 0 {
		port = 8080
	}
	return fmt.Sprintf("http://host.docker.internal:%d", port)
}

// resolveRoleHints looks up the team template for a task and returns a map of
// role name -> prompt_hint for use in subtask prompt building.
func (o *Orchestrator) resolveRoleHints(ctx context.Context, task *db.Task) map[string]string {
	hints := make(map[string]string)
	if task.TeamTemplate == nil || *task.TeamTemplate == "" {
		return hints
	}

	tt, err := o.db.GetTeamTemplate(ctx, *task.TeamTemplate)
	if err != nil || tt == nil {
		return hints
	}

	var roles []db.TeamRole
	if err := json.Unmarshal([]byte(tt.Roles), &roles); err != nil {
		return hints
	}

	for _, r := range roles {
		if r.PromptHint != "" {
			hints[r.Name] = r.PromptHint
		}
	}
	return hints
}

// shouldRunReviewer returns true if we should run a reviewer agent.
func (o *Orchestrator) shouldRunReviewer(plan *ExecutionPlan) bool {
	// Run reviewer if plan has a reviewer subtask role
	for _, st := range plan.Subtasks {
		if st.AgentRole == "reviewer" {
			return true
		}
	}
	// Run reviewer if there were multiple agents (parallel execution)
	completedCount := 0
	for _, st := range plan.Subtasks {
		if st.Status == "completed" {
			completedCount++
		}
	}
	return completedCount > 1
}

// updatePlanSubtasks persists subtask status changes to the database.
func (o *Orchestrator) updatePlanSubtasks(ctx context.Context, plan *ExecutionPlan) {
	subtasksJSON, err := json.Marshal(plan.Subtasks)
	if err != nil {
		slog.Error("failed to marshal subtasks", "error", err)
		return
	}
	now := time.Now().UTC()
	_, err = o.db.ExecContext(ctx,
		"UPDATE plans SET subtasks = ?, updated_at = ? WHERE id = ?",
		string(subtasksJSON), now, plan.PlanID,
	)
	if err != nil {
		slog.Error("failed to update plan subtasks", "plan_id", plan.PlanID, "error", err)
	}
}

// recordEvent creates an event in the database.
func (o *Orchestrator) recordEvent(ctx context.Context, taskID, eventType string, data map[string]string) {
	var dataStr *string
	if data != nil {
		b, _ := json.Marshal(data)
		s := string(b)
		dataStr = &s
	}
	event := &db.Event{
		TaskID: taskID,
		Type:   eventType,
		Data:   dataStr,
	}
	if err := o.db.CreateEvent(ctx, event); err != nil {
		slog.Warn("failed to record event", "type", eventType, "task_id", taskID, "error", err)
	}
}
