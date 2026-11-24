/**
 * HTTP-mode client branches: resource mutations must reach the Go core, never the mock fixtures.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Assignment, Employee, EmployeeCompetency } from '../types';

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
    vi.stubEnv('VITE_API_BASE_URL', '/api/v1');
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: FetchInit) => {
        calls.push({ url, init });
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
    expect(calls[0].url).toBe('/api/v1/employees');
    expect(calls[0].init?.method).toBe('POST');
    const createBody = JSON.parse(String(calls[0].init?.body)) as Record<string, unknown>;
    expect(createBody).not.toHaveProperty('active');
    expect(createBody).not.toHaveProperty('cost_coefficient');

    await api.patchEmployee('emp_test1', { bench_flag: true, cost_coefficient: 2 });
    expect(calls[1].url).toBe('/api/v1/employees/emp_test1');
    expect(calls[1].init?.method).toBe('PATCH');
    expect(JSON.parse(String(calls[1].init?.body))).toEqual({ bench_flag: true });

    const comps: EmployeeCompetency[] = [
      { competency_id: 'lang_go', level: 4, confidence: 0.9, source: 'manager_review' },
    ];
    await api.replaceEmployeeCompetencies('emp_test1', comps);
    expect(calls[2].url).toBe('/api/v1/employees/emp_test1/competencies');
    expect(calls[2].init?.method).toBe('PUT');
  });

  it('uses from_week/to_week for capacity', async () => {
    await api.getEmployeeCapacity('emp_test1', '2026-W07', '2026-W08');
    expect(calls[0].url).toBe('/api/v1/employees/emp_test1/capacity?from_week=2026-W07&to_week=2026-W08');
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
    expect(calls[0].url).toBe('/api/v1/projects/prj_x/assignments');
    expect(calls[0].init?.method).toBe('POST');
    expect(JSON.parse(String(calls[0].init?.body))).toEqual({
      assignments: [{ employee_id: 'emp_test1', role_id: 'backend_engineer', week: '2026-W07', hours: 20 }],
    });

    await api.listAssignments('prj_x');
    expect(calls[1].url).toBe('/api/v1/projects/prj_x/assignments');

    await api.listAssignments();
    expect(calls[2].url).toBe('/api/v1/projects?limit=200');
    expect(calls[3].url).toBe('/api/v1/projects/prj_x/assignments');
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
    expect(calls[0].url).toBe('/api/v1/absences');
    expect(calls[0].init?.method).toBe('POST');
    expect(JSON.parse(String(calls[0].init?.body))).not.toHaveProperty('source');

    await api.listAbsences({ employee_id: 'emp_test1' });
    expect(calls[1].url).toBe('/api/v1/absences?limit=200&employee_id=emp_test1');

    await api.deleteAbsence('abs_test1');
    expect(calls[2].url).toBe('/api/v1/absences/abs_test1');
    expect(calls[2].init?.method).toBe('DELETE');
  });

  it('fails loudly for taxonomy endpoints the core does not expose', async () => {
    await expect(api.listRoles()).rejects.toMatchObject({ status: 501, code: 'NOT_IMPLEMENTED' });
    await expect(api.listCompetencies()).rejects.toMatchObject({ status: 501, code: 'NOT_IMPLEMENTED' });
    expect(calls).toHaveLength(0);
  });
});
