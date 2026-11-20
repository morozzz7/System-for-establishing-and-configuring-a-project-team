/**
 * Tests for the constraint-override helpers: normalization, field validation,
 * duplicate detection and contradictions that must block recalculation.
 */
import { describe, it, expect } from 'vitest';
import type { ConstraintOverride, TeamRun } from '../types';
import {
  EMPLOYEE_ID_PATTERN,
  WEEK_KEY_PATTERN,
  buildRecalculateRequest,
  constraintRows,
  describeOverride,
  draftForType,
  emptyDraft,
  hasBlockingIssues,
  normalizeOverride,
  partiallyUnavailableCandidateIds,
  validateOverride,
  validateOverrides,
} from './constraints';
import type { OverrideDraft, OverrideValidationContext } from './constraints';

const horizon = { start_week: '2026-W07', end_week: '2026-W17' } as const;

const context: OverrideValidationContext = {
  candidateIds: ['emp_0001', 'emp_0002', 'emp_0003', 'emp_0004'],
  unavailableCandidateIds: ['emp_0003'],
  fullyUnavailableCandidateIds: ['emp_0004'],
  horizon,
};

function draft(patch: Partial<OverrideDraft>): OverrideDraft {
  return { ...emptyDraft('force_exclude'), ...patch };
}

describe('normalizeOverride', () => {
  it('trims the reason and drops it when blank', () => {
    expect(normalizeOverride(draft({ type: 'force_exclude', employee_id: ' emp_0001 ', reason: '  уходит  ' }))).toEqual({
      type: 'force_exclude',
      employee_id: 'emp_0001',
      reason: 'уходит',
    });
    expect(normalizeOverride(draft({ type: 'force_exclude', employee_id: 'emp_0001', reason: '   ' }))).toEqual({
      type: 'force_exclude',
      employee_id: 'emp_0001',
    });
  });

  it('dedupes freeze_members ids and removes blank entries', () => {
    const override = normalizeOverride(
      draft({ type: 'freeze_members', employee_ids: ['emp_0001', ' emp_0001 ', '', 'emp_0002'] }),
    );
    expect(override.employee_ids).toEqual(['emp_0001', 'emp_0002']);
  });

  it('encodes the boolean forbid_external constraint as 1 / 0', () => {
    expect(normalizeOverride(draft({ type: 'forbid_external', flag: true }))).toEqual({
      type: 'forbid_external',
      value: 1,
    });
    expect(normalizeOverride(draft({ type: 'forbid_external', flag: false }))).toEqual({
      type: 'forbid_external',
      value: 0,
    });
  });

  it('keeps numeric and week values', () => {
    expect(normalizeOverride(draft({ type: 'max_team_size', value: 4 }))).toEqual({ type: 'max_team_size', value: 4 });
    expect(normalizeOverride(draft({ type: 'deadline', week: ' 2026-W12 ' }))).toEqual({
      type: 'deadline',
      week: '2026-W12',
    });
  });
});

describe('validateOverride', () => {
  it('requires an employee for employee-scoped types', () => {
    const issues = validateOverride({ type: 'force_exclude', employee_id: '   ' }, context);
    expect(issues).toHaveLength(1);
    expect(issues[0].blocking).toBe(true);
    expect(issues[0].field).toBe('employee_id');
  });

  it('rejects malformed pseudonyms', () => {
    expect(hasBlockingIssues(validateOverride({ type: 'force_include', employee_id: 'Иван' }, context))).toBe(true);
    expect(validateOverride({ type: 'force_include', employee_id: 'emp_0001' }, context)).toHaveLength(0);
    expect(EMPLOYEE_ID_PATTERN.test('emp_0001')).toBe(true);
  });

  it('blocks including a candidate who is unavailable for the whole horizon (G1)', () => {
    const issues = validateOverride({ type: 'force_include', employee_id: 'emp_0004' }, context);
    expect(issues).toHaveLength(1);
    expect(issues[0].blocking).toBe(true);
    expect(issues[0].message).toContain('G1');
  });

  it('warns (does not block) when the candidate is partially absent', () => {
    const issues = validateOverride({ type: 'force_include', employee_id: 'emp_0003' }, context);
    expect(issues).toHaveLength(1);
    expect(issues[0].blocking).toBe(false);
    expect(issues[0].message).toContain('не весь горизонт');
  });

  it('warns (does not block) when the employee is outside the snapshot', () => {
    const issues = validateOverride({ type: 'force_include', employee_id: 'emp_9999' }, context);
    expect(issues).toHaveLength(1);
    expect(issues[0].blocking).toBe(false);
  });

  it('validates the week pattern of a deadline', () => {
    expect(WEEK_KEY_PATTERN.test('2026-W14')).toBe(true);
    expect(WEEK_KEY_PATTERN.test('2026-W00')).toBe(false);
    expect(WEEK_KEY_PATTERN.test('2026-W54')).toBe(false);
    expect(WEEK_KEY_PATTERN.test('2026-14')).toBe(false);

    const valid = validateOverride({ type: 'deadline', week: '2026-W14' }, context);
    expect(valid).toHaveLength(0);

    const invalid = validateOverride({ type: 'deadline', week: '2026-14' }, context);
    expect(invalid).toHaveLength(1);
    expect(invalid[0].blocking).toBe(true);

    const beforeHorizon = validateOverride({ type: 'deadline', week: '2026-W01' }, context);
    expect(beforeHorizon).toHaveLength(1);
    expect(beforeHorizon[0].message).toContain('2026-W07');
  });

  it('enforces team size bounds 1..50 and integer values', () => {
    expect(hasBlockingIssues(validateOverride({ type: 'max_team_size', value: 0 }, context))).toBe(true);
    expect(hasBlockingIssues(validateOverride({ type: 'max_team_size', value: 51 }, context))).toBe(true);
    expect(hasBlockingIssues(validateOverride({ type: 'max_team_size', value: 4.5 }, context))).toBe(true);
    expect(validateOverride({ type: 'max_team_size', value: 4 }, context)).toHaveLength(0);
    expect(validateOverride({ type: 'min_team_size', value: 1 }, context)).toHaveLength(0);
    expect(hasBlockingIssues(validateOverride({ type: 'min_team_size', value: undefined }, context))).toBe(true);
  });

  it('enforces the budget and bus-factor ranges', () => {
    expect(validateOverride({ type: 'budget_cap', value: 0 }, context)).toHaveLength(0);
    expect(hasBlockingIssues(validateOverride({ type: 'budget_cap', value: -1 }, context))).toBe(true);
    expect(validateOverride({ type: 'min_bus_factor', value: 2 }, context)).toHaveLength(0);
    expect(hasBlockingIssues(validateOverride({ type: 'min_bus_factor', value: 6 }, context))).toBe(true);
    expect(hasBlockingIssues(validateOverride({ type: 'max_utilization', value: 1.2 }, context))).toBe(true);
    expect(validateOverride({ type: 'max_utilization', value: 0.9 }, context)).toHaveLength(0);
  });

  it('caps the reason at 300 characters', () => {
    const issues = validateOverride(
      { type: 'force_exclude', employee_id: 'emp_0001', reason: 'x'.repeat(301) },
      context,
    );
    expect(issues).toHaveLength(1);
    expect(issues[0].field).toBe('reason');
  });
});

describe('validateOverrides', () => {
  it('flags a duplicate employee constraint', () => {
    const overrides: ConstraintOverride[] = [
      { type: 'force_exclude', employee_id: 'emp_0001' },
      { type: 'force_exclude', employee_id: 'emp_0001' },
    ];
    const issues = validateOverrides(overrides, context);
    const duplicate = issues.find((issue) => issue.message.startsWith('Дубликат'));
    expect(duplicate).toBeDefined();
    expect(duplicate?.index).toBe(1);
    expect(duplicate?.blocking).toBe(true);
  });

  it('flags a repeated scalar constraint of the same type', () => {
    const issues = validateOverrides(
      [
        { type: 'deadline', week: '2026-W12' },
        { type: 'deadline', week: '2026-W13' },
      ],
      context,
    );
    expect(issues.some((issue) => issue.message.startsWith('Дубликат') && issue.index === 1)).toBe(true);
  });

  it('allows the same type for different employees', () => {
    const issues = validateOverrides(
      [
        { type: 'force_exclude', employee_id: 'emp_0001' },
        { type: 'force_exclude', employee_id: 'emp_0002' },
      ],
      context,
    );
    expect(issues).toHaveLength(0);
  });

  it('blocks including and excluding the same employee', () => {
    const issues = validateOverrides(
      [
        { type: 'force_include', employee_id: 'emp_0001' },
        { type: 'force_exclude', employee_id: 'emp_0001' },
      ],
      context,
    );
    expect(hasBlockingIssues(issues)).toBe(true);
    expect(issues.some((issue) => issue.message.includes('одновременно'))).toBe(true);
  });

  it('blocks a team size limit below the number of pinned people', () => {
    const issues = validateOverrides(
      [
        { type: 'freeze_members', employee_ids: ['emp_0001', 'emp_0002'] },
        { type: 'max_team_size', value: 1 },
      ],
      context,
    );
    expect(hasBlockingIssues(issues)).toBe(true);
    expect(issues.some((issue) => issue.message.includes('G4'))).toBe(true);
  });

  it('blocks min_team_size above max_team_size', () => {
    const issues = validateOverrides(
      [
        { type: 'min_team_size', value: 5 },
        { type: 'max_team_size', value: 3 },
      ],
      context,
    );
    expect(hasBlockingIssues(issues)).toBe(true);
  });

  it('accepts a coherent override list', () => {
    const issues = validateOverrides(
      [
        { type: 'force_exclude', employee_id: 'emp_0001', reason: 'другой проект' },
        { type: 'max_team_size', value: 4 },
        { type: 'deadline', week: '2026-W12' },
        { type: 'forbid_external', value: 1 },
      ],
      context,
    );
    expect(issues).toHaveLength(0);
  });
});

describe('drafts and presentation helpers', () => {
  it('keeps the reason when the type changes and resets type-specific fields', () => {
    const next = draftForType('deadline', draft({ type: 'max_team_size', value: 4, reason: 'срок заказчика' }));
    expect(next.type).toBe('deadline');
    expect(next.reason).toBe('срок заказчика');
    expect(next.value).toBeNull();
    expect(next.week).toBe('');
  });

  it('describes overrides in Russian for the list', () => {
    expect(describeOverride({ type: 'force_exclude', employee_id: 'emp_0001' })).toBe('Исключить emp_0001');
    expect(describeOverride({ type: 'max_team_size', value: 4 })).toBe('Не более 4 человек');
    expect(describeOverride({ type: 'deadline', week: '2026-W12' })).toBe('Срок 2026-W12');
    expect(describeOverride({ type: 'max_utilization', value: 0.9 })).toBe('Загрузка ≤ 90%');
  });

  it('flattens current run constraints into rows', () => {
    expect(constraintRows(undefined)).toHaveLength(0);
    const rows = constraintRows({
      max_team_size: 5,
      deadline_week: '2026-W14',
      forbid_external: true,
      force_exclude: ['emp_0001'],
    });
    expect(rows.map((row) => row.key)).toEqual(['force_exclude', 'max_team_size', 'deadline', 'forbid_external']);
    expect(rows.find((row) => row.key === 'forbid_external')?.value).toBe('да');
  });

  it('detects candidates whose capacity does not cover the horizon', () => {
    const ids = partiallyUnavailableCandidateIds(
      [
        { employee_id: 'emp_0001', capacity_weeks: 11 },
        { employee_id: 'emp_0002', capacity_weeks: 10 },
      ],
      horizon,
    );
    expect(ids).toEqual(['emp_0002']);
  });
});

describe('buildRecalculateRequest', () => {
  it('carries the run preset, seed, k and the selected base variant', () => {
    const run = {
      run_id: 'run_m0001',
      brief_id: 'brf_0001',
      status: 'succeeded',
      preset: 'economical',
      k: 2,
      seed: 42,
      time_limit_ms: 5000,
      horizon,
      created_at: '2026-11-17T10:00:00Z',
      variants: [],
      reason_codes: [],
    } satisfies TeamRun;
    const request = buildRecalculateRequest(run, 'var_m0001a', [{ type: 'max_team_size', value: 4 }]);
    expect(request).toEqual({
      base_variant_id: 'var_m0001a',
      preset: 'economical',
      k: 2,
      seed: 42,
      time_limit_ms: 5000,
      overrides: [{ type: 'max_team_size', value: 4 }],
    });
  });
});
