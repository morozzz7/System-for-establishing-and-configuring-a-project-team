"""Team Optimizer ML Service.

Implements /healthz, /readyz, and POST /v1/extract (real extraction pipeline).
/v1/embed and /v1/optimize remain stubs (501) for phases 3.2/3.3.
All error responses conform to the Problem schema from contracts/ml-openapi.yaml.
"""

from __future__ import annotations

import time
from typing import Any

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse

from .extract.router import router as extract_router
from .llm.config import get_settings
from .version import __version__

app = FastAPI(
    title="Team Optimizer ML Service",
    version=__version__,
    description="ML service for team formation: extraction (implemented), embedding and optimization (stubs).",
)

# Include the real /v1/extract router
app.include_router(extract_router)

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
    
    Reports actual capability states based on LLM availability.
    """
    settings = get_settings()
    
    # Determine extract capability state
    extract_state = "ready" if settings.is_available else "degraded"
    
    return {
        "ready": True,
        "capabilities": {
            "extract": extract_state,
            "embed": "degraded",
            "optimize": "degraded",
        },
        "models": {
            "llm": settings.model if settings.is_available else None,
            "embedding": "multilingual-e5-large",
            "embedding_dim": 1024,
            "spacy_pipeline": "ru_core_news_lg",
        },
        "solver": {
            "name": "ortools-cp-sat",
            "version": "9.11.4210",
        },
        "limits": {
            "extract_timeout_ms": settings.timeout_ms,
            "embed_timeout_ms": 5000,
            "optimize_max_time_limit_ms": 60000,
            "max_requirements": 60,
            "max_candidates": 2000,
            "max_weeks": 104,
        },
    }


# ---------------------------------------------------------------------------
# Stub endpoints: /v1/embed and /v1/optimize return 501 Not Implemented
# (they belong to phases 3.2 and 3.3). /v1/extract is now real (via router).
# ---------------------------------------------------------------------------


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
