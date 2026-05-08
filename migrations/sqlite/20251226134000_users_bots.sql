-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    token TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('user', 'bot')),
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_users_token ON users (token);
CREATE INDEX IF NOT EXISTS idx_users_type ON users (type);
CREATE INDEX IF NOT EXISTS idx_users_name ON users (name);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_users_name;
DROP INDEX IF EXISTS idx_users_type;
DROP INDEX IF EXISTS idx_users_token;
DROP TABLE IF EXISTS users;
-- +goose StatementEnd
