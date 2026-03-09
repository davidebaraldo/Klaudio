-- ============================================
-- Migration 001: Core tables
-- ============================================

CREATE TABLE IF NOT EXISTS tasks (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    prompt          TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'created',
    repo_config     TEXT,              -- JSON: RepoConfig
    team_template   TEXT,              -- "solo" | "dev_review" | "full_team" | "custom"
    output_files    TEXT,              -- JSON: array di path attesi
    error           TEXT,
    has_state       BOOLEAN DEFAULT false,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    started_at      DATETIME,
    paused_at       DATETIME,
    completed_at    DATETIME
);

CREATE TABLE IF NOT EXISTS plans (
    id              TEXT PRIMARY KEY,
    task_id         TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    analysis        TEXT,
    strategy        TEXT NOT NULL DEFAULT 'sequential',
    subtasks        TEXT NOT NULL,      -- JSON: array di Subtask
    estimated_agents INTEGER DEFAULT 1,
    notes           TEXT,
    status          TEXT NOT NULL DEFAULT 'draft',
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS agents (
    id              TEXT PRIMARY KEY,
    task_id         TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    subtask_id      TEXT,
    container_id    TEXT,
    role            TEXT NOT NULL DEFAULT 'developer',
    status          TEXT NOT NULL DEFAULT 'created',
    exit_code       INTEGER,
    error           TEXT,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    started_at      DATETIME,
    completed_at    DATETIME
);

CREATE TABLE IF NOT EXISTS events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id         TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    agent_id        TEXT REFERENCES agents(id),
    type            TEXT NOT NULL,
    data            TEXT,              -- JSON payload
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS task_files (
    id              TEXT PRIMARY KEY,
    task_id         TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    direction       TEXT NOT NULL,     -- "input" | "output"
    path            TEXT NOT NULL,     -- Path su disco
    size            INTEGER,
    mime_type       TEXT,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS repo_templates (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    url             TEXT NOT NULL,
    default_branch  TEXT DEFAULT 'main',
    access_token    TEXT,              -- Encrypted
    auto_commit     BOOLEAN DEFAULT false,
    auto_push       BOOLEAN DEFAULT false,
    auto_pr         BOOLEAN DEFAULT false,
    pr_target       TEXT DEFAULT 'main',
    pr_reviewers    TEXT,              -- JSON: array di username
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS checkpoints (
    id              TEXT PRIMARY KEY,
    task_id         TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    state_dir       TEXT NOT NULL,     -- Path alla directory state
    plan_progress   TEXT NOT NULL,     -- JSON: PlanProgress
    agent_states    TEXT,              -- JSON: array di AgentState
    repo_state      TEXT,              -- JSON: RepoState
    resume_prompt   TEXT,
    size_bytes      INTEGER,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS planner_questions (
    id              TEXT PRIMARY KEY,
    task_id         TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    text            TEXT NOT NULL,
    answer          TEXT,
    status          TEXT NOT NULL DEFAULT 'pending',  -- "pending" | "answered"
    asked_at        DATETIME DEFAULT CURRENT_TIMESTAMP,
    answered_at     DATETIME
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_created ON tasks(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_plans_task ON plans(task_id);
CREATE INDEX IF NOT EXISTS idx_agents_task ON agents(task_id);
CREATE INDEX IF NOT EXISTS idx_agents_status ON agents(status);
CREATE INDEX IF NOT EXISTS idx_events_task ON events(task_id);
CREATE INDEX IF NOT EXISTS idx_events_type ON events(type);
CREATE INDEX IF NOT EXISTS idx_files_task ON task_files(task_id);
CREATE INDEX IF NOT EXISTS idx_checkpoints_task ON checkpoints(task_id);
CREATE INDEX IF NOT EXISTS idx_questions_task ON planner_questions(task_id);
CREATE INDEX IF NOT EXISTS idx_questions_status ON planner_questions(status);
