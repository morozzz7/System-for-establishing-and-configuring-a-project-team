"""Shared helpers for optimizer tests: build OptimizeRequest from hand fixtures or
from the read-only ``eval/synth/scenarios`` generator output.

The synthetic generator does *not* produce a capacity matrix (that is Go's job,
ADR-003). Tests build a **simple deterministic candidate capacity approximation**:

- ``norm = weekly_hours_norm`` (default 40), ``reserve = reserve_ratio`` (default 0.2),
  ``max_util = max_utilization_ratio`` (default 0.9).
- ``effective_limit = min(norm*(1-reserve), norm*max_util)``.
- ``assigned_hours[emp, week] = sum(hours_minutes/60)`` over ``assignments.json``.
- A week is ``unavailable`` when any *approved* absence overlaps it (Monday..Sunday);
  otherwise ``free_hours = max(0, effective_limit - assigned_hours)``.

This approximation is deterministic and conservative (a partial-week absence blanks the
whole week). The authoritative capacity comes from the Go ``weekly_capacity`` service;
the optimizer just respects whatever matrix it is given.
"""

from __future__ import annotations

import json
import math
from collections import deque
from datetime import date, timedelta
from pathlib import Path

from app.extract.schemas import EffortRange
from app.optimize.schemas import (
    CandidateCapacityWeek,
    CandidateCompetency,
    OptimizeCandidate,
    OptimizeConstraints,
    OptimizeHorizon,
    OptimizeOptions,
    OptimizeRequest,
    OptimizeRequirement,
    OptimizeWeights,
)

REPO_ROOT = Path(__file__).resolve().parents[2]
SYNTH_DIR = REPO_ROOT / "eval" / "synth" / "scenarios"

# balanced preset from contracts/reference.yaml (sums to 1.0)
BALANCED_WEIGHTS = dict(
    M1=0.15, M2=0.13, M3=0.10, M4=0.08, M5=0.15, M6=0.10,
    M7=0.08, M8=0.08, M9=0.05, M10=0.04, M11=0.02, M12=0.02,
)


def make_weights(**overrides) -> OptimizeWeights:
    w = dict(BALANCED_WEIGHTS)
    w.update(overrides)
    return OptimizeWeights(**w)


def make_options(allow_deficits: bool = True, preset: str = "balanced") -> OptimizeOptions:
    """Solver options; ``allow_deficits`` selects v1 (hard G3) vs v2 (slack) behaviour."""
    return OptimizeOptions(allow_deficits=allow_deficits, objective_preset=preset)


def make_horizon(start_week: str = "2026-W02", n_weeks: int = 8) -> OptimizeHorizon:
    weeks = _week_list(start_week, n_weeks)
    return OptimizeHorizon(start_week=weeks[0], end_week=weeks[-1], weeks=weeks)


def _week_list(start_week: str, n: int) -> list[str]:
    y, w = int(start_week[:4]), int(start_week[-2:])
    out = []
    for i in range(n):
        wk = w + i
        yy = y
        while wk > 52:
            wk -= 52
            yy += 1
        out.append(f"{yy}-W{wk:02d}")
    return out


def make_candidate(
    employee_id: str,
    role: str = "backend_engineer",
    competencies: dict[str, int] | None = None,
    free_hours: float = 40.0,
    grade: str = "S",
    cost_coefficient: float = 1.9,
    bench: bool = False,
    employment_type: str = "internal",
    start_week: str = "2026-W02",
    n_weeks: int = 8,
    unavailable_weeks: list[str] | None = None,
) -> OptimizeCandidate:
    weeks = _week_list(start_week, n_weeks)
    unav = set(unavailable_weeks or [])
    cap = [
        CandidateCapacityWeek(
            week=w, working_days=5, free_hours=free_hours, unavailable=(w in unav),
        )
        for w in weeks
    ]
    comps = [
        CandidateCompetency(competency_id=cid, level=lvl, confidence=0.9)
        for cid, lvl in (competencies or {}).items()
    ]
    return OptimizeCandidate(
        employee_id=employee_id,
        grade=grade,
        role_profile_id=role,
        employment_type=employment_type,
        bench_flag=bench,
        cost_coefficient=cost_coefficient,
        competencies=comps,
        capacity=cap,
    )


def make_requirement(
    requirement_id: str,
    role: str = "backend_engineer",
    competency: str | None = "lang_go",
    level: int = 3,
    criticality: str = "must",
    headcount: int = 1,
    min_pd: float = 10,
    likely_pd: float = 20,
    max_pd: float = 30,
) -> OptimizeRequirement:
    return OptimizeRequirement(
        requirement_id=requirement_id,
        role_id=role,
        competency_id=competency,
        level=level,
        criticality=criticality,
        headcount=headcount,
        effort=EffortRange(
            min_person_days=min_pd, likely_person_days=likely_pd,
            max_person_days=max_pd, unit="person_days",
        ),
    )


def make_request(
    request_id: str = "req_test00000001",
    seed: int = 42,
    time_limit_ms: int = 10000,
    requirements: list[OptimizeRequirement] | None = None,
    candidates: list[OptimizeCandidate] | None = None,
    constraints: OptimizeConstraints | None = None,
    horizon: OptimizeHorizon | None = None,
    weights: OptimizeWeights | None = None,
    k: int = 3,
    min_jaccard_diversity: float = 0.25,
    options: OptimizeOptions | None = None,
) -> OptimizeRequest:
    return OptimizeRequest(
        request_id=request_id,
        run_id="run_test",
        seed=seed,
        time_limit_ms=time_limit_ms,
        k=k,
        min_jaccard_diversity=min_jaccard_diversity,
        horizon=horizon or make_horizon(),
        weights=weights or make_weights(),
        requirements=requirements or [make_requirement("req_0001")],
        candidates=candidates or [make_candidate("emp_abcd", competencies={"lang_go": 4})],
        constraints=constraints or OptimizeConstraints(max_utilization_ratio=0.9),
        options=options or OptimizeOptions(),
    )


# ---------------------------------------------------------------------------
# Synth scenario -> OptimizeRequest (read-only)
# ---------------------------------------------------------------------------


def _load_scenario(scenario_id: str) -> dict:
    d = SYNTH_DIR / scenario_id
    if not d.is_dir():
        raise FileNotFoundError(
            f"scenario {scenario_id} not found; run `python eval/synth/generator.py --seed 42`"
        )
    return {f.stem: json.loads((d / f.name).read_text()) for f in d.iterdir() if f.suffix == ".json"}


def _week_bounds(week: str) -> tuple[date, date]:
    y, w = int(week[:4]), int(week[-2:])
    jan4 = date(y, 1, 4)
    monday = jan4 - timedelta(days=jan4.isoweekday() - 1) + timedelta(weeks=w - 1)
    return monday, monday + timedelta(days=6)


def build_request_from_scenario(
    scenario_id: str,
    seed: int = 42,
    time_limit_ms: int = 10000,
    horizon_weeks: int = 16,
    allow_deficits: bool = True,
    k: int = 3,
    min_jaccard_diversity: float = 0.25,
) -> OptimizeRequest:
    """Build an OptimizeRequest from a synth scenario directory (read-only)."""
    data = _load_scenario(scenario_id)

    comps: dict[str, dict[str, int]] = {}
    for c in data["competencies"]:
        comps.setdefault(c["employee_id"], {})[c["competency_id"]] = c["level"]

    assigned: dict[tuple[str, str], float] = {}
    for a in data["assignments"]:
        key = (a["employee_id"], a["week"])
        assigned[key] = assigned.get(key, 0.0) + a["hours_minutes"] / 60.0

    absences: dict[str, list[tuple[str, str]]] = {}
    for a in data["absences"]:
        if a.get("approved", True):
            absences.setdefault(a["employee_id"], []).append((a["start_date"], a["end_date"]))

    start_week = data["briefs"][0].get("requested_start_week", "2027-W06")
    weeks = _week_list(start_week, horizon_weeks)
    week_bounds = {w: _week_bounds(w) for w in weeks}

    candidates: list[OptimizeCandidate] = []
    for e in data["employees"]:
        if not e.get("active", True):
            continue
        norm = e.get("weekly_hours_norm", 40)
        reserve = e.get("reserve_ratio", 0.2)
        max_util = e.get("max_utilization_ratio", 0.9)
        eff_limit = min(norm * (1 - reserve), norm * max_util)
        eid = e["employee_id"]
        cap = []
        for w in weeks:
            unav = False
            monday, sunday = week_bounds[w]
            for s, en in absences.get(eid, []):
                sd = date.fromisoformat(s)
                ed = date.fromisoformat(en)
                if sd <= sunday and ed >= monday:
                    unav = True
                    break
            free = 0.0 if unav else max(0.0, eff_limit - assigned.get((eid, w), 0.0))
            cap.append(
                CandidateCapacityWeek(week=w, working_days=5, free_hours=round(free, 1), unavailable=unav)
            )
        candidates.append(
            OptimizeCandidate(
                employee_id=eid,
                grade=e["grade"],
                role_profile_id=e["role_profile_id"],
                employment_type=e.get("employment_type", "internal"),
                bench_flag=e.get("bench_flag", False),
                cost_coefficient=e.get("cost_coefficient", 1.0),
                competencies=[
                    CandidateCompetency(competency_id=k, level=v, confidence=0.8)
                    for k, v in comps.get(eid, {}).items()
                ],
                capacity=cap,
            )
        )

    requirements = [
        OptimizeRequirement(
            requirement_id=r["requirement_id"],
            role_id=r["role_id"],
            competency_id=r["competency_id"],
            level=r["level"],
            criticality=r["criticality"],
            headcount=r["headcount"],
            effort=EffortRange(
                min_person_days=r["effort_min"],
                likely_person_days=r["effort_likely"],
                max_person_days=r["effort_max"],
                unit="person_days",
            ),
        )
        for r in data["requirements"]
    ]

    return OptimizeRequest(
        request_id="req_accept0001",
        run_id=f"run_{scenario_id}",
        seed=seed,
        time_limit_ms=time_limit_ms,
        k=k,
        min_jaccard_diversity=min_jaccard_diversity,
        horizon=OptimizeHorizon(start_week=weeks[0], end_week=weeks[-1], weeks=weeks),
        weights=make_weights(),
        requirements=requirements,
        candidates=candidates,
        constraints=OptimizeConstraints(max_utilization_ratio=0.9),
        options=OptimizeOptions(allow_deficits=allow_deficits),
    )


def scenario_ground_truth(scenario_id: str) -> dict:
    data = _load_scenario(scenario_id)
    return data["ground_truth"]


class _Dinic:
    """Small deterministic max-flow implementation used only by the test oracle."""

    def __init__(self, n: int) -> None:
        self.graph: list[list[list[int]]] = [[] for _ in range(n)]

    def add_edge(self, u: int, v: int, cap: int) -> list[int]:
        forward = [v, len(self.graph[v]), cap]
        backward = [u, len(self.graph[u]), 0]
        self.graph[u].append(forward)
        self.graph[v].append(backward)
        return backward

    def max_flow(self, source: int, sink: int) -> int:
        total = 0
        n = len(self.graph)
        while True:
            level = [-1] * n
            level[source] = 0
            queue = deque([source])
            while queue:
                u = queue.popleft()
                for v, _rev, cap in self.graph[u]:
                    if cap > 0 and level[v] < 0:
                        level[v] = level[u] + 1
                        queue.append(v)
            if level[sink] < 0:
                return total
            cursor = [0] * n

            def dfs(u: int, pushed: int) -> int:
                if u == sink:
                    return pushed
                while cursor[u] < len(self.graph[u]):
                    edge = self.graph[u][cursor[u]]
                    v, rev, cap = edge
                    if cap > 0 and level[v] == level[u] + 1:
                        sent = dfs(v, min(pushed, cap))
                        if sent:
                            edge[2] -= sent
                            self.graph[v][rev][2] += sent
                            return sent
                    cursor[u] += 1
                return 0

            while pushed := dfs(source, 10**9):
                total += pushed


def _allocate_fairly(
    slot_cap: list[int],
    pending: list[tuple[object, int, list[int]]],
    priority_rid: str | None = None,
) -> dict[str, int]:
    """Allocate shared slot capacity with max-min fairness across must requirements."""
    allocated = {req.requirement_id: 0 for req, _demand, _edges in pending}
    used = [0] * len(slot_cap)

    def fill(active: list[tuple[object, int, list[int]]], fraction: float | None):
        source = len(slot_cap) + len(active)
        sink = source + 1
        net = _Dinic(sink + 1)
        slot_uses: list[tuple[int, list[int]]] = []
        for si, cap in enumerate(slot_cap):
            remaining = cap - used[si]
            if remaining > 0:
                net.add_edge(source, si, remaining)
        req_sinks: list[list[int]] = []
        targets: list[int] = []
        for ri, (req, demand, edges) in enumerate(active):
            if fraction is None:
                target = demand
            elif fraction > 0:
                target = int(math.ceil(fraction * demand - 1e-9))
            else:
                target = 0
            targets.append(target)
            node = len(slot_cap) + ri
            req_sinks.append(net.add_edge(node, sink, target))
            for si in edges:
                if slot_cap[si] - used[si] > 0:
                    slot_uses.append((si, net.add_edge(si, node, 10**9)))
        return net.max_flow(source, sink), sum(targets), req_sinks, slot_uses

    active = list(pending)
    if priority_rid is not None:
        first = [item for item in active if item[0].requirement_id == priority_rid]
        if first:
            _sent, _needed, sinks, uses = fill(first, None)
            for si, reverse in uses:
                used[si] += reverse[2]
            for (req, _demand, _edges), reverse in zip(first, sinks):
                allocated[req.requirement_id] += reverse[2]
            active = [item for item in active if item[0].requirement_id != priority_rid]

    while active:
        low, high = 0.0, 1.0
        for _ in range(40):
            mid = (low + high) / 2
            sent, needed, _sinks, _uses = fill(active, mid)
            if sent >= needed:
                low = mid
            else:
                high = mid
        _sent, _needed, sinks, uses = fill(active, low)
        for si, reverse in uses:
            used[si] += reverse[2]

        still_pending = []
        for (req, demand, edges), reverse in zip(active, sinks):
            got = reverse[2]
            allocated[req.requirement_id] += got
            target = int(math.ceil(low * demand - 1e-9)) if low > 0 else 0
            remaining_capacity = sum(max(0, slot_cap[si] - used[si]) for si in edges)
            if got + 1 < target or remaining_capacity == 0:
                continue
            if demand - got > 0:
                still_pending.append((req, demand - got, edges))
        if len(still_pending) == len(active):
            break
        active = still_pending
    return allocated


def shared_capacity_deficits(request: OptimizeRequest) -> dict[str, float]:
    """Reference deficits (likely person-days) derived from the scenario's own data.

    Qualified employees are a *shared* pool: hours one must requirement takes are
    unavailable to every other must requirement. Summing each competency's capacity
    in isolation invents capacity and hides genuine shortages, so the oracle allocates
    the scenario's capacity matrix with max-min fairness, applies the solver's
    min-effort gate, and sizes shortfalls on the likely basis (ADR-007 §1).
    """
    weeks = request.horizon.weeks
    week_idx = {w: i for i, w in enumerate(weeks)}
    deadline_idx = (
        week_idx.get(request.constraints.deadline_week)
        if request.constraints.deadline_week
        else None
    )

    slots: list[tuple[object, int, dict[str, int], bool]] = []
    slot_cap: list[int] = []
    for candidate in request.candidates:
        external = candidate.employment_type in ("contractor", "external_partner")
        for cw in candidate.capacity:
            idx = week_idx.get(cw.week)
            if idx is None or cw.unavailable:
                continue
            cap = int(math.floor(cw.free_hours + 1e-9))
            if cap <= 0:
                continue
            comps = {cc.competency_id: cc.level for cc in candidate.competencies}
            slots.append((candidate, idx, comps, external))
            slot_cap.append(cap)

    must_reqs = [r for r in request.requirements if r.criticality == "must"]
    pending: list[tuple[object, int, list[int]]] = []
    demand_by_id: dict[str, int] = {}
    for req in must_reqs:
        demand = int(math.ceil(req.effort.min_person_days * 8 - 1e-9))
        demand_by_id[req.requirement_id] = demand
        lo = week_idx.get(req.earliest_week, 0) if req.earliest_week else 0
        hi = (
            week_idx.get(req.latest_week, len(weeks) - 1)
            if req.latest_week
            else len(weeks) - 1
        )
        if deadline_idx is not None:
            hi = min(hi, deadline_idx)
        edges: list[int] = []
        for si, (candidate, week, comps, external) in enumerate(slots):
            if week < lo or week > hi:
                continue
            if request.constraints.forbid_external and external:
                continue
            if not req.allows_external and external:
                continue
            if req.competency_id is None:
                qualified = candidate.role_profile_id == req.role_id
            else:
                qualified = comps.get(req.competency_id, 0) >= req.level
            if qualified:
                edges.append(si)
        if demand > 0 and edges:
            pending.append((req, demand, edges))

    # Which shared requirement is served first is arbitrary across the solver's
    # diverse variants, so average the shortfall over deterministic priority orders
    # in which the requirement still misses the min-effort gate.
    samples: dict[str, list[float]] = {}
    priorities: list[str | None] = [None] + [req.requirement_id for req, _d, _e in pending]
    for priority in priorities:
        allocation = _allocate_fairly(slot_cap, pending, priority)
        for req in must_reqs:
            rid = req.requirement_id
            got = allocation.get(rid, 0)
            if got >= demand_by_id[rid]:
                continue
            uncovered = max(0.01, round(req.effort.likely_person_days - got / 8, 2))
            samples.setdefault(rid, []).append(uncovered)

    truth: dict[str, float] = {}
    for req in must_reqs:
        values = samples.get(req.requirement_id)
        if not values:
            continue
        size = round(sum(values) / len(values), 2)
        key = req.competency_id or f"role:{req.role_id}"
        truth[key] = truth.get(key, 0.0) + size
    return truth


# ---------------------------------------------------------------------------
# Contract-scale synthetic (METRICS §3.2: 500 employees x 30 requirements)
# ---------------------------------------------------------------------------

#: Taxonomy ids used by the scale fixture (mixed disciplines, 10 distinct competencies).
SCALE_COMPETENCIES = (
    "lang_go", "lang_python", "ml_nlp", "ml_pytorch", "db_postgres",
    "sec_appsec", "fe_react", "ops_k8s", "qa_auto", "arch_design",
)


def build_scale_request(
    n_employees: int = 500,
    n_requirements: int = 30,
    n_weeks: int = 16,
    k: int = 3,
    time_limit_ms: int = 10000,
    seed: int = 42,
) -> OptimizeRequest:
    """Deterministic contract-scale request (500 employees x 30 requirements x 16 weeks).

    Fully synthetic and pseudonymous. Every employee holds three competencies at levels
    1..5, so some must requirements are short (the pool is not uniformly strong) — the
    point of the fixture is size, not feasibility: it exercises the METRICS §3.2
    "solve time p95 <= 10 s" target and the TIMEOUT/FEASIBLE degradation path.
    """
    comps = SCALE_COMPETENCIES
    candidates = []
    for i in range(n_employees):
        held = {
            comps[(i + j) % len(comps)]: 1 + ((i * 7 + j * 3) % 5)
            for j in range(3)
        }
        candidates.append(
            make_candidate(
                f"emp_{i:04d}",
                role="backend_engineer" if i % 3 else "ml_engineer",
                competencies=held,
                free_hours=40.0 if i % 5 else 20.0,
                grade=["J", "M", "S", "L"][i % 4],
                cost_coefficient=1.0 + (i % 4) * 0.3,
                bench=(i % 7 == 0),
                n_weeks=n_weeks,
            )
        )

    requirements = []
    for j in range(n_requirements):
        criticality = "must" if j % 3 == 0 else ("should" if j % 3 == 1 else "nice")
        requirements.append(
            make_requirement(
                f"req_{j:04d}",
                role="backend_engineer" if j % 2 else "ml_engineer",
                competency=comps[j % len(comps)],
                level=1 + (j % 5),
                criticality=criticality,
                min_pd=5 + j % 4,
                likely_pd=10 + j % 6,
                max_pd=20 + j % 8,
            )
        )

    return make_request(
        request_id="req_scale000001",
        seed=seed,
        time_limit_ms=time_limit_ms,
        requirements=requirements,
        candidates=candidates,
        horizon=make_horizon(n_weeks=n_weeks),
        k=k,
    )
