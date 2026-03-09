package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/klaudio-ai/klaudio/internal/stream"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		// Allow connections from the dev servers and same origin.
		return true
	},
}

// clientPayload is the JSON structure sent from the WebSocket client.
type clientPayload struct {
	Type    string `json:"type"`     // "message" | "signal"
	AgentID string `json:"agent_id"`
	Content string `json:"content"`
}

// HandleTaskStream upgrades an HTTP connection to a WebSocket and streams
// real-time agent output for the given task. It supports an optional ?agent=
// query parameter to filter output to a single agent.
//
// Route: GET /ws/tasks/{taskID}/stream
func (h *Handlers) HandleTaskStream(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		http.Error(w, "taskID is required", http.StatusBadRequest)
		return
	}
	agentID := r.URL.Query().Get("agent")

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err, "task_id", taskID)
		return
	}

	client := stream.NewClient(conn, taskID, agentID)

	hub := h.svc.StreamHub
	hub.Subscribe(client)

	// Send backfill data so late joiners see previous output.
	backfill := hub.GetBackfill(taskID, agentID)
	if len(backfill) > 0 {
		// Wrap backfill between start/end markers so the UI knows this is historical data.
		startEvt, _ := json.Marshal(stream.Event{Type: "backfill_start"})
		if wErr := conn.WriteMessage(websocket.TextMessage, startEvt); wErr != nil {
			slog.Warn("failed to write backfill_start", "error", wErr)
		}
		if wErr := conn.WriteMessage(websocket.BinaryMessage, backfill); wErr != nil {
			slog.Warn("failed to write backfill data", "error", wErr)
		}
		endEvt, _ := json.Marshal(stream.Event{Type: "backfill_end"})
		if wErr := conn.WriteMessage(websocket.TextMessage, endEvt); wErr != nil {
			slog.Warn("failed to write backfill_end", "error", wErr)
		}
	}

	// Start the read and write pumps.
	go h.wsReadPump(conn, client, hub)
	h.wsWritePump(conn, client)

	// Cleanup when we return (write pump exited).
	hub.Unsubscribe(client)
	conn.Close()
}

// wsReadPump reads JSON messages from the WebSocket client and dispatches them.
// It runs in its own goroutine and signals completion by closing client.Done.
func (h *Handlers) wsReadPump(conn *websocket.Conn, client *stream.Client, hub *stream.Hub) {
	defer func() {
		// Signal the write pump to stop.
		select {
		case <-client.Done:
		default:
			close(client.Done)
		}
	}()

	conn.SetReadLimit(64 * 1024) // 64KB max message size
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Warn("websocket read error", "error", err, "task_id", client.TaskID)
			}
			return
		}

		var payload clientPayload
		if err := json.Unmarshal(msg, &payload); err != nil {
			slog.Debug("invalid client payload", "error", err)
			continue
		}

		switch payload.Type {
		case "message":
			if payload.AgentID != "" && payload.Content != "" {
				hub.InjectMessage(payload.AgentID, []byte(payload.Content))
			}
		default:
			slog.Debug("unknown client message type", "type", payload.Type)
		}
	}
}

// wsWritePump writes messages from client.Send to the WebSocket connection.
// Binary data is sent as BinaryMessage; JSON events (prefixed or detected)
// are sent as TextMessage if they start with '{'.
func (h *Handlers) wsWritePump(conn *websocket.Conn, client *stream.Client) {
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case data, ok := <-client.Send:
			if !ok {
				conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

			// Determine message type: if it starts with '{', it is a JSON event.
			msgType := websocket.BinaryMessage
			if len(data) > 0 && data[0] == '{' {
				msgType = websocket.TextMessage
			}

			if err := conn.WriteMessage(msgType, data); err != nil {
				slog.Debug("websocket write error", "error", err, "task_id", client.TaskID)
				return
			}

		case <-pingTicker.C:
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-client.Done:
			return
		}
	}
}
