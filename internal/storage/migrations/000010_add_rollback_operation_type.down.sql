-- Revert operation_type constraint (remove 'rollback')
-- Note: This will fail if there are any rollback operations in the database

CREATE TABLE update_operations_new (
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

INSERT INTO update_operations_new
SELECT * FROM update_operations WHERE operation_type != 'rollback';

DROP TABLE update_operations;

ALTER TABLE update_operations_new RENAME TO update_operations;

CREATE UNIQUE INDEX IF NOT EXISTS idx_update_operations_operation_id
ON update_operations(operation_id);

CREATE INDEX IF NOT EXISTS idx_update_operations_container_name
ON update_operations(container_name, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_update_operations_stack_name
ON update_operations(stack_name, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_update_operations_status
ON update_operations(status, created_at);

CREATE INDEX IF NOT EXISTS idx_update_operations_started_at
ON update_operations(started_at DESC);
