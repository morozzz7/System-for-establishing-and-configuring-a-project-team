# ADR-002: Time model - ISO weeks, integer hours, three-point effort, explicit reserve

- Status: accepted
- Date: 2026-11-05
- Deciders: architecture (orchestrator), backend, ml, qa-eval
- Related: `docs/PLAN.md` 1.2, `contracts/reference.yaml` (`time_model`, `schedule`), `docs/METRICS.md` M2/M5/M6, ADR-003
- Supersedes: none

## Context

Everything downstream depends on one arithmetic choice: how "work" becomes "time".
The brief arrives in natural language ("3 months", "2 backend devs for half a year"), effort comes
from the LLM as person-days, the manager thinks in weeks and people, and capacity comes from a Russian
production calendar with holidays and transferred working days.

Failure modes we want to avoid:
- floating-point drift making G2 (overload) flip between "exactly at capacity" and "1 second over";
- two different notions of "week" (calendar week vs rolling 7 days) between UI, Go and ML;
- effort entered as a single number, which hides uncertainty and makes P50/P80 meaningless;
- "40 h/week" silently meaning 40 productive hours while 20% goes to meetings anyway.

## Decision

### 1. The grid is ISO-8601 weeks, Monday-based

- Week key: `YYYY-Www` (`2026-W07`), pattern `^[0-9]{4}-W(0[1-9]|[1-4][0-9]|5[0-3])$`.
- A week has 5 working days by default; the actual count comes from the RU production calendar and is
  reported per week as `working_days` (0..5, 0 on a fully non-working week).
- The planning horizon is an explicit ordered list of weeks (`horizon.weeks`), max 104 weeks,
  default 16. No "week 1..N" relative indices anywhere in contracts - relative indices are how
  off-by-one bugs become vacations that start a week early.

### 2. Capacity and load are hours with one decimal place; effort is person-days

- `1 person-day = 8 hours` (fixed, not derived from the calendar).
- Capacity and assignment values are hours per week with at most one decimal place: the reserve is
  prorated by working days (`40 * 0.2 * 4/5 = 6.4`), so integer-only hours would silently distort the
  weekly norm on short weeks.
- The canonical internal unit is **integer minutes**: every hour value is converted to minutes at the
  edge, all comparisons (G2, `free_hours`) are done in minutes, and the result is converted back.
  There is no floating-point comparison anywhere in the gate path, so "exactly at capacity" is exact.
- The solver receives hours but an explicit integer tolerance applies to the *objective* only (1e-6),
  never to constraints.
- G2 is therefore exact: `sum(hours) <= free_hours` in integer minutes.

### 3. Reserve is explicit and never implicit

`free_hours(i, w) = max(0, norm_hours(w) - reserved_hours(i, w) - assigned_hours(i, w) - absence_hours(i, w))`

- `norm_hours(w) = working_days(w) * 8` (so a holiday week is smaller, not "compensated later").
- `reserved_hours = weekly_hours_norm * reserve_ratio` with `reserve_ratio = 0.2` by default:
  meetings, support, code review, unplanned work. The reserve is subtracted **before** the optimizer
  sees the matrix, so the solver cannot spend it.
- `assigned_hours` = existing assignments on other projects (source: `assignment` table).
- `absence_hours` = prorated by working days of the absence inside the week (ADR-003).

### 4. Effort is always a three-point estimate

`{min_person_days, likely_person_days, max_person_days}` with `min <= likely <= max`, enforced by
`contracts/json-schema/common.schema.json#/$defs/EffortRange`. A single number is a contract
violation; a degenerate range is allowed only with a documented assumption.
`docs/METRICS.md` rule 5 ("never a single number") is therefore a schema-level rule, not a guideline.

### 5. Duration is Monte Carlo with a fixed seed

- N = 2000 samples, seed = 42 (configurable, always recorded in `duration.monte_carlo`).
- Sampling: piecewise-linear (triangular) over `{min, likely, max}` per requirement, then a
  deterministic schedule simulation over the real capacity matrix and absences. No LLM in the loop.
- Output: `p50_weeks`, `p80_weeks` (and working days), `p_on_time` when a deadline exists,
  `slack_weeks = (deadline - p80) / deadline`.
- Reported duration is always a pair; the UI must not show a single "the project takes 11 weeks".

### 6. Cost is dimensionless

`cost_units = sum(person_hours * grade_cost_coefficient)` with J=1.0, M=1.4, S=1.9, L=2.6, P=3.4
(`contracts/reference.yaml`). M6 compares variants relative to `C_min`/`C_ref`, so no currency is
needed and no salary can leak (privacy rule 2).

### 7. Deadlines and start dates

- `requested_start_week` / `deadline_week` are ISO weeks, not dates: the manager plans in weeks and
  half-weeks would reintroduce the ambiguity we just removed.
- If the deadline is absent, M5 falls back to `T_best / T50`; if it is present, `M5 = P(T <= D)`.
- Assignment granularity is a full week. Sub-week granularity is explicitly out of scope for v1
  (a person switching projects mid-week is represented by two weeks with different shares, and by
  `min_assignment_hours_per_week` to avoid fragmentation, M10).

## Alternatives considered

| Alternative | Why rejected |
|---|---|
| Rolling 7-day windows from project start | Not comparable across projects, breaks the shared capacity matrix, calendar holidays become project-specific |
| Person-days as the primary unit | Cannot express "2 days per week for 8 weeks" without a conversion that hides rounding |
| Implicit 20% reserve | The optimizer would consume meeting time and produce plans that are feasible on paper only |
| Floating-point hours | G2 boundary becomes non-deterministic; fails the "0 violations" promise |
| Real dates (`2026-02-09`) for the grid | Weeks are the planning unit for the customer; dates invite timezone/DST and partial-week bugs |
| PERT beta instead of triangular | Different tail assumptions, no evidence it is more accurate for our data; fixed-seed reproducibility matters more |

## Consequences

- The calendar (RU production calendar with holidays and transferred days) is a first-class data
  dependency and must be versioned per year. Missing calendar data = hard failure (G5), never a silent
  5-day week.
- `weekly_capacity` is materialized per employee per week; 500 employees x 104 weeks = 52 000 rows,
  fine for Postgres, and it makes the matrix auditable and cacheable.
- Duration is always a distribution; the UI shows P50 with P80 as risk. `docs/METRICS.md` M5 is
  computed from it, so no separate "duration estimator" can creep in.
- Person-days <-> hours conversion is fixed at 8; changing it later is a metrics version bump.
