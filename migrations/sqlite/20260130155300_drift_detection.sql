-- +goose Up
-- +goose StatementBegin
ALTER TABLE protofiles ADD COLUMN content_hash TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_protofiles_tag_content_hash_null
ON protofiles (tag_id) WHERE content_hash = '';

CREATE TABLE IF NOT EXISTS drift_events (
    id TEXT PRIMARY KEY,
    module_id TEXT NOT NULL REFERENCES modules(id) ON DELETE CASCADE,
    tag_id TEXT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    filename TEXT NOT NULL,
    event_type TEXT NOT NULL CHECK (event_type IN ('added', 'modified', 'deleted')),
    previous_hash TEXT NOT NULL DEFAULT '',
    current_hash TEXT NOT NULL DEFAULT '',
    severity TEXT NOT NULL DEFAULT 'medium' CHECK (severity IN ('info', 'warning', 'critical')),
    detected_at TEXT DEFAULT (datetime('now')),
    acknowledged INTEGER NOT NULL DEFAULT 0,
    acknowledged_at TEXT,
    acknowledged_by TEXT
);

CREATE INDEX IF NOT EXISTS idx_drift_events_module_id ON drift_events (module_id);
CREATE INDEX IF NOT EXISTS idx_drift_events_tag_id ON drift_events (tag_id);
CREATE INDEX IF NOT EXISTS idx_drift_events_detected_at ON drift_events (detected_at);
CREATE INDEX IF NOT EXISTS idx_drift_events_severity ON drift_events (severity);
CREATE INDEX IF NOT EXISTS idx_drift_events_module_acknowledged ON drift_events (module_id, acknowledged);
CREATE UNIQUE INDEX IF NOT EXISTS idx_drift_events_unique
ON drift_events (tag_id, filename, event_type, previous_hash, current_hash);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_drift_events_unique;
DROP INDEX IF EXISTS idx_drift_events_module_acknowledged;
DROP INDEX IF EXISTS idx_drift_events_severity;
DROP INDEX IF EXISTS idx_drift_events_detected_at;
DROP INDEX IF EXISTS idx_drift_events_tag_id;
DROP INDEX IF EXISTS idx_drift_events_module_id;
DROP TABLE IF EXISTS drift_events;
DROP INDEX IF EXISTS idx_protofiles_tag_content_hash_null;
-- +goose StatementEnd
