-- Create rollback_policies table for granular auto-rollback configuration
-- Supports global, per-stack, and per-container rollback settings
-- Enables hierarchical policy resolution (container > stack > global)

CREATE TABLE IF NOT EXISTS rollback_policies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_type TEXT NOT NULL CHECK(entity_type IN ('global', 'container', 'stack')),
    entity_id TEXT,
    auto_rollback_enabled BOOLEAN NOT NULL DEFAULT 0,
    health_check_required BOOLEAN NOT NULL DEFAULT 1,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(entity_type, entity_id)
);

-- Index on entity_type and entity_id for fast policy lookup
CREATE UNIQUE INDEX IF NOT EXISTS idx_rollback_policies_entity
ON rollback_policies(entity_type, entity_id);

-- Insert default global policy
INSERT INTO rollback_policies (entity_type, entity_id, auto_rollback_enabled, health_check_required)
VALUES ('global', NULL, 0, 1);
