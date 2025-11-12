-- Initial migration to verify migration system works
-- This creates a simple metadata table for tracking application info

CREATE TABLE IF NOT EXISTS app_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Insert initial metadata
INSERT INTO app_metadata (key, value) VALUES ('schema_version', '0');
INSERT INTO app_metadata (key, value) VALUES ('initialized_at', datetime('now'));
