-- Migration 00005: Briefs and requirements.
-- Effort in person-days; text_sha256 for dedup.

-- +goose Up

CREATE TABLE brief (
  brief_id                     text PRIMARY KEY CHECK (brief_id ~ '^brf_[A-Za-z0-9]{4,16}$'),
  title                        text NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
  source_type                  text NOT NULL CHECK (source_type IN ('text','docx','pdf','txt')),
  language                     text NOT NULL CHECK (language IN ('ru','en')),
  status                       text NOT NULL CHECK (status IN (
                                 'uploaded','extracting','extracted','needs_review','confirmed','failed'
                               )),
  text                         text NOT NULL CHECK (length(text) BETWEEN 1 AND 200000),
  text_sha256                  text NOT NULL CHECK (text_sha256 ~ '^[a-f0-9]{64}$'),
  requested_start_week         text CHECK (requested_start_week IS NULL OR requested_start_week ~ '^[0-9]{4}-W(0[1-9]|[1-4][0-9]|5[0-3])$'),
  deadline_week                text CHECK (deadline_week IS NULL OR deadline_week ~ '^[0-9]{4}-W(0[1-9]|[1-4][0-9]|5[0-3])$'),
  extraction_model             text,
  extraction_prompt_version    text,
  extraction_duration_ms       integer,
  extraction_json_valid        boolean,
  extraction_retries           integer CHECK (extraction_retries IS NULL OR extraction_retries BETWEEN 0 AND 2),
  created_at                   timestamptz NOT NULL DEFAULT now(),
  updated_at                   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE requirement (
  requirement_id                text PRIMARY KEY CHECK (requirement_id ~ '^req_[A-Za-z0-9]{4,16}$'),
  brief_id                      text NOT NULL REFERENCES brief(brief_id) ON DELETE CASCADE,
  role_id                       text NOT NULL REFERENCES role(role_id),
  role_confidence               numeric NOT NULL CHECK (role_confidence BETWEEN 0 AND 1),
  competency_id                 text NOT NULL REFERENCES competency(competency_id),
  level                         integer NOT NULL CHECK (level BETWEEN 1 AND 5),
  criticality                   text NOT NULL CHECK (criticality IN ('must','should','nice')),
  headcount                     integer NOT NULL CHECK (headcount BETWEEN 1 AND 20),
  effort_min                    numeric NOT NULL CHECK (effort_min BETWEEN 0.5 AND 2000),
  effort_likely                 numeric NOT NULL CHECK (effort_likely BETWEEN 0.5 AND 2000),
  effort_max                    numeric NOT NULL CHECK (effort_max BETWEEN 0.5 AND 2000),
  confidence                    numeric NOT NULL CHECK (confidence BETWEEN 0 AND 1),
  needs_review                  boolean NOT NULL DEFAULT false,
  normalization_top1_score      numeric CHECK (normalization_top1_score IS NULL OR normalization_top1_score BETWEEN 0 AND 1),
  normalization_candidates      jsonb,
  edited                        boolean NOT NULL DEFAULT false,
  edited_by                     text CHECK (edited_by IS NULL OR edited_by ~ '^(emp_[a-z0-9]{4,12}|system)$'),
  edited_at                     timestamptz,
  created_at                    timestamptz NOT NULL DEFAULT now(),
  CHECK (effort_min <= effort_likely AND effort_likely <= effort_max)
);

CREATE TABLE requirement_assumption (
  id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  requirement_id  text NOT NULL REFERENCES requirement(requirement_id) ON DELETE CASCADE,
  position        integer NOT NULL CHECK (position >= 0),
  text            text NOT NULL CHECK (length(text) BETWEEN 1 AND 300),
  UNIQUE (requirement_id, position)
);

CREATE TABLE requirement_evidence (
  id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  requirement_id  text NOT NULL REFERENCES requirement(requirement_id) ON DELETE CASCADE,
  kind            text NOT NULL CHECK (kind IN ('brief_span','taxonomy_synonym','project_history','user_edit')),
  char_start      integer CHECK (char_start IS NULL OR char_start >= 0),
  char_end        integer CHECK (char_end IS NULL OR char_end >= 1),
  quote           text NOT NULL CHECK (length(quote) BETWEEN 3 AND 1000),
  position        integer NOT NULL CHECK (position >= 0),
  UNIQUE (requirement_id, position)
);

-- +goose Down

DROP TABLE requirement_evidence;
DROP TABLE requirement_assumption;
DROP TABLE requirement;
DROP TABLE brief;
