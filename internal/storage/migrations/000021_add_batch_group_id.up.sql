ALTER TABLE update_operations ADD COLUMN batch_group_id TEXT;
CREATE INDEX IF NOT EXISTS idx_update_operations_batch_group_id ON update_operations(batch_group_id);
