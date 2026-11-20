/**
 * Constraint-override helpers for the recalculation UI (contracts/openapi.yaml#ConstraintOverride).
 *
 * The UI must never let the manager build a request that provably breaks a hard constraint
 * (AGENTS.md rule 6): contradictions inside the override list are reported as `blocking` issues
 * and disable the "Пересчитать" action. Everything else (availability, feasibility) is decided by
 * the Go core and surfaced as `rejected_overrides`.
 */
import type {
  ConstraintOverride,
  ConstraintType,
  Horizon,
  RecalculateRequest,
  RunConstraints,
  TeamRun,
  WeekKey,
} from '../types';
import { parseWeek, weekStart } from './week';

export const EMPLOYEE_ID_PATTERN = /^emp_[a-z0-9]{4,12}$/;
export const WEEK_KEY_PATTERN = /^\d{4}-W(0[1-9]|[1-4][0-9]|5[0-3])$/;
export const REASON_MAX_LENGTH = 300;
export const TEAM_SIZE_MIN = 1;
export const TEAM_SIZE_MAX = 50;
export const TEAM_SIZE_DEFAULT_MAX = 6;
export const BUS_FACTOR_MIN = 1;
export const BUS_FACTOR_MAX = 5;

export interface OverrideIssue {
  /** Index inside the override list; -1 for a single draft being edited. */
  index: number;
  field: string;
  message: string;
  /** Blocking issues prevent recalculation; they signal a manual hard-constraint violation. */
  blocking: boolean;
}

/** Draft edited in the drawer before it becomes a contract `ConstraintOverride`. */
export interface OverrideDraft {
  type: ConstraintType;
  employee_id: string;
  employee_ids: string[];
  value: number | null;
  week: string;
  flag: boolean;
  reason: string;
}

export interface OverrideValidationContext {
  /** Employee ids present in the run input snapshot (`RunInputs.candidates`). */
  candidateIds?: string[];
  /** Candidates whose capacity does not cover the whole horizon (absence / late start): warning. */
  unavailableCandidateIds?: string[];
  /** Candidates with zero available capacity in the horizon: including them breaks G1. */
  fullyUnavailableCandidateIds?: string[];
  horizon?: Horizon;
}

export const CONSTRAINT_TYPE_LABELS: Record<ConstraintType, string> = {
  force_include: 'Включить сотрудника',
  force_exclude: 'Исключить сотрудника',
  max_team_size: 'Максимум человек',
  min_team_size: 'Минимум человек',
  budget_cap: 'Потолок бюджета',
  deadline: 'Срок (неделя)',
  forbid_external: 'Запретить внешних',
  freeze_members: 'Закрепить сотрудников',
  min_bus_factor: 'Минимальный bus factor',
  max_utilization: 'Максимальная загрузка',
};

export const CONSTRAINT_TYPE_HINTS: Record<ConstraintType, string> = {
  force_include: 'Человек обязан быть в команде. Сервер отклонит включение недоступного сотрудника (G1).',
  force_exclude: 'Человек не может попасть в команду.',
  max_team_size: 'Верхняя граница размера команды, 1–50 человек.',
  min_team_size: 'Нижняя граница размера команды, 1–50 человек.',
  budget_cap: 'Потолок стоимости в условных единицах (чел.-часы × коэффициент грейда).',
  deadline: 'Конец горизонта в формате ГГГГ-Www, например 2026-W14.',
  forbid_external: 'Запрет привлекать подрядчиков и внешних партнёров.',
  freeze_members: 'Эти люди остаются в команде при пересчёте (явный freeze, ADR-007 §3).',
  min_bus_factor: 'Сколько человек должны владеть must-компетенцией, 1–5.',
  max_utilization: 'Предельная загрузка участника, 0.5–1.0 от нормы.',
};

/** Which draft fields are relevant for a constraint type (drives the dynamic form). */
export function draftFieldsForType(type: ConstraintType): {
  employee: boolean;
  employeeList: boolean;
  number: boolean;
  week: boolean;
  booleanFlag: boolean;
  numberLabel?: string;
  numberMin?: number;
  numberMax?: number;
  numberStep?: number;
} {
  switch (type) {
    case 'force_include':
    case 'force_exclude':
      return { employee: true, employeeList: false, number: false, week: false, booleanFlag: false };
    case 'freeze_members':
      return { employee: false, employeeList: true, number: false, week: false, booleanFlag: false };
    case 'max_team_size':
    case 'min_team_size':
      return {
        employee: false,
        employeeList: false,
        number: true,
        week: false,
        booleanFlag: false,
        numberLabel: 'Человек',
        numberMin: TEAM_SIZE_MIN,
        numberMax: TEAM_SIZE_MAX,
      };
    case 'budget_cap':
      return {
        employee: false,
        employeeList: false,
        number: true,
        week: false,
        booleanFlag: false,
        numberLabel: 'Условные единицы',
        numberMin: 0,
      };
    case 'deadline':
      return { employee: false, employeeList: false, number: false, week: true, booleanFlag: false };
    case 'forbid_external':
      return { employee: false, employeeList: false, number: false, week: false, booleanFlag: true };
    case 'min_bus_factor':
      return {
        employee: false,
        employeeList: false,
        number: true,
        week: false,
        booleanFlag: false,
        numberLabel: 'Человек',
        numberMin: BUS_FACTOR_MIN,
        numberMax: BUS_FACTOR_MAX,
      };
    case 'max_utilization':
      return {
        employee: false,
        employeeList: false,
        number: true,
        week: false,
        booleanFlag: false,
        numberLabel: 'Доля нормы',
        numberMin: 0.5,
        numberMax: 1,
        numberStep: 0.05,
      };
    default:
      return { employee: false, employeeList: false, number: false, week: false, booleanFlag: false };
  }
}

export function emptyDraft(type: ConstraintType = 'force_exclude'): OverrideDraft {
  const fields = draftFieldsForType(type);
  return {
    type,
    employee_id: '',
    employee_ids: [],
    value: fields.number ? (fields.numberMin ?? 1) : null,
    week: '',
    flag: true,
    reason: '',
  };
}

/** Draft with the defaults that make sense when the user switches the type in the form. */
export function draftForType(type: ConstraintType, previous?: OverrideDraft): OverrideDraft {
  const base = emptyDraft(type);
  if (!previous) return base;
  return { ...base, reason: previous.reason };
}

function normalizedReason(reason: string | undefined): string | undefined {
  const trimmed = (reason ?? '').trim();
  return trimmed.length > 0 ? trimmed.slice(0, REASON_MAX_LENGTH) : undefined;
}

/** Convert a draft into the contract shape: trims text, dedupes ids and drops empty optional fields. */
export function normalizeOverride(draft: OverrideDraft): ConstraintOverride {
  const reason = normalizedReason(draft.reason);
  const override: ConstraintOverride = { type: draft.type };
  switch (draft.type) {
    case 'force_include':
    case 'force_exclude':
      override.employee_id = draft.employee_id.trim();
      break;
    case 'freeze_members':
      override.employee_ids = [...new Set(draft.employee_ids.map((id) => id.trim()).filter(Boolean))];
      break;
    case 'deadline':
      override.week = draft.week.trim() as WeekKey;
      break;
    case 'forbid_external':
      override.value = draft.flag ? 1 : 0;
      break;
    default:
      override.value = draft.value === null ? undefined : Number(draft.value);
      break;
  }
  if (reason) override.reason = reason;
  if (override.value === undefined) delete override.value;
  return override;
}

function isBlank(value: string | undefined): boolean {
  return (value ?? '').trim().length === 0;
}

function validateEmployeeId(index: number, employeeId: string | undefined, context: OverrideValidationContext): OverrideIssue[] {
  const issues: OverrideIssue[] = [];
  if (isBlank(employeeId)) {
    issues.push({ index, field: 'employee_id', message: 'Выберите сотрудника', blocking: true });
    return issues;
  }
  const value = (employeeId ?? '').trim();
  if (!EMPLOYEE_ID_PATTERN.test(value)) {
    issues.push({
      index,
      field: 'employee_id',
      message: `Псевдоним должен иметь вид emp_xxxx (получено «${value}»)`,
      blocking: true,
    });
    return issues;
  }
  if (context.fullyUnavailableCandidateIds?.includes(value)) {
    issues.push({
      index,
      field: 'employee_id',
      message: `${value} недоступен весь горизонт: включение нарушит G1 (нет назначений в отсутствие)`,
      blocking: true,
    });
  } else if (context.unavailableCandidateIds?.includes(value)) {
    issues.push({
      index,
      field: 'employee_id',
      message: `${value} доступен не весь горизонт — назначения будут только в доступные недели`,
      blocking: false,
    });
  } else if (context.candidateIds && !context.candidateIds.includes(value)) {
    issues.push({
      index,
      field: 'employee_id',
      message: `${value} отсутствует в снимке входа запуска — сервер отклонит ограничение`,
      blocking: false,
    });
  }
  return issues;
}

function validateWeek(index: number, week: string | undefined, context: OverrideValidationContext): OverrideIssue[] {
  const issues: OverrideIssue[] = [];
  const value = (week ?? '').trim();
  if (value.length === 0) {
    issues.push({ index, field: 'week', message: 'Укажите неделю срока, например 2026-W14', blocking: true });
    return issues;
  }
  if (!WEEK_KEY_PATTERN.test(value)) {
    issues.push({
      index,
      field: 'week',
      message: 'Формат недели: ГГГГ-Www (2026-W01 … 2026-W53)',
      blocking: true,
    });
    return issues;
  }
  if (context.horizon && parseWeek(value) && value < context.horizon.start_week) {
    issues.push({
      index,
      field: 'week',
      message: `Срок раньше начала горизонта (${context.horizon.start_week})`,
      blocking: true,
    });
  }
  return issues;
}

function validateInteger(
  index: number,
  value: number | null | undefined,
  min: number,
  max: number,
  field: string,
  label: string,
): OverrideIssue[] {
  if (value === null || value === undefined || Number.isNaN(value)) {
    return [{ index, field, message: `Укажите значение: ${label}`, blocking: true }];
  }
  if (!Number.isInteger(value) || value < min || value > max) {
    return [{ index, field, message: `${label}: целое число от ${min} до ${max}`, blocking: true }];
  }
  return [];
}

/** Field-level validation of one override (used by the form and by the list validator). */
export function validateOverride(override: ConstraintOverride, context: OverrideValidationContext = {}, index = -1): OverrideIssue[] {
  const issues: OverrideIssue[] = [];
  if ((override.reason ?? '').length > REASON_MAX_LENGTH) {
    issues.push({
      index,
      field: 'reason',
      message: `Обоснование не длиннее ${REASON_MAX_LENGTH} символов`,
      blocking: true,
    });
  }
  switch (override.type) {
    case 'force_include':
    case 'force_exclude':
      issues.push(...validateEmployeeId(index, override.employee_id, context));
      break;
    case 'freeze_members': {
      const ids = override.employee_ids ?? [];
      if (ids.length === 0) {
        issues.push({ index, field: 'employee_ids', message: 'Выберите хотя бы одного сотрудника', blocking: true });
        break;
      }
      if (ids.length > 50) {
        issues.push({ index, field: 'employee_ids', message: 'Не более 50 сотрудников', blocking: true });
      }
      ids.forEach((id) => issues.push(...validateEmployeeId(index, id, context)));
      break;
    }
    case 'max_team_size':
    case 'min_team_size':
      issues.push(
        ...validateInteger(index, override.value, TEAM_SIZE_MIN, TEAM_SIZE_MAX, 'value', CONSTRAINT_TYPE_LABELS[override.type]),
      );
      break;
    case 'budget_cap': {
      const value = override.value;
      if (value === undefined || value === null || Number.isNaN(value)) {
        issues.push({ index, field: 'value', message: 'Укажите потолок бюджета', blocking: true });
      } else if (!Number.isFinite(value) || value < 0) {
        issues.push({ index, field: 'value', message: 'Бюджет — неотрицательное число', blocking: true });
      }
      break;
    }
    case 'deadline':
      issues.push(...validateWeek(index, override.week, context));
      break;
    case 'forbid_external':
      if (override.value !== 0 && override.value !== 1) {
        issues.push({ index, field: 'value', message: 'Значение должно быть 1 или 0', blocking: true });
      }
      break;
    case 'min_bus_factor':
      issues.push(
        ...validateInteger(index, override.value, BUS_FACTOR_MIN, BUS_FACTOR_MAX, 'value', CONSTRAINT_TYPE_LABELS.min_bus_factor),
      );
      break;
    case 'max_utilization': {
      const value = override.value;
      if (value === undefined || value === null || Number.isNaN(value) || value < 0.5 || value > 1) {
        issues.push({ index, field: 'value', message: 'Загрузка — доля нормы от 0.5 до 1.0', blocking: true });
      }
      break;
    }
    default:
      issues.push({ index, field: 'type', message: 'Неизвестный тип ограничения', blocking: true });
  }
  return issues;
}

/** Types that address one specific employee (duplicates are per employee, not per type). */
const EMPLOYEE_SCOPED_TYPES: ConstraintType[] = ['force_include', 'force_exclude', 'freeze_members'];

function overrideSignature(override: ConstraintOverride): string {
  if (override.type === 'freeze_members') {
    return `${override.type}:${[...(override.employee_ids ?? [])].sort().join(',')}`;
  }
  if (EMPLOYEE_SCOPED_TYPES.includes(override.type)) {
    return `${override.type}:${override.employee_id ?? ''}`;
  }
  return override.type;
}

/**
 * Validate the whole override list: field validity, duplicates (same type / same employee) and
 * contradictions that would manually break a hard constraint.
 */
export function validateOverrides(
  overrides: ConstraintOverride[],
  context: OverrideValidationContext = {},
): OverrideIssue[] {
  const issues: OverrideIssue[] = [];
  overrides.forEach((override, index) => {
    issues.push(...validateOverride(override, context, index));
  });

  // Duplicates: the same employee constraint twice, or a scalar constraint twice.
  const seen = new Map<string, number>();
  overrides.forEach((override, index) => {
    const signature = overrideSignature(override);
    const previous = seen.get(signature);
    if (previous === undefined) {
      seen.set(signature, index);
      return;
    }
    const message = EMPLOYEE_SCOPED_TYPES.includes(override.type)
      ? `Дубликат: ${CONSTRAINT_TYPE_LABELS[override.type]} для этого сотрудника уже добавлен`
      : `Дубликат: ограничение «${CONSTRAINT_TYPE_LABELS[override.type]}» уже добавлено`;
    issues.push({ index, field: 'type', message, blocking: true });
  });

  // Contradictions between overrides.
  const includes = new Set<string>();
  const excludes = new Set<string>();
  const frozen = new Set<string>();
  overrides.forEach((override) => {
    if (override.type === 'force_include' && override.employee_id) includes.add(override.employee_id);
    if (override.type === 'force_exclude' && override.employee_id) excludes.add(override.employee_id);
    if (override.type === 'freeze_members') (override.employee_ids ?? []).forEach((id) => frozen.add(id));
  });
  overrides.forEach((override, index) => {
    const employeeId = override.employee_id;
    if (override.type === 'force_include' && employeeId && excludes.has(employeeId)) {
      issues.push({
        index,
        field: 'employee_id',
        message: `${employeeId} одновременно включён и исключён`,
        blocking: true,
      });
    }
    if (override.type === 'force_exclude' && employeeId && (includes.has(employeeId) || frozen.has(employeeId))) {
      issues.push({
        index,
        field: 'employee_id',
        message: `${employeeId} одновременно исключён и закреплён`,
        blocking: true,
      });
    }
    if (override.type === 'freeze_members' && (override.employee_ids ?? []).some((id) => excludes.has(id))) {
      issues.push({
        index,
        field: 'employee_ids',
        message: 'Закреплённый сотрудник одновременно исключён',
        blocking: true,
      });
    }
  });

  const maxSize = overrides.find((override) => override.type === 'max_team_size')?.value;
  const minSize = overrides.find((override) => override.type === 'min_team_size')?.value;
  const pinned = new Set<string>([...includes, ...frozen].filter((id) => !excludes.has(id)));
  overrides.forEach((override, index) => {
    if (override.type === 'max_team_size' && typeof maxSize === 'number' && typeof minSize === 'number' && minSize > maxSize) {
      issues.push({
        index,
        field: 'value',
        message: `Минимум (${minSize}) больше максимума (${maxSize})`,
        blocking: true,
      });
    }
    if (override.type === 'max_team_size' && typeof maxSize === 'number' && maxSize < pinned.size) {
      issues.push({
        index,
        field: 'value',
        message: `Зафиксировано ${pinned.size} сотрудников — лимит ${maxSize} нарушит G4`,
        blocking: true,
      });
    }
  });

  return issues;
}

export function hasBlockingIssues(issues: OverrideIssue[]): boolean {
  return issues.some((issue) => issue.blocking);
}

/** Russian one-line summary of an override, used in the editor list (facts only, no prose numbers). */
export function describeOverride(override: ConstraintOverride): string {
  switch (override.type) {
    case 'force_include':
      return `Включить ${override.employee_id}`;
    case 'force_exclude':
      return `Исключить ${override.employee_id}`;
    case 'freeze_members':
      return `Закрепить: ${(override.employee_ids ?? []).join(', ')}`;
    case 'max_team_size':
      return `Не более ${override.value} человек`;
    case 'min_team_size':
      return `Не менее ${override.value} человек`;
    case 'budget_cap':
      return `Бюджет ≤ ${override.value} у.е.`;
    case 'deadline':
      return `Срок ${override.week}`;
    case 'forbid_external':
      return override.value === 1 ? 'Запретить внешних' : 'Разрешить внешних';
    case 'min_bus_factor':
      return `Bus factor ≥ ${override.value}`;
    case 'max_utilization':
      return `Загрузка ≤ ${Math.round((override.value ?? 0) * 100)}%`;
    default:
      return String(override.type);
  }
}

export interface ConstraintRow {
  key: ConstraintType;
  label: string;
  value: string;
}

/** Read-only rows for `RunInputs.constraints` (current constraints of the run). */
export function constraintRows(constraints: RunConstraints | undefined): ConstraintRow[] {
  if (!constraints) return [];
  const rows: ConstraintRow[] = [];
  if (constraints.force_include?.length) {
    rows.push({ key: 'force_include', label: CONSTRAINT_TYPE_LABELS.force_include, value: constraints.force_include.join(', ') });
  }
  if (constraints.force_exclude?.length) {
    rows.push({ key: 'force_exclude', label: CONSTRAINT_TYPE_LABELS.force_exclude, value: constraints.force_exclude.join(', ') });
  }
  if (constraints.freeze_members?.length) {
    rows.push({ key: 'freeze_members', label: CONSTRAINT_TYPE_LABELS.freeze_members, value: constraints.freeze_members.join(', ') });
  }
  if (typeof constraints.max_team_size === 'number') {
    rows.push({ key: 'max_team_size', label: CONSTRAINT_TYPE_LABELS.max_team_size, value: String(constraints.max_team_size) });
  }
  if (typeof constraints.min_team_size === 'number') {
    rows.push({ key: 'min_team_size', label: CONSTRAINT_TYPE_LABELS.min_team_size, value: String(constraints.min_team_size) });
  }
  if (typeof constraints.budget_cap_units === 'number') {
    rows.push({ key: 'budget_cap', label: CONSTRAINT_TYPE_LABELS.budget_cap, value: `${constraints.budget_cap_units} у.е.` });
  }
  if (constraints.deadline_week) {
    rows.push({ key: 'deadline', label: CONSTRAINT_TYPE_LABELS.deadline, value: constraints.deadline_week });
  }
  if (typeof constraints.forbid_external === 'boolean') {
    rows.push({ key: 'forbid_external', label: CONSTRAINT_TYPE_LABELS.forbid_external, value: constraints.forbid_external ? 'да' : 'нет' });
  }
  if (typeof constraints.min_bus_factor === 'number') {
    rows.push({ key: 'min_bus_factor', label: CONSTRAINT_TYPE_LABELS.min_bus_factor, value: String(constraints.min_bus_factor) });
  }
  if (typeof constraints.max_utilization_ratio === 'number') {
    rows.push({
      key: 'max_utilization',
      label: CONSTRAINT_TYPE_LABELS.max_utilization,
      value: `${Math.round(constraints.max_utilization_ratio * 100)}%`,
    });
  }
  return rows;
}

/** Candidates whose capacity does not cover the horizon (potential G1 violation for force_include). */
export function partiallyUnavailableCandidateIds(
  candidates: Array<{ employee_id: string; capacity_weeks: number }> | undefined,
  horizon: Horizon | undefined,
): string[] {
  if (!candidates || !horizon) return [];
  const start = parseWeek(horizon.start_week);
  const end = parseWeek(horizon.end_week);
  if (!start || !end) return [];
  const horizonWeeks =
    Math.round(
      (weekStart(end.year, end.week).getTime() - weekStart(start.year, start.week).getTime()) / (7 * 86400000),
    ) + 1;
  return candidates.filter((candidate) => candidate.capacity_weeks < horizonWeeks).map((candidate) => candidate.employee_id);
}

/** Candidates with no available capacity at all inside the horizon (force_include would break G1). */
export function candidatesWithoutCapacity(
  candidates: Array<{ employee_id: string; capacity_weeks: number }> | undefined,
): string[] {
  if (!candidates) return [];
  return candidates.filter((candidate) => candidate.capacity_weeks <= 0).map((candidate) => candidate.employee_id);
}

/** Build the contract `RecalculateRequest` from the current run and the edited override list. */
export function buildRecalculateRequest(
  run: TeamRun,
  baseVariantId: string | undefined,
  overrides: ConstraintOverride[],
): RecalculateRequest {
  return {
    ...(baseVariantId ? { base_variant_id: baseVariantId } : {}),
    preset: run.preset,
    k: run.k,
    seed: run.seed,
    time_limit_ms: run.time_limit_ms,
    overrides,
  };
}
