"""Tests for ml.app.extract.validator — hallucination gate, quote normalization, and 6-stage pipeline."""

from __future__ import annotations

import pytest

from app.extract.schemas import (
    EffortRange,
    EvidenceSpan,
    ExtractCompetency,
    ExtractedRequirement,
    ExtractRole,
)
from app.extract.taxonomy import get_registry, reset_registry
from app.extract.validator import EvidenceValidator, find_quote_in_text


@pytest.fixture(autouse=True)
def _reset():
    reset_registry()
    yield
    reset_registry()


@pytest.fixture
def validator() -> EvidenceValidator:
    return EvidenceValidator()


def _req(
    req_id: str = "req_ab12",
    role_id: str = "backend_engineer",
    comp_id: str | None = "lang_go",
    quote: str = "Go разработчик",
    char_start: int | None = None,
    char_end: int | None = None,
    confidence: float = 0.9,
) -> ExtractedRequirement:
    comp = (
        ExtractCompetency(
            competency_id=comp_id,
            name_ru="Go",
            match="llm",
            confidence=confidence,
            candidates=[],
        )
        if comp_id
        else None
    )
    return ExtractedRequirement(
        requirement_id=req_id,
        role=ExtractRole(
            role_id=role_id, name_ru="Бэкенд", match="llm", confidence=confidence
        ),
        competency=comp,
        level=3,
        criticality="must",
        headcount=1,
        effort=EffortRange(
            min_person_days=14,
            likely_person_days=20,
            max_person_days=28,
            unit="person_days",
        ),
        assumptions=["Timeline parsed: 8 weeks = 40 working days"],
        confidence=confidence,
        needs_review=False,
        evidence=[
            EvidenceSpan(
                kind="brief_span",
                quote=quote,
                char_start=char_start,
                char_end=char_end,
            )
        ],
    )


class TestEvidenceValidator:
    def test_valid_quote_kept(self, validator):
        text = "Нужен Go разработчик"
        reqs = [_req(quote="Go разработчик")]
        valid, dropped = validator.validate(reqs, text)
        assert len(valid) == 1
        assert dropped == 0

    def test_invalid_quote_dropped(self, validator):
        text = "Нужен Go разработчик"
        reqs = [_req(quote="FAKE QUOTE THAT IS NOT IN TEXT")]
        valid, dropped = validator.validate(reqs, text)
        assert len(valid) == 0
        assert dropped == 1

    def test_mixed_keeps_valid_drops_invalid(self, validator):
        text = "Нужен Go разработчик и React фронтенд"
        reqs = [
            _req(req_id="req_go01", quote="Go разработчик"),
            _req(req_id="req_fake01", quote="НЕ_СУЩЕСТВУЕТ"),
            _req(req_id="req_react01", role_id="frontend_engineer", comp_id="fw_react", quote="React фронтенд"),
        ]
        valid, dropped = validator.validate(reqs, text)
        assert len(valid) == 2
        assert dropped == 1
        assert {r.requirement_id for r in valid} == {"req_go01", "req_react01"}

    def test_char_offsets_corrected(self, validator):
        text = "Нужен Go разработчик"
        reqs = [_req(quote="Go разработчик", char_start=0, char_end=99)]
        valid, dropped = validator.validate(reqs, text)
        assert len(valid) == 1
        ev = valid[0].evidence[0]
        assert ev.char_start == 6
        assert ev.char_end == 6 + len("Go разработчик")

    def test_char_offsets_filled_when_missing(self, validator):
        text = "Нужен Go разработчик"
        reqs = [_req(quote="Go разработчик", char_start=None, char_end=None)]
        valid, dropped = validator.validate(reqs, text)
        assert len(valid) == 1
        ev = valid[0].evidence[0]
        assert ev.char_start == 6
        assert ev.char_end == 6 + len("Go разработчик")

    def test_dedup_same_id(self, validator):
        text = "Нужен Go разработчик"
        reqs = [
            _req(req_id="req_same01", quote="Go разработчик"),
            _req(req_id="req_same01", quote="Go разработчик"),
        ]
        valid, dropped = validator.validate(reqs, text)
        assert len(valid) == 1
        assert dropped == 1

    def test_empty_list(self, validator):
        valid, dropped = validator.validate([], "some text")
        assert valid == []
        assert dropped == 0

    def test_multiple_evidence_one_invalid(self, validator):
        text = "Нужен Go разработчик"
        req = _req(req_id="req_multi01", quote="Go разработчик")
        req.evidence.append(EvidenceSpan(kind="brief_span", quote="FAKE"))
        valid, dropped = validator.validate([req], text)
        assert len(valid) == 0
        assert dropped == 1


class TestQuoteNormalization:
    def test_yo_ye_normalization(self, validator):
        text = "Платёжный сервис для интернет-магазина"
        # LLM outputs 'е' instead of 'ё'
        reqs = [_req(quote="Платежный сервис для интернет-магазина")]
        valid, dropped = validator.validate(reqs, text)
        assert len(valid) == 1
        assert dropped == 0
        ev = valid[0].evidence[0]
        # Returned quote must be the exact literal slice of brief_text (with 'ё')
        assert ev.quote == "Платёжный сервис для интернет-магазина"
        assert text[ev.char_start : ev.char_end] == ev.quote

    def test_dash_and_nbsp_normalization(self, validator):
        text = "Срок\u00a0— 8  недель, проект «Альфа»"
        # LLM outputs hyphen, single spaces, and ASCII quotes
        found = find_quote_in_text('Срок - 8 недель, проект "Альфа"', text)
        assert found is not None
        orig_slice, cs, ce = found
        assert orig_slice == "Срок\u00a0— 8  недель, проект «Альфа»"
        assert text[cs:ce] == orig_slice


class TestSixStageValidationPipeline:
    def test_pipeline_all_six_checks(self, validator):
        registry = get_registry()
        text = (
            "Нужен бэкенд на Go, фронтенд на React, аналитик Python и ML Python. "
            "Также упомянут PostgreSQL."
        )
        items = [
            # 1. Invalid schema (not a valid ExtractedRequirement dict)
            {"bad_schema": True},
            # 2. Unknown taxonomy competency
            _req(req_id="req_unk", role_id="backend_engineer", comp_id="unknown_comp_xyz", quote="бэкенд на Go"),
            # 3. Quote not in text
            _req(req_id="req_noev", role_id="backend_engineer", comp_id="lang_go", quote="НЕТ В ТЕКСТЕ БРИФА"),
            # 4. Invalid role-competency pair (frontend_engineer not in lang_go.roles)
            _req(req_id="req_badpair", role_id="frontend_engineer", comp_id="lang_go", quote="бэкенд на Go"),
            # Valid 1: backend_engineer / lang_go
            _req(req_id="req_ok1", role_id="backend_engineer", comp_id="lang_go", quote="бэкенд на Go", confidence=0.95),
            # 5a. Duplicate exact (role_id, competency_id)
            _req(req_id="req_dup1", role_id="backend_engineer", comp_id="lang_go", quote="бэкенд на Go", confidence=0.90),
            # Valid 2: data_engineer / lang_python with quote "аналитик Python"
            _req(req_id="req_py1", role_id="data_engineer", comp_id="lang_python", quote="аналитик Python", confidence=0.90),
            # 5b. Same competency_id (lang_python) with different role (ml_engineer) but SAME quote -> dropped!
            _req(req_id="req_py_same_q", role_id="ml_engineer", comp_id="lang_python", quote="аналитик Python", confidence=0.88),
            # Valid 3: Same competency_id (lang_python) with different role (ml_engineer) and DIFFERENT quote -> kept!
            _req(req_id="req_py_diff_q", role_id="ml_engineer", comp_id="lang_python", quote="и ML Python", confidence=0.85),
            # 6a. Below min_confidence (0.4 < 0.55)
            _req(req_id="req_lowc", role_id="frontend_engineer", comp_id="fw_react", quote="фронтенд на React", confidence=0.40),
            # Valid 4 (will be trimmed by max_requirements=2): backend_engineer / db_postgresql
            _req(req_id="req_pg", role_id="backend_engineer", comp_id="db_postgresql", quote="упомянут PostgreSQL", confidence=0.75),
        ]

        valid, stats = validator.validate_pipeline(
            items,
            text,
            registry,
            min_confidence=0.55,
            max_requirements=3,
        )

        assert stats.dropped_schema_invalid == 1
        assert stats.dropped_unknown_taxonomy == 1
        assert stats.dropped_no_evidence == 1
        assert stats.dropped_invalid_pair == 1
        assert stats.dropped_duplicate == 2  # 1 exact pair dup + 1 same comp with identical quote
        assert stats.dropped_low_confidence == 1
        assert stats.dropped_over_limit == 1  # 4 passed confidence, capped at max_requirements=3

        assert len(valid) == 3
        kept_pairs = [(r.role.role_id, r.competency.competency_id) for r in valid]
        assert kept_pairs == [
            ("backend_engineer", "lang_go"),
            ("data_engineer", "lang_python"),
            ("ml_engineer", "lang_python"),
        ]


class TestValidatePartialQuotes:
    def test_check_individual_quotes(self, validator):
        text = "Go разработчик"
        results = validator.validate_partial_quotes(["Go разработчик", "FAKE"], text)
        assert results == [True, False]
