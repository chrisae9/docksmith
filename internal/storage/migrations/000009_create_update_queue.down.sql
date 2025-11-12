-- Rollback migration for update_queue table
DROP INDEX IF EXISTS idx_update_queue_priority;
DROP INDEX IF EXISTS idx_update_queue_queued_at;
DROP INDEX IF EXISTS idx_update_queue_stack_name;
DROP TABLE IF EXISTS update_queue;
