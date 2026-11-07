# Frontend — Optimal Project Team Formation System

React 18 + TypeScript + Vite + Ant Design frontend for the team formation decision-support system.

**UI language:** Russian (i18n-ready, but currently hardcoded ru-RU strings).  
**Code/comments/docs:** English.

## Quick start

```bash
# Install dependencies
npm install

# Start dev server (http://localhost:5173)
npm run dev

# Run tests
npm run test

# Lint
npm run lint

# Type check
npx tsc --noEmit

# Production build
npm run build
```

## Folder structure

```
src/
  app/              Layout, router, providers
  pages/            DashboardPage, EmployeesPage, ProjectsPage, etc.
  components/       Shared UI: LevelBar, GradeTag, StatusTag, Loading, etc.
  api/              Typed API client (currently returns mocks)
  mocks/            In-memory fixtures matching contracts/openapi.yaml
  types/            DTOs mirroring openapi.yaml schemas
  hooks/            useEmployees, useProjects, useAbsences, etc.
  utils/            ISO week helpers (ADR-002)
```

## Current state (Phase 2.5)

Implemented screens (all on mock data):
- **Dashboard** — summary stats (employees, projects, absences)
- **Employees** — list with filters (grade, role, bench), detail drawer with competencies and capacity
- **Projects** — list with status filter, create form
- **Assignments** — table of weekly assignments
- **Absences** — table with delete action
- **Reference data** (read-only):
  - Roles (12 IT roles from taxonomy)
  - Competencies (grouped by category)
  - Grades (J/M/S/L/P with cost coefficients)
  - Weight presets (Fast/Economical/Balanced/Low-risk)
  - Planning parameters

All screens render from mock data. No real API calls yet.

## Mock data

- **30 employees** with diverse grades (J/M/S/L), roles, and competencies
- **5 projects** with statuses: planned, active, done
- **40+ assignments** across employees and weeks (ISO 8601 format: `2026-W07`)
- **15 absences** (vacation, sick_leave, training, business_trip, other)
- All IDs use pseudonyms: `emp_0001`, `prj_0001`, `asg_0001`, `abs_0001`
- **No personal data**: no names, emails, phones, salaries

## Swapping mocks for real API

The API layer (`src/api/client.ts`) currently returns mock data from `src/mocks/`. Each function has the same signature as the future fetch-based client. To switch to real API:

1. Replace each function body in `src/api/client.ts` with `fetch()` calls to `/api/v1/*` endpoints.
2. Keep the same return types — no changes needed in hooks or pages.
3. Add error handling (4xx/5xx responses) and loading states (already supported by hooks).

Example:

```typescript
// Before (mock)
export async function listEmployees(): Promise<Employee[]> {
  await delay();
  return [...mockEmployees];
}

// After (real API)
export async function listEmployees(): Promise<Employee[]> {
  const res = await fetch('/api/v1/employees');
  if (!res.ok) throw new Error('Failed to fetch employees');
  const data = await res.json();
  return data.items; // EmployeePage shape
}
```

## Testing

- **Unit tests:** `src/utils/week.test.ts` (ISO week utilities), `src/mocks/data.test.ts` (data invariants)
- **Component tests:** `src/app/Layout.test.tsx` (menu rendering)
- Run: `npm run test` (Vitest)

## Contract alignment

All DTOs in `src/types/index.ts` match `contracts/openapi.yaml` schemas. Field names use snake_case to minimize friction when switching to real API.

Key contracts implemented:
- `GET /api/v1/employees` → `listEmployees()`
- `GET /api/v1/employees/{employeeId}/competencies` → `listEmployeeCompetencies()`
- `GET /api/v1/employees/{employeeId}/capacity` → `getEmployeeCapacity()`
- `GET /api/v1/projects` → `listProjects()`
- `GET /api/v1/projects/{projectId}/assignments` → `listAssignments()`
- `GET /api/v1/absences` → `listAbsences()`
- `GET /api/v1/taxonomy/roles` → `listRoles()`
- `GET /api/v1/taxonomy/competencies` → `listCompetencies()`
- `GET /api/v1/reference/grades` → `listGrades()`
- `GET /api/v1/reference/weight-presets` → `listWeightPresets()`
- `GET /api/v1/reference/planning-params` → `getPlanningParams()`

## Next steps (not in this task)

- Brief input screen (Phase 3.7)
- Requirements review with evidence highlighting
- Team variants comparison (radar charts, parallel coordinates)
- Constraint editor + recalculation
- Explanations UI
- Deficits and external cooperation view
- Real API integration (Phase 3.6/3.7)
- i18n framework (currently Russian only)

## Tech stack

- React 18.3 + TypeScript 5.6
- Vite 6 (build tool)
- Ant Design 5.22 (UI components, Russian locale)
- React Router 6.28 (routing)
- Vitest 2.1 (testing)
- ESLint 9 (linting, flat config)

## Quality gates

- [x] `npx tsc --noEmit` — 0 errors
- [x] `npm run lint` — 0 errors/warnings
- [x] `npm run test` — all tests pass
- [x] `npm run build` — production build succeeds
- [x] All screens render without runtime errors
- [x] Navigation between all pages works
- [x] No personal data in fixtures
