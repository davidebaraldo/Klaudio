package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/klaudio-ai/klaudio/internal/docker"
)

// AgentStats holds stats for a single agent container.
type AgentStats struct {
	AgentID     string               `json:"agent_id"`
	SubtaskID   string               `json:"subtask_id"`
	Role        string               `json:"role"`
	ContainerID string               `json:"container_id"`
	Stats       *docker.ContainerStats `json:"stats"`
}

// TaskStatsResponse is the response for GET /api/tasks/{taskID}/stats.
type TaskStatsResponse struct {
	TaskID    string       `json:"task_id"`
	Agents    []AgentStats `json:"agents"`
	Timestamp string       `json:"timestamp"`
}

// GetTaskStats handles GET /api/tasks/{taskID}/stats.
// Returns a one-shot snapshot of stats for all running containers of a task.
func (h *Handlers) GetTaskStats(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "taskID is required")
		return
	}

	agents := h.svc.Pool.ActiveForTask(taskID)
	if len(agents) == 0 {
		writeJSON(w, http.StatusOK, TaskStatsResponse{
			TaskID:    taskID,
			Agents:    []AgentStats{},
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	var agentStats []AgentStats
	for _, a := range agents {
		if a.ContainerID == "" {
			continue
		}
		stats, err := h.svc.Docker.GetContainerStats(r.Context(), a.ContainerID)
		if err != nil {
			slog.Debug("failed to get stats for agent", "agent_id", a.ID, "error", err)
			continue
		}
		subtaskID := ""
		if a.SubtaskID != "" {
			subtaskID = a.SubtaskID
		}
		agentStats = append(agentStats, AgentStats{
			AgentID:     a.ID,
			SubtaskID:   subtaskID,
			Role:        string(a.Role),
			ContainerID: a.ContainerID,
			Stats:       stats,
		})
	}

	writeJSON(w, http.StatusOK, TaskStatsResponse{
		TaskID:    taskID,
		Agents:    agentStats,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// HandleStatsStream handles WS /ws/tasks/{taskID}/stats.
// Streams container stats at a configurable interval (default 2s).
func (h *Handlers) HandleStatsStream(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		http.Error(w, "taskID is required", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("stats websocket upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	// Parse interval from query (default 2s)
	interval := 2 * time.Second
	if v := r.URL.Query().Get("interval"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 500*time.Millisecond {
			interval = d
		}
	}

	slog.Debug("stats stream started", "task_id", taskID, "interval", interval)

	// Read pump — just drain client messages to detect disconnect
	closeCh := make(chan struct{})
	go func() {
		defer close(closeCh)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	ctx := context.Background()

	for {
		select {
		case <-closeCh:
			return
		case <-ticker.C:
			agents := h.svc.Pool.ActiveForTask(taskID)

			var agentStats []AgentStats
			for _, a := range agents {
				if a.ContainerID == "" {
					continue
				}
				stats, err := h.svc.Docker.GetContainerStats(ctx, a.ContainerID)
				if err != nil {
					continue
				}
				subtaskID := ""
				if a.SubtaskID != "" {
					subtaskID = a.SubtaskID
				}
				agentStats = append(agentStats, AgentStats{
					AgentID:     a.ID,
					SubtaskID:   subtaskID,
					Role:        string(a.Role),
					ContainerID: a.ContainerID,
					Stats:       stats,
				})
			}

			msg := TaskStatsResponse{
				TaskID:    taskID,
				Agents:    agentStats,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			}

			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}

			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		}
	}
}
