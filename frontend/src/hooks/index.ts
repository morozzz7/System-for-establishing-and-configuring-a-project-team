/**
 * React hooks for data fetching.
 * Uses the API client (currently returning mocks).
 * Swap to real API by changing only src/api/client.ts.
 */
import { useState, useEffect, useCallback, useMemo } from 'react';
import * as api from '../api/client';
import type { Employee } from '../types';

interface AsyncState<T> {
  data: T | null;
  loading: boolean;
  error: string | null;
}

function useAsync<T>(fetcher: () => Promise<T>, deps: unknown[] = []): AsyncState<T> & { refetch: () => void } {
  const [state, setState] = useState<AsyncState<T>>({ data: null, loading: true, error: null });

  const refetch = useCallback(() => {
    setState({ data: null, loading: true, error: null });
    fetcher()
      .then((data) => setState({ data, loading: false, error: null }))
      .catch((err) => setState({ data: null, loading: false, error: String(err) }));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  useEffect(() => {
    refetch();
  }, [refetch]);

  return { ...state, refetch };
}

// ──── Employees ────

export function useEmployees() {
  const state = useAsync(api.listEmployees, []);
  return { ...state, refetch: () => { api.invalidateEmployees(); state.refetch(); } };
}

export function useEmployeeNames() {
  return useEmployeeNameFormatter(useEmployees().data);
}

export function useEmployeeNameFormatter(data: Employee[] | null) {
  const names = useMemo(() => {
    const counts = new Map<string, number>();
    for (const employee of data ?? []) {
      const name = employee.display_name?.trim().toLocaleLowerCase('ru');
      if (name) counts.set(name, (counts.get(name) ?? 0) + 1);
    }
    return new Map(data?.map((employee) => {
      const name = employee.display_name?.trim();
      const duplicate = name && (counts.get(name.toLocaleLowerCase('ru')) ?? 0) > 1;
      return [employee.employee_id, name ? (duplicate ? `${name} · ${employee.employee_id}` : name) : employee.employee_id];
    }));
  }, [data]);
  return useCallback((id: string) => names.get(id) ?? id, [names]);
}

export function useEmployee(employeeId: string) {
  return useAsync(() => api.getEmployee(employeeId), [employeeId]);
}

export function useEmployeeCompetencies(employeeId: string) {
  return useAsync(() => api.listEmployeeCompetencies(employeeId), [employeeId]);
}

export function useEmployeeCapacity(employeeId: string, fromWeek: string, toWeek: string) {
  return useAsync(
    () => api.getEmployeeCapacity(employeeId, fromWeek, toWeek),
    [employeeId, fromWeek, toWeek],
  );
}

// ──── Projects ────

export function useProjects() {
  return useAsync(() => api.listProjects(), []);
}

export function useProject(projectId: string) {
  return useAsync(() => api.getProject(projectId), [projectId]);
}

// ──── Assignments ────

export function useAssignments(projectId?: string) {
  return useAsync(() => api.listAssignments(projectId), [projectId]);
}

// ──── Absences ────

export function useAbsences() {
  return useAsync(() => api.listAbsences(), []);
}

// ──── Reference data ────

export function useRoles() {
  return useAsync(() => api.listRoles(), []);
}

export function useCompetencies() {
  return useAsync(() => api.listCompetencies(), []);
}

export function useGrades() {
  return useAsync(() => api.listGrades(), []);
}

export function useWeightPresets() {
  return useAsync(() => api.listWeightPresets(), []);
}

export function useWeightPresetsWithMetrics() {
  return useAsync(() => api.getWeightPresetsWithMetrics(), []);
}

export function useMetricDefinitions() {
  return useAsync(() => api.listMetricDefinitions(), []);
}

export function usePlanningParams() {
  return useAsync(() => api.getPlanningParams(), []);
}
