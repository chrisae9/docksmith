-- Create update_queue table for managing pending update operations
-- Implements FIFO queue with stack-level locking
-- Persists queue state across application restarts

CREATE TABLE IF NOT EXISTS update_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    operation_id TEXT NOT NULL UNIQUE,
    stack_name TEXT NOT NULL,
    containers TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 0,
    queued_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    estimated_start_time TIMESTAMP
);

-- Index on stack_name for queue processing by stack
CREATE INDEX IF NOT EXISTS idx_update_queue_stack_name
ON update_queue(stack_name, queued_at);

-- Index on queued_at for FIFO processing
CREATE INDEX IF NOT EXISTS idx_update_queue_queued_at
ON update_queue(queued_at);

-- Index on priority for priority-based processing
CREATE INDEX IF NOT EXISTS idx_update_queue_priority
ON update_queue(priority DESC, queued_at);
