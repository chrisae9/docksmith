-- Remove ignore and allow_latest columns
-- SQLite doesn't support DROP COLUMN, so recreate table

CREATE TABLE script_assignments_temp (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    container_name TEXT NOT NULL UNIQUE,
    script_path TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT 1,
    assigned_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    assigned_by TEXT,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO script_assignments_temp (id, container_name, script_path, enabled, assigned_at, assigned_by, updated_at)
SELECT id, container_name, script_path, enabled, assigned_at, assigned_by, updated_at
FROM script_assignments;

DROP TABLE script_assignments;
ALTER TABLE script_assignments_temp RENAME TO script_assignments;

CREATE INDEX IF NOT EXISTS idx_script_assignments_container ON script_assignments(container_name);
CREATE INDEX IF NOT EXISTS idx_script_assignments_enabled ON script_assignments(enabled, container_name);
