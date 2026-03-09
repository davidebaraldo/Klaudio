package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/klaudio-ai/klaudio/internal/db"
)

// ListTaskAgents returns all agents for a given task.
func (h *Handlers) ListTaskAgents(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "taskID is required")
		return
	}

	agents, err := h.svc.DB.ListAgentsByTask(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agents: "+err.Error())
		return
	}
	if agents == nil {
		writeJSON(w, http.StatusOK, []db.Agent{})
		return
	}

	writeJSON(w, http.StatusOK, agents)
}

// GetAgent returns details for a specific agent.
func (h *Handlers) GetAgent(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	agentID := chi.URLParam(r, "agentID")
	if taskID == "" || agentID == "" {
		writeError(w, http.StatusBadRequest, "taskID and agentID are required")
		return
	}

	agent, err := h.svc.DB.GetAgent(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get agent: "+err.Error())
		return
	}
	if agent == nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if agent.TaskID != taskID {
		writeError(w, http.StatusNotFound, "agent not found for this task")
		return
	}

	writeJSON(w, http.StatusOK, agent)
}
