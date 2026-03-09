package stream

import (
	"github.com/gorilla/websocket"
)

// AgentStream represents the I/O channels for a single agent container.
// Data flows from the Docker container through OutputCh to the hub,
// and user messages flow through InputCh to the container's stdin.
type AgentStream struct {
	AgentID  string
	TaskID   string
	Buffer   *RingBuffer
	OutputCh chan []byte   // data FROM the container (stdout/stderr)
	InputCh  chan []byte   // messages TO the container (stdin)
	Done     chan struct{} // closed when the agent terminates
}

// NewAgentStream creates an AgentStream with default channel sizes and buffer.
func NewAgentStream(agentID, taskID string) *AgentStream {
	return &AgentStream{
		AgentID:  agentID,
		TaskID:   taskID,
		Buffer:   NewRingBuffer(DefaultBufferSize),
		OutputCh: make(chan []byte, 256),
		InputCh:  make(chan []byte, 64),
		Done:     make(chan struct{}),
	}
}

// Client represents a connected WebSocket client subscribed to task output.
type Client struct {
	Conn    *websocket.Conn
	TaskID  string
	AgentID string       // empty string means all agents of the task
	Send    chan []byte   // outgoing messages (binary or JSON)
	Done    chan struct{} // closed when the client disconnects
}

// NewClient creates a Client for the given WebSocket connection.
func NewClient(conn *websocket.Conn, taskID, agentID string) *Client {
	return &Client{
		Conn:    conn,
		TaskID:  taskID,
		AgentID: agentID,
		Send:    make(chan []byte, 256),
		Done:    make(chan struct{}),
	}
}

// Event is a JSON event sent to WebSocket clients.
type Event struct {
	Type      string `json:"type"`
	EventName string `json:"event,omitempty"`
	Data      any    `json:"data,omitempty"`
}
