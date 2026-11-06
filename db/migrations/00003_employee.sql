-- Migration 00003: Employee domain.
-- No personal data: pseudonym IDs, grades, roles, competencies only.

-- +goose Up

CREATE TABLE employee (
  employee_id          text PRIMARY KEY CHECK (employee_id ~ '^emp_[a-z0-9]{4,12}$'),
  grade                text    NOT NULL REFERENCES grade_band(grade),
  role_profile_id      text    NOT NULL REFERENCES role(role_id),
  unit_id              text    NOT NULL CHECK (unit_id ~ '^unt_[a-z0-9]{2,12}$'),
  employment_type      text    NOT NULL CHECK (employment_type IN ('internal','contractor','external_partner')),
  bench_flag           boolean NOT NULL DEFAULT false,
  weekly_hours_norm    integer NOT NULL CHECK (weekly_hours_norm BETWEEN 1 AND 60),
  reserve_ratio        numeric NOT NULL CHECK (reserve_ratio BETWEEN 0 AND 0.5),
  max_utilization_ratio numeric NOT NULL CHECK (max_utilization_ratio BETWEEN 0.5 AND 1.0),
  cost_coefficient     numeric NOT NULL CHECK (cost_coefficient BETWEEN 0.5 AND 5.0),
  active               boolean NOT NULL DEFAULT true,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE employee_competency (
  employee_id   text    NOT NULL REFERENCES employee(employee_id) ON DELETE CASCADE,
  competency_id text    NOT NULL REFERENCES competency(competency_id),
  level         integer NOT NULL CHECK (level BETWEEN 1 AND 5),
  confidence    numeric NOT NULL CHECK (confidence BETWEEN 0 AND 1),
  last_used     date,
  source        text    NOT NULL CHECK (source IN (
                    'self_assessment','manager_review','project_history','certification','hr_import'
                  )),
  PRIMARY KEY (employee_id, competency_id)
);

-- Index for fast candidate search by competency + level.
CREATE INDEX idx_employee_competency_lookup
  ON employee_competency (competency_id, level DESC);

-- +goose Down

DROP INDEX idx_employee_competency_lookup;
DROP TABLE employee_competency;
DROP TABLE employee;
