package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// StartTask handles POST /api/tasks/{taskID}/start.
// Transitions the task from "created" to "planning" and launches the planner.
func (h *Handlers) StartTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "taskID is required")
		return
	}

	if err := h.svc.TaskManager.Start(r.Context(), taskID); err != nil {
		slog.Error("failed to start task", "task_id", taskID, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "planning",
		"message": "Planner agent started",
	})
}

// ApproveTask handles POST /api/tasks/{taskID}/approve.
// Approves the plan and starts execution.
func (h *Handlers) ApproveTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "taskID is required")
		return
	}

	var req struct {
		ModifiedPlan json.RawMessage `json:"modified_plan,omitempty"`
	}
	// Body is optional
	_ = json.NewDecoder(r.Body).Decode(&req)

	var modifiedPlan *string
	if len(req.ModifiedPlan) > 0 {
		s := string(req.ModifiedPlan)
		modifiedPlan = &s
	}

	if err := h.svc.TaskManager.Approve(r.Context(), taskID, modifiedPlan); err != nil {
		slog.Error("failed to approve task", "task_id", taskID, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "running",
		"message": "Plan approved, execution started",
	})
}

// StopTask handles POST /api/tasks/{taskID}/stop.
// Pauses a running task.
func (h *Handlers) StopTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "taskID is required")
		return
	}

	if err := h.svc.TaskManager.Stop(r.Context(), taskID); err != nil {
		slog.Error("failed to stop task", "task_id", taskID, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "paused",
		"message": "Task paused",
	})
}

// ResumeTask handles POST /api/tasks/{taskID}/resume.
// Resumes a paused task.
func (h *Handlers) ResumeTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "taskID is required")
		return
	}

	if err := h.svc.TaskManager.Resume(r.Context(), taskID); err != nil {
		slog.Error("failed to resume task", "task_id", taskID, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "running",
		"message": "Task resumed",
	})
}

// ReplanTask handles POST /api/tasks/{taskID}/replan.
// Regenerates the plan for a task.
func (h *Handlers) ReplanTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "taskID is required")
		return
	}

	var req struct {
		AdditionalContext string `json:"additional_context"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if err := h.svc.TaskManager.Replan(r.Context(), taskID, req.AdditionalContext); err != nil {
		slog.Error("failed to replan task", "task_id", taskID, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "planning",
		"message": "Re-planning started",
	})
}

// RelaunchTask handles POST /api/tasks/{taskID}/relaunch.
// Creates a new task from a completed/failed one, inheriting context and workspace.
func (h *Handlers) RelaunchTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "taskID is required")
		return
	}

	var req struct {
		Prompt      string `json:"prompt"`
		AutoStart   bool   `json:"auto_start"`
		KeepContext *bool  `json:"keep_context"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	keepContext := true
	if req.KeepContext != nil {
		keepContext = *req.KeepContext
	}

	newTask, err := h.svc.TaskManager.Relaunch(r.Context(), taskID, req.Prompt, req.AutoStart, keepContext)
	if err != nil {
		slog.Error("failed to relaunch task", "task_id", taskID, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, taskToResponse(newTask))
}

// SendMessage handles POST /api/tasks/{taskID}/message.
// Injects a message into an agent's container stdin.
func (h *Handlers) SendMessage(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "taskID is required")
		return
	}

	var req struct {
		AgentID string `json:"agent_id"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.AgentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	if err := h.svc.TaskManager.SendMessage(r.Context(), taskID, req.AgentID, req.Content); err != nil {
		slog.Error("failed to send message", "task_id", taskID, "agent_id", req.AgentID, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"delivered": true,
		"agent_id":  req.AgentID,
	})
}
