"""Team Optimizer ML Service stub (phase 1.3).

Implements /healthz, /readyz and returns 501 for the three optimizer endpoints
(/v1/extract, /v1/embed, /v1/optimize) which will be implemented in phase 3.
All error responses conform to the Problem schema from contracts/ml-openapi.yaml.
"""

from __future__ import annotations

import time
from typing import Any

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse

from .version import __version__

app = FastAPI(
    title="Team Optimizer ML Service",
    version=__version__,
    description="Stub for phase 1.3. Optimizer endpoints return 501.",
)

_start_time: float = time.time()


def _req_id(request: Request) -> str:
    """Return X-Request-Id from headers, or a deterministic stub placeholder.

    The Problem schema requires request_id matching ^req_[A-Za-z0-9-]{6,32}$.
    """
    hdr = request.headers.get("x-request-id", "")
    if hdr and len(hdr) >= 10 and hdr.startswith("req_"):
        return hdr
    return "req_stub00000001"


def _problem(
    status: int,
    code: str,
    title: str,
    detail: str,
    request_id: str,
    retryable: bool = False,
    reason_codes: list[str] | None = None,
) -> dict[str, Any]:
    """Build a Problem response body matching contracts/ml-openapi.yaml."""
    return {
        "type": f"https://teamopt.internal/ml/problems/{code.lower().replace('_', '-')}",
        "title": title,
        "status": status,
        "code": code,
        "detail": detail,
        "request_id": request_id,
        "retryable": retryable,
        "reason_codes": reason_codes if reason_codes is not None else [],
    }


@app.get("/healthz")
async def healthz() -> dict[str, Any]:
    """Liveness probe per contracts/ml-openapi.yaml."""
    return {
        "status": "ok",
        "service": "ml-service",
        "version": __version__,
        "uptime_s": int(time.time() - _start_time),
    }


@app.get("/readyz")
async def readyz() -> dict[str, Any]:
    """Readiness probe per contracts/ml-openapi.yaml.

    In the stub phase all capabilities are 'degraded' because the underlying
    models/solver are not yet loaded. The service itself is ready to receive
    traffic (it answers), so HTTP 200 is returned with degraded capability states.
    """
    return {
        "ready": True,
        "capabilities": {
            "extract": "degraded",
            "embed": "degraded",
            "optimize": "degraded",
        },
        "models": {
            "llm": None,
            "embedding": "multilingual-e5-large",
            "embedding_dim": 1024,
            "spacy_pipeline": "ru_core_news_lg",
        },
        "solver": {
            "name": "ortools-cp-sat",
            "version": "9.11.4210",
        },
        "limits": {
            "extract_timeout_ms": 20000,
            "embed_timeout_ms": 5000,
            "optimize_max_time_limit_ms": 60000,
            "max_requirements": 60,
            "max_candidates": 2000,
            "max_weeks": 104,
        },
    }


# ---------------------------------------------------------------------------
# Stub endpoints: return 501 Not Implemented per contracts/ml-openapi.yaml
# Problem schema. Will be replaced by real implementations in phase 3.
# ---------------------------------------------------------------------------

@app.post("/v1/extract")
async def extract(request: Request) -> JSONResponse:
    """Stub: brief extraction not yet implemented."""
    return JSONResponse(
        status_code=501,
        content=_problem(
            status=501,
            code="NOT_IMPLEMENTED",
            title="Not implemented",
            detail="POST /v1/extract is a stub; implementation comes in phase 3.",
            request_id=_req_id(request),
            retryable=False,
        ),
    )


@app.post("/v1/embed")
async def embed(request: Request) -> JSONResponse:
    """Stub: text embedding not yet implemented."""
    return JSONResponse(
        status_code=501,
        content=_problem(
            status=501,
            code="NOT_IMPLEMENTED",
            title="Not implemented",
            detail="POST /v1/embed is a stub; implementation comes in phase 3.",
            request_id=_req_id(request),
            retryable=False,
        ),
    )


@app.post("/v1/optimize")
async def optimize(request: Request) -> JSONResponse:
    """Stub: team optimization not yet implemented."""
    return JSONResponse(
        status_code=501,
        content=_problem(
            status=501,
            code="NOT_IMPLEMENTED",
            title="Not implemented",
            detail="POST /v1/optimize is a stub; implementation comes in phase 3.",
            request_id=_req_id(request),
            retryable=False,
        ),
    )
