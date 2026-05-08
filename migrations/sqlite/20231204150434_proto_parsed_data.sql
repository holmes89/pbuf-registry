-- +goose Up
-- +goose StatementBegin
ALTER TABLE tags ADD COLUMN is_processed INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS proto_parsed_data (
    id TEXT PRIMARY KEY,
    tag_id TEXT NOT NULL,
    filename TEXT NOT NULL,
    json TEXT NOT NULL,
    UNIQUE (tag_id, filename)
);

CREATE TABLE IF NOT EXISTS tag_meta (
    id TEXT PRIMARY KEY,
    tag_id TEXT NOT NULL,
    meta TEXT NOT NULL,
    UNIQUE (tag_id)
);

CREATE INDEX IF NOT EXISTS idx_tags_is_processed ON tags (is_processed);
CREATE INDEX IF NOT EXISTS idx_proto_parsed_data_tag_id ON proto_parsed_data (tag_id);
CREATE INDEX IF NOT EXISTS idx_tag_meta_tag_id ON tag_meta (tag_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_tags_is_processed;
DROP INDEX IF EXISTS idx_proto_parsed_data_tag_id;
DROP INDEX IF EXISTS idx_tag_meta_tag_id;
DROP TABLE IF EXISTS proto_parsed_data;
DROP TABLE IF EXISTS tag_meta;
-- SQLite does not support DROP COLUMN portably; leave is_processed column
-- +goose StatementEnd
