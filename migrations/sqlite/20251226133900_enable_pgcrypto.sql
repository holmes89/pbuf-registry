-- +goose Up
-- +goose StatementBegin
-- no-op: pgcrypto is PostgreSQL-specific; token hashing is handled in Go for SQLite
SELECT 1;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
