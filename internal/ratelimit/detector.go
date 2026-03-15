package ratelimit

import (
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// State represents the current rate limit state for an agent.
type State struct {
	AgentID      string    `json:"agent_id"`
	TaskID       string    `json:"task_id"`
	IsLimited    bool      `json:"is_limited"`
	RetryIn      int       `json:"retry_in_seconds"`
	Attempt      int       `json:"attempt"`
	MaxRetries   int       `json:"max_retries"`
	Message      string    `json:"message"`
	ResetAt      time.Time `json:"reset_at"`
	DetectedAt   time.Time `json:"detected_at"`
}

// Tracker tracks rate limit state across agents.
type Tracker struct {
	mu     sync.RWMutex
	states map[string]*State // agentID -> state
}

// NewTracker creates a new rate limit tracker.
func NewTracker() *Tracker {
	return &Tracker{
		states: make(map[string]*State),
	}
}

// streamEvent is the structure of stream-json events from Claude Code.
type streamEvent struct {
	Type    string          `json:"type"`
	Subtype string          `json:"subtype"`
	Data    json.RawMessage `json:"data"`
}

// rateLimitData is the data payload of a rate limit event.
type rateLimitData struct {
	RetryIn    int    `json:"retry_in_seconds"`
	Attempt    int    `json:"attempt"`
	MaxRetries int    `json:"max_retries"`
	Message    string `json:"message"`
}

// DetectFromOutput scans a chunk of output data for rate limit events.
// Returns a State if a rate limit event is detected, nil otherwise.
func (t *Tracker) DetectFromOutput(agentID, taskID string, data []byte) *State {
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line[0] != '{' {
			continue
		}

		var evt streamEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}

		switch {
		case evt.Type == "system" && evt.Subtype == "rate_limit":
			return t.handleRateLimitEvent(agentID, taskID, evt.Data)

		case evt.Type == "system" && evt.Subtype == "rate_limit_retry":
			return t.handleRetryEvent(agentID, taskID, evt.Data)

		case evt.Type == "system" && evt.Subtype == "rate_limit_exhausted":
			return t.handleExhaustedEvent(agentID, taskID, evt.Data)

		case evt.Type == "error":
			if isRateLimitError(line) {
				return t.handleGenericRateLimit(agentID, taskID)
			}
		}
	}

	return nil
}

func (t *Tracker) handleRateLimitEvent(agentID, taskID string, data json.RawMessage) *State {
	var d rateLimitData
	if err := json.Unmarshal(data, &d); err != nil {
		d = rateLimitData{RetryIn: 30, Attempt: 1, MaxRetries: 10, Message: "Rate limited"}
	}

	state := &State{
		AgentID:    agentID,
		TaskID:     taskID,
		IsLimited:  true,
		RetryIn:    d.RetryIn,
		Attempt:    d.Attempt,
		MaxRetries: d.MaxRetries,
		Message:    d.Message,
		ResetAt:    time.Now().Add(time.Duration(d.RetryIn) * time.Second),
		DetectedAt: time.Now(),
	}

	t.mu.Lock()
	t.states[agentID] = state
	t.mu.Unlock()

	return state
}

func (t *Tracker) handleRetryEvent(agentID, taskID string, data json.RawMessage) *State {
	var d rateLimitData
	json.Unmarshal(data, &d) //nolint:errcheck

	state := &State{
		AgentID:    agentID,
		TaskID:     taskID,
		IsLimited:  false,
		Attempt:    d.Attempt,
		MaxRetries: d.MaxRetries,
		Message:    d.Message,
		DetectedAt: time.Now(),
	}

	t.mu.Lock()
	t.states[agentID] = state
	t.mu.Unlock()

	return state
}

func (t *Tracker) handleExhaustedEvent(agentID, taskID string, data json.RawMessage) *State {
	var d rateLimitData
	json.Unmarshal(data, &d) //nolint:errcheck

	state := &State{
		AgentID:    agentID,
		TaskID:     taskID,
		IsLimited:  true,
		Attempt:    d.Attempt,
		MaxRetries: d.MaxRetries,
		Message:    d.Message,
		DetectedAt: time.Now(),
	}

	t.mu.Lock()
	t.states[agentID] = state
	t.mu.Unlock()

	return state
}

func (t *Tracker) handleGenericRateLimit(agentID, taskID string) *State {
	state := &State{
		AgentID:    agentID,
		TaskID:     taskID,
		IsLimited:  true,
		RetryIn:    30,
		Attempt:    1,
		MaxRetries: 10,
		Message:    "Rate limited by API",
		ResetAt:    time.Now().Add(30 * time.Second),
		DetectedAt: time.Now(),
	}

	t.mu.Lock()
	t.states[agentID] = state
	t.mu.Unlock()

	return state
}

// ClearAgent removes rate limit state for an agent.
func (t *Tracker) ClearAgent(agentID string) {
	t.mu.Lock()
	delete(t.states, agentID)
	t.mu.Unlock()
}

// GetAgentState returns the current rate limit state for an agent.
func (t *Tracker) GetAgentState(agentID string) *State {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.states[agentID]
}

// GetTaskStates returns all rate limit states for a task.
func (t *Tracker) GetTaskStates(taskID string) []*State {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var states []*State
	for _, s := range t.states {
		if s.TaskID == taskID {
			states = append(states, s)
		}
	}
	return states
}

// isRateLimitError checks if a JSON error line contains rate limit indicators.
func isRateLimitError(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "rate_limit") ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "overloaded") ||
		strings.Contains(lower, "too many requests") ||
		strings.Contains(lower, "429") ||
		strings.Contains(lower, "resourceexhausted")
}
