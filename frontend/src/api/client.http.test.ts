/**
 * HTTP-mode client branches: resource mutations must reach the Go core, never the mock fixtures.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Assignment, Competency, Employee, EmployeeCompetency } from '../types';

type FetchInit = NonNullable<Parameters<typeof fetch>[1]>;

function jsonResponse(body: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
    headers: { get: () => 'req_test' },
  } as unknown as Response;
}

describe('client in HTTP mode', () => {
  let api: typeof import('./client');
  let calls: Array<{ url: string; init?: FetchInit }>;

  beforeEach(async () => {
    calls = [];
    globalThis.localStorage?.clear();
    vi.stubEnv('VITE_API_BASE_URL', '/api/v1');
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: FetchInit) => {
        calls.push({ url, init });
        if (url.includes('/auth/login')) {
          return jsonResponse({ access_token: 'test-token', token_type: 'Bearer', expires_in: 3600, actor: 'emp_mgr01' });
        }
        if (url.includes('/projects?')) return jsonResponse({ items: [{ project_id: 'prj_x' }] });
        if (url.includes('/absences?')) return jsonResponse({ items: [] });
        if (url.includes('/taxonomy/')) return jsonResponse({ items: [] });
        if (url.includes('/capacity?')) return jsonResponse([]);
        if (init?.method === 'DELETE') return jsonResponse(undefined, 204);
        return jsonResponse([]);
      }),
    );
    vi.resetModules();
    api = await import('./client');
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
    vi.resetModules();
  });

  const resourceCalls = () => calls.filter((call) => !call.url.includes('/auth/login'));

  const employee: Employee = {
    employee_id: 'emp_test1',
    grade: 'M',
    role_profile_id: 'backend_engineer',
    unit_id: 'unt_core',
    employment_type: 'internal',
    bench_flag: false,
    capacity_profile: { weekly_hours_norm: 40, reserve_ratio: 0.2, max_utilization_ratio: 0.95 },
    cost_coefficient: 1.4,
    active: true,
  };

  it('sends contract-shaped employee writes to the core', async () => {
    await api.createEmployee(employee);
    expect(resourceCalls()[0].url).toBe('/api/v1/employees');
    expect(resourceCalls()[0].init?.method).toBe('POST');
    const createBody = JSON.parse(String(resourceCalls()[0].init?.body)) as Record<string, unknown>;
    expect(createBody).not.toHaveProperty('active');
    expect(createBody).not.toHaveProperty('cost_coefficient');

    await api.patchEmployee('emp_test1', { bench_flag: true, cost_coefficient: 2 });
    expect(resourceCalls()[1].url).toBe('/api/v1/employees/emp_test1');
    expect(resourceCalls()[1].init?.method).toBe('PATCH');
    expect(JSON.parse(String(resourceCalls()[1].init?.body))).toEqual({ bench_flag: true });

    const comps: EmployeeCompetency[] = [
      { competency_id: 'lang_go', level: 4, confidence: 0.9, source: 'manager_review' },
    ];
    await api.replaceEmployeeCompetencies('emp_test1', comps);
    expect(resourceCalls()[2].url).toBe('/api/v1/employees/emp_test1/competencies');
    expect(resourceCalls()[2].init?.method).toBe('PUT');
  });

  it('uses from_week/to_week for capacity', async () => {
    await api.getEmployeeCapacity('emp_test1', '2026-W07', '2026-W08');
    expect(resourceCalls()[0].url).toBe('/api/v1/employees/emp_test1/capacity?from_week=2026-W07&to_week=2026-W08');
  });

  it('routes assignments through the project-scoped endpoint', async () => {
    const assignment: Omit<Assignment, 'assignment_id'> = {
      project_id: 'prj_x',
      employee_id: 'emp_test1',
      role_id: 'backend_engineer',
      week: '2026-W07',
      hours: 20,
      source: 'plan',
    };
    await api.createAssignments([assignment]);
    expect(resourceCalls()[0].url).toBe('/api/v1/projects/prj_x/assignments');
    expect(resourceCalls()[0].init?.method).toBe('POST');
    expect(JSON.parse(String(resourceCalls()[0].init?.body))).toEqual({
      assignments: [{ employee_id: 'emp_test1', role_id: 'backend_engineer', week: '2026-W07', hours: 20 }],
    });

    await api.listAssignments('prj_x');
    expect(resourceCalls()[1].url).toBe('/api/v1/projects/prj_x/assignments');

    await api.listAssignments();
    expect(resourceCalls()[2].url).toBe('/api/v1/projects?limit=200');
    expect(resourceCalls()[3].url).toBe('/api/v1/projects/prj_x/assignments');
  });

  it('reads and deletes a single project through the core', async () => {
    await api.getProject('prj_x');
    expect(resourceCalls()[0].url).toBe('/api/v1/projects/prj_x');
    expect(resourceCalls()[0].init?.method).toBeUndefined();

    await api.deleteProject('prj_x');
    expect(resourceCalls()[1].url).toBe('/api/v1/projects/prj_x');
    expect(resourceCalls()[1].init?.method).toBe('DELETE');
  });

  it('sends absence writes to the core and deletes by id', async () => {
    await api.createAbsence({
      employee_id: 'emp_test1',
      type: 'vacation',
      start_date: '2026-02-16',
      end_date: '2026-02-18',
      hours_per_day: 8,
      approved: true,
      source: 'manual',
    });
    expect(resourceCalls()[0].url).toBe('/api/v1/absences');
    expect(resourceCalls()[0].init?.method).toBe('POST');
    expect(JSON.parse(String(resourceCalls()[0].init?.body))).not.toHaveProperty('source');

    await api.listAbsences({ employee_id: 'emp_test1' });
    expect(resourceCalls()[1].url).toBe('/api/v1/absences?limit=200&employee_id=emp_test1');

    await api.deleteAbsence('abs_test1');
    expect(resourceCalls()[2].url).toBe('/api/v1/absences/abs_test1');
    expect(resourceCalls()[2].init?.method).toBe('DELETE');
  });

  it('authenticates once and attaches the bearer token to core requests', async () => {
    await api.createEmployee(employee);
    await api.patchEmployee('emp_test1', { bench_flag: true });
    expect(calls.filter((call) => call.url.includes('/auth/login'))).toHaveLength(1);
    const headers = new Headers(resourceCalls()[0].init?.headers);
    expect(headers.get('Authorization')).toBe('Bearer test-token');
  });

  it('loads taxonomy reference data from the core', async () => {
    await expect(api.listRoles()).resolves.toEqual([]);
    expect(resourceCalls()[0].url).toBe('/api/v1/taxonomy/roles?limit=200&offset=0');
    await expect(api.listCompetencies()).resolves.toEqual([]);
    expect(resourceCalls()[1].url).toBe('/api/v1/taxonomy/competencies?limit=200&offset=0');
  });

  it('paginates taxonomy pages beyond the contract limit of 200', async () => {
    const competency: Competency = {
      competency_id: 'lang_go',
      name_en: 'Go',
      name_ru: 'Go',
      category: 'language',
      synonyms: ['golang'],
      roles: ['backend_engineer'],
      level_scale: 5,
    };
    const page = (offset: number, items: Competency[]) =>
      jsonResponse({ items, meta: { limit: 200, offset, total: 201 } });
    await api.listRoles();
    const taxonomyCalls: string[] = [];
    vi.mocked(fetch)
      .mockImplementationOnce(async (url) => {
        taxonomyCalls.push(String(url));
        return page(0, Array(200).fill(competency));
      })
      .mockImplementationOnce(async (url) => {
        taxonomyCalls.push(String(url));
        return page(200, [competency]);
      });

    await expect(api.listCompetencies()).resolves.toHaveLength(201);
    expect(taxonomyCalls).toEqual([
      '/api/v1/taxonomy/competencies?limit=200&offset=0',
      '/api/v1/taxonomy/competencies?limit=200&offset=200',
    ]);
  });
});
