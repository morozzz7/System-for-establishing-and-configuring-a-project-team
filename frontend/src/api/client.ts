/**
 * API client layer.
 * Currently returns mock data from src/mocks/.
 * Each function signature matches the future fetch-based API client exactly,
 * so swapping to real HTTP calls requires only changing the implementation.
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
} from '../types';
import {
  mockRoles,
  mockCompetencies,
  mockGrades,
  mockWeightPresets,
  mockPlanningParams,
  mockEmployees,
} from '../mocks/reference';
import {
  mockProjects,
  mockAssignments,
  mockAbsences,
  mockEmployeeCompetencies,
  mockCapacityWeeks,
} from '../mocks/data';

// Simulate async API latency (100-300ms)
const delay = () => new Promise((r) => setTimeout(r, 100 + Math.random() * 200));

// ──── Employees ────

export async function listEmployees(_params?: {
  role_id?: string;
  grade?: string;
  bench_only?: boolean;
}): Promise<Employee[]> {
  await delay();
  return [...mockEmployees];
}

export async function getEmployee(employeeId: string): Promise<Employee | null> {
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
  if (!existing) throw new Error('Not found');
  return { ...existing, ...patch };
}

export async function listEmployeeCompetencies(
  employeeId: string,
): Promise<EmployeeCompetency[]> {
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
  _fromWeek: string,
  _toWeek: string,
): Promise<CapacityWeek[]> {
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

// ──── Projects ────

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

// ──── Assignments ────

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

// ──── Absences ────

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

// ──── Taxonomy / Reference ────

export async function listRoles(): Promise<Role[]> {
  await delay();
  return [...mockRoles];
}

export async function listCompetencies(): Promise<Competency[]> {
  await delay();
  return [...mockCompetencies];
}

export async function listGrades(): Promise<GradeBand[]> {
  await delay();
  return [...mockGrades];
}

export async function listWeightPresets(): Promise<WeightPreset[]> {
  await delay();
  return [...mockWeightPresets];
}

export async function getPlanningParams(): Promise<PlanningParams> {
  await delay();
  return { ...mockPlanningParams };
}
