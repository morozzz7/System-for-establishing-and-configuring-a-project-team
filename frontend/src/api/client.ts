/**
 * API client layer for the core service (contracts/openapi.yaml).
 *
 * Two modes, selected only by `VITE_API_BASE_URL`:
 *  - HTTP mode: real calls against the Go core; every signature is contract-exact, so switching
 *    from mocks to the backend requires no UI changes.
 *  - Mock mode (no base URL): deterministic in-memory fixtures from src/mocks/, including the
 *    recalculation engine that demonstrates METRICS 3.6 responsiveness.
 *
 * Errors are always thrown as `ApiError` carrying the contract `Problem` code, detail and reason codes.
 */
import type {
  Employee,
  EmployeeCompetency,
  Project,
  Assignment,
  Absence,
  Role,
  Competency,
  GradeBand,
  WeightPreset,
  PlanningParams,
  CapacityWeek,
  Brief,
  Requirement,
  TeamRun,
  RunInputs,
  Variant,
  Explanation,
  RecalculateRequest,
  RecalculateResponse,
  DecisionRequest,
  DecisionResponse,
  Problem,
  FieldError,
  ReasonCode,
} from '../types';
import type { MetricDefinition, MetricDefinitionsResponse, WeightPresetListResponse } from '../types/reference';
import { ApiError } from './errors';
import {
  mockRoles,
  mockCompetencies,
  mockGrades,
  mockWeightPresets,
  mockPlanningParams,
  mockEmployees,
  mockMetricDefinitions,
  mockWeightPresetMetrics,
} from '../mocks/reference';
import {
  mockProjects,
  mockAssignments,
  mockAbsences,
  mockEmployeeCompetencies,
  mockCapacityWeeks,
} from '../mocks/data';
import {
  createMockRun,
  getMockRun,
  getMockRunInputs,
  getMockVariant,
  getMockExplanation,
  recalculateMockRun,
  resetMockRuns,
} from '../mocks/runs';

const mockBriefs: Brief[] = [];
const mockRequirements: Record<string, Requirement[]> = {};

const API_BASE = import.meta.env.VITE_API_BASE_URL as string | undefined;
/** Mock latency is skipped in unit tests so component tests stay fast and deterministic. */
const MOCK_LATENCY = import.meta.env.MODE === 'test' ? 0 : 120;

type FetchInit = NonNullable<Parameters<typeof fetch>[1]>;

let requestCounter = 0;
function nextRequestId(): string {
  requestCounter += 1;
  return `req_ui${String(requestCounter).padStart(4, '0')}`;
}

function delay(): Promise<void> {
  if (MOCK_LATENCY <= 0) return Promise.resolve();
  return new Promise((resolve) => setTimeout(resolve, MOCK_LATENCY));
}

function isProblemPayload(value: unknown): value is Problem {
  if (typeof value !== 'object' || value === null) return false;
  const candidate = value as Record<string, unknown>;
  return typeof candidate.code === 'string' && typeof candidate.status === 'number';
}

async function problemFromResponse(response: Response): Promise<ApiError> {
  let payload: unknown = null;
  try {
    payload = await response.json();
  } catch {
    payload = null;
  }
  if (isProblemPayload(payload)) return ApiError.fromProblem(payload, response.status);
  const fieldErrors: FieldError[] = [];
  return new ApiError({
    status: response.status,
    code: `HTTP_${response.status}`,
    detail: `Запрос завершился с кодом ${response.status}`,
    fieldErrors,
    requestId: response.headers.get('X-Request-Id') ?? undefined,
    retryable: response.status >= 500,
  });
}

/** Perform a request; non-2xx responses become `ApiError` (no raw bodies or stacks leak to the UI). */
async function rawRequest(path: string, init?: FetchInit): Promise<Response> {
  let response: Response;
  try {
    response = await fetch(`${API_BASE}${path}`, {
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        'X-Request-Id': nextRequestId(),
      },
      ...init,
    });
  } catch (cause) {
    throw ApiError.network(cause instanceof Error ? cause.message : 'Сеть недоступна');
  }
  if (!response.ok) throw await problemFromResponse(response);
  return response;
}

async function backend<T>(path: string, init?: FetchInit): Promise<T> {
  const response = await rawRequest(path, init);
  if (response.status === 204) return undefined as T;
  return (await response.json()) as T;
}

// ──── briefs ────

export async function createBrief(input: { title: string; text: string; language?: 'ru' | 'en' }): Promise<Brief> {
  if (API_BASE) return backend<Brief>('/briefs', { method: 'POST', body: JSON.stringify({ ...input, language: input.language ?? 'ru', source_type: 'text' }) });
  await delay();
  const brief: Brief = { brief_id: `brf_${Math.random().toString(36).slice(2, 8)}`, title: input.title, source_type: 'text', language: input.language ?? 'ru', status: 'needs_review', created_at: new Date().toISOString(), requirement_count: 0, needs_review_count: 0, reason_codes: [] };
  mockBriefs.unshift(brief);
  mockRequirements[brief.brief_id] = [];
  return brief;
}

export async function createBriefFromFile(file: File, title?: string, language?: 'ru' | 'en'): Promise<Brief> {
  const briefTitle = title || file.name.replace(/\.[^/.]+$/, '') || 'Без названия';
  if (API_BASE) {
    const formData = new FormData();
    formData.append('file', file);
    formData.append('title', briefTitle);
    formData.append('language', language ?? 'ru');
    const response = await fetch(`${API_BASE}/briefs`, {
      method: 'POST',
      body: formData,
    });
    if (!response.ok) {
      const err = await response.json().catch(() => ({ detail: 'Ошибка загрузки файла' }));
      throw new ApiError({
        status: response.status,
        code: (err as { code?: string }).code ?? 'UPLOAD_FAILED',
        detail: (err as { detail?: string }).detail ?? 'Не удалось загрузить файл ТЗ',
        reasonCodes: (err as { reason_codes?: ReasonCode[] }).reason_codes ?? [],
      });
    }
    return (await response.json()) as Brief;
  }
  await delay();
  const fileExt = file.name.toLowerCase();
  const srcType: 'text' | 'docx' | 'pdf' | 'txt' = fileExt.endsWith('.docx')
    ? 'docx'
    : fileExt.endsWith('.pdf')
    ? 'pdf'
    : 'txt';
  const brief: Brief = {
    brief_id: `brf_${Math.random().toString(36).slice(2, 8)}`,
    title: briefTitle,
    source_type: srcType,
    language: language ?? 'ru',
    status: 'needs_review',
    created_at: new Date().toISOString(),
    requirement_count: 0,
    needs_review_count: 0,
    reason_codes: [],
  };
  mockBriefs.unshift(brief);
  mockRequirements[brief.brief_id] = [];
  return brief;
}
export async function listBriefs(): Promise<Brief[]> { if (API_BASE) return (await backend<{ items: Brief[] }>('/briefs')).items; await delay(); return [...mockBriefs]; }
export async function getBrief(briefId: string): Promise<Brief | null> {
  if (API_BASE) return backend<Brief>(`/briefs/${briefId}`);
  await delay();
  return mockBriefs.find((b) => b.brief_id === briefId) ?? null;
}
export async function getRequirements(briefId: string): Promise<Requirement[]> { if (API_BASE) return (await backend<{ items: Requirement[] }>(`/briefs/${briefId}/requirements`)).items; await delay(); return [...(mockRequirements[briefId] ?? [])]; }
export async function confirmBrief(briefId: string): Promise<Brief> { if (API_BASE) return backend<Brief>(`/briefs/${briefId}/requirements:confirm`, { method: 'POST' }); await delay(); const b = mockBriefs.find((x) => x.brief_id === briefId); if (!b) throw new ApiError({ status: 404, code: 'NOT_FOUND', detail: 'Бриф не найден', reasonCodes: [] }); b.status = 'confirmed'; return b; }

// ──── runs ────

const RUN_REQUEST_DEFAULTS = {
  preset: 'balanced' as const,
  k: 2,
  seed: 42,
  time_limit_ms: 5000,
};

export async function createRun(briefId: string): Promise<TeamRun> {
  if (API_BASE) {
    return backend<TeamRun>('/runs', {
      method: 'POST',
      body: JSON.stringify({
        brief_id: briefId,
        ...RUN_REQUEST_DEFAULTS,
        horizon: { start_week: '2026-W07', end_week: '2026-W17' },
      }),
    });
  }
  await delay();
  return createMockRun(briefId);
}

export async function getRun(runId: string): Promise<TeamRun | null> {
  if (API_BASE) return backend<TeamRun>(`/runs/${runId}`);
  await delay();
  return getMockRun(runId);
}

/** Full variant detail (members, weekly assignments, deficits, active constraints). */
export async function getVariant(runId: string, variantId: string): Promise<Variant | null> {
  if (API_BASE) return backend<Variant>(`/runs/${runId}/variants/${variantId}`);
  await delay();
  return getMockVariant(runId, variantId);
}

/** Immutable input snapshot of a run: horizon, weights, constraints, candidates. */
export async function getRunInputs(runId: string): Promise<RunInputs | null> {
  if (API_BASE) return backend<RunInputs>(`/runs/${runId}/inputs`);
  await delay();
  return getMockRunInputs(runId);
}

/**
 * Why each member is in / out and the main trade-off (reason codes + facts).
 * In HTTP mode calls GET /runs/{runId}/variants/{variantId}/explanation.
 */
export async function getVariantExplanation(runId: string, variantId: string): Promise<Explanation | null> {
  if (API_BASE) return backend<Explanation>(`/runs/${runId}/variants/${variantId}/explanation`);
  await delay();
  return getMockExplanation(runId, variantId);
}

/**
 * Apply manual constraint overrides: creates a NEW run derived from `runId` (ADR-007 §3).
 * `200` = synchronous result, `202` = queued (both carry `RecalculateResponse`).
 */
export async function recalculateRun(runId: string, request: RecalculateRequest): Promise<RecalculateResponse> {
  if (API_BASE) {
    const response = await rawRequest(`/runs/${runId}/recalculate`, {
      method: 'POST',
      body: JSON.stringify(request),
    });
    if (response.status !== 200 && response.status !== 202) {
      throw new ApiError({
        status: response.status,
        code: 'UNEXPECTED_STATUS',
        detail: `Неожиданный ответ пересчёта: ${response.status}`,
      });
    }
    return (await response.json()) as RecalculateResponse;
  }
  await delay();
  return recalculateMockRun(runId, request);
}

/**
 * Record human manager decision on chosen variant (ADR-007 §4).
 * Calls POST /runs/{runId}/decision.
 */
export async function recordDecision(runId: string, request: DecisionRequest): Promise<DecisionResponse> {
  if (API_BASE) {
    return backend<DecisionResponse>(`/runs/${runId}/decision`, {
      method: 'POST',
      body: JSON.stringify(request),
    });
  }
  await delay();
  return {
    run_id: runId,
    variant_id: request.variant_id,
    decided_at: new Date().toISOString(),
    decided_by: request.decided_by,
    edits_count: request.edits_applied?.length ?? 0,
    audit_event_id: `aud_${Date.now().toString(36)}`,
  };
}

/** Test-only hook: clears the in-memory mock run store. */
export function resetMockState(): void {
  resetMockRuns();
}

// ──── employees ────

export async function listEmployees(_params?: {
  role_id?: string;
  grade?: string;
  bench_only?: boolean;
}): Promise<Employee[]> {
  if (API_BASE) return (await backend<{ items: Employee[] }>('/employees?limit=200')).items;
  await delay();
  return [...mockEmployees];
}

export interface EmployeeImportPayload {
  mode?: 'upsert' | 'replace';
  employees: Array<Record<string, unknown>>;
  validate_only?: boolean;
}

export interface EmployeeImportResponse {
  request_id: string;
  mode: string;
  totals: { received: number; created: number; updated: number; unchanged: number; errors: number };
  employees: Array<{ employee_id: string; status: string; warnings: string[]; error?: string }>;
  validation_errors: Array<{ employee_id: string; field: string; code: string; detail: string }>;
}

export async function importEmployees(payload: EmployeeImportPayload): Promise<EmployeeImportResponse> {
  if (API_BASE) {
    return backend<EmployeeImportResponse>('/employees', {
      method: 'PUT',
      body: JSON.stringify({ ...payload, mode: payload.mode ?? 'upsert' }),
    });
  }
  await delay();
  const total = payload.employees.length;
  const invalid = payload.employees.filter((e) => !e.employee_id || !e.grade || !e.role_profile_id);
  const errors = invalid.map((e, idx) => ({
    employee_id: String(e.employee_id || `row_${idx + 1}`),
    field: !e.employee_id ? 'employee_id' : !e.grade ? 'grade' : 'role_profile_id',
    code: 'REQUIRED_FIELD_MISSING',
    detail: 'Обязательное поле не заполнено',
  }));
  const validCount = total - invalid.length;
  return {
    request_id: `req_mock_${Date.now()}`,
    mode: payload.mode ?? 'upsert',
    totals: {
      received: total,
      created: payload.validate_only ? 0 : Math.round(validCount * 0.7),
      updated: payload.validate_only ? 0 : Math.round(validCount * 0.3),
      unchanged: 0,
      errors: invalid.length,
    },
    employees: payload.employees.map((e, idx) => ({
      employee_id: String(e.employee_id || `emp_${idx + 1}`),
      status: invalid.some((inv) => inv === e) ? 'error' : 'ok',
      warnings: [],
      error: invalid.some((inv) => inv === e) ? 'Пропущено обязательное поле' : undefined,
    })),
    validation_errors: errors,
  };
}

export async function getEmployee(employeeId: string): Promise<Employee | null> {
  if (API_BASE) return backend<Employee>(`/employees/${encodeURIComponent(employeeId)}`);
  await delay();
  return mockEmployees.find((e) => e.employee_id === employeeId) ?? null;
}

export async function createEmployee(emp: Employee): Promise<Employee> {
  await delay();
  return emp;
}

export async function patchEmployee(
  employeeId: string,
  patch: Partial<Employee>,
): Promise<Employee> {
  await delay();
  const existing = mockEmployees.find((e) => e.employee_id === employeeId);
  if (!existing) throw new ApiError({ status: 404, code: 'NOT_FOUND', detail: 'Сотрудник не найден', reasonCodes: [] });
  return { ...existing, ...patch };
}

export async function listEmployeeCompetencies(
  employeeId: string,
): Promise<EmployeeCompetency[]> {
  if (API_BASE) return backend<EmployeeCompetency[]>(`/employees/${encodeURIComponent(employeeId)}/competencies`);
  await delay();
  return mockEmployeeCompetencies[employeeId] ?? [];
}

export async function replaceEmployeeCompetencies(
  employeeId: string,
  comps: EmployeeCompetency[],
): Promise<EmployeeCompetency[]> {
  await delay();
  // In real API this would persist; for mocks just return
  void employeeId;
  return comps;
}

export async function getEmployeeCapacity(
  employeeId: string,
  fromWeek: string,
  toWeek: string,
): Promise<CapacityWeek[]> {
  if (API_BASE) {
    const query = new URLSearchParams({ start_week: fromWeek, end_week: toWeek });
    return backend<CapacityWeek[]>(`/employees/${encodeURIComponent(employeeId)}/capacity?${query}`);
  }
  await delay();
  // Return mock capacity for any employee (simplified — only emp_0001 has explicit data)
  if (employeeId === 'emp_0001') return mockCapacityWeeks;
  // Generate simple capacity for other employees
  return [
    {
      employee_id: employeeId,
      week: '2026-W07',
      working_days: 5,
      norm_hours: 40,
      reserved_hours: 8,
      assigned_hours: 16,
      absence_hours: 0,
      free_hours: 16,
      load_ratio: 0.4,
      unavailable: false,
    },
    {
      employee_id: employeeId,
      week: '2026-W08',
      working_days: 5,
      norm_hours: 40,
      reserved_hours: 8,
      assigned_hours: 16,
      absence_hours: 0,
      free_hours: 16,
      load_ratio: 0.4,
      unavailable: false,
    },
  ];
}

// ──── projects ────

export async function listProjects(_params?: {
  status?: string;
}): Promise<Project[]> {
  await delay();
  return [...mockProjects];
}

export async function createProject(prj: Project): Promise<Project> {
  await delay();
  return prj;
}

// ──── assignments ────

export async function listAssignments(_projectId?: string): Promise<Assignment[]> {
  await delay();
  return [...mockAssignments];
}

export async function createAssignments(
  assignments: Omit<Assignment, 'assignment_id'>[],
): Promise<Assignment[]> {
  await delay();
  return assignments.map((a, i) => ({
    ...a,
    assignment_id: `asg_new_${String(i).padStart(4, '0')}`,
  }));
}

// ──── absences ────

export async function listAbsences(_params?: {
  employee_id?: string;
  from?: string;
  to?: string;
}): Promise<Absence[]> {
  await delay();
  return [...mockAbsences];
}

export async function createAbsence(absence: Omit<Absence, 'absence_id'>): Promise<Absence> {
  await delay();
  return { ...absence, absence_id: 'abs_new_0001' };
}

export async function deleteAbsence(_absenceId: string): Promise<void> {
  await delay();
}

// ──── taxonomy / reference ────

export async function listRoles(): Promise<Role[]> {
  await delay();
  return [...mockRoles];
}

export async function listCompetencies(): Promise<Competency[]> {
  await delay();
  return [...mockCompetencies];
}

export async function listGrades(): Promise<GradeBand[]> {
  if (API_BASE) return backend<GradeBand[]>('/reference/grades');
  await delay();
  return [...mockGrades];
}

export async function listWeightPresets(): Promise<WeightPreset[]> {
  if (API_BASE) return (await backend<WeightPresetListResponse>('/reference/weight-presets')).presets;
  await delay();
  return [...mockWeightPresets];
}

export async function getWeightPresetsWithMetrics(): Promise<WeightPresetListResponse> {
  if (API_BASE) return backend<WeightPresetListResponse>('/reference/weight-presets');
  await delay();
  return {
    presets: [...mockWeightPresets],
    metrics: mockWeightPresetMetrics,
  };
}

export async function listMetricDefinitions(): Promise<MetricDefinition[]> {
  if (API_BASE) return backend<MetricDefinitionsResponse>('/reference/metrics').then((r) => r.metrics);
  await delay();
  return [...mockMetricDefinitions];
}

export async function getPlanningParams(): Promise<PlanningParams> {
  if (API_BASE) return backend<PlanningParams>('/reference/planning-params');
  await delay();
  return { ...mockPlanningParams };
}
