-- +goose Up
-- +goose StatementBegin
-- timestamps already included in initial schema for SQLite
SELECT 1;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
