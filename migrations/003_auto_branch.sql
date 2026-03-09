-- Add auto_branch column to repo_templates
ALTER TABLE repo_templates ADD COLUMN auto_branch BOOLEAN DEFAULT false;
