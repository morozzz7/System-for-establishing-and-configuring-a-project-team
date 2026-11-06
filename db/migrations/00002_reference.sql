-- Migration 00002: Reference / taxonomy tables.
-- Loaded from contracts/taxonomy.yaml and contracts/reference.yaml by the seed binary (task 2.2).
-- No personal data: only roles, competencies, levels, grades and weight presets.

-- +goose Up

CREATE TABLE level_descriptor (
  level          integer PRIMARY KEY CHECK (level BETWEEN 1 AND 5),
  label_ru       text    NOT NULL,
  label_en       text    NOT NULL,
  description_ru text    NOT NULL,
  description_en text    NOT NULL
);

CREATE TABLE role (
  role_id           text PRIMARY KEY CHECK (role_id ~ '^[a-z][a-z0-9_]{2,39}$'),
  name_en           text    NOT NULL,
  name_ru           text    NOT NULL,
  family            text    NOT NULL CHECK (family IN (
                        'engineering','quality','infrastructure','data','analysis',
                        'management','design','security'
                      )),
  synonyms          text[]  NOT NULL DEFAULT '{}',
  core_competencies text[]  NOT NULL DEFAULT '{}',
  typical_grades    text[]  NOT NULL DEFAULT '{}',
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE competency (
  competency_id text PRIMARY KEY CHECK (competency_id ~ '^[a-z][a-z0-9_]{2,39}$'),
  name_en       text    NOT NULL,
  name_ru       text    NOT NULL,
  category      text    NOT NULL CHECK (category IN (
                    'language','framework','database','messaging','devops','cloud',
                    'testing','architecture','analytics','ml','process','soft_skill','domain','security'
                  )),
  level_scale   integer NOT NULL DEFAULT 5 CHECK (level_scale = 5),
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE competency_synonym (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  competency_id text    NOT NULL REFERENCES competency(competency_id) ON DELETE CASCADE,
  synonym       text    NOT NULL CHECK (length(synonym) BETWEEN 1 AND 80),
  UNIQUE (competency_id, synonym)
);

CREATE TABLE role_competency (
  role_id       text    NOT NULL REFERENCES role(role_id) ON DELETE CASCADE,
  competency_id text    NOT NULL REFERENCES competency(competency_id) ON DELETE CASCADE,
  is_core       boolean NOT NULL DEFAULT false,
  PRIMARY KEY (role_id, competency_id)
);

CREATE TABLE grade_band (
  grade              text PRIMARY KEY CHECK (grade IN ('J','M','S','L','P')),
  label_ru           text    NOT NULL,
  label_en           text    NOT NULL,
  cost_coefficient   numeric NOT NULL CHECK (cost_coefficient BETWEEN 0.5 AND 5.0),
  typical_level_min  integer NOT NULL CHECK (typical_level_min BETWEEN 1 AND 5),
  typical_level_max  integer NOT NULL CHECK (typical_level_max BETWEEN 1 AND 5),
  CHECK (typical_level_min <= typical_level_max)
);

CREATE TABLE weight_preset (
  preset    text PRIMARY KEY CHECK (preset IN ('fast','economical','balanced','low_risk')),
  label_ru  text    NOT NULL,
  weights   jsonb   NOT NULL,
  sum_check numeric NOT NULL CHECK (abs(sum_check - 1.0) < 1e-6)
);

-- +goose Down

DROP TABLE weight_preset;
DROP TABLE grade_band;
DROP TABLE role_competency;
DROP TABLE competency_synonym;
DROP TABLE competency;
DROP TABLE role;
DROP TABLE level_descriptor;
