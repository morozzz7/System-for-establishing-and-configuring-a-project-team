"""Regression tests for the critical review findings (timeouts, fail-loudly, review routing)."""

from __future__ import annotations

import json

import pytest
import pytest_asyncio
from httpx import ASGITransport, AsyncClient

import app.extract.router as router_mod
from app.extract.diagnostics import build_diagnostics
from app.extract.llm_extract import llm_extract
from app.extract.review import split_for_review
from app.extract.taxonomy import get_registry, reset_registry
from app.extract.validator import EvidenceValidator
from app.llm.client import LLMErrorResponse, LLMResponse
from app.llm.config import LLMSettings
from app.main import app
from app.optimize import solver
from tests.optimize_helpers import make_candidate, make_options, make_request, make_requirement
from tests.test_adr006 import _requirement
from tests.test_llm_extract import MockLLMClient, _make_settings
from tests.test_router import _valid_request

_GO_BRIEF = "Бэкенд на Go (middle). Срок — 8 недель."
_GO_ITEM = {
    "role_id": "backend_engineer",
    "competency_id": "lang_go",
    "quote": "Бэкенд на Go",
    "level": 3,
    "criticality": "must",
}


@pytest_asyncio.fixture()
async def client():
    async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as c:
        yield c


@pytest.fixture(autouse=True)
def _reset_taxonomy():
    reset_registry()
    yield
    reset_registry()


def _json_response(items: list[dict]) -> LLMResponse:
    return LLMResponse(
        content=json.dumps(items, ensure_ascii=False),
        tokens_in=10,
        tokens_out=5,
        model="test",
    )


@pytest.mark.asyncio
async def test_caller_timeout_is_the_ceiling_not_the_service_default(monkeypatch):
    settings = LLMSettings(api_key="test-key", timeout_ms=300_000, model="test-model")
    monkeypatch.setattr(router_mod, "get_settings", lambda: settings)

    instances: list[object] = []

    class _CapturingClient:
        def __init__(self, client_settings: LLMSettings) -> None:
            self.settings = client_settings
            self.timeouts_ms: list[int | None] = []
            instances.append(self)

        async def chat(self, messages, **kwargs) -> LLMResponse:
            self.timeouts_ms.append(kwargs.get("timeout_ms"))
            return LLMResponse(content="[]", tokens_in=0, tokens_out=0, model="test")

    monkeypatch.setattr(router_mod, "LLMClient", _CapturingClient)

    body = _valid_request()
    body["options"] = {"timeout_ms": 1000}
    async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as c:
        resp = await c.post("/v1/extract", json=body)

    assert resp.status_code == 200
    assert instances
    client = instances[0]
    assert client.settings.timeout_ms == 1000
    assert client.timeouts_ms == [1000]
    assert settings.timeout_ms == 300_000


@pytest.mark.asyncio
async def test_one_failed_ensemble_pass_fails_the_extraction():
    class _OneFailureClient:
        async def chat(self, messages, *, seed, **kwargs) -> LLMResponse:
            if seed == 42:
                raise LLMErrorResponse("Bad Gateway", status_code=502)
            return _json_response([_GO_ITEM])

    with pytest.raises(LLMErrorResponse):
        await llm_extract(
            _GO_BRIEF,
            "ru",
            get_registry(),
            _OneFailureClient(),
            _make_settings(max_retries=0),
            ensemble_runs=3,
            verify_candidates=False,
            role_safety_net=False,
        )


@pytest.mark.asyncio
async def test_ensemble_json_valid_reflects_failed_passes():
    class _OneInvalidPassClient:
        async def chat(self, messages, *, seed, **kwargs) -> LLMResponse:
            if seed == 43:
                return LLMResponse(content="garbage", tokens_in=1, tokens_out=1, model="test")
            return _json_response([_GO_ITEM])

    _reqs, stats = await llm_extract(
        _GO_BRIEF,
        "ru",
        get_registry(),
        _OneInvalidPassClient(),
        _make_settings(max_retries=0),
        ensemble_runs=3,
        verify_candidates=False,
        role_safety_net=False,
    )
    assert stats.json_valid is False


@pytest.mark.asyncio
async def test_parse_failure_returns_schema_invalid_problem(monkeypatch):
    monkeypatch.setattr(
        router_mod,
        "get_settings",
        lambda: LLMSettings(api_key="test-key", max_retries=2),
    )

    class _GarbageClient:
        def __init__(self, settings: LLMSettings) -> None:
            self.settings = settings

        async def chat(self, messages, **kwargs) -> LLMResponse:
            return LLMResponse(content="garbage", tokens_in=1, tokens_out=1, model="test")

    monkeypatch.setattr(router_mod, "LLMClient", _GarbageClient)
    async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as c:
        resp = await c.post("/v1/extract", json=_valid_request())

    assert resp.status_code == 422
    body = resp.json()
    assert body["code"] == "ML_SCHEMA_INVALID"
    assert body["status"] == 422
    assert body["retryable"] is False
    assert body["reason_codes"] == ["ML_SCHEMA_INVALID"]
    assert body["request_id"] == "req_test00000001"


def test_min_confidence_does_not_bypass_review_routing():
    quote = "Дизайн интерфейсов делает UX/UI-дизайнер"
    brief = f"Развёртывание. {quote}."
    pair = _requirement(
        quote=quote,
        role_id="ux_ui_designer",
        competency_id="ui_figma",
        confidence=0.90,
        needs_review=True,
        requirement_id="req_reviewflag",
    )
    valid, _stats = EvidenceValidator().validate_pipeline(
        [pair], brief, get_registry(), min_confidence=0.7
    )
    assert valid
    assert valid[0].confidence >= 0.7

    main, review = split_for_review(valid)
    assert main == []
    assert [r.requirement_id for r in review] == ["req_reviewflag"]


@pytest.mark.asyncio
async def test_max_retries_never_exceeds_diagnostic_cap():
    bad = LLMResponse(content="garbage", tokens_in=5, tokens_out=1, model="test")
    client = MockLLMClient([bad, bad, bad, bad])
    _reqs, stats = await llm_extract(
        _GO_BRIEF,
        "ru",
        get_registry(),
        client,
        _make_settings(max_retries=10),
    )
    assert stats.retries == 2
    diagnostics = build_diagnostics(
        llm_stats=stats,
        engine="llm",
        model="test",
        duration_ms=0,
        dropped_no_evidence=0,
    )
    assert diagnostics.retries == 2


@pytest.mark.asyncio
async def test_readyz_does_not_advertise_a_spacy_pipeline(client: AsyncClient):
    resp = await client.get("/readyz")
    assert resp.status_code == 200
    assert resp.json()["models"]["spacy_pipeline"] != "ru_core_news_lg"


@pytest.mark.asyncio
async def test_embed_malformed_json_is_invalid_request(client: AsyncClient):
    resp = await client.post(
        "/v1/embed",
        content="{not json",
        headers={"content-type": "application/json", "X-Request-Id": "req_test00000001"},
    )
    assert resp.status_code == 422
    assert resp.headers.get("x-request-id") == "req_test00000001"
    body = resp.json()
    assert body["code"] == "INVALID_REQUEST"
    assert body["reason_codes"] == ["VALIDATION_FAILED"]
    assert body["request_id"] == "req_test00000001"


@pytest.mark.asyncio
async def test_embed_unsupported_model_keeps_request_id_header(client: AsyncClient):
    body = {
        "request_id": "req_test00000001",
        "model": "unknown-model",
        "items": [{"item_id": "i1", "kind": "brief", "text": "test"}],
    }
    resp = await client.post(
        "/v1/embed", json=body, headers={"X-Request-Id": "req_test00000001"}
    )
    assert resp.status_code == 422
    assert resp.headers.get("x-request-id") == "req_test00000001"
    assert resp.json()["code"] == "UNSUPPORTED_MODEL"


def test_reported_deficit_uses_likely_minus_assigned():
    candidate = make_candidate("emp_abcd", competencies={"lang_go": 4}, free_hours=8.0)
    requirement = make_requirement(
        "req_0001", competency="lang_go", level=3, min_pd=10, likely_pd=20
    )
    req = make_request(
        requirements=[requirement],
        candidates=[candidate],
        options=make_options(allow_deficits=True),
        k=2,
    )
    outcome = solver.solve(req)
    assert outcome.variants
    variant = outcome.variants[0]
    assigned_hours = sum(h for _e, _r, _w, h in variant.assignments)
    assert assigned_hours > 0
    assert assigned_hours < 10 * 8

    assert len(variant.deficits) == 1
    deficit = variant.deficits[0]
    assert deficit.requirement_ids == ["req_0001"]
    assert deficit.person_days == pytest.approx(
        20.0 - assigned_hours / 8, abs=0.01
    )
    assert deficit.person_days > (10 * 8 - assigned_hours) / 8
