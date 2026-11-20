"""Tests for ml.app.extract.schemas — Pydantic models matching ml-openapi.yaml."""

from __future__ import annotations

import json

import pytest
from pydantic import ValidationError

from app.extract.schemas import (
    BriefPayload,
    CompetencyCandidate,
    EvidenceSpan,
    EffortRange,
    ExtractCompetency,
    ExtractDiagnostics,
    ExtractOptions,
    ExtractRequest,
    ExtractResponse,
    ExtractRole,
    ExtractedRequirement,
    ExtractTaxonomy,
    FieldError,
    Problem,
)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _minimal_requirement(**overrides) -> ExtractedRequirement:
    """Build a minimal valid ExtractedRequirement."""
    defaults = dict(
        requirement_id="req_ab12",
        role=ExtractRole(role_id="backend_engineer", name_ru="Бэкенд-разработчик", match="exact_synonym", confidence=0.95),
        competency=ExtractCompetency(
            competency_id="lang_go", name_ru="Go", match="exact_synonym", confidence=0.95,
            candidates=[CompetencyCandidate(competency_id="lang_go", score=0.95)],
        ),
        level=3,
        criticality="must",
        headcount=1,
        effort=EffortRange(min_person_days=28, likely_person_days=40, max_person_days=56, unit="person_days"),
        assumptions=["Timeline: 8 weeks"],
        confidence=0.9,
        needs_review=False,
        evidence=[EvidenceSpan(kind="brief_span", quote="Go разработчик", char_start=0, char_end=15)],
    )
    defaults.update(overrides)
    return ExtractedRequirement(**defaults)


def _minimal_response() -> ExtractResponse:
    return ExtractResponse(
        request_id="req_01HX7Qaa",
        brief_id="brf_001",
        requirements=[_minimal_requirement()],
        diagnostics=ExtractDiagnostics(
            engine="rules", model="rules-v1", prompt_version="rules-v1",
            temperature=0, duration_ms=100, json_valid=True, retries=0, dropped_no_evidence=0,
        ),
        warnings=[],
        reason_codes=[],
    )


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


class TestEvidenceSpan:
    def test_valid(self):
        ev = EvidenceSpan(kind="brief_span", quote="Go разработчик", char_start=0, char_end=15)
        assert ev.kind == "brief_span"
        assert ev.quote == "Go разработчик"

    def test_quote_min_length(self):
        with pytest.raises(ValidationError):
            EvidenceSpan(kind="brief_span", quote="ab")  # < 3 chars

    def test_reject_extra_field(self):
        with pytest.raises(ValidationError):
            EvidenceSpan(kind="brief_span", quote="test", extra_field="bad")


class TestEffortRange:
    def test_valid(self):
        er = EffortRange(min_person_days=10, likely_person_days=20, max_person_days=30, unit="person_days")
        assert er.unit == "person_days"

    def test_min_below_05(self):
        with pytest.raises(ValidationError):
            EffortRange(min_person_days=0.1, likely_person_days=20, max_person_days=30, unit="person_days")

    def test_invalid_unit(self):
        with pytest.raises(ValidationError):
            EffortRange(min_person_days=10, likely_person_days=20, max_person_days=30, unit="hours")

    def test_max_less_than_min_rejected(self):
        """Contract: max_person_days must be >= min_person_days."""
        with pytest.raises(ValidationError):
            EffortRange(min_person_days=30, likely_person_days=40, max_person_days=20, unit="person_days")

    def test_max_less_than_likely_rejected(self):
        """Contract: max_person_days must be >= likely_person_days."""
        with pytest.raises(ValidationError):
            EffortRange(min_person_days=10, likely_person_days=40, max_person_days=30, unit="person_days")

    def test_likely_less_than_min_rejected(self):
        """Contract: likely_person_days must be >= min_person_days."""
        with pytest.raises(ValidationError):
            EffortRange(min_person_days=30, likely_person_days=20, max_person_days=40, unit="person_days")

    def test_equal_values_allowed(self):
        """Edge case: min = likely = max is valid (single-point estimate, though unusual)."""
        er = EffortRange(min_person_days=20, likely_person_days=20, max_person_days=20, unit="person_days")
        assert er.min_person_days == er.likely_person_days == er.max_person_days


class TestExtractedRequirement:
    def test_valid(self):
        req = _minimal_requirement()
        assert req.requirement_id == "req_ab12"
        assert req.level == 3

    def test_competency_null_allowed(self):
        req = _minimal_requirement(competency=None)
        assert req.competency is None

    def test_invalid_level(self):
        with pytest.raises(ValidationError):
            _minimal_requirement(level=0)
        with pytest.raises(ValidationError):
            _minimal_requirement(level=6)

    def test_headcount_bounds(self):
        with pytest.raises(ValidationError):
            _minimal_requirement(headcount=0)
        with pytest.raises(ValidationError):
            _minimal_requirement(headcount=21)

    def test_empty_evidence_rejected(self):
        with pytest.raises(ValidationError):
            _minimal_requirement(evidence=[])

    def test_empty_assumptions_rejected(self):
        with pytest.raises(ValidationError):
            _minimal_requirement(assumptions=[])

    def test_invalid_requirement_id_pattern(self):
        with pytest.raises(ValidationError):
            _minimal_requirement(requirement_id="bad!")


class TestExtractResponse:
    def test_round_trip_json(self):
        resp = _minimal_response()
        data = resp.model_dump(mode="json")
        json_str = json.dumps(data, ensure_ascii=False)
        reloaded = ExtractResponse.model_validate_json(json_str)
        assert reloaded == resp

    def test_reject_extra_field(self):
        data = _minimal_response().model_dump()
        data["extra"] = "bad"
        with pytest.raises(ValidationError):
            ExtractResponse.model_validate(data)

    def test_invalid_request_id(self):
        with pytest.raises(ValidationError):
            ExtractResponse(
                request_id="bad_id",
                brief_id="brf_001",
                requirements=[],
                diagnostics=ExtractDiagnostics(
                    engine="rules", model="rules-v1", prompt_version="rules-v1",
                    temperature=0, duration_ms=100, json_valid=True, retries=0, dropped_no_evidence=0,
                ),
                warnings=[],
                reason_codes=[],
            )


class TestExtractRequest:
    def test_valid_request(self):
        req = ExtractRequest(
            request_id="req_01HX7Qaa",
            brief_id="brf_001",
            locale="ru",
            brief=BriefPayload(language="ru", text="Нужен бэкенд на Go", source_type="text"),
            taxonomy=ExtractTaxonomy(role_ids=["backend_engineer"], competency_ids=["lang_go"]),
            options=ExtractOptions(),
        )
        assert req.locale == "ru"
        assert req.options.max_requirements == 30


class TestProblem:
    def test_valid_problem(self):
        p = Problem(
            type="https://teamopt.internal/ml/problems/bad-request",
            title="Bad request",
            status=400,
            code="BAD_REQUEST",
            detail="Missing request_id",
            request_id="req_01HX7Qaa",
            retryable=False,
            reason_codes=["VALIDATION_FAILED"],
        )
        assert p.status == 400

    def test_invalid_code_pattern(self):
        with pytest.raises(ValidationError):
            Problem(
                type="https://example.com",
                title="t",
                status=400,
                code="bad_code",  # lowercase
                detail="d",
                request_id="req_01HX7Qaa",
                retryable=False,
                reason_codes=[],
            )


class TestFieldError:
    def test_valid(self):
        fe = FieldError(field="requirements[0].effort.max", code="EFFORT_RANGE_INVALID", detail="too small")
        assert fe.code == "EFFORT_RANGE_INVALID"
