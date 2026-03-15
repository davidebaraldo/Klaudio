package stream

import (
	"log/slog"
	"sync"
)

// AgentMessageEvent represents an agent message pushed through the MessageBus.
type AgentMessageEvent struct {
	ID            int64   `json:"id"`
	TaskID        string  `json:"task_id"`
	FromAgentID   *string `json:"from_agent_id,omitempty"`
	FromSubtaskID *string `json:"from_subtask_id,omitempty"`
	ToAgentID     *string `json:"to_agent_id,omitempty"`
	ToSubtaskID   *string `json:"to_subtask_id,omitempty"`
	MsgType       string  `json:"msg_type"`
	Content       string  `json:"content"`
	CreatedAt     string  `json:"created_at"`
}

// MessageBus is a simple pub/sub for agent messages, keyed by task ID.
// Subscribers receive messages in real-time via channels, replacing the need
// to poll the database for new messages.
type MessageBus struct {
	mu   sync.RWMutex
	subs map[string][]chan AgentMessageEvent // taskID -> subscriber channels
}

// NewMessageBus creates a new MessageBus.
func NewMessageBus() *MessageBus {
	return &MessageBus{
		subs: make(map[string][]chan AgentMessageEvent),
	}
}

// Subscribe creates a channel that receives messages for the given task ID.
// The caller must call Unsubscribe when done to avoid leaks.
func (mb *MessageBus) Subscribe(taskID string) chan AgentMessageEvent {
	ch := make(chan AgentMessageEvent, 64)
	mb.mu.Lock()
	mb.subs[taskID] = append(mb.subs[taskID], ch)
	mb.mu.Unlock()
	slog.Debug("message bus: subscribed", "task_id", taskID)
	return ch
}

// Unsubscribe removes a subscriber channel for the given task ID.
func (mb *MessageBus) Unsubscribe(taskID string, ch chan AgentMessageEvent) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	subs := mb.subs[taskID]
	for i, s := range subs {
		if s == ch {
			mb.subs[taskID] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	if len(mb.subs[taskID]) == 0 {
		delete(mb.subs, taskID)
	}
	slog.Debug("message bus: unsubscribed", "task_id", taskID)
}

// Publish sends an agent message event to all subscribers of the given task.
// Non-blocking: if a subscriber's channel is full, the message is dropped.
func (mb *MessageBus) Publish(taskID string, evt AgentMessageEvent) {
	mb.mu.RLock()
	subs := mb.subs[taskID]
	mb.mu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- evt:
		default:
			slog.Debug("message bus: dropping message for slow subscriber", "task_id", taskID)
		}
	}
}
