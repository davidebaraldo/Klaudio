package task

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/klaudio-ai/klaudio/internal/db"
)

// workspaceFileRule is a standard instruction appended to all agent prompts
// reminding them that only files inside the workspace are visible to the platform.
const workspaceFileRule = `## Workspace Rule
All files you create or modify MUST be inside the workspace directory (/home/agent/workspace).
Files created outside the workspace are invisible to the platform and will NOT be shown to the user.
This includes output files, generated code, test files, documentation — everything.
`

// PlanOutput is the raw JSON structure produced by the planner agent.
type PlanOutput struct {
	Analysis        string       `json:"analysis"`
	Strategy        string       `json:"strategy"`
	Subtasks        []db.Subtask `json:"subtasks"`
	EstimatedAgents int          `json:"estimated_agents"`
	Notes           string       `json:"notes"`
}

// QuestionsOutput is the JSON structure when the planner asks questions.
type QuestionsOutput struct {
	Type      string         `json:"type"`
	Questions []QuestionItem `json:"questions"`
}

// QuestionItem is a single question from the planner.
type QuestionItem struct {
	ID          string   `json:"id"`
	Text        string   `json:"text"`
	Context     string   `json:"context,omitempty"`
	Suggestions []string `json:"suggestions,omitempty"` // hint answers, user can type freely
	Options     []string `json:"options,omitempty"`     // multiple choice, user picks one
}

// ParsePlanOrQuestions attempts to parse the output from the planner agent.
// It returns either a PlanOutput or a QuestionsOutput, depending on the content.
// The output may be raw JSON, wrapped in markdown code blocks, or embedded
// in Claude's stream-json format.
func ParsePlanOrQuestions(output []byte) (*PlanOutput, *QuestionsOutput, error) {
	// First try to extract JSON from stream-json format (lines of JSON objects)
	extracted := extractFromStreamJSON(output)
	if extracted == nil {
		extracted = output
	}

	// Try parsing as questions first
	questions, err := tryParseQuestions(extracted)
	if err == nil && questions != nil {
		return nil, questions, nil
	}

	// Try parsing as plan
	plan, err := tryParsePlan(extracted)
	if err == nil && plan != nil {
		return plan, nil, nil
	}

	// Try extracting from markdown code blocks
	jsonBlock := extractJSONBlock(extracted)
	if jsonBlock != nil {
		questions, err = tryParseQuestions(jsonBlock)
		if err == nil && questions != nil {
			return nil, questions, nil
		}
		plan, err = tryParsePlan(jsonBlock)
		if err == nil && plan != nil {
			return plan, nil, nil
		}
	}

	return nil, nil, fmt.Errorf("failed to parse planner output as plan or questions")
}

// ParsePlanOutput extracts an ExecutionPlan from raw planner output.
func ParsePlanOutput(output []byte) (*PlanOutput, error) {
	plan, _, err := ParsePlanOrQuestions(output)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, fmt.Errorf("output contains questions, not a plan")
	}
	return plan, nil
}

// ValidatePlan checks that a PlanOutput is well-formed.
func ValidatePlan(plan *PlanOutput) error {
	if plan.Strategy != "parallel" && plan.Strategy != "sequential" {
		return fmt.Errorf("invalid strategy %q: must be 'parallel' or 'sequential'", plan.Strategy)
	}
	if len(plan.Subtasks) == 0 {
		return fmt.Errorf("plan must contain at least one subtask")
	}

	ids := make(map[string]bool)
	for _, st := range plan.Subtasks {
		if st.ID == "" {
			return fmt.Errorf("subtask missing id")
		}
		if ids[st.ID] {
			return fmt.Errorf("duplicate subtask id %q", st.ID)
		}
		ids[st.ID] = true

		if st.Name == "" {
			return fmt.Errorf("subtask %q missing name", st.ID)
		}
		if st.Prompt == "" {
			return fmt.Errorf("subtask %q missing prompt", st.ID)
		}

		for _, dep := range st.DependsOn {
			if !ids[dep] {
				// The dependency might be defined later; we check after all subtasks
			}
		}
	}

	// Second pass: validate all dependencies exist
	for _, st := range plan.Subtasks {
		for _, dep := range st.DependsOn {
			if !ids[dep] {
				return fmt.Errorf("subtask %q depends on unknown subtask %q", st.ID, dep)
			}
		}
	}

	return nil
}

// BuildResumePrompt creates a prompt for resuming execution after a pause.
func BuildResumePrompt(originalPrompt string, completedSubtasks []string, failedSubtasks []string) string {
	var b strings.Builder
	b.WriteString("You are resuming a previously paused task.\n\n")
	b.WriteString("## Original Task\n")
	b.WriteString(originalPrompt)
	b.WriteString("\n\n")

	if len(completedSubtasks) > 0 {
		b.WriteString("## Completed Subtasks\n")
		for _, id := range completedSubtasks {
			b.WriteString("- " + id + " (completed successfully)\n")
		}
		b.WriteString("\n")
	}

	if len(failedSubtasks) > 0 {
		b.WriteString("## Failed Subtasks (need retry)\n")
		for _, id := range failedSubtasks {
			b.WriteString("- " + id + " (failed, needs retry)\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("Continue from where execution was paused. Do not redo completed subtasks.\n")
	return b.String()
}

// SubtaskPromptOpts contains optional context to enrich the subtask prompt.
type SubtaskPromptOpts struct {
	DependencyContext string // Rich context from completed dependencies (from CommsService)
	BroadcastMessages string // Messages from other running agents
	RolePromptHint    string // Role-specific instructions from team template
	IsTeamExecution   bool   // Whether this is a multi-agent execution
	APIURL            string // Klaudio API URL for inter-agent messaging
	TaskID            string // Task ID for API calls
}

// BuildSubtaskPrompt creates the prompt for a subtask executor, adding context
// from completed dependencies.
func BuildSubtaskPrompt(subtask db.Subtask, allSubtasks []db.Subtask, taskPrompt string, opts ...SubtaskPromptOpts) string {
	var o SubtaskPromptOpts
	if len(opts) > 0 {
		o = opts[0]
	}

	var b strings.Builder
	b.WriteString("You are executing a subtask as part of a larger plan.\n\n")
	b.WriteString("## Overall Task\n")
	b.WriteString(taskPrompt)
	b.WriteString("\n\n")

	// Role-specific hint from team template
	if o.RolePromptHint != "" {
		b.WriteString("## Your Role\n")
		b.WriteString(o.RolePromptHint)
		b.WriteString("\n\n")
	}

	// Rich dependency context (from CommsService — includes actual work summaries)
	if o.DependencyContext != "" {
		b.WriteString(o.DependencyContext)
		b.WriteString("\n\n")
	} else if len(subtask.DependsOn) > 0 {
		// Fallback: basic dependency listing
		b.WriteString("## Completed Dependencies\n")
		for _, depID := range subtask.DependsOn {
			for _, dep := range allSubtasks {
				if dep.ID == depID {
					b.WriteString(fmt.Sprintf("- **%s** (%s): %s\n", dep.Name, dep.ID, dep.Description))
					break
				}
			}
		}
		b.WriteString("\n")
	}

	// Broadcast messages from other agents
	if o.BroadcastMessages != "" {
		b.WriteString(o.BroadcastMessages)
		b.WriteString("\n\n")
	}

	b.WriteString("## Your Subtask\n")
	b.WriteString(fmt.Sprintf("**ID**: %s\n", subtask.ID))
	b.WriteString(fmt.Sprintf("**Name**: %s\n", subtask.Name))
	b.WriteString(fmt.Sprintf("**Description**: %s\n\n", subtask.Description))
	b.WriteString("## Instructions\n")
	b.WriteString(subtask.Prompt)
	b.WriteString("\n")

	if len(subtask.FilesInvolved) > 0 {
		b.WriteString("\n## Files to work with\n")
		for _, f := range subtask.FilesInvolved {
			b.WriteString("- " + f + "\n")
		}
	}

	// Add comms instructions for team execution
	if o.IsTeamExecution && o.APIURL != "" {
		b.WriteString("\n")
		b.WriteString(APICommsInstructions(o.APIURL, o.TaskID, subtask.ID))
	}

	b.WriteString("\n")
	b.WriteString(workspaceFileRule)

	return b.String()
}

// tryParseQuestions attempts to parse data as a QuestionsOutput.
func tryParseQuestions(data []byte) (*QuestionsOutput, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, fmt.Errorf("empty data")
	}

	var q QuestionsOutput
	if err := json.Unmarshal(data, &q); err != nil {
		return nil, err
	}
	if q.Type == "questions" && len(q.Questions) > 0 {
		return &q, nil
	}
	return nil, fmt.Errorf("not a questions output")
}

// tryParsePlan attempts to parse data as a PlanOutput.
func tryParsePlan(data []byte) (*PlanOutput, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, fmt.Errorf("empty data")
	}

	var p PlanOutput
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	if len(p.Subtasks) > 0 {
		return &p, nil
	}
	return nil, fmt.Errorf("not a plan output")
}

// extractJSONBlock extracts JSON from a markdown code block (```json ... ```).
var jsonBlockRe = regexp.MustCompile("(?s)```(?:json)?\\s*\n?(\\{.*?\\})\n?\\s*```")

func extractJSONBlock(data []byte) []byte {
	matches := jsonBlockRe.FindSubmatch(data)
	if len(matches) >= 2 {
		return matches[1]
	}
	return nil
}

// extractFromStreamJSON tries to find a complete JSON object in Claude's
// stream-json output format. The stream consists of JSON lines; we look for
// text content that contains our JSON payload.
//
// Claude Code stream-json events come in several formats:
//
//	{"type":"system","subtype":"init",...}                                          → skip
//	{"type":"assistant","message":{"content":[{"type":"text","text":"..."}],...}}   → extract text
//	{"type":"content_block_delta","delta":{"type":"text_delta","text":"..."}}       → extract text
//	{"type":"result","result":"...","subtype":"success"}                            → extract result
//	Legacy: {"type":"...", "content":"...", "text":"..."}                           → extract content/text
func extractFromStreamJSON(data []byte) []byte {
	lines := bytes.Split(data, []byte("\n"))
	var textParts []string

	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		// Parse into a generic map to handle nested structures
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}

		// Skip system/init events
		var eventType string
		if t, ok := raw["type"]; ok {
			json.Unmarshal(t, &eventType)
		}
		if eventType == "system" {
			continue
		}

		// Strategy 1: "result" field (top-level string — final result event)
		if r, ok := raw["result"]; ok {
			var result string
			if json.Unmarshal(r, &result) == nil && result != "" {
				textParts = append(textParts, result)
				continue
			}
		}

		// Strategy 2: nested message.content[].text (assistant message events)
		if m, ok := raw["message"]; ok {
			var msg struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			}
			if json.Unmarshal(m, &msg) == nil {
				for _, block := range msg.Content {
					if block.Type == "text" && block.Text != "" {
						textParts = append(textParts, block.Text)
					}
				}
				continue
			}
		}

		// Strategy 3: delta.text (content_block_delta events)
		if d, ok := raw["delta"]; ok {
			var delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if json.Unmarshal(d, &delta) == nil && delta.Text != "" {
				textParts = append(textParts, delta.Text)
				continue
			}
		}

		// Strategy 4: legacy top-level content/text fields
		if c, ok := raw["content"]; ok {
			var content string
			if json.Unmarshal(c, &content) == nil && content != "" {
				textParts = append(textParts, content)
			}
		}
		if t, ok := raw["text"]; ok {
			var text string
			if json.Unmarshal(t, &text) == nil && text != "" {
				textParts = append(textParts, text)
			}
		}
	}

	if len(textParts) == 0 {
		return nil
	}

	combined := strings.Join(textParts, "")
	// Try to find a JSON object in the combined text
	start := strings.Index(combined, "{")
	if start < 0 {
		return nil
	}

	// Find the matching closing brace
	depth := 0
	for i := start; i < len(combined); i++ {
		switch combined[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return []byte(combined[start : i+1])
			}
		}
	}

	return nil
}
