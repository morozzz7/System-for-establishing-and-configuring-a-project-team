-- Migration 00011: persist the full extracted brief so a restart can create new runs
-- without re-uploading the brief (the run_snapshot pattern of 00010 applied to briefs).

-- +goose Up

ALTER TABLE brief ADD COLUMN data_jsonb jsonb NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down

ALTER TABLE brief DROP COLUMN data_jsonb;
