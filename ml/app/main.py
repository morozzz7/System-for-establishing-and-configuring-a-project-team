"""Team Optimizer ML Service.

Implements /healthz, /readyz, POST /v1/extract (extraction pipeline) and
POST /v1/optimize (CP-SAT team optimizer). /v1/embed remains a stub (501) for phase 3.2.
All error responses conform to the Problem schema from contracts/ml-openapi.yaml.
"""

from __future__ import annotations

import time
from typing import Any

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse

from .extract.router import router as extract_router
from .llm.config import get_settings
from .optimize.router import router as optimize_router
from .version import __version__

try:
    from .optimize.solver import SOLVER_NAME, SOLVER_VERSION

    _OPTIMIZE_READY = True
except Exception:  # pragma: no cover - ortools is a hard dependency
    SOLVER_NAME = "ortools-cp-sat"
    SOLVER_VERSION = "9.11.4210"
    _OPTIMIZE_READY = False

app = FastAPI(
    title="Team Optimizer ML Service",
    version=__version__,
    description="ML service for team formation: extraction + optimization implemented; embedding stub.",
)

# Include the real /v1/extract and /v1/optimize routers
app.include_router(extract_router)
app.include_router(optimize_router)

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

    Reports actual capability states based on LLM availability and OR-Tools import.
    """
    settings = get_settings()

    # Determine extract capability state
    extract_state = "ready" if settings.is_available else "degraded"
    optimize_state = "ready" if _OPTIMIZE_READY else "unavailable"

    return {
        "ready": True,
        "capabilities": {
            "extract": extract_state,
            "embed": "degraded",
            "optimize": optimize_state,
        },
        "models": {
            "llm": settings.model if settings.is_available else None,
            "embedding": "multilingual-e5-large",
            "embedding_dim": 1024,
            "spacy_pipeline": "ru_core_news_lg",
        },
        "solver": {
            "name": SOLVER_NAME,
            "version": SOLVER_VERSION,
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
# Stub endpoint: /v1/embed returns 501 Not Implemented (phase 3.2).
# /v1/extract and /v1/optimize are real (via routers).
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
            detail="POST /v1/embed is a stub; implementation comes in phase 3.2.",
            request_id=_req_id(request),
            retryable=False,
        ),
    )
