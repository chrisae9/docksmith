-- Add batch_details JSON field to store multiple containers in batch operations
ALTER TABLE update_operations ADD COLUMN batch_details TEXT;
