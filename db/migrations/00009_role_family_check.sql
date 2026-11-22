-- Migration 00009: Extend role_family_check with the 'product' family (taxonomy expansion 2026-11-22).

-- +goose Up

ALTER TABLE role DROP CONSTRAINT IF EXISTS role_family_check;
ALTER TABLE role ADD CONSTRAINT role_family_check CHECK (family IN (
    'engineering','quality','infrastructure','data','analysis','management','design','security','product'
));

-- +goose Down

ALTER TABLE role DROP CONSTRAINT IF EXISTS role_family_check;
ALTER TABLE role ADD CONSTRAINT role_family_check CHECK (family IN (
    'engineering','quality','infrastructure','data','analysis','management','design','security'
));
