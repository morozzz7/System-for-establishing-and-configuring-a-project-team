# ADR-001: The optimizer lives in the ML service; Go verifies

- Status: accepted
- Date: 2026-11-05
- Deciders: architecture (orchestrator), backend, ml
- Related: `docs/PLAN.md` 1.2, `contracts/ml-openapi.yaml` (`/v1/optimize`), `AGENTS.md` ("solver proposes, Go verifies")
- Supersedes: none

## Context

The system must produce 2-3 diverse, hard-feasible team variants for up to ~500 employees x ~30
requirements x ~15-100 weeks, with soft objectives expressed as 12 metrics (`docs/METRICS.md`) and
hard constraints G1-G5. Candidate solvers:

1. **OR-Tools CP-SAT** - the only mature constraint solver with native interval/capacity reasoning and
   good performance on assignment problems of this size.
2. Custom Go ILP / branch-and-bound - no dependency on Python, but we would rebuild CP-SAT badly.

`or-tools` has **no supported Go API**. The organization has two implementation languages anyway
(Go core, Python ML), so the optimizer has to live on one side of an HTTP boundary.

The dangerous property of a solver is not speed, it is *silent wrongness*: a model bug, a stale
capacity matrix or a hallucinated employee id produces a plan that looks authoritative and violates
someone's vacation. The product promise is "hard constraints = 0 violations, whatever the weights".

## Decision

1. **CP-SAT runs in the Python ML service** (`POST /v1/optimize`). It is stateless: no DB access, no
   employee data of its own.
2. **Go is the system of record.** Go builds the input (requirements, candidates, cost coefficients,
   the authoritative weekly capacity matrix), calls ML, and then, for every returned variant:
   - re-checks G1-G5 independently in `internal/validator`,
   - recomputes duration (P50/P80), effort coverage, deficits and M1-M12,
   - drops the variant on any violation, logs `VARIANT_REJECTED_BY_VALIDATOR`, and emits the metric
     `solver_validator_disagreement` (target: trend to 0).
3. **The ML `metrics` block is advisory only.** It exists to guide the solver and to make
   disagreements observable. The UI never displays ML numbers.
4. **The LLM never touches assignments.** It only structures brief text into requirements
   (`/v1/extract`), which a human reviews before any run.
5. **Determinism:** `options.num_search_workers = 1`, explicit `seed`, explicit `time_limit_ms`,
   `deterministic_fingerprint` over the canonical request + solver version. The **structural** body
   (variants, assignments, metrics, deficits, fingerprint) must be reproducible; timing fields
   (`solver.wall_time_ms`, `diagnostics.presolve_time_ms`) are excluded by definition. Strict
   reproducibility relies on a fixed node/iteration budget in the solver configuration; a
   wall-clock-bounded search may legitimately return `FEASIBLE` instead of `OPTIMAL`
   (reported in `solver.status`, tested in `qa-eval` 3.2).
6. **Fallback is a separate, never-silent path.** If the ML service is unavailable, a run fails with
   `SOLVER_*` reason codes (ADR-004) unless the Go fallback solver is explicitly enabled by a feature
   flag. The fallback (Go ILP/branch-and-bound, coverage-first greedy + local search) has its own ADR
   before it may be enabled in production and must set `diagnostics.fallback_used = true`.
7. **No `/v1/explain` in v0.** Explanations are generated in Go from structured facts (assignments,
   capacity rows, metric values, reason codes). An ML endpoint that writes prose would make the
   faithfulness target (100%, METRICS 3.5) depend on a non-authoritative service and would be
   untestable offline; `ml/app/explain` stays unused until a concrete need is proven by an ADR.

## Why not the alternatives

- **Optimizer in Go (custom ILP):** loses CP-SAT's propagation on weekly capacity intervals; would
  need months to reach "feasibility recall >= 98% on planted solutions" (task 3.3). Acceptable only as
  the documented fallback.
- **Optimizer in Go calling a Python C-extension / child process:** adds a process-management failure
  mode without removing the boundary; HTTP is easier to observe, version and test.
- **Metrics computed in ML (single source):** would make explainability depend on a non-authoritative
  service. Recomputing in Go is cheap (vectorized over <= 500 x <= 104 rows) and makes the validator
  meaningful.

## Consequences

Positive:
- Best-in-class solver, no reimplementation.
- A compromised or buggy ML deployment cannot produce an infeasible plan for the user: Go blocks it.
- Contract is versioned (`contracts/ml-openapi.yaml`), so ML can be replaced by any solver that
  satisfies it (e.g., a different CP/SAT backend) without touching the core.

Negative / risks:
- Extra network hop and a second deployment: adds ~p95 50-150 ms of transport plus serialization for
  500 candidates x 104 weeks. Mitigated by a compact payload (see `/v1/optimize`) and gzip.
- Double implementation of the metric formulas (Go authoritative, ML advisory). Mitigated by
  `qa-eval` cross-checking both on the same fixtures and by the disagreement metric.
- Python service becomes a critical dependency for the core scenario: mitigated by ADR-004
  (timeouts, circuit breaker, explicit degradation) and by keeping `runs` resumable.

## Verification

- Task 3.5: property tests show G1/G2 hold on random data; seeded bad variants are rejected.
- Task 3.3/3.4: feasibility recall >= 98% on planted solutions; Jaccard diversity >= 0.25;
  `INFEASIBLE` returns the blocking deficits.
- CI: contract test compares Go's `Variant` against `contracts/json-schema/team-variant.schema.json`.
