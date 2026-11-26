-- Migration 00012: audit actions for project lifecycle writes.
-- 00008 added project_created/assignment_created but project updates and deletions
-- still fell back to employee_updated, which hid them in the audit trail.

-- +goose Up

ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS audit_log_action_check;
ALTER TABLE audit_log ADD CONSTRAINT audit_log_action_check CHECK (action IN (
    'brief_created','brief_extracted','requirement_edited','requirements_confirmed',
    'run_started','run_finished','variant_rejected',
    'constraint_override_added','recalculation_started','decision_recorded',
    'absence_created','absence_cancelled','employee_updated','competency_updated',
    'employee_created','project_created','assignment_created','assignment_deleted',
    'employee_deleted','project_updated','project_deleted'
));

-- +goose Down

ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS audit_log_action_check;
ALTER TABLE audit_log ADD CONSTRAINT audit_log_action_check CHECK (action IN (
    'brief_created','brief_extracted','requirement_edited','requirements_confirmed',
    'run_started','run_finished','variant_rejected',
    'constraint_override_added','recalculation_started','decision_recorded',
    'absence_created','absence_cancelled','employee_updated','competency_updated',
    'employee_created','project_created','assignment_created','assignment_deleted'
));
