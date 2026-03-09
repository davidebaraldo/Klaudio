package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/klaudio-ai/klaudio/internal/db"
)

// PlanResponse is the JSON response for a plan.
type PlanResponse struct {
	ID              string              `json:"id"`
	TaskID          string              `json:"task_id"`
	Analysis        *string             `json:"analysis,omitempty"`
	Strategy        string              `json:"strategy"`
	Subtasks        []db.Subtask        `json:"subtasks"`
	Questions       []db.PlannerQuestion `json:"questions,omitempty"`
	EstimatedAgents int                 `json:"estimated_agents"`
	Notes           *string             `json:"notes,omitempty"`
	Status          string              `json:"status"`
	CreatedAt       time.Time           `json:"created_at"`
}

// GetPlan handles GET /api/tasks/{taskID}/plan.
func (h *Handlers) GetPlan(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "taskID is required")
		return
	}

	plan, err := h.svc.DB.GetPlanByTask(r.Context(), taskID)
	if err != nil {
		slog.Error("failed to get plan", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to retrieve plan")
		return
	}
	if plan == nil {
		writeError(w, http.StatusNotFound, "no plan found for this task")
		return
	}

	// Parse subtasks from JSON
	var subtasks []db.Subtask
	if err := json.Unmarshal([]byte(plan.Subtasks), &subtasks); err != nil {
		slog.Error("failed to parse subtasks", "plan_id", plan.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to parse plan subtasks")
		return
	}

	// Get questions
	questions, _ := h.svc.DB.ListPlannerQuestions(r.Context(), taskID)

	resp := PlanResponse{
		ID:              plan.ID,
		TaskID:          plan.TaskID,
		Analysis:        plan.Analysis,
		Strategy:        plan.Strategy,
		Subtasks:        subtasks,
		Questions:       questions,
		EstimatedAgents: plan.EstimatedAgents,
		Notes:           plan.Notes,
		Status:          plan.Status,
		CreatedAt:       plan.CreatedAt,
	}

	writeJSON(w, http.StatusOK, resp)
}

// UpdatePlan handles PUT /api/tasks/{taskID}/plan.
// Allows the user to modify the plan while in "planned" state.
func (h *Handlers) UpdatePlan(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "taskID is required")
		return
	}

	// Check task is in planned state
	task, err := h.svc.DB.GetTask(r.Context(), taskID)
	if err != nil || task == nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if task.Status != db.TaskStatusPlanned {
		writeError(w, http.StatusBadRequest, "plan can only be modified when task is in 'planned' state")
		return
	}

	plan, err := h.svc.DB.GetPlanByTask(r.Context(), taskID)
	if err != nil || plan == nil {
		writeError(w, http.StatusNotFound, "no plan found for this task")
		return
	}

	var req struct {
		Strategy string       `json:"strategy,omitempty"`
		Subtasks []db.Subtask `json:"subtasks,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	now := time.Now().UTC()

	if req.Strategy != "" {
		if req.Strategy != "parallel" && req.Strategy != "sequential" {
			writeError(w, http.StatusBadRequest, "strategy must be 'parallel' or 'sequential'")
			return
		}
		_, err := h.svc.DB.ExecContext(r.Context(),
			"UPDATE plans SET strategy = ?, updated_at = ? WHERE id = ?",
			req.Strategy, now, plan.ID,
		)
		if err != nil {
			slog.Error("failed to update plan strategy", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to update plan")
			return
		}
	}

	if len(req.Subtasks) > 0 {
		subtasksJSON, err := json.Marshal(req.Subtasks)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid subtasks: "+err.Error())
			return
		}
		_, err = h.svc.DB.ExecContext(r.Context(),
			"UPDATE plans SET subtasks = ?, status = 'modified', updated_at = ? WHERE id = ?",
			string(subtasksJSON), now, plan.ID,
		)
		if err != nil {
			slog.Error("failed to update plan subtasks", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to update plan")
			return
		}
	}

	// Record event
	h.recordEventBackground(r.Context(), taskID, "plan.modified", nil)

	// Re-fetch the updated plan
	updatedPlan, _ := h.svc.DB.GetPlanByTask(r.Context(), taskID)
	if updatedPlan == nil {
		writeError(w, http.StatusInternalServerError, "failed to retrieve updated plan")
		return
	}

	var subtasks []db.Subtask
	_ = json.Unmarshal([]byte(updatedPlan.Subtasks), &subtasks)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": updatedPlan.Status,
		"plan": PlanResponse{
			ID:              updatedPlan.ID,
			TaskID:          updatedPlan.TaskID,
			Analysis:        updatedPlan.Analysis,
			Strategy:        updatedPlan.Strategy,
			Subtasks:        subtasks,
			EstimatedAgents: updatedPlan.EstimatedAgents,
			Notes:           updatedPlan.Notes,
			Status:          updatedPlan.Status,
			CreatedAt:       updatedPlan.CreatedAt,
		},
	})
}

// GetQuestions handles GET /api/tasks/{taskID}/questions.
func (h *Handlers) GetQuestions(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "taskID is required")
		return
	}

	questions, err := h.svc.DB.ListPlannerQuestions(r.Context(), taskID)
	if err != nil {
		slog.Error("failed to list questions", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list questions")
		return
	}

	if questions == nil {
		questions = []db.PlannerQuestion{}
	}

	// Transform JSON string fields into proper arrays for the frontend
	type questionResponse struct {
		ID          string     `json:"id"`
		TaskID      string     `json:"task_id"`
		Text        string     `json:"text"`
		Answer      *string    `json:"answer,omitempty"`
		Status      string     `json:"status"`
		Suggestions []string   `json:"suggestions,omitempty"`
		Options     []string   `json:"options,omitempty"`
		AskedAt     time.Time  `json:"asked_at"`
		AnsweredAt  *time.Time `json:"answered_at,omitempty"`
	}

	resp := make([]questionResponse, len(questions))
	for i, q := range questions {
		resp[i] = questionResponse{
			ID:         q.ID,
			TaskID:     q.TaskID,
			Text:       q.Text,
			Answer:     q.Answer,
			Status:     q.Status,
			AskedAt:    q.AskedAt,
			AnsweredAt: q.AnsweredAt,
		}
		if q.Suggestions != nil {
			_ = json.Unmarshal([]byte(*q.Suggestions), &resp[i].Suggestions)
		}
		if q.Options != nil {
			_ = json.Unmarshal([]byte(*q.Options), &resp[i].Options)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"questions": resp,
	})
}

// AnswerQuestion handles POST /api/tasks/{taskID}/questions/{questionID}/answer.
func (h *Handlers) AnswerQuestion(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	questionID := chi.URLParam(r, "questionID")

	if taskID == "" || questionID == "" {
		writeError(w, http.StatusBadRequest, "taskID and questionID are required")
		return
	}

	var req struct {
		Answer string `json:"answer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Answer == "" {
		writeError(w, http.StatusBadRequest, "answer is required")
		return
	}

	if err := h.svc.TaskManager.AnswerQuestion(r.Context(), taskID, questionID, req.Answer); err != nil {
		slog.Error("failed to answer question", "task_id", taskID, "question_id", questionID, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":      "answered",
		"question_id": questionID,
	})
}
