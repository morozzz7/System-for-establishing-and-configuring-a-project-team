-- Migration 00010: Durable workflow payloads.
-- data_jsonb keeps the immutable runinput.Data a recalculation needs after a restart.
-- payload_jsonb keeps the exact contract-shaped response Go already computed and verified;
-- the structured run/variant/explanation columns remain available for queries and audit.

-- +goose Up

ALTER TABLE run_snapshot ADD COLUMN data_jsonb jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE team_run ADD COLUMN payload_jsonb jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE team_variant ADD COLUMN payload_jsonb jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE explanation ADD COLUMN payload_jsonb jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE explanation ADD CONSTRAINT explanation_variant_locale_unique UNIQUE (variant_id, locale);

-- +goose Down

ALTER TABLE explanation DROP CONSTRAINT IF EXISTS explanation_variant_locale_unique;
ALTER TABLE explanation DROP COLUMN IF EXISTS payload_jsonb;
ALTER TABLE team_variant DROP COLUMN IF EXISTS payload_jsonb;
ALTER TABLE team_run DROP COLUMN IF EXISTS payload_jsonb;
ALTER TABLE run_snapshot DROP COLUMN IF EXISTS data_jsonb;
