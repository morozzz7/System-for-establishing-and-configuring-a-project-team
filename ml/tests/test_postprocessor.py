"""Tests for the post-processor (3.3 & ADR-005): validate via competency.roles, never auto-rewrite."""

from __future__ import annotations

import pytest

from app.extract.postprocessor import RoleCompetencyValidator
from app.extract.schemas import (
    EffortRange,
    EvidenceSpan,
    ExtractCompetency,
    ExtractedRequirement,
    ExtractRole,
)
from app.extract.taxonomy import get_registry, reset_registry


@pytest.fixture(autouse=True)
def _reset_registry():
    reset_registry()
    yield
    reset_registry()


def _make_req(
    role_id: str,
    comp_id: str | None,
    level: int = 3,
    confidence: float = 0.9,
    needs_review: bool = False,
) -> ExtractedRequirement:
    role = ExtractRole(
        role_id=role_id,
        name_ru=f"Role {role_id}",
        match="llm",
        confidence=confidence,
    )
    comp = None
    if comp_id:
        comp = ExtractCompetency(
            competency_id=comp_id,
            name_ru=f"Comp {comp_id}",
            match="llm",
            confidence=confidence,
            candidates=[],
        )
    return ExtractedRequirement(
        requirement_id="req_test",
        role=role,
        competency=comp,
        level=level,
        criticality="must",
        headcount=1,
        effort=EffortRange(min_person_days=10, likely_person_days=20, max_person_days=30),
        assumptions=["Timeline parsed: 8 weeks = 40 working days"],
        confidence=confidence,
        needs_review=needs_review,
        evidence=[
            EvidenceSpan(
                kind="brief_span",
                quote="some evidence quote here",
                char_start=0,
                char_end=24,
            )
        ],
    )


class TestPostprocessorRejectNoRewrite:
    def test_valid_association_kept(self):
        registry = get_registry()
        validator = RoleCompetencyValidator(registry)
        req = _make_req("backend_engineer", "lang_go")
        result = validator.validate_and_fix([req])
        assert len(result) == 1
        assert validator.dropped_count == 0

    def test_invalid_association_dropped_never_rewritten(self):
        """Per Principle 5 / 3.3, invalid role-competency pairs are NEVER rewritten automatically."""
        registry = get_registry()
        validator = RoleCompetencyValidator(registry)
        req = _make_req("frontend_engineer", "lang_go")
        result = validator.validate_and_fix([req])
        assert len(result) == 0
        assert validator.dropped_count == 1
        assert validator.dropped_invalid_pair == 1

    def test_non_existent_competency_dropped(self):
        registry = get_registry()
        validator = RoleCompetencyValidator(registry)
        req = _make_req("backend_engineer", "not_a_competency")
        result = validator.validate_and_fix([req])
        assert len(result) == 0
        assert validator.dropped_count == 1
        assert validator.dropped_unknown_taxonomy == 1

    def test_role_only_kept_with_review_flag(self):
        registry = get_registry()
        validator = RoleCompetencyValidator(registry)
        req = _make_req("project_manager", None)
        result = validator.validate_and_fix([req])
        assert len(result) == 1
        assert result[0].needs_review is True
        assert validator.dropped_count == 0

    def test_unknown_role_dropped(self):
        registry = get_registry()
        validator = RoleCompetencyValidator(registry)
        req = _make_req("not_a_role", "lang_go")
        result = validator.validate_and_fix([req])
        assert len(result) == 0
        assert validator.dropped_count == 1
        assert validator.dropped_unknown_taxonomy == 1

    def test_mixed_valid_and_invalid(self):
        registry = get_registry()
        validator = RoleCompetencyValidator(registry)
        valid = _make_req("backend_engineer", "lang_go")
        invalid_pair = _make_req("frontend_engineer", "lang_go")
        unknown_comp = _make_req("backend_engineer", "not_a_competency")
        result = validator.validate_and_fix([valid, invalid_pair, unknown_comp])
        assert len(result) == 1
        assert result[0].role.role_id == "backend_engineer"
        assert validator.dropped_count == 2
        assert validator.dropped_invalid_pair == 1
        assert validator.dropped_unknown_taxonomy == 1

    def test_request_taxonomy_subset_enforced(self):
        registry = get_registry()
        validator = RoleCompetencyValidator(
            registry,
            allowed_role_ids=["backend_engineer"],
            allowed_competency_ids=["lang_go"],
        )
        ok_req = _make_req("backend_engineer", "lang_go")
        disallowed_role = _make_req("devops_engineer", "lang_go")
        disallowed_comp = _make_req("backend_engineer", "db_postgresql")
        result = validator.validate_and_fix([ok_req, disallowed_role, disallowed_comp])
        assert len(result) == 1
        assert result[0].role.role_id == "backend_engineer"
        assert validator.dropped_count == 2


class TestPostprocessorReviewFlags:
    def test_low_confidence_flagged(self):
        registry = get_registry()
        validator = RoleCompetencyValidator(registry)
        req = _make_req("backend_engineer", "lang_go", confidence=0.5)
        result = validator.validate_and_fix([req])
        assert result[0].needs_review is True

    def test_non_core_low_confidence_flagged(self):
        """Non-core competency with confidence < 0.75 is flagged with needs_review=True."""
        registry = get_registry()
        validator = RoleCompetencyValidator(registry)
        # cloud_yandex is in comp.roles for devops_engineer, but not in core_competencies
        req = _make_req("devops_engineer", "cloud_yandex", confidence=0.70, needs_review=False)
        result = validator.validate_and_fix([req])
        assert len(result) == 1
        assert result[0].needs_review is True

    def test_junior_architect_flagged(self):
        registry = get_registry()
        validator = RoleCompetencyValidator(registry)
        req = _make_req("solution_architect", "arch_system_design", level=2)
        result = validator.validate_and_fix([req])
        assert result[0].needs_review is True

    def test_valid_middle_not_flagged(self):
        registry = get_registry()
        validator = RoleCompetencyValidator(registry)
        req = _make_req("backend_engineer", "lang_go", level=3, confidence=0.9)
        result = validator.validate_and_fix([req])
        assert result[0].needs_review is False

    def test_dropped_requirement_does_not_flag_others(self):
        registry = get_registry()
        validator = RoleCompetencyValidator(registry)
        valid = _make_req("backend_engineer", "lang_go", confidence=0.9, needs_review=False)
        invalid = _make_req("frontend_engineer", "lang_go")
        result = validator.validate_and_fix([valid, invalid])
        assert len(result) == 1
        assert result[0].needs_review is False
        assert validator.dropped_count == 1


class TestPostprocessorManyToMany:
    def test_devops_cloud_yandex_passes(self):
        """cloud_yandex is valid for devops_engineer via competency.roles (brief_001)."""
        registry = get_registry()
        validator = RoleCompetencyValidator(registry)
        req = _make_req("devops_engineer", "cloud_yandex", confidence=0.9)
        result = validator.validate_and_fix([req])
        assert len(result) == 1
        assert result[0].role.role_id == "devops_engineer"
        assert validator.dropped_count == 0

    def test_db_postgresql_solution_architect_dropped_not_reassigned(self):
        """PostgreSQL is not in comp.roles for solution_architect; it must be dropped, never rewritten."""
        registry = get_registry()
        validator = RoleCompetencyValidator(registry)
        req = _make_req("solution_architect", "db_postgresql")
        result = validator.validate_and_fix([req])
        assert len(result) == 0
        assert validator.dropped_count == 1
        assert validator.dropped_invalid_pair == 1
