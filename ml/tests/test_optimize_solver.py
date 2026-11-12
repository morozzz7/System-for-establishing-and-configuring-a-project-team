"""Solver tests: hard constraints G1-G5, deficits via slack, k-diversity, non-domination,
determinism, INFEASIBLE naming and freeze_members.

Small hand-built fixtures; no LLM, no network. The OR-Tools solver runs in-process.
"""

from __future__ import annotations

import itertools

import pytest
from ortools.sat.python import cp_model

from app.optimize import solver
from app.optimize.metrics import dominates
from app.optimize.schemas import (
    VALID_REASON_CODES,
    OptimizeConstraints,
    ensure_valid_reason_codes,
)
from app.optimize.solver import SolverValidationError, jaccard_distance
from tests.optimize_helpers import (
    make_candidate,
    make_horizon,
    make_options,
    make_request,
    make_requirement,
)


# ---------------------------------------------------------------------------
# fixtures
# ---------------------------------------------------------------------------

def _staffed(*extra_reqs):
    """A request always needs *some* staffable work, otherwise no team exists at all.

    A deficit-only request (every requirement uncoverable) legitimately has no roster to
    propose, so deficit fixtures pair the shortage with one coverable requirement.
    """
    return [make_requirement("req_0100", competency="lang_go", min_pd=5, likely_pd=10), *extra_reqs]


def _diversity_request(k: int = 3, min_jaccard: float = 0.25):
    """Six interchangeable candidates so several distinct rosters are feasible."""
    reqs = [
        make_requirement("req_0001", competency="lang_go", min_pd=10, likely_pd=20),
        make_requirement("req_0002", competency="lang_python", min_pd=10, likely_pd=20),
    ]
    cands = [
        make_candidate("emp_aaaa", competencies={"lang_go": 4, "lang_python": 3}),
        make_candidate("emp_bbbb", competencies={"lang_go": 3, "lang_python": 4}),
        make_candidate("emp_cccc", competencies={"lang_go": 4, "lang_python": 4}),
        make_candidate("emp_dddd", competencies={"lang_go": 5, "lang_python": 3}),
        make_candidate("emp_eeee", competencies={"lang_go": 3, "lang_python": 5}),
        make_candidate("emp_ffff", competencies={"lang_go": 4, "lang_python": 4}),
    ]
    return make_request(requirements=reqs, candidates=cands, k=k, min_jaccard_diversity=min_jaccard)


# ---------------------------------------------------------------------------
# G1 / G2 / G3 / G5
# ---------------------------------------------------------------------------

def test_g1_no_hours_in_unavailable_week() -> None:
    req = make_request(
        requirements=[make_requirement("req_0001", min_pd=10, likely_pd=20, max_pd=30)],
        candidates=[
            make_candidate("emp_abcd", competencies={"lang_go": 4}, free_hours=40, unavailable_weeks=["2026-W02"]),
        ],
    )
    out = solver.solve(req)
    assert out.status in ("OPTIMAL", "FEASIBLE")
    for emp, _rid, week, hours in out.assignments:
        assert week != "2026-W02", f"hours assigned in unavailable week {week}"


def test_g2_no_overload() -> None:
    req = make_request(
        requirements=[make_requirement("req_0001", min_pd=10, likely_pd=20, max_pd=30)],
        candidates=[make_candidate("emp_abcd", competencies={"lang_go": 4}, free_hours=20)],
    )
    out = solver.solve(req)
    assert out.status in ("OPTIMAL", "FEASIBLE")
    per_week: dict[tuple[str, str], float] = {}
    for emp, _rid, week, hours in out.assignments:
        per_week[(emp, week)] = per_week.get((emp, week), 0.0) + hours
    assert all(h <= 20.0 + 1e-6 for h in per_week.values())


def test_g3_must_uncovered_without_deficits_is_infeasible_with_named_shortage() -> None:
    """v1 behaviour is preserved when the caller forbids deficits."""
    req = make_request(
        requirements=[make_requirement("req_0001", competency="sec_appsec", level=4)],
        candidates=[make_candidate("emp_abcd", competencies={"lang_go": 4})],
        options=make_options(allow_deficits=False),
    )
    out = solver.solve(req)
    assert out.status == "INFEASIBLE"
    assert out.variants == []
    assert out.infeasibility is not None
    assert len(out.infeasibility.deficits) >= 1
    deficit = out.infeasibility.deficits[0]
    assert deficit.competency_id == "sec_appsec"
    assert deficit.requirement_ids == ["req_0001"]
    assert deficit.person_days > 0
    assert "req_0001" in out.infeasibility.unscheduled_requirements
    assert out.infeasibility.blocking_constraints


def test_g3_must_uncovered_becomes_deficit_variant_when_allowed() -> None:
    """v2: an uncoverable must yields the best partial team plus an explicit deficit."""
    req = make_request(
        requirements=[
            make_requirement("req_0001", competency="lang_go", min_pd=5, likely_pd=10),
            make_requirement("req_0002", competency="sec_appsec", level=4, min_pd=10, likely_pd=20),
        ],
        candidates=[make_candidate("emp_abcd", competencies={"lang_go": 4})],
    )
    out = solver.solve(req)
    assert out.status in ("OPTIMAL", "FEASIBLE")
    assert out.variants, "a deficit must still return the best partial team"
    variant = out.variants[0]
    assert variant.members == ["emp_abcd"]
    # the coverable requirement is staffed, the uncoverable one is a deficit
    assert {rid for _e, rid, _w, _h in variant.assignments} == {"req_0001"}
    assert len(variant.deficits) == 1
    deficit = variant.deficits[0]
    assert deficit.competency_id == "sec_appsec"
    assert deficit.requirement_ids == ["req_0002"]
    assert deficit.person_days > 0
    assert deficit.external_cooperation_flag is True
    assert "DEFICIT_MUST_ROLE" in deficit.reason_codes
    assert "req_0002" in variant.unscheduled_requirements


def test_g5_only_candidate_ids_in_output() -> None:
    req = make_request(
        requirements=[make_requirement("req_0001")],
        candidates=[
            make_candidate("emp_abcd", competencies={"lang_go": 4}),
            make_candidate("emp_wxyz", competencies={"lang_go": 4}),
        ],
    )
    out = solver.solve(req)
    assert out.status in ("OPTIMAL", "FEASIBLE")
    valid = {"emp_abcd", "emp_wxyz"}
    for variant in out.variants:
        assert set(variant.members) <= valid
        assert all(emp in valid for emp, _r, _w, _h in variant.assignments)


# ---------------------------------------------------------------------------
# G4 constraints
# ---------------------------------------------------------------------------

def test_g4_force_include() -> None:
    req = make_request(
        requirements=[make_requirement("req_0001")],
        candidates=[
            make_candidate("emp_aaaa", competencies={"lang_go": 4}),
            make_candidate("emp_bbbb", competencies={"lang_go": 4}),
        ],
        constraints=OptimizeConstraints(max_utilization_ratio=0.9, force_include=["emp_bbbb"]),
    )
    out = solver.solve(req)
    assert out.status in ("OPTIMAL", "FEASIBLE")
    for variant in out.variants:
        assert "emp_bbbb" in variant.members


def test_g4_force_exclude() -> None:
    req = make_request(
        requirements=[make_requirement("req_0001")],
        candidates=[
            make_candidate("emp_aaaa", competencies={"lang_go": 4}),
            make_candidate("emp_bbbb", competencies={"lang_go": 4}),
        ],
        constraints=OptimizeConstraints(max_utilization_ratio=0.9, force_exclude=["emp_bbbb"]),
    )
    out = solver.solve(req)
    assert out.status in ("OPTIMAL", "FEASIBLE")
    for variant in out.variants:
        assert "emp_bbbb" not in variant.members


def test_g4_max_team_size() -> None:
    req = make_request(
        requirements=[
            make_requirement("req_0001", competency="lang_go"),
            make_requirement("req_0002", competency="lang_python"),
        ],
        candidates=[
            make_candidate("emp_aaaa", competencies={"lang_go": 4}),
            make_candidate("emp_bbbb", competencies={"lang_python": 4}),
            make_candidate("emp_cccc", competencies={"lang_go": 4, "lang_python": 4}),
        ],
        constraints=OptimizeConstraints(max_utilization_ratio=0.9, max_team_size=2),
    )
    out = solver.solve(req)
    assert out.status in ("OPTIMAL", "FEASIBLE")
    for variant in out.variants:
        assert len(variant.members) <= 2


def test_g4_budget_too_low_infeasible_without_deficits() -> None:
    req = make_request(
        requirements=[make_requirement("req_0001", min_pd=10, likely_pd=20, max_pd=30)],
        candidates=[make_candidate("emp_abcd", competencies={"lang_go": 4}, cost_coefficient=1.9)],
        constraints=OptimizeConstraints(max_utilization_ratio=0.9, budget_cap_units=50),
        options=make_options(allow_deficits=False),
    )
    out = solver.solve(req)
    assert out.status == "INFEASIBLE"
    assert out.variants == []
    assert out.infeasibility is not None


def test_g4_budget_shortfall_returns_deficit_variant_and_never_exceeds_cap() -> None:
    """A binding budget is *respected* and reported as a (descope) deficit, never relaxed."""
    req = make_request(
        requirements=[make_requirement("req_0001", min_pd=10, likely_pd=20, max_pd=30)],
        candidates=[make_candidate("emp_abcd", competencies={"lang_go": 4}, cost_coefficient=1.9)],
        constraints=OptimizeConstraints(max_utilization_ratio=0.9, budget_cap_units=50),
    )
    out = solver.solve(req)
    assert out.status in ("OPTIMAL", "FEASIBLE")
    variant = out.variants[0]
    assert variant.cost_units <= 50 + 1e-6, "budget cap must never be exceeded"
    assert variant.deficits, "an unaffordable must effort must be reported as a deficit"
    assert "DEFICIT_DESCOPE" in variant.deficits[0].reason_codes
    assert "DEFICIT_MUST_ROLE" in variant.deficits[0].reason_codes


def test_g4_zero_budget_is_infeasible_with_named_constraint() -> None:
    """Nothing can be staffed at all -> no team to propose -> INFEASIBLE naming the cap."""
    req = make_request(
        requirements=[make_requirement("req_0001", min_pd=10, likely_pd=20, max_pd=30)],
        candidates=[make_candidate("emp_abcd", competencies={"lang_go": 4}, cost_coefficient=1.9)],
        constraints=OptimizeConstraints(max_utilization_ratio=0.9, budget_cap_units=0),
    )
    out = solver.solve(req)
    assert out.status == "INFEASIBLE"
    assert out.variants == []
    assert out.infeasibility is not None
    types = {b.type for b in out.infeasibility.blocking_constraints}
    assert "budget_cap" in types


def test_g4_deadline_too_early_infeasible_without_deficits() -> None:
    req = make_request(
        requirements=[make_requirement("req_0001", min_pd=20, likely_pd=40, max_pd=60)],
        candidates=[make_candidate("emp_abcd", competencies={"lang_go": 4}, free_hours=40)],
        horizon=make_horizon("2026-W02", n_weeks=1),  # single week, 40h < 20pd=160h
        constraints=OptimizeConstraints(max_utilization_ratio=0.9, deadline_week="2026-W02"),
        options=make_options(allow_deficits=False),
    )
    out = solver.solve(req)
    assert out.status == "INFEASIBLE"
    assert out.variants == []
    assert out.infeasibility is not None


def test_g4_deadline_shortfall_recommends_a_deadline_shift() -> None:
    req = make_request(
        requirements=[make_requirement("req_0001", min_pd=20, likely_pd=40, max_pd=60)],
        candidates=[make_candidate("emp_abcd", competencies={"lang_go": 4}, free_hours=40)],
        horizon=make_horizon("2026-W02", n_weeks=1),
        constraints=OptimizeConstraints(max_utilization_ratio=0.9, deadline_week="2026-W02"),
    )
    out = solver.solve(req)
    assert out.status in ("OPTIMAL", "FEASIBLE")
    variant = out.variants[0]
    assert variant.deficits
    codes = {c for d in variant.deficits for c in d.reason_codes}
    assert "DEFICIT_DEADLINE_SHIFT" in codes
    # G4 deadline is respected: no hours after the deadline
    assert all(wk <= "2026-W02" for _e, _r, wk, _h in variant.assignments)


def test_g4_freeze_members_keeps_roster() -> None:
    req = make_request(
        requirements=[make_requirement("req_0001")],
        candidates=[
            make_candidate("emp_aaaa", competencies={"lang_go": 4}),
            make_candidate("emp_bbbb", competencies={"lang_go": 4}),
        ],
        constraints=OptimizeConstraints(max_utilization_ratio=0.9, freeze_members=["emp_bbbb"]),
    )
    out = solver.solve(req)
    assert out.status in ("OPTIMAL", "FEASIBLE")
    for variant in out.variants:
        assert "emp_bbbb" in variant.members


def test_g4_min_bus_factor() -> None:
    req = make_request(
        requirements=[make_requirement("req_0001")],
        candidates=[
            make_candidate("emp_aaaa", competencies={"lang_go": 4}),
            make_candidate("emp_bbbb", competencies={"lang_go": 4}),
            make_candidate("emp_cccc", competencies={"lang_go": 4}),
        ],
        constraints=OptimizeConstraints(max_utilization_ratio=0.9, min_bus_factor=2),
    )
    out = solver.solve(req)
    assert out.status in ("OPTIMAL", "FEASIBLE")
    for variant in out.variants:
        assert len(variant.members) >= 2


def test_g4_min_bus_factor_is_never_relaxed_by_deficits() -> None:
    """G4 stays hard: an uncoverable must + min_bus_factor=2 is INFEASIBLE, not a deficit."""
    req = make_request(
        requirements=[make_requirement("req_0001", competency="sec_appsec", level=4)],
        candidates=[make_candidate("emp_abcd", competencies={"lang_go": 4})],
        constraints=OptimizeConstraints(max_utilization_ratio=0.9, min_bus_factor=2),
    )
    out = solver.solve(req)
    assert out.status == "INFEASIBLE"
    assert out.variants == []
    assert out.infeasibility is not None
    assert any(b.type == "min_bus_factor" for b in out.infeasibility.blocking_constraints)


# ---------------------------------------------------------------------------
# validation errors -> 422 semantics (SolverValidationError)
# ---------------------------------------------------------------------------

def test_force_include_unknown_candidate_raises() -> None:
    req = make_request(
        constraints=OptimizeConstraints(max_utilization_ratio=0.9, force_include=["emp_nope"]),
    )
    with pytest.raises(SolverValidationError):
        solver.solve(req)


def test_force_include_and_exclude_conflict_raises() -> None:
    req = make_request(
        constraints=OptimizeConstraints(
            max_utilization_ratio=0.9, force_include=["emp_abcd"], force_exclude=["emp_abcd"],
        ),
    )
    with pytest.raises(SolverValidationError):
        solver.solve(req)


# ---------------------------------------------------------------------------
# v2: deficits via slack
# ---------------------------------------------------------------------------

def test_allow_deficits_false_never_returns_a_partial_plan() -> None:
    """Every returned must requirement is fully covered at the required level."""
    req = make_request(
        requirements=[
            make_requirement("req_0001", competency="lang_go", min_pd=5, likely_pd=10),
            make_requirement("req_0002", competency="sec_appsec", level=4, min_pd=10, likely_pd=20),
        ],
        candidates=[make_candidate("emp_abcd", competencies={"lang_go": 4})],
        options=make_options(allow_deficits=False),
    )
    out = solver.solve(req)
    assert out.status == "INFEASIBLE"
    assert out.variants == []


def test_deficits_group_by_competency_and_are_sorted() -> None:
    req = make_request(
        requirements=[
            make_requirement("req_0001", competency="sec_appsec", level=4, min_pd=10, likely_pd=20),
            make_requirement("req_0002", competency="sec_appsec", level=3, min_pd=5, likely_pd=10),
            make_requirement("req_0003", competency="lang_go", min_pd=5, likely_pd=10),
        ],
        candidates=[make_candidate("emp_abcd", competencies={"lang_go": 4})],
    )
    out = solver.solve(req)
    variant = out.variants[0]
    assert len(variant.deficits) == 1
    deficit = variant.deficits[0]
    assert deficit.competency_id == "sec_appsec"
    assert deficit.requirement_ids == ["req_0001", "req_0002"]
    assert deficit.person_days == pytest.approx(30.0)  # 20 + 10 likely person-days
    assert deficit.external_cooperation_flag is True


def test_deficit_for_role_only_requirement_has_null_competency() -> None:
    req = make_request(
        requirements=_staffed(
            make_requirement("req_0001", role="ml_engineer", competency=None, min_pd=10, likely_pd=20),
        ),
        candidates=[make_candidate("emp_abcd", role="backend_engineer", competencies={"lang_go": 4})],
    )
    out = solver.solve(req)
    variant = out.variants[0]
    assert len(variant.deficits) == 1
    assert variant.deficits[0].competency_id is None
    assert variant.deficits[0].role_id == "ml_engineer"
    assert out.infeasibility is None


# ---------------------------------------------------------------------------
# v2: k diverse variants + non-domination
# ---------------------------------------------------------------------------

def test_diversity_returns_distinct_rosters_above_threshold() -> None:
    out = solver.solve(_diversity_request(k=3, min_jaccard=0.25))
    assert out.status in ("OPTIMAL", "FEASIBLE")
    assert len(out.variants) >= 2
    rosters = [v.roster for v in out.variants]
    assert len(set(rosters)) == len(rosters), "rosters must never be duplicated"
    for a, b in itertools.combinations(rosters, 2):
        assert jaccard_distance(a, b) >= 0.25 - 1e-9, (sorted(a), sorted(b))


def test_diversity_respects_requested_threshold() -> None:
    """A stricter threshold can only shrink the variant set, never duplicate rosters."""
    loose = solver.solve(_diversity_request(k=3, min_jaccard=0.25))
    strict = solver.solve(_diversity_request(k=3, min_jaccard=1.0))
    assert len(strict.variants) >= 1
    assert len(strict.variants) <= len(loose.variants)
    for a, b in itertools.combinations([v.roster for v in strict.variants], 2):
        assert jaccard_distance(a, b) >= 1.0 - 1e-9


def test_variants_are_non_dominated_on_m1_m12() -> None:
    for request in (
        _diversity_request(k=3),
        make_request(
            requirements=[
                make_requirement("req_0001", competency="lang_go"),
                make_requirement("req_0002", competency="lang_python", criticality="should"),
            ],
            candidates=[
                make_candidate("emp_aaaa", competencies={"lang_go": 5, "lang_python": 5}, cost_coefficient=3.4),
                make_candidate("emp_bbbb", competencies={"lang_go": 3, "lang_python": 3}, cost_coefficient=1.0),
                make_candidate("emp_cccc", competencies={"lang_go": 4, "lang_python": 4}, cost_coefficient=1.9),
                make_candidate("emp_dddd", competencies={"lang_go": 4, "lang_python": 2}, cost_coefficient=1.4),
            ],
        ),
    ):
        out = solver.solve(request)
        for a, b in itertools.combinations(out.variants, 2):
            assert not dominates(a.metrics, b.metrics), (a.members, b.members)
            assert not dominates(b.metrics, a.metrics), (b.members, a.members)


def test_variants_sorted_by_advisory_score() -> None:
    out = solver.solve(_diversity_request(k=3))
    scores = [v.metrics["score"] for v in out.variants]
    assert scores == sorted(scores, reverse=True)


def test_dropped_diversity_variants_accounting() -> None:
    # single candidate -> only one roster can ever exist
    single = solver.solve(make_request(k=3))
    assert len(single.variants) == 1
    assert single.dropped_diversity_variants == 2

    # a rich pool -> we ask for k and drop the surplus attempt
    rich = solver.solve(_diversity_request(k=2))
    assert len(rich.variants) <= 2
    assert rich.dropped_diversity_variants >= max(0, 2 - len(rich.variants))


def test_k_is_respected() -> None:
    out2 = solver.solve(_diversity_request(k=2))
    assert 1 <= len(out2.variants) <= 2


# ---------------------------------------------------------------------------
# v2: reason codes
# ---------------------------------------------------------------------------

def test_deficit_reason_codes_are_contract_valid() -> None:
    req = make_request(
        requirements=_staffed(make_requirement("req_0001", competency="sec_appsec", level=4)),
        candidates=[make_candidate("emp_abcd", competencies={"lang_go": 4})],
    )
    out = solver.solve(req)
    codes = [c for d in out.variants[0].deficits for c in d.reason_codes]
    assert codes, "a deficit must carry reason codes"
    assert all(c in VALID_REASON_CODES for c in codes)
    assert codes[0] == "DEFICIT_MUST_ROLE"
    # nobody holds sec_appsec at all -> recommend external cooperation
    assert "DEFICIT_EXTERNAL_CONTRACTOR" in codes


def test_upskill_code_when_people_hold_the_competency_below_level() -> None:
    req = make_request(
        requirements=_staffed(
            make_requirement("req_0001", competency="lang_go", level=5, min_pd=10, likely_pd=20),
        ),
        candidates=[make_candidate("emp_abcd", competencies={"lang_go": 3})],
    )
    out = solver.solve(req)
    codes = {c for d in out.variants[0].deficits for c in d.reason_codes}
    assert "DEFICIT_UPSKILL_INTERNAL" in codes
    assert "DEFICIT_EXTERNAL_CONTRACTOR" not in codes


def test_ensure_valid_reason_codes_filters_unknown_codes() -> None:
    assert ensure_valid_reason_codes(["INCLUDE_BENCH", "NOT_A_CODE"]) == ["INCLUDE_BENCH"]


# ---------------------------------------------------------------------------
# determinism
# ---------------------------------------------------------------------------

def test_determinism_same_request_same_output() -> None:
    def run():
        req = make_request(
            seed=42,
            requirements=[
                make_requirement("req_0001", competency="lang_go"),
                make_requirement("req_0002", competency="lang_python"),
                make_requirement("req_0003", competency="db_postgresql", criticality="should"),
            ],
            candidates=[
                make_candidate("emp_aaaa", competencies={"lang_go": 4, "lang_python": 3}),
                make_candidate("emp_bbbb", competencies={"lang_go": 3, "db_postgresql": 4}),
                make_candidate("emp_cccc", competencies={"lang_python": 4, "db_postgresql": 3}),
            ],
        )
        return solver.solve(req)

    a = run()
    b = run()
    assert a.status == b.status
    assert [v.members for v in a.variants] == [v.members for v in b.variants]
    assert [v.assignments for v in a.variants] == [v.assignments for v in b.variants]
    assert [v.objective_value for v in a.variants] == [v.objective_value for v in b.variants]
    assert a.members == b.members
    assert a.assignments == b.assignments
    assert a.objective_value == b.objective_value
    assert a.num_vars == b.num_vars
    assert a.num_constraints == b.num_constraints
    assert a.dropped_diversity_variants == b.dropped_diversity_variants


def test_objective_and_gap_reported_for_optimal() -> None:
    req = make_request(requirements=[make_requirement("req_0001")])
    out = solver.solve(req)
    assert out.status == "OPTIMAL"
    assert out.objective_value is not None and 0.0 <= out.objective_value <= 1.0
    assert out.best_bound is not None
    assert out.optimality_gap == 0.0


def test_deficit_variant_reports_no_misleading_bound() -> None:
    """With slack active the CP-SAT bound is not comparable -> omitted, not faked."""
    req = make_request(
        requirements=_staffed(make_requirement("req_0001", competency="sec_appsec", level=4)),
        candidates=[make_candidate("emp_abcd", competencies={"lang_go": 4})],
    )
    variant = solver.solve(req).variants[0]
    assert variant.total_slack_hours > 0
    assert variant.best_bound is None
    assert variant.optimality_gap is None


# ---------------------------------------------------------------------------
# graceful timeout (best found + status)
# ---------------------------------------------------------------------------

def test_feasibility_fallback_returns_best_found_team(monkeypatch) -> None:
    """No incumbent after the first attempt -> objective-free fallback, honest fields.

    The objective-guided solve is made to fail deterministically (that is what CP-SAT
    returns as ``UNKNOWN`` when the whole per-attempt budget is spent without a solution);
    the degradation path must still return the best found team, a ``FEASIBLE`` status, no
    invented objective/bound/gap and the ``SOLVER_TIMEOUT`` reason code.
    """
    from app.optimize.solver import _Model

    real_solve_once = _Model.solve_once
    calls = {"guidance": 0, "fallback": 0}

    def fake_solve_once(self, request, time_limit_ms):
        if not self.objective_cleared:
            calls["guidance"] += 1
            return cp_model.UNKNOWN, 5
        calls["fallback"] += 1
        return real_solve_once(self, request, time_limit_ms)

    monkeypatch.setattr(_Model, "solve_once", fake_solve_once)

    out = solver.solve(make_request())
    assert out.status == "FEASIBLE"
    assert len(out.variants) == 1
    variant = out.variants[0]
    assert variant.members, "the fallback must return a real roster"
    assert variant.objective_value is None
    assert variant.best_bound is None
    assert variant.optimality_gap is None
    assert out.reason_codes == ["SOLVER_TIMEOUT"]
    assert out.dropped_diversity_variants == 2  # k - 1: only one variant was found
    assert calls == {"guidance": 1, "fallback": 1}


def test_no_solution_at_all_is_timeout_not_a_fake_variant(monkeypatch) -> None:
    """If even the feasibility fallback finds nothing, report TIMEOUT with no variants."""
    from app.optimize.solver import _Model

    def always_unknown(self, request, time_limit_ms):
        return cp_model.UNKNOWN, 1

    monkeypatch.setattr(_Model, "solve_once", always_unknown)

    out = solver.solve(make_request())
    assert out.status == "TIMEOUT"
    assert out.variants == []
    assert out.infeasibility is not None
    assert "SOLVER_TIMEOUT" in out.infeasibility.reason_codes
    assert out.infeasibility.blocking_constraints


def test_infeasible_is_not_retried_as_a_degraded_plan(monkeypatch) -> None:
    """INFEASIBLE must never be softened into a fallback plan (G4 stays hard)."""
    from app.optimize.solver import _Model

    calls = {"n": 0}

    def fake_solve_once(self, request, time_limit_ms):
        calls["n"] += 1
        return cp_model.INFEASIBLE, 1

    monkeypatch.setattr(_Model, "solve_once", fake_solve_once)

    out = solver.solve(make_request())
    assert out.status == "INFEASIBLE"
    assert out.variants == []
    assert calls == {"n": 1}
