"""Advisory M1..M12 + composite score for /v1/optimize (post-hoc, deterministic).

The CP-SAT solver optimizes a *linearized coverage proxy* (see ``solver.py`` and
``README.md``). These metrics are computed *after* the solve, from the concrete
assignment, using the formulas from ``docs/METRICS.md`` where the optimizer's inputs
allow it. Where a formula needs data the optimizer does not receive (portfolio delay
model for M8, Monte Carlo for M5, dropout simulation for M12), a documented
deterministic approximation is used instead — the Go backend recomputes M1..M12
authoritatively and may reject a variant (that is a tracked defect, not a normal path).

All values are clamped to [0, 1]; ``score`` is the weights-weighted mean of the present
metrics, renormalized so the weights still sum to 1 when M4 (or another optional metric)
is dropped.
"""

from __future__ import annotations

from typing import Optional

from .schemas import OptimizeRequest

Assignment = tuple[str, str, str, float]  # (employee_id, requirement_id, week, hours)

CRIT_WEIGHT = {"must": 1.0, "should": 0.5, "nice": 0.2}
HOURS_PER_DAY = 8
WORKDAYS_PER_WEEK = 5
FRAG_THRESHOLD_HOURS = 8.0  # 20% of a 40h week, used for M10 fragmentation.

#: Advisory metric vector used for the variant-set non-domination rule
#: (METRICS.md "Rules for the set of 2-3 variants", rule 2).
METRIC_KEYS: tuple[str, ...] = tuple(f"M{i}" for i in range(1, 13))


def _clamp01(x: float) -> float:
    return max(0.0, min(1.0, x))


def dominates(
    a: dict[str, Optional[float]],
    b: dict[str, Optional[float]],
    tol: float = 1e-9,
) -> bool:
    """True when metric vector ``a`` Pareto-dominates ``b`` (higher is better, M1..M12).

    Only metrics present (non-``None``) in *both* vectors are compared, so a variant that
    dropped M4 (no project history) is never punished or rewarded for the missing value.
    Dominance requires ``a >= b`` on every shared metric and ``a > b`` on at least one.
    """
    shared = [k for k in METRIC_KEYS if a.get(k) is not None and b.get(k) is not None]
    if not shared:
        return False
    better = False
    for k in shared:
        av, bv = a[k], b[k]  # type: ignore[assignment]
        if av < bv - tol:
            return False
        if av > bv + tol:
            better = True
    return better


def _round6(x: float) -> float:
    return round(float(x), 6)


def compute_advisory_metrics(
    request: OptimizeRequest,
    member_ids: set[str],
    assignments: list[Assignment],
) -> dict[str, Optional[float]]:
    """Compute advisory M1..M12 + score for a concrete assignment.

    Returns a dict keyed by ``M1``..``M12`` and ``score``; values are floats in [0, 1]
    or ``None`` for a metric that is intentionally dropped (M4 when no candidate carries
    an ``experience_score``).
    """
    # ---- build lookup tables -------------------------------------------------
    req_by_id = {r.requirement_id: r for r in request.requirements}
    cand_by_id = {c.employee_id: c for c in request.candidates}
    weeks = request.horizon.weeks
    week_idx = {w: i for i, w in enumerate(weeks)}

    # member competencies / costs / flags
    mem_comp: dict[str, dict[str, int]] = {}
    for c in request.candidates:
        mem_comp[c.employee_id] = {cc.competency_id: cc.level for cc in c.competencies}

    # allocated hours per (employee, requirement, week) and per employee
    alloc: dict[tuple[str, str, str], float] = {}
    alloc_emp: dict[str, float] = {}
    for emp, req, wk, hrs in assignments:
        if hrs <= 0:
            continue
        key = (emp, req, wk)
        alloc[key] = alloc.get(key, 0.0) + hrs
        alloc_emp[emp] = alloc_emp.get(emp, 0.0) + hrs

    total_effort_pd = sum(r.effort.likely_person_days for r in request.requirements)

    # ---- M1 competency coverage ---------------------------------------------
    def cov_r(r) -> float:
        best = 0.0
        for emp in member_ids:
            cand = cand_by_id[emp]
            if r.competency_id is None:
                # role-only requirement: covered if a member holds the role
                if cand.role_profile_id == r.role_id:
                    best = 1.0
            else:
                lvl = mem_comp[emp].get(r.competency_id, 0)
                best = max(best, min(1.0, lvl / r.level))
        return best

    crit_sum = sum(CRIT_WEIGHT[r.criticality] for r in request.requirements)
    if crit_sum > 0:
        m1 = sum(CRIT_WEIGHT[r.criticality] * cov_r(r) for r in request.requirements) / crit_sum
    else:
        m1 = 1.0

    # ---- M2 effort coverage -------------------------------------------------
    def q_ir(emp: str, r) -> float:
        if r.competency_id is None:
            return 1.0
        lvl = mem_comp[emp].get(r.competency_id, 0)
        return min(1.0, lvl / r.level)

    covered_pd = 0.0
    for emp, req, wk, hrs in assignments:
        r = req_by_id.get(req)
        if r is None:
            continue
        covered_pd += (hrs / HOURS_PER_DAY) * q_ir(emp, r)

    m2 = min(1.0, covered_pd / total_effort_pd) if total_effort_pd > 0 else 1.0

    # ---- M3 level fit -------------------------------------------------------
    def fit_ir(emp: str, r) -> float:
        if r.competency_id is None:
            return 1.0
        lvl = mem_comp[emp].get(r.competency_id, 0)
        diff = lvl - r.level
        if diff >= 0:
            penalty = diff / 4.0
        else:
            penalty = 1.5 * (-diff) / 4.0  # under-qualification weighted x1.5
        return _clamp01(1.0 - penalty)

    fit_num = 0.0
    fit_den = 0.0
    for emp, req, wk, hrs in assignments:
        r = req_by_id.get(req)
        if r is None:
            continue
        pd = hrs / HOURS_PER_DAY
        fit_num += pd * fit_ir(emp, r)
        fit_den += pd
    m3 = (fit_num / fit_den) if fit_den > 0 else 1.0

    # ---- M4 experience relevance (dropped when no history) ------------------
    exp_num = 0.0
    exp_den = 0.0
    for emp in member_ids:
        cand = cand_by_id[emp]
        if cand.experience_score is None:
            continue
        pd = alloc_emp.get(emp, 0.0) / HOURS_PER_DAY
        exp_num += pd * cand.experience_score
        exp_den += pd
    m4: Optional[float] = (exp_num / exp_den) if exp_den > 0 else None

    # ---- M5 schedule (deterministic approximation, no Monte Carlo in v1) ----
    active_weeks = {week_idx[wk] for (_, _, wk), hrs in alloc.items() if hrs > 0}
    span_weeks = (max(active_weeks) - min(active_weeks) + 1) if active_weeks else 1
    deadline_idx = week_idx.get(request.constraints.deadline_week) if request.constraints.deadline_week else None
    team_size = max(1, len(member_ids))
    t_min = max(1, int(total_effort_pd / max(1, team_size) / WORKDAYS_PER_WEEK) + (1 if total_effort_pd % (team_size * WORKDAYS_PER_WEEK) else 0))
    if deadline_idx is not None:
        last_active = max(active_weeks) if active_weeks else 0
        if last_active <= deadline_idx:
            m5 = 1.0
        else:
            over = last_active - deadline_idx
            m5 = max(0.0, 1.0 - over / max(1, len(weeks)))
    else:
        m5 = _clamp01(min(1.0, t_min / max(1, span_weeks)))

    # ---- M6 cost ------------------------------------------------------------
    total_cost = sum(
        alloc_emp.get(emp, 0.0) * cand_by_id[emp].cost_coefficient for emp in member_ids
    )
    # theoretical lower bound: cheapest eligible candidate per requirement x likely hours
    c_min = 0.0
    for r in request.requirements:
        elig = [
            c
            for c in request.candidates
            if c.role_profile_id == r.role_id
            and (r.competency_id is None or r.competency_id in mem_comp[c.employee_id])
        ]
        min_coeff = min((c.cost_coefficient for c in elig), default=1.0)
        c_min += r.effort.likely_person_days * HOURS_PER_DAY * min_coeff
    budget = request.constraints.budget_cap_units
    if budget is not None and budget > c_min:
        c_ref = budget
    else:
        c_ref = max(total_cost, c_min) * 2.0
    m6 = _clamp01(1.0 - (total_cost - c_min) / (c_ref - c_min)) if c_ref > c_min else 1.0

    # ---- M7 load health (assigned vs free capacity) -------------------------
    overheat_total = 0.0
    person_weeks = 0
    for emp in member_ids:
        cand = cand_by_id[emp]
        cap_by_week = {}
        for cw in cand.capacity:
            if cw.unavailable:
                cap_by_week[cw.week] = 0.0
            else:
                cap_by_week[cw.week] = max(0.0, cw.free_hours)
        for wk in weeks:
            assigned_here = sum(
                hrs for (e, _, w, hrs) in assignments if e == emp and w == wk
            )
            cap = cap_by_week.get(wk, 0.0)
            person_weeks += 1
            if cap > 0:
                util = assigned_here / cap
                overheat_total += min(1.0, max(0.0, util - 0.9) / 0.1)
    m7 = 1.0 - (overheat_total / person_weeks) if person_weeks > 0 else 1.0

    # ---- M8 portfolio impact (placeholder: not modeled in v1) ---------------
    m8 = 1.0

    # ---- M9 bus factor ------------------------------------------------------
    must_reqs = [r for r in request.requirements if r.criticality == "must"]
    if must_reqs:
        covered_twice = 0
        for r in must_reqs:
            n_qual = 0
            for emp in member_ids:
                cand = cand_by_id[emp]
                if r.competency_id is None:
                    if cand.role_profile_id == r.role_id:
                        n_qual += 1
                elif mem_comp[emp].get(r.competency_id, 0) >= r.level:
                    n_qual += 1
            if n_qual >= 2:
                covered_twice += 1
        m9 = covered_twice / len(must_reqs)
    else:
        m9 = 1.0

    # ---- M10 focus ----------------------------------------------------------
    positive = [hrs for hrs in alloc.values() if hrs > 0]
    frag = (
        sum(1 for h in positive if h < FRAG_THRESHOLD_HOURS) / len(positive)
        if positive
        else 0.0
    )
    concurrent = [
        len(cand_by_id[emp].current_project_ids) for emp in member_ids
    ]
    mean_ctx = sum(concurrent) / len(concurrent) if concurrent else 0.0
    ctx = min(1.0, max(0.0, (mean_ctx - 1.0) / 3.0))
    t_ref = deadline_idx + 1 if deadline_idx is not None else len(weeks)
    n_opt = max(1, int(total_effort_pd / max(1, t_ref * WORKDAYS_PER_WEEK * 0.8)) + (1 if total_effort_pd % (t_ref * WORKDAYS_PER_WEEK * 0.8) else 0))
    size_pen = min(1.0, max(0.0, (team_size - n_opt) / n_opt)) if n_opt > 0 else 0.0
    m10 = 1.0 - (0.4 * frag + 0.3 * ctx + 0.3 * size_pen)

    # ---- M11 bench utilization ---------------------------------------------
    bench_assigned = sum(alloc_emp.get(emp, 0.0) for emp in member_ids if cand_by_id[emp].bench_flag)
    bench_total = 0.0
    for c in request.candidates:
        if c.bench_flag:
            bench_total += sum(cw.free_hours for cw in c.capacity if not cw.unavailable)
    m11 = _clamp01(bench_assigned / bench_total) if bench_total > 0 else 0.0

    # ---- M12 resilience (proxy: bus factor; dropout sim is backend 4.x) -----
    m12 = m9

    # ---- composite score (renormalized over present metrics) ----------------
    metric_values: dict[str, Optional[float]] = {
        "M1": m1,
        "M2": m2,
        "M3": m3,
        "M4": m4,
        "M5": m5,
        "M6": m6,
        "M7": m7,
        "M8": m8,
        "M9": m9,
        "M10": m10,
        "M11": m11,
        "M12": m12,
    }
    weight_sum = 0.0
    score_num = 0.0
    for key, val in metric_values.items():
        if val is None:
            continue
        w = request.weights.get(key, 0.0)
        weight_sum += w
        score_num += w * val
    score = (score_num / weight_sum) if weight_sum > 0 else 0.0

    result: dict[str, Optional[float]] = {k: _round6(v) for k, v in metric_values.items() if v is not None}
    # re-add dropped M4 as None for schema completeness
    if m4 is None:
        result["M4"] = None
    result["score"] = _round6(score)
    return result
