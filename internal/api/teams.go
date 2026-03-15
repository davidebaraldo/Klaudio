package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/klaudio-ai/klaudio/internal/db"
	"github.com/klaudio-ai/klaudio/internal/stream"
)

// TeamTemplateRequest is the JSON body for creating or updating a team template.
type TeamTemplateRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	MaxAgents   int             `json:"max_agents"`
	Review      bool            `json:"review"`
	Roles       json.RawMessage `json:"roles"` // Array of role objects
	Mode        string          `json:"mode"`  // "sequential" or "collaborative"
	IsDefault   bool            `json:"is_default"`
}

// TeamTemplateResponse is the JSON response for a team template.
type TeamTemplateResponse struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	MaxAgents   int             `json:"max_agents"`
	Review      bool            `json:"review"`
	Roles       json.RawMessage `json:"roles"`
	Mode        string          `json:"mode"`
	IsDefault   bool            `json:"is_default"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// ListTeamTemplates handles GET /api/team-templates.
func (h *Handlers) ListTeamTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := h.svc.DB.ListTeamTemplates(r.Context())
	if err != nil {
		slog.Error("failed to list team templates", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list team templates")
		return
	}

	resp := make([]TeamTemplateResponse, 0, len(templates))
	for i := range templates {
		resp = append(resp, teamTemplateToResponse(&templates[i]))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"templates": resp,
	})
}

// CreateTeamTemplate handles POST /api/team-templates.
func (h *Handlers) CreateTeamTemplate(w http.ResponseWriter, r *http.Request) {
	var req TeamTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.MaxAgents <= 0 {
		req.MaxAgents = 3
	}

	// Validate roles JSON
	rolesStr := "[]"
	if len(req.Roles) > 0 {
		var roles []db.TeamRole
		if err := json.Unmarshal(req.Roles, &roles); err != nil {
			writeError(w, http.StatusBadRequest, "invalid roles: "+err.Error())
			return
		}
		rolesStr = string(req.Roles)
	}

	mode := req.Mode
	if mode != "collaborative" {
		mode = "sequential"
	}

	now := time.Now().UTC()
	tt := &db.TeamTemplate{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
		MaxAgents:   req.MaxAgents,
		Review:      req.Review,
		Roles:       rolesStr,
		Mode:        mode,
		IsDefault:   req.IsDefault,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := h.svc.DB.CreateTeamTemplate(r.Context(), tt); err != nil {
		slog.Error("failed to create team template", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create team template")
		return
	}

	writeJSON(w, http.StatusCreated, teamTemplateToResponse(tt))
}

// GetTeamTemplate handles GET /api/team-templates/{templateID}.
func (h *Handlers) GetTeamTemplate(w http.ResponseWriter, r *http.Request) {
	templateID := chi.URLParam(r, "templateID")
	if templateID == "" {
		writeError(w, http.StatusBadRequest, "templateID is required")
		return
	}

	tt, err := h.svc.DB.GetTeamTemplate(r.Context(), templateID)
	if err != nil {
		slog.Error("failed to get team template", "template_id", templateID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to retrieve team template")
		return
	}
	if tt == nil {
		writeError(w, http.StatusNotFound, "team template not found")
		return
	}

	writeJSON(w, http.StatusOK, teamTemplateToResponse(tt))
}

// UpdateTeamTemplate handles PUT /api/team-templates/{templateID}.
func (h *Handlers) UpdateTeamTemplate(w http.ResponseWriter, r *http.Request) {
	templateID := chi.URLParam(r, "templateID")
	if templateID == "" {
		writeError(w, http.StatusBadRequest, "templateID is required")
		return
	}

	existing, err := h.svc.DB.GetTeamTemplate(r.Context(), templateID)
	if err != nil {
		slog.Error("failed to get team template for update", "template_id", templateID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to retrieve team template")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "team template not found")
		return
	}

	var req TeamTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.MaxAgents <= 0 {
		req.MaxAgents = 3
	}

	rolesStr := "[]"
	if len(req.Roles) > 0 {
		var roles []db.TeamRole
		if err := json.Unmarshal(req.Roles, &roles); err != nil {
			writeError(w, http.StatusBadRequest, "invalid roles: "+err.Error())
			return
		}
		rolesStr = string(req.Roles)
	}

	mode := req.Mode
	if mode != "collaborative" {
		mode = "sequential"
	}

	existing.Name = req.Name
	existing.Description = req.Description
	existing.MaxAgents = req.MaxAgents
	existing.Review = req.Review
	existing.Roles = rolesStr
	existing.Mode = mode
	existing.IsDefault = req.IsDefault

	if err := h.svc.DB.UpdateTeamTemplate(r.Context(), existing); err != nil {
		slog.Error("failed to update team template", "template_id", templateID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update team template")
		return
	}

	updated, err := h.svc.DB.GetTeamTemplate(r.Context(), templateID)
	if err != nil || updated == nil {
		writeJSON(w, http.StatusOK, teamTemplateToResponse(existing))
		return
	}

	writeJSON(w, http.StatusOK, teamTemplateToResponse(updated))
}

// DeleteTeamTemplate handles DELETE /api/team-templates/{templateID}.
func (h *Handlers) DeleteTeamTemplate(w http.ResponseWriter, r *http.Request) {
	templateID := chi.URLParam(r, "templateID")
	if templateID == "" {
		writeError(w, http.StatusBadRequest, "templateID is required")
		return
	}

	if err := h.svc.DB.DeleteTeamTemplate(r.Context(), templateID); err != nil {
		slog.Error("failed to delete team template", "template_id", templateID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete team template")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ListAgentMessages handles GET /api/tasks/{taskID}/messages.
// Supports optional query parameters:
//   - after_id: only return messages with ID > after_id (cursor-based pagination)
//   - limit: max number of messages to return (default 200)
func (h *Handlers) ListAgentMessages(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "taskID is required")
		return
	}

	// Check for cursor-based pagination via after_id
	if afterIDStr := r.URL.Query().Get("after_id"); afterIDStr != "" {
		afterID, err := strconv.ParseInt(afterIDStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid after_id: must be an integer")
			return
		}

		messages, err := h.svc.DB.ListAgentMessagesAfterID(r.Context(), taskID, afterID)
		if err != nil {
			slog.Error("failed to list agent messages after cursor", "task_id", taskID, "after_id", afterID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list agent messages")
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"messages": messages,
		})
		return
	}

	// Default: return latest messages with limit
	limit := 200
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	messages, err := h.svc.DB.ListAgentMessages(r.Context(), taskID, limit)
	if err != nil {
		slog.Error("failed to list agent messages", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list agent messages")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"messages": messages,
	})
}

// SendAgentMessageRequest is the JSON body for agents to send messages.
type SendAgentMessageRequest struct {
	From    string `json:"from"`    // subtask ID of sender
	Content string `json:"content"` // message content
	To      string `json:"to"`      // optional: specific subtask ID recipient (empty = broadcast)
}

// SendAgentMessage handles POST /api/tasks/{taskID}/messages.
// This endpoint is called by agents running in Docker containers to communicate.
func (h *Handlers) SendAgentMessage(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "taskID is required")
		return
	}

	var req SendAgentMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if req.From == "" {
		writeError(w, http.StatusBadRequest, "from is required")
		return
	}

	msg := &db.AgentMessage{
		TaskID:        taskID,
		FromSubtaskID: &req.From,
		MsgType:       "message",
		Content:       req.Content,
		CreatedAt:     time.Now().UTC(),
	}
	if req.To != "" {
		msg.ToSubtaskID = &req.To
	}

	if err := h.svc.DB.CreateAgentMessage(r.Context(), msg); err != nil {
		slog.Error("failed to create agent message", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to send message")
		return
	}

	// Publish to real-time message bus for WebSocket subscribers and orchestrator
	if h.svc.MessageBus != nil {
		h.svc.MessageBus.Publish(taskID, stream.AgentMessageEvent{
			ID:            msg.ID,
			TaskID:        msg.TaskID,
			FromAgentID:   msg.FromAgentID,
			FromSubtaskID: msg.FromSubtaskID,
			ToAgentID:     msg.ToAgentID,
			ToSubtaskID:   msg.ToSubtaskID,
			MsgType:       msg.MsgType,
			Content:       msg.Content,
			CreatedAt:     msg.CreatedAt.Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":      msg.ID,
		"status":  "sent",
		"from":    req.From,
		"content": req.Content,
	})
}

func teamTemplateToResponse(tt *db.TeamTemplate) TeamTemplateResponse {
	mode := tt.Mode
	if mode == "" {
		mode = "sequential"
	}
	return TeamTemplateResponse{
		ID:          tt.ID,
		Name:        tt.Name,
		Description: tt.Description,
		MaxAgents:   tt.MaxAgents,
		Review:      tt.Review,
		Roles:       json.RawMessage(tt.Roles),
		Mode:        mode,
		IsDefault:   tt.IsDefault,
		CreatedAt:   tt.CreatedAt,
		UpdatedAt:   tt.UpdatedAt,
	}
}
