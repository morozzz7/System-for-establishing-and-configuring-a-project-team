"""Deterministic dependency-free embeddings for taxonomy normalization."""

from __future__ import annotations

import hashlib
import math
import re
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

EMBED_MODEL = "hashing-char-ngram-v1"
EMBED_DIM = 1024
_WORD_RE = re.compile(r"[\w+#./-]+", re.UNICODE)


class EmbedItem(BaseModel):
    model_config = ConfigDict(extra="forbid")
    item_id: str = Field(min_length=1, max_length=64)
    kind: Literal["competency_query", "competency_name", "project_description", "brief", "requirement"]
    text: str = Field(min_length=1, max_length=8000)


class EmbedRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")
    request_id: str = Field(pattern=r"^req_[A-Za-z0-9-]{6,32}$")
    model: str | None = None
    normalize: bool = True
    items: list[EmbedItem] = Field(min_length=1, max_length=256)


class EmbeddingVector(BaseModel):
    item_id: str
    vector: list[float]


class EmbedResponse(BaseModel):
    request_id: str
    model: str
    dim: int
    normalized: bool
    duration_ms: int
    vectors: list[EmbeddingVector]
    reason_codes: list[str]


def _hash_index(value: str) -> tuple[int, float]:
    digest = hashlib.blake2b(value.encode("utf-8"), digest_size=8).digest()
    number = int.from_bytes(digest, "big")
    return number % EMBED_DIM, 1.0 if number >> 63 == 0 else -1.0


def embed_text(text: str, normalize: bool = True) -> list[float]:
    """Return a stable signed character n-gram vector."""
    value = " ".join(text.casefold().replace("ё", "е").split())
    vector = [0.0] * EMBED_DIM
    padded = f"  {value}  "
    for n in (2, 3, 4, 5):
        for i in range(max(0, len(padded) - n + 1)):
            idx, sign = _hash_index(f"{n}:{padded[i:i+n]}")
            vector[idx] += sign
    for token in _WORD_RE.findall(value):
        idx, sign = _hash_index(f"w:{token}")
        vector[idx] += 2.0 * sign
    if normalize:
        norm = math.sqrt(sum(x * x for x in vector))
        if norm:
            vector = [x / norm for x in vector]
    return vector


def embed(request: EmbedRequest, duration_ms: int = 0) -> EmbedResponse:
    if request.model and request.model != EMBED_MODEL:
        raise ValueError(f"unsupported embedding model: {request.model}")
    return EmbedResponse(
        request_id=request.request_id,
        model=EMBED_MODEL,
        dim=EMBED_DIM,
        normalized=request.normalize,
        duration_ms=duration_ms,
        vectors=[EmbeddingVector(item_id=i.item_id, vector=embed_text(i.text, request.normalize)) for i in request.items],
        reason_codes=[],
    )
