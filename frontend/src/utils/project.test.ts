import { describe, expect, it } from 'vitest';
import type { Brief, TeamRun, Variant } from '../types';
import { chunkAssignments, plannedAssignments, projectFromRun } from './project';

const run: TeamRun = {
  run_id: 'run_0001',
  brief_id: 'brf_portal01',
  status: 'succeeded',
  preset: 'balanced',
  k: 2,
  seed: 42,
  time_limit_ms: 5000,
  horizon: { start_week: '2026-W08', end_week: '2026-W20' },
  created_at: '2026-11-23T00:00:00Z',
  variants: [],
  reason_codes: [],
};

const brief: Brief = {
  brief_id: run.brief_id,
  title: '  Миграция портала на Go  ',
  source_type: 'text',
  language: 'ru',
  status: 'confirmed',
  created_at: '2026-11-23T00:00:00Z',
  requirement_count: 3,
  needs_review_count: 0,
  reason_codes: [],
};

describe('projectFromRun', () => {
  it('builds a portfolio project from the brief and the run window', () => {
    expect(projectFromRun(run, brief)).toEqual({
      project_id: 'prj_portal01',
      code: 'PRJ-PORTAL01',
      name: 'Миграция портала на Go',
      status: 'planned',
      priority: 3,
      criticality: 'normal',
      start_week: '2026-W08',
      end_week_planned: '2026-W20',
      replaces_other_projects: false,
    });
  });

  it('is deterministic for the same brief so re-approval does not create a duplicate', () => {
    expect(projectFromRun(run, brief).project_id).toBe(projectFromRun(run, brief).project_id);
  });

  it('falls back to the brief id when the brief cannot be loaded', () => {
    expect(projectFromRun(run, null).name).toBe('Проект brf_portal01');
  });

  it('keeps the project id within the contract pattern for short brief ids', () => {
    const shortRun = { ...run, brief_id: 'x' };
    expect(projectFromRun(shortRun, null).project_id).toBe('prj_000x');
  });
});

describe('plannedAssignments', () => {
  const variant = {
    members: [
      { employee_id: 'emp_dev01', role_id: 'backend_engineer' },
      { employee_id: 'emp_qa001', role_id: 'qa_engineer' },
    ],
    weekly_assignments: [
      { employee_id: 'emp_dev01', requirement_id: 'req_1', week: '2026-W10', hours: 20, share: 1 },
      { employee_id: 'emp_dev01', requirement_id: 'req_2', week: '2026-W10', hours: 5, share: 0.25 },
      { employee_id: 'emp_qa001', requirement_id: 'req_3', week: '2026-W10', hours: 8, share: 1 },
      { employee_id: 'emp_ghost', requirement_id: 'req_4', week: '2026-W11', hours: 4, share: 1 },
    ],
  } as unknown as Variant;

  it('maps weekly work to role-bearing plan assignments for the derived project', () => {
    expect(plannedAssignments(variant, 'prj_portal01')).toEqual([
      {
        project_id: 'prj_portal01',
        employee_id: 'emp_dev01',
        role_id: 'backend_engineer',
        week: '2026-W10',
        hours: 25,
        source: 'plan',
      },
      {
        project_id: 'prj_portal01',
        employee_id: 'emp_qa001',
        role_id: 'qa_engineer',
        week: '2026-W10',
        hours: 8,
        source: 'plan',
      },
    ]);
  });

  it('splits large batches within the 500-row endpoint limit', () => {
    expect(chunkAssignments([1, 2, 3, 4, 5], 2)).toEqual([[1, 2], [3, 4], [5]]);
  });
});
