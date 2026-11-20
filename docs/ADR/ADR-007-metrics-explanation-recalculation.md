# ADR-007: Authoritative metrics in Go, deterministic explanations, linear recalculation lineage

- Status: accepted
- Date: 2026-11-17
- Deciders: architecture (orchestrator), backend, frontend, qa-eval
- Related: tasks 4.2, 4.3, 4.4; `docs/METRICS.md`; `contracts/openapi.yaml`
  (`MetricValues`, `MetricDetails`, `Explanation`, `RecalculateRequest/Response`, `VariantSetDiagnostics`)
- Supersedes: none

## Context

The ML service returns advisory `metrics` next to every variant (`MlMetrics` in `ml-openapi.yaml`), but
`AGENTS.md` says **"solver proposes, Go verifies"**. Explanations must be reproducible from structured
facts only, recalculation must be provably responsive to a single constraint edit, and the same inputs
must always yield the same numbers. The ML service cannot be the source of truth for any of those:
it does not own the availability model (ADR-003), it does not see the full portfolio, and it must stay
replaceable (ADR-001). Three decisions are needed: where metrics are computed, how texts are produced,
and what a recalculation run is.

## Decision

### 1. Metrics M1-M12 and Score are computed by Go only

- `backend/internal/metrics` computes every metric from an **immutable run snapshot** plus the variant's
  weekly assignments. Unexported helpers keep the implementation testable; the package has no database,
  HTTP or randomness dependencies except the seed passed to the schedule engine.
- Advisory ML metrics are never copied into the response. When Go cannot compute a metric because the
  snapshot lacks its input, the metric is **dropped** (M4 without project history) and the score weights
  are renormalized over the present metrics. Dropping is reported in `metric_details.dropped_metrics`.
- Composite score: `Score = Gate * Σ λ_k M_k`, `λ` from the run's preset/weights renormalized so the
  present weights sum to 1. `Gate = 0` makes `Score = 0`.
- Formulas are taken literally from `docs/METRICS.md` with these documented interpretations:
  - `a_irw` (assignment load share) = `hours / (working_days_in_week * hours_per_day)`; contributions are
    converted with `hours_per_day = 8` and `working_days_per_week = 5` (ADR-002).
  - **Effort basis:** the G3 gate and the solver feasibility use `min_person_days` (conservative lower
    bound), while M2, deficits and all reported person-days use `likely_person_days`. A variant that
    covers `min` but not `likely` is feasible (G3 = 0) and truthfully reports a positive deficit. This
    closes the "min vs likely" inconsistency recorded after task 3.4.
  - M2 contribution per assignment: `hours / 8 * min(1, level/L)` (equivalent to
    `a_irw * d_w * q_ir`), clamped per requirement at `E_r`.
  - M5 = `P(T <= D)` when a deadline exists, otherwise `T_best / T50` with `T_best` = lowest P50 among
    gate-passing variants of the same run. Duration percentiles come from `internal/schedule`
    (fixed-seed Monte Carlo, N = 2000).
  - M6 uses `C = Σ person_hours * cost_coefficient(grade)` in dimensionless units; `C_min` / `C_ref`
    are computed per run over gate-passing variants (budget cap replaces `C_ref` when set).
  - M7 utilization `u_iw = (already_assigned_minutes + variant_hours*60) / norm_minutes`; absences are
    not load. `overheat = mean(min(1, max(0, u-0.9)/0.1))`.
  - M8 = `exp(-Σ slip_days / 10)` from the schedule engine's portfolio model.
  - M12 dropout simulation is deterministic: for each member, zero that member's assignments in the two
    consecutive weeks with the highest assigned hours (one week if the horizon is shorter), recompute
    T80 with the same seed, and count the scenario as surviving when (a) no must requirement loses its
    last qualified member (G3-equivalent) and (b) `T80_new <= 1.2 * T80_base`. `M12 = survived / members`.
    G1/G2 hold by construction because hours are removed, never added.
- Run-level rules from METRICS ("all gate-passing, non-dominated, Jaccard >= 0.25") are recomputed by Go
  and exposed as `TeamRun.variant_set`; a solver claim about diversity is not trusted.

### 2. Explanations are rendered from reason codes + facts, never generated

- `backend/internal/explain` produces `Explanation` deterministically: RU templates keyed by reason code,
  `facts` filled from the snapshot/variant/metrics/schedule. No LLM, no free-form numbers.
- Coverage is total: every included member, every **relevant** excluded candidate (a candidate whose role
  or competency matches at least one requirement but who is not on the team), every deficit. Exclusion
  reason precedence: user force exclude → absent → overloaded → below required level → team-size limit →
  no free capacity → portfolio protection → expensive → data incomplete.
- `headline_reason_codes` and `Counterfactual.facts` are always emitted (contract keeps them optional for
  backward compatibility); the faithfulness checker re-derives every number in `text` from `facts` and
  fails the build when a number has no source.
- Counterfactuals are **verified by rerun** whenever the pipeline can afford it (schedule re-run or full
  recalculation, `time_limit_ms` bounded): `verified_by=rerun` with the real `run_id` and measured
  before/after values in `facts`. `estimated` is only allowed with a linear approximation and is labelled
  in the UI. Acceptance: >= 95% of counterfactuals verified (rerun), faithfulness 100%.
- Explanation retrieval is `GET /runs/{runId}/variants/{variantId}/explanation`; the text language is RU
  (i18n-ready, `locale` echoed).

### 3. Recalculation is a new run with explicit lineage, never an in-place mutation

- `POST /runs/{runId}/recalculate` creates a **new** run derived from the stored snapshot of the parent;
  the parent run, its snapshot, variants and audit entries stay immutable.
- `base_variant_id` is the UI's diff baseline only. It does **not** freeze the roster (the earlier wording
  "freeze the rest" was ambiguous and would make recalculation unresponsive). Freezing is expressed
  explicitly via `freeze_members`.
- Overrides are applied as a per-field layer on the parent's `RunConstraints`. Invalid or contradictory
  overrides are rejected individually (`rejected_overrides[].reason_codes`) when possible; if the
  resulting constraint set is infeasible, the run reports `SOLVER_INFEASIBLE` with the blocking
  constraint and no relaxed solution (G4 is never relaxed).
- The response reports the measured effect: `applied_overrides`, `rejected_overrides`, `reason_codes`
  (`RECALC_APPLIED` / `RECALC_NO_EFFECT`) and `facts` (`changed_members`, `roster_jaccard`,
  `p50_weeks_before/after`). `RECALC_NO_EFFECT` is only legal together with those facts.
- Synchronous in-process implementation returns `200` with `status=succeeded`; `202` remains reserved for
  a queued deployment.
- NFRs: recalculation p95 <= 5 s and score/duration recomputation p95 <= 10 s at ~500 employees x ~30
  requirements (METRICS 3.2/3.6), measured by `qa-eval` on the synthetic pool; ML unavailability yields
  a `Problem` with `ML_UNAVAILABLE`, never a silently different plan (ADR-004).

## Alternatives considered

| Alternative | Why rejected |
|---|---|
| Trust ML advisory metrics, recompute only the score | Violates "solver proposes, Go verifies"; M7/M8/M12 need data the ML never sees; drift would be silent |
| Compute metrics in SQL / materialized views | Formula changes require migrations; hard to unit-test item by item against METRICS.md |
| LLM-generated explanations | Faithfulness cannot be guaranteed (100% target) and determinism breaks |
| Freeze the base variant on recalculation | A frozen roster cannot react to a force-exclude/deadline edit; responsiveness (100%) would fail |
| In-place run mutation | Breaks audit/immutability NFR and the "why did the team change" story (ADR-003 §6) |
| Async-only recalculation (202 + polling) | Adds infrastructure with no benefit at this scale; 5 s p95 is achievable in-process |

## Consequences

Positive: one source of truth for numbers; explanations are testable string-free templates; recalculation
is auditable and metamorphic-testable; UI can trust `metric_values` and `facts`.

Negative / risks: Go must keep exact parity with METRICS.md formulas (reviewer checks item by item); the
snapshot must carry candidate competencies, grades, bench flags, portfolio and capacity rows, so the run
input grows (kept as canonical JSON + sha256 digest); M12 costs `|T|` extra schedule simulations per
variant (bounded by team size <= 50 and N = 2000, watches the 10 s budget).

Testable: `internal/metrics` hand-computed table tests per metric; property tests (values in [0,1], Gate
propagation, determinism); faithfulness checker on every generated explanation; metamorphic suite on
recalculation (constraint change -> expected direction, 100%); p95 measurements recorded in the plan log.
