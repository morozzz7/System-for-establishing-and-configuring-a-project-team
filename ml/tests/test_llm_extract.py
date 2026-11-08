"""Tests for LLM extraction: compact schema, element-wise validation, feedback retry, error propagation."""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from app.extract.llm_extract import LLMStats, _extract_json_array, llm_extract
from app.extract.schemas import ExtractedRequirement
from app.extract.taxonomy import get_registry, reset_registry
from app.llm.client import (
    LLMErrorResponse,
    LLMResponse,
    LLMTimeoutError,
    LLMUnavailableError,
)
from app.llm.config import LLMSettings

FIXTURES_DIR = Path(__file__).resolve().parent / "fixtures" / "llm"
GOLD_DIR = Path(__file__).resolve().parents[2] / "eval" / "gold"


@pytest.fixture(autouse=True)
def _reset():
    reset_registry()
    yield
    reset_registry()


class TestExtractJsonArray:
    def test_plain_json(self):
        assert _extract_json_array("[1, 2, 3]") == "[1, 2, 3]"

    def test_markdown_fence(self):
        text = '```json\n[{"a": 1}]\n```'
        assert _extract_json_array(text) == '[{"a": 1}]'

    def test_preamble_text(self):
        text = 'Here is the result:\n[{"a": 1}]'
        assert _extract_json_array(text) == '[{"a": 1}]'

    def test_no_json_array(self):
        assert _extract_json_array("no json here") is None

    def test_nested_objects(self):
        text = '[{"role": {"id": "test"}, "items": [1,2]}]'
        assert _extract_json_array(text) == text


class MockLLMClient:
    """Mock LLM client that records calls for inspecting retry feedback."""

    def __init__(self, responses: list[LLMResponse | Exception]):
        self._responses = responses
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
                "temperature": temperature,
                "seed": seed,
                "max_tokens": max_tokens,
                "timeout_ms": timeout_ms,
            }
        )
        idx = len(self.calls) - 1
        if idx >= len(self._responses):
            raise LLMErrorResponse("No more mock responses")
        item = self._responses[idx]
        if isinstance(item, Exception):
            raise item
        return item


def _make_settings(max_retries: int = 2) -> LLMSettings:
    return LLMSettings(api_key="test-key", max_retries=max_retries)


def _brief_001_text() -> str:
    return (GOLD_DIR / "brief_001_simple_web_mvp" / "brief.md").read_text(
        encoding="utf-8"
    )


@pytest.mark.asyncio
class TestLLMExtract:
    async def test_valid_json_parsed(self):
        """Valid compact JSON response -> list of enriched ExtractedRequirement."""
        fixture_path = FIXTURES_DIR / "brief_001_response.json"
        fixture = json.loads(fixture_path.read_text(encoding="utf-8"))

        resp = LLMResponse(
            content=fixture["content"],
            tokens_in=fixture["tokens_in"],
            tokens_out=fixture["tokens_out"],
            model=fixture["model"],
        )
        client = MockLLMClient([resp])
        registry = get_registry()
        settings = _make_settings()

        reqs, stats = await llm_extract(
            _brief_001_text(), "ru", registry, client, settings
        )
        assert len(reqs) == 7
        assert all(isinstance(r, ExtractedRequirement) for r in reqs)
        assert stats.retries == 0
        assert stats.json_valid is True

    async def test_markdown_fence_repair(self):
        """LLM response wrapped in markdown code block -> parses without retry."""
        fixture_path = FIXTURES_DIR / "brief_001_response.json"
        fixture = json.loads(fixture_path.read_text(encoding="utf-8"))

        wrapped = f'```json\n{fixture["content"]}\n```'
        resp = LLMResponse(content=wrapped, tokens_in=100, tokens_out=50, model="test")
        client = MockLLMClient([resp])
        registry = get_registry()
        settings = _make_settings()

        reqs, stats = await llm_extract(
            _brief_001_text(), "ru", registry, client, settings
        )
        assert len(reqs) == 7
        assert stats.json_valid is True

    async def test_element_wise_validation_preserves_valid_items(self):
        """1 bad element in an array of 3 -> 2 valid elements are preserved (never all-or-nothing)."""
        brief_text = "Бэкенд на Go (уровень middle), фронтенд на React. Срок — 8 недель."
        items = [
            {
                "role_id": "backend_engineer",
                "competency_id": "lang_go",
                "quote": "Бэкенд на Go",
                "level": 3,
                "criticality": "must",
            },
            {
                # Bad item: invalid level 99 and missing quote
                "role_id": "backend_engineer",
                "competency_id": "db_postgresql",
                "level": 99,
            },
            {
                "role_id": "frontend_engineer",
                "competency_id": "fw_react",
                "quote": "фронтенд на React",
                "level": 3,
                "criticality": "must",
            },
        ]
        resp = LLMResponse(
            content=json.dumps(items, ensure_ascii=False),
            tokens_in=100,
            tokens_out=60,
            model="test",
        )
        client = MockLLMClient([resp])
        registry = get_registry()
        settings = _make_settings(max_retries=0)

        reqs, stats = await llm_extract(brief_text, "ru", registry, client, settings)
        assert len(reqs) == 2
        assert [(r.role.role_id, r.competency.competency_id) for r in reqs] == [
            ("backend_engineer", "lang_go"),
            ("frontend_engineer", "fw_react"),
        ]
        assert stats.dropped_schema_invalid == 1

    async def test_retry_with_specific_error_feedback(self):
        """Attempt 1 has an invalid role-competency pair -> Attempt 2 receives error text with allowed roles."""
        brief_text = "Бэкенд на Go и REST API. Срок — 8 недель."
        attempt1_items = [
            {
                "role_id": "backend_engineer",
                "competency_id": "lang_go",
                "quote": "Бэкенд на Go",
                "level": 3,
                "criticality": "must",
            },
            {
                # Invalid pair: qa_engineer is not in lang_go.roles
                "role_id": "qa_engineer",
                "competency_id": "lang_go",
                "quote": "Бэкенд на Go",
                "level": 3,
                "criticality": "must",
            },
        ]
        attempt2_items = [
            {
                "role_id": "backend_engineer",
                "competency_id": "api_rest",
                "quote": "REST API",
                "level": 3,
                "criticality": "must",
            },
        ]
        resp1 = LLMResponse(
            content=json.dumps(attempt1_items, ensure_ascii=False),
            tokens_in=100,
            tokens_out=50,
            model="test",
        )
        resp2 = LLMResponse(
            content=json.dumps(attempt2_items, ensure_ascii=False),
            tokens_in=150,
            tokens_out=40,
            model="test",
        )
        client = MockLLMClient([resp1, resp2])
        registry = get_registry()
        settings = _make_settings(max_retries=2)

        reqs, stats = await llm_extract(brief_text, "ru", registry, client, settings)
        assert stats.retries == 1
        assert len(client.calls) == 2
        # Verify the retry prompt includes the specific invalid pair error and allowed roles
        retry_user_msg = client.calls[1]["messages"][-1]["content"]
        assert "qa_engineer" in retry_user_msg
        assert "lang_go" in retry_user_msg
        assert "Allowed roles" in retry_user_msg
        # Valid item from attempt 1 + fixed item from attempt 2 are both present
        pairs = {(r.role.role_id, r.competency.competency_id) for r in reqs}
        assert pairs == {
            ("backend_engineer", "lang_go"),
            ("backend_engineer", "api_rest"),
        }

    async def test_truncation_increases_max_tokens_on_retry(self):
        """When finish_reason=='length', retry increases max_tokens."""
        brief_text = "Бэкенд на Go (middle). Срок — 8 недель."
        resp1 = LLMResponse(
            content='[{"role_id": "backend_engineer", "competency_id": "lang_go"',
            tokens_in=100,
            tokens_out=2048,
            model="test",
            finish_reason="length",
        )
        resp2 = LLMResponse(
            content=json.dumps(
                [
                    {
                        "role_id": "backend_engineer",
                        "competency_id": "lang_go",
                        "quote": "Бэкенд на Go",
                        "level": 3,
                        "criticality": "must",
                    }
                ],
                ensure_ascii=False,
            ),
            tokens_in=120,
            tokens_out=50,
            model="test",
            finish_reason="stop",
        )
        client = MockLLMClient([resp1, resp2])
        registry = get_registry()
        settings = _make_settings(max_retries=2)

        reqs, stats = await llm_extract(brief_text, "ru", registry, client, settings)
        assert len(reqs) == 1
        assert stats.retries == 1
        assert client.calls[1]["max_tokens"] > client.calls[0]["max_tokens"]

    async def test_llm_errors_propagate_not_swallowed(self):
        """LLMTimeoutError, LLMUnavailableError, and HTTP 500 LLMErrorResponse must propagate."""
        registry = get_registry()
        settings = _make_settings()

        for err_cls, err_inst in [
            (LLMTimeoutError, LLMTimeoutError("Request timed out")),
            (LLMUnavailableError, LLMUnavailableError("Connection refused")),
            (LLMErrorResponse, LLMErrorResponse("HTTP 502 Bad Gateway", status_code=502)),
        ]:
            client = MockLLMClient([err_inst])
            with pytest.raises(err_cls):
                await llm_extract("Нужен Go разработчик", "ru", registry, client, settings)

    async def test_max_retries_exhausted(self):
        """All responses invalid JSON -> returns empty + retries=2 + json_valid=False."""
        bad_resp = LLMResponse(content="garbage", tokens_in=50, tokens_out=10, model="test")
        client = MockLLMClient([bad_resp, bad_resp, bad_resp])
        registry = get_registry()
        settings = _make_settings()

        reqs, stats = await llm_extract("test", "ru", registry, client, settings)
        assert reqs == []
        assert stats.retries == 2
        assert stats.json_valid is False

    async def test_all_replay_fixtures_parse(self):
        """All 17 replay fixtures parse and validate against their corresponding gold brief.md."""
        registry = get_registry()
        settings = _make_settings()
        brief_dirs = sorted(GOLD_DIR.glob("brief_*"))
        assert len(brief_dirs) == 17

        for brief_dir in brief_dirs:
            num = brief_dir.name.split("_")[1]
            fixture_path = FIXTURES_DIR / f"brief_{num}_response.json"
            assert fixture_path.exists(), f"Missing fixture: {fixture_path}"
            fixture = json.loads(fixture_path.read_text(encoding="utf-8"))
            brief_text = (brief_dir / "brief.md").read_text(encoding="utf-8")

            resp = LLMResponse(
                content=fixture["content"],
                tokens_in=fixture["tokens_in"],
                tokens_out=fixture["tokens_out"],
                model=fixture["model"],
            )
            client = MockLLMClient([resp])

            reqs, stats = await llm_extract(
                brief_text, "ru", registry, client, settings
            )
            assert stats.json_valid is True, f"Fixture brief_{num} failed JSON validation"
            assert len(reqs) > 0, f"Fixture brief_{num} returned 0 requirements"
            # Verify every quote is a literal substring of brief_text
            for r in reqs:
                for ev in r.evidence:
                    assert ev.quote in brief_text
                    assert brief_text[ev.char_start : ev.char_end] == ev.quote
