-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS dependencies (
    id TEXT PRIMARY KEY,
    tag_id TEXT NOT NULL,
    dependency_tag_id TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE UNIQUE INDEX IF NOT EXISTS dependencies_tag_id_dependency_tag_id_idx ON dependencies (tag_id, dependency_tag_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS dependencies_tag_id_dependency_tag_id_idx;
DROP TABLE IF EXISTS dependencies;
-- +goose StatementEnd
