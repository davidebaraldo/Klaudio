package task

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/klaudio-ai/klaudio/internal/config"
	"github.com/klaudio-ai/klaudio/internal/db"
	"github.com/klaudio-ai/klaudio/internal/docker"
	"github.com/klaudio-ai/klaudio/internal/state"
	"github.com/klaudio-ai/klaudio/internal/stream"
)

// Executor runs subtasks sequentially by spawning a container for each one.
type Executor struct {
	docker     *docker.Manager
	db         *db.DB
	cfg        *config.Config
	stateStore *state.StateStore
	streamHub  *stream.Hub
	repoMemory string // cached repo analysis content (optional)
}

// NewExecutor creates a new Executor.
func NewExecutor(dockerMgr *docker.Manager, database *db.DB, cfg *config.Config, ss *state.StateStore, hub *stream.Hub) *Executor {
	return &Executor{
		docker:     dockerMgr,
		db:         database,
		cfg:        cfg,
		stateStore: ss,
		streamHub:  hub,
	}
}

// ExecutionResult holds the outcome of executing all subtasks.
type ExecutionResult struct {
	CompletedSubtasks []string
	FailedSubtasks    []string
	Error             error
}

// Execute runs all subtasks in the plan sequentially.
// It updates subtask status in the plan as each subtask completes.
// The function respects context cancellation for stop/pause.
// When auto-save is enabled, it periodically saves checkpoints during execution.
func (e *Executor) Execute(ctx context.Context, task *db.Task, plan *db.Plan) *ExecutionResult {
	logger := slog.With("task_id", task.ID, "component", "executor")

	var subtasks []db.Subtask
	if err := json.Unmarshal([]byte(plan.Subtasks), &subtasks); err != nil {
		return &ExecutionResult{Error: fmt.Errorf("unmarshaling subtasks: %w", err)}
	}

	result := &ExecutionResult{}

	// Start auto-save if enabled
	var autoSaver *state.AutoSaver
	if e.stateStore != nil && e.cfg.State.AutoSaveEnabled {
		workspaceDir := filepath.Join(e.cfg.Storage.DataDir, "workspaces", task.ID)
		saveOpts := state.SaveOpts{
			WorkspaceDir:  workspaceDir,
			DockerManager: e.docker,
			TaskPrompt:    task.Prompt,
			PlanJSON:      plan.Subtasks,
		}
		autoSaver = state.NewAutoSaver(e.stateStore, e.cfg.State.AutoSaveInterval)
		autoSaver.Start(ctx, task.ID, saveOpts)
		defer autoSaver.Stop()
	}

	for i, subtask := range subtasks {
		// Check if context is cancelled (task stopped)
		select {
		case <-ctx.Done():
			logger.Info("execution cancelled", "reason", ctx.Err())

			// Save final checkpoint on graceful stop
			if e.stateStore != nil {
				e.saveFinalCheckpoint(task, plan, subtasks, result)
			}
			return result
		default:
		}

		// Skip already completed subtasks (for resume)
		if subtask.Status == "completed" {
			result.CompletedSubtasks = append(result.CompletedSubtasks, subtask.ID)
			continue
		}

		// Verify dependencies are met
		if !e.dependenciesMet(subtasks, subtask) {
			errMsg := fmt.Sprintf("dependencies not met for subtask %s", subtask.ID)
			logger.Error(errMsg)
			result.FailedSubtasks = append(result.FailedSubtasks, subtask.ID)
			result.Error = fmt.Errorf("%s", errMsg)
			break
		}

		// Execute the subtask
		logger.Info("executing subtask", "subtask_id", subtask.ID, "subtask_name", subtask.Name)

		// Update subtask status to running
		subtasks[i].Status = "running"
		e.updatePlanSubtasks(ctx, plan.ID, subtasks)

		err := e.executeSubtask(ctx, task, plan, &subtasks[i], subtasks)
		if err != nil {
			logger.Error("subtask failed", "subtask_id", subtask.ID, "error", err)
			subtasks[i].Status = "failed"
			e.updatePlanSubtasks(ctx, plan.ID, subtasks)
			result.FailedSubtasks = append(result.FailedSubtasks, subtask.ID)
			result.Error = fmt.Errorf("subtask %s failed: %w", subtask.ID, err)

			// Record event
			e.recordEvent(ctx, task.ID, "subtask.failed", map[string]string{
				"subtask_id": subtask.ID,
				"error":      err.Error(),
			})
			break
		}

		subtasks[i].Status = "completed"
		e.updatePlanSubtasks(ctx, plan.ID, subtasks)
		result.CompletedSubtasks = append(result.CompletedSubtasks, subtask.ID)

		// Record event
		e.recordEvent(ctx, task.ID, "subtask.completed", map[string]string{
			"subtask_id": subtask.ID,
		})

		logger.Info("subtask completed", "subtask_id", subtask.ID)
	}

	return result
}

// saveFinalCheckpoint saves a checkpoint when execution is being stopped gracefully.
func (e *Executor) saveFinalCheckpoint(task *db.Task, plan *db.Plan, subtasks []db.Subtask, result *ExecutionResult) {
	logger := slog.With("task_id", task.ID, "component", "executor")

	progress := db.PlanProgress{
		PlanID:            plan.ID,
		CompletedSubtasks: result.CompletedSubtasks,
		FailedSubtasks:    result.FailedSubtasks,
	}
	for _, st := range subtasks {
		if st.Status == "running" {
			progress.CurrentSubtask = st.ID
			break
		}
	}

	workspaceDir := filepath.Join(e.cfg.Storage.DataDir, "workspaces", task.ID)
	saveOpts := state.SaveOpts{
		WorkspaceDir:  workspaceDir,
		PlanProgress:  progress,
		DockerManager: e.docker,
		TaskPrompt:    task.Prompt,
		PlanJSON:      plan.Subtasks,
	}

	bgCtx := context.Background()
	if _, err := e.stateStore.SaveCheckpoint(bgCtx, task.ID, saveOpts); err != nil {
		logger.Warn("failed to save final checkpoint on stop", "error", err)
	} else {
		logger.Info("final checkpoint saved on stop")
	}
}

// executeSubtask runs a single subtask in a container.
func (e *Executor) executeSubtask(ctx context.Context, task *db.Task, plan *db.Plan, subtask *db.Subtask, allSubtasks []db.Subtask) error {
	logger := slog.With("task_id", task.ID, "subtask_id", subtask.ID, "component", "executor")

	// Build the prompt with context from dependencies
	prompt := BuildSubtaskPrompt(*subtask, allSubtasks, task.Prompt, SubtaskPromptOpts{
		RepoMemory: e.repoMemory,
	})

	// Resolve workspace path
	workspaceDir := filepath.Join(e.cfg.Storage.DataDir, "workspaces", task.ID)
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return fmt.Errorf("creating workspace directory: %w", err)
	}

	absWorkspace, err := filepath.Abs(workspaceDir)
	if err != nil {
		return fmt.Errorf("resolving workspace path: %w", err)
	}

	// Create agent record
	agentID := uuid.New().String()
	agent := &db.Agent{
		ID:        agentID,
		TaskID:    task.ID,
		SubtaskID: &subtask.ID,
		Role:      subtask.AgentRole,
		Status:    "created",
		CreatedAt: time.Now().UTC(),
	}
	if agent.Role == "" {
		agent.Role = "developer"
	}
	if err := e.db.CreateAgent(ctx, agent); err != nil {
		return fmt.Errorf("creating agent record: %w", err)
	}

	// Record agent spawn event
	e.recordEvent(ctx, task.ID, "agent.spawned", map[string]string{
		"agent_id":   agentID,
		"subtask_id": subtask.ID,
	})

	// Create the container
	containerName := fmt.Sprintf("klaudio-%s-%s", task.ID[:8], subtask.ID)
	containerID, err := e.docker.CreateContainer(ctx, docker.ContainerOpts{
		Name:   containerName,
		Prompt: prompt,
		Volumes: []docker.VolumeMount{
			{
				HostPath:      absWorkspace,
				ContainerPath: "/home/agent/workspace",
				ReadOnly:      false, // Executor can write
			},
		},
	})
	if err != nil {
		return fmt.Errorf("creating container: %w", err)
	}

	// Update agent with container ID
	if err := e.db.UpdateAgentContainer(ctx, agentID, containerID); err != nil {
		logger.Error("failed to update agent container", "error", err)
	}

	subtask.AgentID = agentID

	// Register agent stream for real-time output
	var agentStream *stream.AgentStream
	if e.streamHub != nil {
		agentStream = e.streamHub.RegisterAgent(agentID, task.ID)
	}

	// Attach BEFORE starting — with Tty:true, we must attach first to not miss output
	if agentStream != nil {
		reader, _, attachErr := e.docker.AttachContainer(ctx, containerID)
		if attachErr != nil {
			logger.Warn("failed to attach to subtask container", "error", attachErr)
		} else {
			logger.Info("attached to subtask container for streaming")
			go func() {
				totalBytes := 0
				buf := make([]byte, 4096)
				for {
					n, readErr := reader.Read(buf)
					if n > 0 {
						totalBytes += n
						data := make([]byte, n)
						copy(data, buf[:n])
						select {
						case agentStream.OutputCh <- data:
						default:
							logger.Warn("dropping subtask output, channel full")
						}
					}
					if readErr != nil {
						logger.Info("subtask attach reader ended", "total_bytes", totalBytes, "error", readErr)
						return
					}
				}
			}()
		}
	}

	// Start the container
	if err := e.docker.StartContainer(ctx, containerID); err != nil {
		e.docker.RemoveContainer(ctx, containerID)
		if e.streamHub != nil {
			e.streamHub.UnregisterAgent(agentID)
		}
		return fmt.Errorf("starting container: %w", err)
	}

	logger.Info("subtask container started", "container_id", containerID)

	e.recordEvent(ctx, task.ID, "subtask.started", map[string]string{
		"subtask_id":   subtask.ID,
		"agent_id":     agentID,
		"container_id": containerID,
	})

	// Wait for the container to finish
	exitCh, errCh := e.docker.WaitContainer(ctx, containerID)

	select {
	case <-ctx.Done():
		// Context cancelled — stop the container gracefully
		logger.Info("stopping subtask container due to cancellation")
		if e.streamHub != nil {
			e.streamHub.UnregisterAgent(agentID)
		}
		e.docker.StopContainer(context.Background(), containerID, 10)
		e.docker.RemoveContainer(context.Background(), containerID)
		e.db.UpdateAgentStatus(context.Background(), agentID, "stopped")

		e.recordEvent(context.Background(), task.ID, "agent.stopped", map[string]string{
			"agent_id":   agentID,
			"subtask_id": subtask.ID,
		})
		return ctx.Err()

	case exitCode := <-exitCh:
		waitErr := <-errCh

		logger.Info("subtask container finished", "exit_code", exitCode, "container_id", containerID)

		// Unregister stream
		if e.streamHub != nil {
			e.streamHub.UnregisterAgent(agentID)
		}

		// Collect logs
		logsReader, logErr := e.docker.ContainerLogs(ctx, containerID)
		if logErr == nil {
			outputBytes, _ := io.ReadAll(logsReader)
			logsReader.Close()
			_ = stripDockerLogHeaders(outputBytes) // logs available if needed
		}

		// Clean up
		if removeErr := e.docker.RemoveContainer(ctx, containerID); removeErr != nil {
			logger.Warn("failed to remove container", "error", removeErr)
		}

		// Update agent
		var agentErr *string
		if waitErr != nil {
			s := waitErr.Error()
			agentErr = &s
		}
		e.db.UpdateAgentCompleted(ctx, agentID, int(exitCode), agentErr)

		e.recordEvent(ctx, task.ID, "agent.stopped", map[string]string{
			"agent_id":   agentID,
			"subtask_id": subtask.ID,
			"exit_code":  fmt.Sprintf("%d", exitCode),
		})

		if waitErr != nil {
			return fmt.Errorf("container error: %w", waitErr)
		}
		if exitCode != 0 {
			return fmt.Errorf("container exited with code %d", exitCode)
		}
		return nil
	}
}

// dependenciesMet checks if all dependencies of a subtask have been completed.
func (e *Executor) dependenciesMet(subtasks []db.Subtask, subtask db.Subtask) bool {
	for _, depID := range subtask.DependsOn {
		found := false
		for _, s := range subtasks {
			if s.ID == depID {
				found = true
				if s.Status != "completed" {
					return false
				}
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// updatePlanSubtasks persists updated subtask statuses to the plan in the DB.
func (e *Executor) updatePlanSubtasks(ctx context.Context, planID string, subtasks []db.Subtask) {
	subtasksJSON, err := json.Marshal(subtasks)
	if err != nil {
		slog.Error("failed to marshal subtasks for update", "error", err)
		return
	}
	now := time.Now().UTC()
	_, err = e.db.ExecContext(ctx,
		"UPDATE plans SET subtasks = ?, updated_at = ? WHERE id = ?",
		string(subtasksJSON), now, planID,
	)
	if err != nil {
		slog.Error("failed to update plan subtasks", "plan_id", planID, "error", err)
	}
}

// recordEvent creates an event in the database.
func (e *Executor) recordEvent(ctx context.Context, taskID, eventType string, data map[string]string) {
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
	if err := e.db.CreateEvent(ctx, event); err != nil {
		slog.Warn("failed to record event", "type", eventType, "task_id", taskID, "error", err)
	}
}
