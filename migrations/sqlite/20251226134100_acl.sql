-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS acl (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    module_name TEXT NOT NULL,
    permission TEXT NOT NULL CHECK (permission IN ('read', 'write', 'admin')),
    created_at TEXT DEFAULT (datetime('now')),
    UNIQUE (user_id, module_name)
);

CREATE INDEX IF NOT EXISTS idx_acl_user_id ON acl (user_id);
CREATE INDEX IF NOT EXISTS idx_acl_module_name ON acl (module_name);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_acl_module_name;
DROP INDEX IF EXISTS idx_acl_user_id;
DROP TABLE IF EXISTS acl;
-- +goose StatementEnd
