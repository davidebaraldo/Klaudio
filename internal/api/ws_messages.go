package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/klaudio-ai/klaudio/internal/stream"
)

// HandleMessageStream upgrades an HTTP connection to a WebSocket and streams
// agent messages in real-time for the given task via the MessageBus.
//
// Route: GET /ws/tasks/{taskID}/messages
func (h *Handlers) HandleMessageStream(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		http.Error(w, "taskID is required", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed for message stream", "error", err, "task_id", taskID)
		return
	}
	defer conn.Close()

	bus := h.svc.MessageBus
	if bus == nil {
		slog.Error("message bus not available")
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "message bus not available"))
		return
	}

	// Subscribe to the message bus for this task
	ch := bus.Subscribe(taskID)
	defer bus.Unsubscribe(taskID, ch)

	// Send existing messages as backfill
	h.sendMessageBackfill(conn, taskID)

	done := make(chan struct{})

	// Read pump: keep connection alive (read pongs, detect close)
	go func() {
		defer close(done)
		conn.SetReadLimit(4096)
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		conn.SetPongHandler(func(string) error {
			conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			return nil
		})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	// Write pump: forward messages from bus to WebSocket
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

			evt := stream.Event{
				Type:      "agent_message",
				EventName: msg.MsgType,
				Data:      msg,
			}
			data, err := json.Marshal(evt)
			if err != nil {
				slog.Warn("failed to marshal agent message event", "error", err)
				continue
			}
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				slog.Debug("websocket write error on message stream", "error", err, "task_id", taskID)
				return
			}

		case <-pingTicker.C:
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-done:
			return
		}
	}
}

// sendMessageBackfill sends all existing messages for a task as a backfill batch.
func (h *Handlers) sendMessageBackfill(conn *websocket.Conn, taskID string) {
	messages, err := h.svc.DB.ListAgentMessages(context.Background(), taskID, 500)
	if err != nil {
		slog.Warn("failed to load message backfill", "task_id", taskID, "error", err)
		return
	}

	if len(messages) == 0 {
		return
	}

	// Send backfill start marker
	startEvt, _ := json.Marshal(stream.Event{Type: "message_backfill_start"})
	if err := conn.WriteMessage(websocket.TextMessage, startEvt); err != nil {
		return
	}

	// Send each message
	for _, m := range messages {
		evt := stream.Event{
			Type:      "agent_message",
			EventName: m.MsgType,
			Data:      m,
		}
		data, err := json.Marshal(evt)
		if err != nil {
			continue
		}
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return
		}
	}

	// Send backfill end marker
	endEvt, _ := json.Marshal(stream.Event{Type: "message_backfill_end"})
	conn.WriteMessage(websocket.TextMessage, endEvt)
}
