package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/klaudio-ai/klaudio/internal/db"
)

// RepoTemplateRequest is the JSON body for creating or updating a repo template.
type RepoTemplateRequest struct {
	Name          string   `json:"name"`
	URL           string   `json:"url"`
	DefaultBranch string   `json:"default_branch"`
	AccessToken   *string  `json:"access_token,omitempty"`
	AutoBranch    bool     `json:"auto_branch"`
	AutoCommit    bool     `json:"auto_commit"`
	AutoPush      bool     `json:"auto_push"`
	AutoPR        bool     `json:"auto_pr"`
	PRTarget      string   `json:"pr_target"`
	PRReviewers   []string `json:"pr_reviewers,omitempty"`
}

// RepoTemplateResponse is the JSON response for a repo template.
type RepoTemplateResponse struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	URL           string    `json:"url"`
	DefaultBranch string    `json:"default_branch"`
	AutoBranch    bool      `json:"auto_branch"`
	AutoCommit    bool      `json:"auto_commit"`
	AutoPush      bool      `json:"auto_push"`
	AutoPR        bool      `json:"auto_pr"`
	PRTarget      string    `json:"pr_target"`
	PRReviewers   []string  `json:"pr_reviewers,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ListRepoTemplates handles GET /api/repo-templates.
func (h *Handlers) ListRepoTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := h.svc.DB.ListRepoTemplates(r.Context())
	if err != nil {
		slog.Error("failed to list repo templates", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list repo templates")
		return
	}

	resp := make([]RepoTemplateResponse, 0, len(templates))
	for i := range templates {
		resp = append(resp, repoTemplateToResponse(&templates[i]))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"templates": resp,
	})
}

// CreateRepoTemplate handles POST /api/repo-templates.
func (h *Handlers) CreateRepoTemplate(w http.ResponseWriter, r *http.Request) {
	var req RepoTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}

	if req.DefaultBranch == "" {
		req.DefaultBranch = "main"
	}
	if req.PRTarget == "" {
		req.PRTarget = "main"
	}

	now := time.Now().UTC()
	rt := &db.RepoTemplate{
		ID:            uuid.New().String(),
		Name:          req.Name,
		URL:           req.URL,
		DefaultBranch: req.DefaultBranch,
		AccessToken:   req.AccessToken,
		AutoBranch:    req.AutoBranch,
		AutoCommit:    req.AutoCommit,
		AutoPush:      req.AutoPush,
		AutoPR:        req.AutoPR,
		PRTarget:      req.PRTarget,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if len(req.PRReviewers) > 0 {
		reviewersJSON, err := json.Marshal(req.PRReviewers)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid pr_reviewers")
			return
		}
		s := string(reviewersJSON)
		rt.PRReviewers = &s
	}

	if err := h.svc.DB.CreateRepoTemplate(r.Context(), rt); err != nil {
		slog.Error("failed to create repo template", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create repo template")
		return
	}

	writeJSON(w, http.StatusCreated, repoTemplateToResponse(rt))
}

// GetRepoTemplate handles GET /api/repo-templates/{templateID}.
func (h *Handlers) GetRepoTemplate(w http.ResponseWriter, r *http.Request) {
	templateID := chi.URLParam(r, "templateID")
	if templateID == "" {
		writeError(w, http.StatusBadRequest, "templateID is required")
		return
	}

	rt, err := h.svc.DB.GetRepoTemplate(r.Context(), templateID)
	if err != nil {
		slog.Error("failed to get repo template", "template_id", templateID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to retrieve repo template")
		return
	}
	if rt == nil {
		writeError(w, http.StatusNotFound, "repo template not found")
		return
	}

	writeJSON(w, http.StatusOK, repoTemplateToResponse(rt))
}

// UpdateRepoTemplate handles PUT /api/repo-templates/{templateID}.
func (h *Handlers) UpdateRepoTemplate(w http.ResponseWriter, r *http.Request) {
	templateID := chi.URLParam(r, "templateID")
	if templateID == "" {
		writeError(w, http.StatusBadRequest, "templateID is required")
		return
	}

	existing, err := h.svc.DB.GetRepoTemplate(r.Context(), templateID)
	if err != nil {
		slog.Error("failed to get repo template for update", "template_id", templateID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to retrieve repo template")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "repo template not found")
		return
	}

	var req RepoTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}

	if req.DefaultBranch == "" {
		req.DefaultBranch = "main"
	}
	if req.PRTarget == "" {
		req.PRTarget = "main"
	}

	existing.Name = req.Name
	existing.URL = req.URL
	existing.DefaultBranch = req.DefaultBranch
	existing.AutoBranch = req.AutoBranch
	existing.AutoCommit = req.AutoCommit
	existing.AutoPush = req.AutoPush
	existing.AutoPR = req.AutoPR
	existing.PRTarget = req.PRTarget

	// Only update the access token if provided; otherwise keep the existing one.
	if req.AccessToken != nil {
		existing.AccessToken = req.AccessToken
	}

	if len(req.PRReviewers) > 0 {
		reviewersJSON, err := json.Marshal(req.PRReviewers)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid pr_reviewers")
			return
		}
		s := string(reviewersJSON)
		existing.PRReviewers = &s
	} else {
		existing.PRReviewers = nil
	}

	if err := h.svc.DB.UpdateRepoTemplate(r.Context(), existing); err != nil {
		slog.Error("failed to update repo template", "template_id", templateID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update repo template")
		return
	}

	// Re-fetch to get updated_at set by the DB layer.
	updated, err := h.svc.DB.GetRepoTemplate(r.Context(), templateID)
	if err != nil || updated == nil {
		// Fall back to returning the existing object.
		writeJSON(w, http.StatusOK, repoTemplateToResponse(existing))
		return
	}

	writeJSON(w, http.StatusOK, repoTemplateToResponse(updated))
}

// DeleteRepoTemplate handles DELETE /api/repo-templates/{templateID}.
func (h *Handlers) DeleteRepoTemplate(w http.ResponseWriter, r *http.Request) {
	templateID := chi.URLParam(r, "templateID")
	if templateID == "" {
		writeError(w, http.StatusBadRequest, "templateID is required")
		return
	}

	if err := h.svc.DB.DeleteRepoTemplate(r.Context(), templateID); err != nil {
		slog.Error("failed to delete repo template", "template_id", templateID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete repo template")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// repoTemplateToResponse converts a db.RepoTemplate to a RepoTemplateResponse.
// The access token is intentionally omitted from responses for security.
func repoTemplateToResponse(rt *db.RepoTemplate) RepoTemplateResponse {
	resp := RepoTemplateResponse{
		ID:            rt.ID,
		Name:          rt.Name,
		URL:           rt.URL,
		DefaultBranch: rt.DefaultBranch,
		AutoBranch:    rt.AutoBranch,
		AutoCommit:    rt.AutoCommit,
		AutoPush:      rt.AutoPush,
		AutoPR:        rt.AutoPR,
		PRTarget:      rt.PRTarget,
		CreatedAt:     rt.CreatedAt,
		UpdatedAt:     rt.UpdatedAt,
	}

	if rt.PRReviewers != nil {
		var reviewers []string
		if err := json.Unmarshal([]byte(*rt.PRReviewers), &reviewers); err == nil {
			resp.PRReviewers = reviewers
		}
	}

	return resp
}
