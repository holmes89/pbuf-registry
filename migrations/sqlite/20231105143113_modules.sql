-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS modules (
    id TEXT PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS tags (
    id TEXT PRIMARY KEY,
    module_id TEXT NOT NULL,
    tag TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (module_id, tag)
);

CREATE TABLE IF NOT EXISTS protofiles (
    id TEXT PRIMARY KEY,
    tag_id TEXT NOT NULL,
    filename TEXT NOT NULL,
    content TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (tag_id, filename)
);

CREATE INDEX IF NOT EXISTS idx_modules_name ON modules (name);
CREATE INDEX IF NOT EXISTS idx_tags_module_id_tag ON tags (module_id, tag);
CREATE INDEX IF NOT EXISTS idx_protofiles_tag_id ON protofiles (tag_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_protofiles_tag_id;
DROP INDEX IF EXISTS idx_tags_module_id_tag;
DROP INDEX IF EXISTS idx_modules_name;
DROP TABLE IF EXISTS protofiles;
DROP TABLE IF EXISTS tags;
DROP TABLE IF EXISTS modules;
-- +goose StatementEnd
