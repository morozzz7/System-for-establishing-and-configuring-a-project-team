/**
 * Mock recalculation responsiveness (METRICS 3.6): a constraint edit must change the result
 * accordingly, and a no-op edit must be reported as RECALC_NO_EFFECT with measured facts.
 */
import { beforeEach, describe, expect, it } from 'vitest';
import {
  createRun,
  getRun,
  getRunInputs,
  getVariant,
  getVariantExplanation,
  recalculateRun,
  resetMockState,
} from './client';
import { MOCK_ABSENT_EMPLOYEES, MOCK_HORIZON } from '../mocks/runs';
import type { TeamRun } from '../types';

async function primaryMembers(run: TeamRun): Promise<string[]> {
  const variant = await getVariant(run.run_id, run.variants[0].variant_id);
  expect(variant).not.toBeNull();
  return (variant?.members ?? []).map((member) => member.employee_id);
}

describe('mock runs', () => {
  beforeEach(() => {
    resetMockState();
  });

  it('creates two variants with members, metric values, duration and deficits', async () => {
    const run = await createRun('brf_test');

    expect(run.status).toBe('succeeded');
    expect(run.variants).toHaveLength(2);
    expect(run.horizon).toEqual(MOCK_HORIZON);

    for (const summary of run.variants) {
      expect(summary.team_size).toBeGreaterThan(0);
      expect(summary.metric_values.M1).toBeGreaterThan(0);
      expect(summary.metric_values.score).toBeGreaterThan(0);
      expect(summary.duration.p50_weeks).toBeGreaterThan(0);
      expect(summary.duration.p80_weeks).toBeGreaterThanOrEqual(summary.duration.p50_weeks);
      expect(summary.reason_codes.length).toBeGreaterThan(0);

      const variant = await getVariant(run.run_id, summary.variant_id);
      expect(variant).not.toBeNull();
      expect(variant?.members).toHaveLength(summary.team_size);
      expect(variant?.weekly_assignments.length).toBeGreaterThan(0);
      expect(variant?.deficits).toHaveLength(summary.deficit_count);
      expect(variant?.gate_violations.G1).toBe(0);
      expect(variant?.gate_violations.G2).toBe(0);
    }

    // Variant rosters differ (METRICS variant-set rule: Jaccard distance >= 0.25).
    const first = await primaryMembers(run);
    const second = (await getVariant(run.run_id, run.variants[1].variant_id))?.members ?? [];
    const shared = first.filter((id) => second.some((member) => member.employee_id === id));
    const union = new Set([...first, ...second.map((member) => member.employee_id)]).size;
    expect(1 - shared.length / union).toBeGreaterThanOrEqual(0.25);
  });

  it('exposes an immutable input snapshot with constraints and candidates', async () => {
    const run = await createRun('brf_test');
    const inputs = await getRunInputs(run.run_id);

    expect(inputs).not.toBeNull();
    expect(inputs?.run_id).toBe(run.run_id);
    expect(inputs?.snapshot_sha256).toMatch(/^[a-f0-9]{64}$/);
    expect(inputs?.candidates.length).toBe(inputs?.candidate_count);
    expect(inputs?.requirement_count).toBeGreaterThan(0);
    expect(inputs?.weights.M1).toBeGreaterThan(0);
    // Absent employees have fewer available weeks than the horizon (G1 source).
    const absent = inputs?.candidates.find((candidate) => candidate.employee_id === MOCK_ABSENT_EMPLOYEES[0]);
    expect(absent?.capacity_weeks).toBeLessThan(11);
  });

  it('force_exclude removes the member and changes the metrics', async () => {
    const run = await createRun('brf_test');
    const base = run.variants[0];
    const before = await primaryMembers(run);
    const target = before[0];

    const response = await recalculateRun(run.run_id, {
      base_variant_id: base.variant_id,
      overrides: [{ type: 'force_exclude', employee_id: target, reason: 'Уходит на другой проект' }],
    });

    expect(response.reason_codes).toEqual(['RECALC_APPLIED']);
    expect(response.applied_overrides).toBe(1);
    expect(response.rejected_overrides).toHaveLength(0);
    expect(response.parent_run_id).toBe(run.run_id);
    expect(response.facts?.changed_members).toBeGreaterThan(0);
    expect(response.facts?.roster_jaccard).toBeGreaterThan(0);

    const derived = await getRun(response.run_id);
    expect(derived).not.toBeNull();
    expect(derived?.parent_run_id).toBe(run.run_id);

    const after = await primaryMembers(derived as TeamRun);
    expect(after).not.toContain(target);
    expect(after.length).toBe(before.length - 1);

    const derivedVariant = await getVariant((derived as TeamRun).run_id, (derived as TeamRun).variants[0].variant_id);
    const baseVariant = await getVariant(run.run_id, base.variant_id);
    expect(derivedVariant?.score).not.toBe(baseVariant?.score);
    expect(derivedVariant?.duration.p50_weeks).not.toBe(baseVariant?.duration.p50_weeks);

    // The parent run stays immutable.
    const parentAgain = await getRun(run.run_id);
    expect(parentAgain?.variants[0].team_size).toBe(base.team_size);
    expect(await primaryMembers(parentAgain as TeamRun)).toEqual(before);
  });

  it('force_include adds a member', async () => {
    const run = await createRun('brf_test');
    const before = await primaryMembers(run);
    const inputs = await getRunInputs(run.run_id);
    const candidate = inputs?.candidates.find(
      (item) => !before.includes(item.employee_id) && item.capacity_weeks >= 11,
    );
    expect(candidate).toBeDefined();

    const response = await recalculateRun(run.run_id, {
      base_variant_id: run.variants[0].variant_id,
      overrides: [{ type: 'force_include', employee_id: candidate?.employee_id ?? '' }],
    });

    expect(response.reason_codes).toContain('RECALC_APPLIED');
    expect(response.applied_overrides).toBe(1);
    const derived = (await getRun(response.run_id)) as TeamRun;
    const after = await primaryMembers(derived);
    expect(after).toContain(candidate?.employee_id);
    expect(after.length).toBe(before.length + 1);
  });

  it('shrinks the roster for max_team_size and changes P50', async () => {
    const run = await createRun('brf_test');
    const before = await primaryMembers(run);
    const response = await recalculateRun(run.run_id, {
      base_variant_id: run.variants[0].variant_id,
      overrides: [{ type: 'max_team_size', value: 3 }],
    });

    expect(response.reason_codes).toEqual(['RECALC_APPLIED']);
    const derived = (await getRun(response.run_id)) as TeamRun;
    const variant = await getVariant(derived.run_id, derived.variants[0].variant_id);
    expect(variant?.team_size).toBe(3);
    expect(variant?.team_size).toBeLessThan(before.length);
    expect((response.facts?.p50_weeks_after as number) ?? 0).toBeGreaterThan(
      (response.facts?.p50_weeks_before as number) ?? 0,
    );
  });

  it('applies a deadline without changing the roster', async () => {
    const run = await createRun('brf_test');
    const before = await primaryMembers(run);
    const response = await recalculateRun(run.run_id, {
      base_variant_id: run.variants[0].variant_id,
      overrides: [{ type: 'deadline', week: '2026-W12' }],
    });

    expect(response.reason_codes).toEqual(['RECALC_APPLIED']);
    expect(response.facts?.changed_members).toBe(0);
    const derived = (await getRun(response.run_id)) as TeamRun;
    const variant = await getVariant(derived.run_id, derived.variants[0].variant_id);
    expect(await primaryMembers(derived)).toEqual(before);
    expect(variant?.duration.deadline_week).toBe('2026-W12');
    expect(variant?.duration.p_on_time).toBeDefined();
  });

  it('reports RECALC_NO_EFFECT with facts for a no-op override', async () => {
    const run = await createRun('brf_test');
    const response = await recalculateRun(run.run_id, {
      base_variant_id: run.variants[0].variant_id,
      overrides: [{ type: 'forbid_external', value: 1 }],
    });

    expect(response.reason_codes).toEqual(['RECALC_NO_EFFECT']);
    expect(response.applied_overrides).toBe(1);
    expect(response.rejected_overrides).toHaveLength(0);
    expect(response.facts).toMatchObject({ changed_members: 0, roster_jaccard: 0 });
    expect(response.facts?.p50_weeks_before).toBe(response.facts?.p50_weeks_after);
  });

  it('rejects force_include of an unavailable employee with a reason code', async () => {
    expect(MOCK_ABSENT_EMPLOYEES.length).toBeGreaterThan(0);
    const run = await createRun('brf_test');
    const response = await recalculateRun(run.run_id, {
      base_variant_id: run.variants[0].variant_id,
      overrides: [{ type: 'force_include', employee_id: MOCK_ABSENT_EMPLOYEES[0] }],
    });

    expect(response.applied_overrides).toBe(0);
    expect(response.rejected_overrides).toHaveLength(1);
    expect(response.rejected_overrides[0].index).toBe(0);
    expect(response.rejected_overrides[0].reason_codes).toContain('OVERRIDE_VIOLATES_G1');
    expect(response.reason_codes).toEqual(['RECALC_NO_EFFECT']);
  });

  it('rejects a force_exclude conflicting with force_include', async () => {
    const run = await createRun('brf_test');
    const target = (await primaryMembers(run))[0];
    const response = await recalculateRun(run.run_id, {
      base_variant_id: run.variants[0].variant_id,
      overrides: [
        { type: 'force_include', employee_id: target, reason: 'нужен опыт' },
        { type: 'force_exclude', employee_id: target, reason: 'конфликт' },
      ],
    });

    expect(response.applied_overrides).toBe(1);
    expect(response.rejected_overrides).toHaveLength(1);
    expect(response.rejected_overrides[0].reason_codes).toContain('OVERRIDE_VIOLATES_G4');
  });

  it('rejects an empty override list with a validation problem', async () => {
    const run = await createRun('brf_test');
    await expect(
      recalculateRun(run.run_id, { base_variant_id: run.variants[0].variant_id, overrides: [] }),
    ).rejects.toMatchObject({ code: 'VALIDATION_FAILED', status: 422 });
  });

  it('is deterministic for the same input', async () => {
    const first = await createRun('brf_test');
    const firstResponse = await recalculateRun(first.run_id, {
      base_variant_id: first.variants[0].variant_id,
      overrides: [{ type: 'max_team_size', value: 4 }],
    });
    const second = await createRun('brf_test');
    const secondResponse = await recalculateRun(second.run_id, {
      base_variant_id: second.variants[0].variant_id,
      overrides: [{ type: 'max_team_size', value: 4 }],
    });
    expect(secondResponse.reason_codes).toEqual(firstResponse.reason_codes);
    const factKeys = [
      'changed_members',
      'roster_jaccard',
      'p50_weeks_before',
      'p50_weeks_after',
      'overrides_applied',
      'overrides_rejected',
      'seed',
    ];
    for (const key of factKeys) {
      expect(secondResponse.facts?.[key]).toEqual(firstResponse.facts?.[key]);
    }
  });

  it('builds a faithful explanation consistent with the variant members and deficits', async () => {
    const run = await createRun('brf_test');
    const variant = run.variants[0];

    const explanation = await getVariantExplanation(run.run_id, variant.variant_id);
    expect(explanation).not.toBeNull();
    expect(explanation?.locale).toBe('ru');
    expect(explanation?.variant_id).toBe(variant.variant_id);
    expect(explanation?.run_id).toBe(run.run_id);
    expect(explanation?.headline.length).toBeGreaterThan(0);
    expect(explanation?.headline_reason_codes.length).toBeGreaterThan(0);
    expect(explanation?.tradeoff.text.length).toBeGreaterThan(0);

    // Included factors mirror the member count exactly (coverage / explainability).
    const detail = await getVariant(run.run_id, variant.variant_id);
    expect(detail).not.toBeNull();
    expect(explanation?.included.length).toBe(detail?.members.length);
    expect(explanation?.deficits.length).toBe(detail?.deficits.length);

    // Every included factor is faithful: numbers in the text come from its facts / the member.
    const memberIds = new Set(detail?.members.map((member) => member.employee_id) ?? []);
    for (const factor of explanation?.included ?? []) {
      expect(factor.employee_id).toBeDefined();
      expect(memberIds.has(factor.employee_id as string)).toBe(true);
      expect(factor.reason_codes.length).toBeGreaterThan(0);
      // Reason codes stay a subset of the member's own reason codes.
      const member = detail?.members.find((item) => item.employee_id === factor.employee_id);
      for (const code of factor.reason_codes) {
        expect(member?.reason_codes).toContain(code);
      }
      // allocated hours are reproduced verbatim in the text.
      if (member) expect(factor.text).toContain(`${member.allocated_hours} ч`);
    }

    // Deficits factors cite the same person-days as the variant deficits.
    for (const factor of explanation?.deficits ?? []) {
      const deficit = detail?.deficits.find((item) => item.competency_id === factor.competency_id);
      if (deficit) {
        expect(factor.text).toContain(`${deficit.person_days} чел.-дн.`);
      }
    }

    // A fresh run without a parent emits only "estimated" counterfactuals (no fabricated rerun).
    expect(explanation?.counterfactuals.length).toBeGreaterThan(0);
    for (const counterfactual of explanation?.counterfactuals ?? []) {
      expect(counterfactual.verified_by).toBe('estimated');
    }
  });

  it('marks counterfactuals from a re-run as verified_by=rerun with a run_id', async () => {
    const run = await createRun('brf_test');
    const response = await recalculateRun(run.run_id, {
      base_variant_id: run.variants[0].variant_id,
      overrides: [{ type: 'force_exclude', employee_id: (await primaryMembers(run))[0], reason: 'Уходит' }],
    });

    const child = (await getRun(response.run_id)) as TeamRun;
    const explanation = await getVariantExplanation(child.run_id, child.variants[0].variant_id);
    expect(explanation).not.toBeNull();

    const rerun = explanation?.counterfactuals.find((item) => item.verified_by === 'rerun');
    expect(rerun).toBeDefined();
    expect(rerun?.run_id).toBe(child.run_id);
    expect(explanation?.run_id).toBe(child.run_id);
  });
});
