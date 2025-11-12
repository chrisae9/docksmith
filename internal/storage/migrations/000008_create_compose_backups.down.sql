-- Rollback migration for compose_backups table
DROP INDEX IF EXISTS idx_compose_backups_timestamp;
DROP INDEX IF EXISTS idx_compose_backups_container_name;
DROP INDEX IF EXISTS idx_compose_backups_operation_id;
DROP TABLE IF EXISTS compose_backups;
