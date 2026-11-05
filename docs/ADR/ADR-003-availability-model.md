# ADR-003: Availability model - one authoritative capacity matrix, computed only in Go

- Status: accepted
- Date: 2026-11-05
- Deciders: architecture (orchestrator), backend, ml
- Related: `docs/PLAN.md` 1.2/2.4, `contracts/openapi.yaml` (`CapacityWeek`), `contracts/ml-openapi.yaml` (`CandidateCapacityWeek`), ADR-002, ADR-004
- Supersedes: none

## Context

Whether a person can be assigned in week W is decided by four independent sources: the norm and its
reserve (ADR-002), already-existing project assignments, approved absences, and the production
calendar. Getting one of them wrong in one place produces the two failures the customer will notice
immediately:

- someone is planned during vacation (G1),
- someone ends up at 118% load (G2).

If both Go and Python compute availability, they will disagree - always, eventually - and the
disagreement will look like a solver bug.

## Decision

### 1. Go computes; ML consumes

The capacity matrix is computed **only** by Go (`internal/availability`, task 2.4) and passed to the
optimizer as a number per `(employee, week)`:

```json
{ "employee_id": "emp_7fa2", "week": "2026-W07",
  "working_days": 4, "norm_hours": 32, "reserved_hours": 6.4,
  "assigned_hours": 12, "absence_hours": 0, "free_hours": 13.6,
  "unavailable": false }
```

The ML service treats `free_hours` and `unavailable` as hard facts and has no way to recompute them
(it does not receive absences, other projects or the calendar). It must never exceed `free_hours`:
that is constraint G2 in the model. `unavailable = true` forbids any positive assignment (G1).

### 2. Capacity formula (single definition)

For employee `i`, week `w`:

```
norm_hours(i,w)      = working_days(w) * hours_per_day            # 8, calendar-aware
reserved_hours(i,w)  = weekly_hours_norm(i) * reserve_ratio(i) * (working_days(w) / 5)
assigned_hours(i,w)  = sum of hours of active assignments in w     # existing projects only
absence_hours(i,w)   = hours_per_day * working_days(w) * (absence share of w)
free_hours(i,w)      = max(0, norm_hours - reserved - assigned - absence)
unavailable(i,w)     = working_days(w) == 0 or absence covers all working days
load_ratio(i,w)      = assigned_hours / max(norm_hours, 1)
```

Rules that follow from this and are tested as properties (task 2.4):

- `free_hours >= 0` always; `assigned + reserved + absence <= norm` for every consistent input.
- A week with 0 working days has `free_hours = 0` and `unavailable = true` (holidays cannot be spent).
- The reserve is prorated by the actual working days, so a 4-day week reserves 4/5 of the reserve.
- Absences are prorated by working days *inside* the absence range: a vacation Mon-Thu in a week with
  a Friday holiday yields 3 absent working days, not 4 and not 5.

### 3. Absence semantics

- Absence is a date range (`start_date`, `end_date` inclusive), `hours_per_day` (usually 8), a type
  (`vacation`, `sick_leave`, `training`, `business_trip`, `parental_leave`, `other`) and an
  `approved` flag. Only `approved = true` removes capacity.
- Overlapping absences for the same employee are rejected (`CONFLICT_ABSENCE_OVERLAP`), not merged
  silently: double-counting would inflate `absence_hours` above the norm.
- Absences are converted to weeks at read time using the production calendar; the week view is derived,
  the date range is the source of truth (so a later calendar change re-derives correctly).
- Cancelling an absence is a soft delete with an audit event: a plan may have been built on it.

### 4. Assignments, not "load"

- Existing load is a list of `(employee, project, week, hours)` rows. There is no free-form
  "utilization %": percentages are derived (`load_ratio`) and never stored.
- Creating an assignment that would push a week above the norm is rejected at write time (G2 guard on
  the API, `409 CONFLICT_OVERLOAD`) unless the caller explicitly passes the override used by the
  manager UI, which is audited.
- Bench status (`bench_flag`) is an input to M11; it is not derived from capacity, because a person at
  50% on a project is still not on the bench in HR terms.

### 5. Horizon and edge weeks

- The horizon is clamped to `[requested_start_week, max(deadline_week, start + default_horizon)]`.
- Weeks outside the horizon do not exist for the solver. An assignment that would need to start before
  the horizon is reported as a deficit with `earliest_week` facts, not silently shifted.
- `G5` (data integrity) fails the run when a candidate has no capacity rows in the horizon, has an
  invalid date range, or references an inactive employee.

### 6. Recalculation

Manual constraint edits (force include/exclude, team size, budget, deadline) never touch the capacity
matrix. A recalculate run re-reads assignments and absences, recomputes the matrix, and re-solves; the
previous run stays immutable for audit. This keeps the response to "why did the team change?" honest:
either the input changed (facts + audit) or the constraint changed (override + audit).

## Alternatives considered

| Alternative | Why rejected |
|---|---|
| ML computes capacity from raw absences/assignments | Duplicated calendar logic in two languages; disagreements become solver bugs; ML would need more personal-ish data |
| Percent-based availability (`0.8 FTE`) | Cannot represent holidays, partial weeks, or exact G2 checks; the customer thinks in days and weeks |
| Absences panel merged into assignments | Loses the reason and the approval state, breaks audit and the "no assignment during vacation" rule |
| Storing a denormalized `free_hours` without derivation | Goes stale after any assignment change; recomputation is cheap (< 1 ms per employee-week) |
| Optimizer allowed to "borrow" from the reserve | Produces plans that are feasible only if people skip meetings - i.e. not feasible |

## Consequences

- One implementation, one test suite (property tests, task 2.4), one place to fix calendar bugs.
- ML payload grows: 500 candidates x ~16 weeks x ~60 bytes ≈ 0.5 MB uncompressed; acceptable, gzip
  enabled, and the horizon is bounded (104 weeks max).
- Every run is reproducible from stored inputs: the variant references `run_id`, and the run stores
  `metrics_version`, `contract_version`, `seed`.
- If the production calendar is missing for a year, the system fails closed (G5) instead of guessing.
