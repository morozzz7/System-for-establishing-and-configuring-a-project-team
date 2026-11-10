"""ADR-006 tests: batched verification, review routing, generic-mention gate, scanner precision.

All tests are deterministic and offline: the LLM client is a queued mock, no network.
"""

from __future__ import annotations

import json
from pathlib import Path

import pytest

import app.extract.llm_extract as llm_extract_mod
from app.extract.candidates import (
    CompetencyCandidateMention,
    RoleMention,
    scan_unmatched_competencies,
)
from app.extract.diagnostics import build_diagnostics
from app.extract.llm_extract import _verification_budget, llm_extract
from app.extract.mentions import is_generic_mention
from app.extract.review import REVIEW_CONFIDENCE_MAX, split_for_review
from app.extract.schemas import (
    CompetencyCandidate,
    EvidenceSpan,
    EffortRange,
    ExtractCompetency,
    ExtractDiagnostics,
    ExtractResponse,
    ExtractRole,
    ExtractedRequirement,
    ExtractVerification,
)
from app.extract.taxonomy import get_registry, reset_registry
from app.extract.validator import EvidenceValidator
from app.llm.client import (
    LLMErrorResponse,
    LLMResponse,
    LLMTimeoutError,
    LLMUnavailableError,
)
from app.llm.config import LLMSettings

REPO_ROOT = Path(__file__).resolve().parents[2]
LONG_BRIEF2 = REPO_ROOT / "eval" / "long_brief2" / "brief.md"
LONG_BRIEF2_GOLD = REPO_ROOT / "eval" / "long_brief2" / "gold.json"
GOLD_DIR = REPO_ROOT / "eval" / "gold"

_GENERIC_PROSE_FPS = {"dom_telecom", "an_user_stories", "test_performance"}


@pytest.fixture(autouse=True)
def _reset():
    reset_registry()
    yield
    reset_registry()


class _QueuedClient:
    """Mock LLM client returning queued responses and recording call kwargs."""

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
                "max_tokens": max_tokens,
                "seed": seed,
            }
        )
        idx = len(self.calls) - 1
        if idx < len(self._responses):
            return self._responses[idx]
        return LLMResponse(content="[]", tokens_in=10, tokens_out=2, model="test")


def _primary_go_item() -> dict:
    return {
        "role_id": "backend_engineer",
        "competency_id": "lang_go",
        "quote": "Бэкенд на Go",
        "level": 3,
        "criticality": "must",
        "confidence": 0.95,
    }


def _settings(max_retries: int = 1) -> LLMSettings:
    return LLMSettings(api_key="test", max_retries=max_retries)


def _requirement(
    *,
    role_id: str = "devops_engineer",
    competency_id: str | None = "cloud_docker",
    quote: str,
    confidence: float = 0.9,
    needs_review: bool = False,
    requirement_id: str = "req_adr00601",
) -> ExtractedRequirement:
    comp = None
    if competency_id is not None:
        comp = ExtractCompetency(
            competency_id=competency_id,
            name_ru=competency_id,
            match="llm",
            confidence=confidence,
            candidates=[CompetencyCandidate(competency_id=competency_id, score=confidence)],
        )
    return ExtractedRequirement(
        requirement_id=requirement_id,
        role=ExtractRole(
            role_id=role_id, name_ru=role_id, match="llm", confidence=confidence
        ),
        competency=comp,
        level=3,
        criticality="must",
        headcount=1,
        effort=EffortRange(
            min_person_days=5, likely_person_days=8, max_person_days=12, unit="person_days"
        ),
        assumptions=["ADR-006 test fixture"],
        confidence=confidence,
        needs_review=needs_review,
        evidence=[EvidenceSpan(kind="brief_span", quote=quote)],
    )


# ---------------------------------------------------------------------------
# 1. Verification: retry, batching/budget, missing decision, accounting
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
class TestVerificationRobustness:
    async def test_empty_response_is_retried_and_candidate_resolved(self):
        """Empty body with finish_reason=length -> retry with a larger budget -> confirmed."""
        brief = "Бэкенд на Go. DevOps-инженер описывает инфраструктуру в Terraform. Срок — 6 месяцев."
        confirm = [
            {
                "decision": "confirm",
                "role_id": "devops_engineer",
                "competency_id": "iac_terraform",
                "quote": "описывает инфраструктуру в Terraform",
                "level": 3,
                "criticality": "must",
                "headcount": 1,
                "confidence": 0.9,
            }
        ]
        client = _QueuedClient(
            [
                LLMResponse(
                    content=json.dumps([_primary_go_item()], ensure_ascii=False),
                    tokens_in=100,
                    tokens_out=20,
                    model="test",
                ),
                LLMResponse(
                    content="",
                    tokens_in=120,
                    tokens_out=0,
                    model="test",
                    finish_reason="length",
                ),
                LLMResponse(
                    content=json.dumps(confirm, ensure_ascii=False),
                    tokens_in=140,
                    tokens_out=30,
                    model="test",
                    finish_reason="stop",
                ),
            ]
        )

        reqs, stats = await llm_extract(
            brief,
            "ru",
            get_registry(),
            client,
            _settings(max_retries=1),
            ensemble_runs=1,
            verify_candidates=True,
            role_safety_net=False,
        )

        pairs = {(r.role.role_id, r.competency.competency_id) for r in reqs if r.competency}
        assert ("devops_engineer", "iac_terraform") in pairs
        assert stats.candidate_competencies_confirmed == 1
        assert stats.candidate_competencies_skipped == 0
        assert stats.verification_attempts == 2
        assert stats.verification_failed_batches == 0

        # Retry raised the per-batch budget: 1024 -> 1536.
        assert client.calls[1]["max_tokens"] == 1024
        assert client.calls[2]["max_tokens"] == 1536
        # The nudge asks for ONLY the JSON array.
        retry_messages = client.calls[2]["messages"]
        assert "Return ONLY a JSON array" in retry_messages[-1]["content"]
        # An empty assistant turn is never appended before the nudge.
        assert all(
            m["content"].strip()
            for m in retry_messages
            if m["role"] == "assistant"
        )

    async def test_fourteen_candidates_are_split_into_two_budgeted_calls(
        self, monkeypatch
    ):
        """14 candidates -> 2 verification calls; each budget follows 256 + 192*n_items."""
        brief = "Бэкенд на Go. Срок — 6 месяцев."
        registry = get_registry()
        fake_ids = [
            cid
            for cid in sorted(registry.competencies)
            if registry.competencies[cid].roles and cid != "lang_go"
        ][:14]
        assert len(fake_ids) == 14
        fake_cands = [
            CompetencyCandidateMention(
                competency_id=cid,
                name_ru=registry.competencies[cid].name_ru,
                matched_text=registry.competencies[cid].name_ru,
                quote_sentence=f"mention of {cid}",
                allowed_roles=list(registry.competencies[cid].roles),
            )
            for cid in fake_ids
        ]
        monkeypatch.setattr(
            llm_extract_mod,
            "scan_unmatched_competencies",
            lambda *args, **kwargs: fake_cands,
        )

        def _rejects(items: list[str]) -> str:
            return json.dumps(
                [{"decision": "reject", "competency_id": cid} for cid in items],
                ensure_ascii=False,
            )

        client = _QueuedClient(
            [
                LLMResponse(
                    content=json.dumps([_primary_go_item()], ensure_ascii=False),
                    tokens_in=100,
                    tokens_out=20,
                    model="test",
                ),
                LLMResponse(content=_rejects(fake_ids[:8]), tokens_in=100, tokens_out=20, model="test"),
                LLMResponse(content=_rejects(fake_ids[8:]), tokens_in=100, tokens_out=20, model="test"),
            ]
        )

        _reqs, stats = await llm_extract(
            brief,
            "ru",
            registry,
            client,
            _settings(max_retries=0),
            ensemble_runs=1,
            verify_candidates=True,
            role_safety_net=False,
        )

        verification_calls = client.calls[1:]
        assert len(verification_calls) == 2
        assert stats.verification_attempts == 2
        assert stats.verification_failed_batches == 0
        assert stats.candidate_competencies_found == 14
        assert stats.candidate_competencies_rejected == 14
        assert stats.candidate_competencies_skipped == 0

        for call, n_items in zip(verification_calls, (8, 6), strict=True):
            budget = call["max_tokens"]
            assert budget >= 256 + 192 * n_items
            assert budget <= 8192
            assert budget == _verification_budget(n_items)

    async def test_missing_decision_is_skipped_not_confirmed(self):
        """A verification object without `decision` must be skipped, never default-confirmed."""
        brief = "Бэкенд на Go. DevOps-инженер описывает инфраструктуру в Terraform. Срок — 6 месяцев."
        no_decision = [
            {
                "role_id": "devops_engineer",
                "competency_id": "iac_terraform",
                "quote": "описывает инфраструктуру в Terraform",
            }
        ]
        client = _QueuedClient(
            [
                LLMResponse(
                    content=json.dumps([_primary_go_item()], ensure_ascii=False),
                    tokens_in=100,
                    tokens_out=20,
                    model="test",
                ),
                LLMResponse(
                    content=json.dumps(no_decision, ensure_ascii=False),
                    tokens_in=100,
                    tokens_out=20,
                    model="test",
                ),
            ]
        )

        reqs, stats = await llm_extract(
            brief,
            "ru",
            get_registry(),
            client,
            _settings(max_retries=0),
            ensemble_runs=1,
            verify_candidates=True,
            role_safety_net=False,
        )

        pairs = {(r.role.role_id, r.competency.competency_id) for r in reqs if r.competency}
        assert ("devops_engineer", "iac_terraform") not in pairs
        log_by_id = {e["id"]: e for e in stats.verification_log}
        assert log_by_id["iac_terraform"]["status"] == "skipped"
        assert log_by_id["iac_terraform"]["reason"] == "missing decision field"
        assert stats.candidate_competencies_confirmed == 0

    async def test_failed_batch_marks_candidates_skipped_with_finish_reason(self):
        """A batch that still fails after retries is skipped, counted, and extraction continues."""
        brief = "Бэкенд на Go. DevOps-инженер описывает инфраструктуру в Terraform. Срок — 6 месяцев."
        client = _QueuedClient(
            [
                LLMResponse(
                    content=json.dumps([_primary_go_item()], ensure_ascii=False),
                    tokens_in=100,
                    tokens_out=20,
                    model="test",
                ),
                LLMResponse(
                    content="", tokens_in=120, tokens_out=0, model="test", finish_reason="length"
                ),
                LLMResponse(
                    content="", tokens_in=160, tokens_out=0, model="test", finish_reason="length"
                ),
            ]
        )

        reqs, stats = await llm_extract(
            brief,
            "ru",
            get_registry(),
            client,
            _settings(max_retries=1),
            ensemble_runs=1,
            verify_candidates=True,
            role_safety_net=False,
        )

        # Extraction is not aborted: the primary pass survives.
        assert any(
            r.competency is not None and r.competency.competency_id == "lang_go" for r in reqs
        )
        assert stats.verification_failed_batches == 1
        assert stats.verification_attempts == 2
        assert stats.candidate_competencies_skipped == 1
        log_by_id = {e["id"]: e for e in stats.verification_log}
        reason = log_by_id["iac_terraform"]["reason"]
        assert "finish_reason=length" in reason
        assert "max_tokens=" in reason
        assert (
            stats.candidate_competencies_found
            == stats.candidate_competencies_confirmed
            + stats.candidate_competencies_rejected
            + stats.candidate_competencies_skipped
        )


# ---------------------------------------------------------------------------
# 2. Review routing
# ---------------------------------------------------------------------------


class TestReviewRouting:
    def test_split_for_review_threshold_and_role_only(self):
        low_conf_pair = _requirement(
            quote="Дизайн интерфейсов делает UX/UI-дизайнер",
            role_id="ux_ui_designer",
            competency_id="ui_figma",
            confidence=0.56,
            needs_review=True,
            requirement_id="req_lowconf1",
        )
        role_only = _requirement(
            quote="На этапе обследования работает бизнес-аналитик",
            role_id="business_analyst",
            competency_id=None,
            confidence=0.58,
            needs_review=True,
            requirement_id="req_roleonly",
        )
        high_conf = _requirement(
            quote="Контейнеризация — разумеется, Docker, как у всех",
            confidence=0.80,
            needs_review=True,
            requirement_id="req_highconf",
        )

        main, review = split_for_review([low_conf_pair, role_only, high_conf])

        assert [r.requirement_id for r in review] == ["req_lowconf1"]
        assert [r.requirement_id for r in main] == ["req_roleonly", "req_highconf"]
        assert REVIEW_CONFIDENCE_MAX == 0.60

    def test_boundary_confidence_060_goes_to_review(self):
        boundary = _requirement(
            quote="Дизайн интерфейсов делает UX/UI-дизайнер",
            role_id="ux_ui_designer",
            competency_id="ui_figma",
            confidence=0.60,
            needs_review=True,
            requirement_id="req_boundary",
        )
        main, review = split_for_review([boundary])
        assert main == []
        assert [r.requirement_id for r in review] == ["req_boundary"]


# ---------------------------------------------------------------------------
# 3. Deterministic generic-mention gate
# ---------------------------------------------------------------------------


class TestGenericMentionGate:
    def test_passing_remark_is_generic(self):
        assert is_generic_mention("Контейнеризация — разумеется, Docker, как у всех") is True

    def test_marker_with_task_cue_is_not_generic(self):
        assert (
            is_generic_mention("Разумеется, DevOps-инженер настраивает Docker в кластере")
            is False
        )

    def test_generic_stack_line_is_generic(self):
        assert is_generic_mention("Стек: Python, Vue.js") is True

    def test_plain_task_sentence_is_not_generic(self):
        assert is_generic_mention("Инфраструктура описывается кодом на Terraform") is False

    def test_validator_drops_generic_requirement_and_counts_it(self):
        quote = "Контейнеризация — разумеется, Docker, как у всех"
        brief = f"Развёртывание. {quote}. Дашборды собираем в Grafana."
        req = _requirement(quote=quote, confidence=0.90)

        valid, stats = EvidenceValidator().validate_pipeline(
            [req], brief, get_registry(), min_confidence=0.0
        )

        assert valid == []
        assert stats.dropped_generic_mention == 1
        assert stats.dropped_generic_ids == [("req_adr00601", quote)]

    def test_validator_keeps_marker_with_task_cue_and_forces_review(self):
        quote = "Разумеется, DevOps-инженер настраивает Docker в кластере"
        brief = f"Развёртывание. {quote}."
        req = _requirement(quote=quote, confidence=0.90, needs_review=False)

        valid, stats = EvidenceValidator().validate_pipeline(
            [req], brief, get_registry(), min_confidence=0.0
        )

        assert len(valid) == 1
        assert stats.dropped_generic_mention == 0
        assert valid[0].needs_review is True
        assert any("Generic/passing" in a for a in valid[0].assumptions)

    def test_zero_of_135_gold_quotes_flagged(self):
        """Gold quotes must never be flagged by the generic gate (0/116 + 0/19)."""
        gold_files = sorted(GOLD_DIR.glob("*/gold.json")) + [LONG_BRIEF2_GOLD]
        total = 0
        flagged: list[tuple[str, str, str]] = []
        for path in gold_files:
            for gold in json.loads(path.read_text(encoding="utf-8")):
                for ev in gold.get("evidence", []):
                    quote = ev.get("quote", "")
                    total += 1
                    if is_generic_mention(quote):
                        flagged.append((str(path), gold.get("requirement_id", ""), quote))

        assert total == 135, f"expected 135 gold quotes (116 + 19), found {total}"
        assert flagged == []


# ---------------------------------------------------------------------------
# 4. Scanner precision on eval/long_brief2
# ---------------------------------------------------------------------------


class TestScannerPrecision:
    def test_long_brief2_excludes_generic_prose_false_positives(self):
        brief = LONG_BRIEF2.read_text(encoding="utf-8")
        registry = get_registry()

        candidate_ids = {
            c.competency_id
            for c in scan_unmatched_competencies(brief, registry, set())
        }

        # Generic-prose FPs from the failed run must no longer be proposed.
        assert candidate_ids.isdisjoint(_GENERIC_PROSE_FPS)
        # Concrete, task-bearing mentions stay scannable for verification.
        assert "iac_terraform" in candidate_ids
        assert "cloud_docker" in candidate_ids

    def test_hyphenated_proper_name_does_not_match_telecom(self):
        brief = LONG_BRIEF2.read_text(encoding="utf-8")
        registry = get_registry()
        candidate_ids = {
            c.competency_id
            for c in scan_unmatched_competencies(brief, registry, set())
        }
        assert "dom_telecom" not in candidate_ids

    def test_load_bearing_phrase_stays_scannable(self):
        """Only bare nouns are stopped: 'нагрузочное тестирование' must still be scannable."""
        registry = get_registry()
        text = "QA-инженер проводит нагрузочное тестирование перед релизом."
        candidate_ids = {
            c.competency_id for c in scan_unmatched_competencies(text, registry, set())
        }
        assert "test_performance" in candidate_ids


# ---------------------------------------------------------------------------
# 5. Contract conformance
# ---------------------------------------------------------------------------


class TestContractConformance:
    def test_response_dump_has_exact_new_keys(self):
        review_req = _requirement(
            quote="Дизайн интерфейсов делает UX/UI-дизайнер",
            role_id="ux_ui_designer",
            competency_id="ui_figma",
            confidence=0.56,
            needs_review=True,
            requirement_id="req_review01",
        )
        main_req = _requirement(
            quote="Инфраструктура описывается кодом на Terraform",
            role_id="devops_engineer",
            competency_id="iac_terraform",
            confidence=0.95,
            requirement_id="req_main0001",
        )
        resp = ExtractResponse(
            request_id="req_contract01",
            brief_id="brf_adr006",
            requirements=[main_req],
            requirements_for_review=[review_req],
            diagnostics=ExtractDiagnostics(
                engine="llm",
                model="test-model",
                prompt_version="extract-v3",
                temperature=0,
                duration_ms=10,
                json_valid=True,
                retries=0,
                dropped_no_evidence=0,
                dropped_generic_mention=1,
                verification=ExtractVerification(
                    candidates_found=3,
                    confirmed=1,
                    rejected=1,
                    skipped=1,
                    failed_batches=0,
                    attempts=2,
                ),
            ),
            warnings=[],
            reason_codes=["REQ_NEEDS_REVIEW"],
        )

        dump = resp.model_dump()
        assert set(dump) == {
            "request_id",
            "brief_id",
            "requirements",
            "requirements_for_review",
            "diagnostics",
            "warnings",
            "reason_codes",
        }
        assert len(dump["requirements"]) == 1
        assert len(dump["requirements_for_review"]) == 1
        diag = dump["diagnostics"]
        assert "dropped_generic_mention" in diag
        assert diag["dropped_generic_mention"] == 1
        assert diag["verification"] == {
            "candidates_found": 3,
            "confirmed": 1,
            "rejected": 1,
            "skipped": 1,
            "failed_batches": 0,
            "attempts": 2,
        }
        assert "post_processor_dropped" not in diag

    def test_build_diagnostics_populates_verification_and_generic_count(self):
        stats = llm_extract_mod.LLMStats(
            tokens_in=10,
            tokens_out=5,
            candidate_competencies_found=14,
            candidate_competencies_confirmed=10,
            candidate_competencies_rejected=3,
            candidate_competencies_skipped=1,
            verification_attempts=2,
            verification_failed_batches=0,
            dropped_generic_mention=1,
        )
        diag = build_diagnostics(
            llm_stats=stats,
            engine="llm",
            model="test-model",
            duration_ms=100,
            dropped_no_evidence=0,
            dropped_generic_mention=1,
        )
        assert diag.verification is not None
        assert diag.verification.candidates_found == 14
        assert diag.verification.attempts == 2
        assert diag.dropped_generic_mention == 1


@pytest.mark.asyncio
async def test_router_routes_low_confidence_pair_to_review(monkeypatch):
    """End-to-end wiring: a low-confidence pair leaves `requirements` for `requirements_for_review`."""
    from httpx import ASGITransport, AsyncClient

    import app.extract.router as router_mod
    from app.llm.config import clear_settings_cache

    class _StubClient:
        def __init__(self, settings):
            self.settings = settings

        async def chat(self, messages, **kwargs):
            return LLMResponse(
                content=json.dumps(
                    [
                        {
                            "role_id": "business_analyst",
                            "competency_id": "an_requirements",
                            "quote": "На этапе обследования работает бизнес-аналитик",
                            "level": 3,
                            "criticality": "must",
                            "confidence": 0.9,
                            "needs_review": False,
                        }
                    ],
                    ensure_ascii=False,
                ),
                tokens_in=20,
                tokens_out=10,
                model="test",
            )

    monkeypatch.setenv("LLM_API_KEY", "test-mock-api-key")
    clear_settings_cache()
    monkeypatch.setattr(router_mod, "LLMClient", _StubClient)
    try:
        from app.main import app

        body = {
            "request_id": "req_adr006rtr",
            "brief_id": "brf_adr006",
            "locale": "ru",
            "brief": {
                "language": "ru",
                "text": "На этапе обследования работает бизнес-аналитик. Срок — 7 месяцев.",
                "source_type": "text",
            },
            "taxonomy": {},
            "options": {"min_confidence": 0.55},
        }
        async with AsyncClient(
            transport=ASGITransport(app=app), base_url="http://test"
        ) as client:
            resp = await client.post("/v1/extract", json=body)

        assert resp.status_code == 200
        data = resp.json()
        main_pairs = [
            (r["role"]["role_id"], r["competency"]["competency_id"])
            for r in data["requirements"]
            if r["competency"]
        ]
        review_pairs = [
            (r["role"]["role_id"], r["competency"]["competency_id"])
            for r in data["requirements_for_review"]
        ]
        assert ("business_analyst", "an_requirements") not in main_pairs
        assert review_pairs == [("business_analyst", "an_requirements")]
        assert any("moved to requirements_for_review" in w for w in data["warnings"])
        assert "REQ_NEEDS_REVIEW" in data["reason_codes"]
    finally:
        clear_settings_cache()


@pytest.mark.asyncio
async def test_router_surfaces_dropped_generic_ids_in_warnings(monkeypatch):
    """End-to-end wiring: dropped generic-mention ids/quotes reach the live `warnings`."""
    from httpx import ASGITransport, AsyncClient

    import app.extract.router as router_mod
    from app.llm.config import clear_settings_cache

    quote = "Контейнеризация — разумеется, Docker, как у всех"

    class _StubClient:
        def __init__(self, settings):
            self.settings = settings

        async def chat(self, messages, **kwargs):
            return LLMResponse(
                content=json.dumps(
                    [
                        {
                            "role_id": "devops_engineer",
                            "competency_id": "cloud_docker",
                            "quote": quote,
                            "level": 3,
                            "criticality": "must",
                            "confidence": 0.9,
                        }
                    ],
                    ensure_ascii=False,
                ),
                tokens_in=20,
                tokens_out=10,
                model="test",
            )

    monkeypatch.setenv("LLM_API_KEY", "test-mock-api-key")
    clear_settings_cache()
    monkeypatch.setattr(router_mod, "LLMClient", _StubClient)
    try:
        from app.main import app

        body = {
            "request_id": "req_generic001",
            "brief_id": "brf_generic",
            "locale": "ru",
            "brief": {
                "language": "ru",
                "text": f"Развёртывание. {quote}. Дашборды собираем в Grafana.",
                "source_type": "text",
            },
            "taxonomy": {},
            "options": {"min_confidence": 0.55},
        }
        async with AsyncClient(
            transport=ASGITransport(app=app), base_url="http://test"
        ) as client:
            resp = await client.post("/v1/extract", json=body)

        assert resp.status_code == 200
        data = resp.json()
        assert data["requirements"] == []
        dropped = [w for w in data["warnings"] if "Dropped generic mention" in w]
        assert dropped, f"expected dropped generic ids in warnings, got {data['warnings']}"
        assert any("req_001" in w and "Docker" in w for w in dropped)
    finally:
        clear_settings_cache()


# ---------------------------------------------------------------------------
# 5. Verification decision attribution (follow-up on ADR-006 / 3.1-fix)
# ---------------------------------------------------------------------------


def _cand(competency_id: str) -> CompetencyCandidateMention:
    """Build a candidate mention straight from the taxonomy (no scanner involved)."""
    comp = get_registry().competencies[competency_id]
    return CompetencyCandidateMention(
        competency_id=competency_id,
        name_ru=comp.name_ru,
        matched_text=comp.name_ru,
        quote_sentence=f"mention of {comp.name_ru}",
        allowed_roles=list(comp.roles),
    )


def _assert_accounting_invariant(stats) -> None:
    assert (
        stats.candidate_competencies_found
        == stats.candidate_competencies_confirmed
        + stats.candidate_competencies_rejected
        + stats.candidate_competencies_skipped
    )


@pytest.mark.asyncio
class TestVerificationAttribution:
    """A decision may only affect the candidate that was actually sent for verification."""

    async def test_confirm_for_non_candidate_competency_is_not_appended(
        self, monkeypatch
    ):
        """`confirm` for a competency that was never a candidate must not add a pair."""
        brief = (
            "Бэкенд на Go. DevOps-инженер описывает инфраструктуру в Terraform. "
            "Настроить кластер Kubernetes. Срок — 6 месяцев."
        )
        monkeypatch.setattr(
            llm_extract_mod,
            "scan_unmatched_competencies",
            lambda *args, **kwargs: [_cand("iac_terraform")],
        )
        confirm_non_candidate = [
            {
                "decision": "confirm",
                "role_id": "devops_engineer",
                "competency_id": "cloud_kubernetes",
                "quote": "Настроить кластер Kubernetes",
                "level": 3,
                "criticality": "must",
                "confidence": 0.9,
            }
        ]
        client = _QueuedClient(
            [
                LLMResponse(
                    content=json.dumps([_primary_go_item()], ensure_ascii=False),
                    tokens_in=100,
                    tokens_out=20,
                    model="test",
                ),
                LLMResponse(
                    content=json.dumps(confirm_non_candidate, ensure_ascii=False),
                    tokens_in=100,
                    tokens_out=20,
                    model="test",
                ),
            ]
        )

        reqs, stats = await llm_extract(
            brief,
            "ru",
            get_registry(),
            client,
            _settings(max_retries=0),
            ensemble_runs=1,
            verify_candidates=True,
            role_safety_net=False,
        )

        pairs = {(r.role.role_id, r.competency.competency_id) for r in reqs if r.competency}
        assert ("devops_engineer", "cloud_kubernetes") not in pairs
        assert stats.candidate_competencies_found == 1
        assert stats.candidate_competencies_confirmed == 0
        _assert_accounting_invariant(stats)
        log_by_id = {e["id"]: e for e in stats.verification_log}
        assert log_by_id["iac_terraform"]["status"] == "skipped"

    async def test_confirm_with_role_outside_allowed_roles_is_skipped(
        self, monkeypatch
    ):
        """A confirm naming a role outside the candidate's allowed roles is skipped."""
        brief = (
            "Бэкенд на Go. DevOps-инженер настраивает дашборды в Grafana. "
            "Срок — 6 месяцев."
        )
        monkeypatch.setattr(
            llm_extract_mod,
            "scan_unmatched_competencies",
            lambda *args, **kwargs: [_cand("obs_grafana")],
        )
        confirm_bad_role = [
            {
                "decision": "confirm",
                "role_id": "backend_engineer",
                "competency_id": "obs_grafana",
                "quote": "настраивает дашборды в Grafana",
                "level": 3,
                "criticality": "must",
                "confidence": 0.9,
            }
        ]
        client = _QueuedClient(
            [
                LLMResponse(
                    content=json.dumps([_primary_go_item()], ensure_ascii=False),
                    tokens_in=100,
                    tokens_out=20,
                    model="test",
                ),
                LLMResponse(
                    content=json.dumps(confirm_bad_role, ensure_ascii=False),
                    tokens_in=100,
                    tokens_out=20,
                    model="test",
                ),
            ]
        )

        reqs, stats = await llm_extract(
            brief,
            "ru",
            get_registry(),
            client,
            _settings(max_retries=0),
            ensemble_runs=1,
            verify_candidates=True,
            role_safety_net=False,
        )

        assert all(
            r.competency is None or r.competency.competency_id != "obs_grafana"
            for r in reqs
        )
        assert stats.candidate_competencies_confirmed == 0
        _assert_accounting_invariant(stats)
        log_by_id = {e["id"]: e for e in stats.verification_log}
        assert log_by_id["obs_grafana"]["status"] == "skipped"
        assert (
            log_by_id["obs_grafana"]["reason"]
            == "confirmed role backend_engineer not in allowed_roles for obs_grafana"
        )

    async def test_reject_with_unrelated_competency_does_not_reject_role(
        self, monkeypatch
    ):
        """A reject carrying an unrelated competency_id must not reject the named role."""
        brief = "Бэкенд на Go. Тестировщик проверяет регресс. Срок — 6 месяцев."
        monkeypatch.setattr(
            llm_extract_mod,
            "scan_unmatched_competencies",
            lambda *args, **kwargs: [],
        )
        role_cand = RoleMention(
            role_id="qa_engineer",
            name_ru="QA-инженер",
            matched_text="Тестировщик",
            quote_sentence="Тестировщик проверяет регресс.",
            allowed_competencies=["test_manual"],
        )
        monkeypatch.setattr(
            llm_extract_mod,
            "scan_missing_roles",
            lambda *args, **kwargs: [role_cand],
        )
        reject_unrelated = [
            {
                "decision": "reject",
                "competency_id": "cloud_docker",  # never a candidate
                "role_id": "qa_engineer",
                "reason": "not needed",
            }
        ]
        client = _QueuedClient(
            [
                LLMResponse(
                    content=json.dumps([_primary_go_item()], ensure_ascii=False),
                    tokens_in=100,
                    tokens_out=20,
                    model="test",
                ),
                LLMResponse(
                    content=json.dumps(reject_unrelated, ensure_ascii=False),
                    tokens_in=100,
                    tokens_out=20,
                    model="test",
                ),
            ]
        )

        _reqs, stats = await llm_extract(
            brief,
            "ru",
            get_registry(),
            client,
            _settings(max_retries=0),
            ensemble_runs=1,
            verify_candidates=True,
            role_safety_net=True,
        )

        assert stats.missing_roles_found == 1
        assert stats.missing_roles_rejected == 0
        log_by_id = {e["id"]: e for e in stats.verification_log}
        # Not rejected by the unrelated object: the Role Safety Net may still recover it.
        assert log_by_id["qa_engineer"]["status"] != "rejected"

    async def test_two_batches_confirming_produce_unique_requirement_ids(
        self, monkeypatch
    ):
        """The shared monotonic index keeps default requirement ids unique across batches.

        Batch 1 confirms its *second* candidate only. With the previous
        `len(union_reqs) + idx` formula that id collides with the first item of
        batch 2, because `union_reqs` grew by one while the position advanced by two.
        """
        brief = (
            "Бэкенд на Go. DevOps-инженер описывает инфраструктуру в Terraform. "
            "Настроить кластер Kubernetes. Упаковать сервисы в Docker. "
            "Срок — 6 месяцев."
        )
        # 9 candidates -> batch 1 (8 items) + batch 2 (1 item).
        candidate_ids = [
            "iac_terraform",
            "cloud_kubernetes",
            "db_postgresql",
            "sec_iam",
            "proc_kanban",
            "arch_c4",
            "web_accessibility",
            "cloud_yandex",
            "cloud_docker",
        ]
        cands = [_cand(cid) for cid in candidate_ids]
        monkeypatch.setattr(
            llm_extract_mod,
            "scan_unmatched_competencies",
            lambda *args, **kwargs: cands,
        )
        batch1 = [
            # Rejected first: the confirmed item is at response position 1.
            {"decision": "reject", "competency_id": "iac_terraform"},
            {
                "decision": "confirm",
                "role_id": "devops_engineer",
                "competency_id": "cloud_kubernetes",
                "quote": "Настроить кластер Kubernetes",
                "level": 3,
                "criticality": "must",
                "confidence": 0.9,
            },
        ]
        batch2 = [
            {
                "decision": "confirm",
                "role_id": "devops_engineer",
                "competency_id": "cloud_docker",
                "quote": "Упаковать сервисы в Docker",
                "level": 3,
                "criticality": "must",
                "confidence": 0.9,
            }
        ]
        client = _QueuedClient(
            [
                LLMResponse(
                    content=json.dumps([_primary_go_item()], ensure_ascii=False),
                    tokens_in=100,
                    tokens_out=20,
                    model="test",
                ),
                LLMResponse(
                    content=json.dumps(batch1, ensure_ascii=False),
                    tokens_in=100,
                    tokens_out=20,
                    model="test",
                ),
                LLMResponse(
                    content=json.dumps(batch2, ensure_ascii=False),
                    tokens_in=100,
                    tokens_out=20,
                    model="test",
                ),
            ]
        )

        reqs, stats = await llm_extract(
            brief,
            "ru",
            get_registry(),
            client,
            _settings(max_retries=0),
            ensemble_runs=1,
            verify_candidates=True,
            role_safety_net=False,
        )

        pairs = {(r.role.role_id, r.competency.competency_id) for r in reqs if r.competency}
        assert ("devops_engineer", "cloud_kubernetes") in pairs
        assert ("devops_engineer", "cloud_docker") in pairs
        assert stats.candidate_competencies_confirmed == 2
        _assert_accounting_invariant(stats)

        ids = [r.requirement_id for r in reqs]
        assert len(ids) == len(set(ids)), f"duplicate requirement_id in {ids}"
        # The verified items get fresh ids at assignment time (no downstream renumbering
        # of a clash between batches).
        assert ids == ["req_001", "req_002", "req_003"]


# ---------------------------------------------------------------------------
# 6. Verification error handling: transient retried, unavailable not retried
# ---------------------------------------------------------------------------


class _ScriptedClient:
    """Mock client that can raise typed LLM errors at a chosen call index."""

    def __init__(self, script: list):
        self._script = list(script)
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
        self.calls.append({"messages": list(messages), "max_tokens": max_tokens})
        idx = len(self.calls) - 1
        item = self._script[min(idx, len(self._script) - 1)]
        if isinstance(item, Exception):
            raise item
        return item


@pytest.mark.asyncio
class TestVerificationErrorHandling:
    async def test_transient_5xx_is_retried_within_the_batch(self, monkeypatch):
        brief = (
            "Бэкенд на Go. DevOps-инженер описывает инфраструктуру в Terraform. "
            "Срок — 6 месяцев."
        )
        monkeypatch.setattr(
            llm_extract_mod,
            "scan_unmatched_competencies",
            lambda *args, **kwargs: [_cand("iac_terraform")],
        )
        confirm = [
            {
                "decision": "confirm",
                "role_id": "devops_engineer",
                "competency_id": "iac_terraform",
                "quote": "описывает инфраструктуру в Terraform",
                "level": 3,
                "criticality": "must",
                "confidence": 0.9,
            }
        ]
        client = _ScriptedClient(
            [
                LLMResponse(
                    content=json.dumps([_primary_go_item()], ensure_ascii=False),
                    tokens_in=100,
                    tokens_out=20,
                    model="test",
                ),
                LLMErrorResponse("upstream exploded", status_code=503),
                LLMResponse(
                    content=json.dumps(confirm, ensure_ascii=False),
                    tokens_in=100,
                    tokens_out=20,
                    model="test",
                ),
            ]
        )

        _reqs, stats = await llm_extract(
            brief,
            "ru",
            get_registry(),
            client,
            _settings(max_retries=1),
            ensemble_runs=1,
            verify_candidates=True,
            role_safety_net=False,
        )

        assert stats.verification_attempts == 2
        assert stats.verification_failed_batches == 0
        assert stats.candidate_competencies_confirmed == 1
        _assert_accounting_invariant(stats)

    async def test_timeout_is_retried_and_unavailable_is_not(self, monkeypatch):
        brief = (
            "Бэкенд на Go. DevOps-инженер описывает инфраструктуру в Terraform. "
            "Срок — 6 месяцев."
        )
        monkeypatch.setattr(
            llm_extract_mod,
            "scan_unmatched_competencies",
            lambda *args, **kwargs: [_cand("iac_terraform")],
        )
        primary = LLMResponse(
            content=json.dumps([_primary_go_item()], ensure_ascii=False),
            tokens_in=100,
            tokens_out=20,
            model="test",
        )

        # Timeout on the first verification attempt -> retried -> batch skipped.
        timeout_client = _ScriptedClient(
            [primary, LLMTimeoutError("timed out"), LLMTimeoutError("timed out")]
        )
        _reqs, stats = await llm_extract(
            brief,
            "ru",
            get_registry(),
            timeout_client,
            _settings(max_retries=1),
            ensemble_runs=1,
            verify_candidates=True,
            role_safety_net=False,
        )
        assert stats.verification_attempts == 2
        assert stats.verification_failed_batches == 1
        assert stats.candidate_competencies_skipped == 1
        _assert_accounting_invariant(stats)

        # Unavailable is never retried: one attempt, batch skipped, no crash.
        unavailable_client = _ScriptedClient(
            [primary, LLMUnavailableError("no key")]
        )
        _reqs2, stats2 = await llm_extract(
            brief,
            "ru",
            get_registry(),
            unavailable_client,
            _settings(max_retries=3),
            ensemble_runs=1,
            verify_candidates=True,
            role_safety_net=False,
        )
        assert stats2.verification_attempts == 1
        assert stats2.verification_failed_batches == 1
        _assert_accounting_invariant(stats2)

