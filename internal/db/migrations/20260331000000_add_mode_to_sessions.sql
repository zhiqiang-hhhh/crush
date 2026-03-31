-- +goose Up
-- +goose StatementBegin
ALTER TABLE sessions ADD COLUMN mode TEXT NOT NULL DEFAULT 'build';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sessions DROP COLUMN mode;
-- +goose StatementEnd
