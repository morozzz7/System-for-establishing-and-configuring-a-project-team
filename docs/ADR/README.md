# Architecture Decision Records

One significant decision = one ADR. An accepted ADR is never edited into a different decision: write a
new ADR that supersedes it and mark the old one `superseded by ADR-XXX`. Every ADR states the numbers
that make the decision testable, so `qa-eval` and `reviewer` can check it.

| ADR | Title | Status | Decided | Affects |
|---|---|---|---|---|
| [ADR-001](ADR-001-optimizer-location.md) | The optimizer lives in the ML service; Go verifies | accepted | 2026-11-05 | service boundary, `ml-openapi.yaml` `/v1/optimize`, validator, metrics |
| [ADR-002](ADR-002-time-model.md) | Time model: ISO weeks, integer hours, three-point effort, explicit reserve | accepted | 2026-11-05 | availability, schedule engine, M2/M5/M6, UI duration display |
| [ADR-003](ADR-003-availability-model.md) | Availability: one authoritative capacity matrix, computed only in Go | accepted | 2026-11-05 | G1/G2, `weekly_capacity`, absences, recalculation |
| [ADR-004](ADR-004-ml-timeout-error-policy.md) | ML timeout and error policy: fail loudly, degrade explicitly | accepted | 2026-11-05 | ml client, readiness, UI degradation, audit |
| [ADR-005](ADR-005-extraction-strategy.md) | Extraction: LLM-first with deterministic validation | accepted | 2026-11-08 | `/v1/extract`, prompt `extract-v3`, validator, eval |
| [ADR-006](ADR-006-review-routing-and-mention-quality.md) | Review routing + deterministic mention-quality gates for `/v1/extract` | accepted | 2026-11-09 | `ExtractResponse.requirements_for_review`, `diagnostics.verification`, scanner, eval |

## Template

```markdown
# ADR-NNN: <decision in one line>

- Status: proposed | accepted | superseded by ADR-XXX
- Date: YYYY-MM-DD
- Deciders: <roles>
- Related: <tasks, files, ADRs>
- Supersedes: <ADR or none>

## Context
<forces, constraints, failure modes we are avoiding>

## Decision
<the decision, with numbers>

## Alternatives considered
<table: alternative | why rejected>

## Consequences
<positive / negative / risks / what becomes testable>
```

## Open decisions (not yet ADRs)

These are known and deliberately unresolved; each blocks the task in brackets and needs a human
decision first (see `docs/PLAN.md` "Open questions"):

- LLM hosting: cloud API vs on-premise model (task 3.1). Affects latency budget and data residency.
- Fallback Go solver algorithm and its acceptance threshold (only if ADR-001 §6 is ever used).
- Calibration of metric weights against expert PMs (task 4.6, Bradley-Terry; METRICS section 6).
- Long-term storage of variants after a project closes (retention vs audit retention, 12 months).
