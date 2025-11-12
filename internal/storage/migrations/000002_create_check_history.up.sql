-- Create check_history table for logging check operations
-- This table records every check operation with results and errors

CREATE TABLE IF NOT EXISTS check_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    container_name TEXT NOT NULL,
    image TEXT NOT NULL,
    check_time TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    current_version TEXT NOT NULL,
    latest_version TEXT NOT NULL,
    status TEXT NOT NULL,
    error TEXT
);

-- Composite index on (container_name, check_time) for efficient queries
-- Allows fast lookup of check history for a specific container ordered by time
CREATE INDEX IF NOT EXISTS idx_check_history_container_time
ON check_history(container_name, check_time DESC);

-- Index on check_time for time range queries
CREATE INDEX IF NOT EXISTS idx_check_history_check_time
ON check_history(check_time DESC);
