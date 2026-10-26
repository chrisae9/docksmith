CREATE TABLE notify_services (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    delivery TEXT NOT NULL DEFAULT 'immediate',
    events TEXT NOT NULL DEFAULT '[]',
    config TEXT NOT NULL DEFAULT '{}',
    last_success_at TIMESTAMP,
    last_error TEXT NOT NULL DEFAULT '',
    last_error_at TIMESTAMP,
    last_summary_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE notify_container_state (
    container_name TEXT PRIMARY KEY,
    last_notified_version TEXT NOT NULL DEFAULT '',
    last_notified_digest TEXT NOT NULL DEFAULT '',
    blocked INTEGER NOT NULL DEFAULT 0,
    check_fail_count INTEGER NOT NULL DEFAULT 0,
    check_fail_notified INTEGER NOT NULL DEFAULT 0,
    compose_mismatch INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE notify_pending (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    service_id INTEGER NOT NULL REFERENCES notify_services(id) ON DELETE CASCADE,
    dedup_key TEXT NOT NULL,
    kind TEXT NOT NULL,
    container_name TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(service_id, dedup_key)
);

CREATE INDEX idx_notify_pending_container ON notify_pending(container_name);
