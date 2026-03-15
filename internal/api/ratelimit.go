package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/klaudio-ai/klaudio/internal/ratelimit"
)

// GetRateLimitStatus returns the current rate limit state for all agents of a task.
// GET /api/tasks/{taskID}/rate-limit
func (h *Handlers) GetRateLimitStatus(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "taskID is required")
		return
	}

	tracker := h.svc.RateLimitTracker
	if tracker == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"task_id": taskID,
			"agents":  []*ratelimit.State{},
		})
		return
	}

	states := tracker.GetTaskStates(taskID)
	if states == nil {
		states = []*ratelimit.State{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"task_id": taskID,
		"agents":  states,
	})
}
