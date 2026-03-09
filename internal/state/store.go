package state

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/klaudio-ai/klaudio/internal/db"
)

// StateStore manages checkpoint persistence for task state.
type StateStore struct {
	baseDir string
	db      *db.DB
	mu      sync.Mutex
}

// NewStateStore creates a new StateStore that writes checkpoints under baseDir.
func NewStateStore(baseDir string, database *db.DB) *StateStore {
	return &StateStore{
		baseDir: baseDir,
		db:      database,
	}
}

// SaveCheckpoint creates a full checkpoint of the task state.
// It copies workspace files, Claude memory, agent logs, and metadata.
// The save is atomic: data is written to a temp directory and renamed on success.
func (s *StateStore) SaveCheckpoint(ctx context.Context, taskID string, opts SaveOpts) (*db.Checkpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.doSaveCheckpoint(ctx, taskID, opts)
}

// SaveCheckpointLive creates a checkpoint without stopping containers.
// It uses docker cp on running containers to copy workspace and memory files.
func (s *StateStore) SaveCheckpointLive(ctx context.Context, taskID string, opts SaveOpts) (*db.Checkpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.doSaveCheckpoint(ctx, taskID, opts)
}

// doSaveCheckpoint is the internal implementation shared by SaveCheckpoint and SaveCheckpointLive.
func (s *StateStore) doSaveCheckpoint(ctx context.Context, taskID string, opts SaveOpts) (*db.Checkpoint, error) {
	logger := slog.With("task_id", taskID, "component", "state_store")

	checkpointID := uuid.New().String()
	stateDir := filepath.Join(s.baseDir, taskID, checkpointID)
	tmpDir := stateDir + ".tmp"

	// Clean up temp dir on error
	success := false
	defer func() {
		if !success {
			os.RemoveAll(tmpDir)
		}
	}()

	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating temp state directory: %w", err)
	}

	// 1. Copy workspace files
	if err := s.copyWorkspace(ctx, tmpDir, opts); err != nil {
		logger.Warn("failed to copy workspace", "error", err)
		// Non-fatal: continue saving other state
	}

	// 2. Copy Claude memory from container
	if err := s.copyClaudeMemory(ctx, tmpDir, opts); err != nil {
		logger.Warn("failed to copy Claude memory", "error", err)
	}

	// 3. Save agent output logs from StreamHub buffers
	if err := s.saveAgentLogs(tmpDir, opts); err != nil {
		logger.Warn("failed to save agent logs", "error", err)
	}

	// 4. Save plan progress
	if err := writeJSON(filepath.Join(tmpDir, "plan-progress.json"), opts.PlanProgress); err != nil {
		return nil, fmt.Errorf("writing plan progress: %w", err)
	}

	// 5. Build resume prompt
	resumePrompt := s.buildResumePrompt(opts)

	// 6. Build and save checkpoint metadata
	now := time.Now().UTC()

	agentStatesJSON, err := marshalOptional(opts.AgentStates)
	if err != nil {
		return nil, fmt.Errorf("marshaling agent states: %w", err)
	}

	repoStateJSON, err := marshalOptional(opts.RepoState)
	if err != nil {
		return nil, fmt.Errorf("marshaling repo state: %w", err)
	}

	planProgressJSON, err := marshalRequired(opts.PlanProgress)
	if err != nil {
		return nil, fmt.Errorf("marshaling plan progress: %w", err)
	}

	// Write checkpoint.json metadata file
	checkpointMeta := map[string]any{
		"id":            checkpointID,
		"task_id":       taskID,
		"created_at":    now,
		"plan_progress": opts.PlanProgress,
		"agent_states":  opts.AgentStates,
		"repo_state":    opts.RepoState,
		"resume_prompt": resumePrompt,
	}
	if err := writeJSON(filepath.Join(tmpDir, "checkpoint.json"), checkpointMeta); err != nil {
		return nil, fmt.Errorf("writing checkpoint metadata: %w", err)
	}

	// Atomic rename: tmp -> final
	if err := os.Rename(tmpDir, stateDir); err != nil {
		return nil, fmt.Errorf("renaming temp dir to final: %w", err)
	}
	success = true

	// Calculate size
	size, _ := calculateDirSize(stateDir)

	// Build DB checkpoint record
	cp := &db.Checkpoint{
		ID:           checkpointID,
		TaskID:       taskID,
		StateDir:     stateDir,
		PlanProgress: planProgressJSON,
		AgentStates:  agentStatesJSON,
		RepoState:    repoStateJSON,
		ResumePrompt: &resumePrompt,
		SizeBytes:    &size,
		CreatedAt:    now,
	}

	// Save to DB
	if err := s.db.CreateCheckpoint(ctx, cp); err != nil {
		logger.Error("failed to save checkpoint to DB", "error", err)
		return nil, fmt.Errorf("saving checkpoint to DB: %w", err)
	}

	logger.Info("checkpoint saved", "checkpoint_id", checkpointID, "size_bytes", size)
	return cp, nil
}

// copyWorkspace copies the workspace directory either from the host filesystem
// or from a running/stopped Docker container.
func (s *StateStore) copyWorkspace(ctx context.Context, tmpDir string, opts SaveOpts) error {
	workspaceDst := filepath.Join(tmpDir, "workspace")

	// If container IDs are provided and we have a docker manager, copy from container
	if len(opts.ContainerIDs) > 0 && opts.DockerManager != nil {
		if err := opts.DockerManager.CopyFromContainer(ctx, opts.ContainerIDs[0], "/home/agent/workspace/", workspaceDst); err != nil {
			return fmt.Errorf("copying workspace from container: %w", err)
		}
		return nil
	}

	// Otherwise copy from host workspace dir
	if opts.WorkspaceDir != "" {
		if _, err := os.Stat(opts.WorkspaceDir); err == nil {
			return copyDir(opts.WorkspaceDir, workspaceDst)
		}
	}

	return nil
}

// copyClaudeMemory copies the .claude/ directory from a container.
func (s *StateStore) copyClaudeMemory(ctx context.Context, tmpDir string, opts SaveOpts) error {
	if len(opts.ContainerIDs) == 0 || opts.DockerManager == nil {
		return nil
	}

	memoryDst := filepath.Join(tmpDir, "claude-memory")
	if err := opts.DockerManager.CopyFromContainer(ctx, opts.ContainerIDs[0], "/home/agent/.claude/", memoryDst); err != nil {
		return fmt.Errorf("copying Claude memory from container: %w", err)
	}
	return nil
}

// saveAgentLogs saves output logs from the StreamHub ring buffers.
func (s *StateStore) saveAgentLogs(tmpDir string, opts SaveOpts) error {
	if opts.StreamHub == nil || len(opts.AgentStates) == 0 {
		return nil
	}

	for _, as := range opts.AgentStates {
		agentDir := filepath.Join(tmpDir, "agent-states", as.AgentID)
		if err := os.MkdirAll(agentDir, 0o755); err != nil {
			return fmt.Errorf("creating agent state dir: %w", err)
		}

		// Get buffer data from StreamHub
		bufferData := opts.StreamHub.GetFullBuffer(as.AgentID)
		if len(bufferData) > 0 {
			if err := os.WriteFile(filepath.Join(agentDir, "output.log"), bufferData, 0o644); err != nil {
				return fmt.Errorf("writing agent output log: %w", err)
			}
		}

		// Save agent metadata
		if err := writeJSON(filepath.Join(agentDir, "metadata.json"), as); err != nil {
			return fmt.Errorf("writing agent metadata: %w", err)
		}
	}

	return nil
}

// buildResumePrompt constructs the prompt used when resuming execution.
func (s *StateStore) buildResumePrompt(opts SaveOpts) string {
	var b strings.Builder

	b.WriteString("You are resuming a previously interrupted task.\n\n")

	b.WriteString("## Original Task\n")
	b.WriteString(opts.TaskPrompt)
	b.WriteString("\n\n")

	if opts.PlanJSON != "" {
		b.WriteString("## Execution Plan\n")
		b.WriteString(opts.PlanJSON)
		b.WriteString("\n\n")
	}

	b.WriteString("## Progress\n")
	if len(opts.PlanProgress.CompletedSubtasks) > 0 {
		b.WriteString("Completed subtasks:\n")
		for _, id := range opts.PlanProgress.CompletedSubtasks {
			b.WriteString("- " + id + " (completed)\n")
		}
	}
	if opts.PlanProgress.CurrentSubtask != "" {
		b.WriteString("Current subtask (to resume): " + opts.PlanProgress.CurrentSubtask + "\n")
	}
	if len(opts.PlanProgress.FailedSubtasks) > 0 {
		b.WriteString("Failed subtasks (need retry):\n")
		for _, id := range opts.PlanProgress.FailedSubtasks {
			b.WriteString("- " + id + " (failed)\n")
		}
	}
	b.WriteString("\n")

	// Add last output excerpt from agents
	if len(opts.AgentStates) > 0 {
		for _, as := range opts.AgentStates {
			if as.LastOutput != "" {
				b.WriteString("## Last Output (agent " + as.AgentID + ")\n")
				// Truncate to last 4KB
				output := as.LastOutput
				if len(output) > 4096 {
					output = output[len(output)-4096:]
				}
				b.WriteString(output)
				b.WriteString("\n\n")
			}
		}
	}

	b.WriteString("## Instructions\n")
	b.WriteString("Resume execution from the current subtask. The workspace files reflect the state at the time of interruption. ")
	b.WriteString("Your memory directory contains previously saved context. ")
	b.WriteString("Continue without redoing completed subtasks.\n")

	return b.String()
}

// RestoreCheckpoint loads a checkpoint and returns all paths and data needed to resume.
func (s *StateStore) RestoreCheckpoint(ctx context.Context, taskID string) (*RestoreResult, error) {
	// Get the latest checkpoint from DB
	cp, err := s.db.GetLatestCheckpoint(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("getting latest checkpoint: %w", err)
	}
	if cp == nil {
		return nil, fmt.Errorf("no checkpoint found for task %s", taskID)
	}

	stateDir := cp.StateDir

	// Verify the state directory exists
	if _, err := os.Stat(stateDir); err != nil {
		return nil, fmt.Errorf("state directory %s not found: %w", stateDir, err)
	}

	// Read checkpoint metadata for the resume prompt
	var meta map[string]any
	metaPath := filepath.Join(stateDir, "checkpoint.json")
	if err := readJSON(metaPath, &meta); err != nil {
		slog.Warn("could not read checkpoint metadata", "error", err)
	}

	workspaceDir := filepath.Join(stateDir, "workspace")
	claudeMemoryDir := filepath.Join(stateDir, "claude-memory")

	resumePrompt := ""
	if cp.ResumePrompt != nil {
		resumePrompt = *cp.ResumePrompt
	}

	return &RestoreResult{
		Checkpoint:      cp,
		WorkspaceDir:    workspaceDir,
		ClaudeMemoryDir: claudeMemoryDir,
		ResumePrompt:    resumePrompt,
	}, nil
}

// ListCheckpoints returns summary info for all checkpoints of a task, newest first.
func (s *StateStore) ListCheckpoints(taskID string) ([]CheckpointInfo, error) {
	taskDir := filepath.Join(s.baseDir, taskID)
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading task state dir: %w", err)
	}

	var infos []CheckpointInfo
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasSuffix(entry.Name(), ".tmp") {
			continue
		}

		cpDir := filepath.Join(taskDir, entry.Name())
		size, _ := calculateDirSize(cpDir)

		fi, fiErr := entry.Info()
		var createdAt time.Time
		if fiErr == nil {
			createdAt = fi.ModTime()
		}

		infos = append(infos, CheckpointInfo{
			ID:        entry.Name(),
			TaskID:    taskID,
			CreatedAt: createdAt,
			SizeBytes: size,
		})
	}

	// Sort newest first
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].CreatedAt.After(infos[j].CreatedAt)
	})

	return infos, nil
}

// DeleteCheckpoint removes a single checkpoint from disk and DB.
func (s *StateStore) DeleteCheckpoint(ctx context.Context, taskID, checkpointID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cpDir := filepath.Join(s.baseDir, taskID, checkpointID)
	if err := os.RemoveAll(cpDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing checkpoint dir: %w", err)
	}

	if err := s.db.DeleteCheckpoint(ctx, checkpointID); err != nil {
		slog.Warn("failed to delete checkpoint from DB", "checkpoint_id", checkpointID, "error", err)
	}

	return nil
}

// Cleanup removes old checkpoints based on retention policy.
// It keeps at most maxPerTask checkpoints per task and removes checkpoints older than retentionDays.
func (s *StateStore) Cleanup(ctx context.Context, retentionDays int, maxPerTask int) error {
	logger := slog.With("component", "state_store")

	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading base state dir: %w", err)
	}

	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)

	for _, taskEntry := range entries {
		if !taskEntry.IsDir() {
			continue
		}
		taskID := taskEntry.Name()

		checkpoints, err := s.ListCheckpoints(taskID)
		if err != nil {
			logger.Warn("failed to list checkpoints for cleanup", "task_id", taskID, "error", err)
			continue
		}

		for i, cp := range checkpoints {
			shouldDelete := false

			// Delete if beyond max per task
			if maxPerTask > 0 && i >= maxPerTask {
				shouldDelete = true
			}

			// Delete if older than retention period
			if cp.CreatedAt.Before(cutoff) {
				shouldDelete = true
			}

			if shouldDelete {
				if err := s.DeleteCheckpoint(ctx, taskID, cp.ID); err != nil {
					logger.Warn("failed to delete checkpoint during cleanup",
						"task_id", taskID, "checkpoint_id", cp.ID, "error", err)
				} else {
					logger.Info("cleaned up checkpoint", "task_id", taskID, "checkpoint_id", cp.ID)
				}
			}
		}
	}

	return nil
}

// marshalOptional marshals a value to a JSON string pointer, returning nil for nil input.
func marshalOptional(v any) (*string, error) {
	if v == nil {
		return nil, nil
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	s := string(data)
	return &s, nil
}

// marshalRequired marshals a value to a JSON string.
func marshalRequired(v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
