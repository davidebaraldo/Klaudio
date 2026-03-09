-- Team templates: reusable team configurations for the planner
CREATE TABLE IF NOT EXISTS team_templates (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    max_agents INTEGER NOT NULL DEFAULT 3,
    review BOOLEAN NOT NULL DEFAULT false,
    roles TEXT NOT NULL DEFAULT '[]',        -- JSON array of role definitions
    is_default BOOLEAN NOT NULL DEFAULT false,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Agent messages: communication between agents during execution
CREATE TABLE IF NOT EXISTS agent_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    from_agent_id TEXT,              -- NULL = system/orchestrator
    from_subtask_id TEXT,
    to_agent_id TEXT,                -- NULL = broadcast
    to_subtask_id TEXT,
    msg_type TEXT NOT NULL DEFAULT 'message',  -- 'message', 'context', 'summary'
    content TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_agent_messages_task ON agent_messages(task_id);
CREATE INDEX IF NOT EXISTS idx_agent_messages_to ON agent_messages(task_id, to_subtask_id);
