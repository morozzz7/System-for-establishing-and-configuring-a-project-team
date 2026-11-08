"""FastAPI router for POST /v1/extract (LLM-first with deterministic validation).

Per ADR-005 v2 (`tz_ml_extract_fix_v2.md` §3.7):
- `RulesEngine` and `Merger` are removed from the `/v1/extract` request path.
- LLM is the sole source of extraction decisions (`diagnostics.engine = "llm"`).
- Deterministic 6-stage validation enforces schema, taxonomy, literal quote evidence,
  `role_id in competency.roles`, deduplication, and `min_confidence`/`max_requirements`.
- When LLM is unavailable or fails, returns an honest `Problem` (`503 CAPABILITY_UNAVAILABLE`
  with `ML_LLM_UNAVAILABLE` or `408 TIMEOUT` with `ML_TIMEOUT`, `retryable=True`).
"""

from __future__ import annotations

import time
from typing import Any

from fastapi import APIRouter, Request
from fastapi.responses import JSONResponse

from ..llm.client import LLMClient, LLMError, LLMTimeoutError, LLMUnavailableError
from ..llm.config import get_settings
from .diagnostics import build_diagnostics
from .llm_extract import LLMStats, llm_extract
from .schemas import ExtractRequest, ExtractResponse
from .taxonomy import get_registry
from .validator import EvidenceValidator

router = APIRouter()


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


def _echo_headers(request: Request) -> dict[str, str]:
    """Echo X-Request-Id header when present."""
    hdr = request.headers.get("x-request-id")
    return {"X-Request-Id": hdr} if hdr else {}


@router.post("/v1/extract")
async def extract_handler(request: Request) -> JSONResponse:
    """POST /v1/extract: brief -> structured requirements (LLM-first)."""
    t_start = time.monotonic()
    resp_headers = _echo_headers(request)

    # Parse request body
    try:
        body = await request.json()
    except Exception:
        req_id = request.headers.get("x-request-id", "req_stub00000001")
        if not req_id.startswith("req_") or len(req_id) < 10:
            req_id = "req_stub00000001"
        return JSONResponse(
            status_code=422,
            headers=resp_headers,
            content=_problem(
                status=422,
                code="INVALID_REQUEST",
                title="Invalid request body",
                detail="Request body is not valid JSON",
                request_id=req_id,
                reason_codes=["VALIDATION_FAILED"],
            ),
        )

    # Validate against ExtractRequest schema
    try:
        req = ExtractRequest.model_validate(body)
    except Exception as exc:
        req_id = (
            body.get("request_id", "req_stub00000001")
            if isinstance(body, dict)
            else "req_stub00000001"
        )
        if (
            not isinstance(req_id, str)
            or not req_id.startswith("req_")
            or len(req_id) < 10
        ):
            req_id = "req_stub00000001"
        return JSONResponse(
            status_code=422,
            headers=resp_headers,
            content=_problem(
                status=422,
                code="INVALID_REQUEST",
                title="Validation error",
                detail=str(exc)[:2000],
                request_id=req_id,
                reason_codes=["VALIDATION_FAILED"],
            ),
        )

    request_id = req.request_id
    brief_text = req.brief.text
    locale = req.locale
    warnings: list[str] = []
    reason_codes: list[str] = []

    settings = get_settings()
    if not settings.is_available:
        return JSONResponse(
            status_code=503,
            headers=resp_headers,
            content=_problem(
                status=503,
                code="CAPABILITY_UNAVAILABLE",
                title="Capability unavailable",
                detail="LLM_API_KEY is not configured; /v1/extract requires an available LLM endpoint.",
                request_id=request_id,
                retryable=True,
                reason_codes=["ML_LLM_UNAVAILABLE"],
            ),
        )

    registry = get_registry()

    try:
        client = LLMClient(settings)
        llm_reqs, llm_stats = await llm_extract(
            brief_text,
            locale,
            registry,
            client,
            settings,
            allowed_role_ids=req.taxonomy.role_ids,
            allowed_competency_ids=req.taxonomy.competency_ids,
            max_requirements=req.options.max_requirements,
            min_confidence=req.options.min_confidence,
            return_candidates=req.options.return_candidates,
            timeout_ms=req.options.timeout_ms,
        )
    except LLMTimeoutError as exc:
        return JSONResponse(
            status_code=408,
            headers=resp_headers,
            content=_problem(
                status=408,
                code="TIMEOUT",
                title="Request timed out",
                detail=str(exc)[:2000],
                request_id=request_id,
                retryable=True,
                reason_codes=["ML_TIMEOUT"],
            ),
        )
    except (LLMUnavailableError, LLMError) as exc:
        return JSONResponse(
            status_code=503,
            headers=resp_headers,
            content=_problem(
                status=503,
                code="CAPABILITY_UNAVAILABLE",
                title="Capability unavailable",
                detail=str(exc)[:2000],
                request_id=request_id,
                retryable=True,
                reason_codes=["ML_LLM_UNAVAILABLE"],
            ),
        )

    # Re-verify through deterministic 6-stage validator
    validator = EvidenceValidator()
    valid, pipe_stats = validator.validate_pipeline(
        llm_reqs,
        brief_text,
        registry,
        allowed_role_ids=req.taxonomy.role_ids,
        allowed_competency_ids=req.taxonomy.competency_ids,
        min_confidence=req.options.min_confidence,
        max_requirements=req.options.max_requirements,
    )

    dropped_no_evidence = llm_stats.dropped_no_evidence + pipe_stats.dropped_no_evidence
    post_processor_dropped = (
        llm_stats.post_processor_dropped + pipe_stats.post_processor_dropped
    )

    if post_processor_dropped > 0:
        warnings.append(
            f"Deterministic validation dropped {post_processor_dropped} requirement(s) (taxonomy/pair/dedup/limit)"
        )
        if "REQ_TAXONOMY_LOW_CONFIDENCE" not in reason_codes:
            reason_codes.append("REQ_TAXONOMY_LOW_CONFIDENCE")

    if dropped_no_evidence > 0:
        warnings.append(
            f"Evidence gate dropped {dropped_no_evidence} requirement(s) with unverified quote(s)"
        )

    if any(r.needs_review for r in valid):
        if "REQ_NEEDS_REVIEW" not in reason_codes:
            reason_codes.append("REQ_NEEDS_REVIEW")

    if any(r.confidence < 0.7 for r in valid):
        if "REQ_LOW_CONFIDENCE" not in reason_codes:
            reason_codes.append("REQ_LOW_CONFIDENCE")

    if not valid:
        if not llm_stats.json_valid:
            warnings.append(
                "LLM output failed JSON/schema validation after retries; 0 requirements returned"
            )
            if "ML_SCHEMA_INVALID" not in reason_codes:
                reason_codes.append("ML_SCHEMA_INVALID")
        else:
            warnings.append(
                "No valid requirements survived deterministic validation for this brief"
            )
            if "REQ_LOW_CONFIDENCE" not in reason_codes:
                reason_codes.append("REQ_LOW_CONFIDENCE")

    duration_ms = int((time.monotonic() - t_start) * 1000)
    diagnostics = build_diagnostics(
        llm_stats=llm_stats,
        engine="llm",
        model=settings.model,
        duration_ms=duration_ms,
        dropped_no_evidence=dropped_no_evidence,
        post_processor_dropped=post_processor_dropped,
    )

    response = ExtractResponse(
        request_id=request_id,
        brief_id=req.brief_id,
        requirements=valid,
        diagnostics=diagnostics,
        warnings=warnings,
        reason_codes=reason_codes,
    )

    return JSONResponse(
        content=response.model_dump(),
        headers=resp_headers,
    )
