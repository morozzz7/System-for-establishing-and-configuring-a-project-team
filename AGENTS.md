# Optimal Project Team Formation System

Decision-support system for resource managers. Given a new technical brief (TZ), it estimates required
roles/competencies and effort, matches them against employee profiles, current workload and planned
absences, builds 2-3 feasible team variants with duration estimates, shows competency/capacity deficits
(incl. need for external cooperation), explains every include/exclude decision, and recalculates after
manual constraint edits. A human always makes the final decision (human-in-the-loop).

Sources: `паспорт_проекта.docx`, `10_Система_формирования_оптимальной_проектной_команды.docx`.
Quality metrics: `docs/METRICS.md` (mandatory to implement and measure).
UI language: Russian (i18n-ready). Code, comments, docs, prompts: English.

## Stack
| Layer | Tech |
|---|---|
| **Backend (core)** | **Go 1.23+**: chi, pgx, sqlc, goose, oapi-codegen, slog, testify, testcontainers-go |
| DB | PostgreSQL 16 + pgvector (optional) |
| **ML service** | **Python 3.12 + FastAPI, ML/optimization ONLY**: LLM client, spaCy/Transformers, embeddings, OR-Tools CP-SAT |
| Frontend | React + TypeScript, Ant Design, Apache ECharts |
| Tests | `go test`, pytest, Vitest, Playwright |
| Infra | Docker Compose, Git, GitHub Actions / GitLab CI |

## Key architectural decision
OR-Tools CP-SAT has no supported Go API, so the optimizer lives in the ML service. Principle:
**"solver proposes, Go verifies"**.
- Go independently re-checks ALL hard constraints of every variant (`internal/validator`) and recomputes
  duration, deficits and metrics. Invalid variants are dropped and logged.
- The LLM never decides assignments. It only structures brief text.
- Fallback (needs its own ADR, never switch silently): custom Go solver (ILP/branch-and-bound).

## Pipeline
1. `POST /api/v1/briefs`: brief text/file input (Go).
2. Go -> ML `POST /v1/extract` -> `Requirement[]` (role, competency, level, criticality must/should/nice,
   effort min/likely/max in person-days, assumptions, confidence, **evidence** span from the brief).
3. Normalize competencies to taxonomy (ML `/v1/embed` + pgvector search in Go; human review on low confidence).
4. Go builds availability: capacity(employee, week) = norm - current assignments - absences (Russian production calendar).
5. Go -> ML `POST /v1/optimize` (candidates, requirements, constraints, weights, k) -> variants.
6. Go: validator -> schedule engine (duration, P50/P80) -> deficits -> portfolio impact -> metrics.
7. Explainer: reasons from structured facts (reason codes).
8. UI: compare variants, explanations, constraint editor -> `POST /api/v1/runs/{id}/recalculate`.

## Repository layout
```
backend/          Go: cmd/api, internal/{domain,profile,brief,availability,validator,schedule,explain,metrics,mlclient,runs,platform}
ml/               FastAPI: app/{extract,embed,optimize,explain,schemas}, tests/
frontend/         React + TS
db/migrations/    goose migrations; db/seed/ synthetic data
contracts/        openapi.yaml (UI<->Go), ml-openapi.yaml (Go<->ML), JSON Schemas: single source of truth
eval/             benchmark, synthetic generator, metric computation
deploy/           docker-compose.yml, Dockerfiles
docs/             PLAN.md, ADR/, METRICS.md
```

## Data model (minimum)
employee, competency (taxonomy), employee_competency (level 1-5, last_used, confidence), project,
assignment (employee, project, week, % load), absence, brief, requirement, team_run, team_variant,
variant_member, deficit, constraint_override, audit_log.

## Hard rules for all agents
1. **Contract-first.** API change = edit `contracts/` first, then code. Never hand-edit generated code.
2. **Privacy.** Synthetic/anonymized data only. No names, salaries or real personal data in repo, logs, LLM prompts, fixtures. Rates via grades. Employee id = pseudonym.
3. **Determinism.** Duration/load/validation math: no LLM, no randomness (except Monte Carlo with fixed seed).
4. **Explainability.** Every output cites DB facts or a brief fragment. A claim without a source is a bug.
5. **Explicit assumptions.** Effort estimates carry assumptions and a range, never a single number.
6. **Hard constraints = 0 violations.** Never assign an unavailable or overloaded employee, whatever the weights.
7. **Definition of Done:** code + green tests + updated contract + linters + task ticked in `docs/PLAN.md` with a log line + affected metrics re-measured.
8. No `git push`, no data deletion, no edits outside your zone: route cross-zone work via orchestrator.
