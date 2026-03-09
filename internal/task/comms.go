package task

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/klaudio-ai/klaudio/internal/db"
)

// CommsService handles communication between agents in a task.
// It manages:
// 1. Context passing: completed subtask summaries written to filesystem for dependent subtasks
// 2. Broadcast messages: agents exchange messages via the klaudio API (stored in DB)
type CommsService struct {
	db *db.DB
}

// NewCommsService creates a new CommsService.
func NewCommsService(database *db.DB) *CommsService {
	return &CommsService{db: database}
}

// SaveSubtaskContext writes the completion summary for a subtask to:
// - The workspace at .klaudio/context/{subtaskID}.md
// - The database as an agent_message of type "context"
func (cs *CommsService) SaveSubtaskContext(ctx context.Context, taskID, subtaskID, agentID, subtaskName, summary string, workspaceDir string) error {
	// Write to workspace filesystem (for agents that read context files)
	contextDir := filepath.Join(workspaceDir, ".klaudio", "context")
	if err := os.MkdirAll(contextDir, 0o755); err != nil {
		return fmt.Errorf("creating context directory: %w", err)
	}

	content := fmt.Sprintf("# %s\n\nSubtask: %s\nAgent: %s\nCompleted at: %s\n\n%s\n",
		subtaskName, subtaskID, agentID, time.Now().UTC().Format(time.RFC3339), summary)

	contextFile := filepath.Join(contextDir, subtaskID+".md")
	if err := os.WriteFile(contextFile, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing context file: %w", err)
	}

	// Save to DB for UI visibility
	msg := &db.AgentMessage{
		TaskID:        taskID,
		FromAgentID:   &agentID,
		FromSubtaskID: &subtaskID,
		MsgType:       "context",
		Content:       summary,
		CreatedAt:     time.Now().UTC(),
	}
	if err := cs.db.CreateAgentMessage(ctx, msg); err != nil {
		slog.Warn("failed to persist context message", "task_id", taskID, "subtask_id", subtaskID, "error", err)
	}

	return nil
}

// CollectDependencyContext reads the context files for all completed dependencies
// and returns a formatted string to include in the agent prompt.
func (cs *CommsService) CollectDependencyContext(subtask db.Subtask, allSubtasks []db.Subtask, workspaceDir string) string {
	if len(subtask.DependsOn) == 0 {
		return ""
	}

	var parts []string
	contextDir := filepath.Join(workspaceDir, ".klaudio", "context")

	for _, depID := range subtask.DependsOn {
		depName := depID
		for _, st := range allSubtasks {
			if st.ID == depID {
				depName = st.Name
				break
			}
		}

		contextFile := filepath.Join(contextDir, depID+".md")
		data, err := os.ReadFile(contextFile)
		if err != nil {
			slog.Debug("no context file for dependency", "dep_id", depID)
			continue
		}

		parts = append(parts, fmt.Sprintf("### Context from: %s (%s)\n%s", depName, depID, string(data)))
	}

	if len(parts) == 0 {
		return ""
	}

	return "## Context from completed dependencies\n\n" + strings.Join(parts, "\n---\n")
}

// CollectBroadcastMessages reads broadcast messages from the DB for the given task,
// excluding messages from the specified subtask.
func (cs *CommsService) CollectBroadcastMessages(ctx context.Context, taskID string, excludeSubtaskID string) string {
	messages, err := cs.db.ListAgentMessages(ctx, taskID, 200)
	if err != nil {
		slog.Warn("failed to list broadcast messages", "task_id", taskID, "error", err)
		return ""
	}

	var parts []string
	for _, m := range messages {
		if m.MsgType != "message" {
			continue
		}
		// Skip own messages
		if m.FromSubtaskID != nil && *m.FromSubtaskID == excludeSubtaskID {
			continue
		}

		from := "unknown"
		if m.FromSubtaskID != nil {
			from = *m.FromSubtaskID
		}
		parts = append(parts, fmt.Sprintf("[%s] %s: %s", m.CreatedAt.Format(time.RFC3339), from, m.Content))
	}

	if len(parts) == 0 {
		return ""
	}

	return "## Messages from other agents\n\n" + strings.Join(parts, "\n")
}

// RecordBroadcast persists a broadcast message to DB only (no filesystem).
func (cs *CommsService) RecordBroadcast(ctx context.Context, taskID, fromAgentID, fromSubtaskID, content string) error {
	msg := &db.AgentMessage{
		TaskID:        taskID,
		FromAgentID:   &fromAgentID,
		FromSubtaskID: &fromSubtaskID,
		MsgType:       "message",
		Content:       content,
		CreatedAt:     time.Now().UTC(),
	}
	if err := cs.db.CreateAgentMessage(ctx, msg); err != nil {
		return fmt.Errorf("persisting broadcast message: %w", err)
	}
	return nil
}

// APICommsInstructions returns prompt text telling agents how to communicate via the API.
func APICommsInstructions(apiURL, taskID, subtaskID string) string {
	return fmt.Sprintf(`## Agent Communication

You are part of a team of agents working on the same task. Communicate via the klaudio API.

### Sending messages to other agents
To broadcast a message to all other agents:
` + "```bash" + `
curl -s -X POST %s/api/tasks/%s/messages \
  -H "Content-Type: application/json" \
  -d '{"from": "%s", "content": "YOUR MESSAGE HERE"}'
` + "```" + `

### Reading messages from other agents
To see messages from the team:
` + "```bash" + `
curl -s %s/api/tasks/%s/messages | jq '.messages[] | select(.msg_type=="message")'
` + "```" + `

### Reading context from completed agents
Check .klaudio/context/ for summaries from agents that completed before you.

### When to communicate
- IMMEDIATELY when you define a shared interface or API that others depend on
- When you find a critical issue or blocker
- When you complete a significant milestone
- When you change something that affects other agents
`, apiURL, taskID, subtaskID, apiURL, taskID)
}

// ExtractAgentSummary tries to extract a meaningful summary from the agent's output.
func ExtractAgentSummary(output string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 2000
	}

	markers := []string{
		"## Summary",
		"## Work Summary",
		"### Summary",
		"SUMMARY:",
	}
	for _, marker := range markers {
		idx := strings.LastIndex(output, marker)
		if idx >= 0 {
			summary := output[idx:]
			if len(summary) > maxLen {
				summary = summary[:maxLen]
			}
			return summary
		}
	}

	if len(output) > maxLen {
		output = output[len(output)-maxLen:]
	}
	return output
}
