# ADR-004: ML timeout and error policy - fail loudly, degrade explicitly, never silently

- Status: accepted
- Date: 2026-11-05
- Deciders: architecture (orchestrator), backend, ml
- Related: `docs/PLAN.md` 1.2, `contracts/ml-openapi.yaml`, `contracts/reference.yaml` (`ml_policy`, `nfr`), ADR-001
- Supersedes: none

## Context

The core scenario depends on an external Python service that itself depends on an LLM endpoint. Three
different failure classes exist and they must not be treated the same:

1. **Extraction** (`/v1/extract`): slow (seconds), can time out at the LLM, produces *requirements*.
   A failure here blocks a single brief; the user can retry or enter requirements manually.
2. **Embedding** (`/v1/embed`): fast, deterministic, produces *normalization candidates*. A failure
   degrades mapping quality but must not block anything.
3. **Optimization** (`/v1/optimize`): bounded by `time_limit_ms`, CPU-only, produces *the answer*.
   A failure means no variants at all, and a retry costs the full solve time again.

The product rule is "a claim without a source is a bug" and "hard constraints = 0 violations". Silent
degradation - returning a half-valid plan, or a plan built on stale capacity - is worse than an
honest error, because the manager cannot see that the answer is weaker.

## Decision

### 1. Go owns retries; ML owns its own deadline

- The Go ML client sets a per-endpoint timeout and retries according to `ml_policy` in
  `contracts/reference.yaml`. The ML service enforces its own `options.timeout_ms` and answers `408`
  with `retryable: true` instead of hanging.
- No retry on `4xx` (except `408`); no retry on `422` - a schema-valid but semantically invalid request
  will fail identically.
- Budgets:

| Endpoint | Timeout | Retries | On failure |
|---|---|---|---|
| `/v1/extract` | 20 s | 1 (timeout, 5xx, malformed JSON) | brief `failed`, `ML_UNAVAILABLE`; manual entry allowed |
| `/v1/embed` | 5 s | 1 (timeout, 5xx) | exact-synonym matching only, flagged as degraded in the UI |
| `/v1/optimize` | `time_limit_ms` + 2 s grace | 0 | run `failed`, `SOLVER_TIMEOUT`; user may retry with a longer limit |

- **Zero retries for optimization** is deliberate: the request is expensive and deterministic, so a
  retry burns the same CPU without new information. The client returns the failure and the UI offers
  an explicit "retry with 20 s" action.

### 2. Circuit breaker and readiness

- Breaker per endpoint: 5 consecutive failures in 30 s opens it for 60 s, then one half-open probe.
- While the breaker is open, Go short-circuits immediately: `503` to the UI with
  `reason_codes: [ML_UNAVAILABLE]` and an ETA, never a 30 s hang.
- `GET /readyz` on the core reports the dependency state and `degraded_capabilities`, so the UI can
  grey out exactly the buttons that cannot work (contract in `contracts/openapi.yaml`).

### 3. Degradation matrix (what still works when ML is down)

| Capability | ML down | Effect |
|---|---|---|
| View briefs, requirements, runs, variants, audit | works | read-only DB paths |
| Extract a new brief | blocked | `503`, manual requirement entry stays available |
| Normalize competency | degraded | exact synonym match from `contracts/taxonomy.yaml` only |
| Optimize / recalculate | blocked | `503` with `ML_UNAVAILABLE`; no plan is fabricated |
| Explain an existing variant | works | explanations are computed in Go from structured facts (ADR-001) |
| Duration/metrics of an existing variant | works | recomputed in Go, deterministic |

**Cardinal rule:** no component invents data to stay "green". A blocked capability is reported as
blocked, with a reason code and an audit entry.

### 4. Validation of ML output is not optional

- Every ML response is validated before it is used: the **OpenAPI document is normative** for the
  wire shape, and `contracts/json-schema/*` mirrors the structural invariants (mandatory evidence,
  three-point effort, gate consistency). Where the two differ, the OpenAPI wins and the schema is
  fixed in the same PR - two validators must never give two answers.
- A schema failure is `ML_SCHEMA_INVALID`: the response is discarded, the attempt is logged with the
  request id, and the run fails - we never "partially apply" it.
- For `/v1/optimize`, schema validity is necessary but not sufficient: the Go validator re-checks
  G1-G5 and recomputes metrics. Rejected variants are dropped and counted in
  `diagnostics.validator_rejected`; a non-zero value is a tracked defect (METRICS 3.2), not a retry
  trigger.
- Size guards: the product limits (`contracts/reference.yaml`: 30 requirements, 500 candidates,
  500 employees, 104 weeks) are enforced by Go before the call; the ML service also advertises its own
  hard ceilings in `/readyz` and rejects anything above them. Oversized requests never reach the model.

### 5. Timeouts are visible, never hidden

- `run.diagnostics` records `ml_status`, `ml_attempts`, `solve_time_ms`, `fallback_used` and
  `fallback_reason`. The UI shows a badge when a run was produced under degradation.
- `fallback_used = true` may only be set by the documented fallback path (ADR-001, feature-flagged,
  needs its own ADR before production use). A fallback that is not recorded is a bug.
- Audit: every failed run writes an `audit_log` entry with the request id and the reason codes, so
  "the system was down at 14:05" is answerable later.

### 6. LLM-specific rules

- Temperature 0, `prompt_version` recorded, max 2 repair attempts for JSON, then `ML_SCHEMA_INVALID`.
- The LLM never receives candidate data, employee ids, or anything beyond the de-identified brief text.
- If no valid evidence span can be produced for a requirement, the requirement is dropped and counted
  in `diagnostics.dropped_no_evidence`; hallucinated evidence is a hard failure of the brief
  (METRICS 3.1: hallucination rate target 0%).

## Alternatives considered

| Alternative | Why rejected |
|---|---|
| Retry optimization until success | Multiplies CPU cost, hides instability, and invalidates the "recalculation p95 <= 5 s" target |
| Silent fallback to a greedy plan | The manager cannot tell the difference; violates explainability and the 0-violation promise |
| Merge partially valid ML output | Any partially applied plan is unverifiable and unauditable |
| Long timeouts (60 s) with no breaker | Turns an ML outage into a global core outage (thread/connection exhaustion) |
| Client-side (UI) retries | No visibility, no idempotency, duplicates runs |

## Consequences

- The system has a defined, testable failure surface: `qa-eval` injects timeouts, 5xx, malformed JSON
  and slow responses and asserts the degradation matrix above.
- Users see fewer features during an outage but never a wrong plan - which is the whole point of a
  decision-support system.
- The Go client becomes a small state machine (timeout, retry, breaker, readiness) that must be
  covered by unit tests; the alternative (ad-hoc error handling at each call site) is how inconsistent
  behaviour appears.
