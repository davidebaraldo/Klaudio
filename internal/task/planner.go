package task

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/klaudio-ai/klaudio/internal/config"
	"github.com/klaudio-ai/klaudio/internal/db"
	"github.com/klaudio-ai/klaudio/internal/docker"
	"github.com/klaudio-ai/klaudio/internal/stream"
)

// Planner launches a read-only Claude Code container to analyze a task and
// produce an execution plan. It supports a Q&A flow where the planner can
// ask clarification questions before producing the final plan.
type Planner struct {
	docker    *docker.Manager
	db        *db.DB
	cfg       *config.Config
	streamHub *stream.Hub
}

// NewPlanner creates a new Planner.
func NewPlanner(dockerMgr *docker.Manager, database *db.DB, cfg *config.Config, hub *stream.Hub) *Planner {
	return &Planner{
		docker:    dockerMgr,
		db:        database,
		cfg:       cfg,
		streamHub: hub,
	}
}

// PlanResult holds the outcome of a planning run.
type PlanResult struct {
	Plan      *PlanOutput
	Questions *QuestionsOutput
	Error     error
}

// plannerPromptTemplate is the system prompt for the planner agent.
const plannerPromptTemplate = `You are a software development planner. Your role is ONLY to analyze, never modify.

RULES:
- Do NOT modify any file. Use only read operations.
- If something is unclear, you MUST ask questions before producing the plan.
- Analyze the code structure, patterns, dependencies, and existing tests.
- You can ONLY access files inside the workspace (/home/agent/workspace). If the task references files or paths outside the project folder, you cannot read or analyze them. In that case, note this limitation in the plan and ask the user to provide the files as input.
- All output files listed in files_involved MUST be paths inside the workspace. Agents cannot create files outside of it — the platform only shows files within the workspace to the user.

Analyze the following task and produce a structured execution plan.

## Task
%s

## Input files available
%s

## Phase 1: Questions (if needed)
If something is unclear about the task, the codebase, or the files, BEFORE producing the plan
emit a JSON with your questions. Each question can be:
- Free text: just "text" (user types their answer)
- Free text with suggestions: "text" + "suggestions" array (user can click a suggestion or type freely)
- Multiple choice: "text" + "options" array (user picks one option, or types a custom answer)

Use "suggestions" when you want to hint at common answers but allow free input.
Use "options" when there are a clear set of valid choices.

Example:
{"type": "questions", "questions": [
  {"id": "q-1", "text": "What style do you prefer?", "context": "Need to know the visual direction", "options": ["Minimal", "Modern", "Retro", "Playful"]},
  {"id": "q-2", "text": "Target format?", "suggestions": ["SVG", "PNG", "Both SVG and PNG"]},
  {"id": "q-3", "text": "Any specific color preference?", "context": "For the palette"}
]}

Then WAIT for answers. You will receive them as additional input. Only after receiving all
answers, proceed to Phase 2.

If everything is clear, skip directly to Phase 2.

## Phase 2: Plan
Produce a plan in JSON format with the following structure:
- Task analysis
- Ordered list of subtasks
- For each subtask: description, files involved, dependencies on other subtasks
- Complexity estimate (low/medium/high)
- Suggestion whether parallel agents are needed or sequential is sufficient

Respond ONLY with the JSON, no other text.

## Expected JSON Schema
{
  "analysis": "Brief task analysis",
  "strategy": "parallel" | "sequential",
  "subtasks": [
    {
      "id": "sub-1",
      "name": "Subtask name",
      "description": "What the executor agent must do",
      "prompt": "Exact prompt to pass to the executor agent",
      "depends_on": [],
      "files_involved": ["path/to/file.go"],
      "complexity": "low" | "medium" | "high",
      "agent_role": "developer" | "reviewer" | "tester"
    }
  ],
  "estimated_agents": 1,
  "notes": "Additional notes for the user"
}`

// Run launches the planner container for a task and waits for results.
// It handles the Q&A flow: if the planner asks questions, they are persisted
// to the database and the method returns with Questions set. The caller
// (TaskManager) is responsible for re-running the planner after answers arrive.
func (p *Planner) Run(ctx context.Context, task *db.Task, additionalContext string) (*PlanResult, error) {
	logger := slog.With("task_id", task.ID, "component", "planner")

	// Build the prompt
	inputFiles := p.listInputFiles(task.ID)
	prompt := fmt.Sprintf(plannerPromptTemplate, task.Prompt, inputFiles)

	// Add repo context if available
	if task.RepoConfig != nil && *task.RepoConfig != "" {
		var rc db.RepoConfig
		if err := json.Unmarshal([]byte(*task.RepoConfig), &rc); err == nil {
			prompt += "\n\n## Repository\n"
			prompt += fmt.Sprintf("The workspace contains a cloned Git repository from: %s\n", rc.URL)
			prompt += fmt.Sprintf("Working branch: klaudio/%s\n", task.ID[:8])
			prompt += "The repository code is available in /home/agent/workspace. Analyze the existing code structure when planning.\n"

			// Inject cached repo memory if available
			if rc.EnableMemory && rc.RepoTemplateID != "" {
				memory, memErr := p.db.GetRepoMemory(ctx, rc.RepoTemplateID, rc.Branch)
				if memErr == nil && memory != nil {
					prompt += "\n## Cached Repository Analysis (commit " + memory.CommitHash[:8] + ")\n"
					prompt += "The following analysis was generated automatically. Use it to speed up your understanding of the codebase.\n\n"
					prompt += memory.Content
				}
			}
		}
	}

	// Inject team template constraints if specified
	if task.TeamTemplate != nil && *task.TeamTemplate != "" {
		teamSection := p.buildTeamSection(ctx, *task.TeamTemplate)
		if teamSection != "" {
			prompt += "\n\n" + teamSection
		}
	}

	if additionalContext != "" {
		prompt += "\n\n## Additional Context\n" + additionalContext
	}

	// Append any previously answered questions
	questions, err := p.db.ListPlannerQuestions(ctx, task.ID)
	if err != nil {
		return nil, fmt.Errorf("listing planner questions: %w", err)
	}
	if len(questions) > 0 {
		var answered []string
		for _, q := range questions {
			if q.Status == "answered" && q.Answer != nil {
				answered = append(answered, fmt.Sprintf("Q: %s\nA: %s", q.Text, *q.Answer))
			}
		}
		if len(answered) > 0 {
			prompt += "\n\n## Answers to previous questions\n" + strings.Join(answered, "\n\n")
			prompt += "\n\nAll questions have been answered. Now produce the execution plan."
		}
	}

	// Create workspace directory
	workspaceDir := filepath.Join(p.cfg.Storage.DataDir, "workspaces", task.ID)
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating workspace directory: %w", err)
	}

	absWorkspace, err := filepath.Abs(workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("resolving workspace path: %w", err)
	}

	// Create agent record for the planner so it shows up in the UI
	plannerAgentID := uuid.New().String()
	plannerAgent := &db.Agent{
		ID:        plannerAgentID,
		TaskID:    task.ID,
		Role:      "planner",
		Status:    "running",
		CreatedAt: time.Now().UTC(),
	}
	startedAt := time.Now().UTC()
	plannerAgent.StartedAt = &startedAt
	if err := p.db.CreateAgent(ctx, plannerAgent); err != nil {
		logger.Warn("failed to create planner agent record", "error", err)
	}

	// Register agent stream for real-time output
	var agentStream *stream.AgentStream
	if p.streamHub != nil {
		agentStream = p.streamHub.RegisterAgent(plannerAgentID, task.ID)
	}

	// Create planner container — read-only workspace
	containerName := fmt.Sprintf("klaudio-planner-%s", task.ID[:8])
	containerID, createErr := p.docker.CreateContainer(ctx, docker.ContainerOpts{
		Name:   containerName,
		Prompt: prompt,
		Volumes: []docker.VolumeMount{
			{
				HostPath:      absWorkspace,
				ContainerPath: "/home/agent/workspace",
				ReadOnly:      true, // CRITICAL: planner is read-only
			},
		},
	})
	if createErr != nil {
		if p.streamHub != nil {
			p.streamHub.UnregisterAgent(plannerAgentID)
		}
		return nil, fmt.Errorf("creating planner container: %w", createErr)
	}

	// Update agent record with container ID
	plannerAgent.ContainerID = &containerID
	_ = p.db.UpdateAgentStatus(ctx, plannerAgentID, "running")

	logger = logger.With("container_id", containerID, "agent_id", plannerAgentID)

	// Attach BEFORE starting — with Tty:true, we must attach first to not miss output
	if agentStream != nil {
		reader, _, attachErr := p.docker.AttachContainer(ctx, containerID)
		if attachErr != nil {
			logger.Warn("failed to attach to planner container", "error", attachErr)
		} else {
			logger.Info("attached to planner container for streaming")
			go func() {
				totalBytes := 0
				buf := make([]byte, 4096)
				for {
					n, readErr := reader.Read(buf)
					if n > 0 {
						totalBytes += n
						data := make([]byte, n)
						copy(data, buf[:n])
						select {
						case agentStream.OutputCh <- data:
						default:
							logger.Warn("dropping planner output, channel full")
						}
					}
					if readErr != nil {
						logger.Info("planner attach reader ended", "total_bytes", totalBytes, "error", readErr)
						return
					}
				}
			}()
		}
	}

	// Start the container
	if err := p.docker.StartContainer(ctx, containerID); err != nil {
		p.docker.RemoveContainer(ctx, containerID)
		if p.streamHub != nil {
			p.streamHub.UnregisterAgent(plannerAgentID)
		}
		return nil, fmt.Errorf("starting planner container: %w", err)
	}

	logger.Info("planner container started")

	// Wait for the planner to finish
	exitCh, errCh := p.docker.WaitContainer(ctx, containerID)
	exitCode := <-exitCh
	waitErr := <-errCh

	logger.Info("planner container finished", "exit_code", exitCode)

	// Unregister stream
	if p.streamHub != nil {
		p.streamHub.UnregisterAgent(plannerAgentID)
	}

	// Update agent record
	_ = p.db.UpdateAgentCompleted(ctx, plannerAgentID, int(exitCode), nil)

	// Collect output
	logsReader, logErr := p.docker.ContainerLogs(ctx, containerID)
	var output string
	if logErr == nil {
		outputBytes, _ := io.ReadAll(logsReader)
		logsReader.Close()
		output = stripDockerLogHeaders(outputBytes)
		logger.Info("planner raw output", "length", len(output), "first_500", truncate(output, 500))
	} else {
		logger.Error("failed to get planner logs", "error", logErr)
	}

	// Clean up container
	if removeErr := p.docker.RemoveContainer(ctx, containerID); removeErr != nil {
		logger.Warn("failed to remove planner container", "error", removeErr)
	}

	// Check for errors
	if waitErr != nil {
		return &PlanResult{Error: fmt.Errorf("planner container error: %w", waitErr)}, nil
	}
	if exitCode != 0 {
		return &PlanResult{Error: fmt.Errorf("planner exited with code %d: %s", exitCode, output)}, nil
	}

	if output == "" {
		return &PlanResult{Error: fmt.Errorf("planner produced empty output")}, nil
	}

	// Parse the output
	plan, questionsOut, parseErr := ParsePlanOrQuestions([]byte(output))
	if parseErr != nil {
		logger.Error("failed to parse planner output", "error", parseErr, "raw_output_length", len(output), "first_1000", truncate(output, 1000))
		return &PlanResult{Error: fmt.Errorf("parsing planner output: %w (raw: %s)", parseErr, truncate(output, 500))}, nil
	}

	if questionsOut != nil {
		// Persist questions to DB
		// Use task-scoped IDs to avoid UNIQUE constraint collisions across tasks
		for i, q := range questionsOut.Questions {
			uniqueID := task.ID[:8] + "-" + q.ID
			questionsOut.Questions[i].ID = uniqueID
			pq := &db.PlannerQuestion{
				ID:      uniqueID,
				TaskID:  task.ID,
				Text:    q.Text,
				Status:  "pending",
				AskedAt: time.Now().UTC(),
			}
			if len(q.Suggestions) > 0 {
				sugJSON, _ := json.Marshal(q.Suggestions)
				s := string(sugJSON)
				pq.Suggestions = &s
			}
			if len(q.Options) > 0 {
				optJSON, _ := json.Marshal(q.Options)
				s := string(optJSON)
				pq.Options = &s
			}
			if err := p.db.CreatePlannerQuestion(ctx, pq); err != nil {
				logger.Error("failed to persist planner question", "question_id", uniqueID, "error", err)
			}
		}
		return &PlanResult{Questions: questionsOut}, nil
	}

	// Validate the plan
	if err := ValidatePlan(plan); err != nil {
		return &PlanResult{Error: fmt.Errorf("invalid plan: %w", err)}, nil
	}

	return &PlanResult{Plan: plan}, nil
}

// SendAnswerToPlanner re-runs the planner with previously answered questions
// injected into the prompt. This is called after the user answers all pending
// questions.
func (p *Planner) SendAnswerToPlanner(ctx context.Context, task *db.Task) (*PlanResult, error) {
	return p.Run(ctx, task, "")
}

// listInputFiles returns a formatted list of input files available for the task.
func (p *Planner) listInputFiles(taskID string) string {
	inputDir := filepath.Join(p.cfg.Storage.FilesDir, taskID, "input")
	entries, err := os.ReadDir(inputDir)
	if err != nil {
		return "(no input files)"
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, e.Name())
		}
	}
	if len(files) == 0 {
		return "(no input files)"
	}
	return strings.Join(files, "\n")
}

// PersistPlan saves a PlanOutput to the database as a db.Plan.
func (p *Planner) PersistPlan(ctx context.Context, taskID string, planOutput *PlanOutput) (*db.Plan, error) {
	// Set initial status for all subtasks
	for i := range planOutput.Subtasks {
		planOutput.Subtasks[i].Status = "pending"
	}

	subtasksJSON, err := json.Marshal(planOutput.Subtasks)
	if err != nil {
		return nil, fmt.Errorf("marshaling subtasks: %w", err)
	}

	now := time.Now().UTC()
	plan := &db.Plan{
		ID:              uuid.New().String(),
		TaskID:          taskID,
		Analysis:        strPtr(planOutput.Analysis),
		Strategy:        planOutput.Strategy,
		Subtasks:        string(subtasksJSON),
		EstimatedAgents: planOutput.EstimatedAgents,
		Notes:           strPtr(planOutput.Notes),
		Status:          "draft",
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := p.db.CreatePlan(ctx, plan); err != nil {
		return nil, fmt.Errorf("persisting plan: %w", err)
	}

	return plan, nil
}

// stripDockerLogHeaders removes the 8-byte Docker log header from each log line.
// With Tty: true, Docker returns raw output without headers — detect and skip stripping.
func stripDockerLogHeaders(data []byte) string {
	if len(data) < 8 {
		return string(data)
	}
	// Docker multiplexed log headers start with stream type: 0=stdin, 1=stdout, 2=stderr
	// and bytes 1-3 are always zero. If that pattern isn't present, it's raw TTY output.
	if data[0] > 2 || data[1] != 0 || data[2] != 0 || data[3] != 0 {
		return string(data)
	}

	var result []byte
	for len(data) >= 8 {
		// Verify this still looks like a Docker log header
		if data[0] > 2 || data[1] != 0 || data[2] != 0 || data[3] != 0 {
			result = append(result, data...)
			break
		}
		size := int(data[4])<<24 | int(data[5])<<16 | int(data[6])<<8 | int(data[7])
		data = data[8:]
		if size > len(data) {
			size = len(data)
		}
		result = append(result, data[:size]...)
		data = data[size:]
	}
	if len(result) == 0 {
		return string(data)
	}
	return string(result)
}

// buildTeamSection creates the team composition constraints section for the planner prompt.
func (p *Planner) buildTeamSection(ctx context.Context, templateID string) string {
	tt, err := p.db.GetTeamTemplate(ctx, templateID)
	if err != nil || tt == nil {
		return ""
	}

	var roles []db.TeamRole
	if err := json.Unmarshal([]byte(tt.Roles), &roles); err != nil {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Team Composition\n\n")
	b.WriteString(fmt.Sprintf("Team template: **%s** — %s\n\n", tt.Name, tt.Description))

	// Indicate execution mode
	if tt.Mode == "collaborative" {
		b.WriteString("**Execution mode: COLLABORATIVE**\n")
		b.WriteString("All worker agents will run SIMULTANEOUSLY. A team manager agent will coordinate them.\n")
		b.WriteString("Dependencies (`depends_on`) are treated as soft guidance — workers handle coordination via messaging.\n")
		b.WriteString("Design subtasks that can run in parallel. Avoid strict sequential dependencies.\n\n")
	} else {
		b.WriteString("**Execution mode: Sequential (DAG)**\n")
		b.WriteString("Agents run in dependency order. Use `depends_on` to enforce execution order.\n\n")
	}

	b.WriteString("You MUST assign each subtask to one of these roles using the `agent_role` field:\n\n")

	for _, r := range roles {
		maxStr := ""
		if r.MaxInstances > 0 {
			maxStr = fmt.Sprintf(" (max %d)", r.MaxInstances)
		}
		b.WriteString(fmt.Sprintf("- **%s**%s: %s", r.Name, maxStr, r.Description))
		if r.RunLast {
			b.WriteString(" [runs after all other subtasks complete]")
		}
		b.WriteString("\n")
	}

	b.WriteString(fmt.Sprintf("\nDo not exceed **%d total agents** running in parallel.\n", tt.MaxAgents))

	if tt.Review {
		b.WriteString("\nA **reviewer agent** will run automatically after all subtasks complete to verify consistency.\n")
		b.WriteString("You do NOT need to include a reviewer subtask unless you want custom review logic.\n")
	}

	return b.String()
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
