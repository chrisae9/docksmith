-- Create version_cache table for storing SHA-to-version mappings
-- This table caches registry lookups to prevent UNKNOWN status for containers
-- that have fallen behind many versions

CREATE TABLE IF NOT EXISTS version_cache (
    sha256 TEXT NOT NULL,
    image_ref TEXT NOT NULL,
    resolved_version TEXT NOT NULL,
    architecture TEXT NOT NULL DEFAULT 'amd64',
    resolved_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- Composite primary key to handle multi-architecture images
    PRIMARY KEY (sha256, image_ref, architecture)
);

-- Index on resolved_at for efficient TTL-based cleanup queries
CREATE INDEX IF NOT EXISTS idx_version_cache_resolved_at
ON version_cache(resolved_at);
