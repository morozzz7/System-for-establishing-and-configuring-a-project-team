-- +goose Up
ALTER TABLE employee ADD COLUMN display_name varchar(120) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE employee DROP COLUMN display_name;
