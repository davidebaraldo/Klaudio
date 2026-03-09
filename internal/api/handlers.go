package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/klaudio-ai/klaudio/internal/db"
	"github.com/klaudio-ai/klaudio/internal/docker"
)

// Handlers holds all HTTP handler methods.
type Handlers struct {
	svc *Services
}

// NewHandlers creates a Handlers instance.
func NewHandlers(svc *Services) *Handlers {
	return &Handlers{svc: svc}
}

// HealthCheck returns a simple health status.
func (h *Handlers) HealthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

// CreateTaskRequest is the JSON body for creating a new task.
type CreateTaskRequest struct {
	Name           string            `json:"name"`
	Prompt         string            `json:"prompt"`
	AutoStart      bool              `json:"auto_start"`
	RepoTemplateID string            `json:"repo_template_id,omitempty"`
	RepoOverrides  *RepoOverrides    `json:"repo_overrides,omitempty"`
	OutputFiles    []string          `json:"output_files,omitempty"`
	TeamTemplate   string            `json:"team_template,omitempty"`
}

// RepoOverrides allows overriding repo template permissions per-task.
// nil values mean "use template default".
type RepoOverrides struct {
	AutoBranch *bool `json:"auto_branch,omitempty"`
	AutoCommit *bool `json:"auto_commit,omitempty"`
	AutoPush   *bool `json:"auto_push,omitempty"`
	AutoPR     *bool `json:"auto_pr,omitempty"`
}

// TaskResponse is the JSON response for a task.
type TaskResponse struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Prompt       string          `json:"prompt"`
	Status       string          `json:"status"`
	Error        *string         `json:"error,omitempty"`
	HasState     bool            `json:"has_state"`
	TeamTemplate *string         `json:"team_template,omitempty"`
	Agents       []AgentResponse `json:"agents,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	StartedAt    *time.Time      `json:"started_at,omitempty"`
	CompletedAt  *time.Time      `json:"completed_at,omitempty"`
}

// AgentResponse is the JSON response for an agent.
type AgentResponse struct {
	ID          string     `json:"id"`
	TaskID      string     `json:"task_id"`
	SubtaskID   *string    `json:"subtask_id,omitempty"`
	Role        string     `json:"role"`
	Status      string     `json:"status"`
	ContainerID *string    `json:"container_id,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// CreateTask handles POST /api/tasks.
// It creates a new task via the TaskManager. If auto_start is true, it also starts planning.
func (h *Handlers) CreateTask(w http.ResponseWriter, r *http.Request) {
	var req CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Prompt == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	// Resolve repo template → RepoConfig JSON string
	var repoConfigJSON *string
	if req.RepoTemplateID != "" {
		tmpl, err := h.svc.DB.GetRepoTemplate(r.Context(), req.RepoTemplateID)
		if err != nil {
			slog.Error("failed to get repo template", "template_id", req.RepoTemplateID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to resolve repo template")
			return
		}
		if tmpl == nil {
			writeError(w, http.StatusBadRequest, "repo template not found")
			return
		}

		rc := db.RepoConfig{
			URL:        tmpl.URL,
			Branch:     tmpl.DefaultBranch,
			AutoBranch: tmpl.AutoBranch,
			AutoCommit: tmpl.AutoCommit,
			AutoPush:   tmpl.AutoPush,
			AutoPR:     tmpl.AutoPR,
			PRTarget:   tmpl.PRTarget,
		}
		if tmpl.AccessToken != nil {
			rc.AccessToken = *tmpl.AccessToken
		}
		if tmpl.PRReviewers != nil {
			_ = json.Unmarshal([]byte(*tmpl.PRReviewers), &rc.PRReviewers)
		}

		// Apply per-task overrides
		if req.RepoOverrides != nil {
			if req.RepoOverrides.AutoBranch != nil {
				rc.AutoBranch = *req.RepoOverrides.AutoBranch
			}
			if req.RepoOverrides.AutoCommit != nil {
				rc.AutoCommit = *req.RepoOverrides.AutoCommit
			}
			if req.RepoOverrides.AutoPush != nil {
				rc.AutoPush = *req.RepoOverrides.AutoPush
			}
			if req.RepoOverrides.AutoPR != nil {
				rc.AutoPR = *req.RepoOverrides.AutoPR
			}
		}

		rcJSON, err := json.Marshal(rc)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to serialize repo config")
			return
		}
		s := string(rcJSON)
		repoConfigJSON = &s
	}

	// Resolve output files
	var outputFilesJSON *string
	if len(req.OutputFiles) > 0 {
		ofJSON, _ := json.Marshal(req.OutputFiles)
		s := string(ofJSON)
		outputFilesJSON = &s
	}

	// Resolve team template
	var teamTemplate *string
	if req.TeamTemplate != "" {
		teamTemplate = &req.TeamTemplate
	}

	// Use TaskManager if available (Phase 2+), otherwise fall back to direct creation
	if h.svc.TaskManager != nil {
		task, err := h.svc.TaskManager.Create(r.Context(), req.Name, req.Prompt, repoConfigJSON, teamTemplate, outputFilesJSON)
		if err != nil {
			slog.Error("failed to create task", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create task")
			return
		}

		// Auto-start if requested
		if req.AutoStart {
			if err := h.svc.TaskManager.Start(r.Context(), task.ID); err != nil {
				slog.Error("failed to auto-start task", "task_id", task.ID, "error", err)
				// Task was created, just not started — return it anyway
			}
		}

		writeJSON(w, http.StatusCreated, taskToResponse(task))
		return
	}

	// Legacy fallback (Phase 1 behavior)
	now := time.Now().UTC()
	task := &db.Task{
		ID:           uuid.New().String(),
		Name:         req.Name,
		Prompt:       req.Prompt,
		Status:       db.TaskStatusCreated,
		RepoConfig:   repoConfigJSON,
		TeamTemplate: teamTemplate,
		OutputFiles:  outputFilesJSON,
		HasState:     false,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := h.svc.DB.CreateTask(r.Context(), task); err != nil {
		slog.Error("failed to create task in database", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create task")
		return
	}

	h.recordEvent(r, task.ID, "task.created", nil)
	go h.runTask(task)

	writeJSON(w, http.StatusCreated, taskToResponse(task))
}

// GetTask handles GET /api/tasks/{taskID}.
func (h *Handlers) GetTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "taskID is required")
		return
	}

	task, err := h.svc.DB.GetTask(r.Context(), taskID)
	if err != nil {
		slog.Error("failed to get task", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to retrieve task")
		return
	}
	if task == nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	resp := taskToResponse(task)

	// Load agents for this task
	agents, agentsErr := h.svc.DB.ListAgentsByTask(r.Context(), taskID)
	if agentsErr == nil {
		for _, a := range agents {
			resp.Agents = append(resp.Agents, AgentResponse{
				ID:          a.ID,
				TaskID:      a.TaskID,
				SubtaskID:   a.SubtaskID,
				Role:        a.Role,
				Status:      a.Status,
				ContainerID: a.ContainerID,
				StartedAt:   a.StartedAt,
				CompletedAt: a.CompletedAt,
			})
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// ListTasks handles GET /api/tasks.
func (h *Handlers) ListTasks(w http.ResponseWriter, r *http.Request) {
	limit := 50
	offset := 0

	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	tasks, err := h.svc.DB.ListTasks(r.Context(), limit, offset)
	if err != nil {
		slog.Error("failed to list tasks", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list tasks")
		return
	}

	total, err := h.svc.DB.CountTasks(r.Context(), nil)
	if err != nil {
		slog.Error("failed to count tasks", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to count tasks")
		return
	}

	resp := make([]TaskResponse, 0, len(tasks))
	for i := range tasks {
		resp = append(resp, taskToResponse(&tasks[i]))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tasks":  resp,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// DeleteTask handles DELETE /api/tasks/{taskID}.
func (h *Handlers) DeleteTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "taskID is required")
		return
	}

	// Use TaskManager if available (Phase 2+), otherwise fall back to direct deletion
	if h.svc.TaskManager != nil {
		if err := h.svc.TaskManager.Delete(r.Context(), taskID); err != nil {
			slog.Error("failed to delete task", "task_id", taskID, "error", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
		return
	}

	// Legacy fallback (Phase 1 behavior)
	task, err := h.svc.DB.GetTask(r.Context(), taskID)
	if err != nil {
		slog.Error("failed to get task for deletion", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to retrieve task")
		return
	}
	if task == nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	agents, _ := h.svc.DB.ListAgentsByTask(r.Context(), taskID)
	for _, agent := range agents {
		if agent.ContainerID != nil && agent.Status == "running" {
			_ = h.svc.Docker.StopContainer(r.Context(), *agent.ContainerID, 5)
			_ = h.svc.Docker.RemoveContainer(r.Context(), *agent.ContainerID)
		}
	}

	if err := h.svc.DB.DeleteTask(r.Context(), taskID); err != nil {
		slog.Error("failed to delete task", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete task")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// GetConfig handles GET /api/config. Returns non-sensitive configuration info.
func (h *Handlers) GetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := h.svc.Config
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"server": map[string]interface{}{
			"port": cfg.Server.Port,
			"host": cfg.Server.Host,
		},
		"docker": map[string]interface{}{
			"image_name": cfg.Docker.ImageName,
			"network":    cfg.Docker.Network,
			"max_agents": cfg.Docker.MaxAgents,
		},
		"claude": map[string]interface{}{
			"auth_mode": cfg.Claude.AuthMode,
		},
		"database": map[string]interface{}{
			"path": cfg.Database.Path,
		},
	})
}

// runTask executes the full lifecycle of a task: create container, run, collect output.
// This runs in a goroutine and must not use the HTTP request context.
func (h *Handlers) runTask(task *db.Task) {
	ctx := context.Background()
	logger := slog.With("task_id", task.ID)

	// Create agent record
	agentID := uuid.New().String()
	agent := &db.Agent{
		ID:        agentID,
		TaskID:    task.ID,
		Role:      "developer",
		Status:    "created",
		CreatedAt: time.Now().UTC(),
	}
	if err := h.svc.DB.CreateAgent(ctx, agent); err != nil {
		logger.Error("failed to create agent record", "error", err)
		h.failTask(ctx, task.ID, "failed to create agent record: "+err.Error())
		return
	}

	// Ensure workspace directory exists
	workspaceDir := filepath.Join(h.svc.Config.Storage.DataDir, "workspaces", task.ID)
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		logger.Error("failed to create workspace directory", "error", err)
		h.failTask(ctx, task.ID, "failed to create workspace: "+err.Error())
		return
	}

	absWorkspace, err := filepath.Abs(workspaceDir)
	if err != nil {
		logger.Error("failed to resolve workspace path", "error", err)
		h.failTask(ctx, task.ID, "failed to resolve workspace path: "+err.Error())
		return
	}

	// Create the container
	containerName := fmt.Sprintf("klaudio-agent-%s", task.ID[:8])
	containerID, err := h.svc.Docker.CreateContainer(ctx, docker.ContainerOpts{
		Name:   containerName,
		Prompt: task.Prompt,
		Volumes: []docker.VolumeMount{
			{
				HostPath:      absWorkspace,
				ContainerPath: "/home/agent/workspace",
				ReadOnly:      false,
			},
		},
	})
	if err != nil {
		logger.Error("failed to create container", "error", err)
		h.failTask(ctx, task.ID, "failed to create container: "+err.Error())
		return
	}

	// Update agent with container ID
	if err := h.svc.DB.UpdateAgentContainer(ctx, agentID, containerID); err != nil {
		logger.Error("failed to update agent container", "error", err)
	}

	logger = logger.With("container_id", containerID)

	// Start the container
	if err := h.svc.Docker.StartContainer(ctx, containerID); err != nil {
		logger.Error("failed to start container", "error", err)
		h.svc.Docker.RemoveContainer(ctx, containerID)
		h.failTask(ctx, task.ID, "failed to start container: "+err.Error())
		return
	}

	// Mark task as running
	if err := h.svc.DB.UpdateTaskStarted(ctx, task.ID); err != nil {
		logger.Error("failed to mark task as running", "error", err)
	}
	h.recordEventBackground(ctx, task.ID, "task.started", nil)

	// Wait for completion
	exitCh, errCh := h.svc.Docker.WaitContainer(ctx, containerID)
	exitCode := <-exitCh
	waitErr := <-errCh

	logger.Info("container finished", "exit_code", exitCode)

	// Collect logs
	logsReader, logErr := h.svc.Docker.ContainerLogs(ctx, containerID)
	var output string
	if logErr == nil {
		outputBytes, _ := io.ReadAll(logsReader)
		logsReader.Close()
		output = stripDockerLogHeaders(outputBytes)
	}

	// Clean up container
	if removeErr := h.svc.Docker.RemoveContainer(ctx, containerID); removeErr != nil {
		logger.Warn("failed to remove container", "error", removeErr)
	}

	// Update agent completion
	var agentErr *string
	if waitErr != nil {
		errStr := waitErr.Error()
		agentErr = &errStr
	}
	h.svc.DB.UpdateAgentCompleted(ctx, agentID, int(exitCode), agentErr)

	// Update task based on exit code
	if exitCode == 0 && waitErr == nil {
		if err := h.svc.DB.UpdateTaskCompleted(ctx, task.ID); err != nil {
			logger.Error("failed to mark task completed", "error", err)
		}
		h.recordEventBackground(ctx, task.ID, "task.completed", &output)
	} else {
		errMsg := fmt.Sprintf("agent exited with code %d", exitCode)
		if waitErr != nil {
			errMsg += ": " + waitErr.Error()
		}
		if output != "" {
			errMsg += "\n\nOutput:\n" + output
		}
		h.failTask(ctx, task.ID, errMsg)
	}
}

// failTask marks a task as failed in the database.
func (h *Handlers) failTask(ctx context.Context, taskID string, errMsg string) {
	if err := h.svc.DB.UpdateTaskFailed(ctx, taskID, errMsg); err != nil {
		slog.Error("failed to mark task as failed", "task_id", taskID, "error", err)
	}
	h.recordEventBackground(ctx, taskID, "task.failed", &errMsg)
}

// recordEvent logs an event, using the request context for the DB call.
func (h *Handlers) recordEvent(r *http.Request, taskID, eventType string, data *string) {
	event := &db.Event{
		TaskID: taskID,
		Type:   eventType,
		Data:   data,
	}
	if err := h.svc.DB.CreateEvent(r.Context(), event); err != nil {
		slog.Warn("failed to record event", "type", eventType, "task_id", taskID, "error", err)
	}
}

// recordEventBackground logs an event using a background context.
func (h *Handlers) recordEventBackground(ctx context.Context, taskID, eventType string, data *string) {
	event := &db.Event{
		TaskID: taskID,
		Type:   eventType,
		Data:   data,
	}
	if err := h.svc.DB.CreateEvent(ctx, event); err != nil {
		slog.Warn("failed to record event", "type", eventType, "task_id", taskID, "error", err)
	}
}

// taskToResponse converts a db.Task to a TaskResponse.
func taskToResponse(t *db.Task) TaskResponse {
	return TaskResponse{
		ID:           t.ID,
		Name:         t.Name,
		Prompt:       t.Prompt,
		Status:       string(t.Status),
		Error:        t.Error,
		HasState:     t.HasState,
		TeamTemplate: t.TeamTemplate,
		CreatedAt:    t.CreatedAt,
		UpdatedAt:    t.UpdatedAt,
		StartedAt:    t.StartedAt,
		CompletedAt:  t.CompletedAt,
	}
}

// writeJSON serializes data as JSON and writes it to the response.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to encode JSON response", "error", err)
	}
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// stripDockerLogHeaders removes the 8-byte Docker log header from each log line.
// Docker multiplexed streams prepend an 8-byte header to each frame.
// With Tty: true, Docker returns raw output without headers — detect and skip stripping.
func stripDockerLogHeaders(data []byte) string {
	if len(data) < 8 {
		return string(data)
	}
	// Docker multiplexed log headers start with stream type: 0=stdin, 1=stdout, 2=stderr
	// and bytes 1-3 are always zero. If that pattern isn't present, it's raw TTY output.
	if data[0] > 2 || data[1] != 0 || data[2] != 0 || data[3] != 0 {
		return string(data)
	}

	var result []byte
	for len(data) >= 8 {
		if data[0] > 2 || data[1] != 0 || data[2] != 0 || data[3] != 0 {
			result = append(result, data...)
			break
		}
		// Header: [stream_type(1)][0(3)][size(4)]
		size := int(data[4])<<24 | int(data[5])<<16 | int(data[6])<<8 | int(data[7])
		data = data[8:]
		if size > len(data) {
			size = len(data)
		}
		result = append(result, data[:size]...)
		data = data[size:]
	}
	if len(result) == 0 {
		return string(data)
	}
	return string(result)
}
