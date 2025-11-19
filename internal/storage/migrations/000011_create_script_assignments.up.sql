-- Create table for tracking pre-update check script assignments
-- Each container can have one pre-update check script assigned
CREATE TABLE script_assignments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    container_name TEXT NOT NULL UNIQUE,
    script_path TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT 1,
    assigned_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    assigned_by TEXT,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Index for fast container lookups
CREATE INDEX IF NOT EXISTS idx_script_assignments_container
ON script_assignments(container_name);

-- Index for listing enabled assignments
CREATE INDEX IF NOT EXISTS idx_script_assignments_enabled
ON script_assignments(enabled, container_name);
