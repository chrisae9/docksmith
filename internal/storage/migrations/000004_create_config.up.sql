-- Create config table for storing key-value configuration
-- Stores user preferences like cache TTL, GitHub token, compose file paths
-- Uses INSERT OR REPLACE pattern for upserts with timestamp tracking

CREATE TABLE IF NOT EXISTS config (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
