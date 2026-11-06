-- Migration 00007: Audit log, decisions, explanations, constraint overrides.

-- +goose Up

CREATE TABLE audit_log (
  event_id      text PRIMARY KEY CHECK (event_id ~ '^aud_[a-z0-9]{4,12}$'),
  at            timestamptz NOT NULL DEFAULT now(),
  actor         text NOT NULL CHECK (actor ~ '^(emp_[a-z0-9]{4,12}|system)$'),
  action        text NOT NULL CHECK (action IN (
                  'brief_created','brief_extracted','requirement_edited','requirements_confirmed',
                  'run_started','run_finished','variant_rejected',
                  'constraint_override_added','recalculation_started','decision_recorded',
                  'absence_created','absence_cancelled','employee_updated','competency_updated'
                )),
  entity_type   text NOT NULL CHECK (entity_type IN (
                  'brief','requirement','run','variant','employee','absence','constraint_override'
                )),
  entity_id     text NOT NULL,
  request_id    text CHECK (request_id IS NULL OR request_id ~ '^req_[A-Za-z0-9-]{6,32}$'),
  summary       text NOT NULL CHECK (length(summary) BETWEEN 1 AND 500),
  details       jsonb
);

CREATE INDEX idx_audit_entity ON audit_log (entity_type, entity_id);
CREATE INDEX idx_audit_at     ON audit_log (at DESC);

CREATE TABLE constraint_override (
  id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  run_id          text NOT NULL REFERENCES team_run(run_id),
  type            text NOT NULL CHECK (type IN (
                    'force_include','force_exclude','max_team_size','min_team_size',
                    'budget_cap','deadline','forbid_external','freeze_members',
                    'min_bus_factor','max_utilization'
                  )),
  employee_id     text CHECK (employee_id IS NULL OR employee_id ~ '^emp_[a-z0-9]{4,12}$'),
  requirement_id  text,
  value           numeric,
  week            text,
  reason          text CHECK (reason IS NULL OR length(reason) BETWEEN 1 AND 300),
  created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE decision (
  run_id          text PRIMARY KEY REFERENCES team_run(run_id),
  variant_id      text NOT NULL,
  decided_at      timestamptz NOT NULL DEFAULT now(),
  decided_by      text NOT NULL CHECK (decided_by ~ '^(emp_[a-z0-9]{4,12}|system)$'),
  comment         text CHECK (comment IS NULL OR length(comment) <= 1000),
  edits_applied   jsonb,
  audit_event_id  text NOT NULL CHECK (audit_event_id ~ '^aud_[a-z0-9]{4,12}$')
);

CREATE TABLE explanation (
  explanation_id  text PRIMARY KEY CHECK (explanation_id ~ '^exp_[a-z0-9]{4,12}$'),
  variant_id      text NOT NULL REFERENCES team_variant(variant_id) ON DELETE CASCADE,
  locale          text NOT NULL CHECK (locale IN ('ru','en')),
  headline        text NOT NULL CHECK (length(headline) BETWEEN 1 AND 400),
  tradeoff        jsonb NOT NULL,
  included        jsonb NOT NULL,
  excluded        jsonb NOT NULL,
  deficits        jsonb NOT NULL,
  counterfactuals jsonb NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now()
);

-- +goose Down

DROP TABLE explanation;
DROP TABLE decision;
DROP TABLE constraint_override;
DROP INDEX idx_audit_at;
DROP INDEX idx_audit_entity;
DROP TABLE audit_log;
