/**
 * Portfolio project derived from an approved run.
 *
 * One project per brief: the id is a deterministic function of the brief id, so approving a
 * recalculation run reuses the existing project instead of creating a duplicate.
 */
import type { Assignment, Brief, Project, TeamRun, Variant } from '../types';
import { parseWeek, weekStart } from "./week";

const MAX_ID_SUFFIX = 12;
const MIN_ID_SUFFIX = 4;

export function projectFromRun(run: TeamRun, brief: Brief | null): Project {
  const suffix = briefIdSuffix(run.brief_id);
  return {
    project_id: `prj_${suffix}`,
    code: `PRJ-${suffix.toUpperCase()}`,
    name: brief?.title.trim() || `Проект ${run.brief_id}`,
    status: 'planned',
    priority: 3,
    criticality: 'normal',
    start_week: run.horizon.start_week,
    end_week_planned: run.horizon.end_week,
    replaces_other_projects: false,
  };
}

function briefIdSuffix(briefId: string): string {
  const cleaned = briefId.toLowerCase().replace(/^brf_?/, '').replace(/[^a-z0-9]/g, '');
  const suffix = cleaned.slice(-MAX_ID_SUFFIX);
  return suffix.padStart(MIN_ID_SUFFIX, '0');
}

/**
 * Planned assignments of an approved variant, in the shape accepted by the bulk endpoint.
 * A variant carries one weekly row per requirement, while the endpoint allows only one row per
 * employee and week, so hours are summed across requirements.
 */
export function plannedAssignments(variant: Variant, projectId: string): Omit<Assignment, 'assignment_id'>[] {
  const rolesByEmployee = new Map(variant.members.map((member) => [member.employee_id, member.role_id]));
  const hoursByEmployeeWeek = new Map<string, number>();
  for (const weekly of variant.weekly_assignments) {
    const key = `${weekly.employee_id}|${weekly.week}`;
    hoursByEmployeeWeek.set(key, (hoursByEmployeeWeek.get(key) ?? 0) + weekly.hours);
  }
  return [...hoursByEmployeeWeek].flatMap(([key, hours]) => {
    const [employeeId, week] = key.split('|');
    const roleId = rolesByEmployee.get(employeeId);
    if (!roleId) return [];
    return [{
      project_id: projectId,
      employee_id: employeeId,
      role_id: roleId,
      week,
      hours,
      source: 'plan' as const,
    }];
  });
}

export function chunkAssignments<T>(items: T[], size: number): T[][] {
  const chunks: T[][] = [];
  for (let i = 0; i < items.length; i += size) chunks.push(items.slice(i, i + size));
  return chunks;
}

export function effectiveProject(project: Project, now = new Date()): Project {
  const week = parseWeek(project.start_week);
  if (project.status !== 'planned' || !week) return project;
  const startsAt = weekStart(week.year, week.week).getTime() - 3 * 60 * 60 * 1000;
  return now.getTime() >= startsAt ? { ...project, status: 'active' } : project;
}
