-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS draft_tags (
    id TEXT PRIMARY KEY,
    module_id TEXT NOT NULL,
    tag TEXT NOT NULL,
    proto_files TEXT NOT NULL,
    dependencies TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (module_id, tag)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS draft_tags;
-- +goose StatementEnd
