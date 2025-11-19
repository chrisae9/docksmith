-- Rollback script_assignments table creation
DROP INDEX IF EXISTS idx_script_assignments_enabled;
DROP INDEX IF EXISTS idx_script_assignments_container;
DROP TABLE IF EXISTS script_assignments;
