# Contracts (single source of truth)

Everything that crosses a process or team boundary is defined here first. Code follows the contract,
never the other way round, and generated code is never hand-edited.

```
contracts/
  openapi.yaml                     UI <-> Go core API (/api/v1), observability, ref data
  ml-openapi.yaml                  Go <-> ML service (/v1/extract, /v1/embed, /v1/optimize, health)
  taxonomy.yaml                    roles -> competencies -> levels 1..5, RU/EN synonyms (seed v1)
  reference.yaml                   grades + cost coefficients, weight presets, planning params, NFRs, ML policy
  json-schema/
    common.schema.json             week keys, levels, criticality, evidence span, effort range
    requirement.schema.json        extracted requirement (evidence + assumptions mandatory)
    team-variant.schema.json       variant with weekly assignments, gates, metrics, deficits
    optimize-request.schema.json   exact optimizer input (capacity matrix is authoritative)
    optimize-response.schema.json  exact optimizer output (advisory metrics)
    taxonomy.schema.json           structure of taxonomy.yaml
  spectral.yaml                    lint ruleset for both OpenAPI documents
```

## Versioning

- API version is in the path: `/api/v1`, `/v1/*`. A breaking change means a **new major path**, and
  both versions live side by side until the old one is retired. Breaking = removing/renaming a field,
  tightening a validation, changing a type or the meaning of a number, changing an enum value.
- Additive changes (new optional field, new enum value the client must tolerate, new endpoint) stay in
  the same version. Clients must ignore unknown fields.
- `info.version` (semver) tracks the document; `x-contract-version` semantics are carried by
  `contract_version: v1` inside `RunDiagnostics` and in `taxonomy.yaml` / `reference.yaml`.
- Reason code lists are additive. Codes are never reused for a different meaning; retired codes stay
  listed as deprecated.
- Any change to a schema that `oapi-codegen`/`datamodel-code-generator` consumes requires regenerating
  both sides in the same PR (backend + ml), and the contract test must be green before merge.

## Linting and validation

Run before every commit that touches this directory (wired into 1.3 `/setup` CI):

```bash
# OpenAPI structure + examples (both documents)
npx --yes @stoplight/spectral-cli lint contracts/openapi.yaml contracts/ml-openapi.yaml \
  --ruleset contracts/spectral.yaml

# JSON Schemas: meta-validation + fixtures
npx --yes ajv-cli@5 compile -s contracts/json-schema/common.schema.json -s contracts/json-schema/requirement.schema.json \
  -s contracts/json-schema/team-variant.schema.json -s contracts/json-schema/optimize-request.schema.json \
  -s contracts/json-schema/optimize-response.schema.json -s contracts/json-schema/taxonomy.schema.json

# Golden examples must round-trip: every `example` in the specs is validated against its schema
# (implemented as a contract test in backend/internal/contracttest and ml/tests/test_contracts.py)
```

Acceptance for task 1.1: spectral exits 0 with `error` severity only, and every operation has a
request example (where there is a body) and a response example.

## Reason codes (canonical list)

Codes are the only machine-readable explanation channel; UI texts are rendered from codes + `facts`.
The enum lives in `openapi.yaml#/components/schemas/ReasonCode` (full list) and
`ml-openapi.yaml#/components/schemas/ReasonCode` (ML-relevant subset). Keep them in sync - a contract
test asserts the subset relation.

| Group | Codes |
|---|---|
| Inclusion | `INCLUDE_HIGH_COMPETENCE`, `INCLUDE_AVAILABLE_CAPACITY`, `INCLUDE_COST_EFFICIENT`, `INCLUDE_REQUIRED_ROLE`, `INCLUDE_BUS_FACTOR`, `INCLUDE_BENCH`, `INCLUDE_EXPERIENCE_RELEVANT`, `INCLUDE_USER_FORCE` |
| Exclusion | `EXCLUDE_OVERLOADED`, `EXCLUDE_ABSENT`, `EXCLUDE_BELOW_REQUIRED_LEVEL`, `EXCLUDE_EXPENSIVE`, `EXCLUDE_USER_FORCE`, `EXCLUDE_TEAM_SIZE_LIMIT`, `EXCLUDE_NO_FREE_CAPACITY`, `EXCLUDE_PORTFOLIO_PROTECTION`, `EXCLUDE_DATA_INCOMPLETE` |
| Deficits | `DEFICIT_MUST_ROLE`, `DEFICIT_EXTERNAL_CONTRACTOR`, `DEFICIT_UPSKILL_INTERNAL`, `DEFICIT_DEADLINE_SHIFT`, `DEFICIT_DESCOPE` |
| Constraints | `CONSTRAINT_ACTIVE_TEAM_SIZE`, `CONSTRAINT_ACTIVE_BUDGET`, `CONSTRAINT_ACTIVE_DEADLINE`, `CONSTRAINT_ACTIVE_FORCE_INCLUDE`, `CONSTRAINT_ACTIVE_FORCE_EXCLUDE`, `CONSTRAINT_ACTIVE_BUS_FACTOR`, `CONSTRAINT_BINDING_CAPACITY` |
| Variants | `VARIANT_FASTEST`, `VARIANT_CHEAPEST`, `VARIANT_LOWEST_RISK`, `VARIANT_BALANCED`, `VARIANT_DOMINATED_REPLACED` |
| Trade-offs / counterfactuals | `TRADEOFF_SPEED_VS_DEFICIT`, `TRADEOFF_COST_VS_SKILL`, `TRADEOFF_LOAD_VS_SPEED`, `COUNTERFACTUAL_TEAM_SIZE`, `COUNTERFACTUAL_DEADLINE`, `COUNTERFACTUAL_CAPACITY` |
| Extraction / normalization | `REQ_LOW_CONFIDENCE`, `REQ_NEEDS_REVIEW`, `REQ_EFFORT_WIDE_RANGE`, `REQ_EVIDENCE_MISSING`, `REQ_TAXONOMY_LOW_CONFIDENCE`, `NORMALIZE_TOP1_STRONG`, `NORMALIZE_TOP1_WEAK` |
| Gates (G1-G5) | `G1_UNAVAILABLE_ASSIGNED`, `G2_OVERLOAD`, `G3_UNCOVERED_MUST`, `G4_USER_CONSTRAINT_VIOLATED`, `G5_INVALID_DATA` |
| Solver / ML | `VARIANT_REJECTED_BY_VALIDATOR`, `VARIANT_DIVERSITY_TOO_LOW`, `SOLVER_INFEASIBLE`, `SOLVER_TIMEOUT`, `SOLVER_UNKNOWN`, `ML_UNAVAILABLE`, `ML_TIMEOUT`, `ML_SCHEMA_INVALID`, `ML_LLM_UNAVAILABLE`, `FALLBACK_SOLVER_USED` |
| Recalculation / decisions | `RECALC_QUEUED`, `RECALC_NO_EFFECT`, `OVERRIDE_VIOLATES_G1`, `OVERRIDE_VIOLATES_G2`, `OVERRIDE_VIOLATES_G4`, `OVERRIDE_REJECTED`, `DECISION_IMMUTABLE` |
| Validation / data | `VALIDATION_FAILED`, `DUPLICATE_EMPLOYEE_ID`, `ABSENCE_OVERLAP`, `BRIEF_NOT_CONFIRMED`, `BRIEF_STATE_CONFLICT`, `AUDIT_TRUNCATED` |

## Privacy rules for these documents

- No personal data anywhere: ids are pseudonyms (`emp_*`, `brf_*`, `run_*`, `var_*`, `req_*`, `prj_*`,
  `abs_*`, `asg_*`, `aud_*`, `exp_*`), no names, e-mails, phones, addresses, salaries.
- Cost is a dimensionless coefficient by grade (`reference.yaml`), never currency.
- A free-text field exists only where a human writes a rationale (`RequirementPatch.comment`,
  `ConstraintOverride.reason`, `DecisionRequest.comment`, `assumptions`). These are user-authored and
  must not be auto-filled with personal data; they are stored in the audit trail.
- Examples in this directory are synthetic and consistent with each other (same ids, same weeks).

## Ownership

| Path | Owner | Consumers |
|---|---|---|
| `openapi.yaml` | orchestrator (architecture) | backend, frontend, qa-eval |
| `ml-openapi.yaml` | orchestrator | ml, backend |
| `taxonomy.yaml`, `reference.yaml` | orchestrator | backend (load), ml (vocabulary), qa-eval (gold labels) |
| `json-schema/*` | orchestrator | backend, ml, qa-eval |

Changes are proposed by the consuming team as a contract PR; the architect merges. Cross-zone needs go
through the orchestrator, never through a direct edit of another team's generated code.
