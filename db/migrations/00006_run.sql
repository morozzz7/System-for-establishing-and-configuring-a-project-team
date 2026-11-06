-- Migration 00006: Runs, variants, solver results.

-- +goose Up

CREATE TABLE team_run (
  run_id             text PRIMARY KEY CHECK (run_id ~ '^run_[A-Za-z0-9]{4,16}$'),
  brief_id           text NOT NULL REFERENCES brief(brief_id),
  parent_run_id      text REFERENCES team_run(run_id),
  status             text NOT NULL CHECK (status IN ('queued','running','succeeded','partial','failed')),
  preset             text NOT NULL CHECK (preset IN ('fast','economical','balanced','low_risk')),
  k                  integer NOT NULL CHECK (k BETWEEN 1 AND 3),
  seed               integer NOT NULL CHECK (seed >= 0),
  time_limit_ms      integer NOT NULL CHECK (time_limit_ms BETWEEN 1000 AND 60000),
  start_week         text NOT NULL CHECK (start_week ~ '^[0-9]{4}-W(0[1-9]|[1-4][0-9]|5[0-3])$'),
  end_week           text NOT NULL CHECK (end_week ~ '^[0-9]{4}-W(0[1-9]|[1-4][0-9]|5[0-3])$'),
  weights            jsonb NOT NULL,
  constraints        jsonb,
  candidate_filter   jsonb,
  solver_status      text CHECK (solver_status IS NULL OR solver_status IN (
                       'OPTIMAL','FEASIBLE','INFEASIBLE','UNKNOWN','TIMEOUT','MODEL_INVALID','ERROR'
                     )),
  solve_time_ms      integer,
  ml_status          text CHECK (ml_status IS NULL OR ml_status IN (
                       'ok','degraded','timeout','unavailable','schema_invalid'
                     )),
  ml_attempts        integer CHECK (ml_attempts IS NULL OR ml_attempts BETWEEN 0 AND 5),
  validator_rejected integer DEFAULT 0 CHECK (validator_rejected >= 0),
  fallback_used      boolean DEFAULT false,
  fallback_reason    text,
  metrics_version    text NOT NULL DEFAULT 'm1-m12@2026-10',
  contract_version   text NOT NULL DEFAULT 'v1',
  reason_codes       text[] NOT NULL DEFAULT '{}',
  created_at         timestamptz NOT NULL DEFAULT now(),
  finished_at        timestamptz
);

CREATE INDEX idx_team_run_brief ON team_run (brief_id);

CREATE TABLE run_snapshot (
  snapshot_id          text PRIMARY KEY CHECK (snapshot_id ~ '^snp_[a-z0-9]{4,12}$'),
  run_id               text NOT NULL UNIQUE REFERENCES team_run(run_id),
  snapshot_sha256      text NOT NULL CHECK (snapshot_sha256 ~ '^[a-f0-9]{64}$'),
  horizon_start        text NOT NULL,
  horizon_end          text NOT NULL,
  weights              jsonb NOT NULL,
  constraints          jsonb,
  requirement_count    integer NOT NULL,
  candidate_count      integer NOT NULL,
  capacity_rows        integer NOT NULL,
  created_at           timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE run_snapshot_candidate (
  id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  snapshot_id       text NOT NULL REFERENCES run_snapshot(snapshot_id) ON DELETE CASCADE,
  employee_id       text NOT NULL,
  grade             text NOT NULL,
  cost_coefficient  numeric NOT NULL,
  competency_ids    text[] NOT NULL,
  capacity_digest   text NOT NULL CHECK (capacity_digest ~ '^[a-f0-9]{64}$'),
  capacity_weeks    integer NOT NULL
);

CREATE TABLE team_variant (
  variant_id         text PRIMARY KEY CHECK (variant_id ~ '^var_[A-Za-z0-9]{2,16}$'),
  run_id             text NOT NULL REFERENCES team_run(run_id) ON DELETE CASCADE,
  label              text NOT NULL CHECK (length(label) BETWEEN 1 AND 120),
  gate_pass          boolean NOT NULL,
  gate_g1            integer NOT NULL DEFAULT 0 CHECK (gate_g1 >= 0),
  gate_g2            integer NOT NULL DEFAULT 0 CHECK (gate_g2 >= 0),
  gate_g3            integer NOT NULL DEFAULT 0 CHECK (gate_g3 >= 0),
  gate_g4            integer NOT NULL DEFAULT 0 CHECK (gate_g4 >= 0),
  gate_g5            integer NOT NULL DEFAULT 0 CHECK (gate_g5 >= 0),
  max_overload_pct   numeric DEFAULT 0,
  score              numeric NOT NULL CHECK (score BETWEEN 0 AND 1),
  team_size          integer NOT NULL CHECK (team_size >= 0),
  cost_units         numeric NOT NULL CHECK (cost_units >= 0),
  duration           jsonb NOT NULL,
  metric_values      jsonb NOT NULL,
  metric_details     jsonb,
  reason_codes       text[] NOT NULL DEFAULT '{}',
  UNIQUE (run_id, variant_id)
);

CREATE TABLE variant_member (
  id                       bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  variant_id               text NOT NULL REFERENCES team_variant(variant_id) ON DELETE CASCADE,
  employee_id              text NOT NULL,
  role_id                  text NOT NULL,
  grade                    text NOT NULL,
  allocated_hours_minutes  integer NOT NULL CHECK (allocated_hours_minutes >= 0),
  assignment_share         numeric NOT NULL CHECK (assignment_share BETWEEN 0 AND 1),
  competence_fit           numeric NOT NULL CHECK (competence_fit BETWEEN 0 AND 1),
  reason_codes             text[] NOT NULL,
  explanation_id           text CHECK (explanation_id IS NULL OR explanation_id ~ '^exp_[a-z0-9]{4,12}$'),
  UNIQUE (variant_id, employee_id)
);

CREATE TABLE variant_weekly_assignment (
  id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  variant_id      text NOT NULL REFERENCES team_variant(variant_id) ON DELETE CASCADE,
  employee_id     text NOT NULL,
  requirement_id  text NOT NULL,
  week            text NOT NULL CHECK (week ~ '^[0-9]{4}-W(0[1-9]|[1-4][0-9]|5[0-3])$'),
  hours_minutes   integer NOT NULL CHECK (hours_minutes >= 0),
  share           numeric NOT NULL CHECK (share BETWEEN 0 AND 1),
  UNIQUE (variant_id, employee_id, requirement_id, week)
);

CREATE TABLE variant_deficit (
  id                        bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  variant_id                text NOT NULL REFERENCES team_variant(variant_id) ON DELETE CASCADE,
  competency_id             text NOT NULL,
  competency_name_ru        text NOT NULL,
  requirement_ids           text[] NOT NULL,
  person_days               numeric NOT NULL CHECK (person_days >= 0),
  weeks                     numeric CHECK (weeks IS NULL OR weeks >= 0),
  external_cooperation_flag boolean NOT NULL DEFAULT false,
  UNIQUE (variant_id, competency_id)
);

CREATE TABLE variant_recommended_action (
  id                      bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  deficit_id              bigint NOT NULL REFERENCES variant_deficit(id) ON DELETE CASCADE,
  action                  text NOT NULL CHECK (action IN (
                            'hire','contractor','move_deadline','upskill','reallocate','descope'
                          )),
  estimated_cost_units    numeric CHECK (estimated_cost_units IS NULL OR estimated_cost_units >= 0),
  time_to_start_weeks     numeric CHECK (time_to_start_weeks IS NULL OR time_to_start_weeks >= 0),
  from_project_id         text,
  reason_codes            text[] NOT NULL
);

CREATE TABLE variant_active_constraint (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  variant_id    text NOT NULL REFERENCES team_variant(variant_id) ON DELETE CASCADE,
  type          text NOT NULL CHECK (type IN (
                  'force_include','force_exclude','max_team_size','min_team_size',
                  'budget_cap','deadline','forbid_external','freeze_members',
                  'min_bus_factor','max_utilization'
                )),
  value         jsonb,
  source        text NOT NULL CHECK (source IN ('user','brief','policy')),
  reason_codes  text[] NOT NULL
);

CREATE TABLE variant_excluded_candidate (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  variant_id    text NOT NULL REFERENCES team_variant(variant_id) ON DELETE CASCADE,
  employee_id   text NOT NULL,
  reason_codes  text[] NOT NULL,
  detail        jsonb,
  UNIQUE (variant_id, employee_id)
);

-- +goose Down

DROP TABLE variant_excluded_candidate;
DROP TABLE variant_active_constraint;
DROP TABLE variant_recommended_action;
DROP TABLE variant_deficit;
DROP TABLE variant_weekly_assignment;
DROP TABLE variant_member;
DROP TABLE team_variant;
DROP TABLE run_snapshot_candidate;
DROP TABLE run_snapshot;
DROP TABLE team_run;
