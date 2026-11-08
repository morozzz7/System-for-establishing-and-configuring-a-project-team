"""Tests for the /v1/extract router (LLM-first): contract conformance & fail-loudly Problem responses."""

from __future__ import annotations

import json
import os
from pathlib import Path
from typing import Any

import pytest
import pytest_asyncio
from httpx import ASGITransport, AsyncClient

import app.extract.router as router_mod
from app.extract.schemas import ExtractResponse, Problem
from app.extract.taxonomy import reset_registry
from app.llm.client import (
    LLMErrorResponse,
    LLMResponse,
    LLMTimeoutError,
    LLMUnavailableError,
)
from app.llm.config import LLMSettings, clear_settings_cache
from app.main import app

GOLD_DIR = Path(__file__).resolve().parents[2] / "eval" / "gold"
FIXTURES_DIR = Path(__file__).resolve().parent / "fixtures" / "llm"


class _StubLLMClient:
    """Deterministic stub replacing LLMClient in router tests."""

    response_content: str = json.dumps(
        [
            {
                "role_id": "backend_engineer",
                "competency_id": "lang_go",
                "quote": "бэкенд-разработчик на Go",
                "level": 3,
                "criticality": "must",
                "headcount": 1,
                "confidence": 0.95,
                "needs_review": False,
            }
        ],
        ensure_ascii=False,
    )
    error_to_raise: Exception | None = None

    def __init__(self, settings: LLMSettings) -> None:
        self.settings = settings

    async def chat(self, messages: list[dict[str, Any]], **kwargs: Any) -> LLMResponse:
        if _StubLLMClient.error_to_raise is not None:
            raise _StubLLMClient.error_to_raise
        return LLMResponse(
            content=_StubLLMClient.response_content,
            tokens_in=1200,
            tokens_out=150,
            model=self.settings.model,
        )


@pytest.fixture(autouse=True)
def _clean_env(monkeypatch):
    """Isolate environment variables and default to a mocked LLM key + stub client."""
    saved = {}
    for key in list(os.environ):
        if key.startswith("LLM_"):
            saved[key] = os.environ.pop(key)
    os.environ["LLM_API_KEY"] = "test-mock-api-key"

    prev_env_file = LLMSettings.model_config.get("env_file")
    LLMSettings.model_config["env_file"] = None
    clear_settings_cache()
    reset_registry()

    _StubLLMClient.error_to_raise = None
    _StubLLMClient.response_content = json.dumps(
        [
            {
                "role_id": "backend_engineer",
                "competency_id": "lang_go",
                "quote": "бэкенд-разработчик на Go",
                "level": 3,
                "criticality": "must",
                "headcount": 1,
                "confidence": 0.95,
                "needs_review": False,
            }
        ],
        ensure_ascii=False,
    )
    monkeypatch.setattr(router_mod, "LLMClient", _StubLLMClient)

    yield

    for key in list(os.environ):
        if key.startswith("LLM_"):
            del os.environ[key]
    for key, val in saved.items():
        os.environ[key] = val
    LLMSettings.model_config["env_file"] = prev_env_file
    clear_settings_cache()
    reset_registry()


@pytest_asyncio.fixture()
async def client():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as c:
        yield c


def _valid_request(
    brief_text: str = "Нужен бэкенд-разработчик на Go, middle уровень. Срок — 8 недель.",
) -> dict:
    """Build a valid ExtractRequest body."""
    return {
        "request_id": "req_test00000001",
        "brief_id": "brf_test001",
        "locale": "ru",
        "brief": {
            "language": "ru",
            "text": brief_text,
            "source_type": "text",
        },
        "taxonomy": {},
        "options": {},
    }


def test_router_does_not_import_rules_or_merger():
    """Verify 3.7: router.py must not import RulesEngine or Merger."""
    assert not hasattr(router_mod, "RulesEngine")
    assert not hasattr(router_mod, "Merger")


@pytest.mark.asyncio
class TestExtractRouterValid:
    async def test_valid_request_returns_200(self, client: AsyncClient):
        resp = await client.post("/v1/extract", json=_valid_request())
        assert resp.status_code == 200
        body = resp.json()
        assert "request_id" in body
        assert "requirements" in body
        assert "diagnostics" in body
        assert len(body["requirements"]) == 1

    async def test_echo_x_request_id(self, client: AsyncClient):
        resp = await client.post(
            "/v1/extract",
            json=_valid_request(),
            headers={"X-Request-Id": "req_test00000001"},
        )
        assert resp.status_code == 200
        assert resp.headers.get("x-request-id") == "req_test00000001"

    async def test_response_validates_as_extract_response(self, client: AsyncClient):
        resp = await client.post("/v1/extract", json=_valid_request())
        assert resp.status_code == 200
        parsed = ExtractResponse.model_validate(resp.json())
        assert parsed.request_id == "req_test00000001"
        assert isinstance(parsed.requirements, list)
        assert isinstance(parsed.warnings, list)
        assert isinstance(parsed.reason_codes, list)

    async def test_diagnostics_fields(self, client: AsyncClient):
        resp = await client.post("/v1/extract", json=_valid_request())
        diag = resp.json()["diagnostics"]
        assert diag["engine"] == "llm"
        assert diag["prompt_version"] == "extract-v3"
        assert diag["temperature"] == 0
        assert diag["duration_ms"] >= 0
        assert diag["json_valid"] is True
        assert isinstance(diag["retries"], int)
        assert isinstance(diag["dropped_no_evidence"], int)
        assert "post_processor_dropped" not in diag

    async def test_empty_surviving_requirements_includes_warning(
        self, client: AsyncClient
    ):
        """Per 3.7, HTTP 200 with empty requirements must include an explicit warning."""
        _StubLLMClient.response_content = "[]"
        body = _valid_request(brief_text="Короткий текст без компетенций")
        resp = await client.post("/v1/extract", json=body)
        assert resp.status_code == 200
        data = resp.json()
        assert data["requirements"] == []
        assert len(data["warnings"]) >= 1
        assert "REQ_LOW_CONFIDENCE" in data["reason_codes"]

    async def test_gold_brief_end_to_end(self, client: AsyncClient):
        """Run brief_001 through the endpoint with its replay fixture."""
        brief_dir = GOLD_DIR / "brief_001_simple_web_mvp"
        brief_text = (brief_dir / "brief.md").read_text(encoding="utf-8")
        fixture = json.loads(
            (FIXTURES_DIR / "brief_001_response.json").read_text(encoding="utf-8")
        )
        _StubLLMClient.response_content = fixture["content"]

        body = _valid_request(brief_text=brief_text)
        body["brief_id"] = "brf_001"
        body["request_id"] = "req_gold00000001"
        resp = await client.post("/v1/extract", json=body)
        assert resp.status_code == 200
        data = resp.json()
        assert len(data["requirements"]) == 7
        assert data["diagnostics"]["engine"] == "llm"


@pytest.mark.asyncio
class TestExtractRouterErrors:
    async def test_invalid_body_422(self, client: AsyncClient):
        resp = await client.post("/v1/extract", json={})
        assert resp.status_code == 422

    async def test_invalid_request_id_pattern_422(self, client: AsyncClient):
        body = _valid_request()
        body["request_id"] = "invalid"
        resp = await client.post("/v1/extract", json=body)
        assert resp.status_code == 422

    async def test_missing_brief_text_422(self, client: AsyncClient):
        body = _valid_request()
        del body["brief"]["text"]
        resp = await client.post("/v1/extract", json=body)
        assert resp.status_code == 422


@pytest.mark.asyncio
class TestExtractRouterLLMFailures:
    async def test_no_api_key_returns_503_problem(self, client: AsyncClient):
        """Missing LLM_API_KEY -> 503 Problem with retryable=True and ML_LLM_UNAVAILABLE."""
        os.environ["LLM_API_KEY"] = ""
        clear_settings_cache()

        resp = await client.post("/v1/extract", json=_valid_request())
        assert resp.status_code == 503
        prob = Problem.model_validate(resp.json())
        assert prob.code == "CAPABILITY_UNAVAILABLE"
        assert prob.retryable is True
        assert "ML_LLM_UNAVAILABLE" in prob.reason_codes

    async def test_llm_unavailable_returns_503_problem(self, client: AsyncClient):
        """LLMUnavailableError -> 503 Problem with retryable=True and ML_LLM_UNAVAILABLE."""
        _StubLLMClient.error_to_raise = LLMUnavailableError("Connection refused")
        resp = await client.post("/v1/extract", json=_valid_request())
        assert resp.status_code == 503
        prob = Problem.model_validate(resp.json())
        assert prob.code == "CAPABILITY_UNAVAILABLE"
        assert prob.retryable is True
        assert "ML_LLM_UNAVAILABLE" in prob.reason_codes

    async def test_llm_http_5xx_returns_503_problem(self, client: AsyncClient):
        """LLMErrorResponse (HTTP 502) -> 503 Problem with retryable=True and ML_LLM_UNAVAILABLE."""
        _StubLLMClient.error_to_raise = LLMErrorResponse("Bad Gateway", status_code=502)
        resp = await client.post("/v1/extract", json=_valid_request())
        assert resp.status_code == 503
        prob = Problem.model_validate(resp.json())
        assert prob.code == "CAPABILITY_UNAVAILABLE"
        assert prob.retryable is True
        assert "ML_LLM_UNAVAILABLE" in prob.reason_codes

    async def test_llm_timeout_returns_408_problem(self, client: AsyncClient):
        """LLMTimeoutError -> 408 Problem with retryable=True and ML_TIMEOUT."""
        _StubLLMClient.error_to_raise = LLMTimeoutError("LLM timed out after 20s")
        resp = await client.post("/v1/extract", json=_valid_request())
        assert resp.status_code == 408
        prob = Problem.model_validate(resp.json())
        assert prob.code == "TIMEOUT"
        assert prob.retryable is True
        assert "ML_TIMEOUT" in prob.reason_codes

    async def test_deterministic_output(self, client: AsyncClient):
        """Same input -> same output (except duration_ms)."""
        body = _valid_request()
        resp1 = await client.post("/v1/extract", json=body)
        resp2 = await client.post("/v1/extract", json=body)
        b1 = resp1.json()
        b2 = resp2.json()
        b1["diagnostics"]["duration_ms"] = 0
        b2["diagnostics"]["duration_ms"] = 0
        assert b1 == b2
