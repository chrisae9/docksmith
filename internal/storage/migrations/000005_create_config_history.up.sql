-- Create config_history table for storing configuration snapshots
-- Enables configuration rollback and audit trail with mobile-friendly revert
-- Stores complete configuration state as JSON for atomic restoration

CREATE TABLE IF NOT EXISTS config_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    snapshot_time TIMESTAMP NOT NULL,
    config_snapshot TEXT NOT NULL,
    changed_by TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Index on snapshot_time DESC for fast retrieval of recent history
CREATE INDEX IF NOT EXISTS idx_config_history_snapshot_time
ON config_history(snapshot_time DESC);
