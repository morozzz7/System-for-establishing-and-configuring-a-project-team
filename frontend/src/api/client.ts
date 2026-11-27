import { demoBriefs, demoRequirements } from "../mocks/briefs";
import { effectiveProject, plannedAssignments, projectFromRun } from '../utils/project';
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
  PageMeta,
} from '../types';
import type { MetricDefinition, MetricDefinitionsResponse, WeightPresetListResponse } from '../types/reference';
import { ApiError } from './errors';
import { clearToken, ensureToken, login } from './auth';
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

const mockBriefs: Brief[] = [...demoBriefs];
const mockRequirements: Record<string, Requirement[]> = { ...demoRequirements };
const mockCreatedProjects: Project[] = [];
const mockDeletedProjectIds = new Set<string>();
const mockAddedAbsences: Absence[] = [];
let mockEntitySeq = 0;
function mockId(prefix: string): string {
  mockEntitySeq += 1;
  return `${prefix}_mock${String(mockEntitySeq).padStart(4, '0')}`;
}

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

async function requestHeaders(init?: FetchInit): Promise<Headers> {
  const headers = new Headers(init?.headers);
  if (!headers.has('Accept')) headers.set('Accept', 'application/json');
  if (init?.body instanceof FormData) {
    headers.delete('Content-Type');
  } else if (!headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json');
  }
  if (!headers.has('X-Request-Id')) headers.set('X-Request-Id', nextRequestId());
  const token = await ensureToken();
  if (token) headers.set('Authorization', `Bearer ${token}`);
  return headers;
}

/** Perform a request; non-2xx responses become `ApiError` (no raw bodies or stacks leak to the UI). */
async function rawRequest(path: string, init?: FetchInit, allowRetry = true): Promise<Response> {
  const headers = await requestHeaders(init);
  let response: Response;
  try {
    response = await fetch(`${API_BASE}${path}`, { ...init, headers });
  } catch (cause) {
    throw ApiError.network(cause instanceof Error ? cause.message : 'Сеть недоступна');
  }
  if (response.status === 401 && allowRetry && path !== '/auth/login') {
    clearToken();
    await login();
    return rawRequest(path, init, false);
  }
  if (!response.ok) throw await problemFromResponse(response);
  return response;
}

async function backend<T>(path: string, init?: FetchInit): Promise<T> {
  const response = await rawRequest(path, init);
  if (response.status === 204) return undefined as T;
  return (await response.json()) as T;
}

async function fetchAllPages<T>(path: string): Promise<T[]> {
  const items: T[] = [];
  const limit = 200;
  for (let offset = 0; ; offset += limit) {
    const page = await backend<{ items: T[]; meta?: PageMeta }>(`${path}?limit=${limit}&offset=${offset}`);
    items.push(...page.items);
    if (page.items.length < limit || (page.meta && items.length >= page.meta.total)) break;
  }
  return items;
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
    const headers = await requestHeaders({ method: 'POST', body: formData });
    const response = await fetch(`${API_BASE}/briefs`, {
      method: 'POST',
      body: formData,
      headers,
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
        // No horizon: the core derives the window from the confirmed brief.
        ...RUN_REQUEST_DEFAULTS,
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
const mockDecisions = new Map<string, DecisionResponse>();
const mockApprovedPlans = new Map<string, Assignment[]>();

export async function getDecision(runId: string): Promise<DecisionResponse | null> {
  if (!API_BASE) return mockDecisions.get(runId) ?? null;
  try {
    return await backend<DecisionResponse>(`/runs/${runId}/decision`);
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) return null;
    throw error;
  }
}

export async function synchronizeApprovedPlan(projectId: string, runId: string): Promise<Assignment[]> {
  if (API_BASE) {
    return backend<Assignment[]>(`/projects/${encodeURIComponent(projectId)}/assignments`, {
      method: 'POST',
      body: JSON.stringify({ approved_run_id: runId }),
    });
  }
  const decision = mockDecisions.get(runId);
  const run = getMockRun(runId);
  const variant = decision && getMockVariant(runId, decision.variant_id);
  if (!run || !variant || projectFromRun(run, null).project_id !== projectId) {
    throw new ApiError({ status: 409, code: 'CONFLICT_APPROVED_PLAN', detail: 'Утверждённый вариант не найден' });
  }
  const next = plannedAssignments(variant, projectId).filter((row) => row.hours > 0);
  const previous = new Set(mockApprovedPlans.get(projectId)?.map((row) => row.assignment_id));
  const manual = mockAssignments.filter((row) => row.project_id === projectId && !previous.has(row.assignment_id));
  if (next.some((row) => manual.some((item) => item.employee_id === row.employee_id && item.week === row.week))) {
    throw new ApiError({ status: 409, code: 'CONFLICT_DUPLICATE', detail: 'На эту неделю уже есть ручное назначение' });
  }
  const stored = next.map((row) => ({ ...row, assignment_id: mockId('asg') }));
  mockAssignments.splice(0, mockAssignments.length,
    ...mockAssignments.filter((row) => !previous.has(row.assignment_id)), ...stored);
  mockApprovedPlans.set(projectId, stored);
  return stored;
}

export async function recordDecision(runId: string, request: DecisionRequest): Promise<DecisionResponse> {
  if (API_BASE) {
    return backend<DecisionResponse>(`/runs/${runId}/decision`, {
      method: 'POST',
      body: JSON.stringify(request),
    });
  }
  await delay();
  const result = {
    run_id: runId,
    variant_id: request.variant_id,
    decided_at: new Date().toISOString(),
    decided_by: request.decided_by ?? 'system',
    edits_count: request.edits_applied?.length ?? 0,
    audit_event_id: `aud_${Date.now().toString(36)}`,
  };
  mockDecisions.set(runId, result);
  return result;
}

/** Test-only hook: clears the in-memory mock run store. */
export function resetMockState(): void {
  resetMockRuns();
  mockDecisions.clear();
  for (const rows of mockApprovedPlans.values()) {
    for (const row of rows) {
      const index = mockAssignments.indexOf(row);
      if (index >= 0) mockAssignments.splice(index, 1);
    }
  }
  mockApprovedPlans.clear();
  mockCreatedProjects.length = 0;
}

// ──── employees ────

let employeeDirectory: Promise<Employee[]> | undefined;

export function invalidateEmployees() {
  employeeDirectory = undefined;
}

export function listEmployees(): Promise<Employee[]> {
  if (employeeDirectory) return employeeDirectory;
  const request = API_BASE ? fetchAllPages<Employee>('/employees') : Promise.resolve([...mockEmployees]);
  employeeDirectory = request;
  void request.catch(() => {
    if (employeeDirectory === request) invalidateEmployees();
  });
  return request;
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
    const result = await backend<EmployeeImportResponse>('/employees', {
      method: 'PUT',
      body: JSON.stringify({ ...payload, mode: payload.mode ?? 'upsert' }),
    });
    if (!payload.validate_only) invalidateEmployees();
    return result;
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
  if (!payload.validate_only) {
    for (const employee of payload.employees) {
      if (invalid.includes(employee)) continue;
      const employeeId = String(employee.employee_id);
      const existing = mockEmployees.find((item) => item.employee_id === employeeId);
      const grade = String(employee.grade) as Employee['grade'];
      const profile: Employee = {
        employee_id: employeeId,
        grade,
        role_profile_id: String(employee.role_profile_id),
        unit_id: String(employee.unit_id ?? existing?.unit_id ?? 'unt_default'),
        employment_type: (employee.employment_type ?? existing?.employment_type ?? 'internal') as Employee['employment_type'],
        bench_flag: Boolean(employee.bench_flag ?? existing?.bench_flag),
        capacity_profile: (employee.capacity_profile ?? existing?.capacity_profile ?? {
          weekly_hours_norm: 40, reserve_ratio: 0.2, max_utilization_ratio: 0.95,
        }) as Employee['capacity_profile'],
        active: existing?.active ?? true,
        cost_coefficient: mockGrades.find((item) => item.grade === grade)?.cost_coefficient ?? 1,
        display_name: typeof employee.display_name === 'string' ? employee.display_name.trim() : existing?.display_name,
      };
      if (existing) Object.assign(existing, profile);
      else mockEmployees.push(profile);
      if (Array.isArray(employee.competencies)) {
        mockEmployeeCompetencies[employeeId] = employee.competencies as EmployeeCompetency[];
      }

      const rawAbsences = employee.absences;
      if (!Array.isArray(rawAbsences)) continue;
      for (const raw of rawAbsences) {
        const absence = raw as Record<string, unknown>;
        mockAddedAbsences.push({
          absence_id: mockId('abs'),
          employee_id: String(absence.employee_id ?? employee.employee_id ?? ''),
          type: String(absence.type ?? 'other') as Absence['type'],
          start_date: String(absence.start_date ?? ''),
          end_date: String(absence.end_date ?? ''),
          hours_per_day: Number(absence.hours_per_day ?? 8),
          approved: absence.approved !== false,
          source: 'hr_import',
        });
      }
    }
  }
  if (!payload.validate_only) invalidateEmployees();
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
  if (API_BASE) {
    const created = await backend<Employee>('/employees', {
      method: 'POST',
      body: JSON.stringify({
        employee_id: emp.employee_id,
        display_name: emp.display_name,
        grade: emp.grade,
        role_profile_id: emp.role_profile_id,
        unit_id: emp.unit_id,
        employment_type: emp.employment_type,
        bench_flag: emp.bench_flag,
        capacity_profile: emp.capacity_profile,
      }),
    });
    invalidateEmployees();
    return created;
  }
  await delay();
  mockEmployees.push(emp);
  invalidateEmployees();
  return emp;
}

export async function patchEmployee(
  employeeId: string,
  patch: Partial<Employee>,
): Promise<Employee> {
  if (API_BASE) {
    const body = Object.fromEntries(
      (['display_name', 'grade', 'role_profile_id', 'unit_id', 'employment_type', 'bench_flag', 'capacity_profile', 'active'] as const)
        .filter((key) => patch[key] !== undefined)
        .map((key) => [key, patch[key]]),
    );
    const updated = await backend<Employee>(`/employees/${encodeURIComponent(employeeId)}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    });
    invalidateEmployees();
    return updated;
  }
  await delay();
  const existing = mockEmployees.find((e) => e.employee_id === employeeId);
  if (!existing) throw new ApiError({ status: 404, code: 'NOT_FOUND', detail: 'Сотрудник не найден', reasonCodes: [] });
  Object.assign(existing, patch);
  invalidateEmployees();
  return existing;
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
  if (API_BASE) {
    return backend<EmployeeCompetency[]>(`/employees/${encodeURIComponent(employeeId)}/competencies`, {
      method: 'PUT',
      body: JSON.stringify(comps),
    });
  }
  await delay();
  void employeeId;
  return comps;
}

export async function getEmployeeCapacity(
  employeeId: string,
  fromWeek: string,
  toWeek: string,
): Promise<CapacityWeek[]> {
  if (API_BASE) {
    const query = new URLSearchParams({ from_week: fromWeek, to_week: toWeek });
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

export async function listProjects(params?: {
  status?: string;
}): Promise<Project[]> {
  if (API_BASE) {
    const query = new URLSearchParams({ limit: '200' });
    if (params?.status) query.set('status', params.status);
    return (await backend<{ items: Project[] }>(`/projects?${query.toString()}`)).items;
  }
  await delay();
  const projects = [...mockCreatedProjects, ...mockProjects].filter(
    (p) => !mockDeletedProjectIds.has(p.project_id),
  ).map((project) => effectiveProject(project));
  return params?.status ? projects.filter((p) => p.status === params.status) : projects;
}

export async function getProject(projectId: string): Promise<Project | null> {
  if (API_BASE) {
    try {
      return await backend<Project>(`/projects/${encodeURIComponent(projectId)}`);
    } catch (error) {
      if (error instanceof ApiError && error.status === 404) return null;
      throw error;
    }
  }
  await delay();
  if (mockDeletedProjectIds.has(projectId)) return null;
  const project = (
    mockCreatedProjects.find((p) => p.project_id === projectId) ??
    mockProjects.find((p) => p.project_id === projectId) ??
    null
  );
  return project && effectiveProject(project);
}

export async function createProject(prj: Project): Promise<Project> {
  if (API_BASE) {
    return backend<Project>('/projects', {
      method: 'POST',
      body: JSON.stringify({
        project_id: prj.project_id,
        code: prj.code,
        name: prj.name,
        status: prj.status,
        priority: prj.priority,
        criticality: prj.criticality,
        start_week: prj.start_week,
        end_week_planned: prj.end_week_planned,
        replaces_other_projects: prj.replaces_other_projects,
      }),
    });
  }
  await delay();
  const existing = mockCreatedProjects.findIndex((p) => p.project_id === prj.project_id);
  if (existing >= 0) mockCreatedProjects.splice(existing, 1, prj);
  else mockCreatedProjects.unshift(prj);
  mockDeletedProjectIds.delete(prj.project_id);
  return effectiveProject(prj);
}

export async function deleteProject(projectId: string): Promise<void> {
  if (API_BASE) {
    await backend<void>(`/projects/${encodeURIComponent(projectId)}`, { method: 'DELETE' });
    return;
  }
  await delay();
  mockDeletedProjectIds.add(projectId);
  const index = mockCreatedProjects.findIndex((p) => p.project_id === projectId);
  if (index >= 0) mockCreatedProjects.splice(index, 1);
  for (let i = mockAssignments.length - 1; i >= 0; i -= 1) {
    if (mockAssignments[i].project_id === projectId) mockAssignments.splice(i, 1);
  }
}

// ──── assignments ────

export async function listAssignments(projectId?: string): Promise<Assignment[]> {
  if (API_BASE) {
    if (projectId) {
      return backend<Assignment[]>(`/projects/${encodeURIComponent(projectId)}/assignments`);
    }
    // The core has no global assignments route; compose the view from its project-scoped route.
    const projects = await listProjects();
    const batches = await Promise.all(
      projects.map((project) =>
        backend<Assignment[]>(`/projects/${encodeURIComponent(project.project_id)}/assignments`),
      ),
    );
    return batches.flat();
  }
  await delay();
  return mockAssignments.filter(
    (a) => !mockDeletedProjectIds.has(a.project_id) && (!projectId || a.project_id === projectId),
  );
}

export async function createAssignments(
  assignments: Omit<Assignment, 'assignment_id'>[],
): Promise<Assignment[]> {
  if (API_BASE) {
    const byProject = new Map<string, Array<Pick<Assignment, 'employee_id' | 'role_id' | 'week' | 'hours'>>>();
    for (const assignment of assignments) {
      if (!assignment.project_id) {
        throw new ApiError({
          status: 422,
          code: 'VALIDATION_FAILED',
          detail: 'Для назначения требуется project_id',
          reasonCodes: ['VALIDATION_FAILED'],
        });
      }
      const items = byProject.get(assignment.project_id) ?? [];
      items.push({
        employee_id: assignment.employee_id,
        role_id: assignment.role_id,
        week: assignment.week,
        hours: assignment.hours,
      });
      byProject.set(assignment.project_id, items);
    }
    const batches = await Promise.all(
      [...byProject].map(([projectId, items]) =>
        backend<Assignment[]>(`/projects/${encodeURIComponent(projectId)}/assignments`, {
          method: 'POST',
          body: JSON.stringify({ assignments: items }),
        }),
      ),
    );
    return batches.flat();
  }
  await delay();
  const created = assignments.map((a) => ({ ...a, assignment_id: mockId('asg') }));
  mockAssignments.push(...created);
  return created;
}

// ──── absences ────

export async function listAbsences(params?: {
  employee_id?: string;
  from?: string;
  to?: string;
}): Promise<Absence[]> {
  if (API_BASE) {
    const query = new URLSearchParams({ limit: '200' });
    if (params?.employee_id) query.set('employee_id', params.employee_id);
    if (params?.from) query.set('from', params.from);
    if (params?.to) query.set('to', params.to);
    return (await backend<{ items: Absence[] }>(`/absences?${query.toString()}`)).items;
  }
  await delay();
  const all = [...mockAddedAbsences, ...mockAbsences];
  return all.filter(
    (a) =>
      (!params?.employee_id || a.employee_id === params.employee_id) &&
      (!params?.from || a.end_date >= params.from) &&
      (!params?.to || a.start_date <= params.to),
  );
}

export async function createAbsence(absence: Omit<Absence, 'absence_id'>): Promise<Absence> {
  if (API_BASE) {
    return backend<Absence>('/absences', {
      method: 'POST',
      body: JSON.stringify({
        employee_id: absence.employee_id,
        type: absence.type,
        start_date: absence.start_date,
        end_date: absence.end_date,
        hours_per_day: absence.hours_per_day,
        approved: absence.approved,
      }),
    });
  }
  await delay();
  const created = { ...absence, absence_id: mockId('abs') };
  mockAddedAbsences.unshift(created);
  return created;
}

export async function deleteAbsence(absenceId: string): Promise<void> {
  if (API_BASE) {
    await backend<void>(`/absences/${encodeURIComponent(absenceId)}`, { method: 'DELETE' });
    return;
  }
  await delay();
  const addedIndex = mockAddedAbsences.findIndex((a) => a.absence_id === absenceId);
  if (addedIndex >= 0) mockAddedAbsences.splice(addedIndex, 1);
  const seededIndex = mockAbsences.findIndex((a) => a.absence_id === absenceId);
  if (seededIndex >= 0) mockAbsences.splice(seededIndex, 1);
}

// ──── taxonomy / reference ────

export async function listRoles(): Promise<Role[]> {
  if (API_BASE) return fetchAllPages<Role>('/taxonomy/roles');
  await delay();
  return [...mockRoles];
}

export async function listCompetencies(): Promise<Competency[]> {
  if (API_BASE) return fetchAllPages<Competency>('/taxonomy/competencies');
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
