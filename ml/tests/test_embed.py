from __future__ import annotations

import pytest
import httpx

from app.embed import EMBED_DIM, EMBED_MODEL, embed_text
from app.main import app


def test_embedding_is_deterministic_and_normalized() -> None:
    first = embed_text("Миграция на Go и PostgreSQL")
    assert first == embed_text("Миграция на Go и PostgreSQL")
    assert len(first) == EMBED_DIM
    assert sum(x * x for x in first) == pytest.approx(1.0)


@pytest.mark.asyncio
async def test_embed_endpoint_returns_order_and_model() -> None:
    transport = httpx.ASGITransport(app=app)
    async with httpx.AsyncClient(transport=transport, base_url="http://test") as client:
        response = await client.post("/v1/embed", json={
            "request_id": "req_embed01",
            "items": [
                {"item_id": "b", "kind": "brief", "text": "Go"},
                {"item_id": "a", "kind": "competency_name", "text": "Python"},
            ],
        })
    assert response.status_code == 200
    body = response.json()
    assert body["model"] == EMBED_MODEL
    assert body["dim"] == EMBED_DIM
    assert [item["item_id"] for item in body["vectors"]] == ["b", "a"]


@pytest.mark.asyncio
async def test_embed_rejects_pinned_unknown_model() -> None:
    transport = httpx.ASGITransport(app=app)
    async with httpx.AsyncClient(transport=transport, base_url="http://test") as client:
        response = await client.post("/v1/embed", json={
            "request_id": "req_embed01",
            "model": "unknown",
            "items": [{"item_id": "a", "kind": "brief", "text": "Go"}],
        })
    assert response.status_code == 422
