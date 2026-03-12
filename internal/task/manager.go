package task

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/klaudio-ai/klaudio/internal/agent"
	"github.com/klaudio-ai/klaudio/internal/config"
	"github.com/klaudio-ai/klaudio/internal/db"
	"github.com/klaudio-ai/klaudio/internal/docker"
	"github.com/klaudio-ai/klaudio/internal/repo"
	"github.com/klaudio-ai/klaudio/internal/state"
	"github.com/klaudio-ai/klaudio/internal/stream"
)

// TaskManager is the central orchestrator for task lifecycle operations.
// It manages state transitions, planner invocations, and subtask execution.
type TaskManager struct {
	db           *db.DB
	docker       *docker.Manager
	cfg          *config.Config
	planner      *Planner
	executor     *Executor
	stateStore   *state.StateStore
	Pool         *agent.Pool
	streamHub    *stream.Hub
	orchestrator *Orchestrator

	// mu guards the cancels map.
	mu      sync.Mutex
	cancels map[string]context.CancelFunc // taskID -> cancel function for running tasks
}

// NewTaskManager creates a new TaskManager with all dependencies.
func NewTaskManager(database *db.DB, dockerMgr *docker.Manager, cfg *config.Config, hub *stream.Hub) *TaskManager {
	ss := state.NewStateStore(cfg.Storage.StatesDir, database)
	pool := agent.NewPool(dockerMgr, hub, database, cfg)
	return &TaskManager{
		db:           database,
		docker:       dockerMgr,
		cfg:          cfg,
		planner:      NewPlanner(dockerMgr, database, cfg, hub),
		executor:     NewExecutor(dockerMgr, database, cfg, ss, hub),
		stateStore:   ss,
		Pool:         pool,
		streamHub:    hub,
		orchestrator: NewOrchestrator(pool, database, hub, cfg),
		cancels:      make(map[string]context.CancelFunc),
	}
}

// validTransitions defines which state transitions are allowed.
var validTransitions = map[db.TaskStatus][]db.TaskStatus{
	db.TaskStatusCreated:  {db.TaskStatusPlanning},
	db.TaskStatusPlanning: {db.TaskStatusPlanned, db.TaskStatusFailed},
	db.TaskStatusPlanned:  {db.TaskStatusApproved},
	db.TaskStatusApproved: {db.TaskStatusRunning},
	db.TaskStatusRunning:  {db.TaskStatusPaused, db.TaskStatusCompleted, db.TaskStatusFailed},
	db.TaskStatusPaused:   {db.TaskStatusRunning},
	db.TaskStatusFailed:   {db.TaskStatusPlanning},
}

// canTransition checks if a state transition is valid.
func canTransition(from, to db.TaskStatus) bool {
	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// Create creates a new task in the database and returns it.
func (tm *TaskManager) Create(ctx context.Context, name, prompt string, repoConfig *string, teamTemplate *string, outputFiles *string) (*db.Task, error) {
	now := time.Now().UTC()
	task := &db.Task{
		ID:           uuid.New().String(),
		Name:         name,
		Prompt:       prompt,
		Status:       db.TaskStatusCreated,
		RepoConfig:   repoConfig,
		TeamTemplate: teamTemplate,
		OutputFiles:  outputFiles,
		HasState:     false,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := tm.db.CreateTask(ctx, task); err != nil {
		return nil, fmt.Errorf("creating task: %w", err)
	}

	tm.recordEvent(ctx, task.ID, "task.created", nil)
	return task, nil
}

// Start transitions a task from "created" to "planning" and launches the planner agent.
func (tm *TaskManager) Start(ctx context.Context, taskID string) error {
	task, err := tm.getTaskOrError(ctx, taskID)
	if err != nil {
		return err
	}

	if !canTransition(task.Status, db.TaskStatusPlanning) {
		return fmt.Errorf("cannot start task in status %q", task.Status)
	}

	if err := tm.db.UpdateTaskStatus(ctx, taskID, db.TaskStatusPlanning); err != nil {
		return fmt.Errorf("updating task status to planning: %w", err)
	}

	tm.recordEvent(ctx, taskID, "task.started", nil)

	// Launch planner asynchronously
	go tm.runPlanner(taskID, "")

	return nil
}

// Approve transitions a task from "planned" to "approved" and starts execution.
// An optional modified plan can be provided for last-minute edits.
func (tm *TaskManager) Approve(ctx context.Context, taskID string, modifiedPlan *string) error {
	task, err := tm.getTaskOrError(ctx, taskID)
	if err != nil {
		return err
	}

	if !canTransition(task.Status, db.TaskStatusApproved) {
		return fmt.Errorf("cannot approve task in status %q", task.Status)
	}

	// If a modified plan was provided, update it
	if modifiedPlan != nil {
		plan, err := tm.db.GetPlanByTask(ctx, taskID)
		if err != nil || plan == nil {
			return fmt.Errorf("no plan found for task %s", taskID)
		}
		now := time.Now().UTC()
		_, err = tm.db.ExecContext(ctx,
			"UPDATE plans SET subtasks = ?, status = 'approved', updated_at = ? WHERE id = ?",
			*modifiedPlan, now, plan.ID,
		)
		if err != nil {
			return fmt.Errorf("updating plan: %w", err)
		}
	} else {
		// Mark the plan as approved
		plan, err := tm.db.GetPlanByTask(ctx, taskID)
		if err != nil || plan == nil {
			return fmt.Errorf("no plan found for task %s", taskID)
		}
		now := time.Now().UTC()
		_, err = tm.db.ExecContext(ctx,
			"UPDATE plans SET status = 'approved', updated_at = ? WHERE id = ?",
			now, plan.ID,
		)
		if err != nil {
			return fmt.Errorf("updating plan status: %w", err)
		}
	}

	// Transition: planned -> approved
	if err := tm.db.UpdateTaskStatus(ctx, taskID, db.TaskStatusApproved); err != nil {
		return fmt.Errorf("updating task status to approved: %w", err)
	}

	tm.recordEvent(ctx, taskID, "plan.approved", nil)

	// Automatically start execution: approved -> running
	if err := tm.db.UpdateTaskStarted(ctx, taskID); err != nil {
		return fmt.Errorf("updating task to running: %w", err)
	}

	// Launch executor asynchronously
	go tm.runExecutor(taskID)

	return nil
}

// Stop pauses a running task, saving a checkpoint before stopping containers.
func (tm *TaskManager) Stop(ctx context.Context, taskID string) error {
	task, err := tm.getTaskOrError(ctx, taskID)
	if err != nil {
		return err
	}

	if !canTransition(task.Status, db.TaskStatusPaused) {
		return fmt.Errorf("cannot stop task in status %q", task.Status)
	}

	// Gather running container IDs and agent states for checkpoint
	agents, _ := tm.db.ListAgentsByTask(ctx, taskID)
	var containerIDs []string
	var agentStates []db.AgentState
	for _, agent := range agents {
		if agent.ContainerID != nil && agent.Status == "running" {
			containerIDs = append(containerIDs, *agent.ContainerID)
		}
		subtaskID := ""
		if agent.SubtaskID != nil {
			subtaskID = *agent.SubtaskID
		}
		agentStates = append(agentStates, db.AgentState{
			AgentID:   agent.ID,
			SubtaskID: subtaskID,
			Status:    agent.Status,
		})
	}

	// Build plan progress from current plan state
	planProgress := db.PlanProgress{}
	plan, planErr := tm.db.GetPlanByTask(ctx, taskID)
	planJSON := ""
	if planErr == nil && plan != nil {
		planJSON = plan.Subtasks
		planProgress.PlanID = plan.ID
		var subtasks []db.Subtask
		if err := json.Unmarshal([]byte(plan.Subtasks), &subtasks); err == nil {
			for _, st := range subtasks {
				switch st.Status {
				case "completed":
					planProgress.CompletedSubtasks = append(planProgress.CompletedSubtasks, st.ID)
				case "failed":
					planProgress.FailedSubtasks = append(planProgress.FailedSubtasks, st.ID)
				case "running":
					planProgress.CurrentSubtask = st.ID
				}
			}
		}
	}

	// Save checkpoint before stopping containers
	workspaceDir := filepath.Join(tm.cfg.Storage.DataDir, "workspaces", taskID)
	saveOpts := state.SaveOpts{
		WorkspaceDir:  workspaceDir,
		ContainerIDs:  containerIDs,
		PlanProgress:  planProgress,
		AgentStates:   agentStates,
		DockerManager: tm.docker,
		TaskPrompt:    task.Prompt,
		PlanJSON:      planJSON,
	}

	if _, saveErr := tm.stateStore.SaveCheckpoint(ctx, taskID, saveOpts); saveErr != nil {
		slog.Warn("failed to save checkpoint on stop", "task_id", taskID, "error", saveErr)
	} else {
		// Mark task as having saved state
		_ = tm.db.UpdateTaskHasState(ctx, taskID, true)
	}

	// Terminate pool agents for this task
	if tm.Pool != nil {
		if poolErr := tm.Pool.TerminateTaskAgents(taskID); poolErr != nil {
			slog.Warn("failed to terminate pool agents on stop", "task_id", taskID, "error", poolErr)
		}
	}

	// Cancel the running context
	tm.mu.Lock()
	cancel, ok := tm.cancels[taskID]
	tm.mu.Unlock()
	if ok {
		cancel()
	}

	now := time.Now().UTC()
	_, err = tm.db.ExecContext(ctx,
		"UPDATE tasks SET status = 'paused', paused_at = ?, updated_at = ? WHERE id = ?",
		now, now, taskID,
	)
	if err != nil {
		return fmt.Errorf("updating task to paused: %w", err)
	}

	tm.recordEvent(ctx, taskID, "task.paused", nil)
	return nil
}

// Resume resumes a paused task, restoring from checkpoint and continuing execution.
func (tm *TaskManager) Resume(ctx context.Context, taskID string) error {
	task, err := tm.getTaskOrError(ctx, taskID)
	if err != nil {
		return err
	}

	if !canTransition(task.Status, db.TaskStatusRunning) {
		return fmt.Errorf("cannot resume task in status %q", task.Status)
	}

	// Try to restore checkpoint if task has saved state
	if task.HasState {
		restoreResult, restoreErr := tm.stateStore.RestoreCheckpoint(ctx, taskID)
		if restoreErr != nil {
			slog.Warn("failed to restore checkpoint, resuming without state",
				"task_id", taskID, "error", restoreErr)
		} else {
			// Copy restored workspace to the working workspace directory
			workspaceDir := filepath.Join(tm.cfg.Storage.DataDir, "workspaces", taskID)
			if _, statErr := os.Stat(restoreResult.WorkspaceDir); statErr == nil {
				if copyErr := copyDirForResume(restoreResult.WorkspaceDir, workspaceDir); copyErr != nil {
					slog.Warn("failed to restore workspace", "task_id", taskID, "error", copyErr)
				}
			}

			slog.Info("checkpoint restored",
				"task_id", taskID,
				"checkpoint_id", restoreResult.Checkpoint.ID)

			tm.recordEvent(ctx, taskID, "checkpoint.restored", map[string]interface{}{
				"checkpoint_id": restoreResult.Checkpoint.ID,
			})
		}
	}

	if err := tm.db.UpdateTaskStarted(ctx, taskID); err != nil {
		return fmt.Errorf("updating task to running: %w", err)
	}

	tm.recordEvent(ctx, taskID, "task.resumed", nil)

	// Launch executor asynchronously — it will skip already-completed subtasks
	go tm.runExecutor(taskID)

	return nil
}

// copyDirForResume copies a directory tree, used to restore workspace from checkpoint.
func copyDirForResume(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, relPath)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

// Replan regenerates the plan for a task.
func (tm *TaskManager) Replan(ctx context.Context, taskID string, additionalContext string) error {
	task, err := tm.getTaskOrError(ctx, taskID)
	if err != nil {
		return err
	}

	// Allow replan from planned or failed states
	if task.Status != db.TaskStatusPlanned && task.Status != db.TaskStatusFailed {
		return fmt.Errorf("cannot replan task in status %q", task.Status)
	}

	if err := tm.db.UpdateTaskStatus(ctx, taskID, db.TaskStatusPlanning); err != nil {
		return fmt.Errorf("updating task status to planning: %w", err)
	}

	tm.recordEvent(ctx, taskID, "task.started", nil)

	// Launch planner asynchronously
	go tm.runPlanner(taskID, additionalContext)

	return nil
}

// Delete removes a task and cleans up any running containers.
func (tm *TaskManager) Delete(ctx context.Context, taskID string) error {
	task, err := tm.getTaskOrError(ctx, taskID)
	if err != nil {
		return err
	}

	// Cancel any running operations
	tm.mu.Lock()
	cancel, ok := tm.cancels[taskID]
	if ok {
		cancel()
		delete(tm.cancels, taskID)
	}
	tm.mu.Unlock()

	// Stop running containers
	agents, _ := tm.db.ListAgentsByTask(ctx, task.ID)
	for _, agent := range agents {
		if agent.ContainerID != nil && agent.Status == "running" {
			_ = tm.docker.StopContainer(ctx, *agent.ContainerID, 5)
			_ = tm.docker.RemoveContainer(ctx, *agent.ContainerID)
		}
	}

	if err := tm.db.DeleteTask(ctx, taskID); err != nil {
		return fmt.Errorf("deleting task: %w", err)
	}

	return nil
}

// Get retrieves a task by ID.
func (tm *TaskManager) Get(ctx context.Context, taskID string) (*db.Task, error) {
	return tm.getTaskOrError(ctx, taskID)
}

// List returns tasks with pagination.
func (tm *TaskManager) List(ctx context.Context, limit, offset int) ([]db.Task, int, error) {
	tasks, err := tm.db.ListTasks(ctx, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("listing tasks: %w", err)
	}
	total, err := tm.db.CountTasks(ctx, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("counting tasks: %w", err)
	}
	return tasks, total, nil
}

// SendMessage injects a message into a running agent's container stdin.
func (tm *TaskManager) SendMessage(ctx context.Context, taskID string, agentID string, content string) error {
	// Try the pool first (for orchestrated agents)
	if tm.Pool != nil {
		if poolAgent := tm.Pool.Get(agentID); poolAgent != nil {
			if poolAgent.TaskID != taskID {
				return fmt.Errorf("agent %s does not belong to task %s", agentID, taskID)
			}
			if err := tm.Pool.SendMessage(agentID, []byte(content+"\n")); err != nil {
				return fmt.Errorf("sending message via pool: %w", err)
			}
			tm.recordEvent(ctx, taskID, "message.sent", map[string]interface{}{
				"agent_id": agentID,
				"content":  content,
			})
			return nil
		}
	}

	// Fall back to direct container attach
	dbAgent, err := tm.db.GetAgent(ctx, agentID)
	if err != nil {
		return fmt.Errorf("getting agent: %w", err)
	}
	if dbAgent == nil {
		return fmt.Errorf("agent %s not found", agentID)
	}
	if dbAgent.TaskID != taskID {
		return fmt.Errorf("agent %s does not belong to task %s", agentID, taskID)
	}
	if dbAgent.ContainerID == nil || dbAgent.Status != "running" {
		return fmt.Errorf("agent %s is not running", agentID)
	}

	// Attach to the container's stdin
	_, stdin, err := tm.docker.AttachContainer(ctx, *dbAgent.ContainerID)
	if err != nil {
		return fmt.Errorf("attaching to container: %w", err)
	}
	defer stdin.Close()

	// Write the message followed by a newline
	_, err = stdin.Write([]byte(content + "\n"))
	if err != nil {
		return fmt.Errorf("writing to container stdin: %w", err)
	}

	tm.recordEvent(ctx, taskID, "message.sent", map[string]interface{}{
		"agent_id": agentID,
		"content":  content,
	})

	return nil
}

// AnswerQuestion records a user's answer to a planner question and, if all
// pending questions are answered, re-runs the planner.
func (tm *TaskManager) AnswerQuestion(ctx context.Context, taskID, questionID, answer string) error {
	if err := tm.db.AnswerPlannerQuestion(ctx, questionID, answer); err != nil {
		return fmt.Errorf("answering question: %w", err)
	}

	// Check if all questions are now answered
	questions, err := tm.db.ListPlannerQuestions(ctx, taskID)
	if err != nil {
		return fmt.Errorf("listing questions: %w", err)
	}

	allAnswered := true
	for _, q := range questions {
		if q.Status == "pending" {
			allAnswered = false
			break
		}
	}

	if allAnswered {
		// Re-run planner with answers
		slog.Info("all questions answered, re-running planner", "task_id", taskID)
		go tm.runPlanner(taskID, "")
	}

	return nil
}

// buildRelaunchContext builds a concise context summary from the source task's
// plan and workspace to carry over to the new task.
func (tm *TaskManager) buildRelaunchContext(ctx context.Context, src *db.Task) string {
	var b strings.Builder

	b.WriteString("## Previous Task Context\n")
	b.WriteString(fmt.Sprintf("This task continues from a previous run (status: %s).\n", src.Status))
	b.WriteString(fmt.Sprintf("Original prompt: %s\n\n", src.Prompt))

	// Plan summary
	plan, err := tm.db.GetPlanByTask(ctx, src.ID)
	if err == nil && plan != nil {
		if plan.Analysis != nil && *plan.Analysis != "" {
			b.WriteString("### Analysis\n")
			b.WriteString(*plan.Analysis + "\n\n")
		}

		var subtasks []db.Subtask
		if json.Unmarshal([]byte(plan.Subtasks), &subtasks) == nil && len(subtasks) > 0 {
			b.WriteString("### Subtasks executed\n")
			for _, st := range subtasks {
				b.WriteString(fmt.Sprintf("- [%s] %s: %s\n", st.Status, st.Name, st.Description))
			}
			b.WriteString("\n")
		}
	}

	// Changed files in workspace
	workspaceDir := filepath.Join(tm.cfg.Storage.DataDir, "workspaces", src.ID)
	repoMgr := repo.NewManager(tm.cfg.Storage.DataDir)
	if files, err := repoMgr.GetChangedFiles(workspaceDir); err == nil && len(files) > 0 {
		b.WriteString("### Files modified in workspace\n")
		for _, f := range files {
			b.WriteString("- " + f + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("The workspace already contains the results of the previous run. Build on top of what was done.\n")
	return b.String()
}

// Relaunch creates a new task from a completed/failed one, inheriting its
// repo config, team template and workspace contents. An optional new prompt
// can be provided; if empty the original prompt is reused.
func (tm *TaskManager) Relaunch(ctx context.Context, sourceTaskID string, newPrompt string, autoStart bool, keepContext bool) (*db.Task, error) {
	src, err := tm.getTaskOrError(ctx, sourceTaskID)
	if err != nil {
		return nil, err
	}

	if src.Status != db.TaskStatusCompleted && src.Status != db.TaskStatusFailed && src.Status != db.TaskStatusCancelled {
		return nil, fmt.Errorf("can only relaunch completed, failed, or cancelled tasks (current: %s)", src.Status)
	}

	prompt := newPrompt
	if prompt == "" {
		prompt = src.Prompt
	}

	if keepContext {
		contextBlock := tm.buildRelaunchContext(ctx, src)
		prompt = contextBlock + "\n## Current Task\n" + prompt
	}

	newTask, err := tm.Create(ctx, src.Name+" (relaunch)", prompt, src.RepoConfig, src.TeamTemplate, src.OutputFiles)
	if err != nil {
		return nil, fmt.Errorf("creating relaunched task: %w", err)
	}

	tm.recordEvent(ctx, newTask.ID, "task.relaunched", map[string]interface{}{
		"source_task_id": sourceTaskID,
	})

	// Copy workspace from source to new task
	srcWorkspace := filepath.Join(tm.cfg.Storage.DataDir, "workspaces", sourceTaskID)
	if _, statErr := os.Stat(srcWorkspace); statErr == nil {
		dstWorkspace := filepath.Join(tm.cfg.Storage.DataDir, "workspaces", newTask.ID)
		if copyErr := copyDirForResume(srcWorkspace, dstWorkspace); copyErr != nil {
			slog.Warn("failed to copy workspace for relaunch", "source", sourceTaskID, "dest", newTask.ID, "error", copyErr)
		} else {
			slog.Info("workspace copied for relaunch", "source", sourceTaskID, "dest", newTask.ID)
		}
	}

	// Copy input files
	srcInputDir := filepath.Join(tm.cfg.Storage.FilesDir, sourceTaskID, "input")
	if _, statErr := os.Stat(srcInputDir); statErr == nil {
		dstInputDir := filepath.Join(tm.cfg.Storage.FilesDir, newTask.ID, "input")
		if copyErr := copyDirForResume(srcInputDir, dstInputDir); copyErr != nil {
			slog.Warn("failed to copy input files for relaunch", "error", copyErr)
		}
	}

	if autoStart {
		if startErr := tm.Start(ctx, newTask.ID); startErr != nil {
			slog.Error("failed to auto-start relaunched task", "task_id", newTask.ID, "error", startErr)
		}
	}

	// Re-fetch to get updated status
	updated, _ := tm.db.GetTask(ctx, newTask.ID)
	if updated != nil {
		return updated, nil
	}
	return newTask, nil
}

// prepareWorkspace creates the workspace directory and, if the task has a repo
// config, clones the repository into it. Returns the workspace path.
// It is safe to call multiple times — if the workspace already contains a .git
// directory, the clone step is skipped.
func (tm *TaskManager) prepareWorkspace(ctx context.Context, task *db.Task) (string, error) {
	workspaceDir := filepath.Join(tm.cfg.Storage.DataDir, "workspaces", task.ID)
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return "", fmt.Errorf("creating workspace directory: %w", err)
	}

	// Skip clone if no repo config
	if task.RepoConfig == nil || *task.RepoConfig == "" {
		return workspaceDir, nil
	}

	// Skip if already cloned (.git directory exists)
	gitDir := filepath.Join(workspaceDir, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		return workspaceDir, nil
	}

	var rc db.RepoConfig
	if err := json.Unmarshal([]byte(*task.RepoConfig), &rc); err != nil {
		return "", fmt.Errorf("parsing repo config: %w", err)
	}

	logger := slog.With("task_id", task.ID, "component", "workspace")
	logger.Info("cloning repository into workspace", "url", rc.URL, "branch", rc.Branch)

	mgr := repo.NewManager(tm.cfg.Storage.DataDir)
	if err := mgr.Clone(ctx, rc, workspaceDir); err != nil {
		return "", fmt.Errorf("cloning repository: %w", err)
	}

	// Create a work branch if auto_branch is enabled
	branchName := rc.Branch
	if rc.AutoBranch {
		branchName = fmt.Sprintf("klaudio/%s", task.ID[:8])
		if err := mgr.CreateWorkBranch(workspaceDir, branchName); err != nil {
			logger.Warn("failed to create work branch, continuing on default branch", "error", err)
			branchName = rc.Branch
		} else {
			logger.Info("created work branch", "branch", branchName)
		}
	}

	tm.recordEvent(ctx, task.ID, "repo.cloned", map[string]interface{}{
		"url":    rc.URL,
		"branch": branchName,
	})

	// Generate repo memory if enabled
	if rc.EnableMemory && rc.RepoTemplateID != "" {
		tm.ensureRepoMemory(ctx, task.ID, rc.RepoTemplateID, branchName, workspaceDir)
	}

	return workspaceDir, nil
}

// ensureRepoMemory checks if an up-to-date repo memory exists for the current
// commit and generates one if not. This is a non-blocking best-effort operation.
func (tm *TaskManager) ensureRepoMemory(ctx context.Context, taskID, templateID, branch, workspaceDir string) {
	logger := slog.With("task_id", taskID, "template_id", templateID, "component", "repo_memory")

	commitHash, err := repo.GetLastCommitHash(workspaceDir)
	if err != nil {
		logger.Warn("failed to get commit hash for repo memory", "error", err)
		return
	}

	// Check if memory already exists for this commit
	existing, err := tm.db.GetRepoMemoryByCommit(ctx, templateID, branch, commitHash)
	if err != nil {
		logger.Warn("failed to check existing repo memory", "error", err)
		return
	}
	if existing != nil {
		logger.Info("repo memory is up-to-date", "commit", commitHash[:8])
		return
	}

	// Generate new analysis
	logger.Info("generating repo memory", "commit", commitHash[:8])
	analysis, err := repo.Analyze(workspaceDir)
	if err != nil {
		logger.Warn("failed to analyze repository", "error", err)
		return
	}

	// Marshal JSON fields
	fileTreeJSON, _ := json.Marshal(analysis.FileTree)
	langsJSON, _ := json.Marshal(analysis.Languages)
	fwJSON, _ := json.Marshal(analysis.Frameworks)
	keyFilesJSON, _ := json.Marshal(analysis.KeyFiles)
	depsJSON, _ := json.Marshal(analysis.Dependencies)

	ftStr := string(fileTreeJSON)
	lStr := string(langsJSON)
	fwStr := string(fwJSON)
	kfStr := string(keyFilesJSON)
	dStr := string(depsJSON)

	memory := &db.RepoMemory{
		ID:             uuid.New().String(),
		RepoTemplateID: templateID,
		Branch:         branch,
		CommitHash:     commitHash,
		Content:        analysis.Content,
		FileTree:       &ftStr,
		Languages:      &lStr,
		Frameworks:     &fwStr,
		KeyFiles:       &kfStr,
		Dependencies:   &dStr,
		CreatedAt:      time.Now().UTC(),
	}

	if err := tm.db.CreateRepoMemory(ctx, memory); err != nil {
		logger.Warn("failed to save repo memory", "error", err)
		return
	}

	logger.Info("repo memory generated and saved", "commit", commitHash[:8])
	tm.recordEvent(ctx, taskID, "repo.memory_generated", map[string]interface{}{
		"template_id": templateID,
		"branch":      branch,
		"commit":      commitHash[:8],
	})
}

// getRepoMemoryContent retrieves the repo memory content for a task's repo config, if available.
func (tm *TaskManager) getRepoMemoryContent(ctx context.Context, task *db.Task) string {
	if task.RepoConfig == nil || *task.RepoConfig == "" {
		return ""
	}

	var rc db.RepoConfig
	if err := json.Unmarshal([]byte(*task.RepoConfig), &rc); err != nil {
		return ""
	}

	if !rc.EnableMemory || rc.RepoTemplateID == "" {
		return ""
	}

	memory, err := tm.db.GetRepoMemory(ctx, rc.RepoTemplateID, rc.Branch)
	if err != nil || memory == nil {
		return ""
	}

	return memory.Content
}

// copyInputFilesToWorkspace copies uploaded input files into the workspace
// so they are available inside the Docker container.
func (tm *TaskManager) copyInputFilesToWorkspace(taskID, workspaceDir string) {
	inputDir := filepath.Join(tm.cfg.Storage.FilesDir, taskID, "input")
	if _, err := os.Stat(inputDir); os.IsNotExist(err) {
		return // No input files
	}

	dstDir := filepath.Join(workspaceDir, "input")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		slog.Error("failed to create workspace input dir", "task_id", taskID, "error", err)
		return
	}

	entries, err := os.ReadDir(inputDir)
	if err != nil {
		slog.Error("failed to read input dir", "task_id", taskID, "error", err)
		return
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		src := filepath.Join(inputDir, e.Name())
		dst := filepath.Join(dstDir, e.Name())

		data, rErr := os.ReadFile(src)
		if rErr != nil {
			slog.Error("failed to read input file", "file", src, "error", rErr)
			continue
		}
		if wErr := os.WriteFile(dst, data, 0o644); wErr != nil {
			slog.Error("failed to write input file to workspace", "file", dst, "error", wErr)
		}
	}

	slog.Info("copied input files to workspace", "task_id", taskID, "count", len(entries))
}

// postExecution runs the post-execution repo flow (commit, push, PR) if configured.
func (tm *TaskManager) postExecution(ctx context.Context, task *db.Task) {
	if task.RepoConfig == nil || *task.RepoConfig == "" {
		return
	}

	var rc db.RepoConfig
	if err := json.Unmarshal([]byte(*task.RepoConfig), &rc); err != nil {
		slog.Error("failed to parse repo config for post-execution", "task_id", task.ID, "error", err)
		return
	}

	if !rc.AutoCommit && !rc.AutoPush && !rc.AutoPR {
		return
	}

	workspaceDir := filepath.Join(tm.cfg.Storage.DataDir, "workspaces", task.ID)
	logger := slog.With("task_id", task.ID, "component", "post_execution")

	result, err := repo.PostExecution(ctx, task.Name, workspaceDir, rc)
	if err != nil {
		logger.Error("post-execution failed", "error", err)
		tm.recordEvent(ctx, task.ID, "repo.postexec_error", map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	logger.Info("post-execution completed",
		"has_changes", result.HasChanges,
		"commit", result.CommitHash,
		"pushed", result.Pushed,
		"pr_url", result.PRUrl,
	)

	tm.recordEvent(ctx, task.ID, "repo.postexec", map[string]interface{}{
		"has_changes": result.HasChanges,
		"commit_hash": result.CommitHash,
		"pushed":      result.Pushed,
		"pr_url":      result.PRUrl,
	})
}

// runPlanner runs the planner in a background goroutine.
func (tm *TaskManager) runPlanner(taskID string, additionalContext string) {
	ctx := context.Background()
	logger := slog.With("task_id", taskID, "component", "task_manager")

	task, err := tm.db.GetTask(ctx, taskID)
	if err != nil || task == nil {
		logger.Error("failed to get task for planning", "error", err)
		return
	}

	// Clone repo into workspace if configured
	workspaceDir, wsErr := tm.prepareWorkspace(ctx, task)
	if wsErr != nil {
		logger.Error("failed to prepare workspace", "error", wsErr)
		tm.failTask(ctx, taskID, "workspace preparation failed: "+wsErr.Error())
		return
	}

	// Copy uploaded input files into workspace
	tm.copyInputFilesToWorkspace(taskID, workspaceDir)

	result, err := tm.planner.Run(ctx, task, additionalContext)
	if err != nil {
		logger.Error("planner run failed", "error", err)
		tm.failTask(ctx, taskID, "planner error: "+err.Error())
		return
	}

	if result.Error != nil {
		logger.Error("planner produced error", "error", result.Error)
		tm.failTask(ctx, taskID, "planner error: "+result.Error.Error())
		return
	}

	logger.Info("planner result", "has_plan", result.Plan != nil, "has_questions", result.Questions != nil)

	if result.Questions != nil {
		// Questions were asked — leave task in "planning" state and wait for answers
		logger.Info("planner asked questions", "count", len(result.Questions.Questions))
		tm.recordEvent(ctx, taskID, "planner.question", map[string]interface{}{
			"count": len(result.Questions.Questions),
		})
		return
	}

	if result.Plan != nil {
		// Persist the plan
		plan, err := tm.planner.PersistPlan(ctx, taskID, result.Plan)
		if err != nil {
			logger.Error("failed to persist plan", "error", err)
			tm.failTask(ctx, taskID, "failed to persist plan: "+err.Error())
			return
		}

		logger.Info("plan generated", "plan_id", plan.ID, "subtasks", len(result.Plan.Subtasks))

		// Update plan status to "planned"
		_ = tm.db.UpdatePlanStatus(ctx, plan.ID, "planned")

		// Transition to planned
		if err := tm.db.UpdateTaskStatus(ctx, taskID, db.TaskStatusPlanned); err != nil {
			logger.Error("failed to update task status to planned", "error", err)
			return
		}

		tm.recordEvent(ctx, taskID, "plan.generated", map[string]interface{}{
			"plan_id":       plan.ID,
			"subtask_count": len(result.Plan.Subtasks),
		})
	}
}

// runExecutor runs the executor in a background goroutine.
func (tm *TaskManager) runExecutor(taskID string) {
	ctx, cancel := context.WithCancel(context.Background())
	logger := slog.With("task_id", taskID, "component", "task_manager")

	// Register the cancel function so Stop() can use it
	tm.mu.Lock()
	tm.cancels[taskID] = cancel
	tm.mu.Unlock()

	defer func() {
		cancel()
		tm.mu.Lock()
		delete(tm.cancels, taskID)
		tm.mu.Unlock()
	}()

	task, err := tm.db.GetTask(ctx, taskID)
	if err != nil || task == nil {
		logger.Error("failed to get task for execution", "error", err)
		return
	}

	// Ensure workspace + repo are ready (idempotent — skips if already cloned)
	workspaceDir, wsErr := tm.prepareWorkspace(ctx, task)
	if wsErr != nil {
		logger.Error("failed to prepare workspace for execution", "error", wsErr)
		tm.failTask(ctx, taskID, "workspace preparation failed: "+wsErr.Error())
		return
	}

	// Copy uploaded input files into workspace
	tm.copyInputFilesToWorkspace(taskID, workspaceDir)

	plan, err := tm.db.GetPlanByTask(ctx, taskID)
	if err != nil || plan == nil {
		logger.Error("no plan found for execution", "error", err)
		tm.failTask(ctx, taskID, "no plan found for execution")
		return
	}

	// Check plan strategy to decide between sequential executor and parallel orchestrator
	if plan.Strategy == "parallel" && tm.Pool != nil {
		// Parse subtasks for the execution plan
		var subtasks []db.Subtask
		if err := json.Unmarshal([]byte(plan.Subtasks), &subtasks); err != nil {
			logger.Error("failed to unmarshal subtasks for orchestration", "error", err)
			tm.failTask(ctx, taskID, "failed to parse subtasks: "+err.Error())
			return
		}

		// Resolve team mode from template
		teamMode := tm.orchestrator.resolveTeamMode(ctx, task)

		execPlan := &ExecutionPlan{
			PlanID:     plan.ID,
			Strategy:   plan.Strategy,
			Subtasks:   subtasks,
			TaskPrompt: task.Prompt,
			Mode:       teamMode,
			RepoMemory: tm.getRepoMemoryContent(ctx, task),
		}

		orchErr := tm.orchestrator.Run(ctx, task, execPlan)

		if ctx.Err() != nil {
			logger.Info("orchestration was cancelled")
			return
		}

		if orchErr != nil {
			logger.Error("orchestration failed", "error", orchErr)
			tm.failTask(ctx, taskID, orchErr.Error())
			return
		}

		// Run post-execution (commit/push/PR) before marking complete
		tm.postExecution(ctx, task)

		if err := tm.db.UpdateTaskCompleted(ctx, taskID); err != nil {
			logger.Error("failed to mark task completed", "error", err)
			return
		}

		tm.recordEvent(ctx, taskID, "task.completed", nil)
		logger.Info("task completed successfully via orchestrator")
		return
	}

	// Sequential execution — inject repo memory if available
	tm.executor.repoMemory = tm.getRepoMemoryContent(ctx, task)
	result := tm.executor.Execute(ctx, task, plan)

	if ctx.Err() != nil {
		// Task was stopped/paused — don't update final status (Stop() already did)
		logger.Info("execution was cancelled")
		return
	}

	if result.Error != nil {
		logger.Error("execution failed", "error", result.Error,
			"completed", result.CompletedSubtasks,
			"failed", result.FailedSubtasks,
		)
		tm.failTask(ctx, taskID, result.Error.Error())
		return
	}

	// Run post-execution (commit/push/PR) before marking complete
	tm.postExecution(ctx, task)

	// All subtasks completed
	if err := tm.db.UpdateTaskCompleted(ctx, taskID); err != nil {
		logger.Error("failed to mark task completed", "error", err)
		return
	}

	tm.recordEvent(ctx, taskID, "task.completed", map[string]interface{}{
		"completed_subtasks": result.CompletedSubtasks,
	})

	logger.Info("task completed successfully", "completed", len(result.CompletedSubtasks))
}

// failTask marks a task as failed.
func (tm *TaskManager) failTask(ctx context.Context, taskID string, errMsg string) {
	if err := tm.db.UpdateTaskFailed(ctx, taskID, errMsg); err != nil {
		slog.Error("failed to mark task as failed", "task_id", taskID, "error", err)
	}
	tm.recordEvent(ctx, taskID, "task.failed", map[string]interface{}{
		"error": errMsg,
	})
}

// recordEvent creates an event in the database.
func (tm *TaskManager) recordEvent(ctx context.Context, taskID, eventType string, data interface{}) {
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
	if err := tm.db.CreateEvent(ctx, event); err != nil {
		slog.Warn("failed to record event", "type", eventType, "task_id", taskID, "error", err)
	}
}

// getTaskOrError retrieves a task or returns an error.
func (tm *TaskManager) getTaskOrError(ctx context.Context, taskID string) (*db.Task, error) {
	task, err := tm.db.GetTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("getting task %s: %w", taskID, err)
	}
	if task == nil {
		return nil, fmt.Errorf("task %s not found", taskID)
	}
	return task, nil
}
