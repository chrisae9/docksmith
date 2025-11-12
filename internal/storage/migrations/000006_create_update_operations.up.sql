-- Create update_operations table for tracking orchestrated update operations
-- Records detailed state and progress of each update operation (single, batch, stack)
-- Supports real-time progress tracking and audit logging

CREATE TABLE IF NOT EXISTS update_operations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    operation_id TEXT NOT NULL UNIQUE,
    container_id TEXT,
    container_name TEXT NOT NULL,
    stack_name TEXT,
    operation_type TEXT NOT NULL CHECK(operation_type IN ('single', 'batch', 'stack')),
    status TEXT NOT NULL CHECK(status IN ('queued', 'validating', 'backup', 'updating_compose', 'pulling_image', 'stopping', 'starting', 'health_check', 'restarting_dependents', 'complete', 'failed', 'rolling_back', 'cancelled')),
    old_version TEXT,
    new_version TEXT,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    error_message TEXT,
    dependents_affected TEXT,
    rollback_occurred BOOLEAN NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Index on operation_id for fast lookup by operation ID
CREATE UNIQUE INDEX IF NOT EXISTS idx_update_operations_operation_id
ON update_operations(operation_id);

-- Index on container_name for container-specific operation history
CREATE INDEX IF NOT EXISTS idx_update_operations_container_name
ON update_operations(container_name, started_at DESC);

-- Index on stack_name for stack-level operation queries
CREATE INDEX IF NOT EXISTS idx_update_operations_stack_name
ON update_operations(stack_name, started_at DESC);

-- Index on status for filtering queued/in-progress operations
CREATE INDEX IF NOT EXISTS idx_update_operations_status
ON update_operations(status, created_at);

-- Index on started_at for chronological queries
CREATE INDEX IF NOT EXISTS idx_update_operations_started_at
ON update_operations(started_at DESC);
