-- Create compose_backups table for tracking compose file backups
-- Links backups to update operations for rollback capability
-- Records backup metadata for audit trail and restoration

CREATE TABLE IF NOT EXISTS compose_backups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    operation_id TEXT NOT NULL,
    container_name TEXT NOT NULL,
    stack_name TEXT,
    compose_file_path TEXT NOT NULL,
    backup_file_path TEXT NOT NULL,
    backup_timestamp TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Index on operation_id for fast lookup during rollback
CREATE INDEX IF NOT EXISTS idx_compose_backups_operation_id
ON compose_backups(operation_id);

-- Index on container_name for container-specific backup history
CREATE INDEX IF NOT EXISTS idx_compose_backups_container_name
ON compose_backups(container_name, backup_timestamp DESC);

-- Index on backup_timestamp for chronological queries and cleanup
CREATE INDEX IF NOT EXISTS idx_compose_backups_timestamp
ON compose_backups(backup_timestamp DESC);
