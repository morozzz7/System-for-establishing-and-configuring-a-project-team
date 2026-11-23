import { describe, expect, it } from 'vitest';
import type { Brief, TeamRun } from '../types';
import { projectFromRun } from './project';

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
