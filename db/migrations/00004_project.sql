-- Migration 00004: Projects, assignments, absences.
-- Hours stored internally as integer minutes (ADR-002 §2).

-- +goose Up

CREATE TABLE project (
  project_id                text PRIMARY KEY CHECK (project_id ~ '^prj_[a-z0-9]{4,12}$'),
  code                      text NOT NULL CHECK (length(code) BETWEEN 1 AND 40),
  name                      text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
  status                    text NOT NULL CHECK (status IN ('planned','active','paused','done','cancelled')),
  priority                  integer NOT NULL CHECK (priority BETWEEN 1 AND 5),
  criticality               text NOT NULL CHECK (criticality IN ('critical','high','normal','low')),
  start_week                text NOT NULL CHECK (start_week ~ '^[0-9]{4}-W(0[1-9]|[1-4][0-9]|5[0-3])$'),
  end_week_planned          text NOT NULL CHECK (end_week_planned ~ '^[0-9]{4}-W(0[1-9]|[1-4][0-9]|5[0-3])$'),
  end_week_forecast         text CHECK (end_week_forecast IS NULL OR end_week_forecast ~ '^[0-9]{4}-W(0[1-9]|[1-4][0-9]|5[0-3])$'),
  replaces_other_projects   boolean NOT NULL DEFAULT false,
  created_at                timestamptz NOT NULL DEFAULT now(),
  updated_at                timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE assignment (
  assignment_id   text PRIMARY KEY CHECK (assignment_id ~ '^asg_[a-z0-9]{4,12}$'),
  project_id      text NOT NULL REFERENCES project(project_id) ON DELETE CASCADE,
  employee_id     text NOT NULL REFERENCES employee(employee_id),
  role_id         text NOT NULL REFERENCES role(role_id),
  week            text NOT NULL CHECK (week ~ '^[0-9]{4}-W(0[1-9]|[1-4][0-9]|5[0-3])$'),
  hours_minutes   integer NOT NULL CHECK (hours_minutes > 0),
  source          text NOT NULL CHECK (source IN ('plan','actual','forecast')),
  created_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE (employee_id, project_id, week)
);

CREATE INDEX idx_assignment_employee_week ON assignment (employee_id, week);

CREATE TABLE absence (
  absence_id    text PRIMARY KEY CHECK (absence_id ~ '^abs_[a-z0-9]{4,12}$'),
  employee_id   text NOT NULL REFERENCES employee(employee_id),
  type          text NOT NULL CHECK (type IN (
                    'vacation','sick_leave','training','business_trip','parental_leave','other'
                  )),
  start_date    date NOT NULL,
  end_date      date NOT NULL CHECK (end_date >= start_date),
  hours_per_day numeric NOT NULL CHECK (hours_per_day BETWEEN 0 AND 12),
  approved      boolean NOT NULL DEFAULT true,
  source        text NOT NULL CHECK (source IN ('manual','hr_import','calendar_import')),
  deleted_at    timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (employee_id, start_date, end_date, type)
);

CREATE INDEX idx_absence_employee ON absence (employee_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_absence_date_range
  ON absence USING gist (daterange(start_date, end_date, '[]'))
  WHERE deleted_at IS NULL;

-- +goose Down

DROP INDEX idx_absence_date_range;
DROP INDEX idx_absence_employee;
DROP TABLE absence;
DROP INDEX idx_assignment_employee_week;
DROP TABLE assignment;
DROP TABLE project;
