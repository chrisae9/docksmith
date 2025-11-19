-- Add ignore and allow_latest fields to script_assignments table
-- This allows database-only management of container settings without touching compose files
ALTER TABLE script_assignments ADD COLUMN ignore BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE script_assignments ADD COLUMN allow_latest BOOLEAN NOT NULL DEFAULT 0;
