"""Tests for ml.app.optimize.schemas — Pydantic models mirroring ml-openapi.yaml."""

from __future__ import annotations

import pytest
from pydantic import ValidationError

from app.optimize.schemas import (
    ActiveConstraintRef,
    CandidateCapacityWeek,
    CandidateCompetency,
    InfeasibilityReport,
    MlDeficit,
    MlMetrics,
    OptimizeCandidate,
    OptimizeConstraints,
    OptimizeHorizon,
    OptimizeOptions,
    OptimizeRequest,
    OptimizeRequirement,
    OptimizeWeights,
    ProposedAssignment,
    ProposedMember,
    SolverInfo,
    VariantProposal,
)
from tests.optimize_helpers import (
    BALANCED_WEIGHTS,
    make_candidate,
    make_horizon,
    make_request,
    make_requirement,
    make_weights,
)


# ---------------------------------------------------------------------------
# OptimizeWeights
# ---------------------------------------------------------------------------

def test_weights_balanced_valid() -> None:
    w = OptimizeWeights(**BALANCED_WEIGHTS)
    assert abs(w.M1 + w.M2 + w.M3 + w.M4 + w.M5 + w.M6 + w.M7 + w.M8 + w.M9 + w.M10 + w.M11 + w.M12 - 1.0) < 1e-6


def test_weights_m4_optional_renormalized() -> None:
    # M4 omitted => remaining must sum to 1.0
    w = OptimizeWeights(
        M1=0.15, M2=0.14, M3=0.11, M5=0.16, M6=0.11, M7=0.09, M8=0.09, M9=0.05, M10=0.05, M11=0.03, M12=0.02,
    )
    assert w.M4 is None
    assert w.get("M4") == 0.0


def test_weights_sum_must_be_one() -> None:
    with pytest.raises(ValidationError):
        OptimizeWeights(M1=0.5, M2=0.4, M3=0.0, M5=0.0, M6=0.0, M7=0.0, M8=0.0, M9=0.0, M10=0.0, M11=0.0)


def test_weights_out_of_range_rejected() -> None:
    with pytest.raises(ValidationError):
        OptimizeWeights(M1=1.5, M2=0.0, M3=0.0, M5=0.0, M6=0.0, M7=0.0, M8=0.0, M9=0.0, M10=0.0, M11=0.0)


# ---------------------------------------------------------------------------
# extra = forbid
# ---------------------------------------------------------------------------

def test_extra_fields_forbidden() -> None:
    with pytest.raises(ValidationError):
        OptimizeRequirement(
            requirement_id="req_0001", role_id="backend_engineer", competency_id="lang_go",
            level=3, criticality="must", headcount=1,
            effort=dict(min_person_days=10, likely_person_days=20, max_person_days=30, unit="person_days"),
            bogus_field=True,
        )
    with pytest.raises(ValidationError):
        OptimizeConstraints(max_utilization_ratio=0.9, not_a_real_constraint=1)


# ---------------------------------------------------------------------------
# patterns / ranges
# ---------------------------------------------------------------------------

def test_employee_id_pattern() -> None:
    with pytest.raises(ValidationError):
        make_candidate("EMP_bad_upper")  # uppercase not allowed
    with pytest.raises(ValidationError):
        make_candidate("emp_x")  # too short


def test_horizon_alignment_enforced() -> None:
    with pytest.raises(ValidationError):
        OptimizeHorizon(start_week="2026-W02", end_week="2026-W05", weeks=["2026-W03", "2026-W04"])


def test_requirement_level_range() -> None:
    with pytest.raises(ValidationError):
        make_requirement("req_x", level=6)
    with pytest.raises(ValidationError):
        make_requirement("req_x", level=0)


def test_candidate_cost_coefficient_range() -> None:
    with pytest.raises(ValidationError):
        make_candidate("emp_abcd", cost_coefficient=9.9)


def test_capacity_free_hours_range() -> None:
    with pytest.raises(ValidationError):
        CandidateCapacityWeek(week="2026-W02", working_days=5, free_hours=99, unavailable=False)


def test_min_team_size_range() -> None:
    with pytest.raises(ValidationError):
        OptimizeConstraints(max_utilization_ratio=0.9, min_team_size=0)


def test_seed_range() -> None:
    with pytest.raises(ValidationError):
        make_request(seed=2**31)


# ---------------------------------------------------------------------------
# full request round-trip + response schemas
# ---------------------------------------------------------------------------

def test_minimal_request_builds() -> None:
    req = make_request()
    assert req.request_id == "req_test00000001"
    assert req.k == 3
    assert len(req.requirements) == 1
    assert len(req.candidates) == 1


def test_response_schemas_build() -> None:
    SolverInfo(name="ortools-cp-sat", version="9.11.4210", status="OPTIMAL", wall_time_ms=10, num_workers=1, seed=42, deterministic=True)
    ProposedMember(employee_id="emp_abcd", role_id="backend_engineer", allocated_hours=100, assignment_share=0.5, reason_codes=["INCLUDE_REQUIRED_ROLE"])
    ProposedAssignment(employee_id="emp_abcd", requirement_id="req_0001", week="2026-W02", hours=40)
    MlMetrics(M1=0.9, score=0.8)
    MlDeficit(competency_id="lang_go", requirement_ids=["req_0001"], person_days=5.0, external_cooperation_flag=True, reason_codes=["DEFICIT_MUST_ROLE"])
    ActiveConstraintRef(id="c1", type="capacity", binding=True, reason_codes=["CONSTRAINT_BINDING_CAPACITY"])
    InfeasibilityReport(blocking_constraints=[ActiveConstraintRef(id="c1", type="capacity", binding=True, reason_codes=["CONSTRAINT_BINDING_CAPACITY"])])


def test_member_reason_codes_min_items() -> None:
    with pytest.raises(ValidationError):
        ProposedMember(employee_id="emp_abcd", role_id="backend_engineer", allocated_hours=10, assignment_share=0.1, reason_codes=[])


def test_variant_id_pattern() -> None:
    with pytest.raises(ValidationError):
        VariantProposal(
            variant_id="V1", label="x", members=[], assignments=[],
            metrics=MlMetrics(), deficits=[], unscheduled_requirements=[], active_constraints=[], reason_codes=[],
        )
