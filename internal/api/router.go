package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/klaudio-ai/klaudio/internal/agent"
	"github.com/klaudio-ai/klaudio/internal/config"
	"github.com/klaudio-ai/klaudio/internal/db"
	"github.com/klaudio-ai/klaudio/internal/docker"
	"github.com/klaudio-ai/klaudio/internal/embedded"
	"github.com/klaudio-ai/klaudio/internal/files"
	"github.com/klaudio-ai/klaudio/internal/stream"
	"github.com/klaudio-ai/klaudio/internal/task"
)

// Services bundles all dependencies needed by HTTP handlers.
type Services struct {
	DB          *db.DB
	Docker      *docker.Manager
	Config      *config.Config
	StreamHub   *stream.Hub
	MessageBus  *stream.MessageBus
	TaskManager *task.TaskManager
	FileManager *files.Manager
	Pool        *agent.Pool
}

// NewRouter creates the Chi router with all middleware and route definitions.
func NewRouter(cfg *config.Config, svc *Services) chi.Router {
	r := chi.NewRouter()

	h := NewHandlers(svc)

	// -- Routes --
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		h.HealthCheck(w, r)
	})

	// WebSocket streaming — minimal middleware only.
	// Most middleware wraps ResponseWriter which breaks http.Hijacker
	// needed for WebSocket upgrade.
	r.Route("/ws", func(ws chi.Router) {
		ws.Get("/tasks/{taskID}/stream", h.HandleTaskStream)
		ws.Get("/tasks/{taskID}/messages", h.HandleMessageStream)
	})

	r.Route("/api", func(r chi.Router) {
		r.Use(RequestID)
		r.Use(RequestLogger)
		r.Use(middleware.Recoverer)
		r.Use(middleware.Timeout(120 * time.Second))
		r.Use(cors.Handler(cors.Options{
			AllowedOrigins:   []string{"http://localhost:5173", "http://localhost:3000"},
			AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
			AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
			ExposedHeaders:   []string{"X-Request-ID"},
			AllowCredentials: true,
			MaxAge:           300,
		}))
		// Tasks
		r.Route("/tasks", func(r chi.Router) {
			r.Post("/", h.CreateTask)
			r.Get("/", h.ListTasks)
			r.Get("/{taskID}", h.GetTask)
			r.Delete("/{taskID}", h.DeleteTask)

			// Task actions
			r.Post("/{taskID}/start", h.StartTask)
			r.Post("/{taskID}/approve", h.ApproveTask)
			r.Post("/{taskID}/stop", h.StopTask)
			r.Post("/{taskID}/resume", h.ResumeTask)
			r.Post("/{taskID}/replan", h.ReplanTask)
			r.Post("/{taskID}/relaunch", h.RelaunchTask)
			r.Post("/{taskID}/message", h.SendMessage)

			// Plan management
			r.Get("/{taskID}/plan", h.GetPlan)
			r.Put("/{taskID}/plan", h.UpdatePlan)

			// Planner questions
			r.Get("/{taskID}/questions", h.GetQuestions)
			r.Post("/{taskID}/questions/{questionID}/answer", h.AnswerQuestion)

			// File management
			r.Post("/{taskID}/files", h.UploadFiles)
			r.Get("/{taskID}/files", h.ListFiles)
			r.Get("/{taskID}/files/{filename}", h.DownloadFile)

			// File content, edit, delete (separate paths to avoid {filename} conflicts)
			r.Get("/{taskID}/file-viewer", h.GetFileContent)
			r.Put("/{taskID}/file-viewer", h.UpdateFileContent)
			r.Delete("/{taskID}/file-viewer", h.DeleteFile)

			// Agent management
			r.Get("/{taskID}/agents", h.ListTaskAgents)
			r.Get("/{taskID}/agents/{agentID}", h.GetAgent)

			// Agent messages (inter-agent communication)
			r.Get("/{taskID}/messages", h.ListAgentMessages)
			r.Post("/{taskID}/messages", h.SendAgentMessage)
		})

		// Phase 3 — WebSocket streaming (no middleware timeout for long-lived connections)

		// Team templates
		r.Route("/team-templates", func(r chi.Router) {
			r.Get("/", h.ListTeamTemplates)
			r.Post("/", h.CreateTeamTemplate)
			r.Get("/{templateID}", h.GetTeamTemplate)
			r.Delete("/{templateID}", h.DeleteTeamTemplate)
		})

		// Repository templates
		r.Route("/repo-templates", func(r chi.Router) {
			r.Get("/", h.ListRepoTemplates)
			r.Post("/", h.CreateRepoTemplate)
			r.Get("/{templateID}", h.GetRepoTemplate)
			r.Put("/{templateID}", h.UpdateRepoTemplate)
			r.Delete("/{templateID}", h.DeleteRepoTemplate)

			// Repo memory
			r.Get("/{templateID}/memory", h.GetRepoMemory)
			r.Delete("/{templateID}/memory", h.DeleteRepoMemory)
		})

		// Config endpoints
		r.Route("/config", func(r chi.Router) {
			r.Get("/", h.GetConfig)
		})
	})

	// Serve embedded frontend on all unmatched routes (SPA fallback)
	if embedded.HasFrontend() {
		slog.Info("serving embedded frontend")
		r.NotFound(embedded.FrontendHandler().ServeHTTP)
	}

	return r
}
