"""Pydantic v2 models matching contracts/ml-openapi.yaml for /v1/extract.

All models enforce additionalProperties=false via ConfigDict(extra="forbid").
Field names, types, constraints and patterns mirror the OpenAPI contract exactly.
"""

from __future__ import annotations

import re
from typing import Literal, Optional

from pydantic import BaseModel, ConfigDict, Field, field_validator

# ---------------------------------------------------------------------------
# Reusable literal / constrained types
# ---------------------------------------------------------------------------

WEEK_KEY_RE = re.compile(r"^[0-9]{4}-W(0[1-9]|[1-4][0-9]|5[0-3])$")
REQUEST_ID_RE = re.compile(r"^req_[A-Za-z0-9-]{6,32}$")
REQUIREMENT_ID_RE = re.compile(r"^[A-Za-z0-9_-]{4,40}$")
PROBLEM_CODE_RE = re.compile(r"^[A-Z][A-Z0-9_]{2,63}$")

# ---------------------------------------------------------------------------
# Common building blocks
# ---------------------------------------------------------------------------


class _FrozenBase(BaseModel):
    """Base config: no extras, validate assignment off (immutable after build)."""

    model_config = ConfigDict(extra="forbid", frozen=False)


class EvidenceSpan(_FrozenBase):
    """Literal span of brief.text (ml-openapi.yaml EvidenceSpan)."""

    kind: Literal["brief_span", "taxonomy_synonym", "project_history", "user_edit"]
    char_start: Optional[int] = Field(default=None, ge=0)
    char_end: Optional[int] = Field(default=None, ge=1)
    quote: str = Field(min_length=3, max_length=1000)


class EffortRange(_FrozenBase):
    """Three-point estimate; a single number is never accepted."""

    min_person_days: float = Field(ge=0.5, le=2000)
    likely_person_days: float = Field(ge=0.5, le=2000)
    max_person_days: float = Field(ge=0.5, le=2000)
    unit: Literal["person_days"] = "person_days"

    @field_validator("max_person_days")
    @classmethod
    def _max_gte_min(cls, v: float, info) -> float:
        min_v = info.data.get("min_person_days")
        likely_v = info.data.get("likely_person_days")
        if min_v is not None and v < min_v:
            raise ValueError(f"max_person_days ({v}) must be >= min_person_days ({min_v})")
        if likely_v is not None and v < likely_v:
            raise ValueError(f"max_person_days ({v}) must be >= likely_person_days ({likely_v})")
        return v

    @field_validator("likely_person_days")
    @classmethod
    def _likely_gte_min(cls, v: float, info) -> float:
        min_v = info.data.get("min_person_days")
        if min_v is not None and v < min_v:
            raise ValueError(f"likely_person_days ({v}) must be >= min_person_days ({min_v})")
        return v


class ExtractRole(_FrozenBase):
    role_id: str
    name_ru: str = Field(max_length=120)
    match: Literal["exact_synonym", "embedding", "llm", "hybrid"]
    confidence: float = Field(ge=0, le=1)


class CompetencyCandidate(_FrozenBase):
    competency_id: str
    score: float = Field(ge=0, le=1)


class ExtractCompetency(_FrozenBase):
    competency_id: str
    name_ru: str = Field(max_length=120)
    match: Literal["exact_synonym", "embedding", "llm", "hybrid"]
    confidence: float = Field(ge=0, le=1)
    candidates: list[CompetencyCandidate] = Field(default_factory=list, max_length=10)


class ExtractedRequirement(_FrozenBase):
    """A single extracted requirement (ml-openapi.yaml ExtractedRequirement)."""

    requirement_id: str = Field(pattern=r"^[A-Za-z0-9_-]{4,40}$")
    role: ExtractRole
    competency: Optional[ExtractCompetency] = None
    level: int = Field(ge=1, le=5)
    criticality: Literal["must", "should", "nice"]
    headcount: int = Field(ge=1, le=20)
    effort: EffortRange
    assumptions: list[str] = Field(min_length=1, max_length=10)
    confidence: float = Field(ge=0, le=1)
    needs_review: bool
    evidence: list[EvidenceSpan] = Field(min_length=1, max_length=10)

    @field_validator("assumptions")
    @classmethod
    def _assumption_maxlen(cls, v: list[str]) -> list[str]:
        for s in v:
            if len(s) > 300:
                raise ValueError(f"assumption too long ({len(s)} > 300 chars)")
        return v


class ExtractDiagnostics(_FrozenBase):
    engine: Literal["rules", "llm", "hybrid"]
    model: str = Field(max_length=120)
    prompt_version: str = Field(max_length=40)
    temperature: Literal[0] = 0
    duration_ms: int = Field(ge=0)
    json_valid: bool
    retries: int = Field(ge=0, le=2)
    dropped_no_evidence: int = Field(ge=0)
    post_processor_dropped: int = Field(default=0, ge=0, exclude=True)
    llm_tokens_in: Optional[int] = Field(default=None, ge=0)
    llm_tokens_out: Optional[int] = Field(default=None, ge=0)


class BriefPayload(_FrozenBase):
    language: Literal["ru", "en"]
    text: str = Field(min_length=1, max_length=200000)
    source_type: Literal["text", "docx", "pdf", "txt"]


class ExtractTaxonomy(_FrozenBase):
    role_ids: Optional[list[str]] = Field(default=None, max_length=200)
    competency_ids: Optional[list[str]] = Field(default=None, max_length=500)


class ExtractOptions(_FrozenBase):
    max_requirements: int = Field(default=30, ge=1, le=60)
    min_confidence: float = Field(default=0.55, ge=0, le=1)
    return_candidates: bool = True
    timeout_ms: int = Field(default=20000, ge=1000, le=60000)


class ExtractRequest(_FrozenBase):
    request_id: str = Field(pattern=r"^req_[A-Za-z0-9-]{6,32}$")
    brief_id: str = Field(max_length=40)
    locale: Literal["ru", "en"]
    brief: BriefPayload
    taxonomy: ExtractTaxonomy
    options: ExtractOptions


class ExtractResponse(_FrozenBase):
    request_id: str = Field(pattern=r"^req_[A-Za-z0-9-]{6,32}$")
    brief_id: str
    requirements: list[ExtractedRequirement] = Field(max_length=60)
    diagnostics: ExtractDiagnostics
    warnings: list[str] = Field(default_factory=list)
    reason_codes: list[str] = Field(default_factory=list)

    @field_validator("warnings")
    @classmethod
    def _warning_maxlen(cls, v: list[str]) -> list[str]:
        for s in v:
            if len(s) > 300:
                raise ValueError(f"warning too long ({len(s)} > 300 chars)")
        return v


# ---------------------------------------------------------------------------
# Error models
# ---------------------------------------------------------------------------


class FieldError(_FrozenBase):
    field: str = Field(max_length=200)
    code: str = Field(pattern=r"^[A-Z][A-Z0-9_]{2,63}$")
    detail: str = Field(max_length=500)


class Problem(_FrozenBase):
    type: str  # URI
    title: str = Field(max_length=200)
    status: int = Field(ge=400, le=599)
    code: str = Field(pattern=r"^[A-Z][A-Z0-9_]{2,63}$")
    detail: str = Field(max_length=2000)
    request_id: str = Field(pattern=r"^req_[A-Za-z0-9-]{6,32}$")
    retryable: bool
    reason_codes: list[str]
    errors: Optional[list[FieldError]] = None
