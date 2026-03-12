package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// CreateTask inserts a new task into the database.
func (db *DB) CreateTask(ctx context.Context, task *Task) error {
	query := `
		INSERT INTO tasks (id, name, prompt, status, repo_config, team_template, output_files, has_state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.ExecContext(ctx, query,
		task.ID, task.Name, task.Prompt, task.Status,
		task.RepoConfig, task.TeamTemplate, task.OutputFiles,
		task.HasState, task.CreatedAt, task.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting task: %w", err)
	}
	return nil
}

// GetTask retrieves a task by ID.
func (db *DB) GetTask(ctx context.Context, id string) (*Task, error) {
	query := `
		SELECT id, name, prompt, status, repo_config, team_template, output_files,
		       error, has_state, created_at, updated_at, started_at, paused_at, completed_at
		FROM tasks WHERE id = ?
	`
	task := &Task{}
	err := db.QueryRowContext(ctx, query, id).Scan(
		&task.ID, &task.Name, &task.Prompt, &task.Status,
		&task.RepoConfig, &task.TeamTemplate, &task.OutputFiles,
		&task.Error, &task.HasState,
		&task.CreatedAt, &task.UpdatedAt,
		&task.StartedAt, &task.PausedAt, &task.CompletedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying task %s: %w", id, err)
	}
	return task, nil
}

// ListTasks returns tasks ordered by creation time descending with pagination.
func (db *DB) ListTasks(ctx context.Context, limit, offset int) ([]Task, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `
		SELECT id, name, prompt, status, repo_config, team_template, output_files,
		       error, has_state, created_at, updated_at, started_at, paused_at, completed_at
		FROM tasks ORDER BY created_at DESC LIMIT ? OFFSET ?
	`
	rows, err := db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listing tasks: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(
			&t.ID, &t.Name, &t.Prompt, &t.Status,
			&t.RepoConfig, &t.TeamTemplate, &t.OutputFiles,
			&t.Error, &t.HasState,
			&t.CreatedAt, &t.UpdatedAt,
			&t.StartedAt, &t.PausedAt, &t.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning task row: %w", err)
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating task rows: %w", err)
	}
	return tasks, nil
}

// UpdateTaskStatus changes the status of a task and updates the updated_at timestamp.
func (db *DB) UpdateTaskStatus(ctx context.Context, id string, status TaskStatus) error {
	now := time.Now().UTC()
	query := `UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?`
	result, err := db.ExecContext(ctx, query, status, now, id)
	if err != nil {
		return fmt.Errorf("updating task %s status to %s: %w", id, status, err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("task %s not found", id)
	}
	return nil
}

// UpdateTaskStarted sets the started_at timestamp and status to running.
func (db *DB) UpdateTaskStarted(ctx context.Context, id string) error {
	now := time.Now().UTC()
	query := `UPDATE tasks SET status = 'running', started_at = ?, updated_at = ? WHERE id = ?`
	_, err := db.ExecContext(ctx, query, now, now, id)
	if err != nil {
		return fmt.Errorf("updating task %s started: %w", id, err)
	}
	return nil
}

// UpdateTaskCompleted marks a task as completed.
func (db *DB) UpdateTaskCompleted(ctx context.Context, id string) error {
	now := time.Now().UTC()
	query := `UPDATE tasks SET status = 'completed', completed_at = ?, updated_at = ? WHERE id = ?`
	_, err := db.ExecContext(ctx, query, now, now, id)
	if err != nil {
		return fmt.Errorf("updating task %s completed: %w", id, err)
	}
	return nil
}

// UpdateTaskFailed marks a task as failed with an error message.
func (db *DB) UpdateTaskFailed(ctx context.Context, id string, errMsg string) error {
	now := time.Now().UTC()
	query := `UPDATE tasks SET status = 'failed', error = ?, completed_at = ?, updated_at = ? WHERE id = ?`
	_, err := db.ExecContext(ctx, query, errMsg, now, now, id)
	if err != nil {
		return fmt.Errorf("updating task %s failed: %w", id, err)
	}
	return nil
}

// DeleteTask removes a task and all associated data (cascading).
func (db *DB) DeleteTask(ctx context.Context, id string) error {
	result, err := db.ExecContext(ctx, "DELETE FROM tasks WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting task %s: %w", id, err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("task %s not found", id)
	}
	return nil
}

// CountTasks returns the total number of tasks, optionally filtered by status.
func (db *DB) CountTasks(ctx context.Context, status *TaskStatus) (int, error) {
	var count int
	var err error
	if status != nil {
		err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks WHERE status = ?", *status).Scan(&count)
	} else {
		err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks").Scan(&count)
	}
	if err != nil {
		return 0, fmt.Errorf("counting tasks: %w", err)
	}
	return count, nil
}

// CreateEvent inserts a new event.
func (db *DB) CreateEvent(ctx context.Context, event *Event) error {
	query := `INSERT INTO events (task_id, agent_id, type, data) VALUES (?, ?, ?, ?)`
	result, err := db.ExecContext(ctx, query, event.TaskID, event.AgentID, event.Type, event.Data)
	if err != nil {
		return fmt.Errorf("inserting event: %w", err)
	}
	id, _ := result.LastInsertId()
	event.ID = id
	return nil
}

// ListEventsByTask returns events for a task ordered by creation time.
func (db *DB) ListEventsByTask(ctx context.Context, taskID string, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `
		SELECT id, task_id, agent_id, type, data, created_at
		FROM events WHERE task_id = ? ORDER BY created_at DESC LIMIT ?
	`
	rows, err := db.QueryContext(ctx, query, taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("listing events for task %s: %w", taskID, err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.TaskID, &e.AgentID, &e.Type, &e.Data, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning event row: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// CreateAgent inserts a new agent record.
func (db *DB) CreateAgent(ctx context.Context, agent *Agent) error {
	query := `
		INSERT INTO agents (id, task_id, subtask_id, container_id, role, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.ExecContext(ctx, query,
		agent.ID, agent.TaskID, agent.SubtaskID, agent.ContainerID,
		agent.Role, agent.Status, agent.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting agent: %w", err)
	}
	return nil
}

// GetAgent retrieves an agent by ID.
func (db *DB) GetAgent(ctx context.Context, id string) (*Agent, error) {
	query := `
		SELECT id, task_id, subtask_id, container_id, role, status, exit_code, error,
		       created_at, started_at, completed_at
		FROM agents WHERE id = ?
	`
	a := &Agent{}
	err := db.QueryRowContext(ctx, query, id).Scan(
		&a.ID, &a.TaskID, &a.SubtaskID, &a.ContainerID,
		&a.Role, &a.Status, &a.ExitCode, &a.Error,
		&a.CreatedAt, &a.StartedAt, &a.CompletedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying agent %s: %w", id, err)
	}
	return a, nil
}

// UpdateAgentStatus changes the status of an agent.
func (db *DB) UpdateAgentStatus(ctx context.Context, id string, status string) error {
	_, err := db.ExecContext(ctx,
		"UPDATE agents SET status = ? WHERE id = ?", status, id,
	)
	if err != nil {
		return fmt.Errorf("updating agent %s status: %w", id, err)
	}
	return nil
}

// UpdateAgentCompleted marks an agent as completed with an exit code.
func (db *DB) UpdateAgentCompleted(ctx context.Context, id string, exitCode int, agentErr *string) error {
	now := time.Now().UTC()
	status := "completed"
	if exitCode != 0 {
		status = "failed"
	}
	_, err := db.ExecContext(ctx,
		"UPDATE agents SET status = ?, exit_code = ?, error = ?, completed_at = ? WHERE id = ?",
		status, exitCode, agentErr, now, id,
	)
	if err != nil {
		return fmt.Errorf("updating agent %s completed: %w", id, err)
	}
	return nil
}

// UpdateAgentContainer sets the container ID and marks the agent as running.
func (db *DB) UpdateAgentContainer(ctx context.Context, id string, containerID string) error {
	now := time.Now().UTC()
	_, err := db.ExecContext(ctx,
		"UPDATE agents SET container_id = ?, status = 'running', started_at = ? WHERE id = ?",
		containerID, now, id,
	)
	if err != nil {
		return fmt.Errorf("updating agent %s container: %w", id, err)
	}
	return nil
}

// ListAgentsByTask returns all agents for a given task.
func (db *DB) ListAgentsByTask(ctx context.Context, taskID string) ([]Agent, error) {
	query := `
		SELECT id, task_id, subtask_id, container_id, role, status, exit_code, error,
		       created_at, started_at, completed_at
		FROM agents WHERE task_id = ? ORDER BY created_at
	`
	rows, err := db.QueryContext(ctx, query, taskID)
	if err != nil {
		return nil, fmt.Errorf("listing agents for task %s: %w", taskID, err)
	}
	defer rows.Close()

	var agents []Agent
	for rows.Next() {
		var a Agent
		if err := rows.Scan(
			&a.ID, &a.TaskID, &a.SubtaskID, &a.ContainerID,
			&a.Role, &a.Status, &a.ExitCode, &a.Error,
			&a.CreatedAt, &a.StartedAt, &a.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning agent row: %w", err)
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

// CreatePlan inserts a new execution plan.
func (db *DB) CreatePlan(ctx context.Context, plan *Plan) error {
	query := `
		INSERT INTO plans (id, task_id, analysis, strategy, subtasks, estimated_agents, notes, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.ExecContext(ctx, query,
		plan.ID, plan.TaskID, plan.Analysis, plan.Strategy,
		plan.Subtasks, plan.EstimatedAgents, plan.Notes,
		plan.Status, plan.CreatedAt, plan.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting plan: %w", err)
	}
	return nil
}

// GetPlanByTask retrieves the plan for a given task.
func (db *DB) GetPlanByTask(ctx context.Context, taskID string) (*Plan, error) {
	query := `
		SELECT id, task_id, analysis, strategy, subtasks, estimated_agents, notes, status, created_at, updated_at
		FROM plans WHERE task_id = ?
	`
	p := &Plan{}
	err := db.QueryRowContext(ctx, query, taskID).Scan(
		&p.ID, &p.TaskID, &p.Analysis, &p.Strategy,
		&p.Subtasks, &p.EstimatedAgents, &p.Notes,
		&p.Status, &p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying plan for task %s: %w", taskID, err)
	}
	return p, nil
}

// UpdatePlanStatus updates a plan's status.
func (db *DB) UpdatePlanStatus(ctx context.Context, planID string, status string) error {
	_, err := db.ExecContext(ctx,
		"UPDATE plans SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		status, planID,
	)
	if err != nil {
		return fmt.Errorf("updating plan %s status: %w", planID, err)
	}
	return nil
}

// UpdatePlanSubtasks updates a plan's subtasks JSON.
func (db *DB) UpdatePlanSubtasks(ctx context.Context, planID string, subtasksJSON string) error {
	_, err := db.ExecContext(ctx,
		"UPDATE plans SET subtasks = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		subtasksJSON, planID,
	)
	if err != nil {
		return fmt.Errorf("updating plan %s subtasks: %w", planID, err)
	}
	return nil
}

// CreateTaskFile inserts a file record.
func (db *DB) CreateTaskFile(ctx context.Context, f *TaskFile) error {
	query := `
		INSERT INTO task_files (id, task_id, name, direction, path, size, mime_type, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.ExecContext(ctx, query,
		f.ID, f.TaskID, f.Name, f.Direction, f.Path, f.Size, f.MimeType, f.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting task file: %w", err)
	}
	return nil
}

// ListTaskFiles returns files for a given task, optionally filtered by direction.
func (db *DB) ListTaskFiles(ctx context.Context, taskID string, direction *string) ([]TaskFile, error) {
	var query string
	var args []interface{}
	if direction != nil {
		query = `SELECT id, task_id, name, direction, path, size, mime_type, created_at
		         FROM task_files WHERE task_id = ? AND direction = ? ORDER BY created_at`
		args = []interface{}{taskID, *direction}
	} else {
		query = `SELECT id, task_id, name, direction, path, size, mime_type, created_at
		         FROM task_files WHERE task_id = ? ORDER BY created_at`
		args = []interface{}{taskID}
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing files for task %s: %w", taskID, err)
	}
	defer rows.Close()

	var files []TaskFile
	for rows.Next() {
		var f TaskFile
		if err := rows.Scan(&f.ID, &f.TaskID, &f.Name, &f.Direction, &f.Path, &f.Size, &f.MimeType, &f.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning task file row: %w", err)
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// CreateCheckpoint inserts a new checkpoint.
func (db *DB) CreateCheckpoint(ctx context.Context, cp *Checkpoint) error {
	query := `
		INSERT INTO checkpoints (id, task_id, state_dir, plan_progress, agent_states, repo_state, resume_prompt, size_bytes, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.ExecContext(ctx, query,
		cp.ID, cp.TaskID, cp.StateDir, cp.PlanProgress,
		cp.AgentStates, cp.RepoState, cp.ResumePrompt,
		cp.SizeBytes, cp.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting checkpoint: %w", err)
	}
	return nil
}

// GetLatestCheckpoint returns the most recent checkpoint for a task.
func (db *DB) GetLatestCheckpoint(ctx context.Context, taskID string) (*Checkpoint, error) {
	query := `
		SELECT id, task_id, state_dir, plan_progress, agent_states, repo_state, resume_prompt, size_bytes, created_at
		FROM checkpoints WHERE task_id = ? ORDER BY created_at DESC LIMIT 1
	`
	cp := &Checkpoint{}
	err := db.QueryRowContext(ctx, query, taskID).Scan(
		&cp.ID, &cp.TaskID, &cp.StateDir, &cp.PlanProgress,
		&cp.AgentStates, &cp.RepoState, &cp.ResumePrompt,
		&cp.SizeBytes, &cp.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying latest checkpoint for task %s: %w", taskID, err)
	}
	return cp, nil
}

// ListCheckpointsByTask returns all checkpoints for a task, newest first.
func (db *DB) ListCheckpointsByTask(ctx context.Context, taskID string) ([]Checkpoint, error) {
	query := `
		SELECT id, task_id, state_dir, plan_progress, agent_states, repo_state, resume_prompt, size_bytes, created_at
		FROM checkpoints WHERE task_id = ? ORDER BY created_at DESC
	`
	rows, err := db.QueryContext(ctx, query, taskID)
	if err != nil {
		return nil, fmt.Errorf("listing checkpoints for task %s: %w", taskID, err)
	}
	defer rows.Close()

	var checkpoints []Checkpoint
	for rows.Next() {
		var cp Checkpoint
		if err := rows.Scan(
			&cp.ID, &cp.TaskID, &cp.StateDir, &cp.PlanProgress,
			&cp.AgentStates, &cp.RepoState, &cp.ResumePrompt,
			&cp.SizeBytes, &cp.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning checkpoint row: %w", err)
		}
		checkpoints = append(checkpoints, cp)
	}
	return checkpoints, rows.Err()
}

// DeleteCheckpoint removes a checkpoint by ID.
func (db *DB) DeleteCheckpoint(ctx context.Context, id string) error {
	_, err := db.ExecContext(ctx, "DELETE FROM checkpoints WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting checkpoint %s: %w", id, err)
	}
	return nil
}

// UpdateTaskHasState sets the has_state flag on a task.
func (db *DB) UpdateTaskHasState(ctx context.Context, id string, hasState bool) error {
	now := time.Now().UTC()
	_, err := db.ExecContext(ctx,
		"UPDATE tasks SET has_state = ?, updated_at = ? WHERE id = ?",
		hasState, now, id,
	)
	if err != nil {
		return fmt.Errorf("updating task %s has_state: %w", id, err)
	}
	return nil
}

// CreateRepoTemplate inserts a new repo template.
func (db *DB) CreateRepoTemplate(ctx context.Context, rt *RepoTemplate) error {
	query := `
		INSERT INTO repo_templates (id, name, url, default_branch, access_token, auto_branch, auto_commit, auto_push, auto_pr, pr_target, pr_reviewers, enable_memory, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.ExecContext(ctx, query,
		rt.ID, rt.Name, rt.URL, rt.DefaultBranch, rt.AccessToken,
		rt.AutoBranch, rt.AutoCommit, rt.AutoPush, rt.AutoPR, rt.PRTarget, rt.PRReviewers,
		rt.EnableMemory, rt.CreatedAt, rt.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting repo template: %w", err)
	}
	return nil
}

// ListRepoTemplates returns all repo templates.
func (db *DB) ListRepoTemplates(ctx context.Context) ([]RepoTemplate, error) {
	query := `
		SELECT id, name, url, default_branch, access_token, auto_branch, auto_commit, auto_push, auto_pr, pr_target, pr_reviewers, enable_memory, created_at, updated_at
		FROM repo_templates ORDER BY name
	`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("listing repo templates: %w", err)
	}
	defer rows.Close()

	var templates []RepoTemplate
	for rows.Next() {
		var rt RepoTemplate
		if err := rows.Scan(
			&rt.ID, &rt.Name, &rt.URL, &rt.DefaultBranch, &rt.AccessToken,
			&rt.AutoBranch, &rt.AutoCommit, &rt.AutoPush, &rt.AutoPR, &rt.PRTarget, &rt.PRReviewers,
			&rt.EnableMemory, &rt.CreatedAt, &rt.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning repo template row: %w", err)
		}
		templates = append(templates, rt)
	}
	return templates, rows.Err()
}

// GetRepoTemplate retrieves a repo template by ID.
func (db *DB) GetRepoTemplate(ctx context.Context, id string) (*RepoTemplate, error) {
	query := `
		SELECT id, name, url, default_branch, access_token, auto_branch, auto_commit, auto_push, auto_pr, pr_target, pr_reviewers, enable_memory, created_at, updated_at
		FROM repo_templates WHERE id = ?
	`
	rt := &RepoTemplate{}
	err := db.QueryRowContext(ctx, query, id).Scan(
		&rt.ID, &rt.Name, &rt.URL, &rt.DefaultBranch, &rt.AccessToken,
		&rt.AutoBranch, &rt.AutoCommit, &rt.AutoPush, &rt.AutoPR, &rt.PRTarget, &rt.PRReviewers,
		&rt.EnableMemory, &rt.CreatedAt, &rt.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying repo template %s: %w", id, err)
	}
	return rt, nil
}

// UpdateRepoTemplate updates an existing repo template.
func (db *DB) UpdateRepoTemplate(ctx context.Context, rt *RepoTemplate) error {
	now := time.Now().UTC()
	query := `
		UPDATE repo_templates
		SET name = ?, url = ?, default_branch = ?, access_token = ?,
		    auto_branch = ?, auto_commit = ?, auto_push = ?, auto_pr = ?, pr_target = ?, pr_reviewers = ?,
		    enable_memory = ?, updated_at = ?
		WHERE id = ?
	`
	result, err := db.ExecContext(ctx, query,
		rt.Name, rt.URL, rt.DefaultBranch, rt.AccessToken,
		rt.AutoBranch, rt.AutoCommit, rt.AutoPush, rt.AutoPR, rt.PRTarget, rt.PRReviewers,
		rt.EnableMemory, now, rt.ID,
	)
	if err != nil {
		return fmt.Errorf("updating repo template %s: %w", rt.ID, err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("repo template %s not found", rt.ID)
	}
	return nil
}

// DeleteRepoTemplate removes a repo template by ID.
func (db *DB) DeleteRepoTemplate(ctx context.Context, id string) error {
	result, err := db.ExecContext(ctx, "DELETE FROM repo_templates WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting repo template %s: %w", id, err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("repo template %s not found", id)
	}
	return nil
}

// CreatePlannerQuestion inserts a new planner question.
func (db *DB) CreatePlannerQuestion(ctx context.Context, q *PlannerQuestion) error {
	query := `INSERT INTO planner_questions (id, task_id, text, status, suggestions, options, asked_at) VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err := db.ExecContext(ctx, query, q.ID, q.TaskID, q.Text, q.Status, q.Suggestions, q.Options, q.AskedAt)
	if err != nil {
		return fmt.Errorf("inserting planner question: %w", err)
	}
	return nil
}

// AnswerPlannerQuestion records an answer to a planner question.
func (db *DB) AnswerPlannerQuestion(ctx context.Context, id string, answer string) error {
	now := time.Now().UTC()
	_, err := db.ExecContext(ctx,
		"UPDATE planner_questions SET answer = ?, status = 'answered', answered_at = ? WHERE id = ?",
		answer, now, id,
	)
	if err != nil {
		return fmt.Errorf("answering planner question %s: %w", id, err)
	}
	return nil
}

// ---- Team Templates ----

// CreateTeamTemplate inserts a new team template.
func (db *DB) CreateTeamTemplate(ctx context.Context, tt *TeamTemplate) error {
	if tt.Mode == "" {
		tt.Mode = "sequential"
	}
	query := `
		INSERT INTO team_templates (id, name, description, max_agents, review, roles, mode, is_default, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.ExecContext(ctx, query,
		tt.ID, tt.Name, tt.Description, tt.MaxAgents, tt.Review, tt.Roles,
		tt.Mode, tt.IsDefault, tt.CreatedAt, tt.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting team template: %w", err)
	}
	return nil
}

// ListTeamTemplates returns all team templates.
func (db *DB) ListTeamTemplates(ctx context.Context) ([]TeamTemplate, error) {
	query := `
		SELECT id, name, description, max_agents, review, roles, mode, is_default, created_at, updated_at
		FROM team_templates ORDER BY name
	`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("listing team templates: %w", err)
	}
	defer rows.Close()

	var templates []TeamTemplate
	for rows.Next() {
		var tt TeamTemplate
		if err := rows.Scan(
			&tt.ID, &tt.Name, &tt.Description, &tt.MaxAgents, &tt.Review, &tt.Roles,
			&tt.Mode, &tt.IsDefault, &tt.CreatedAt, &tt.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning team template row: %w", err)
		}
		if tt.Mode == "" {
			tt.Mode = "sequential"
		}
		templates = append(templates, tt)
	}
	return templates, rows.Err()
}

// GetTeamTemplate retrieves a team template by ID.
func (db *DB) GetTeamTemplate(ctx context.Context, id string) (*TeamTemplate, error) {
	query := `
		SELECT id, name, description, max_agents, review, roles, mode, is_default, created_at, updated_at
		FROM team_templates WHERE id = ?
	`
	tt := &TeamTemplate{}
	err := db.QueryRowContext(ctx, query, id).Scan(
		&tt.ID, &tt.Name, &tt.Description, &tt.MaxAgents, &tt.Review, &tt.Roles,
		&tt.Mode, &tt.IsDefault, &tt.CreatedAt, &tt.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying team template %s: %w", id, err)
	}
	return tt, nil
}

// UpdateTeamTemplate updates an existing team template.
func (db *DB) UpdateTeamTemplate(ctx context.Context, tt *TeamTemplate) error {
	if tt.Mode == "" {
		tt.Mode = "sequential"
	}
	now := time.Now().UTC()
	query := `
		UPDATE team_templates
		SET name = ?, description = ?, max_agents = ?, review = ?, roles = ?, mode = ?, is_default = ?, updated_at = ?
		WHERE id = ?
	`
	result, err := db.ExecContext(ctx, query,
		tt.Name, tt.Description, tt.MaxAgents, tt.Review, tt.Roles, tt.Mode, tt.IsDefault, now, tt.ID,
	)
	if err != nil {
		return fmt.Errorf("updating team template %s: %w", tt.ID, err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("team template %s not found", tt.ID)
	}
	return nil
}

// DeleteTeamTemplate removes a team template by ID.
func (db *DB) DeleteTeamTemplate(ctx context.Context, id string) error {
	result, err := db.ExecContext(ctx, "DELETE FROM team_templates WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting team template %s: %w", id, err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("team template %s not found", id)
	}
	return nil
}

// ---- Agent Messages ----

// CreateAgentMessage inserts a new agent message.
func (db *DB) CreateAgentMessage(ctx context.Context, msg *AgentMessage) error {
	query := `
		INSERT INTO agent_messages (task_id, from_agent_id, from_subtask_id, to_agent_id, to_subtask_id, msg_type, content, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	result, err := db.ExecContext(ctx, query,
		msg.TaskID, msg.FromAgentID, msg.FromSubtaskID,
		msg.ToAgentID, msg.ToSubtaskID, msg.MsgType, msg.Content, msg.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting agent message: %w", err)
	}
	id, _ := result.LastInsertId()
	msg.ID = id
	return nil
}

// ListAgentMessages returns messages for a task, ordered by creation time.
func (db *DB) ListAgentMessages(ctx context.Context, taskID string, limit int) ([]AgentMessage, error) {
	if limit <= 0 {
		limit = 200
	}
	query := `
		SELECT id, task_id, from_agent_id, from_subtask_id, to_agent_id, to_subtask_id, msg_type, content, created_at
		FROM agent_messages WHERE task_id = ? ORDER BY created_at LIMIT ?
	`
	rows, err := db.QueryContext(ctx, query, taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("listing agent messages for task %s: %w", taskID, err)
	}
	defer rows.Close()

	var messages []AgentMessage
	for rows.Next() {
		var m AgentMessage
		if err := rows.Scan(
			&m.ID, &m.TaskID, &m.FromAgentID, &m.FromSubtaskID,
			&m.ToAgentID, &m.ToSubtaskID, &m.MsgType, &m.Content, &m.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning agent message row: %w", err)
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

// ListAgentMessagesAfterID returns messages for a task with ID greater than afterID.
func (db *DB) ListAgentMessagesAfterID(ctx context.Context, taskID string, afterID int64) ([]AgentMessage, error) {
	query := `
		SELECT id, task_id, from_agent_id, from_subtask_id, to_agent_id, to_subtask_id, msg_type, content, created_at
		FROM agent_messages WHERE task_id = ? AND id > ? ORDER BY id
	`
	rows, err := db.QueryContext(ctx, query, taskID, afterID)
	if err != nil {
		return nil, fmt.Errorf("listing agent messages after ID %d for task %s: %w", afterID, taskID, err)
	}
	defer rows.Close()

	var messages []AgentMessage
	for rows.Next() {
		var m AgentMessage
		if err := rows.Scan(
			&m.ID, &m.TaskID, &m.FromAgentID, &m.FromSubtaskID,
			&m.ToAgentID, &m.ToSubtaskID, &m.MsgType, &m.Content, &m.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning agent message row: %w", err)
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

// ---- Repo Memory ----

// CreateRepoMemory inserts a new repo memory record.
func (db *DB) CreateRepoMemory(ctx context.Context, rm *RepoMemory) error {
	query := `
		INSERT INTO repo_memories (id, repo_template_id, branch, commit_hash, content, file_tree, languages, frameworks, key_files, dependencies, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.ExecContext(ctx, query,
		rm.ID, rm.RepoTemplateID, rm.Branch, rm.CommitHash, rm.Content,
		rm.FileTree, rm.Languages, rm.Frameworks, rm.KeyFiles, rm.Dependencies,
		rm.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting repo memory: %w", err)
	}
	return nil
}

// GetRepoMemory retrieves the latest repo memory for a template and branch.
func (db *DB) GetRepoMemory(ctx context.Context, templateID, branch string) (*RepoMemory, error) {
	query := `
		SELECT id, repo_template_id, branch, commit_hash, content, file_tree, languages, frameworks, key_files, dependencies, created_at
		FROM repo_memories WHERE repo_template_id = ? AND branch = ?
		ORDER BY created_at DESC LIMIT 1
	`
	rm := &RepoMemory{}
	err := db.QueryRowContext(ctx, query, templateID, branch).Scan(
		&rm.ID, &rm.RepoTemplateID, &rm.Branch, &rm.CommitHash, &rm.Content,
		&rm.FileTree, &rm.Languages, &rm.Frameworks, &rm.KeyFiles, &rm.Dependencies,
		&rm.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying repo memory for template %s branch %s: %w", templateID, branch, err)
	}
	return rm, nil
}

// GetRepoMemoryByCommit retrieves a repo memory for a specific template, branch, and commit.
func (db *DB) GetRepoMemoryByCommit(ctx context.Context, templateID, branch, commitHash string) (*RepoMemory, error) {
	query := `
		SELECT id, repo_template_id, branch, commit_hash, content, file_tree, languages, frameworks, key_files, dependencies, created_at
		FROM repo_memories WHERE repo_template_id = ? AND branch = ? AND commit_hash = ?
	`
	rm := &RepoMemory{}
	err := db.QueryRowContext(ctx, query, templateID, branch, commitHash).Scan(
		&rm.ID, &rm.RepoTemplateID, &rm.Branch, &rm.CommitHash, &rm.Content,
		&rm.FileTree, &rm.Languages, &rm.Frameworks, &rm.KeyFiles, &rm.Dependencies,
		&rm.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying repo memory by commit: %w", err)
	}
	return rm, nil
}

// DeleteRepoMemoriesByTemplate removes all repo memories for a template.
func (db *DB) DeleteRepoMemoriesByTemplate(ctx context.Context, templateID string) error {
	_, err := db.ExecContext(ctx, "DELETE FROM repo_memories WHERE repo_template_id = ?", templateID)
	if err != nil {
		return fmt.Errorf("deleting repo memories for template %s: %w", templateID, err)
	}
	return nil
}

// ListPlannerQuestions returns questions for a task.
func (db *DB) ListPlannerQuestions(ctx context.Context, taskID string) ([]PlannerQuestion, error) {
	query := `
		SELECT id, task_id, text, answer, status, suggestions, options, asked_at, answered_at
		FROM planner_questions WHERE task_id = ? ORDER BY asked_at
	`
	rows, err := db.QueryContext(ctx, query, taskID)
	if err != nil {
		return nil, fmt.Errorf("listing questions for task %s: %w", taskID, err)
	}
	defer rows.Close()

	var questions []PlannerQuestion
	for rows.Next() {
		var q PlannerQuestion
		if err := rows.Scan(&q.ID, &q.TaskID, &q.Text, &q.Answer, &q.Status, &q.Suggestions, &q.Options, &q.AskedAt, &q.AnsweredAt); err != nil {
			return nil, fmt.Errorf("scanning planner question row: %w", err)
		}
		questions = append(questions, q)
	}
	return questions, rows.Err()
}
