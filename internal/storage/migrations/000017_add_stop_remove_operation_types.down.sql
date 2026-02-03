-- Remove 'stop' and 'remove' from operation_type constraint
-- Recreate table without stop and remove types

-- Step 1: Create table without stop and remove
CREATE TABLE update_operations_old (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    operation_id TEXT NOT NULL UNIQUE,
    container_id TEXT,
    container_name TEXT NOT NULL,
    stack_name TEXT,
    operation_type TEXT NOT NULL CHECK(operation_type IN ('single', 'batch', 'stack', 'rollback', 'restart', 'label_change')),
    status TEXT NOT NULL CHECK(status IN ('queued', 'validating', 'backup', 'updating_compose', 'pulling_image', 'stopping', 'starting', 'health_check', 'restarting_dependents', 'complete', 'failed', 'rolling_back', 'cancelled', 'in_progress')),
    old_version TEXT,
    new_version TEXT,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    error_message TEXT,
    dependents_affected TEXT,
    rollback_occurred BOOLEAN NOT NULL DEFAULT 0,
    batch_details TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Step 2: Copy data excluding stop and remove operations
INSERT INTO update_operations_old
SELECT id, operation_id, container_id, container_name, stack_name, operation_type, status, old_version, new_version, started_at, completed_at, error_message, dependents_affected, rollback_occurred, batch_details, created_at, updated_at FROM update_operations WHERE operation_type NOT IN ('stop', 'remove');

-- Step 3: Drop new table
DROP TABLE update_operations;

-- Step 4: Rename old table
ALTER TABLE update_operations_old RENAME TO update_operations;

-- Step 5: Recreate indexes
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
