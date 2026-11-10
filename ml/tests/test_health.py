"""Smoke tests for ML service health and stub endpoints."""

from __future__ import annotations

import pytest
import pytest_asyncio
from httpx import ASGITransport, AsyncClient

from app.main import app


@pytest_asyncio.fixture()
async def client():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as c:
        yield c


@pytest.mark.asyncio
async def test_healthz(client: AsyncClient) -> None:
    resp = await client.get("/healthz")
    assert resp.status_code == 200
    body = resp.json()
    assert body["status"] == "ok"
    assert body["service"] == "ml-service"
    assert body["version"] == "1.0.0"
    assert isinstance(body["uptime_s"], int)


@pytest.mark.asyncio
async def test_readyz(client: AsyncClient) -> None:
    resp = await client.get("/readyz")
    assert resp.status_code == 200
    body = resp.json()
    assert body["ready"] is True
    assert "extract" in body["capabilities"]
    assert "solver" in body
    assert "limits" in body


@pytest.mark.asyncio
async def test_extract_invalid_body_422(client: AsyncClient) -> None:
    """Real /v1/extract returns 422 on invalid request body (was 501 stub)."""
    resp = await client.post("/v1/extract", json={})
    assert resp.status_code == 422
    body = resp.json()
    assert body["status"] == 422
    assert body["code"] == "INVALID_REQUEST"
    assert "request_id" in body
    assert isinstance(body["reason_codes"], list)


@pytest.mark.asyncio
async def test_embed_stub_501(client: AsyncClient) -> None:
    resp = await client.post("/v1/embed", json={})
    assert resp.status_code == 501
    body = resp.json()
    assert body["code"] == "NOT_IMPLEMENTED"


@pytest.mark.asyncio
async def test_optimize_stub_501(client: AsyncClient) -> None:
    resp = await client.post("/v1/optimize", json={})
    assert resp.status_code == 501
    body = resp.json()
    assert body["code"] == "NOT_IMPLEMENTED"


@pytest.mark.asyncio
async def test_stub_respects_request_id_header(client: AsyncClient) -> None:
    """Stub endpoints (/v1/embed) echo X-Request-Id in Problem response."""
    resp = await client.post(
        "/v1/embed",
        json={},
        headers={"X-Request-Id": "req_test00000001"},
    )
    assert resp.status_code == 501
    body = resp.json()
    assert body["request_id"] == "req_test00000001"
