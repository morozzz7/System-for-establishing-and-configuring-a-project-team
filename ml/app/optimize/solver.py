"""OR-Tools CP-SAT team-formation optimizer for POST /v1/optimize (Optimizer v2, task 3.4).

Design (see ``README.md``):
- Hard constraints G1..G5 are modeled *exactly*; the solver never returns a relaxed or
  infeasible plan. G4 (user constraints) is never relaxed in any mode: if it makes the
  roster impossible the caller gets ``INFEASIBLE`` with the blocking constraint ids.
- **Deficits via slack** (``options.allow_deficits = true``, the default): every ``must``
  requirement gets an integer ``slack[r]`` (uncovered hours). A shortage is reported as a
  ``deficits[]`` entry (competency/role, requirement ids, positive ``person_days``,
  ``external_cooperation_flag`` + ``DEFICIT_*`` codes) instead of a blanket ``INFEASIBLE``,
  so the caller always receives the best partial team. With ``allow_deficits = false`` the
  slack is omitted and an uncovered ``must`` makes the model ``INFEASIBLE`` (v1 behavior).
  Slack is minimized lexicographically before any quality term (a penalty larger than the
  whole quality objective), so a deficit never hides a coverable requirement.
- **k = 2..3 diverse variants**: after the base solve, the model is re-solved with a linear
  exact "Jaccard distance >= min_jaccard_diversity from every roster found so far"
  constraint. A roster that cannot be made diverse is dropped, never duplicated.
- **Non-domination**: the candidate pool is filtered on the advisory M1..M12 vector
  (``metrics.dominates``), so a dominated variant is replaced by the next diverse solution.
- The CP-SAT objective is a *linearized coverage proxy* (M1 competency coverage via a
  coverage flag, M2 effort coverage via quality-weighted hours), weighted by the request's
  ``weights.M1`` / ``weights.M2`` and by criticality (must=1.0 / should=0.5 / nice=0.2).
  A tiny cost term acts as a lexicographic tie-breaker (coverage first, then cost).
- The advisory M1..M12 + score are computed *post-hoc* in ``metrics.py`` and are not part
  of the search objective. ``objective_value`` reported in the response is the normalized
  linearized objective; ``best_bound``/``optimality_gap`` come from CP-SAT directly.
- Determinism: ``num_search_workers = 1``, a fixed ``random_seed = request.seed``, a fixed
  variable construction order, a fixed attempt order and no wall-clock-dependent decision.
  Timing fields are excluded from the fingerprint by definition.
- Performance: CP-SAT presolve is disabled on purpose (it consumes the per-attempt budget
  without producing an incumbent at 500 x 30 scale) and the request budget is split
  deterministically, reserving a fixed share for model build. If the first attempt returns
  no incumbent at all, the remaining budget is spent on one objective-free feasibility solve
  so the caller still receives the best found team with an honest ``FEASIBLE`` status
  ("graceful timeout") instead of nothing.

The optimizer receives an already-materialized capacity matrix
(``candidate.capacity[].free_hours`` / ``unavailable``) and must respect it; it never
computes availability itself ("solver proposes, Go verifies").
"""

from __future__ import annotations

import math
from dataclasses import dataclass, field
from typing import Optional

from ortools.sat.python import cp_model

from .metrics import Assignment, compute_advisory_metrics, dominates
from .schemas import (
    ActiveConstraintRef,
    InfeasibilityReport,
    MlDeficit,
    OptimizeRequest,
)

SOLVER_NAME = "ortools-cp-sat"


def solver_version() -> str:
    """Return the installed ortools version, falling back to the contract pin."""
    try:
        import ortools  # noqa: PLC0415

        return getattr(ortools, "__version__", "9.11.4210")
    except Exception:  # pragma: no cover - ortools is a hard dependency
        return "9.11.4210"


SOLVER_VERSION = solver_version()

CRIT_COEF = {"must": 100, "should": 50, "nice": 20}
COVER_BONUS = 1000
HOURS_PER_DAY = 8
COST_SCALE = 100
#: Safety margin added to the requested Jaccard threshold in the *model* constraint so
#: floating-point rounding in the post-hoc check can never turn a cut into a near-miss.
DIVERSITY_EPS = 0.001
#: Share of ``time_limit_ms`` reserved for model build + response assembly. The contract
#: says ``time_limit_ms`` is enforced strictly for the *whole* call, while CP-SAT only
#: bounds search time, so the build cost has to be budgeted explicitly. The reserve is a
#: fixed fraction of the request value (never a clock reading), so the budget split stays
#: deterministic: 500 x 30 x 16 weeks needs ~1.8 s to build, i.e. 18% of a 10 s request.
BUILD_RESERVE_SHARE = 0.3


class SolverValidationError(Exception):
    """Semantically invalid input that should map to HTTP 422 (never a solver bug)."""

    def __init__(self, detail: str, field: str | None = None, code: str = "VALIDATION_FAILED"):
        super().__init__(detail)
        self.detail = detail
        self.field = field
        self.code = code


@dataclass
class VariantOutcome:
    """One concrete team variant (roster + weekly assignments + deficits + metrics)."""

    status: str
    members: list[str]
    assignments: list[Assignment]
    allocated_hours: dict[str, float]
    metrics: dict[str, Optional[float]]
    deficits: list[MlDeficit]
    unscheduled_requirements: list[str]
    objective_value: Optional[float]
    best_bound: Optional[float]
    optimality_gap: Optional[float]
    total_slack_hours: int
    attempt: int
    cost_units: float = 0.0

    @property
    def roster(self) -> frozenset[str]:
        return frozenset(self.members)

    @property
    def score(self) -> float:
        return float(self.metrics.get("score") or 0.0)


@dataclass
class SolveOutcome:
    """Everything the router needs to build an ``OptimizeResponse``.

    The single-variant accessors (``members``/``assignments``/``metrics``/...) delegate to
    the best variant so v1 call sites keep working unchanged; ``variants`` carries all
    diverse, non-dominated variants in descending advisory-score order.
    """

    status: str
    wall_time_ms: int
    num_workers: int
    num_vars: int
    num_constraints: int
    presolve_time_ms: int
    variants: list[VariantOutcome] = field(default_factory=list)
    infeasibility: Optional[InfeasibilityReport] = None
    reason_codes: list[str] = field(default_factory=list)
    dropped_diversity_variants: int = 0

    @property
    def _primary(self) -> Optional[VariantOutcome]:
        return self.variants[0] if self.variants else None

    @property
    def members(self) -> list[str]:
        return self._primary.members if self._primary else []

    @property
    def assignments(self) -> list[Assignment]:
        return self._primary.assignments if self._primary else []

    @property
    def allocated_hours(self) -> dict[str, float]:
        return self._primary.allocated_hours if self._primary else {}

    @property
    def metrics(self) -> dict[str, Optional[float]]:
        return self._primary.metrics if self._primary else {}

    @property
    def deficits(self) -> list[MlDeficit]:
        return self._primary.deficits if self._primary else []

    @property
    def unscheduled_requirements(self) -> list[str]:
        return self._primary.unscheduled_requirements if self._primary else []

    @property
    def objective_value(self) -> Optional[float]:
        return self._primary.objective_value if self._primary else None

    @property
    def best_bound(self) -> Optional[float]:
        return self._primary.best_bound if self._primary else None

    @property
    def optimality_gap(self) -> Optional[float]:
        return self._primary.optimality_gap if self._primary else None


# ---------------------------------------------------------------------------
# Precomputation
# ---------------------------------------------------------------------------


class _Pre:
    """Deterministic precomputed tables derived from the request."""

    def __init__(self, request: OptimizeRequest):
        self.request = request
        self.weeks: list[str] = request.horizon.weeks
        self.week_idx: dict[str, int] = {w: i for i, w in enumerate(self.weeks)}
        self.n_weeks = len(self.weeks)

        # canonical (sorted) order for deterministic model construction
        self.candidates = sorted(request.candidates, key=lambda c: c.employee_id)
        self.emp_ids = [c.employee_id for c in self.candidates]
        self.cand = {c.employee_id: c for c in self.candidates}

        self.requirements = sorted(request.requirements, key=lambda r: r.requirement_id)
        self.req_ids = [r.requirement_id for r in self.requirements]
        self.req = {r.requirement_id: r for r in self.requirements}

        self.role = {c.employee_id: c.role_profile_id for c in self.candidates}
        self.cost = {c.employee_id: c.cost_coefficient for c in self.candidates}
        self.cost_int = {
            c.employee_id: int(round(c.cost_coefficient * COST_SCALE)) for c in self.candidates
        }
        self.bench = {c.employee_id: c.bench_flag for c in self.candidates}
        self.external = {
            c.employee_id: c.employment_type in ("contractor", "external_partner")
            for c in self.candidates
        }
        self.comp = {
            c.employee_id: {cc.competency_id: cc.level for cc in c.competencies}
            for c in self.candidates
        }
        self.exp = {c.employee_id: c.experience_score for c in self.candidates}

        # integer hours of free capacity per (employee, week); unavailable => 0
        self.cap: dict[str, dict[str, int]] = {}
        for c in self.candidates:
            cap_by_week: dict[str, int] = {}
            for cw in c.capacity:
                if cw.unavailable:
                    cap_by_week[cw.week] = 0
                else:
                    cap_by_week[cw.week] = _floor_hours(cw.free_hours)
            self.cap[c.employee_id] = cap_by_week

        self.eff_min_h = {
            r.requirement_id: int(math.ceil(r.effort.min_person_days * HOURS_PER_DAY - 1e-9))
            for r in self.requirements
        }
        self.eff_likely_h = {
            r.requirement_id: int(math.ceil(r.effort.likely_person_days * HOURS_PER_DAY - 1e-9))
            for r in self.requirements
        }
        self.likely_pd = {
            r.requirement_id: float(r.effort.likely_person_days) for r in self.requirements
        }

        c = request.constraints
        self.force_include = list(c.force_include)
        self.force_exclude = set(c.force_exclude)
        self.freeze = list(c.freeze_members)
        self.max_team = c.max_team_size
        self.min_team = c.min_team_size
        self.budget = c.budget_cap_units
        self.budget_int = int(round(c.budget_cap_units * COST_SCALE)) if c.budget_cap_units is not None else None
        self.deadline_idx = self.week_idx.get(c.deadline_week) if c.deadline_week else None
        self.forbid_external = bool(c.forbid_external)
        self.min_bus = c.min_bus_factor if c.min_bus_factor is not None else 1
        self.min_assign_h = c.min_assignment_hours_per_week if c.min_assignment_hours_per_week else 0.0
        self.min_assign_int = int(math.ceil(self.min_assign_h - 1e-9)) if self.min_assign_h > 0 else 0

        self.wM1_int = int(round(request.weights.M1 * 1000))
        self.wM2_int = int(round(request.weights.M2 * 1000))

        self.allow_deficits = bool(request.options.allow_deficits)
        self.k = int(request.k)
        self.min_jaccard = float(request.min_jaccard_diversity)

        # eligibility / qualification
        self.eligible: dict[tuple[str, str], bool] = {}
        self.qualified: dict[tuple[str, str], bool] = {}
        for emp in self.emp_ids:
            for r in self.requirements:
                e = self._is_eligible(emp, r)
                self.eligible[(emp, r.requirement_id)] = e
                self.qualified[(emp, r.requirement_id)] = e and self._is_qualified(emp, r)

    def _is_eligible(self, emp: str, r) -> bool:
        """Candidate may work on requirement r at all.

        Coverage is competency-based (M1): a competency requirement is covered by any
        candidate holding that competency at level >= 1, regardless of their primary
        role profile. Role matching applies only to role-only requirements
        (``competency_id is null``).
        """
        if r.competency_id is None:
            return self.role[emp] == r.role_id
        return r.competency_id in self.comp[emp]

    def _is_qualified(self, emp: str, r) -> bool:
        """Candidate covers r at the required level (G3 for must requirements)."""
        if r.competency_id is None:
            return self.role[emp] == r.role_id
        return self.comp[emp].get(r.competency_id, 0) >= r.level

    def assignable(self, emp: str, r) -> bool:
        """Final assignment-allowed predicate (external rules + must qualification)."""
        rid = r.requirement_id
        if not self.eligible[(emp, rid)]:
            return False
        if self.forbid_external and self.external[emp]:
            return False
        if (not r.allows_external) and self.external[emp]:
            return False
        if r.criticality == "must" and not self.qualified[(emp, rid)]:
            return False
        return True

    def assignable_candidates(self, r) -> list[str]:
        """Candidates that may be assigned to r at all (deterministic order)."""
        return [e for e in self.emp_ids if self.assignable(e, r)]

    def fit_coef(self, emp: str, r) -> int:
        """Integer level-fit coefficient in [0, 100] (100 at/above required level)."""
        if r.competency_id is None:
            return 100
        lvl = self.comp[emp].get(r.competency_id, 0)
        return min(100, int(round(100 * lvl / r.level)))

    def week_window(self, r) -> tuple[int, int]:
        """Inclusive [lo, hi] week indices for requirement r, honoring deadline."""
        lo = self.week_idx.get(r.earliest_week, 0) if r.earliest_week else 0
        hi = self.week_idx.get(r.latest_week, self.n_weeks - 1) if r.latest_week else self.n_weeks - 1
        if self.deadline_idx is not None:
            hi = min(hi, self.deadline_idx)
        lo = max(0, lo)
        hi = min(self.n_weeks - 1, hi)
        return lo, hi

    # ---- deficit / cause helpers -------------------------------------------

    def holding_competency(self, r) -> list[str]:
        """Candidates holding the competency at *any* level (role match for role-only)."""
        if r.competency_id is None:
            return [e for e in self.emp_ids if self.role[e] == r.role_id]
        return [e for e in self.emp_ids if r.competency_id in self.comp[e]]

    def max_level(self, r) -> int:
        if r.competency_id is None:
            return 5 if any(self.role[e] == r.role_id for e in self.emp_ids) else 0
        return max((self.comp[e].get(r.competency_id, 0) for e in self.emp_ids), default=0)


def _floor_hours(x: float) -> int:
    return int(math.floor(x + 1e-9))


def _validate_request(pre: _Pre) -> None:
    """Reject semantically invalid requests with a clear 422 (never silently ignore)."""
    c = pre.request.constraints
    emp_set = set(pre.emp_ids)

    unknown_inc = [e for e in pre.force_include if e not in emp_set]
    if unknown_inc:
        raise SolverValidationError(
            f"force_include references unknown candidate(s): {', '.join(unknown_inc)}",
            field="constraints.force_include",
        )
    unknown_freeze = [e for e in pre.freeze if e not in emp_set]
    if unknown_freeze:
        raise SolverValidationError(
            f"freeze_members references unknown candidate(s): {', '.join(unknown_freeze)}",
            field="constraints.freeze_members",
        )
    overlap = [e for e in pre.force_include if e in pre.force_exclude] + [
        e for e in pre.freeze if e in pre.force_exclude
    ]
    if overlap:
        raise SolverValidationError(
            f"candidate(s) both forced in and excluded: {', '.join(sorted(set(overlap)))}",
            field="constraints.force_exclude",
        )
    if c.max_team_size is not None and c.min_team_size is not None and c.min_team_size > c.max_team_size:
        raise SolverValidationError(
            f"min_team_size ({c.min_team_size}) > max_team_size ({c.max_team_size})",
            field="constraints.min_team_size",
        )
    if pre.forbid_external:
        ext_forced = [e for e in pre.force_include + pre.freeze if pre.external.get(e)]
        if ext_forced:
            raise SolverValidationError(
                f"forbid_external conflicts with force_include/freeze of external candidate(s): {', '.join(ext_forced)}",
                field="constraints.forbid_external",
            )
    if c.deadline_week is not None and c.deadline_week not in pre.week_idx:
        raise SolverValidationError(
            f"deadline_week ({c.deadline_week}) is outside the horizon",
            field="constraints.deadline_week",
        )
    for r in pre.requirements:
        if r.earliest_week is not None and r.earliest_week not in pre.week_idx:
            raise SolverValidationError(
                f"requirement {r.requirement_id} earliest_week ({r.earliest_week}) is outside the horizon",
                field=f"requirements.{r.requirement_id}.earliest_week",
            )
        if r.latest_week is not None and r.latest_week not in pre.week_idx:
            raise SolverValidationError(
                f"requirement {r.requirement_id} latest_week ({r.latest_week}) is outside the horizon",
                field=f"requirements.{r.requirement_id}.latest_week",
            )


# ---------------------------------------------------------------------------
# Model build
# ---------------------------------------------------------------------------

_STATUS_MAP = {
    cp_model.OPTIMAL: "OPTIMAL",
    cp_model.FEASIBLE: "FEASIBLE",
    cp_model.INFEASIBLE: "INFEASIBLE",
    cp_model.MODEL_INVALID: "MODEL_INVALID",
    cp_model.UNKNOWN: "UNKNOWN",
}


class _Model:
    """CP-SAT model for one request; diversity cuts are added incrementally."""

    def __init__(self, pre: _Pre):
        self.pre = pre
        self.model = cp_model.CpModel()
        self.num_vars = 0
        self.num_constraints = 0

        self.x: dict[str, cp_model.IntVar] = {}
        self.y: dict[tuple[str, str], cp_model.IntVar] = {}
        self.a: dict[tuple[str, str, str], cp_model.IntVar] = {}
        self.b: dict[tuple[str, str, str], cp_model.IntVar] = {}
        self.cov_flag: dict[str, cp_model.IntVar] = {}
        self.slack: dict[str, cp_model.IntVar] = {}
        self.slack_penalty = 0
        self.objective_cleared = False

        self._build()

    # -- helpers ------------------------------------------------------------

    def _new_var(self) -> None:
        self.num_vars += 1

    def _new_con(self) -> None:
        self.num_constraints += 1

    # -- build --------------------------------------------------------------

    def _build(self) -> None:
        pre = self.pre
        model = self.model

        # ---- variables ----------------------------------------------------
        for emp in pre.emp_ids:
            if pre.forbid_external and pre.external[emp]:
                continue
            self.x[emp] = model.NewBoolVar(f"x_{emp}")
            self._new_var()

        for r in pre.requirements:
            rid = r.requirement_id
            self.cov_flag[rid] = model.NewBoolVar(f"cov_{rid}")
            self._new_var()
            lo, hi = pre.week_window(r)
            for emp in pre.emp_ids:
                if emp not in self.x:
                    continue
                if not pre.assignable(emp, r):
                    continue
                key = (emp, rid)
                self.y[key] = model.NewBoolVar(f"y_{emp}_{rid}")
                self._new_var()
                for w_idx in range(lo, hi + 1):
                    week = pre.weeks[w_idx]
                    cap = pre.cap[emp].get(week, 0)
                    if cap <= 0:
                        continue
                    akey = (emp, rid, week)
                    self.a[akey] = model.NewIntVar(0, cap, f"a_{emp}_{rid}_{w_idx}")
                    self._new_var()
                    if pre.min_assign_int > 0:
                        self.b[akey] = model.NewBoolVar(f"b_{emp}_{rid}_{w_idx}")
                        self._new_var()

        # ---- G2/G1 capacity (G1 via cap=0 on unavailable weeks) ------------
        for emp in pre.emp_ids:
            for w_idx, week in enumerate(pre.weeks):
                terms = [self.a[(emp, r.requirement_id, week)] for r in pre.requirements
                         if (emp, r.requirement_id, week) in self.a]
                if not terms:
                    continue
                cap = pre.cap[emp].get(week, 0)
                if cap > 0:
                    model.Add(sum(terms) <= cap)
                    self._new_con()

        # ---- member <-> assignment links ----------------------------------
        for (emp, rid), yv in self.y.items():
            r = pre.req[rid]
            lo, hi = pre.week_window(r)
            week_terms = [self.a[(emp, rid, pre.weeks[w_idx])] for w_idx in range(lo, hi + 1)
                          if (emp, rid, pre.weeks[w_idx]) in self.a]
            # y => x (a member must be on the roster)
            model.Add(yv <= self.x[emp])
            self._new_con()
            if not week_terms:
                model.Add(yv == 0)
                self._new_con()
                continue
            # y => at least 1 hour of work
            model.Add(sum(week_terms) >= yv)
            self._new_con()
            # a > 0 => y (no phantom assignments). One aggregate implication per
            # (employee, requirement) instead of one per week: with y = 1 the bound
            # eff_likely_h[r] is implied by the global effort cap below, so this is
            # *exactly* equivalent and removes ~#weeks constraints per pair (the
            # dominant cost of both model build and presolve at 500 x 30 scale).
            model.Add(sum(week_terms) <= pre.eff_likely_h[rid] * yv)
            self._new_con()

        # ---- x => at least one assignment (no phantom members) ------------
        for emp, xv in self.x.items():
            eligible_terms = [self.y[(emp, r.requirement_id)] for r in pre.requirements
                              if (emp, r.requirement_id) in self.y]
            if eligible_terms:
                model.Add(xv <= sum(eligible_terms))
                self._new_con()
            else:
                # candidate cannot be assigned to anything
                model.Add(xv == 0)
                self._new_con()

        # ---- effort caps (all requirements, at the likely point estimate) --
        for r in pre.requirements:
            rid = r.requirement_id
            lo, hi = pre.week_window(r)
            terms = [self.a[(emp, rid, pre.weeks[w_idx])] for emp in pre.emp_ids
                     for w_idx in range(lo, hi + 1) if (emp, rid, pre.weeks[w_idx]) in self.a]
            if terms:
                model.Add(sum(terms) <= pre.eff_likely_h[rid])
                self._new_con()

        # ---- G3: must minimum effort, relaxed through slack when allowed ---
        for r in pre.requirements:
            if r.criticality != "must":
                continue
            rid = r.requirement_id
            lo, hi = pre.week_window(r)
            terms = [self.a[(emp, rid, pre.weeks[w_idx])] for emp in pre.emp_ids
                     for w_idx in range(lo, hi + 1) if (emp, rid, pre.weeks[w_idx]) in self.a]
            target = pre.eff_min_h[rid]
            if not pre.allow_deficits:
                if not terms:
                    # no assignable candidate at all -> model infeasible; add a fake lower bound
                    model.Add(0 >= target)
                    self._new_con()
                    continue
                model.Add(sum(terms) >= target)
                self._new_con()
                continue
            # allow_deficits: uncovered must effort is an explicit, minimized slack
            if target <= 0:
                continue
            self.slack[rid] = model.NewIntVar(0, target, f"slack_{rid}")
            self._new_var()
            model.Add(sum(terms) + self.slack[rid] >= target)
            self._new_con()

        # ---- G4 team size -------------------------------------------------
        team_terms = list(self.x.values())
        if pre.max_team is not None:
            model.Add(sum(team_terms) <= pre.max_team)
            self._new_con()
        if pre.min_team is not None:
            model.Add(sum(team_terms) >= pre.min_team)
            self._new_con()

        # ---- G4 force include / freeze / exclude ---------------------------
        for emp in pre.force_include:
            if emp in self.x:
                model.Add(self.x[emp] == 1)
                self._new_con()
        for emp in pre.freeze:
            if emp in self.x:
                model.Add(self.x[emp] == 1)
                self._new_con()
        for emp in pre.force_exclude:
            if emp in self.x:
                model.Add(self.x[emp] == 0)
                self._new_con()

        # ---- G4 budget ----------------------------------------------------
        if pre.budget_int is not None:
            budget_terms = []
            for emp in pre.emp_ids:
                hrs_terms = [self.a[(emp, r.requirement_id, pre.weeks[w_idx])]
                             for r in pre.requirements for w_idx in range(pre.n_weeks)
                             if (emp, r.requirement_id, pre.weeks[w_idx]) in self.a]
                if hrs_terms:
                    budget_terms.append(pre.cost_int[emp] * sum(hrs_terms))
            if budget_terms:
                model.Add(sum(budget_terms) <= pre.budget_int)
                self._new_con()

        # ---- G4 min bus factor (>= min_bus qualified members per must) -----
        if pre.min_bus > 1:
            for r in pre.requirements:
                if r.criticality != "must":
                    continue
                qualified_x = [self.x[emp] for emp in pre.emp_ids
                               if emp in self.x and pre.assignable(emp, r)]
                model.Add(sum(qualified_x) >= pre.min_bus)
                self._new_con()

        # ---- G4 min assignment hours (anti-fragmentation, M10) ------------
        if pre.min_assign_int > 0:
            for akey, av in self.a.items():
                emp, _rid, week = akey
                cap = pre.cap[emp].get(week, 0)
                model.Add(av >= pre.min_assign_int * self.b[akey])
                self._new_con()
                model.Add(av <= cap * self.b[akey])
                self._new_con()

        # ---- coverage flag links ------------------------------------------
        for r in pre.requirements:
            rid = r.requirement_id
            y_terms = [self.y[(emp, rid)] for emp in pre.emp_ids if (emp, rid) in self.y]
            if y_terms:
                for yv in y_terms:
                    model.Add(self.cov_flag[rid] >= yv)
                    self._new_con()
                model.Add(self.cov_flag[rid] <= sum(y_terms))
                self._new_con()
            else:
                model.Add(self.cov_flag[rid] == 0)
                self._new_con()

        # ---- objective (linearized coverage proxy + slack penalty) --------
        obj_terms = []
        for r in pre.requirements:
            rid = r.requirement_id
            obj_terms.append(CRIT_COEF[r.criticality] * pre.wM1_int * COVER_BONUS * self.cov_flag[rid])
        for (emp, rid, _week), av in self.a.items():
            r = pre.req[rid]
            coef = CRIT_COEF[r.criticality] * pre.wM2_int * pre.fit_coef(emp, r) - pre.cost_int[emp]
            if coef != 0:
                obj_terms.append(coef * av)

        # objective normalization scale (deterministic upper bound, cost tiebreak ignored)
        obj_scale = 0.0
        for r in pre.requirements:
            obj_scale += CRIT_COEF[r.criticality] * pre.wM1_int * COVER_BONUS
            obj_scale += CRIT_COEF[r.criticality] * pre.wM2_int * 100 * pre.eff_likely_h[r.requirement_id]
        if obj_scale <= 0:
            obj_scale = 1.0
        self.obj_scale = obj_scale

        if self.slack:
            # Slack must dominate *every* quality term, including the cost tie-breaker,
            # so a coverable requirement is never reported as a deficit.
            cost_bound = float(sum(
                pre.cost_int[emp] * pre.cap[emp].get(week, 0)
                for (emp, _rid, week) in self.a
            ))
            self.slack_penalty = int(math.ceil(obj_scale + cost_bound)) + 1
            for rid, sv in self.slack.items():
                obj_terms.append(-self.slack_penalty * sv)

        if obj_terms:
            model.Maximize(sum(obj_terms))

    # -- diversity ----------------------------------------------------------

    def add_diversity_cut(self, roster: frozenset[str], min_distance: float) -> None:
        """Require Jaccard distance >= ``min_distance`` between the roster and ``roster``.

        With ``I = sum_{i in roster} x[i]`` and ``S = sum_i x[i]`` the Jaccard index is
        ``I / (|roster| + S - I)``; ``1 - J >= d`` is therefore the linear inequality
        ``I * (2 - d) <= (1 - d) * (|roster| + S)``. Scaled to integers, it is exact
        (no big-M, no auxiliary booleans).
        """
        if not roster:
            return
        scale = 10000
        d_q = int(round(min(1.0, min_distance + DIVERSITY_EPS) * scale))
        size = len(roster)
        inter = sum(self.x[e] for e in sorted(roster) if e in self.x)
        total = sum(self.x.values())
        self.model.Add(inter * (2 * scale - d_q) <= (scale - d_q) * (size + total))
        self._new_con()

    # -- solve --------------------------------------------------------------

    def solve_once(self, request: OptimizeRequest, time_limit_ms: int) -> tuple[int, int]:
        """Run CP-SAT once; returns ``(cp_status, wall_time_ms)``."""
        solver = cp_model.CpSolver()
        solver.parameters.num_search_workers = 1  # determinism (non-negotiable)
        solver.parameters.random_seed = request.seed
        solver.parameters.max_time_in_seconds = max(0.05, time_limit_ms / 1000.0)
        # Presolve is *disabled on purpose*: for this model family (many implication
        # constraints, up to 2000 x 60 candidates/requirements) CP-SAT's presolve consumes
        # the whole per-attempt budget without producing an incumbent. Measured at
        # 500 x 30 x 16 weeks (66840 vars / 24170 constraints), 2.5 s per attempt:
        # presolve on => UNKNOWN (no solution), presolve off => OPTIMAL in 1.3 s.
        # Infeasibility is still detected immediately (the violating constraints are
        # propagated at the root), and every synth scenario solves OPTIMAL and faster.
        solver.parameters.cp_model_presolve = False
        solver.parameters.log_search_progress = bool(request.options.log_search_progress)
        cp_status = solver.Solve(self.model)
        self._last_solver = solver
        return cp_status, int(solver.WallTime() * 1000)

    def solve_feasibility(self, request: OptimizeRequest, time_limit_ms: int) -> tuple[int, int]:
        """Degradation path: drop the objective and just find *any* feasible plan.

        Used only when the first objective-guided attempt returned no incumbent at all.
        Deterministic (status-driven, never clock-driven): the remaining attempts' budget is
        spent on one feasibility solve so the caller still receives the best found team
        instead of nothing ("graceful timeout").
        """
        self.model.ClearObjective()
        self.objective_cleared = True
        return self.solve_once(request, time_limit_ms)

    def extract(self, cp_status: int, attempt: int, degraded: bool = False) -> VariantOutcome:
        """Turn the current solution into a ``VariantOutcome`` (deficits, metrics, ...).

        ``degraded`` marks a plan obtained from the objective-free fallback solve: it is a
        valid feasible team, but no objective value / bound / gap can be claimed for it, so
        those fields are reported as ``None`` (honest, never invented).
        """
        pre = self.pre
        solver = self._last_solver

        members = sorted(emp for emp, xv in self.x.items() if solver.Value(xv) == 1)
        assignments: list[Assignment] = []
        allocated: dict[str, float] = {}
        assigned_hours_by_req: dict[str, int] = {}
        for (emp, rid, week), av in self.a.items():
            h = solver.Value(av)
            if h > 0:
                assignments.append((emp, rid, week, float(h)))
                allocated[emp] = allocated.get(emp, 0.0) + float(h)
                assigned_hours_by_req[rid] = assigned_hours_by_req.get(rid, 0) + h
        # canonical ordering for byte-identical output
        assignments.sort(key=lambda t: (pre.week_idx[t[2]], t[1], t[0]))

        slack_h: dict[str, int] = {rid: solver.Value(sv) for rid, sv in self.slack.items()}
        total_slack = sum(slack_h.values())
        cost = sum(allocated.get(emp, 0.0) * pre.cost[emp] for emp in members)

        metrics = compute_advisory_metrics(pre.request, set(members), assignments)

        deficits = _build_deficits(pre, slack_h, cost)
        unscheduled = sorted(
            rid for rid in pre.req_ids if assigned_hours_by_req.get(rid, 0) == 0
        )

        obj_raw = solver.ObjectiveValue() if cp_status in (cp_model.OPTIMAL, cp_model.FEASIBLE) else 0.0
        quality_raw = obj_raw + self.slack_penalty * total_slack
        obj_norm = _clamp01(quality_raw / self.obj_scale)
        best_bound: Optional[float] = None
        gap: Optional[float] = None
        if degraded:
            obj_norm = None
            best_bound = None
            gap = None
        # Bounds are only comparable to the reported value when no slack penalty is active.
        elif total_slack == 0:
            if cp_status == cp_model.OPTIMAL:
                best_bound = obj_norm
                gap = 0.0
            else:
                try:
                    bound_raw = solver.BestObjectiveBound()
                    if bound_raw is not None and bound_raw > 0:
                        best_bound = _clamp01(bound_raw / self.obj_scale)
                        if best_bound > 0:
                            gap = _clamp01(max(0.0, (best_bound - obj_norm) / best_bound))
                except Exception:
                    best_bound = None
                    gap = None

        return VariantOutcome(
            status="FEASIBLE" if degraded else _STATUS_MAP.get(cp_status, "UNKNOWN"),
            members=members,
            assignments=assignments,
            allocated_hours=allocated,
            metrics=metrics,
            deficits=deficits,
            unscheduled_requirements=unscheduled,
            objective_value=round(obj_norm, 6) if obj_norm is not None else None,
            best_bound=round(best_bound, 6) if best_bound is not None else None,
            optimality_gap=round(gap, 6) if gap is not None else None,
            total_slack_hours=total_slack,
            attempt=attempt,
            cost_units=round(cost, 6),
        )


def _clamp01(x: float) -> float:
    return max(0.0, min(1.0, x))


# ---------------------------------------------------------------------------
# Deficits (slack -> contract MlDeficit, with the recommended action)
# ---------------------------------------------------------------------------


def _build_deficits(pre: _Pre, slack_h: dict[str, int], cost: float) -> list[MlDeficit]:
    """Group the ``must`` slack into ``MlDeficit`` entries with accurate reason codes.

    ``person_days`` is the uncovered *minimum* effort (slack / 8) when some qualified
    person is assignable; when *nobody* is assignable the whole requirement is external and
    the full ``likely`` effort is reported instead (that is the effort at risk).

    Reason codes follow the recommended action from METRICS.md "Level 2":
    ``DEFICIT_EXTERNAL_CONTRACTOR`` (nobody holds the competency),
    ``DEFICIT_UPSKILL_INTERNAL`` (people exist below the required level),
    ``DEFICIT_DEADLINE_SHIFT`` (qualified people exist but capacity/window is short),
    plus ``DEFICIT_DESCOPE`` when the budget cap is binding.
    """
    budget_binding = (
        pre.budget is not None and pre.budget > 0 and cost >= 0.98 * pre.budget
    )

    grouped: dict[tuple[str, str], dict] = {}
    for rid in sorted(slack_h):
        short_h = slack_h[rid]
        if short_h <= 0:
            continue
        r = pre.req[rid]
        assignable = pre.assignable_candidates(r)
        if not assignable:
            person_days = max(0.25, round(pre.likely_pd[rid], 2))
            action = (
                "DEFICIT_EXTERNAL_CONTRACTOR"
                if not pre.holding_competency(r)
                else "DEFICIT_UPSKILL_INTERNAL"
                if pre.max_level(r) < r.level
                else "DEFICIT_DEADLINE_SHIFT"
            )
        else:
            person_days = max(0.25, round(short_h / HOURS_PER_DAY, 2))
            lo, hi = pre.week_window(r)
            if hi < lo:
                action = "DEFICIT_DEADLINE_SHIFT"
            elif not pre.holding_competency(r) or pre.max_level(r) < r.level:
                action = (
                    "DEFICIT_EXTERNAL_CONTRACTOR"
                    if not pre.holding_competency(r)
                    else "DEFICIT_UPSKILL_INTERNAL"
                )
            else:
                action = "DEFICIT_DEADLINE_SHIFT"

        key = (r.competency_id or "", r.role_id)
        slot = grouped.setdefault(
            key,
            {"competency_id": r.competency_id, "role_id": r.role_id,
             "requirement_ids": [], "person_days": 0.0, "actions": set()},
        )
        slot["requirement_ids"].append(rid)
        slot["person_days"] += person_days
        slot["actions"].add(action)

    deficits: list[MlDeficit] = []
    for key in sorted(grouped):
        slot = grouped[key]
        codes = ["DEFICIT_MUST_ROLE"]
        for action in (
            "DEFICIT_EXTERNAL_CONTRACTOR",
            "DEFICIT_UPSKILL_INTERNAL",
            "DEFICIT_DEADLINE_SHIFT",
        ):
            if action in slot["actions"]:
                codes.append(action)
        if budget_binding:
            codes.append("DEFICIT_DESCOPE")
        deficits.append(
            MlDeficit(
                competency_id=slot["competency_id"],
                role_id=slot["role_id"],
                requirement_ids=sorted(slot["requirement_ids"]),
                person_days=round(slot["person_days"], 2),
                external_cooperation_flag=True,  # METRICS Level 2: deficit_must > 0 => true
                reason_codes=codes,
            )
        )
    return deficits


# ---------------------------------------------------------------------------
# Variant-set selection (diversity + non-domination)
# ---------------------------------------------------------------------------


def jaccard_distance(a: frozenset[str], b: frozenset[str]) -> float:
    """``1 - |A n B| / |A u B|``; two empty rosters are defined as distance 0."""
    union = a | b
    if not union:
        return 0.0
    return 1.0 - len(a & b) / len(union)


def select_variants(
    candidates: list[VariantOutcome], k: int, min_jaccard: float
) -> tuple[list[VariantOutcome], int]:
    """Pick up to ``k`` diverse, non-dominated variants (deterministic).

    Candidates are visited in descending advisory score (tie-broken by the sorted roster and
    the attempt index). A candidate is kept only when it is diverse from every already-kept
    roster *and* not Pareto-dominated on M1..M12 by a kept variant; a dominated or
    non-diverse candidate is dropped and the next one takes its place.

    Returns ``(kept, dropped)`` where ``dropped`` counts variants the caller asked for or
    that were found but could not be returned.
    """
    ordered = sorted(
        candidates,
        key=lambda v: (-v.score, tuple(v.members), v.attempt),
    )
    kept: list[VariantOutcome] = []
    for v in ordered:
        if len(kept) >= k:
            break
        roster = v.roster
        if any(jaccard_distance(roster, k2.roster) < min_jaccard - 1e-9 for k2 in kept):
            continue
        if any(dominates(k2.metrics, v.metrics) for k2 in kept):
            continue
        kept.append(v)
    dropped = max(k, len(candidates)) - len(kept)
    return kept, max(0, dropped)


# ---------------------------------------------------------------------------
# Public entry point
# ---------------------------------------------------------------------------


def solve(request: OptimizeRequest) -> SolveOutcome:
    """Build and solve the CP-SAT model; returns a fully-populated ``SolveOutcome``."""
    pre = _Pre(request)
    _validate_request(pre)

    built = _Model(pre)
    num_vars = built.num_vars
    num_constraints = built.num_constraints

    # Deterministic budget split: up to k+1 attempts (the base solve + one re-solve per
    # requested extra variant + one spare for a dominated roster). No wall-clock decision
    # is ever taken, so the same request always issues the same number of solves. The
    # searchable share of the request budget (``1 - BUILD_RESERVE_SHARE``) is divided
    # evenly, so build + search + assembly stays inside ``time_limit_ms``.
    max_attempts = pre.k + 1
    searchable_ms = int(request.time_limit_ms * (1.0 - BUILD_RESERVE_SHARE))
    per_attempt_ms = max(20, int(searchable_ms / max_attempts))

    candidates: list[VariantOutcome] = []
    total_wall_ms = 0
    timed_out = False
    infeasible = False
    degraded = False

    for attempt in range(max_attempts):
        cp_status, wall_ms = built.solve_once(request, per_attempt_ms)
        total_wall_ms += wall_ms

        if cp_status not in (cp_model.OPTIMAL, cp_model.FEASIBLE):
            if cp_status == cp_model.INFEASIBLE:
                infeasible = True
                break
            if attempt == 0 and not built.objective_cleared:
                # No incumbent at all after the whole first attempt: spend the remaining
                # attempts' search budget on one objective-free feasibility solve, so the
                # caller gets the best found team plus a truthful status instead of nothing.
                remaining_ms = per_attempt_ms * (max_attempts - 1)
                fb_status, fb_wall_ms = built.solve_feasibility(request, remaining_ms)
                total_wall_ms += fb_wall_ms
                if fb_status in (cp_model.OPTIMAL, cp_model.FEASIBLE):
                    variant = built.extract(fb_status, attempt, degraded=True)
                    if variant.members:
                        degraded = True
                        candidates.append(variant)
                        break
            timed_out = True
            break

        variant = built.extract(cp_status, attempt)
        if not variant.members:
            # An empty roster is not a team. On the first attempt it means the request (or
            # a G4 constraint) leaves nothing to staff -> INFEASIBLE; on later attempts the
            # diversity cut simply has no non-empty roster left -> stop, never duplicate.
            if attempt == 0:
                infeasible = True
            break
        candidates.append(variant)
        built.add_diversity_cut(variant.roster, pre.min_jaccard)

    if not candidates:
        return _no_variant_outcome(
            pre, request, total_wall_ms, timed_out, infeasible, num_vars, num_constraints,
            max_attempts,
        )

    kept, dropped = select_variants(candidates, pre.k, pre.min_jaccard)
    if not kept:  # pragma: no cover - select_variants always keeps the first candidate
        kept = candidates[:1]
        dropped = max(0, max(pre.k, len(candidates)) - 1)

    primary = kept[0]

    return SolveOutcome(
        status=primary.status,
        wall_time_ms=total_wall_ms,
        num_workers=1,
        num_vars=num_vars,
        num_constraints=num_constraints,
        presolve_time_ms=0,
        variants=kept,
        infeasibility=None,
        reason_codes=["SOLVER_TIMEOUT"] if degraded else [],
        dropped_diversity_variants=dropped,
    )


def _no_variant_outcome(
    pre: _Pre,
    request: OptimizeRequest,
    wall_time_ms: int,
    timed_out: bool,
    infeasible: bool,
    num_vars: int,
    num_constraints: int,
    max_attempts: int,
) -> SolveOutcome:
    """Build the INFEASIBLE / TIMEOUT outcome (never a partial or relaxed plan)."""
    if timed_out and not infeasible:
        report = InfeasibilityReport(
            blocking_constraints=[
                ActiveConstraintRef(
                    id="c_timeout",
                    type="capacity",
                    value=str(request.time_limit_ms),
                    binding=True,
                    reason_codes=["SOLVER_TIMEOUT"],
                )
            ],
            reason_codes=["SOLVER_TIMEOUT"],
        )
        return SolveOutcome(
            status="TIMEOUT",
            wall_time_ms=wall_time_ms,
            num_workers=1,
            num_vars=num_vars,
            num_constraints=num_constraints,
            presolve_time_ms=0,
            variants=[],
            infeasibility=report,
            reason_codes=["SOLVER_TIMEOUT"],
            dropped_diversity_variants=pre.k,
        )

    report = _analyze_infeasibility(pre)
    return SolveOutcome(
        status="INFEASIBLE",
        wall_time_ms=wall_time_ms,
        num_workers=1,
        num_vars=num_vars,
        num_constraints=num_constraints,
        presolve_time_ms=0,
        variants=[],
        infeasibility=report,
        reason_codes=["SOLVER_INFEASIBLE"],
        dropped_diversity_variants=pre.k,
    )


# ---------------------------------------------------------------------------
# Infeasibility diagnosis (names the shortage, never just "no solution")
# ---------------------------------------------------------------------------


def _analyze_infeasibility(pre: _Pre) -> InfeasibilityReport:
    """Name blocking constraints + the deficit for every unmet must requirement."""
    blocking: list[ActiveConstraintRef] = []
    deficits: list[MlDeficit] = []
    unscheduled: list[str] = []

    # 1) Uncoverable must requirements (G3 shortage)
    for r in pre.requirements:
        if r.criticality != "must":
            continue
        rid = r.requirement_id
        lo, hi = pre.week_window(r)
        qualified_emps = [emp for emp in pre.emp_ids if pre.qualified[(emp, rid)]]
        if pre.forbid_external:
            qualified_emps = [e for e in qualified_emps if not pre.external[e]]
        if not r.allows_external:
            qualified_emps = [e for e in qualified_emps if not pre.external[e]]

        available_hours = 0
        for emp in qualified_emps:
            for w_idx in range(lo, hi + 1):
                available_hours += pre.cap[emp].get(pre.weeks[w_idx], 0)

        # window may be empty (deadline before earliest, etc.)
        window_empty = hi < lo

        shortage = not qualified_emps or available_hours < pre.eff_min_h[rid] or window_empty
        bus_shortage = len(qualified_emps) < pre.min_bus

        if shortage or bus_shortage:
            unscheduled.append(rid)
            person_days = max(0.5, round(pre.eff_min_h[rid] / HOURS_PER_DAY, 2))
            deficits.append(
                MlDeficit(
                    competency_id=r.competency_id,
                    role_id=r.role_id,
                    requirement_ids=[rid],
                    person_days=person_days,
                    external_cooperation_flag=True,
                    reason_codes=["DEFICIT_MUST_ROLE", "DEFICIT_EXTERNAL_CONTRACTOR"],
                )
            )
            if shortage:
                blocking.append(
                    ActiveConstraintRef(
                        id=f"c_must_deficit_{rid}",
                        type="capacity",
                        value=r.competency_id if r.competency_id else r.role_id,
                        binding=True,
                        reason_codes=["CONSTRAINT_BINDING_CAPACITY"],
                    )
                )
            if bus_shortage:
                blocking.append(
                    ActiveConstraintRef(
                        id=f"c_min_bus_factor_{rid}",
                        type="min_bus_factor",
                        value=str(pre.min_bus),
                        binding=True,
                        reason_codes=["CONSTRAINT_ACTIVE_BUS_FACTOR"],
                    )
                )

    # 2) budget too low for the must minimum effort
    if pre.budget_int is not None and not deficits:
        min_cost = 0
        for r in pre.requirements:
            if r.criticality != "must":
                continue
            rid = r.requirement_id
            quals = [
                pre.cost_int[e]
                for e in pre.emp_ids
                if pre.qualified[(e, rid)]
                and not (pre.forbid_external and pre.external[e])
                and (r.allows_external or not pre.external[e])
            ]
            min_coeff = min(quals) if quals else 100
            min_cost += pre.eff_min_h[rid] * min_coeff
        if min_cost > pre.budget_int:
            blocking.append(
                ActiveConstraintRef(
                    id="c_budget_cap",
                    type="budget_cap",
                    value=str(pre.budget),
                    binding=True,
                    reason_codes=["CONSTRAINT_ACTIVE_BUDGET"],
                )
            )

    # 3) min team size larger than assignable candidates
    assignable = sum(
        1 for e in pre.emp_ids
        if not (pre.forbid_external and pre.external[e])
        and any(pre.assignable(e, r) for r in pre.requirements)
    )
    if pre.min_team is not None and pre.min_team > assignable:
        blocking.append(
            ActiveConstraintRef(
                id="c_min_team_size",
                type="min_team_size",
                value=str(pre.min_team),
                binding=True,
                reason_codes=["CONSTRAINT_ACTIVE_TEAM_SIZE"],
            )
        )

    # 4) force-included candidate with no assignable work
    for emp in pre.force_include + pre.freeze:
        if emp not in pre.cand:
            continue
        if not any(pre.assignable(emp, r) for r in pre.requirements):
            blocking.append(
                ActiveConstraintRef(
                    id=f"c_force_include_{emp}",
                    type="force_include",
                    value=emp,
                    binding=True,
                    reason_codes=["CONSTRAINT_ACTIVE_FORCE_INCLUDE"],
                )
            )

    # 5) a force_exclude that removes the only qualified people for a must requirement
    for emp in sorted(pre.force_exclude):
        if emp not in pre.cand:
            continue
        blocking.append(
            ActiveConstraintRef(
                id=f"c_force_exclude_{emp}",
                type="force_exclude",
                value=emp,
                binding=False,
                reason_codes=["CONSTRAINT_ACTIVE_FORCE_EXCLUDE", "EXCLUDE_USER_FORCE"],
            )
        )
        if len(blocking) > 60:  # pragma: no cover - defensive payload cap
            break

    # guarantee at least one blocking constraint (schema requires the array)
    if not blocking:
        blocking.append(
            ActiveConstraintRef(
                id="c_infeasible",
                type="capacity",
                binding=True,
                reason_codes=["SOLVER_INFEASIBLE"],
            )
        )

    # de-duplicate blocking constraints deterministically
    seen: set[tuple[str, str, Optional[str]]] = set()
    unique_blocking: list[ActiveConstraintRef] = []
    for b in blocking:
        key = (b.type, b.id, b.value)
        if key not in seen:
            seen.add(key)
            unique_blocking.append(b)

    return InfeasibilityReport(
        blocking_constraints=unique_blocking,
        deficits=deficits,
        unscheduled_requirements=sorted(set(unscheduled)),
        reason_codes=["SOLVER_INFEASIBLE"],
    )
