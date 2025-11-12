-- Rollback migration for rollback_policies table
DROP INDEX IF EXISTS idx_rollback_policies_entity;
DROP TABLE IF EXISTS rollback_policies;
