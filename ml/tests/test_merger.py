"""Tests for the merger: dedup, conflict resolution, determinism."""

from __future__ import annotations

import pytest

from app.extract.merger import Merger
from app.extract.schemas import (
    EffortRange,
    EvidenceSpan,
    ExtractCompetency,
    ExtractRole,
    ExtractedRequirement,
)


def _make_req(
    req_id: str,
    role_id: str,
    comp_id: str | None,
    confidence: float,
    evidence_quote: str = "some evidence quote",
) -> ExtractedRequirement:
    """Helper to build an ExtractedRequirement for testing."""
    role = ExtractRole(role_id=role_id, name_ru=f"Role {role_id}", match="exact_synonym", confidence=confidence)
    comp = None
    if comp_id:
        comp = ExtractCompetency(
            competency_id=comp_id, name_ru=f"Comp {comp_id}",
            match="exact_synonym", confidence=confidence, candidates=[],
        )
    # requirement_id must match ^[A-Za-z0-9_-]{4,40}$
    safe_id = req_id if len(req_id) >= 4 else f"req_{req_id}"
    return ExtractedRequirement(
        requirement_id=safe_id,
        role=role,
        competency=comp,
        level=3,
        criticality="must",
        headcount=1,
        effort=EffortRange(min_person_days=10, likely_person_days=20, max_person_days=30),
        assumptions=["test assumption"],
        confidence=confidence,
        needs_review=False,
        evidence=[EvidenceSpan(kind="brief_span", quote=evidence_quote, char_start=0, char_end=len(evidence_quote))],
    )


class TestMergerUnion:
    def test_disjoint_sets_all_kept(self):
        rules = [_make_req("r1", "backend_engineer", "lang_go", 0.9)]
        llm = [_make_req("r2", "frontend_engineer", "lib_react", 0.8)]
        result = Merger().merge(rules, llm)
        assert len(result) == 2

    def test_empty_rules_returns_llm(self):
        llm = [_make_req("r1", "backend_engineer", "lang_go", 0.8)]
        result = Merger().merge([], llm)
        assert len(result) == 1
        assert result[0].requirement_id == "req_r1"

    def test_empty_llm_returns_rules(self):
        rules = [_make_req("r1", "backend_engineer", "lang_go", 0.9)]
        result = Merger().merge(rules, [])
        assert len(result) == 1
        assert result[0].requirement_id == "req_r1"

    def test_empty_both_returns_empty(self):
        result = Merger().merge([], [])
        assert result == []


class TestMergerDedup:
    def test_same_pair_rules_wins_even_if_lower_confidence(self):
        """ADR-005: rules requirement is kept, LLM evidence is merged in."""
        rules = [_make_req("r1", "backend_engineer", "lang_go", 0.8, "rules evidence")]
        llm = [_make_req("r2", "backend_engineer", "lang_go", 0.95, "llm evidence")]
        result = Merger().merge(rules, llm)
        assert len(result) == 1
        # Rules requirement wins; LLM evidence is appended
        assert result[0].evidence[0].quote == "rules evidence"
        assert any(ev.quote == "llm evidence" for ev in result[0].evidence)
        assert result[0].confidence == 0.8

    def test_same_pair_equal_confidence_rules_wins(self):
        rules = [_make_req("r1", "backend_engineer", "lang_go", 0.9, "rules evidence")]
        llm = [_make_req("r2", "backend_engineer", "lang_go", 0.9, "llm evidence")]
        result = Merger().merge(rules, llm)
        assert len(result) == 1
        # Rules wins on tie
        assert result[0].evidence[0].quote == "rules evidence"

    def test_role_only_dedup(self):
        """Same (role_id, None) pair should dedup."""
        rules = [_make_req("r1", "backend_engineer", None, 0.9)]
        llm = [_make_req("r2", "backend_engineer", None, 0.8)]
        result = Merger().merge(rules, llm)
        assert len(result) == 1


class TestMergerMaxRequirements:
    def test_trim_lowest_confidence(self):
        rules = [
            _make_req("r1", "backend_engineer", "lang_go", 0.9),
            _make_req("r2", "frontend_engineer", "lib_react", 0.5),
            _make_req("r3", "qa_engineer", "tool_selenium", 0.7),
        ]
        result = Merger().merge(rules, [], max_requirements=2)
        assert len(result) == 2
        # Lowest confidence (0.5) should be dropped
        confidences = {r.confidence for r in result}
        assert 0.5 not in confidences
        assert 0.9 in confidences
        assert 0.7 in confidences


class TestMergerDeterminism:
    def test_same_input_same_output(self):
        rules = [
            _make_req("r1", "backend_engineer", "lang_go", 0.9),
            _make_req("r2", "frontend_engineer", "lib_react", 0.8),
        ]
        llm = [
            _make_req("r3", "qa_engineer", "tool_selenium", 0.7),
        ]
        m = Merger()
        result1 = m.merge(rules, llm)
        result2 = m.merge(rules, llm)
        assert len(result1) == len(result2)
        for a, b in zip(result1, result2):
            assert a.requirement_id == b.requirement_id
            assert a.role.role_id == b.role.role_id
            assert a.confidence == b.confidence

    def test_sorted_output(self):
        """Output should be sorted by (role_id, competency_id)."""
        rules = [
            _make_req("r1", "qa_engineer", "tool_selenium", 0.7),
            _make_req("r2", "backend_engineer", "lang_go", 0.9),
            _make_req("r3", "frontend_engineer", "lib_react", 0.8),
        ]
        result = Merger().merge(rules, [])
        keys = [(r.role.role_id, r.competency.competency_id if r.competency else "") for r in result]
        assert keys == sorted(keys)


class TestMergerPreserveFields:
    def test_rules_evidence_preserved_llm_evidence_merged(self):
        rules = [_make_req("r1", "backend_engineer", "lang_go", 0.8, "rules evidence text")]
        llm = [_make_req("r2", "backend_engineer", "lang_go", 0.95, "llm evidence text")]
        result = Merger().merge(rules, llm)
        # Rules evidence is first, LLM evidence is merged in
        assert result[0].evidence[0].quote == "rules evidence text"
        assert any(ev.quote == "llm evidence text" for ev in result[0].evidence)

    def test_rules_effort_preserved(self):
        """ADR-005: rules effort is preserved; LLM effort is ignored on conflict."""
        r1 = _make_req("r1", "backend_engineer", "lang_go", 0.8)
        r1_updated = r1.model_copy(update={
            "effort": EffortRange(min_person_days=5, likely_person_days=10, max_person_days=15),
        })
        r2 = _make_req("r2", "backend_engineer", "lang_go", 0.95)
        r2_updated = r2.model_copy(update={
            "effort": EffortRange(min_person_days=50, likely_person_days=100, max_person_days=150),
        })
        result = Merger().merge([r1_updated], [r2_updated])
        assert result[0].effort.likely_person_days == 10

    def test_llm_only_key_added(self):
        """New (role, comp) keys from LLM are added when they pass validation."""
        rules = [_make_req("r1", "backend_engineer", "lang_go", 0.9)]
        llm = [_make_req("r2", "security_engineer", "sec_appsec", 0.7)]
        result = Merger().merge(rules, llm)
        assert len(result) == 2
        keys = {(r.role.role_id, r.competency.competency_id if r.competency else "") for r in result}
        assert ("backend_engineer", "lang_go") in keys
        assert ("security_engineer", "sec_appsec") in keys
