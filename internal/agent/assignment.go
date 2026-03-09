package agent

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/klaudio-ai/klaudio/internal/db"
)

// Assignment records that a specific agent was assigned to a subtask.
type Assignment struct {
	AgentID   string
	SubtaskID string
}

// AssignPlan holds the data needed by the Assigner. It mirrors
// task.ExecutionPlan but lives here to avoid an import cycle.
type AssignPlan struct {
	Subtasks   []db.Subtask
	TaskPrompt string
}

// PromptBuilderFunc builds a prompt for a subtask given the full subtask list
// and the overall task prompt.
type PromptBuilderFunc func(subtask db.Subtask, allSubtasks []db.Subtask, taskPrompt string) string

// Assigner handles mapping ready subtasks to agents in the pool.
type Assigner struct {
	pool         *Pool
	promptBuilder PromptBuilderFunc
}

// NewAssigner creates a new Assigner backed by the given pool.
func NewAssigner(pool *Pool, pb PromptBuilderFunc) *Assigner {
	return &Assigner{pool: pool, promptBuilder: pb}
}

// AssignReady finds subtasks that are pending with all dependencies met,
// and spawns an agent for each one (up to pool limits).
func (a *Assigner) AssignReady(ctx context.Context, plan *AssignPlan, taskID, workspaceDir string, envVars map[string]string) ([]Assignment, error) {
	logger := slog.With("task_id", taskID, "component", "assigner")

	assignments := make([]Assignment, 0)

	for i := range plan.Subtasks {
		subtask := &plan.Subtasks[i]

		if subtask.Status != "pending" {
			continue
		}

		// Verify dependencies
		if !a.dependenciesMet(plan.Subtasks, *subtask) {
			continue
		}

		// Build the prompt
		prompt := a.promptBuilder(*subtask, plan.Subtasks, plan.TaskPrompt)

		role := AgentRole(subtask.AgentRole)
		if role == "" {
			role = RoleDeveloper
		}

		ag, err := a.pool.Spawn(ctx, SpawnOpts{
			TaskID:       taskID,
			SubtaskID:    subtask.ID,
			Role:         role,
			Prompt:       prompt,
			WorkspaceDir: workspaceDir,
			EnvVars:      envVars,
		})
		if err == ErrPoolFull || err == ErrTaskLimitReached {
			logger.Debug("pool limit reached, stopping assignment", "error", err)
			break
		}
		if err != nil {
			return assignments, fmt.Errorf("spawning agent for subtask %s: %w", subtask.ID, err)
		}

		subtask.Status = "running"
		subtask.AgentID = ag.ID

		assignments = append(assignments, Assignment{
			AgentID:   ag.ID,
			SubtaskID: subtask.ID,
		})

		logger.Info("assigned subtask to agent", "subtask_id", subtask.ID, "agent_id", ag.ID)
	}

	return assignments, nil
}

// dependenciesMet checks if all dependencies of a subtask are completed.
func (a *Assigner) dependenciesMet(subtasks []db.Subtask, subtask db.Subtask) bool {
	for _, depID := range subtask.DependsOn {
		found := false
		for _, s := range subtasks {
			if s.ID == depID {
				found = true
				if s.Status != "completed" {
					return false
				}
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
