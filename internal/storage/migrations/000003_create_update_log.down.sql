-- Rollback: Drop update_log table and indexes

DROP INDEX IF EXISTS idx_update_log_container_name;
DROP TABLE IF EXISTS update_log;
