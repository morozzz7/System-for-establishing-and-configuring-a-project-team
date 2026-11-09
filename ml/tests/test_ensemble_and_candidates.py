"""Tests for 3-run parallel ensemble union, candidate verification, role safety net, and headcount reconciliation."""

from __future__ import annotations

import json
import sys
from pathlib import Path

import pytest

from app.extract.candidates import (
    detect_headcount_for_role_near_quote,
    extract_role_headcounts_from_text,
    normalize_headcounts_by_role,
    scan_missing_roles,
    scan_unmatched_competencies,
)
from app.extract.llm_extract import llm_extract
from app.extract.taxonomy import get_registry, reset_registry
from app.extract.validator import EvidenceValidator, has_competency_lexical_anchor
from app.llm.client import LLMResponse
from app.llm.config import LLMSettings

EVAL_DIR = Path(__file__).resolve().parents[2] / "eval"
if str(EVAL_DIR) not in sys.path:
    sys.path.insert(0, str(EVAL_DIR))

from run_eval import compute_metrics, compute_pair_hit_rates  # noqa: E402


@pytest.fixture(autouse=True)
def _reset():
    reset_registry()
    yield
    reset_registry()


class _QueuedLLMClient:
    """Mock client returning queued LLMResponse objects in order."""

    def __init__(self, responses: list[LLMResponse]):
        self._responses = list(responses)
        self.calls: list[dict] = []

    async def chat(
        self,
        messages,
        temperature=0,
        seed=None,
        max_tokens=None,
        timeout_ms=None,
        response_format=None,
    ):
        self.calls.append(
            {
                "messages": list(messages),
                "seed": seed,
                "max_tokens": max_tokens,
                "timeout_ms": timeout_ms,
            }
        )
        idx = len(self.calls) - 1
        if idx < len(self._responses):
            return self._responses[idx]
        return LLMResponse(content="[]", tokens_in=50, tokens_out=2, model="test")


@pytest.mark.asyncio
class TestEnsembleUnionAndHeadcount:
    async def test_union_keeps_1_of_3_item_with_lower_confidence_and_review(self):
        """A pair found in only 1 of 3 parallel runs is kept (union), with lowered confidence and needs_review=True."""
        brief_text = (
            "Бэкенд на Go (middle), публичные методы через REST API, дашборды желательно строить в Grafana. "
            "Срок — 8 недель."
        )
        run1 = [
            {
                "role_id": "backend_engineer",
                "competency_id": "lang_go",
                "quote": "Бэкенд на Go",
                "level": 3,
                "criticality": "must",
                "confidence": 0.95,
            },
            {
                "role_id": "backend_engineer",
                "competency_id": "api_rest",
                "quote": "публичные методы через REST API",
                "level": 3,
                "criticality": "must",
                "confidence": 0.90,
            },
        ]
        run2 = [
            {
                "role_id": "backend_engineer",
                "competency_id": "lang_go",
                "quote": "Бэкенд на Go",
                "level": 3,
                "criticality": "must",
                "confidence": 0.95,
            },
            {
                "role_id": "backend_engineer",
                "competency_id": "api_rest",
                "quote": "публичные методы через REST API",
                "level": 3,
                "criticality": "must",
                "confidence": 0.90,
            },
        ]
        # Only run 3 finds obs_grafana
        run3 = [
            {
                "role_id": "backend_engineer",
                "competency_id": "lang_go",
                "quote": "Бэкенд на Go",
                "level": 3,
                "criticality": "must",
                "confidence": 0.95,
            },
            {
                "role_id": "devops_engineer",
                "competency_id": "obs_grafana",
                "quote": "дашборды желательно строить в Grafana",
                "level": 3,
                "criticality": "should",
                "confidence": 0.90,
            },
        ]

        client = _QueuedLLMClient(
            [
                LLMResponse(content=json.dumps(run1, ensure_ascii=False), tokens_in=100, tokens_out=40, model="test"),
                LLMResponse(content=json.dumps(run2, ensure_ascii=False), tokens_in=100, tokens_out=40, model="test"),
                LLMResponse(content=json.dumps(run3, ensure_ascii=False), tokens_in=100, tokens_out=40, model="test"),
            ]
        )
        registry = get_registry()
        settings = LLMSettings(api_key="test", max_retries=0)

        reqs, stats = await llm_extract(
            brief_text,
            "ru",
            registry,
            client,
            settings,
            min_confidence=0.55,
            ensemble_runs=3,
            verify_candidates=False,
            role_safety_net=False,
        )

        assert stats.ensemble_runs_completed == 3
        by_comp = {r.competency.competency_id: r for r in reqs if r.competency}
        assert set(by_comp.keys()) == {"lang_go", "api_rest", "obs_grafana"}

        # 3/3 votes -> full confidence
        assert by_comp["lang_go"].confidence == pytest.approx(0.95, abs=0.01)
        assert by_comp["lang_go"].needs_review is False

        # 2/3 votes -> 0.95 * 0.90 = 0.86
        assert by_comp["api_rest"].confidence == pytest.approx(0.86, abs=0.02)

        # 1/3 votes -> kept (>= 0.56 so min_confidence=0.55 does not drop it) with needs_review=True
        assert 0.56 <= by_comp["obs_grafana"].confidence <= 0.58
        assert by_comp["obs_grafana"].needs_review is True

    async def test_headcount_max_and_russian_word_reconciliation(self):
        """Headcount takes max across runs and Russian word numbers ('два бэкенд-разработчика'), setting needs_review=True."""
        brief_text = (
            "Ядро платформы пишется на Go, и для его разработки нужны два бэкенд-разработчика уровня senior. "
            "Срок — 9 месяцев."
        )
        # Run 1 outputs headcount=1, Run 2 outputs headcount=1, but sentence says "два бэкенд-разработчика"
        run1 = [
            {
                "role_id": "backend_engineer",
                "competency_id": "lang_go",
                "quote": "Ядро платформы пишется на Go",
                "level": 4,
                "criticality": "must",
                "headcount": 1,
                "confidence": 0.95,
            }
        ]
        run2 = [
            {
                "role_id": "backend_engineer",
                "competency_id": "lang_go",
                "quote": "Ядро платформы пишется на Go",
                "level": 4,
                "criticality": "must",
                "headcount": 1,
                "confidence": 0.95,
            }
        ]
        client = _QueuedLLMClient(
            [
                LLMResponse(content=json.dumps(run1, ensure_ascii=False), tokens_in=100, tokens_out=30, model="test"),
                LLMResponse(content=json.dumps(run2, ensure_ascii=False), tokens_in=100, tokens_out=30, model="test"),
            ]
        )
        registry = get_registry()
        settings = LLMSettings(api_key="test", max_retries=0)

        reqs, _ = await llm_extract(
            brief_text,
            "ru",
            registry,
            client,
            settings,
            ensemble_runs=2,
            verify_candidates=False,
            role_safety_net=False,
        )
        assert len(reqs) == 1
        assert reqs[0].headcount == 2
        assert reqs[0].needs_review is True


class TestCandidateVerificationAndRoleSafetyNet:
    def test_word_boundary_does_not_match_substrings(self):
        """Verify 'спринтами' does not trigger fw_spring ('спринг') and 'архитектура' does not trigger solution_architect."""
        registry = get_registry()
        text = "Руководитель проекта ведёт работу с двухнедельными спринтами. Событийная архитектура."
        cands = scan_unmatched_competencies(text, registry, set())
        cand_ids = {c.competency_id for c in cands}
        assert "fw_spring" not in cand_ids

        roles = scan_missing_roles(text, registry, {"project_manager"})
        role_ids = {r.role_id for r in roles}
        assert "solution_architect" not in role_ids

    @pytest.mark.asyncio
    async def test_candidate_scanner_confirms_missed_and_rejects_trap(self):
        """Word-boundary scanner finds missed 'Terraform' and trap 'RabbitMQ'; verification confirms Terraform and rejects RabbitMQ."""
        brief_text = (
            "Бэкенд на Go. DevOps-инженер описывает инфраструктуру в Terraform. "
            "Обсуждался вариант на RabbitMQ, но по итогам сравнения от него отказались. Срок — 6 месяцев."
        )
        # Primary pass extracts only lang_go
        primary_items = [
            {
                "role_id": "backend_engineer",
                "competency_id": "lang_go",
                "quote": "Бэкенд на Go",
                "level": 3,
                "criticality": "must",
                "confidence": 0.95,
            }
        ]
        # Verification call confirms iac_terraform and rejects mq_rabbitmq
        ver_items = [
            {
                "decision": "confirm",
                "role_id": "devops_engineer",
                "competency_id": "iac_terraform",
                "quote": "описывает инфраструктуру в Terraform",
                "level": 3,
                "criticality": "must",
                "headcount": 1,
                "confidence": 0.90,
            },
            {
                "decision": "reject",
                "role_id": "backend_engineer",
                "competency_id": "mq_rabbitmq",
            },
        ]
        client = _QueuedLLMClient(
            [
                LLMResponse(content=json.dumps(primary_items, ensure_ascii=False), tokens_in=100, tokens_out=30, model="test"),
                LLMResponse(content=json.dumps(ver_items, ensure_ascii=False), tokens_in=150, tokens_out=40, model="test"),
            ]
        )
        registry = get_registry()
        settings = LLMSettings(api_key="test", max_retries=0)

        reqs, stats = await llm_extract(
            brief_text,
            "ru",
            registry,
            client,
            settings,
            ensemble_runs=1,
            verify_candidates=True,
            role_safety_net=False,
        )

        pairs = {(r.role.role_id, r.competency.competency_id) for r in reqs if r.competency}
        assert pairs == {
            ("backend_engineer", "lang_go"),
            ("devops_engineer", "iac_terraform"),
        }
        assert stats.candidate_competencies_found >= 2
        assert stats.candidate_competencies_confirmed == 1
        assert stats.candidate_competencies_rejected == 1

    @pytest.mark.asyncio
    async def test_role_safety_net_recovers_missing_role_without_pair_fp_penalty(self):
        """Missing role in brief is recovered as role-only (competency=None, needs_review=True) and not penalized in pair F1."""
        brief_text = (
            "Бэкенд разрабатывается на Go. В команду также требуется бизнес-аналитик для работы со стейкхолдерами. "
            "Срок — 4 месяца."
        )
        primary_items = [
            {
                "role_id": "backend_engineer",
                "competency_id": "lang_go",
                "quote": "Бэкенд разрабатывается на Go",
                "level": 3,
                "criticality": "must",
                "confidence": 0.95,
            }
        ]
        # Verification call returns empty array -> fallback emits role-only business_analyst
        client = _QueuedLLMClient(
            [
                LLMResponse(content=json.dumps(primary_items, ensure_ascii=False), tokens_in=100, tokens_out=30, model="test"),
                LLMResponse(content="[]", tokens_in=100, tokens_out=5, model="test"),
            ]
        )
        registry = get_registry()
        settings = LLMSettings(api_key="test", max_retries=0)

        reqs, stats = await llm_extract(
            brief_text,
            "ru",
            registry,
            client,
            settings,
            ensemble_runs=1,
            verify_candidates=False,
            role_safety_net=True,
        )

        assert stats.missing_roles_found == 1
        assert stats.missing_roles_recovered == 1
        roles = {r.role.role_id for r in reqs}
        assert roles == {"backend_engineer", "business_analyst"}

        ba_req = next(r for r in reqs if r.role.role_id == "business_analyst")
        assert ba_req.competency is None
        assert ba_req.needs_review is True

        # Verify compute_metrics: role recall = 1.0, and role-only is NOT counted as FP in pair metric
        gold = [
            {
                "role": {"role_id": "backend_engineer"},
                "competency": {"competency_id": "lang_go"},
                "headcount": 1,
            },
            {
                "role": {"role_id": "business_analyst"},
                "competency": {"competency_id": "an_requirements"},
                "headcount": 1,
            },
        ]
        m = compute_metrics(gold, reqs, registry, brief_text)
        assert m["role"]["recall"] == 1.0
        assert m["role"]["f1"] == 1.0
        assert m["role_only_count"] == 1
        assert m["pair"]["fp"] == 0  # role-only is NOT penalized as pair FP!

    def test_compute_pair_hit_rates(self):
        gold = [
            {"role": {"role_id": "backend_engineer"}, "competency": {"competency_id": "lang_go"}},
            {"role": {"role_id": "devops_engineer"}, "competency": {"competency_id": "obs_grafana"}},
        ]
        run1 = [{"role_id": "backend_engineer", "competency_id": "lang_go"}]
        run2 = [
            {"role_id": "backend_engineer", "competency_id": "lang_go"},
            {"role_id": "devops_engineer", "competency_id": "obs_grafana"},
        ]
        rows = compute_pair_hit_rates(gold, [run1, run2])
        by_comp = {r["competency_id"]: r for r in rows}
        assert by_comp["lang_go"]["hit_rate"] == 1.0
        assert by_comp["obs_grafana"]["hit_rate"] == 0.5

    @pytest.mark.asyncio
    async def test_compact_schema_allows_null_competency_id(self):
        """Compact item with competency_id: null is parsed as competency=None, needs_review=True."""
        brief_text = "Дизайн интерфейсов делает UX/UI-дизайнер, он подключается на первом этапе. Срок — 7 месяцев."
        items = [
            {
                "role_id": "ux_ui_designer",
                "competency_id": None,
                "quote": "Дизайн интерфейсов делает UX/UI-дизайнер",
                "level": 3,
                "criticality": "must",
                "headcount": 1,
                "confidence": 0.9,
            }
        ]
        client = _QueuedLLMClient(
            [LLMResponse(content=json.dumps(items, ensure_ascii=False), tokens_in=100, tokens_out=30, model="test")]
        )
        registry = get_registry()
        settings = LLMSettings(api_key="test", max_retries=0)

        reqs, _ = await llm_extract(
            brief_text,
            "ru",
            registry,
            client,
            settings,
            ensemble_runs=1,
            verify_candidates=False,
            role_safety_net=False,
        )
        assert len(reqs) == 1
        assert reqs[0].role.role_id == "ux_ui_designer"
        assert reqs[0].competency is None
        assert reqs[0].needs_review is True

    @pytest.mark.asyncio
    async def test_lexical_competency_check_lowers_confidence_without_deleting(self):
        """When neither competency name nor synonyms appear near quote, item is kept with needs_review=True and lowered confidence."""
        brief_text = (
            "На этапе обследования работает бизнес-аналитик. "
            "На втором этапе в команду подключается тестировщик. Срок — 7 месяцев."
        )
        items = [
            {
                "role_id": "business_analyst",
                "competency_id": "an_requirements",
                "quote": "На этапе обследования работает бизнес-аналитик",
                "level": 3,
                "criticality": "must",
                "confidence": 0.95,
                "needs_review": False,
            },
            {
                "role_id": "qa_engineer",
                "competency_id": "qa_manual",
                "quote": "На втором этапе в команду подключается тестировщик",
                "level": 3,
                "criticality": "must",
                "confidence": 0.95,
                "needs_review": False,
            },
        ]
        client = _QueuedLLMClient(
            [LLMResponse(content=json.dumps(items, ensure_ascii=False), tokens_in=100, tokens_out=40, model="test")]
        )
        registry = get_registry()
        settings = LLMSettings(api_key="test", max_retries=0)

        reqs, stats = await llm_extract(
            brief_text,
            "ru",
            registry,
            client,
            settings,
            min_confidence=0.55,
            ensemble_runs=1,
            verify_candidates=False,
            role_safety_net=False,
        )
        # Neither item is deleted! Both are flagged and have lowered confidence.
        assert len(reqs) == 2
        assert stats.flagged_no_lexical_anchor == 2
        for r in reqs:
            assert r.needs_review is True
            assert 0.55 <= r.confidence <= 0.58
            assert any("Lexical check" in a for a in r.assumptions)

    @pytest.mark.asyncio
    async def test_headcount_normalized_by_role_in_code(self):
        """Code normalizes headcount per role across all requirements of that role (max among role mentions in text)."""
        brief_text = (
            "Веб-кабинет разрабатывают трое фронтенд-разработчиков на React. "
            "Код фронтенда пишется на TypeScript. "
            "Серверную часть пара бэкенд-разработчиков уровня senior пишет на Java. "
            "Основной фреймворк — Spring Boot, бэкенд-разработчики работают в нём. "
            "Проектом руководит один руководитель проекта по Kanban."
        )
        # Model outputs inconsistent/wrong headcounts (1 on TypeScript, 1 on Spring, 5 on PM)
        items = [
            {"role_id": "frontend_engineer", "competency_id": "fw_react", "quote": "трое фронтенд-разработчиков на React", "headcount": 3},
            {"role_id": "frontend_engineer", "competency_id": "lang_typescript", "quote": "Код фронтенда пишется на TypeScript", "headcount": 1},
            {"role_id": "backend_engineer", "competency_id": "lang_java", "quote": "пара бэкенд-разработчиков уровня senior пишет на Java", "headcount": 1},
            {"role_id": "backend_engineer", "competency_id": "fw_spring", "quote": "Основной фреймворк — Spring Boot", "headcount": 1},
            {"role_id": "project_manager", "competency_id": "proc_kanban", "quote": "один руководитель проекта по Kanban", "headcount": 5},
        ]
        client = _QueuedLLMClient(
            [LLMResponse(content=json.dumps(items, ensure_ascii=False), tokens_in=100, tokens_out=60, model="test")]
        )
        registry = get_registry()
        settings = LLMSettings(api_key="test", max_retries=0)

        reqs, _ = await llm_extract(
            brief_text,
            "ru",
            registry,
            client,
            settings,
            ensemble_runs=1,
            verify_candidates=False,
            role_safety_net=False,
        )
        by_comp = {r.competency.competency_id: r for r in reqs if r.competency}
        assert by_comp["fw_react"].headcount == 3
        assert by_comp["lang_typescript"].headcount == 3
        assert by_comp["lang_java"].headcount == 2
        assert by_comp["fw_spring"].headcount == 2
        assert by_comp["proc_kanban"].headcount == 1

    @pytest.mark.asyncio
    async def test_verification_step_logs_confirmed_rejected_and_skipped(self):
        """Verification step accounts for 100% of found candidates: confirmed + rejected + skipped == found."""
        brief_text = (
            "Бэкенд на Go. Инфраструктура в Terraform. "
            "Шина на RabbitMQ отключается. "
            "Скрипты выгрузки на Python пишут аналитики заказчика. Срок — 5 месяцев."
        )
        primary_items = [
            {"role_id": "backend_engineer", "competency_id": "lang_go", "quote": "Бэкенд на Go"}
        ]
        # Scanner finds iac_terraform, mq_rabbitmq, lang_python (3 candidates).
        # LLM confirms iac_terraform, rejects mq_rabbitmq, and omits lang_python!
        ver_items = [
            {
                "decision": "confirm",
                "role_id": "devops_engineer",
                "competency_id": "iac_terraform",
                "quote": "Инфраструктура в Terraform",
                "reason": "Needed by DevOps",
            },
            {
                "decision": "reject",
                "role_id": "backend_engineer",
                "competency_id": "mq_rabbitmq",
                "reason": "Decommissioned after migration",
            },
        ]
        client = _QueuedLLMClient(
            [
                LLMResponse(content=json.dumps(primary_items, ensure_ascii=False), tokens_in=100, tokens_out=30, model="test"),
                LLMResponse(content=json.dumps(ver_items, ensure_ascii=False), tokens_in=150, tokens_out=50, model="test"),
            ]
        )
        registry = get_registry()
        settings = LLMSettings(api_key="test", max_retries=0)

        reqs, stats = await llm_extract(
            brief_text,
            "ru",
            registry,
            client,
            settings,
            ensemble_runs=1,
            verify_candidates=True,
            role_safety_net=False,
        )
        assert stats.candidate_competencies_found == 3
        assert stats.candidate_competencies_confirmed == 1
        assert stats.candidate_competencies_rejected == 1
        assert stats.candidate_competencies_skipped == 1
        assert (
            stats.candidate_competencies_found
            == stats.candidate_competencies_confirmed
            + stats.candidate_competencies_rejected
            + stats.candidate_competencies_skipped
        )
        # ADR-006 §1: batching/retry accounting is observable and consistent.
        assert stats.verification_attempts == 1
        assert stats.verification_failed_batches == 0
        log_by_id = {e["id"]: e for e in stats.verification_log}
        assert log_by_id["iac_terraform"]["status"] == "confirmed"
        assert log_by_id["mq_rabbitmq"]["status"] == "rejected"
        assert log_by_id["lang_python"]["status"] == "skipped"
        assert "omitted by LLM" in log_by_id["lang_python"]["reason"]

