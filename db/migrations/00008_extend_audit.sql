-- Migration 00008: Extend audit_log actions and entity types for CRUD operations.
-- Adds employee_created, project_created, assignment_created, assignment_deleted.
-- Adds 'project' and 'assignment' to entity_type.

-- +goose Up

ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS audit_log_action_check;
ALTER TABLE audit_log ADD CONSTRAINT audit_log_action_check CHECK (action IN (
    'brief_created','brief_extracted','requirement_edited','requirements_confirmed',
    'run_started','run_finished','variant_rejected',
    'constraint_override_added','recalculation_started','decision_recorded',
    'absence_created','absence_cancelled','employee_updated','competency_updated',
    'employee_created','project_created','assignment_created','assignment_deleted'
));

ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS audit_log_entity_type_check;
ALTER TABLE audit_log ADD CONSTRAINT audit_log_entity_type_check CHECK (entity_type IN (
    'brief','requirement','run','variant','employee','absence','constraint_override',
    'project','assignment'
));

-- +goose Down

ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS audit_log_action_check;
ALTER TABLE audit_log ADD CONSTRAINT audit_log_action_check CHECK (action IN (
    'brief_created','brief_extracted','requirement_edited','requirements_confirmed',
    'run_started','run_finished','variant_rejected',
    'constraint_override_added','recalculation_started','decision_recorded',
    'absence_created','absence_cancelled','employee_updated','competency_updated'
));

ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS audit_log_entity_type_check;
ALTER TABLE audit_log ADD CONSTRAINT audit_log_entity_type_check CHECK (entity_type IN (
    'brief','requirement','run','variant','employee','absence','constraint_override'
));
