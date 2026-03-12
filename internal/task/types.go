package task

import "github.com/klaudio-ai/klaudio/internal/db"

// ExecutionPlan holds the parsed subtask list along with metadata needed
// during orchestration. It is built from a db.Plan at execution time.
type ExecutionPlan struct {
	PlanID     string
	Strategy   string // "parallel" or "sequential"
	Subtasks   []db.Subtask
	TaskPrompt string // original task prompt, used to build agent prompts
	Mode       string // "sequential" (default, DAG-based) or "collaborative" (manager + concurrent workers)
	RepoMemory string // cached repo analysis content (optional, from repo memory)
}
