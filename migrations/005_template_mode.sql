-- Add execution mode to team templates: "sequential" (default) or "collaborative"
ALTER TABLE team_templates ADD COLUMN mode TEXT NOT NULL DEFAULT 'sequential';
