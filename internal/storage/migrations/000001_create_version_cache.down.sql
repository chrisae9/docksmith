-- Rollback migration for version_cache table

DROP INDEX IF EXISTS idx_version_cache_resolved_at;
DROP TABLE IF EXISTS version_cache;
