-- Create update_log table for audit logging of update operations
-- This is an append-only, immutable log for tracking container updates
-- Supports future rollback feature by preserving version history

CREATE TABLE IF NOT EXISTS update_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    container_name TEXT NOT NULL,
    operation TEXT NOT NULL CHECK(operation IN ('pull', 'restart', 'rollback')),
    from_version TEXT NOT NULL,
    to_version TEXT NOT NULL,
    timestamp TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    success BOOLEAN NOT NULL,
    error TEXT
);

-- Index on container_name for container-specific audit trails
-- Allows fast lookup of update history for a specific container
CREATE INDEX IF NOT EXISTS idx_update_log_container_name
ON update_log(container_name, timestamp DESC);
