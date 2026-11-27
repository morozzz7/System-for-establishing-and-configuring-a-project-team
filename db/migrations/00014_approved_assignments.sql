-- +goose Up
ALTER TABLE assignment ADD COLUMN approval_managed boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE assignment DROP COLUMN approval_managed;
