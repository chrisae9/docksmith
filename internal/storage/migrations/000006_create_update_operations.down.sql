-- Rollback migration for update_operations table
DROP INDEX IF EXISTS idx_update_operations_started_at;
DROP INDEX IF EXISTS idx_update_operations_status;
DROP INDEX IF EXISTS idx_update_operations_stack_name;
DROP INDEX IF EXISTS idx_update_operations_container_name;
DROP INDEX IF EXISTS idx_update_operations_operation_id;
DROP TABLE IF EXISTS update_operations;
