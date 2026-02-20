-- SQLite does not support DROP COLUMN before 3.35.0, so recreate the table
-- This is a rollback migration; data loss is acceptable.
CREATE TABLE update_queue_backup AS SELECT id, operation_id, stack_name, containers, operation_type, priority, queued_at, estimated_start_time FROM update_queue;
DROP TABLE update_queue;
ALTER TABLE update_queue_backup RENAME TO update_queue;
