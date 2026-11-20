"""Tests for ml.app.extract.effort — timeline parsing and three-point estimation."""

from __future__ import annotations

import pytest

from app.extract.effort import EffortEstimator, PartialRequirement, parse_timeline
from app.extract.schemas import EvidenceSpan, ExtractCompetency, ExtractRole


@pytest.fixture
def estimator() -> EffortEstimator:
    return EffortEstimator()


def _partial(
    role_id: str = "backend_engineer",
    level: int = 3,
    comp_id: str | None = None,
) -> PartialRequirement:
    comp = None
    if comp_id:
        comp = ExtractCompetency(
            competency_id=comp_id,
            name_ru="Тест",
            match="exact_synonym",
            confidence=1.0,
            candidates=[],
        )
    return PartialRequirement(
        requirement_id="req_test",
        role=ExtractRole(role_id=role_id, name_ru="Тест", match="exact_synonym", confidence=1.0),
        competency=comp,
        level=level,
        criticality="must",
        headcount=1,
        evidence=[EvidenceSpan(kind="brief_span", quote="тестовая задача", char_start=0, char_end=15)],
        confidence=0.9,
    )


class TestParseTimeline:
    def test_8_weeks(self):
        t = parse_timeline("Срок — 8 недель")
        assert t is not None
        assert t.person_days == 40  # 8 * 5

    def test_3_months(self):
        t = parse_timeline("Срок — 3 месяца")
        assert t is not None
        assert t.person_days == 63  # 3 * 21 (gold-calibrated working month)

    def test_1_month(self):
        t = parse_timeline("1 месяц")
        assert t is not None
        assert t.person_days == 21

    def test_10_days(self):
        t = parse_timeline("10 рабочих дней")
        assert t is not None
        assert t.person_days == 10

    def test_5_months(self):
        t = parse_timeline("5 месяцев")
        assert t is not None
        assert t.person_days == 105  # 5 * 21

    def test_12_months(self):
        t = parse_timeline("12 месяцев")
        assert t is not None
        assert t.person_days == 252  # 12 * 21

    def test_no_timeline(self):
        t = parse_timeline("Нет сроков")
        assert t is None

    def test_english_weeks(self):
        t = parse_timeline("Duration: 4 weeks")
        assert t is not None
        assert t.person_days == 20

    def test_1_5_months(self):
        t = parse_timeline("1.5 месяца")
        assert t is not None
        assert t.person_days == 31.5  # 1.5 * 21


class TestEffortEstimator:
    def test_timeline_parsed_backend_go(self, estimator):
        p = _partial(comp_id="lang_go")
        effort = estimator.estimate(p, "Срок — 8 недель")
        # 8 weeks * 5 days = 40; backend Go calibrated weight = 1.7; level 3 factor = 1.0
        assert effort.likely_person_days == pytest.approx(68.0, abs=0.1)
        assert effort.min_person_days == pytest.approx(0.7 * effort.likely_person_days, abs=0.1)
        assert effort.max_person_days == pytest.approx(1.4 * effort.likely_person_days, abs=0.1)
        assert effort.unit == "person_days"

    def test_timeline_parsed_default_weight(self, estimator):
        p = _partial()  # no competency -> family default weight 0.5
        effort = estimator.estimate(p, "Срок — 8 недель")
        assert effort.likely_person_days == pytest.approx(20.0, abs=0.1)

    def test_three_point_range(self, estimator):
        p = _partial(comp_id="lang_go")
        effort = estimator.estimate(p, "3 месяца")
        assert effort.min_person_days < effort.likely_person_days < effort.max_person_days
        assert effort.min_person_days == pytest.approx(0.7 * effort.likely_person_days, abs=0.1)
        assert effort.max_person_days == pytest.approx(1.4 * effort.likely_person_days, abs=0.1)

    def test_no_timeline_default(self, estimator):
        p = _partial(comp_id="lang_go")
        effort = estimator.estimate(p, "Срок не указан")
        assert effort.likely_person_days > 0
        assert effort.min_person_days < effort.likely_person_days
        assert effort.max_person_days > effort.likely_person_days

    def test_headcount_not_multiplied(self, estimator):
        """Weights encode effective headcount; explicit headcount does not multiply effort."""
        p = _partial(comp_id="lang_go")
        p.headcount = 2
        effort = estimator.estimate(p, "8 недель, команда 2 человека")
        # Weight already includes typical effective headcount
        assert effort.likely_person_days == pytest.approx(68.0, abs=0.1)

    def test_level_adjustment(self, estimator):
        p_junior = _partial(comp_id="lang_go", level=2)
        p_middle = _partial(comp_id="lang_go", level=3)
        p_senior = _partial(comp_id="lang_go", level=4)
        ej = estimator.estimate(p_junior, "8 недель")
        em = estimator.estimate(p_middle, "8 недель")
        es = estimator.estimate(p_senior, "8 недель")
        # Level factor: 0.9 / 1.0 / 1.1, base = 40 * 1.7 = 68
        assert ej.likely_person_days == pytest.approx(68.0 * 0.9, abs=0.1)
        assert em.likely_person_days == pytest.approx(68.0, abs=0.1)
        assert es.likely_person_days == pytest.approx(68.0 * 1.1, abs=0.1)

    def test_assumption_added(self, estimator):
        p = _partial(comp_id="lang_go")
        estimator.estimate(p, "8 недель")
        assert len(p.assumptions) >= 1

    def test_bounds_clamped(self, estimator):
        """Effort values must be within [0.5, 2000]."""
        p = _partial(comp_id="lang_go")
        effort = estimator.estimate(p, "100 месяцев")
        assert effort.min_person_days >= 0.5
        assert effort.max_person_days <= 2000

    def test_different_roles(self, estimator):
        for role_id in ["backend_engineer", "qa_engineer", "devops_engineer", "business_analyst"]:
            p = _partial(role_id=role_id, comp_id="lang_go")
            effort = estimator.estimate(p, "No timeline")
            assert effort.likely_person_days > 0
            assert effort.min_person_days <= effort.likely_person_days <= effort.max_person_days

    def test_estimate_all_allocates(self, estimator):
        """estimate_all parses timeline once and returns one range per partial."""
        partials = [
            _partial(role_id="backend_engineer", comp_id="lang_go"),
            _partial(role_id="frontend_engineer", comp_id="fw_react"),
            _partial(role_id="qa_engineer", comp_id="test_playwright"),
        ]
        ranges = estimator.estimate_all(partials, "Срок — 8 недель")
        assert len(ranges) == 3
        # All should be based on the same 40-day timeline
        assert ranges[0].likely_person_days == pytest.approx(68.0, abs=0.1)  # backend Go weight 1.7
        assert ranges[1].likely_person_days == pytest.approx(50.8, abs=0.1)  # React weight 1.27
        assert ranges[2].likely_person_days == pytest.approx(28.4, abs=0.1)  # playwright weight 0.71
