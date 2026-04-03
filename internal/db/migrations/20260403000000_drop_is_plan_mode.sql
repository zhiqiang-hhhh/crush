-- +goose Up
ALTER TABLE messages DROP COLUMN is_plan_mode;

-- +goose Down
ALTER TABLE messages ADD COLUMN is_plan_mode INTEGER DEFAULT 0 NOT NULL;
