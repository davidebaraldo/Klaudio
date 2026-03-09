-- ============================================
-- Migration 002: Add suggestions and options to planner questions
-- ============================================

ALTER TABLE planner_questions ADD COLUMN suggestions TEXT;  -- JSON array of suggestion strings
ALTER TABLE planner_questions ADD COLUMN options TEXT;      -- JSON array of option strings
