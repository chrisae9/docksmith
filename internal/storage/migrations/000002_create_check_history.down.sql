-- Rollback: Drop check_history table and indexes

DROP INDEX IF EXISTS idx_check_history_check_time;
DROP INDEX IF EXISTS idx_check_history_container_time;
DROP TABLE IF EXISTS check_history;
