/**
 * Portfolio project derived from an approved run.
 *
 * One project per brief: the id is a deterministic function of the brief id, so approving a
 * recalculation run reuses the existing project instead of creating a duplicate.
 */
import type { Brief, Project, TeamRun } from '../types';

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
