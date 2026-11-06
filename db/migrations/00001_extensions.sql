-- Migration 00001: Enable pgvector extension.
-- pgvector is needed for competency embeddings (task 3.2).
-- The extension is optional at this stage: migrations work without it,
-- but vector columns/indexes (task 3.2) will depend on it.

-- +goose Up
CREATE EXTENSION IF NOT EXISTS vector;

-- +goose Down
DROP EXTENSION IF EXISTS vector;
