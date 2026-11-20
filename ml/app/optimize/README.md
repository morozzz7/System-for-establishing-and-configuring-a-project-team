# Optimizer v2 (`POST /v1/optimize`)

Tasks 3.3 (v1) + 3.4 (v2). OR-Tools CP-SAT team-formation optimizer. **"Solver proposes, Go
verifies":** the optimizer receives an already-materialized capacity matrix and respects it;
it never computes availability itself and never emits personal data (IDs are pseudonyms only).

v2 adds, on top of v1, **deficits via slack** (best partial team instead of a blanket
`INFEASIBLE`), **k = 2..3 diverse variants** (Jaccard cut), a **non-domination filter** on the
advisory metric vector and richer, cause-specific **reason codes**.

## Files

| File | Role |
|---|---|
| `schemas.py` | Pydantic v2 models mirroring `Optimize*` schemas in `contracts/ml-openapi.yaml` (1:1, `extra="forbid"`), `VALID_REASON_CODES`, `ensure_valid_reason_codes`. |
| `solver.py` | CP-SAT model (`_Model`): hard constraints G1–G5, slack-based deficits, diversity cuts, non-domination selection, infeasibility diagnosis. |
| `metrics.py` | Advisory M1..M12 + composite `score`, `METRIC_KEYS`, `dominates()` — computed post-hoc (deterministic). |
| `fingerprint.py` | `sha256` over canonical request JSON + solver version. |
| `router.py` | Transport layer: response assembly, trade-off labels, per-member/per-variant reason codes, `Problem`-shaped errors, `X-Request-Id` echo. |

## Hard constraints (modeled exactly, never relaxed)

- **G1** no hours in a week where `capacity[].unavailable == true` (free capacity is forced to 0 there).
- **G2** `sum_r a[i,r,w] <= capacity[].free_hours` per (employee, week).
- **G3** every `criticality = must` requirement gets `min_person_days * 8` hours from
  candidates qualified at level >= required level — enforced exactly when
  `options.allow_deficits = false`; when `true` the shortfall becomes a minimized `slack[r]`
  and is reported as an explicit `deficits[]` entry (see below).
- **G4** `force_include`, `freeze_members`, `force_exclude`, `max_team_size`, `min_team_size`,
  `budget_cap_units`, `deadline_week`, `forbid_external`, `min_bus_factor`,
  `min_assignment_hours_per_week` are never relaxed in **any** mode. If G4 makes the roster
  impossible, the response is `INFEASIBLE` with `variants: []` and the blocking constraint(s)
  named in `infeasibility.blocking_constraints` — never a silently relaxed plan. Contradictory
  combinations (unknown `force_include`/`freeze_members`, forced-in and -excluded overlap,
  `min_team_size > max_team_size`, `forbid_external` vs forced external, out-of-horizon
  `deadline_week`/`earliest_week`/`latest_week`) are rejected with HTTP 422 + reason code.
- **G5** only ids present in `candidates` appear in the output (assignment variables exist only
  for candidates; `force_include` of an unknown id is a 422).

`max_utilization_ratio` is *already reflected* in `free_hours` (Go materializes it, ADR-003), so
the optimizer enforces it indirectly via G2; it does not re-derive the utilization ceiling
because it does not receive `norm`. This is documented, not silent.

## Deficits via slack (`options.allow_deficits`, default `true`)

Every `must` requirement `r` gets an integer `slack[r] ∈ [0, eff_min_hours(r)]` and the model
enforces `sum(hours assigned to r) + slack[r] >= eff_min_hours(r)`. Consequences:

- The caller always receives the **best partial team** plus an explicit deficit, never
  "no solution" for a mere shortage.
- Slack is minimized *before* any quality term: the penalty is
  `ceil(obj_scale + cost_bound) + 1`, i.e. strictly larger than the whole quality objective
  including the cost tie-breaker. A coverable requirement can therefore never be reported as a
  deficit, and deficits are minimized lexicographically (fewest hours of uncovered minimum
  effort first, quality second).
- `person_days` per deficit: `slack_hours / 8` when at least one candidate is assignable to the
  requirement; the full `likely_person_days` ("effort at risk") when **nobody** is assignable
  (no holder, holder below the required level, or all holders excluded by G4). Minimum reported
  value is 0.25 pd.
- Deficits are grouped by `(competency_id, role_id)`, `requirement_ids` sorted,
  `external_cooperation_flag = true` for every `must` deficit (METRICS Level 2), and the reason
  code states the recommended action:

| Cause | Code |
|---|---|
| Nobody in the pool holds the competency | `DEFICIT_EXTERNAL_CONTRACTOR` |
| Holders exist but all are below the required level | `DEFICIT_UPSKILL_INTERNAL` |
| Qualified people exist but capacity/window is short | `DEFICIT_DEADLINE_SHIFT` |
| Budget cap binding (`cost >= 98%` of the cap) | `DEFICIT_DESCOPE` |
| Always present on a deficit | `DEFICIT_MUST_ROLE` |

With `options.allow_deficits = false` the slack variable is omitted and an uncovered `must`
makes the model `INFEASIBLE` (v1 behaviour); `infeasibility` still names the shortage.

**Empty roster is not a team.** If the first solve returns no members (nothing is staffable, or
G4 leaves nothing) the outcome is `INFEASIBLE` even with `allow_deficits = true`; a later
attempt that yields an empty roster just ends the search (a roster is never duplicated). A
deficit is only ever reported for a variant that has at least one member.

## Variant set: diversity + non-domination

- Attempt 0 is the plain optimum. After each accepted roster the model gets one exact linear
  **no-good/diversity cut**: with `I = sum_{i in roster} x[i]`, `S = sum_i x[i]` and
  `Jaccard = I / (|roster| + S - I)`, the constraint `I*(2-d) <= (1-d)*(|roster| + S)` (scaled
  by 10000, `d = min_jaccard_diversity + 0.001`) enforces `1 - Jaccard >= d` exactly — no big-M,
  no auxiliary booleans. `DIVERSITY_EPS` guards the post-hoc float check.
- Number of solves is fixed and deterministic: `k + 1` (base + one re-solve per requested extra
  variant + one spare in case a roster is dominated). The request's `time_limit_ms` is split
  deterministically: `BUILD_RESERVE_SHARE = 0.3` of it is reserved for model build + response
  assembly (the contract enforces `time_limit_ms` for the *whole* call, CP-SAT only bounds
  search) and the rest is divided evenly across the attempts. No wall-clock decision is ever
  taken, so the same request always issues the same number of solves.
- `select_variants()` orders candidates by descending advisory score (ties: sorted roster, then
  attempt) and keeps a candidate only when it is diverse from every kept roster **and** not
  Pareto-dominated on M1..M12 (`metrics.dominates`, shared/present keys only). Dominated or
  non-diverse candidates are replaced by the next solution.
- `diagnostics.dropped_diversity_variants = max(k, len(candidates)) - len(kept)` — the honest
  shortfall (variants asked for or found but not returned). Responses may return 1..3 variants;
  fewer than `k` is accompanied by the response-level code `VARIANT_DIVERSITY_TOO_LOW`.

## Labels and reason codes

- One variant: labelled from `options.objective_preset` (v1 behaviour: fast / economical /
  balanced / low_risk / custom).
- Several variants: the unique winner of M5 (schedule) gets `VARIANT_FASTEST`, of M6 (cost)
  `VARIANT_CHEAPEST`, of `mean(M7, M9, M12)` (resilience) `VARIANT_LOWEST_RISK`; the rest are
  `VARIANT_BALANCED`. Ties award nothing, so a label always states a fact about that variant.
- Per member: `INCLUDE_AVAILABLE_CAPACITY`, `INCLUDE_REQUIRED_ROLE`, `INCLUDE_HIGH_COMPETENCE`,
  `INCLUDE_BUS_FACTOR`, `INCLUDE_BENCH`, `INCLUDE_COST_EFFICIENT`.
- Per variant: `active_constraints` lists only constraints that are actually tight or that
  explain the variant (force include/exclude, team-size, budget at >= 98% of the cap, deadline
  hit exactly, bus factor exactly met, `forbid_external` that cost coverage, one `capacity` ref
  per deficit requirement). Every entry carries its own `reason_codes`.
- Only codes from the contract `ReasonCode` enum are ever emitted (`ensure_valid_reason_codes`).

## Objective (linearized coverage proxy)

```
max  Σ_r crit(r) * ( wM1*1000 * COVER_BONUS * cov_flag[r] )
   + Σ_{i,r,w} [ crit(r) * wM2*1000 * fit(i,r) - cost(i) ] * a[i,r,w]
   - slack_penalty * Σ_r slack[r]                (only when allow_deficits)
```

- `crit(r)` = must 100 / should 50 / nice 20 (criticality weights 1.0 / 0.5 / 0.2).
- `wM1`, `wM2` = `weights.M1`, `weights.M2` from the request.
- `cov_flag[r]` = 1 iff some candidate is assigned to `r` (M1 competency-coverage proxy).
- `fit(i,r)` = `min(100, round(100 * level_i / level_r))` (M2 quality weighting; 100 when
  level >= required). For `competency_id = null` (role-only) it is 100.
- `cost(i)` = `round(cost_coefficient * 100)` — a *lexicographic tie-breaker* so that, when
  coverage is equal, cheaper candidates are preferred. Coverage dominates cost.

The advisory M1..M12 + `score` in the response are computed **post-hoc** in `metrics.py` and are
*not* part of the search objective. `solver.objective_value` is the normalized linearized
objective (slack penalty added back); `best_bound`/`optimality_gap` come from CP-SAT and are
reported only when the returned plan carries no slack (`null` otherwise, because the bounds are
not comparable to a penalized value).

## Determinism

- `num_search_workers = 1` (forced; the request value is ignored and reported as 1).
- fixed `random_seed = request.seed`, no wall-clock-dependent decisions, canonical (sorted)
  variable construction order and a fixed attempt count.
- `deterministic_fingerprint` = `sha256(canonical_json(request) + "|" + solver_version)`.
- `wall_time_ms` lives in the response only and is excluded from the fingerprint by definition.

Same request + seed => byte-identical structural body and identical fingerprint.

## Performance (500 employees x 30 requirements x 16 weeks)

Measured on the synthetic contract-scale fixture (`tests/optimize_helpers.build_scale_request`,
66840 variables / 24170 constraints), `k = 3`, `time_limit_ms = 10000`:

| Variant | Result |
|---|---|
| CP-SAT presolve **on**, 2.5 s/attempt | `UNKNOWN`, **no** variant (budget spent in presolve) |
| CP-SAT presolve **off**, 2.5 s/attempt | `OPTIMAL` in 1.3 s per attempt |
| Whole call, current settings | 3 variants, `OPTIMAL`, search 6.3 s, total 8.4 s |

Two changes made this possible:

1. **Presolve is disabled** (`cp_model_presolve = False`). For this model family (many
   implication constraints) CP-SAT's presolve never yields an incumbent inside the budget.
   Infeasibility is still detected immediately (the contradicting constraints propagate at the
   root) and every synth scenario is `OPTIMAL`, most of them faster.
2. **One aggregate implication per (employee, requirement)** instead of one per week:
   `sum_w a[i,r,w] <= eff_likely_h[r] * y[i,r]`. With `y = 1` the bound is implied by the global
   effort cap, so this is exactly equivalent and cuts the constraint count ~3.4x
   (82670 -> 24170), which also shrinks presolve/build time.

### Graceful timeout (best found + status)

If the first objective-guided attempt returns no incumbent at all (`UNKNOWN`), the solver
spends the *remaining* attempts' search budget on one objective-free feasibility solve and
returns that plan as `FEASIBLE` with the `SOLVER_TIMEOUT` reason code. Such a plan reports
`objective_value`/`best_bound`/`optimality_gap` as `null` — a degraded plan never claims a
quality it did not measure. If even that solve finds nothing, the response is `TIMEOUT` with
`variants: []` and a `c_timeout` blocking constraint. The decision is status-driven, never
clock-driven; `INFEASIBLE` is never softened this way.

## Known limits (documented, not silent)

- `base_variant_id` is accepted; variants are always recomputed from scratch (no warm start).
- `headcount` is accepted; there is no parallelism bonus in the objective.
- M5/M8/M12 are deterministic approximations, not the authoritative backend Monte-Carlo /
  portfolio / dropout computations.
- G3 in the Go validator compares against `likely_person_days`, while the ML model guarantees
  `min_person_days`; the validator is stricter, so a returned variant still passes. Flagged to
  the orchestrator as a cross-zone contract question.
- CP-SAT search is wall-clock bounded, so on a machine much slower than the calibration host an
  *oversized* request (near the 2000 x 60 x 104 contract maxima) may return `FEASIBLE` where the
  calibration host returns `OPTIMAL`. Structural determinism (same input + seed -> same output)
  holds on a given host and is asserted by the tests; all synth scenarios are `OPTIMAL` in
  under 0.4 s, i.e. far below their budget.
