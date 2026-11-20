"""Tests for ml.app.extract.rules — rule-based extraction engine."""

from __future__ import annotations

import pytest

from app.extract.rules import RulesEngine, _detect_criticality, _detect_level, _detect_headcount
from app.extract.taxonomy import get_registry, reset_registry


@pytest.fixture(autouse=True)
def _reset():
    reset_registry()
    yield
    reset_registry()


@pytest.fixture
def engine() -> RulesEngine:
    return RulesEngine(get_registry())


class TestLevelDetection:
    def test_junior(self):
        assert _detect_level("нужен junior разработчик") == 2

    def test_middle(self):
        assert _detect_level("уровень middle") == 3

    def test_senior(self):
        assert _detect_level("senior разработчик") == 4

    def test_lead(self):
        assert _detect_level("lead архитектор") == 4

    def test_principal(self):
        assert _detect_level("principal engineer") == 5

    def test_default(self):
        assert _detect_level("разработчик без указания уровня") == 3

    def test_djuunior(self):
        assert _detect_level("джуниор специалист") == 2

    def test_middl(self):
        assert _detect_level("мидл разработчик") == 3


class TestCriticalityDetection:
    def test_must(self):
        assert _detect_criticality("обязательно нужен Go", 1) == "must"

    def test_should(self):
        assert _detect_criticality("желательно React", 1) == "should"

    def test_nice(self):
        assert _detect_criticality("опционально Docker", 1) == "nice"

    def test_default_must(self):
        assert _detect_criticality("Go разработчик", 1) == "must"

    def test_default_should_zero_headcount(self):
        assert _detect_criticality("разработчик", 0) == "should"


class TestHeadcountDetection:
    def test_digit_before_role(self):
        assert _detect_headcount("2 бэкенда") == 2

    def test_team_size_not_used(self):
        """ADR-005: general team size is not used as per-competency headcount."""
        assert _detect_headcount("команда из 5 человек") == 1

    def test_default_one(self):
        assert _detect_headcount("разработчик") == 1


class TestRulesEngine:
    def test_single_role_competency(self, engine):
        text = "Бэкенд на Go (уровень middle)"
        reqs = engine.extract(text)
        assert len(reqs) >= 1
        # Should find backend_engineer + lang_go
        found_go = any(
            r.role.role_id == "backend_engineer" and r.competency
            and r.competency.competency_id == "lang_go"
            for r in reqs
        )
        assert found_go, f"Expected backend+Go, got: {[(r.role.role_id, r.competency.competency_id if r.competency else None) for r in reqs]}"

    def test_multiple_roles(self, engine):
        text = "Бэкенд на Go и фронтенд на React"
        reqs = engine.extract(text)
        role_ids = {r.role.role_id for r in reqs}
        assert "backend_engineer" in role_ids
        assert "frontend_engineer" in role_ids

    def test_headcount_extraction(self, engine):
        text = "3 бэкенд-разработчика (Go)"
        reqs = engine.extract(text)
        backend_reqs = [r for r in reqs if r.role.role_id == "backend_engineer"]
        assert len(backend_reqs) >= 1
        assert any(r.headcount == 3 for r in backend_reqs)

    def test_level_extraction(self, engine):
        text = "Go разработчик уровня senior"
        reqs = engine.extract(text)
        go_reqs = [r for r in reqs if r.competency and r.competency.competency_id == "lang_go"]
        assert len(go_reqs) >= 1
        assert any(r.level == 4 for r in go_reqs)

    def test_evidence_quote_verbatim(self, engine):
        text = "Нужен бэкенд на Go"
        reqs = engine.extract(text)
        for r in reqs:
            for ev in r.evidence:
                assert ev.quote in text, f"Quote '{ev.quote}' not in text"

    def test_unknown_role_no_hallucination(self, engine):
        text = "Нужен марсианский разработчик"
        reqs = engine.extract(text)
        # "марсианский" is not in taxonomy -> no role requirement should be emitted
        # (unless some substring matches a known synonym)
        for r in reqs:
            assert r.role.role_id != "mars_engineer"

    def test_deterministic_output(self, engine):
        text = "Бэкенд на Go, фронтенд на React, срок 8 недель"
        r1 = engine.extract(text)
        r2 = engine.extract(text)
        assert [r.requirement_id for r in r1] == [r.requirement_id for r in r2]

    def test_empty_text(self, engine):
        reqs = engine.extract("")
        assert reqs == []

    def test_postgresql_mention(self, engine):
        text = "База данных PostgreSQL"
        reqs = engine.extract(text)
        comp_ids = [r.competency.competency_id for r in reqs if r.competency]
        assert "db_postgresql" in comp_ids

    def test_kubernetes_mention(self, engine):
        text = "Нужен DevOps-инженер, Kubernetes"
        reqs = engine.extract(text)
        assert any(r.role.role_id == "devops_engineer" for r in reqs)

    def test_complex_brief_001(self, engine):
        text = (
            "Бэкенд на Go (уровень middle), REST API, PostgreSQL. "
            "Фронтенд на React + TypeScript. Playwright для E2E."
        )
        reqs = engine.extract(text)
        assert len(reqs) >= 3  # at least Go, React, PostgreSQL

    def test_all_roles_detectable(self, engine):
        """Each role should have at least one synonym detectable in a sample sentence."""
        reg = get_registry()
        for role_id, role in reg.roles.items():
            # Use the first synonym to test
            syn = role.synonyms[0] if role.synonyms else role.name_ru
            text = f"Нужен {syn}"
            reqs = engine.extract(text)
            role_ids = {r.role.role_id for r in reqs}
            assert role_id in role_ids, f"Role {role_id} not detected via synonym '{syn}'"

    def test_criticality_from_text(self, engine):
        text = "Желательно бизнес-аналитик"
        reqs = engine.extract(text)
        analyst_reqs = [r for r in reqs if "analyst" in r.role.role_id]
        assert len(analyst_reqs) >= 1
        assert analyst_reqs[0].criticality == "should"
