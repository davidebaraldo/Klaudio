-- Repo memory: cached codebase analysis per repo template + branch + commit.
-- Optional feature, enabled per template via enable_memory flag.

CREATE TABLE IF NOT EXISTS repo_memories (
    id               TEXT PRIMARY KEY,
    repo_template_id TEXT NOT NULL REFERENCES repo_templates(id) ON DELETE CASCADE,
    branch           TEXT NOT NULL,
    commit_hash      TEXT NOT NULL,
    content          TEXT NOT NULL,       -- Markdown summary of the codebase
    file_tree        TEXT,                -- JSON: directory/file structure
    languages        TEXT,                -- JSON: detected languages with file counts
    frameworks       TEXT,                -- JSON: detected frameworks
    key_files        TEXT,                -- JSON: important files found
    dependencies     TEXT,                -- JSON: main dependencies
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(repo_template_id, branch, commit_hash)
);

-- Enable memory per repo template (opt-in).
ALTER TABLE repo_templates ADD COLUMN enable_memory BOOLEAN NOT NULL DEFAULT 0;
