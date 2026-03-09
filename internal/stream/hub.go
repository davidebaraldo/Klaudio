package stream

import (
	"encoding/json"
	"log/slog"
	"sync"
)

// hubCommand is the type tag for internal hub operations dispatched via channel.
type hubCommand int

const (
	cmdRegisterAgent hubCommand = iota
	cmdUnregisterAgent
	cmdSubscribe
	cmdUnsubscribe
	cmdInjectMessage
	cmdBroadcastEvent
)

// hubMsg carries a command and its associated data through the hub's event loop channel.
type hubMsg struct {
	cmd     hubCommand
	agentID string
	taskID  string
	client  *Client
	data    []byte
	event   *Event
	result  chan any // for synchronous replies (RegisterAgent, GetBackfill, etc.)
}

// Hub is the central real-time data router. It manages agent output streams
// and WebSocket client subscriptions. All mutations go through a single event
// loop goroutine to avoid lock contention on the hot path; read-only helpers
// that need immediate results use an RWMutex.
type Hub struct {
	mu              sync.RWMutex
	streams         map[string]*AgentStream // agentID -> active stream
	finishedBuffers map[string]*finishedAgent // agentID -> preserved buffer for backfill
	clients         map[string][]*Client    // taskID -> connected clients

	msgCh    chan hubMsg
	shutdown chan struct{}
	done     chan struct{}
}

// finishedAgent holds the buffer and task ID for an agent that has completed,
// allowing late-connecting clients to still receive backfill data.
type finishedAgent struct {
	TaskID string
	Buffer *RingBuffer
}

// NewHub creates a new Hub. Call Run() in a goroutine to start it.
func NewHub() *Hub {
	return &Hub{
		streams:         make(map[string]*AgentStream),
		finishedBuffers: make(map[string]*finishedAgent),
		clients:         make(map[string][]*Client),
		msgCh:           make(chan hubMsg, 512),
		shutdown:        make(chan struct{}),
		done:            make(chan struct{}),
	}
}

// Run is the main event loop. It should be started in its own goroutine.
// It processes agent output and hub commands until Shutdown is called.
func (h *Hub) Run() {
	defer close(h.done)

	for {
		select {
		case <-h.shutdown:
			h.cleanupAll()
			return

		case msg := <-h.msgCh:
			h.handleMsg(msg)
		}
	}
}

// handleMsg dispatches a single hub command.
func (h *Hub) handleMsg(msg hubMsg) {
	switch msg.cmd {
	case cmdRegisterAgent:
		h.doRegisterAgent(msg)
	case cmdUnregisterAgent:
		h.doUnregisterAgent(msg)
	case cmdSubscribe:
		h.doSubscribe(msg)
	case cmdUnsubscribe:
		h.doUnsubscribe(msg)
	case cmdInjectMessage:
		h.doInjectMessage(msg)
	case cmdBroadcastEvent:
		h.doBroadcastEvent(msg)
	}
}

// RegisterAgent creates and registers a new AgentStream. It blocks until the
// stream is set up and returns the stream pointer.
func (h *Hub) RegisterAgent(agentID, taskID string) *AgentStream {
	result := make(chan any, 1)
	h.msgCh <- hubMsg{
		cmd:     cmdRegisterAgent,
		agentID: agentID,
		taskID:  taskID,
		result:  result,
	}
	return (<-result).(*AgentStream)
}

func (h *Hub) doRegisterAgent(msg hubMsg) {
	as := NewAgentStream(msg.agentID, msg.taskID)

	h.mu.Lock()
	h.streams[msg.agentID] = as
	h.mu.Unlock()

	// Start a goroutine that reads from the agent's OutputCh, buffers data,
	// and fans it out to subscribed clients.
	go h.pumpAgentOutput(as)

	slog.Info("agent stream registered", "agent_id", msg.agentID, "task_id", msg.taskID)

	if msg.result != nil {
		msg.result <- as
	}
}

// pumpAgentOutput reads from an agent's OutputCh, writes to the ring buffer,
// and forwards data to all clients subscribed to the agent's task.
func (h *Hub) pumpAgentOutput(as *AgentStream) {
	totalBytes := 0
	for {
		select {
		case data, ok := <-as.OutputCh:
			if !ok {
				slog.Info("agent output channel closed", "agent_id", as.AgentID, "total_bytes", totalBytes)
				return
			}
			totalBytes += len(data)
			// Buffer the data for late joiners.
			as.Buffer.Write(data) //nolint:errcheck

			// Fan out to subscribed clients.
			h.mu.RLock()
			clients := h.clients[as.TaskID]
			h.mu.RUnlock()

			for _, c := range clients {
				// Filter: if client subscribed to a specific agent, skip others.
				if c.AgentID != "" && c.AgentID != as.AgentID {
					continue
				}
				select {
				case c.Send <- data:
				default:
					// Client too slow — drop this frame.
					slog.Debug("dropping frame for slow client", "task_id", as.TaskID)
				}
			}

		case <-as.Done:
			slog.Info("agent output pump stopped (done signal)", "agent_id", as.AgentID, "total_bytes", totalBytes, "buffer_len", as.Buffer.Len())
			return
		}
	}
}

// UnregisterAgent removes the stream for the given agent and closes its channels.
func (h *Hub) UnregisterAgent(agentID string) {
	h.msgCh <- hubMsg{
		cmd:     cmdUnregisterAgent,
		agentID: agentID,
	}
}

func (h *Hub) doUnregisterAgent(msg hubMsg) {
	h.mu.Lock()
	as, ok := h.streams[msg.agentID]
	if ok {
		delete(h.streams, msg.agentID)
		// Preserve the buffer for late-connecting clients (backfill)
		h.finishedBuffers[msg.agentID] = &finishedAgent{
			TaskID: as.TaskID,
			Buffer: as.Buffer,
		}
	}
	h.mu.Unlock()

	if ok {
		// Signal the output pump to stop.
		select {
		case <-as.Done:
		default:
			close(as.Done)
		}
		slog.Info("agent stream unregistered (buffer preserved)", "agent_id", msg.agentID)
	}
}

// Subscribe adds a WebSocket client to the hub.
func (h *Hub) Subscribe(client *Client) {
	h.msgCh <- hubMsg{
		cmd:    cmdSubscribe,
		client: client,
	}
}

func (h *Hub) doSubscribe(msg hubMsg) {
	c := msg.client
	h.mu.Lock()
	h.clients[c.TaskID] = append(h.clients[c.TaskID], c)
	h.mu.Unlock()

	slog.Info("client subscribed", "task_id", c.TaskID, "agent_filter", c.AgentID)
}

// Unsubscribe removes a WebSocket client from the hub and closes its Send channel.
func (h *Hub) Unsubscribe(client *Client) {
	h.msgCh <- hubMsg{
		cmd:    cmdUnsubscribe,
		client: client,
	}
}

func (h *Hub) doUnsubscribe(msg hubMsg) {
	c := msg.client
	h.mu.Lock()
	clients := h.clients[c.TaskID]
	for i, existing := range clients {
		if existing == c {
			h.clients[c.TaskID] = append(clients[:i], clients[i+1:]...)
			break
		}
	}
	if len(h.clients[c.TaskID]) == 0 {
		delete(h.clients, c.TaskID)
	}
	h.mu.Unlock()

	// Close the send channel so the write pump exits.
	select {
	case <-c.Done:
	default:
		close(c.Done)
	}

	slog.Info("client unsubscribed", "task_id", c.TaskID)
}

// InjectMessage sends a user message to the agent's stdin channel.
func (h *Hub) InjectMessage(agentID string, message []byte) {
	h.msgCh <- hubMsg{
		cmd:     cmdInjectMessage,
		agentID: agentID,
		data:    message,
	}
}

func (h *Hub) doInjectMessage(msg hubMsg) {
	h.mu.RLock()
	as, ok := h.streams[msg.agentID]
	h.mu.RUnlock()

	if !ok {
		slog.Warn("inject message: agent not found", "agent_id", msg.agentID)
		return
	}

	select {
	case as.InputCh <- msg.data:
	default:
		slog.Warn("inject message: agent input channel full", "agent_id", msg.agentID)
	}
}

// GetBackfill returns buffered output data for backfill when a client connects.
// If agentID is non-empty, returns only that agent's buffer; otherwise returns
// concatenated buffers for all agents belonging to the task.
// Checks both active streams and finished agent buffers.
func (h *Hub) GetBackfill(taskID, agentID string) []byte {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if agentID != "" {
		// Check active streams first
		if as, ok := h.streams[agentID]; ok {
			return as.Buffer.ReadAll()
		}
		// Then check finished buffers
		if fa, ok := h.finishedBuffers[agentID]; ok {
			return fa.Buffer.ReadAll()
		}
		return nil
	}

	// Concatenate all agent buffers for this task (active + finished).
	var result []byte
	for _, as := range h.streams {
		if as.TaskID == taskID {
			data := as.Buffer.ReadAll()
			if len(data) > 0 {
				result = append(result, data...)
			}
		}
	}
	for _, fa := range h.finishedBuffers {
		if fa.TaskID == taskID {
			data := fa.Buffer.ReadAll()
			if len(data) > 0 {
				result = append(result, data...)
			}
		}
	}
	return result
}

// GetFullBuffer returns all buffered data for a specific agent.
func (h *Hub) GetFullBuffer(agentID string) []byte {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if as, ok := h.streams[agentID]; ok {
		return as.Buffer.ReadAll()
	}
	if fa, ok := h.finishedBuffers[agentID]; ok {
		return fa.Buffer.ReadAll()
	}
	return nil
}

// BroadcastEvent sends a JSON event to all WebSocket clients subscribed to the given task.
func (h *Hub) BroadcastEvent(taskID string, event Event) {
	data, err := json.Marshal(event)
	if err != nil {
		slog.Error("failed to marshal event", "error", err)
		return
	}

	h.msgCh <- hubMsg{
		cmd:    cmdBroadcastEvent,
		taskID: taskID,
		data:   data,
		event:  &event,
	}
}

func (h *Hub) doBroadcastEvent(msg hubMsg) {
	h.mu.RLock()
	clients := h.clients[msg.taskID]
	h.mu.RUnlock()

	for _, c := range clients {
		select {
		case c.Send <- msg.data:
		default:
			slog.Debug("dropping event for slow client", "task_id", msg.taskID)
		}
	}
}

// Shutdown gracefully shuts down the hub, closing all streams and client connections.
func (h *Hub) Shutdown() {
	select {
	case <-h.shutdown:
		// Already shut down.
		return
	default:
		close(h.shutdown)
	}
	<-h.done
}

// cleanupAll closes all agent streams and client connections.
func (h *Hub) cleanupAll() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for id, as := range h.streams {
		select {
		case <-as.Done:
		default:
			close(as.Done)
		}
		delete(h.streams, id)
	}

	for taskID, clients := range h.clients {
		for _, c := range clients {
			select {
			case <-c.Done:
			default:
				close(c.Done)
			}
		}
		delete(h.clients, taskID)
	}
}
