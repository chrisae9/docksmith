-- Remove batch_details field
-- SQLite doesn't support DROP COLUMN directly in older versions,
-- but we can just leave it for rollback compatibility
-- ALTER TABLE update_operations DROP COLUMN batch_details;

-- For older SQLite versions, we'd need to recreate the table without the column
-- But since this is a non-critical field, we can skip the down migration
