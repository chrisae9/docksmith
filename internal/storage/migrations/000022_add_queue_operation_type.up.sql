-- Add operation_type column to update_queue table
-- Stores the operation type so processQueue dispatches the correct workflow
ALTER TABLE update_queue ADD COLUMN operation_type TEXT NOT NULL DEFAULT 'single';
