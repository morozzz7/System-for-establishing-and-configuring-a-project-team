"""Deterministic fingerprint for /v1/optimize (ml-openapi.yaml `deterministic_fingerprint`).

Spec: ``sha256`` over the canonical JSON of the request + solver version. Same semantic
request + solver version => same fingerprint. Timing fields live in the response only, so
they are excluded by construction.

Canonicalization is ``json.dumps(request.model_dump(mode="json"), sort_keys=True,
separators=(",", ":"))`` so field order in the wire payload never changes the hash.
"""

from __future__ import annotations

import hashlib
import json
from typing import Any

from .schemas import OptimizeRequest


def canonical_request_json(request: OptimizeRequest) -> str:
    """Serialize the validated request to a canonical, order-independent JSON string."""
    data: dict[str, Any] = request.model_dump(mode="json")
    return json.dumps(data, sort_keys=True, separators=(",", ":"), ensure_ascii=False)


def deterministic_fingerprint(request: OptimizeRequest, solver_version: str) -> str:
    """Return ``sha256:<hex>`` over the canonical request + solver version."""
    payload = canonical_request_json(request) + "|" + solver_version
    digest = hashlib.sha256(payload.encode("utf-8")).hexdigest()
    return f"sha256:{digest}"
