"""FastAPI router for POST /v1/optimize (Optimizer v2, task 3.4).

Thin transport layer: parse + validate the ``OptimizeRequest``, delegate to
``solver.solve``, then assemble a schema-valid ``OptimizeResponse``: up to ``k`` diverse,
non-dominated ``VariantProposal`` objects with trade-off labels, rich ``reason_codes``
(contract ``ReasonCode`` enum only), deficits, active constraints, the deterministic
fingerprint and the diagnostics block. Returns 200 for every solver outcome
(``OPTIMAL``/``FEASIBLE``/``INFEASIBLE``/``TIMEOUT``/...); transport-level failures use
4xx/5xx ``Problem`` responses, mirroring ``app.extract.router``.
"""

from __future__ import annotations

from typing import Any

from fastapi import APIRouter, Request
from fastapi.responses import JSONResponse

from . import solver
from .fingerprint import deterministic_fingerprint
from .schemas import (
    ActiveConstraintRef,
    MlMetrics,
    OptimizeRequest,
    OptimizeResponse,
    ProposedAssignment,
    ProposedMember,
    SolverInfo,
    VariantProposal,
    ensure_valid_reason_codes,
)
from .solver import SolverValidationError, SolveOutcome, VariantOutcome

router = APIRouter()

NORM_HOURS_PER_WEEK = 40.0

#: Single-variant fallback: the request preset names the trade-off (v1 behaviour).
_PRESET_LABELS = {
    "fast": ("Fastest — schedule-optimised", "VARIANT_FASTEST"),
    "economical": ("Economical — cost-optimised", "VARIANT_CHEAPEST"),
    "balanced": ("Balanced — best overall trade-off", "VARIANT_BALANCED"),
    "low_risk": ("Low risk — resilience-optimised", "VARIANT_LOWEST_RISK"),
    "custom": ("Custom — request-defined weights", "VARIANT_BALANCED"),
}

#: Multi-variant trade-off dimensions, in deterministic priority order.
_TRADEOFF_DIMENSIONS: tuple[tuple[str, str, str], ...] = (
    ("fastest", "VARIANT_FASTEST", "Fastest — shortest schedule"),
    ("cheapest", "VARIANT_CHEAPEST", "Economical — lowest cost"),
    ("low_risk", "VARIANT_LOWEST_RISK", "Low risk — highest resilience"),
)
_BALANCED = ("Balanced — best overall trade-off", "VARIANT_BALANCED")


def _problem(
    status: int,
    code: str,
    title: str,
    detail: str,
    request_id: str,
    retryable: bool = False,
    reason_codes: list[str] | None = None,
    errors: list[dict[str, Any]] | None = None,
) -> dict[str, Any]:
    body: dict[str, Any] = {
        "type": f"https://teamopt.internal/ml/problems/{code.lower().replace('_', '-')}",
        "title": title,
        "status": status,
        "code": code,
        "detail": detail,
        "request_id": request_id,
        "retryable": retryable,
        "reason_codes": reason_codes if reason_codes is not None else [],
    }
    if errors:
        body["errors"] = errors
    return body


def _echo_headers(request: Request) -> dict[str, str]:
    hdr = request.headers.get("x-request-id")
    return {"X-Request-Id": hdr} if hdr else {}


def _fallback_req_id(body: Any) -> str:
    rid = body.get("request_id", "req_stub00000001") if isinstance(body, dict) else "req_stub00000001"
    if not isinstance(rid, str) or not rid.startswith("req_") or len(rid) < 10:
        rid = "req_stub00000001"
    return rid


# ---------------------------------------------------------------------------
# Per-member / per-variant reason codes (contract ReasonCode enum only)
# ---------------------------------------------------------------------------


def _comp_map(candidate) -> dict[str, int]:
    return {cc.competency_id: cc.level for cc in candidate.competencies}


def _qualified(candidate, req) -> bool:
    comps = _comp_map(candidate)
    if req.competency_id is None:
        return candidate.role_profile_id == req.role_id
    return comps.get(req.competency_id, 0) >= req.level


def _member_reason_codes(
    request: OptimizeRequest,
    emp: str,
    assigned_req_ids: set[str],
    cheapest_emps: set[str],
) -> list[str]:
    """Deterministic, contract-valid reason codes for one proposed member.

    Every code states a fact about *this* member in *this* variant, so the Go explainer can
    quote it without recomputation: the member is on the roster because they have free
    capacity, they cover the required role/competency, they are qualified for a ``must``
    requirement, they satisfy the bus-factor rule, they were on the bench, or they are the
    cheapest (by grade coefficient) person assigned to at least one requirement.
    """
    cand = next(c for c in request.candidates if c.employee_id == emp)
    req_by_id = {r.requirement_id: r for r in request.requirements}
    codes: list[str] = ["INCLUDE_AVAILABLE_CAPACITY"]

    assigned = [req_by_id[rid] for rid in sorted(assigned_req_ids) if rid in req_by_id]
    if assigned:
        codes.append("INCLUDE_REQUIRED_ROLE")

    covers_must = [r for r in assigned if r.criticality == "must" and _qualified(cand, r)]
    if covers_must:
        codes.append("INCLUDE_HIGH_COMPETENCE")

    min_bus = request.constraints.min_bus_factor or 1
    if min_bus > 1 and any(_qualified(cand, r) for r in request.requirements if r.criticality == "must"):
        codes.append("INCLUDE_BUS_FACTOR")

    if cand.bench_flag:
        codes.append("INCLUDE_BENCH")

    if emp in cheapest_emps:
        codes.append("INCLUDE_COST_EFFICIENT")

    # de-dup preserving order
    return ensure_valid_reason_codes(list(dict.fromkeys(codes)))


def _active_constraints(
    request: OptimizeRequest,
    variant: VariantOutcome,
) -> list[ActiveConstraintRef]:
    """Constraints that are tight or that explain this variant (never invented)."""
    c = request.constraints
    members = variant.members
    refs: list[ActiveConstraintRef] = []

    for emp in c.force_include + c.freeze_members:
        refs.append(
            ActiveConstraintRef(
                id=f"c_force_include_{emp}",
                type="force_include",
                value=emp,
                binding=True,
                reason_codes=["CONSTRAINT_ACTIVE_FORCE_INCLUDE"],
            )
        )

    if c.max_team_size is not None and len(members) >= c.max_team_size:
        refs.append(
            ActiveConstraintRef(
                id="c_max_team_size", type="max_team_size", value=str(c.max_team_size),
                binding=True, reason_codes=["CONSTRAINT_ACTIVE_TEAM_SIZE"],
            )
        )
    if c.min_team_size is not None and len(members) <= c.min_team_size:
        refs.append(
            ActiveConstraintRef(
                id="c_min_team_size", type="min_team_size", value=str(c.min_team_size),
                binding=True, reason_codes=["CONSTRAINT_ACTIVE_TEAM_SIZE"],
            )
        )

    if c.budget_cap_units is not None:
        cost = variant.cost_units
        if cost >= 0.98 * c.budget_cap_units:
            refs.append(
                ActiveConstraintRef(
                    id="c_budget_cap", type="budget_cap", value=str(c.budget_cap_units),
                    binding=True, reason_codes=["CONSTRAINT_ACTIVE_BUDGET"],
                )
            )

    last_active_week = max((wk for _, _, wk, _ in variant.assignments), default=None)
    if c.deadline_week is not None and last_active_week == c.deadline_week:
        refs.append(
            ActiveConstraintRef(
                id="c_deadline", type="deadline", value=c.deadline_week,
                binding=True, reason_codes=["CONSTRAINT_ACTIVE_DEADLINE"],
            )
        )

    cand_by_id = {cd.employee_id: cd for cd in request.candidates}
    if c.min_bus_factor and c.min_bus_factor > 1:
        for r in request.requirements:
            if r.criticality != "must":
                continue
            n_qual = sum(1 for m in members if _qualified(cand_by_id[m], r))
            if n_qual == c.min_bus_factor:
                refs.append(
                    ActiveConstraintRef(
                        id=f"c_min_bus_factor_{r.requirement_id}", type="min_bus_factor",
                        value=str(c.min_bus_factor), binding=True,
                        reason_codes=["CONSTRAINT_ACTIVE_BUS_FACTOR"],
                    )
                )
                break

    # force_exclude: name the (eligible) people the user removed from the pool
    excluded = set(c.force_exclude)
    eligible_excluded = [
        cd.employee_id
        for cd in sorted(request.candidates, key=lambda x: x.employee_id)
        if cd.employee_id in excluded
        and any(_qualified(cd, r) for r in request.requirements)
    ]
    for emp in eligible_excluded[:10]:
        refs.append(
            ActiveConstraintRef(
                id=f"c_force_exclude_{emp}", type="force_exclude", value=emp,
                binding=False,
                reason_codes=["CONSTRAINT_ACTIVE_FORCE_EXCLUDE", "EXCLUDE_USER_FORCE"],
            )
        )

    # forbid_external: only reported when it demonstrably cost us coverage
    if c.forbid_external and variant.deficits:
        external_qualified = [
            cd.employee_id
            for cd in request.candidates
            if cd.employment_type in ("contractor", "external_partner")
            and any(_qualified(cd, r) for r in request.requirements)
        ]
        if external_qualified:
            refs.append(
                ActiveConstraintRef(
                    id="c_forbid_external", type="forbid_external", value="true",
                    binding=True, reason_codes=["CONSTRAINT_ACTIVE_FORCE_EXCLUDE"],
                )
            )

    # every deficit is a tight capacity constraint on the named requirement(s)
    for deficit in variant.deficits:
        for rid in deficit.requirement_ids:
            refs.append(
                ActiveConstraintRef(
                    id=f"c_must_deficit_{rid}", type="capacity",
                    value=deficit.competency_id or deficit.role_id,
                    binding=True, reason_codes=["CONSTRAINT_BINDING_CAPACITY"],
                )
            )

    return refs


# ---------------------------------------------------------------------------
# Variant assembly
# ---------------------------------------------------------------------------


def _build_variant(
    request: OptimizeRequest, outcome: VariantOutcome, index: int
) -> VariantProposal:
    """Assemble one ``VariantProposal`` from a solver ``VariantOutcome``."""
    n_weeks = len(request.horizon.weeks)
    cand_by_id = {c.employee_id: c for c in request.candidates}

    assigned_by_emp: dict[str, set[str]] = {}
    hours_by_req: dict[str, dict[str, float]] = {}
    for emp, rid, _wk, hrs in outcome.assignments:
        assigned_by_emp.setdefault(emp, set()).add(rid)
        hours_by_req.setdefault(rid, {})
        hours_by_req[rid][emp] = hours_by_req[rid].get(emp, 0.0) + float(hrs)

    # cheapest-by-grade member assigned to at least one requirement (ties included)
    cheapest_emps: set[str] = set()
    for rid, by_emp in hours_by_req.items():
        if len(by_emp) < 2:
            continue
        min_coef = min(cand_by_id[e].cost_coefficient for e in by_emp)
        for e in by_emp:
            if cand_by_id[e].cost_coefficient <= min_coef + 1e-9:
                cheapest_emps.add(e)

    members: list[ProposedMember] = []
    for emp in outcome.members:
        alloc = outcome.allocated_hours.get(emp, 0.0)
        share = min(1.0, alloc / (n_weeks * NORM_HOURS_PER_WEEK)) if n_weeks > 0 else 0.0
        members.append(
            ProposedMember(
                employee_id=emp,
                role_id=cand_by_id[emp].role_profile_id,
                allocated_hours=round(alloc, 2),
                assignment_share=round(share, 6),
                reason_codes=_member_reason_codes(
                    request, emp, assigned_by_emp.get(emp, set()), cheapest_emps,
                ),
            )
        )

    assignments = [
        ProposedAssignment(
            employee_id=emp, requirement_id=rid, week=wk, hours=round(float(h), 2),
        )
        for emp, rid, wk, h in outcome.assignments
    ]

    metrics = MlMetrics(**{k: v for k, v in outcome.metrics.items() if v is not None})

    return VariantProposal(
        variant_id=f"var_opt{index + 1}",
        label="",
        members=members,
        assignments=assignments,
        metrics=metrics,
        deficits=list(outcome.deficits),
        unscheduled_requirements=list(outcome.unscheduled_requirements),
        active_constraints=_active_constraints(request, outcome),
        reason_codes=[],
    )


def _dimension_value(metrics: MlMetrics, dimension: str) -> float | None:
    """Trade-off dimension value; ``None`` when a required metric was dropped."""
    if dimension == "fastest":
        return metrics.M5
    if dimension == "cheapest":
        return metrics.M6
    if dimension == "low_risk":
        parts = [metrics.M7, metrics.M9, metrics.M12]
        present = [p for p in parts if p is not None]
        return sum(present) / len(present) if present else None
    return None


def _apply_labels(request: OptimizeRequest, proposals: list[VariantProposal]) -> None:
    """Give every variant a meaningful label + one ``VARIANT_*`` trade-off reason code.

    With a single variant the request preset names the trade-off (v1 behaviour). With
    several variants, each dimension (schedule / cost / resilience) is awarded to its
    unique winner; variants that win nothing are labelled ``VARIANT_BALANCED`` (they are
    kept for their all-round profile, which is exactly the non-domination criterion).
    """
    if len(proposals) == 1:
        label, code = _PRESET_LABELS.get(request.options.objective_preset, _BALANCED)
        proposals[0].label = label
        proposals[0].reason_codes = [code]
        return

    labels: list[tuple[str, str] | None] = [None] * len(proposals)
    for dimension, code, text in _TRADEOFF_DIMENSIONS:
        values = [_dimension_value(p.metrics, dimension) for p in proposals]
        present = [(i, v) for i, v in enumerate(values) if v is not None]
        if len(present) < 2:
            continue
        best = max(v for _, v in present)
        winners = [i for i, v in present if abs(v - best) <= 1e-9]
        if len(winners) != 1:  # tie => no single winner for this trade-off
            continue
        idx = winners[0]
        if labels[idx] is not None:
            continue
        labels[idx] = (text, code)

    for i in range(len(proposals)):
        label, code = labels[i] if labels[i] is not None else _BALANCED
        proposals[i].label = label
        proposals[i].reason_codes = [code]


def _aggregate_reason_codes(
    request: OptimizeRequest, outcome: SolveOutcome, proposals: list[VariantProposal]
) -> list[str]:
    """Response-level reason codes for the Go explainer (contract enum only)."""
    codes: list[str] = []
    codes.extend(outcome.reason_codes)
    if any(p.deficits for p in proposals):
        codes.append("DEFICIT_MUST_ROLE")
    if outcome.status == "INFEASIBLE":
        codes.append("SOLVER_INFEASIBLE")
    if outcome.status == "TIMEOUT":
        codes.append("SOLVER_TIMEOUT")
    if len(proposals) < request.k:
        codes.append("VARIANT_DIVERSITY_TOO_LOW")
    return ensure_valid_reason_codes(list(dict.fromkeys(codes)))


# ---------------------------------------------------------------------------
# Response serialization
# ---------------------------------------------------------------------------


def _response_payload(response: OptimizeResponse) -> dict[str, Any]:
    """Dump the response, keeping the contract-required nullable deficit keys.

    ``MlMetrics`` fields are *not* nullable in the contract but are omitted when a metric
    is dropped, so the generic dump uses ``exclude_none``. ``MlDeficit.competency_id`` is
    required-and-nullable, so deficits are re-dumped without ``exclude_none`` to keep the
    key present (``null`` for role-level deficits).
    """
    data = response.model_dump(mode="json", exclude_none=True)
    for i, variant in enumerate(response.variants):
        data["variants"][i]["deficits"] = [d.model_dump(mode="json") for d in variant.deficits]
    if response.infeasibility is not None:
        data["infeasibility"]["deficits"] = [
            d.model_dump(mode="json") for d in response.infeasibility.deficits
        ]
    return data


# ---------------------------------------------------------------------------
# Handler
# ---------------------------------------------------------------------------


@router.post("/v1/optimize")
async def optimize_handler(request: Request) -> JSONResponse:
    resp_headers = _echo_headers(request)

    try:
        body = await request.json()
    except Exception:
        return JSONResponse(
            status_code=422,
            headers=resp_headers,
            content=_problem(
                status=422, code="INVALID_REQUEST", title="Invalid request body",
                detail="Request body is not valid JSON",
                request_id=request.headers.get("x-request-id", "req_stub00000001"),
                reason_codes=["VALIDATION_FAILED"],
            ),
        )

    try:
        req = OptimizeRequest.model_validate(body)
    except Exception as exc:
        return JSONResponse(
            status_code=422,
            headers=resp_headers,
            content=_problem(
                status=422, code="INVALID_REQUEST", title="Validation error",
                detail=str(exc)[:2000], request_id=_fallback_req_id(body),
                reason_codes=["VALIDATION_FAILED"],
            ),
        )

    try:
        outcome = solver.solve(req)
    except SolverValidationError as exc:
        errors = []
        if exc.field:
            errors.append({"field": exc.field, "code": exc.code, "detail": exc.detail[:500]})
        return JSONResponse(
            status_code=422,
            headers=resp_headers,
            content=_problem(
                status=422, code="VALIDATION_FAILED", title="Validation failed",
                detail=exc.detail[:2000], request_id=req.request_id,
                reason_codes=["VALIDATION_FAILED"], errors=errors,
            ),
        )

    proposals: list[VariantProposal] = []
    if outcome.status in ("OPTIMAL", "FEASIBLE") and outcome.variants:
        proposals = [_build_variant(req, v, i) for i, v in enumerate(outcome.variants)]
        _apply_labels(req, proposals)

    response = OptimizeResponse(
        request_id=req.request_id,
        run_id=req.run_id,
        solver=SolverInfo(
            name="ortools-cp-sat",
            version=solver.SOLVER_VERSION,
            status=outcome.status,  # type: ignore[arg-type]
            wall_time_ms=outcome.wall_time_ms,
            objective_value=outcome.objective_value,
            best_bound=outcome.best_bound,
            optimality_gap=outcome.optimality_gap,
            num_workers=outcome.num_workers,
            seed=req.seed,
            deterministic=True,
        ),
        variants=proposals,
        deterministic_fingerprint=deterministic_fingerprint(req, solver.SOLVER_VERSION),
        diagnostics=solver_diagnostics(req, outcome),
        infeasibility=outcome.infeasibility,
        reason_codes=_aggregate_reason_codes(req, outcome, proposals),
    )

    return JSONResponse(
        content=_response_payload(response), headers=resp_headers,
    )


def solver_diagnostics(request: OptimizeRequest, outcome: SolveOutcome):
    """Build the ``OptimizeDiagnostics`` block (imported lazily to avoid circulars)."""
    from .schemas import OptimizeDiagnostics

    return OptimizeDiagnostics(
        model_version="opt-v2",
        weeks=len(request.horizon.weeks),
        requirement_count=len(request.requirements),
        candidate_count=len(request.candidates),
        variables=outcome.num_vars,
        constraints=outcome.num_constraints,
        time_limit_ms=request.time_limit_ms,
        presolve_time_ms=outcome.presolve_time_ms,
        dropped_diversity_variants=outcome.dropped_diversity_variants,
    )
