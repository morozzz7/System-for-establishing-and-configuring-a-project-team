"""Pydantic v2 models for POST /v1/optimize, mirroring contracts/ml-openapi.yaml.

All models enforce ``additionalProperties: false`` via ``ConfigDict(extra="forbid")``,
exactly like ``app.extract.schemas``. Field names, types, patterns, enums and ranges
match the OpenAPI ``Optimize*`` schemas one-to-one.

The canonical ``ReasonCode`` enum is kept as a module constant (``VALID_REASON_CODES``)
so the solver can guarantee it only ever emits codes present in the contract; the
Pydantic fields themselves stay ``list[str]`` (same convention as the extract router,
which is more forgiving of clients while the service stays strict about what it emits).
"""

from __future__ import annotations

import re
from typing import Literal, Optional

from pydantic import BaseModel, ConfigDict, Field, model_validator

from ..extract.schemas import EffortRange

# ---------------------------------------------------------------------------
# Reusable literal / constrained types
# ---------------------------------------------------------------------------

WEEK_KEY_RE = re.compile(r"^[0-9]{4}-W(0[1-9]|[1-4][0-9]|5[0-3])$")
REQUEST_ID_RE = re.compile(r"^req_[A-Za-z0-9-]{6,32}$")
EMPLOYEE_ID_RE = re.compile(r"^emp_[a-z0-9]{4,12}$")
VARIANT_ID_RE = re.compile(r"^var_[A-Za-z0-9]{2,16}$")
PROBLEM_CODE_RE = re.compile(r"^[A-Z][A-Z0-9_]{2,63}$")

Criticality = Literal["must", "should", "nice"]
Grade = Literal["J", "M", "S", "L", "P"]
EmploymentType = Literal["internal", "contractor", "external_partner"]
SolverStatus = Literal[
    "OPTIMAL", "FEASIBLE", "INFEASIBLE", "UNKNOWN", "TIMEOUT", "MODEL_INVALID", "ERROR"
]
ObjectivePreset = Literal["fast", "economical", "balanced", "low_risk", "custom"]

# Canonical reason-code list from contracts/ml-openapi.yaml `ReasonCode` (ML subset).
# The solver emits only these codes.
VALID_REASON_CODES: frozenset[str] = frozenset(
    {
        "INCLUDE_HIGH_COMPETENCE",
        "INCLUDE_AVAILABLE_CAPACITY",
        "INCLUDE_COST_EFFICIENT",
        "INCLUDE_REQUIRED_ROLE",
        "INCLUDE_BUS_FACTOR",
        "INCLUDE_BENCH",
        "EXCLUDE_OVERLOADED",
        "EXCLUDE_ABSENT",
        "EXCLUDE_BELOW_REQUIRED_LEVEL",
        "EXCLUDE_EXPENSIVE",
        "EXCLUDE_USER_FORCE",
        "EXCLUDE_TEAM_SIZE_LIMIT",
        "EXCLUDE_NO_FREE_CAPACITY",
        "DEFICIT_MUST_ROLE",
        "DEFICIT_EXTERNAL_CONTRACTOR",
        "DEFICIT_UPSKILL_INTERNAL",
        "DEFICIT_DEADLINE_SHIFT",
        "DEFICIT_DESCOPE",
        "CONSTRAINT_ACTIVE_TEAM_SIZE",
        "CONSTRAINT_ACTIVE_BUDGET",
        "CONSTRAINT_ACTIVE_DEADLINE",
        "CONSTRAINT_ACTIVE_FORCE_INCLUDE",
        "CONSTRAINT_ACTIVE_FORCE_EXCLUDE",
        "CONSTRAINT_ACTIVE_BUS_FACTOR",
        "CONSTRAINT_BINDING_CAPACITY",
        "VARIANT_FASTEST",
        "VARIANT_CHEAPEST",
        "VARIANT_LOWEST_RISK",
        "VARIANT_BALANCED",
        "VARIANT_DIVERSITY_TOO_LOW",
        "SOLVER_INFEASIBLE",
        "SOLVER_TIMEOUT",
        "SOLVER_UNKNOWN",
        "REQ_LOW_CONFIDENCE",
        "REQ_NEEDS_REVIEW",
        "REQ_EFFORT_WIDE_RANGE",
        "REQ_TAXONOMY_LOW_CONFIDENCE",
        "NORMALIZE_TOP1_STRONG",
        "NORMALIZE_TOP1_WEAK",
        "ML_TIMEOUT",
        "ML_LLM_UNAVAILABLE",
        "ML_SCHEMA_INVALID",
        "VALIDATION_FAILED",
    }
)


def ensure_valid_reason_codes(codes: list[str]) -> list[str]:
    """Return only codes that exist in the contract enum (defensive, drops unknown)."""
    return [c for c in codes if c in VALID_REASON_CODES]


# ---------------------------------------------------------------------------
# Base config
# ---------------------------------------------------------------------------


class _FrozenBase(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=False)


# ---------------------------------------------------------------------------
# Optimize sub-schemas (input)
# ---------------------------------------------------------------------------


class OptimizeWeights(_FrozenBase):
    """Objective weights. Sum of the provided keys must equal 1.0 +- 1e-6."""

    M1: float = Field(ge=0, le=1)
    M2: float = Field(ge=0, le=1)
    M3: float = Field(ge=0, le=1)
    M4: Optional[float] = Field(default=None, ge=0, le=1)
    M5: float = Field(ge=0, le=1)
    M6: float = Field(ge=0, le=1)
    M7: float = Field(ge=0, le=1)
    M8: float = Field(ge=0, le=1)
    M9: float = Field(ge=0, le=1)
    M10: float = Field(ge=0, le=1)
    M11: float = Field(ge=0, le=1)
    M12: Optional[float] = Field(default=None, ge=0, le=1)

    @model_validator(mode="after")
    def _sum_is_one(self) -> "OptimizeWeights":
        present = [
            getattr(self, k)
            for k in (
                "M1", "M2", "M3", "M4", "M5", "M6", "M7", "M8", "M9", "M10", "M11", "M12",
            )
            if getattr(self, k) is not None
        ]
        total = sum(present)
        if abs(total - 1.0) > 1e-6:
            raise ValueError(
                f"OptimizeWeights must sum to 1.0 +- 1e-6 (got {total:g} over {len(present)} keys)"
            )
        return self

    def get(self, key: str, default: float = 0.0) -> float:
        """Read a weight by metric key, defaulting to 0 when absent (e.g. M4/M12)."""
        val = getattr(self, key, None)
        return default if val is None else val


class OptimizeHorizon(_FrozenBase):
    start_week: str = Field(pattern=WEEK_KEY_RE.pattern)
    end_week: str = Field(pattern=WEEK_KEY_RE.pattern)
    weeks: list[str] = Field(min_length=1, max_length=104)

    @model_validator(mode="after")
    def _weeks_align(self) -> "OptimizeHorizon":
        for w in self.weeks:
            if not WEEK_KEY_RE.match(w):
                raise ValueError(f"Invalid week key {w!r}")
        if self.weeks[0] != self.start_week:
            raise ValueError(
                f"horizon.weeks[0] ({self.weeks[0]}) must equal start_week ({self.start_week})"
            )
        if self.weeks[-1] != self.end_week:
            raise ValueError(
                f"horizon.weeks[-1] ({self.weeks[-1]}) must equal end_week ({self.end_week})"
            )
        return self


class OptimizeRequirement(_FrozenBase):
    requirement_id: str = Field(max_length=40)
    role_id: str
    competency_id: Optional[str] = None
    level: int = Field(ge=1, le=5)
    criticality: Criticality
    headcount: int = Field(ge=1, le=20)
    effort: EffortRange
    earliest_week: Optional[str] = Field(default=None, pattern=WEEK_KEY_RE.pattern)
    latest_week: Optional[str] = Field(default=None, pattern=WEEK_KEY_RE.pattern)
    allows_external: bool = True


class CandidateCompetency(_FrozenBase):
    competency_id: str
    level: int = Field(ge=1, le=5)
    confidence: float = Field(ge=0, le=1)


class CandidateCapacityWeek(_FrozenBase):
    week: str = Field(pattern=WEEK_KEY_RE.pattern)
    working_days: int = Field(ge=0, le=5)
    free_hours: float = Field(ge=0, le=60)
    unavailable: bool


class OptimizeCandidate(_FrozenBase):
    employee_id: str = Field(pattern=EMPLOYEE_ID_RE.pattern)
    grade: Grade
    role_profile_id: str
    employment_type: EmploymentType
    bench_flag: bool
    cost_coefficient: float = Field(ge=0.5, le=5)
    experience_score: Optional[float] = Field(default=None, ge=0, le=1)
    competencies: list[CandidateCompetency] = Field(max_length=200)
    capacity: list[CandidateCapacityWeek] = Field(min_length=1, max_length=104)
    current_project_ids: list[str] = Field(default_factory=list, max_length=50)


class OptimizeConstraints(_FrozenBase):
    max_utilization_ratio: float = Field(ge=0.5, le=1)
    force_include: list[str] = Field(default_factory=list, max_length=50)
    force_exclude: list[str] = Field(default_factory=list, max_length=500)
    max_team_size: Optional[int] = Field(default=None, ge=1, le=50)
    min_team_size: Optional[int] = Field(default=None, ge=1, le=50)
    budget_cap_units: Optional[float] = Field(default=None, ge=0)
    deadline_week: Optional[str] = Field(default=None, pattern=WEEK_KEY_RE.pattern)
    forbid_external: Optional[bool] = None
    min_bus_factor: Optional[int] = Field(default=None, ge=1, le=5)
    min_assignment_hours_per_week: Optional[float] = Field(default=None, ge=0, le=40)
    freeze_members: list[str] = Field(default_factory=list, max_length=50)
    base_variant_id: Optional[str] = None


class OptimizeOptions(_FrozenBase):
    num_search_workers: int = Field(default=1, ge=1, le=16)
    deterministic: bool = True
    allow_deficits: bool = True
    objective_preset: ObjectivePreset = "balanced"
    log_search_progress: bool = False


class OptimizeRequest(_FrozenBase):
    request_id: str = Field(pattern=REQUEST_ID_RE.pattern)
    run_id: str = Field(max_length=40)
    seed: int = Field(ge=0, le=2147483647)
    time_limit_ms: int = Field(ge=100, le=60000)
    k: int = Field(ge=2, le=3)
    min_jaccard_diversity: float = Field(ge=0.1, le=1)
    horizon: OptimizeHorizon
    weights: OptimizeWeights
    requirements: list[OptimizeRequirement] = Field(min_length=1, max_length=60)
    candidates: list[OptimizeCandidate] = Field(min_length=1, max_length=2000)
    constraints: OptimizeConstraints
    options: OptimizeOptions


# ---------------------------------------------------------------------------
# Optimize sub-schemas (output)
# ---------------------------------------------------------------------------


class SolverInfo(_FrozenBase):
    name: Literal["ortools-cp-sat"] = "ortools-cp-sat"
    version: str
    status: SolverStatus
    wall_time_ms: int = Field(ge=0)
    objective_value: Optional[float] = None
    best_bound: Optional[float] = None
    optimality_gap: Optional[float] = Field(default=None, ge=0)
    num_workers: int = Field(ge=1, le=16)
    seed: int = Field(ge=0)
    deterministic: bool


class ProposedMember(_FrozenBase):
    employee_id: str = Field(pattern=EMPLOYEE_ID_RE.pattern)
    role_id: str
    allocated_hours: float = Field(ge=0)
    assignment_share: float = Field(ge=0, le=1)
    reason_codes: list[str] = Field(min_length=1)


class ProposedAssignment(_FrozenBase):
    employee_id: str = Field(pattern=EMPLOYEE_ID_RE.pattern)
    requirement_id: str
    week: str = Field(pattern=WEEK_KEY_RE.pattern)
    hours: float = Field(ge=0, le=60)


class MlMetrics(_FrozenBase):
    M1: Optional[float] = Field(default=None, ge=0, le=1)
    M2: Optional[float] = Field(default=None, ge=0, le=1)
    M3: Optional[float] = Field(default=None, ge=0, le=1)
    M4: Optional[float] = Field(default=None, ge=0, le=1)
    M5: Optional[float] = Field(default=None, ge=0, le=1)
    M6: Optional[float] = Field(default=None, ge=0, le=1)
    M7: Optional[float] = Field(default=None, ge=0, le=1)
    M8: Optional[float] = Field(default=None, ge=0, le=1)
    M9: Optional[float] = Field(default=None, ge=0, le=1)
    M10: Optional[float] = Field(default=None, ge=0, le=1)
    M11: Optional[float] = Field(default=None, ge=0, le=1)
    M12: Optional[float] = Field(default=None, ge=0, le=1)
    score: Optional[float] = Field(default=None, ge=0, le=1)


class MlDeficit(_FrozenBase):
    competency_id: Optional[str] = None
    role_id: Optional[str] = None
    requirement_ids: list[str] = Field(default_factory=list)
    person_days: float = Field(gt=0)
    external_cooperation_flag: bool
    reason_codes: list[str] = Field(min_length=1)


class ActiveConstraintRef(_FrozenBase):
    id: str
    type: Literal[
        "force_include",
        "force_exclude",
        "max_team_size",
        "min_team_size",
        "budget_cap",
        "deadline",
        "forbid_external",
        "min_bus_factor",
        "max_utilization",
        "capacity",
    ]
    value: Optional[str] = None
    binding: bool
    reason_codes: list[str] = Field(min_length=1)


class VariantProposal(_FrozenBase):
    variant_id: str = Field(pattern=VARIANT_ID_RE.pattern)
    label: str = Field(max_length=120)
    members: list[ProposedMember] = Field(default_factory=list)
    assignments: list[ProposedAssignment] = Field(default_factory=list)
    metrics: MlMetrics
    deficits: list[MlDeficit] = Field(default_factory=list)
    unscheduled_requirements: list[str] = Field(default_factory=list)
    active_constraints: list[ActiveConstraintRef] = Field(default_factory=list)
    reason_codes: list[str] = Field(default_factory=list)


class OptimizeDiagnostics(_FrozenBase):
    model_version: str
    weeks: int = Field(ge=1)
    requirement_count: int = Field(ge=0)
    candidate_count: int = Field(ge=0)
    variables: int = Field(ge=0)
    constraints: int = Field(ge=0)
    time_limit_ms: int = Field(ge=0)
    presolve_time_ms: int = Field(default=0, ge=0)
    dropped_diversity_variants: int = Field(default=0, ge=0)


class InfeasibilityReport(_FrozenBase):
    blocking_constraints: list[ActiveConstraintRef]
    deficits: list[MlDeficit] = Field(default_factory=list)
    unscheduled_requirements: list[str] = Field(default_factory=list)
    reason_codes: list[str] = Field(default_factory=list)


class OptimizeResponse(_FrozenBase):
    request_id: str = Field(pattern=REQUEST_ID_RE.pattern)
    run_id: str
    solver: SolverInfo
    variants: list[VariantProposal] = Field(max_length=3)
    deterministic_fingerprint: str = Field(pattern=r"^sha256:[a-f0-9]{64}$")
    diagnostics: OptimizeDiagnostics
    infeasibility: Optional[InfeasibilityReport] = None
    reason_codes: list[str] = Field(default_factory=list)


# ---------------------------------------------------------------------------
# Error models (shared shape with the rest of the service)
# ---------------------------------------------------------------------------


class FieldError(_FrozenBase):
    field: str = Field(max_length=200)
    code: str = Field(pattern=PROBLEM_CODE_RE.pattern)
    detail: str = Field(max_length=500)


class Problem(_FrozenBase):
    type: str
    title: str = Field(max_length=200)
    status: int = Field(ge=400, le=599)
    code: str = Field(pattern=PROBLEM_CODE_RE.pattern)
    detail: str = Field(max_length=2000)
    request_id: str = Field(pattern=REQUEST_ID_RE.pattern)
    retryable: bool
    reason_codes: list[str] = Field(default_factory=list)
    errors: Optional[list[FieldError]] = None
