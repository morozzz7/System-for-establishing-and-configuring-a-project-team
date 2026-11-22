/**
 * Deterministic mock run engine for the frontend-only development mode.
 *
 * It mirrors the contract (contracts/openapi.yaml: TeamRun, Variant, RunInputs, Recalculate*)
 * and the semantics of ADR-007 §3: a recalculation creates a NEW run derived from the parent,
 * reports `applied_overrides` / `rejected_overrides` and always returns measured `facts`.
 *
 * Everything here is deterministic: no Math.random, no Date.now in values, fixed timestamps and
 * fixed-seed id sequences. Numbers are illustrative but internally consistent (they are derived
 * from the roster, grades and cost coefficients of the mock employee pool).
 */
import type {
  ActiveConstraint,
  ConstraintOverride,
  ConstraintType,
  Counterfactual,
  Deficit,
  Employee,
  Explanation,
  ExplanationFactor,
  Grade,
  Horizon,
  MetricDetails,
  MetricValues,
  PresetName,
  ReasonCode,
  RecalculateRequest,
  RecalculateResponse,
  RejectedOverride,
  RunConstraints,
  RunInputCandidate,
  RunInputs,
  TeamRun,
  Tradeoff,
  Variant,
  VariantMember,
  VariantSetDiagnostics,
  VariantSummary,
  WeeklyAssignment,
  Weights,
  WeekKey,
} from '../types';
import { ApiError } from '../api/errors';
import { mockAbsences } from './data';
import { mockEmployees, mockRoles, mockWeightPresets } from './reference';
import { parseWeek, weekStart } from '../utils/week';

// ──── constants ────

export const MOCK_HORIZON: Horizon = { start_week: '2026-W07', end_week: '2026-W17' };
export const MOCK_PRESET: PresetName = 'balanced';
export const MOCK_SEED = 42;
export const MOCK_K = 2;
export const MOCK_TIME_LIMIT_MS = 5000;

const HOURS_PER_DAY = 8;
const WORKING_DAYS_PER_WEEK = 5;
/** Productive days a person delivers per week after reserve/meetings (ADR-002 reserve_ratio 0.2). */
const PRODUCTIVE_DAYS_PER_WEEK = 4;
const TOTAL_EFFORT_PERSON_DAYS = 180;
const MIN_JACCARD_DISTANCE = 0.25;
const MOCK_START_TIMESTAMP = '2026-11-17T10:00:00Z';

const GRADE_LEVEL: Record<Grade, number> = { J: 2, M: 3, S: 4, L: 5, P: 5 };

interface MockRequirement {
  requirement_id: string;
  role_id: string;
  competency_id: string;
  competency_name_ru: string;
  level: number;
  criticality: 'must' | 'should' | 'nice';
  person_days: number;
}

const MOCK_REQUIREMENTS: MockRequirement[] = [
  { requirement_id: 'req_m001', role_id: 'backend_engineer', competency_id: 'lang_go', competency_name_ru: 'Go', level: 4, criticality: 'must', person_days: 60 },
  { requirement_id: 'req_m002', role_id: 'frontend_engineer', competency_id: 'lang_typescript', competency_name_ru: 'TypeScript', level: 4, criticality: 'must', person_days: 45 },
  { requirement_id: 'req_m003', role_id: 'qa_engineer', competency_id: 'qa_test_design', competency_name_ru: 'Тест-дизайн', level: 3, criticality: 'should', person_days: 30 },
  { requirement_id: 'req_m004', role_id: 'devops_engineer', competency_id: 'cicd_gitlab', competency_name_ru: 'CI/CD', level: 3, criticality: 'should', person_days: 30 },
  { requirement_id: 'req_m005', role_id: 'business_analyst', competency_id: 'an_requirements', competency_name_ru: 'Анализ требований', level: 3, criticality: 'nice', person_days: 15 },
];

const CONSTRAINT_REASON_CODES: Record<ConstraintType, ReasonCode> = {
  force_include: 'CONSTRAINT_ACTIVE_FORCE_INCLUDE',
  force_exclude: 'CONSTRAINT_ACTIVE_FORCE_EXCLUDE',
  freeze_members: 'CONSTRAINT_ACTIVE_FORCE_INCLUDE',
  max_team_size: 'CONSTRAINT_ACTIVE_TEAM_SIZE',
  min_team_size: 'CONSTRAINT_ACTIVE_TEAM_SIZE',
  budget_cap: 'CONSTRAINT_ACTIVE_BUDGET',
  deadline: 'CONSTRAINT_ACTIVE_DEADLINE',
  min_bus_factor: 'CONSTRAINT_ACTIVE_BUS_FACTOR',
  forbid_external: 'CONSTRAINT_BINDING_CAPACITY',
  max_utilization: 'CONSTRAINT_BINDING_CAPACITY',
};

// ──── small deterministic helpers ────

function round1(value: number): number {
  return Math.round(value * 10) / 10;
}

function round3(value: number): number {
  return Math.round(value * 1000) / 1000;
}

function clamp(value: number, min = 0, max = 1): number {
  return Math.min(max, Math.max(min, value));
}

function mean(values: number[]): number {
  if (values.length === 0) return 0;
  return values.reduce((acc, v) => acc + v, 0) / values.length;
}

function unique(values: string[]): string[] {
  return [...new Set(values)];
}

/** Deterministic 64-hex digest (mock stand-in for sha256 of the canonical snapshot JSON). */
function hashHex(input: string): string {
  let h1 = 0x811c9dc5;
  let h2 = 0x01000193;
  for (let i = 0; i < input.length; i += 1) {
    h1 = Math.imul(h1 ^ input.charCodeAt(i), 16777619) >>> 0;
    h2 = (h2 + Math.imul(h1 ^ (i + 1), 2654435761)) >>> 0;
  }
  let seed = (h1 ^ h2) >>> 0;
  let out = '';
  for (let i = 0; i < 8; i += 1) {
    seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0;
    out += seed.toString(16).padStart(8, '0');
  }
  return out.slice(0, 64);
}

/** ISO-8601 week key of a calendar date (Monday-based, same rule as utils/week.ts). */
function weekKeyFromDate(date: Date): WeekKey {
  const dayNum = date.getUTCDay() || 7;
  const thursday = new Date(date.getTime());
  thursday.setUTCDate(date.getUTCDate() + 4 - dayNum);
  const yearStart = new Date(Date.UTC(thursday.getUTCFullYear(), 0, 1));
  const week = Math.ceil(((thursday.getTime() - yearStart.getTime()) / 86400000 + 1) / 7);
  return `${thursday.getUTCFullYear()}-W${String(week).padStart(2, '0')}`;
}

function addWeeks(week: WeekKey, amount: number): WeekKey {
  const parsed = parseWeek(week);
  if (!parsed) return week;
  const monday = weekStart(parsed.year, parsed.week);
  monday.setUTCDate(monday.getUTCDate() + amount * 7);
  return weekKeyFromDate(monday);
}

/** Inclusive list of week keys of the planning horizon. */
function horizonWeekList(horizon: Horizon): WeekKey[] {
  const weeks: WeekKey[] = [];
  let cursor = horizon.start_week;
  for (let i = 0; i < 60 && cursor <= horizon.end_week; i += 1) {
    weeks.push(cursor);
    cursor = addWeeks(cursor, 1);
  }
  return weeks;
}

/** Duration (in weeks) from the horizon start until `week`, inclusive; 1-based. */
function weeksUntil(horizon: Horizon, week: WeekKey): number {
  const start = parseWeek(horizon.start_week);
  const target = parseWeek(week);
  if (!start || !target) return horizonWeekList(horizon).length;
  const diff = Math.round(
    (weekStart(target.year, target.week).getTime() - weekStart(start.year, start.week).getTime()) / (7 * 86400000),
  );
  return Math.max(1, diff + 1);
}

// ──── candidate pool / availability ────

/** Week keys inside the horizon touched by an absence (G1 source of truth in the real backend). */
function absenceWeeksOf(employeeId: string): Set<WeekKey> {
  const horizonWeeks = horizonWeekList(MOCK_HORIZON);
  const inHorizon = new Set<WeekKey>();
  for (const absence of mockAbsences) {
    if (absence.employee_id !== employeeId) continue;
    const start = new Date(`${absence.start_date}T00:00:00Z`);
    const end = new Date(`${absence.end_date}T00:00:00Z`);
    for (let cursor = start; cursor <= end; cursor.setUTCDate(cursor.getUTCDate() + 1)) {
      const week = weekKeyFromDate(new Date(cursor.getTime()));
      if (horizonWeeks.includes(week)) inHorizon.add(week);
    }
  }
  return inHorizon;
}

export interface MockCandidate {
  employee: Employee;
  capacity_weeks: number;
  absent_weeks: WeekKey[];
}

function buildPool(): MockCandidate[] {
  const horizonWeeks = horizonWeekList(MOCK_HORIZON).length;
  return mockEmployees
    .filter((employee) => employee.active)
    .map((employee) => {
      const absent = [...absenceWeeksOf(employee.employee_id)];
      return {
        employee,
        capacity_weeks: Math.max(0, horizonWeeks - absent.length),
        absent_weeks: absent,
      };
    })
    .sort((a, b) => a.employee.employee_id.localeCompare(b.employee.employee_id));
}

const pool = buildPool();
const poolById = new Map(pool.map((candidate) => [candidate.employee.employee_id, candidate]));

/** Employees with no available capacity at all in the mock horizon (G1 rejection case). */
export const MOCK_ABSENT_EMPLOYEES: string[] = pool
  .filter((candidate) => candidate.capacity_weeks === 0)
  .map((candidate) => candidate.employee.employee_id);

function employeeById(employeeId: string): Employee | undefined {
  return poolById.get(employeeId)?.employee;
}

// ──── roster building ────

type RosterStrategy = 'capability' | 'economy';

function levelOf(employee: Employee): number {
  return GRADE_LEVEL[employee.grade] ?? 3;
}

function compareCapability(a: MockCandidate, b: MockCandidate): number {
  const byLevel = levelOf(b.employee) - levelOf(a.employee);
  if (byLevel !== 0) return byLevel;
  const byCapacity = b.capacity_weeks - a.capacity_weeks;
  if (byCapacity !== 0) return byCapacity;
  return a.employee.employee_id.localeCompare(b.employee.employee_id);
}

function compareEconomy(a: MockCandidate, b: MockCandidate): number {
  const byCost = a.employee.cost_coefficient - b.employee.cost_coefficient;
  if (byCost !== 0) return byCost;
  if (a.employee.bench_flag !== b.employee.bench_flag) return a.employee.bench_flag ? -1 : 1;
  const byCapacity = b.capacity_weeks - a.capacity_weeks;
  if (byCapacity !== 0) return byCapacity;
  const byLevel = levelOf(b.employee) - levelOf(a.employee);
  if (byLevel !== 0) return byLevel;
  return a.employee.employee_id.localeCompare(b.employee.employee_id);
}

/** One member per requirement, chosen deterministically by the strategy; fully absent people are skipped (G1). */
function buildStrategyRoster(strategy: RosterStrategy): string[] {
  const compare = strategy === 'capability' ? compareCapability : compareEconomy;
  const chosen: string[] = [];
  for (const requirement of MOCK_REQUIREMENTS) {
    const candidates = pool
      .filter(
        (candidate) =>
          candidate.capacity_weeks > 0 &&
          candidate.employee.role_profile_id === requirement.role_id &&
          !chosen.includes(candidate.employee.employee_id),
      )
      .sort(compare);
    if (candidates.length > 0) chosen.push(candidates[0].employee.employee_id);
  }
  return chosen;
}

function rosterCost(roster: string[]): number {
  const size = Math.max(1, roster.length);
  const coefficientSum = roster.reduce(
    (acc, id) => acc + (employeeById(id)?.cost_coefficient ?? 1),
    0,
  );
  return round1((TOTAL_EFFORT_PERSON_DAYS * HOURS_PER_DAY * coefficientSum) / size);
}

function countHighLevel(roster: string[]): number {
  return roster.filter((id) => {
    const employee = employeeById(id);
    return employee ? GRADE_LEVEL[employee.grade] >= 4 : false;
  }).length;
}

/** Apply the constraint layer to a roster (single source of truth for both create and recalc). */
function resolveRoster(base: string[], constraints: RunConstraints): string[] {
  const excluded = new Set(constraints.force_exclude ?? []);
  const pinned = unique([...(constraints.force_include ?? []), ...(constraints.freeze_members ?? [])]).filter(
    (id) => poolById.has(id) && !excluded.has(id),
  );
  let roster = base.filter((id) => poolById.has(id) && !excluded.has(id));
  for (const id of pinned) {
    if (!roster.includes(id)) roster.push(id);
  }

  const minSize = constraints.min_team_size ?? 0;
  if (roster.length < minSize) {
    for (const candidate of pool) {
      if (roster.length >= minSize) break;
      const id = candidate.employee.employee_id;
      if (candidate.capacity_weeks > 0 && !roster.includes(id) && !excluded.has(id)) {
        roster.push(id);
      }
    }
  }

  const busFactor = constraints.min_bus_factor ?? 0;
  if (busFactor > 0) {
    const ranked = pool
      .filter((candidate) => candidate.capacity_weeks > 0)
      .sort(compareCapability);
    for (const candidate of ranked) {
      if (countHighLevel(roster) >= busFactor) break;
      if (typeof constraints.max_team_size === 'number' && roster.length >= constraints.max_team_size) break;
      const id = candidate.employee.employee_id;
      if (!roster.includes(id) && !excluded.has(id)) roster.push(id);
    }
  }

  const budgetCap = constraints.budget_cap_units;
  if (typeof budgetCap === 'number') {
    for (let guard = 0; guard < 50; guard += 1) {
      const cost = rosterCost(roster);
      if (cost <= budgetCap || roster.length <= 1) break;
      const removable = roster
        .filter((id) => !pinned.includes(id))
        .map((id) => ({ id, cost: rosterCost(roster.filter((other) => other !== id)) }))
        .sort((a, b) => a.cost - b.cost || a.id.localeCompare(b.id))[0];
      if (!removable || removable.cost >= cost) break;
      roster = roster.filter((id) => id !== removable.id);
    }
  }

  const maxSize = constraints.max_team_size;
  if (typeof maxSize === 'number') {
    for (let guard = 0; guard < 50 && roster.length > maxSize; guard += 1) {
      const removable = [...roster].reverse().find((id) => !pinned.includes(id));
      if (!removable) break;
      roster = roster.filter((id) => id !== removable);
    }
  }

  return roster;
}

// ──── metrics ────

interface RosterStats {
  roster: string[];
  size: number;
  avgLevel: number;
  cost: number;
  p50: number;
  p80: number;
  benchCount: number;
  highLevelCount: number;
  nonBenchCount: number;
}

function rosterStats(roster: string[]): RosterStats {
  const employees = roster
    .map((id) => employeeById(id))
    .filter((employee): employee is Employee => employee !== undefined);
  const size = employees.length;
  const avgLevel = size > 0 ? mean(employees.map(levelOf)) : 0;
  const speedFactor = 0.85 + 0.05 * avgLevel;
  const p50 = round1(TOTAL_EFFORT_PERSON_DAYS / Math.max(1, size * PRODUCTIVE_DAYS_PER_WEEK * speedFactor));
  const p80 = round1(p50 * 1.25);
  const benchCount = employees.filter((employee) => employee.bench_flag).length;
  return {
    roster,
    size,
    avgLevel,
    cost: rosterCost(roster),
    p50,
    p80,
    benchCount,
    highLevelCount: employees.filter((employee) => GRADE_LEVEL[employee.grade] >= 4).length,
    nonBenchCount: size - benchCount,
  };
}

interface MetricContext {
  weights: Weights;
  bestP50: number;
  minCost: number;
  maxCost: number;
  deadlineWeeks?: number;
  maxUtilizationRatio?: number;
}

const METRIC_KEYS = ['M1', 'M2', 'M3', 'M5', 'M6', 'M7', 'M8', 'M9', 'M10', 'M11', 'M12'] as const;

function compositeScore(values: MetricValues, weights: Weights): number {
  let numerator = 0;
  let denominator = 0;
  for (const key of METRIC_KEYS) {
    const weight = weights[key];
    if (typeof weight !== 'number' || weight <= 0) continue;
    numerator += weight * values[key];
    denominator += weight;
  }
  if (denominator === 0) return 0;
  return round3(numerator / denominator);
}

function computeMetrics(stats: RosterStats, context: MetricContext): MetricValues {
  const { avgLevel, size, cost, p50 } = stats;
  const deadlineWeeks = context.deadlineWeeks;
  let m5: number;
  if (typeof deadlineWeeks === 'number') {
    const sigma = Math.max(0.05, (stats.p80 - stats.p50) / 0.8416);
    const z = (deadlineWeeks - p50) / sigma;
    m5 = clamp(1 / (1 + Math.exp(-1.7 * z)));
  } else {
    m5 = clamp(context.bestP50 / Math.max(0.1, p50));
  }
  const costRange = context.maxCost - context.minCost;
  const m6 = costRange <= 0 ? 1 : clamp(1 - (cost - context.minCost) / costRange);
  let m7 = clamp(1 - 0.015 * size - 0.01 * avgLevel);
  if (typeof context.maxUtilizationRatio === 'number') {
    m7 = clamp(m7 + (0.95 - context.maxUtilizationRatio) * 0.5);
  }
  const values: MetricValues = {
    M1: round3(clamp(0.62 + 0.06 * avgLevel)),
    M2: round3(clamp(0.42 + 0.05 * avgLevel + 0.03 * size)),
    M3: round3(clamp(0.55 + 0.08 * avgLevel)),
    M5: round3(m5),
    M6: round3(m6),
    M7: round3(m7),
    M8: round3(clamp(1 - 0.06 * stats.nonBenchCount)),
    M9: round3(clamp((stats.highLevelCount + 1) / 4)),
    M10: round3(clamp(0.95 - 0.01 * Math.max(0, size - 3))),
    M11: round3(size > 0 ? stats.benchCount / size : 0),
    M12: round3(clamp(0.45 + 0.08 * avgLevel + 0.02 * size)),
    score: 0,
  };
  values.score = compositeScore(values, context.weights);
  return values;
}

function metricDetails(stats: RosterStats, context: MetricContext): MetricDetails {
  return {
    must_coverage: round3(clamp(0.7 + 0.06 * stats.avgLevel)),
    overqualified_share: round3(clamp(stats.highLevelCount / Math.max(1, stats.size))),
    peak_utilization: 0.92,
    mean_utilization: round3(0.6 + 0.03 * stats.size),
    min_bus_factor: stats.highLevelCount,
    single_points_of_failure: stats.size === 1 ? stats.roster : [],
    n_opt: Math.max(2, stats.size),
    bench_hours_assigned: round1(stats.benchCount * 20 * stats.p50),
    bench_hours_available: round1(Math.max(1, stats.size) * 40 * stats.p50),
    cost_min_units: round1(context.minCost),
    cost_ref_units: round1(context.maxCost),
    t_best_weeks: round1(context.bestP50),
    portfolio_slip_days: round1(2 * stats.nonBenchCount),
    dropped_metrics: ['M4'],
  };
}

function buildDeficits(roster: string[], p50: number): Deficit[] {
  const deficits: Deficit[] = [];
  for (const requirement of MOCK_REQUIREMENTS) {
    if (requirement.criticality === 'nice') continue;
    const matching = roster
      .map((id) => employeeById(id))
      .filter((employee): employee is Employee => employee !== undefined && employee.role_profile_id === requirement.role_id);
    const covered = matching.reduce(
      (acc, employee) => acc + p50 * PRODUCTIVE_DAYS_PER_WEEK * clamp(levelOf(employee) / requirement.level),
      0,
    );
    const deficit = round1(requirement.person_days - covered);
    if (deficit <= 0) continue;
    deficits.push({
      competency_id: requirement.competency_id,
      competency_name_ru: requirement.competency_name_ru,
      requirement_ids: [requirement.requirement_id],
      person_days: deficit,
      weeks: round1(deficit / (PRODUCTIVE_DAYS_PER_WEEK * Math.max(1, roster.length))),
      external_cooperation_flag: matching.length === 0 || deficit > requirement.person_days * 0.25,
      recommended_actions: [
        {
          action: matching.length === 0 ? 'hire' : 'contractor',
          estimated_cost_units: round1(deficit * 2.2),
          time_to_start_weeks: matching.length === 0 ? 4 : 2,
          reason_codes: ['DEFICIT_EXTERNAL_CONTRACTOR'],
        },
      ],
    });
  }
  return deficits;
}

function buildActiveConstraints(constraints: RunConstraints): ActiveConstraint[] {
  const result: ActiveConstraint[] = [];
  const add = (type: ConstraintType, value: ActiveConstraint['value']) => {
    result.push({ type, value, source: 'user', reason_codes: [CONSTRAINT_REASON_CODES[type]] });
  };
  if (constraints.force_include?.length) add('force_include', constraints.force_include);
  if (constraints.force_exclude?.length) add('force_exclude', constraints.force_exclude);
  if (constraints.freeze_members?.length) add('freeze_members', constraints.freeze_members);
  if (typeof constraints.max_team_size === 'number') add('max_team_size', constraints.max_team_size);
  if (typeof constraints.min_team_size === 'number') add('min_team_size', constraints.min_team_size);
  if (typeof constraints.budget_cap_units === 'number') add('budget_cap', constraints.budget_cap_units);
  if (constraints.deadline_week) add('deadline', constraints.deadline_week);
  if (typeof constraints.forbid_external === 'boolean') add('forbid_external', constraints.forbid_external);
  if (typeof constraints.min_bus_factor === 'number') add('min_bus_factor', constraints.min_bus_factor);
  if (typeof constraints.max_utilization_ratio === 'number') add('max_utilization', constraints.max_utilization_ratio);
  return result;
}

function buildWeeklyAssignments(roster: string[], p50: number, horizon: Horizon): WeeklyAssignment[] {
  const weeks = horizonWeekList(horizon).slice(0, Math.max(1, Math.ceil(p50)));
  const perMemberHours = round1((TOTAL_EFFORT_PERSON_DAYS * HOURS_PER_DAY) / Math.max(1, roster.length));
  const perWeekHours = Math.min(40, round1(perMemberHours / Math.max(1, weeks.length)));
  const rows: WeeklyAssignment[] = [];
  roster.forEach((employeeId, memberIndex) => {
    const absenceWeeks = new Set(absenceWeeksOf(employeeId));
    const requirement = MOCK_REQUIREMENTS[Math.min(memberIndex, MOCK_REQUIREMENTS.length - 1)];
    for (const week of weeks) {
      if (absenceWeeks.has(week)) continue;
      rows.push({
        employee_id: employeeId,
        requirement_id: requirement.requirement_id,
        week,
        hours: perWeekHours,
        share: round3(clamp(perWeekHours / (WORKING_DAYS_PER_WEEK * HOURS_PER_DAY))),
      });
    }
  });
  return rows;
}

function buildMembers(roster: string[], constraints: RunConstraints): VariantMember[] {
  const forced = new Set([
    ...(constraints.force_include ?? []),
    ...(constraints.freeze_members ?? []),
  ]);
  return roster.map((employeeId) => {
    const employee = employeeById(employeeId);
    const requirement = MOCK_REQUIREMENTS.find((item) => item.role_id === employee?.role_profile_id);
    const reasonCodes: ReasonCode[] = ['INCLUDE_REQUIRED_ROLE'];
    if (employee?.bench_flag) reasonCodes.push('INCLUDE_BENCH');
    if (forced.has(employeeId)) reasonCodes.push('INCLUDE_USER_FORCE');
    if (employee && levelOf(employee) >= 4) reasonCodes.push('INCLUDE_HIGH_COMPETENCE');
    return {
      employee_id: employeeId,
      role_id: employee?.role_profile_id ?? 'backend_engineer',
      grade: employee?.grade ?? 'M',
      allocated_hours: round1((TOTAL_EFFORT_PERSON_DAYS * HOURS_PER_DAY) / Math.max(1, roster.length)),
      assignment_share: round3(clamp(0.55 + 0.05 * (employee ? levelOf(employee) : 3))),
      competence_fit: round3(requirement && employee ? clamp(levelOf(employee) / requirement.level) : 0.7),
      reason_codes: reasonCodes,
    };
  });
}

// ──── run records ────

export interface MockRunRecord {
  run: TeamRun;
  inputs: RunInputs;
  variants: Map<string, Variant>;
  constraints: RunConstraints;
  briefId: string;
}

const runRecords = new Map<string, MockRunRecord>();
let runSequence = 0;

/** Reset module state (tests + explicit UI reset in mock mode). */
export function resetMockRuns(): void {
  runRecords.clear();
  runSequence = 0;
}

function nextRunIds(): { runId: string; snapshotId: string } {
  runSequence += 1;
  const suffix = String(runSequence).padStart(4, '0');
  return { runId: `run_m${suffix}`, snapshotId: `snp_m${suffix}` };
}

function timestampFor(sequence: number): string {
  const base = new Date(MOCK_START_TIMESTAMP).getTime();
  return new Date(base + sequence * 60_000).toISOString();
}

function weightsForPreset(preset: PresetName): Weights {
  const found = mockWeightPresets.find((item) => item.preset === preset);
  return found ? { ...found.weights } : { ...mockWeightPresets[2].weights };
}

interface RosterPlan {
  roster: string[];
  label: string;
  purpose: ReasonCode;
}

interface BuildRunParams {
  runId: string;
  snapshotId: string;
  briefId: string;
  preset: PresetName;
  k: number;
  seed: number;
  timeLimitMs: number;
  horizon: Horizon;
  constraints: RunConstraints;
  plans: RosterPlan[];
  createdAt: string;
  parentRunId?: string;
  weights?: Weights;
}

function buildVariantSetDiagnostics(variants: Variant[]): VariantSetDiagnostics {
  const pairwise: Record<string, number> = {};
  let minDistance = 1;
  for (let i = 0; i < variants.length; i += 1) {
    for (let j = i + 1; j < variants.length; j += 1) {
      const left = new Set(variants[i].members.map((m) => m.employee_id));
      const right = new Set(variants[j].members.map((m) => m.employee_id));
      const intersection = [...left].filter((id) => right.has(id)).length;
      const union = new Set([...left, ...right]).size;
      const distance = union === 0 ? 0 : round3(1 - intersection / union);
      pairwise[`${variants[i].variant_id}|${variants[j].variant_id}`] = distance;
      minDistance = Math.min(minDistance, distance);
    }
  }
  return {
    pairwise_jaccard: pairwise,
    min_pairwise_jaccard: variants.length > 1 ? minDistance : 1,
    dominated_variant_ids: [],
    gate_failed_variant_ids: [],
    diversity_rule_ok: variants.length > 1 ? minDistance >= MIN_JACCARD_DISTANCE : true,
  };
}

function buildRunRecord(params: BuildRunParams): MockRunRecord {
  const weights = params.weights ?? weightsForPreset(params.preset);
  const deadlineWeeks = params.constraints.deadline_week
    ? weeksUntil(params.horizon, params.constraints.deadline_week)
    : undefined;
  const plans = params.plans.filter((plan) => plan.roster.length > 0);
  if (plans.length === 0) {
    throw new ApiError({
      status: 409,
      code: 'SOLVER_INFEASIBLE',
      detail: 'Не удалось собрать состав команды при заданных ограничениях.',
      reasonCodes: ['SOLVER_INFEASIBLE'],
    });
  }
  const statsList = plans.map((plan) => rosterStats(plan.roster));
  const metricContext: MetricContext = {
    weights,
    bestP50: Math.min(...statsList.map((stats) => stats.p50)),
    minCost: Math.min(...statsList.map((stats) => stats.cost)),
    maxCost: Math.max(...statsList.map((stats) => stats.cost)),
    deadlineWeeks,
    maxUtilizationRatio: params.constraints.max_utilization_ratio,
  };

  const variants: Variant[] = plans.map((plan, index) => {
    const stats = statsList[index];
    const variantId = `var_${params.runId.replace('run_', '')}${index === 0 ? 'a' : 'b'}`;
    const metricValues = computeMetrics(stats, metricContext);
    const deficits = buildDeficits(stats.roster, stats.p50);
    const reasonCodes: ReasonCode[] = [plan.purpose];
    for (const constraint of buildActiveConstraints(params.constraints)) {
      if (!reasonCodes.includes(constraint.reason_codes[0])) reasonCodes.push(constraint.reason_codes[0]);
    }
    return {
      variant_id: variantId,
      run_id: params.runId,
      label: plan.label,
      gate_pass: true,
      gate_violations: { G1: 0, G2: 0, G3: 0, G4: 0, G5: 0, max_overload_pct: 0, uncovered_must_not_in_deficit: 0 },
      score: metricValues.score,
      team_size: stats.size,
      cost_units: stats.cost,
      duration: {
        p50_weeks: stats.p50,
        p80_weeks: stats.p80,
        p50_working_days: Math.round(stats.p50 * WORKING_DAYS_PER_WEEK),
        p80_working_days: Math.round(stats.p80 * WORKING_DAYS_PER_WEEK),
        ...(params.constraints.deadline_week
          ? {
              deadline_week: params.constraints.deadline_week,
              p_on_time: metricValues.M5,
              slack_weeks: round1(((deadlineWeeks ?? stats.p80) - stats.p80) / WORKING_DAYS_PER_WEEK),
            }
          : {}),
        monte_carlo: { runs: 2000, seed: params.seed },
      },
      metric_values: metricValues,
      metric_details: metricDetails(stats, metricContext),
      members: buildMembers(stats.roster, params.constraints),
      weekly_assignments: buildWeeklyAssignments(stats.roster, stats.p50, params.horizon),
      deficits,
      active_constraints: buildActiveConstraints(params.constraints),
      excluded_candidates: (params.constraints.force_exclude ?? []).map((employeeId) => ({
        employee_id: employeeId,
        reason_codes: ['EXCLUDE_USER_FORCE'] as ReasonCode[],
      })),
      reason_codes: reasonCodes,
    };
  });

  const variantSet = buildVariantSetDiagnostics(variants);
  variants.forEach((variant) => {
    const diversity: Record<string, number> = {};
    for (const other of variants) {
      if (other.variant_id === variant.variant_id) continue;
      diversity[other.variant_id] = variantSet.pairwise_jaccard?.[`${variant.variant_id}|${other.variant_id}`]
        ?? variantSet.pairwise_jaccard?.[`${other.variant_id}|${variant.variant_id}`]
        ?? 0;
    }
    variant.diversity_jaccard = diversity;
  });

  const summaries: VariantSummary[] = variants.map((variant) => ({
    variant_id: variant.variant_id,
    label: variant.label,
    gate_pass: variant.gate_pass,
    score: variant.score,
    team_size: variant.team_size,
    duration: variant.duration,
    cost_units: variant.cost_units,
    metric_values: variant.metric_values,
    deficit_count: variant.deficits.length,
    reason_codes: variant.reason_codes,
  }));

  const candidates: RunInputCandidate[] = pool.map((candidate) => ({
    employee_id: candidate.employee.employee_id,
    grade: candidate.employee.grade,
    cost_coefficient: candidate.employee.cost_coefficient,
    competency_ids: MOCK_REQUIREMENTS.filter(
      (requirement) => requirement.role_id === candidate.employee.role_profile_id,
    ).map((requirement) => requirement.competency_id),
    capacity_digest: hashHex(`${candidate.employee.employee_id}:${candidate.capacity_weeks}`),
    capacity_weeks: candidate.capacity_weeks,
  }));

  const snapshotSource = JSON.stringify({
    run_id: params.runId,
    horizon: params.horizon,
    constraints: params.constraints,
    plans: plans.map((plan) => plan.roster),
    candidates: candidates.map((candidate) => [candidate.employee_id, candidate.capacity_weeks]),
  });

  const inputs: RunInputs = {
    run_id: params.runId,
    snapshot_id: params.snapshotId,
    created_at: params.createdAt,
    snapshot_sha256: hashHex(snapshotSource),
    horizon: params.horizon,
    weights,
    constraints: params.constraints,
    requirement_count: MOCK_REQUIREMENTS.length,
    candidate_count: candidates.length,
    capacity_rows: candidates.reduce((acc, candidate) => acc + candidate.capacity_weeks, 0),
    candidates,
  };

  const run: TeamRun = {
    run_id: params.runId,
    brief_id: params.briefId,
    ...(params.parentRunId ? { parent_run_id: params.parentRunId } : {}),
    snapshot_id: params.snapshotId,
    status: 'succeeded',
    preset: params.preset,
    k: params.k,
    seed: params.seed,
    time_limit_ms: params.timeLimitMs,
    horizon: params.horizon,
    created_at: params.createdAt,
    finished_at: params.createdAt,
    diagnostics: {
      solver_status: 'FEASIBLE',
      solve_time_ms: 420 + variants.length * 30,
      ml_status: 'ok',
      ml_attempts: 1,
      validator_rejected: 0,
      fallback_used: false,
      metrics_version: 'm1-m12@2026-10',
      contract_version: 'v1',
    },
    variants: summaries,
    variant_set: variantSet,
    reason_codes: variants.flatMap((variant) => variant.reason_codes).slice(0, 3),
  };

  const record: MockRunRecord = {
    run,
    inputs,
    variants: new Map(variants.map((variant) => [variant.variant_id, variant])),
    constraints: params.constraints,
    briefId: params.briefId,
  };
  runRecords.set(run.run_id, record);
  return record;
}

// ──── public mock API ────

/** Create a run with the two baseline strategies (capability-first and economy/bench-first). */
export function createMockRun(briefId: string): TeamRun {
  const { runId, snapshotId } = nextRunIds();
  const record = buildRunRecord({
    runId,
    snapshotId,
    briefId,
    preset: MOCK_PRESET,
    k: MOCK_K,
    seed: MOCK_SEED,
    timeLimitMs: MOCK_TIME_LIMIT_MS,
    horizon: MOCK_HORIZON,
    constraints: {},
    createdAt: timestampFor(runSequence),
    plans: [
      {
        roster: resolveRoster(buildStrategyRoster('capability'), {}),
        label: 'Сбалансированный / приоритет компетенций',
        purpose: 'VARIANT_BALANCED',
      },
      {
        roster: resolveRoster(buildStrategyRoster('economy'), {}),
        label: 'Экономичный / ставка на bench',
        purpose: 'VARIANT_CHEAPEST',
      },
    ],
  });
  return record.run;
}

export function getMockRun(runId: string): TeamRun | null {
  return runRecords.get(runId)?.run ?? null;
}

export function getMockVariant(runId: string, variantId: string): Variant | null {
  return runRecords.get(runId)?.variants.get(variantId) ?? null;
}

export function getMockRunInputs(runId: string): RunInputs | null {
  return runRecords.get(runId)?.inputs ?? null;
}

// ──── explanation (GET /runs/{runId}/variants/{variantId}/explanation) ────

/** Role name map from the mock taxonomy (ids are pseudonyms, no personal data). */
const MOCK_ROLE_NAMES: Record<string, string> = Object.fromEntries(
  mockRoles.map((role) => [role.role_id, role.name_ru]),
);

const MOCK_GRADE_LABEL: Record<Grade, string> = { J: 'Джуниор', M: 'Мидл', S: 'Сеньор', L: 'Лид', P: 'Принципал' };

/**
 * Deterministic explanation for a mock variant (METRICS 3.5 faithfulness by construction):
 * every number in the Russian `text` is re-derived from the emitted `facts` and the variant's
 * own members / deficits / excluded candidates / duration — no LLM, no prose without a source.
 */
export function getMockExplanation(runId: string, variantId: string): Explanation | null {
  const record = runRecords.get(runId);
  const variant = record?.variants.get(variantId);
  if (!record || !variant) return null;
  const run = record.run;

  // Included: one factor per member, texts built strictly from member/employee facts.
  const included: ExplanationFactor[] = variant.members.map((member) => {
    const employee = employeeById(member.employee_id);
    const level = employee ? levelOf(employee) : 3;
    const requirement = MOCK_REQUIREMENTS.find((item) => item.role_id === member.role_id);
    const facts: Record<string, unknown> = {
      employee_id: member.employee_id,
      role_id: member.role_id,
      role_name_ru: MOCK_ROLE_NAMES[member.role_id] ?? member.role_id,
      grade: member.grade,
      grade_label_ru: MOCK_GRADE_LABEL[member.grade],
      level,
      required_level: requirement?.level ?? 3,
      competence_fit: member.competence_fit,
      allocated_hours: member.allocated_hours,
      bench: employee?.bench_flag ?? false,
    };
    const levelText =
      level >= (requirement?.level ?? 4)
        ? `уровень ${level} ≥ требуемого ${requirement?.level ?? 4}`
        : `уровень ${level} (требуется ${requirement?.level ?? 4})`;
    const benchText = employee?.bench_flag ? ', на скамейке' : '';
    const text =
      `${MOCK_ROLE_NAMES[member.role_id] ?? member.role_id} ${member.employee_id}: грейд ` +
      `${MOCK_GRADE_LABEL[member.grade]} (${member.grade}), ${levelText}, ` +
      `соответствие ${Math.round(member.competence_fit * 100)}%, ${member.allocated_hours} ч${benchText}.`;
    return { employee_id: member.employee_id, text, reason_codes: member.reason_codes, facts };
  });

  // Excluded: user-forced exclusions plus a deterministic absent bench example (facts only).
  const excluded: ExplanationFactor[] = [];
  for (const candidate of variant.excluded_candidates ?? []) {
    const employee = employeeById(candidate.employee_id);
    const facts: Record<string, unknown> = {
      employee_id: candidate.employee_id,
      role_id: employee?.role_profile_id,
      absent: candidate.reason_codes.includes('EXCLUDE_USER_FORCE') ? false : undefined,
    };
    const text = `${candidate.employee_id}: исключён пользователем.`;
    excluded.push({ employee_id: candidate.employee_id, text, reason_codes: candidate.reason_codes, facts });
  }
  // Show one absent bench candidate that was not selected (reproducible from the pool).
  const absentBench = MOCK_ABSENT_EMPLOYEES.find((id) => !variant.members.some((member) => member.employee_id === id));
  if (absentBench) {
    const employee = employeeById(absentBench);
    const facts: Record<string, unknown> = {
      employee_id: absentBench,
      role_id: employee?.role_profile_id,
      role_name_ru: MOCK_ROLE_NAMES[employee?.role_profile_id ?? ''] ?? employee?.role_profile_id,
      absent_weeks: absenceWeeksOf(absentBench),
    };
    excluded.push({
      employee_id: absentBench,
      text: `${absentBench}: недоступен в горизонте планирования (отпуск/отсутствие) — назначения нарушили бы G1.`,
      reason_codes: ['EXCLUDE_ABSENT', 'EXCLUDE_NO_FREE_CAPACITY'],
      facts,
    });
  }

  // Deficits: one factor per deficit derived from variant.deficits.
  const deficits: ExplanationFactor[] = variant.deficits.map((deficit) => {
    const facts: Record<string, unknown> = {
      competency_id: deficit.competency_id,
      competency_name_ru: deficit.competency_name_ru,
      person_days: deficit.person_days,
      weeks: deficit.weeks ?? 0,
      external_cooperation_flag: deficit.external_cooperation_flag,
    };
    const cooperation =
      deficit.external_cooperation_flag ? ' — нужна внешняя кооперация' : ' — закрывается внутри команды';
    const text =
      `Недостаточно «${deficit.competency_name_ru}»: ${deficit.person_days} чел.-дн. ` +
      `(~${deficit.weeks ?? 0} нед.)${cooperation}.`;
    return {
      competency_id: deficit.competency_id,
      text,
      reason_codes: deficit.recommended_actions[0]?.reason_codes ?? ['DEFICIT_MUST_ROLE'],
      facts,
    };
  });

  // Counterfactuals: an estimated team-size "what-if" plus (for re-run runs) a rerun-verified one.
  const counterfactuals: Counterfactual[] = [];
  const deltaWeeks = round1(variant.duration.p50_weeks * 0.12);
  counterfactuals.push({
    text: `Если снять ограничение по размеру команды, P50 снизится примерно на ${deltaWeeks.toFixed(1)} нед.`,
    reason_codes: ['COUNTERFACTUAL_TEAM_SIZE'],
    verified_by: 'estimated',
    facts: { p50_weeks: variant.duration.p50_weeks, delta_weeks: deltaWeeks },
  });
  if (run.parent_run_id) {
    counterfactuals.push({
      text: `Пересчёт (${run.parent_run_id}) подтвердил: ограничение ${run.variants[0]?.reason_codes?.[0] ?? 'CONSTRAINT_ACTIVE_TEAM_SIZE'} изменило состав.`,
      reason_codes: ['COUNTERFACTUAL_OVERRIDE'],
      verified_by: 'rerun',
      run_id: run.run_id,
      facts: { parent_run_id: run.parent_run_id, changed_members: variant.team_size },
    });
  }

  // Tradeoff: derived from the variant purpose + duration/cost facts.
  const costVsSkill = variant.label.includes('Эконом');
  const tradeoff: Tradeoff = {
    text: costVsSkill
      ? `Экономичность оплачена компетентностью: при P50 ${variant.duration.p50_weeks.toFixed(1)} нед. дефицитов ${variant.deficits.length}.`
      : `Скорость/компетенции при P50 ${variant.duration.p50_weeks.toFixed(1)} нед. стоят ${Math.round(variant.cost_units)} у.е. и оставляют ${variant.deficits.length} дефицита.`,
    reason_codes: costVsSkill ? ['TRADEOFF_COST_VS_SKILL'] : ['TRADEOFF_LOAD_VS_SPEED'],
    facts: {
      p50_weeks: variant.duration.p50_weeks,
      cost_units: variant.cost_units,
      team_size: variant.team_size,
      deficit_count: variant.deficits.length,
    },
  };

  const headline =
    `${variant.label}: состав ${variant.team_size} чел., P50 ${variant.duration.p50_weeks.toFixed(1)} нед. / ` +
    `P80 ${variant.duration.p80_weeks.toFixed(1)} нед., стоимость ${Math.round(variant.cost_units)} у.е., ` +
    `дефицитов ${variant.deficits.length}.`;
  const headlineReasonCodes: ReasonCode[] = [variant.reason_codes[0] ?? 'VARIANT_BALANCED'];

  return {
    variant_id: variant.variant_id,
    run_id: variant.run_id,
    locale: 'ru',
    headline,
    headline_reason_codes: headlineReasonCodes,
    tradeoff,
    included,
    excluded,
    deficits,
    counterfactuals,
  };
}

interface OverrideApplication {
  constraints: RunConstraints;
  rejected: RejectedOverride[];
  appliedIndex: Map<ConstraintType, number>;
}

const OVERRIDE_TYPES: ConstraintType[] = [
  'force_include',
  'force_exclude',
  'max_team_size',
  'min_team_size',
  'budget_cap',
  'deadline',
  'forbid_external',
  'freeze_members',
  'min_bus_factor',
  'max_utilization',
];

const WEEK_KEY_PATTERN = /^\d{4}-W(0[1-9]|[1-4][0-9]|5[0-3])$/;
const EMPLOYEE_ID_PATTERN = /^emp_[a-z0-9]{4,12}$/;

function reject(index: number, reasonCodes: ReasonCode[], detail: string): RejectedOverride {
  return { index, reason_codes: reasonCodes, detail };
}

/** Apply the override layer field by field; invalid entries are rejected individually (ADR-007 §3). */
function applyOverrides(parent: RunConstraints, overrides: ConstraintOverride[], horizon: Horizon): OverrideApplication {
  const constraints: RunConstraints = {
    ...parent,
    force_include: [...(parent.force_include ?? [])],
    force_exclude: [...(parent.force_exclude ?? [])],
    freeze_members: [...(parent.freeze_members ?? [])],
  };
  const rejected: RejectedOverride[] = [];
  const appliedIndex = new Map<ConstraintType, number>();
  const absent = new Set(MOCK_ABSENT_EMPLOYEES);

  overrides.forEach((override, index) => {
    if (!OVERRIDE_TYPES.includes(override.type)) {
      rejected.push(reject(index, ['VALIDATION_FAILED'], `Неизвестный тип ограничения: ${String(override.type)}`));
      return;
    }
    const setConstraint = () => appliedIndex.set(override.type, index);
    switch (override.type) {
      case 'force_include': {
        const employeeId = override.employee_id;
        if (!employeeId || !EMPLOYEE_ID_PATTERN.test(employeeId)) {
          rejected.push(reject(index, ['VALIDATION_FAILED'], 'Некорректный employee_id'));
          return;
        }
        if (!poolById.has(employeeId)) {
          rejected.push(reject(index, ['OVERRIDE_REJECTED'], `${employeeId} отсутствует в снимке входа`));
          return;
        }
        if (absent.has(employeeId)) {
          rejected.push(
            reject(index, ['OVERRIDE_VIOLATES_G1'], `${employeeId} недоступен в горизонте планирования`),
          );
          return;
        }
        if (constraints.force_exclude?.includes(employeeId)) {
          rejected.push(reject(index, ['OVERRIDE_VIOLATES_G4'], `${employeeId} уже принудительно исключён`));
          return;
        }
        constraints.force_include = unique([...(constraints.force_include ?? []), employeeId]);
        setConstraint();
        return;
      }
      case 'force_exclude': {
        const employeeId = override.employee_id;
        if (!employeeId || !EMPLOYEE_ID_PATTERN.test(employeeId)) {
          rejected.push(reject(index, ['VALIDATION_FAILED'], 'Некорректный employee_id'));
          return;
        }
        if (!poolById.has(employeeId)) {
          rejected.push(reject(index, ['OVERRIDE_REJECTED'], `${employeeId} отсутствует в снимке входа`));
          return;
        }
        if (constraints.force_include?.includes(employeeId)) {
          rejected.push(reject(index, ['OVERRIDE_VIOLATES_G4'], `${employeeId} уже принудительно включён`));
          return;
        }
        constraints.force_exclude = unique([...(constraints.force_exclude ?? []), employeeId]);
        constraints.freeze_members = (constraints.freeze_members ?? []).filter((id) => id !== employeeId);
        setConstraint();
        return;
      }
      case 'freeze_members': {
        const ids = (override.employee_ids ?? []).filter((id) => EMPLOYEE_ID_PATTERN.test(id));
        if (ids.length === 0) {
          rejected.push(reject(index, ['VALIDATION_FAILED'], 'Не указаны сотрудники для фиксации'));
          return;
        }
        const unknown = ids.filter((id) => !poolById.has(id));
        if (unknown.length > 0) {
          rejected.push(reject(index, ['OVERRIDE_REJECTED'], `Нет в снимке входа: ${unknown.join(', ')}`));
          return;
        }
        if (ids.some((id) => constraints.force_exclude?.includes(id))) {
          rejected.push(reject(index, ['OVERRIDE_VIOLATES_G4'], 'Сотрудник одновременно исключён'));
          return;
        }
        constraints.freeze_members = unique([...(constraints.freeze_members ?? []), ...ids]);
        setConstraint();
        return;
      }
      case 'max_team_size': {
        const value = override.value;
        if (typeof value !== 'number' || !Number.isInteger(value) || value < 1 || value > 50) {
          rejected.push(reject(index, ['VALIDATION_FAILED'], 'Размер команды должен быть целым числом 1..50'));
          return;
        }
        const pinned = unique([
          ...(constraints.force_include ?? []),
          ...(constraints.freeze_members ?? []),
        ]).filter((id) => !(constraints.force_exclude ?? []).includes(id));
        if (value < pinned.length) {
          rejected.push(
            reject(index, ['OVERRIDE_VIOLATES_G4'], `Зафиксировано ${pinned.length} сотрудников, лимит ${value}`),
          );
          return;
        }
        if (typeof constraints.min_team_size === 'number' && value < constraints.min_team_size) {
          rejected.push(reject(index, ['OVERRIDE_VIOLATES_G4'], 'Лимит меньше минимального размера команды'));
          return;
        }
        constraints.max_team_size = value;
        setConstraint();
        return;
      }
      case 'min_team_size': {
        const value = override.value;
        if (typeof value !== 'number' || !Number.isInteger(value) || value < 1 || value > 50) {
          rejected.push(reject(index, ['VALIDATION_FAILED'], 'Размер команды должен быть целым числом 1..50'));
          return;
        }
        if (typeof constraints.max_team_size === 'number' && value > constraints.max_team_size) {
          rejected.push(reject(index, ['OVERRIDE_VIOLATES_G4'], 'Минимум больше максимального размера команды'));
          return;
        }
        constraints.min_team_size = value;
        setConstraint();
        return;
      }
      case 'budget_cap': {
        const value = override.value;
        if (typeof value !== 'number' || !Number.isFinite(value) || value < 0) {
          rejected.push(reject(index, ['VALIDATION_FAILED'], 'Бюджет должен быть неотрицательным числом'));
          return;
        }
        constraints.budget_cap_units = value;
        setConstraint();
        return;
      }
      case 'deadline': {
        const week = override.week;
        if (!week || !WEEK_KEY_PATTERN.test(week)) {
          rejected.push(reject(index, ['VALIDATION_FAILED'], 'Срок должен быть в формате YYYY-Www'));
          return;
        }
        if (week < horizon.start_week) {
          rejected.push(reject(index, ['OVERRIDE_REJECTED'], 'Срок раньше начала горизонта планирования'));
          return;
        }
        constraints.deadline_week = week;
        setConstraint();
        return;
      }
      case 'forbid_external': {
        const value = override.value;
        if (value !== 0 && value !== 1) {
          rejected.push(reject(index, ['VALIDATION_FAILED'], 'forbid_external принимает 1 или 0'));
          return;
        }
        constraints.forbid_external = value === 1;
        setConstraint();
        return;
      }
      case 'min_bus_factor': {
        const value = override.value;
        if (typeof value !== 'number' || !Number.isInteger(value) || value < 1 || value > 5) {
          rejected.push(reject(index, ['VALIDATION_FAILED'], 'Bus factor должен быть целым числом 1..5'));
          return;
        }
        constraints.min_bus_factor = value;
        setConstraint();
        return;
      }
      case 'max_utilization': {
        const value = override.value;
        if (typeof value !== 'number' || value < 0.5 || value > 1) {
          rejected.push(reject(index, ['VALIDATION_FAILED'], 'Загрузка должна быть в диапазоне 0.5..1'));
          return;
        }
        constraints.max_utilization_ratio = value;
        setConstraint();
        return;
      }
      default:
        rejected.push(reject(index, ['VALIDATION_FAILED'], 'Неподдерживаемый тип ограничения'));
    }
  });

  return { constraints, rejected, appliedIndex };
}

function jaccardDistance(left: string[], right: string[]): number {
  const leftSet = new Set(left);
  const rightSet = new Set(right);
  const intersection = [...leftSet].filter((id) => rightSet.has(id)).length;
  const union = new Set([...leftSet, ...rightSet]).size;
  if (union === 0) return 0;
  return round3(1 - intersection / union);
}

function changedMembers(left: string[], right: string[]): number {
  const leftSet = new Set(left);
  const rightSet = new Set(right);
  const removed = [...leftSet].filter((id) => !rightSet.has(id)).length;
  const added = [...rightSet].filter((id) => !leftSet.has(id)).length;
  return removed + added;
}

/**
 * Recalculate a run from its stored snapshot: a NEW run with explicit lineage (ADR-007 §3).
 * Returns the same `RecalculateResponse` the Go core will return, including `facts`.
 */
export function recalculateMockRun(runId: string, request: RecalculateRequest): RecalculateResponse {
  const parentRecord = runRecords.get(runId);
  if (!parentRecord) {
    throw new ApiError({
      status: 404,
      code: 'NOT_FOUND',
      detail: `Запуск ${runId} не найден`,
      reasonCodes: [],
    });
  }
  if (!Array.isArray(request.overrides) || request.overrides.length === 0) {
    throw new ApiError({
      status: 422,
      code: 'VALIDATION_FAILED',
      detail: 'Нужно хотя бы одно ограничение',
      reasonCodes: ['VALIDATION_FAILED'],
    });
  }
  if (request.overrides.length > 30) {
    throw new ApiError({
      status: 422,
      code: 'VALIDATION_FAILED',
      detail: 'Не более 30 ограничений за один пересчёт',
      reasonCodes: ['VALIDATION_FAILED'],
    });
  }

  const parentRun = parentRecord.run;
  const horizon = parentRun.horizon;
  const baseVariantId = request.base_variant_id ?? parentRun.variants[0]?.variant_id;
  const baseVariant = baseVariantId ? parentRecord.variants.get(baseVariantId) : undefined;
  if (!baseVariant) {
    throw new ApiError({
      status: 404,
      code: 'NOT_FOUND',
      detail: 'Базовый вариант не найден в этом запуске',
      reasonCodes: [],
    });
  }

  const { constraints, rejected, appliedIndex } = applyOverrides(
    parentRecord.constraints,
    request.overrides,
    horizon,
  );

  const baseRoster = baseVariant.members.map((member) => member.employee_id);
  const strategy: RosterStrategy = baseVariant.label.includes('Эконом') ? 'economy' : 'capability';
  const alternate: RosterStrategy = strategy === 'capability' ? 'economy' : 'capability';

  let effectiveConstraints: RunConstraints = constraints;
  let primary = resolveRoster(baseRoster, effectiveConstraints);
  let secondary = resolveRoster(buildStrategyRoster(alternate), effectiveConstraints);

  const budgetIndex = appliedIndex.get('budget_cap');
  const budgetCap = effectiveConstraints.budget_cap_units;
  if (typeof budgetCap === 'number' && rosterCost(primary) > budgetCap) {
    if (typeof budgetIndex === 'number') {
      rejected.push(
        reject(
          budgetIndex,
          ['OVERRIDE_VIOLATES_G4'],
          'Минимальный работоспособный состав превышает бюджет',
        ),
      );
    }
    effectiveConstraints = { ...effectiveConstraints, budget_cap_units: undefined };
    primary = resolveRoster(baseRoster, effectiveConstraints);
    secondary = resolveRoster(buildStrategyRoster(alternate), effectiveConstraints);
  }

  const minSizeIndex = appliedIndex.get('min_team_size');
  if (typeof effectiveConstraints.min_team_size === 'number' && primary.length < effectiveConstraints.min_team_size) {
    if (typeof minSizeIndex === 'number') {
      rejected.push(
        reject(minSizeIndex, ['OVERRIDE_VIOLATES_G4'], 'Недостаточно доступных кандидатов для минимального состава'),
      );
    }
    effectiveConstraints = { ...effectiveConstraints, min_team_size: undefined };
    primary = resolveRoster(baseRoster, effectiveConstraints);
    secondary = resolveRoster(buildStrategyRoster(alternate), effectiveConstraints);
  }

  const { runId: nextRunId, snapshotId } = nextRunIds();  const plans: RosterPlan[] = [
    {
      roster: primary,
      label: 'Пересчёт / основной состав',
      purpose: strategy === 'capability' ? 'VARIANT_BALANCED' : 'VARIANT_CHEAPEST',
    },
  ];
  const secondaryDiffers =
    secondary.length !== primary.length || secondary.some((id) => !primary.includes(id));
  if (secondaryDiffers) {
    plans.push({
      roster: secondary,
      label: 'Пересчёт / альтернатива',
      purpose: strategy === 'capability' ? 'VARIANT_CHEAPEST' : 'VARIANT_BALANCED',
    });
  }

  const record = buildRunRecord({
    runId: nextRunId,
    snapshotId,
    briefId: parentRecord.briefId,
    preset: request.preset ?? parentRun.preset,
    k: Math.max(1, Math.min(3, request.k ?? parentRun.k)),
    seed: request.seed ?? parentRun.seed,
    timeLimitMs: request.time_limit_ms ?? parentRun.time_limit_ms,
    horizon,
    constraints: effectiveConstraints,
    plans,
    createdAt: timestampFor(runSequence),
    parentRunId: runId,
    weights: request.weights ?? weightsForPreset(request.preset ?? parentRun.preset),
  });

  const newBaseVariant = record.variants.get(record.run.variants[0].variant_id);
  const newRoster = newBaseVariant?.members.map((member) => member.employee_id) ?? [];
  const p50Before = baseVariant.duration.p50_weeks;
  const p50After = newBaseVariant?.duration.p50_weeks ?? p50Before;
  const deadlineBefore = parentRecord.constraints.deadline_week ?? null;
  const deadlineAfter = effectiveConstraints.deadline_week ?? null;

  const effectBefore = JSON.stringify({
    roster: [...baseRoster].sort(),
    p50: p50Before,
    deadline: deadlineBefore,
    score: baseVariant.score,
  });
  const effectAfter = JSON.stringify({
    roster: [...newRoster].sort(),
    p50: p50After,
    deadline: deadlineAfter,
    score: newBaseVariant?.score ?? baseVariant.score,
  });

  const facts: Record<string, unknown> = {
    changed_members: changedMembers(baseRoster, newRoster),
    roster_jaccard: jaccardDistance(baseRoster, newRoster),
    p50_weeks_before: p50Before,
    p50_weeks_after: p50After,
    base_variant_id: baseVariant.variant_id,
    new_variant_id: newBaseVariant?.variant_id,
    overrides_applied: request.overrides.length - rejected.length,
    overrides_rejected: rejected.length,
    seed: record.run.seed,
  };

  const reasonCodes: ReasonCode[] = effectBefore === effectAfter ? ['RECALC_NO_EFFECT'] : ['RECALC_APPLIED'];
  record.run.reason_codes = reasonCodes;

  return {
    run_id: record.run.run_id,
    parent_run_id: runId,
    status: record.run.status,
    applied_overrides: request.overrides.length - rejected.length,
    rejected_overrides: rejected,
    reason_codes: reasonCodes,
    facts,
  };
}

/** Exposed for tests and UI helpers: grade → level mapping used by the mock metric model. */
export { GRADE_LEVEL };
