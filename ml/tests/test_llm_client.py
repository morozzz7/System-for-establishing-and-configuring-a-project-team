"""Tests for LLM HTTP client with mocked HTTP responses."""

from __future__ import annotations

import json

import httpx
import pytest

from app.llm.client import (
    LLMClient,
    LLMErrorResponse,
    LLMTimeoutError,
    LLMUnavailableError,
)
from app.llm.config import LLMSettings


def _make_settings(api_key: str = "test-key-123", **kwargs) -> LLMSettings:
    return LLMSettings(api_key=api_key, **kwargs)


def _success_response(content: str = '[{"test": true}]') -> dict:
    return {
        "choices": [{"message": {"content": content}}],
        "usage": {"prompt_tokens": 100, "completion_tokens": 50},
        "model": "qwen/qwen-2.5-72b-instruct",
    }


class MockTransport(httpx.AsyncBaseTransport):
    """Configurable mock HTTP transport."""

    def __init__(self, handler):
        self._handler = handler
        self.last_request: httpx.Request | None = None

    async def handle_async_request(self, request: httpx.Request) -> httpx.Response:
        self.last_request = request
        return await self._handler(request)


@pytest.mark.asyncio
class TestLLMClientSuccess:
    async def test_valid_response_parsed(self):
        content = '[{"requirement_id": "req_test"}]'

        async def handler(request):
            return httpx.Response(200, json=_success_response(content))

        transport = MockTransport(handler)
        client = LLMClient(_make_settings())
        # Patch httpx.AsyncClient to use mock transport
        orig_init = httpx.AsyncClient.__init__

        def patched_init(self_client, **kwargs):
            kwargs["transport"] = transport
            orig_init(self_client, **kwargs)

        httpx.AsyncClient.__init__ = patched_init
        try:
            resp = await client.chat([{"role": "user", "content": "hi"}], temperature=0, seed=42)
        finally:
            httpx.AsyncClient.__init__ = orig_init

        assert resp.content == content
        assert resp.tokens_in == 100
        assert resp.tokens_out == 50
        assert resp.model == "qwen/qwen-2.5-72b-instruct"

    async def test_headers_include_authorization(self):
        async def handler(request):
            assert "authorization" in {k.lower() for k in request.headers}
            assert request.headers["authorization"] == "Bearer test-key-123"
            assert request.headers["http-referer"] == "teamopt-ml-service"
            assert request.headers["x-title"] == "TeamOpt ML"
            return httpx.Response(200, json=_success_response("[]"))

        transport = MockTransport(handler)
        client = LLMClient(_make_settings())
        orig_init = httpx.AsyncClient.__init__

        def patched_init(self_client, **kwargs):
            kwargs["transport"] = transport
            orig_init(self_client, **kwargs)

        httpx.AsyncClient.__init__ = patched_init
        try:
            await client.chat([{"role": "user", "content": "test"}])
        finally:
            httpx.AsyncClient.__init__ = orig_init

    async def test_request_body_matches_openai_schema(self):
        async def handler(request):
            body = json.loads(request.content)
            assert "model" in body
            assert "messages" in body
            assert "temperature" in body
            assert body["temperature"] == 0
            assert body["seed"] == 42
            return httpx.Response(200, json=_success_response("[]"))

        transport = MockTransport(handler)
        client = LLMClient(_make_settings())
        orig_init = httpx.AsyncClient.__init__

        def patched_init(self_client, **kwargs):
            kwargs["transport"] = transport
            orig_init(self_client, **kwargs)

        httpx.AsyncClient.__init__ = patched_init
        try:
            await client.chat(
                [{"role": "user", "content": "test"}],
                temperature=0,
                seed=42,
            )
        finally:
            httpx.AsyncClient.__init__ = orig_init


@pytest.mark.asyncio
class TestLLMClientErrors:
    async def test_http_500_raises_error_response(self):
        async def handler(request):
            return httpx.Response(500, text="Internal Server Error")

        transport = MockTransport(handler)
        client = LLMClient(_make_settings())
        orig_init = httpx.AsyncClient.__init__

        def patched_init(self_client, **kwargs):
            kwargs["transport"] = transport
            orig_init(self_client, **kwargs)

        httpx.AsyncClient.__init__ = patched_init
        try:
            with pytest.raises(LLMErrorResponse) as exc_info:
                await client.chat([{"role": "user", "content": "test"}])
            assert exc_info.value.status_code == 500
        finally:
            httpx.AsyncClient.__init__ = orig_init

    async def test_connection_refused_raises_unavailable(self):
        async def handler(request):
            raise httpx.ConnectError("Connection refused")

        transport = MockTransport(handler)
        client = LLMClient(_make_settings())
        orig_init = httpx.AsyncClient.__init__

        def patched_init(self_client, **kwargs):
            kwargs["transport"] = transport
            orig_init(self_client, **kwargs)

        httpx.AsyncClient.__init__ = patched_init
        try:
            with pytest.raises(LLMUnavailableError):
                await client.chat([{"role": "user", "content": "test"}])
        finally:
            httpx.AsyncClient.__init__ = orig_init

    async def test_timeout_raises_timeout_error(self):
        async def handler(request):
            raise httpx.TimeoutException("timed out")

        transport = MockTransport(handler)
        client = LLMClient(_make_settings())
        orig_init = httpx.AsyncClient.__init__

        def patched_init(self_client, **kwargs):
            kwargs["transport"] = transport
            orig_init(self_client, **kwargs)

        httpx.AsyncClient.__init__ = patched_init
        try:
            with pytest.raises(LLMTimeoutError):
                await client.chat([{"role": "user", "content": "test"}])
        finally:
            httpx.AsyncClient.__init__ = orig_init

    async def test_malformed_json_body_raises_error_response(self):
        async def handler(request):
            return httpx.Response(200, text="not json")

        transport = MockTransport(handler)
        client = LLMClient(_make_settings())
        orig_init = httpx.AsyncClient.__init__

        def patched_init(self_client, **kwargs):
            kwargs["transport"] = transport
            orig_init(self_client, **kwargs)

        httpx.AsyncClient.__init__ = patched_init
        try:
            with pytest.raises(LLMErrorResponse):
                await client.chat([{"role": "user", "content": "test"}])
        finally:
            httpx.AsyncClient.__init__ = orig_init

    async def test_no_api_key_raises_unavailable(self):
        with pytest.raises(LLMUnavailableError):
            LLMClient(_make_settings(api_key=""))
