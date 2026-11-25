# Optimal Project Team Formation System

Decision-support system for resource managers. Given a new technical brief (TZ), it estimates required
roles/competencies and effort, matches them against employee profiles, current workload and planned
absences, builds 2-3 feasible team variants with duration estimates, shows competency/capacity deficits
(incl. need for external cooperation), explains every include/exclude decision, and recalculates after
manual constraint edits. A human always makes the final decision (human-in-the-loop).

Sources: `паспорт_проекта.docx`, `10_Система_формирования_оптимальной_проектной_команды.docx`.
Quality metrics: `docs/METRICS.md` (mandatory to implement and measure). Task status: `docs/PLAN.md`
(authoritative; ticks and the log line live there).
UI language: Russian (i18n-ready; strings are currently hardcoded `ru-RU`). Code, comments, docs, prompts: English.

## Stack (what the code uses today)

| Layer | Tech |
|---|---|
| **Backend (core)** | **Go** (`go 1.26` in `backend/go.mod`, built with `golang:1.27-alpine`): chi, pgx, goose, slog, testify, testcontainers-go |
| DB | PostgreSQL 16 + pgvector image (`pgvector/pgvector:pg16`); pgvector ranking is not used yet |
| **ML service** | **Python 3.12 + FastAPI, ML/optimization ONLY**: pydantic, httpx, ruamel.yaml, OR-Tools CP-SAT, pytest |
| Frontend | React 18 + TypeScript, Vite, Ant Design, React Router; Vitest + Testing Library; deterministic mocks by default |
| Tests | `go test` (incl. testcontainers), pytest, Vitest, Playwright (E2E, full Docker stack, CI-verified); frontend checks are run separately from `make test` |
| Infra | Docker Compose, nginx gateway, Makefile, GitHub Actions CI (`.github/workflows/ci.yml`) |

## Target, not implemented yet

Keep these as the intended architecture; do not describe them as current in code, docs or reports.

- sqlc / oapi-codegen code generation (repositories and DTOs are hand-written pgx/Go today).
- spaCy/Transformers pipelines (embeddings are a deterministic hashing char-ngram baseline, 1024d).
- pgvector ranking for competency normalization (exact-synonym baseline today).
- Apache ECharts on the frontend (Ant Design only today).

## Key architectural decision

OR-Tools CP-SAT has no supported Go API, so the optimizer lives in the ML service. Principle:
**"solver proposes, Go verifies"**.
- Go independently re-checks ALL hard constraints of every variant (`internal/validator`) and recomputes
  duration, deficits and metrics. Invalid variants are dropped and logged.
- The LLM never decides assignments. It only structures brief text.
- Fallback (needs its own ADR, never switch silently): custom Go solver (ILP/branch-and-bound).

## Pipeline
0. `POST /api/v1/auth/login`: local HS256 JWT provider; the bearer token is verified in trusted
   middleware and the `sub` claim becomes the acting principal in the request context. Clients cannot
   assert identity: `decided_by`, `edited_by` and `actor` are server-derived echoes. `/healthz`,
   `/readyz` and `/auth/login` are public.
1. `POST /api/v1/briefs`: brief text/file input (Go).
2. Go -> ML `POST /v1/extract` -> `Requirement[]` (role, competency, level, criticality must/should/nice,
   effort min/likely/max in person-days, assumptions, confidence, **evidence** span from the brief).
3. Normalize competencies to taxonomy (ML `/v1/embed` + `/api/v1/taxonomy/normalize`; exact-synonym
   ranking with `needs_review` on low confidence. **Target:** pgvector ranking in Go).
4. Go builds availability: capacity(employee, week) = norm - current assignments - absences.
   The norm, working days and absence minutes come from the versioned Russian production calendar
   (`contracts/calendar-ru-YYYY.yaml`, embedded in `internal/domain/data/`); a year without calendar
   data is a G5 hard failure, never an implicit five-day week. The candidate pool comes from
   active employees, their competency levels and current assignments (`internal/service/candidates.go`),
   filtered by `CandidateFilter`; the in-memory stand-ins remain only for tests. Bulk import
   (`PUT /api/v1/employees`) applies the same G1/G2 checks before writing assignments and absences.
5. Go -> ML `POST /v1/optimize` (candidates, requirements, constraints, weights, k) -> variants.
6. Go: validator -> schedule engine (duration, P50/P80) -> deficits -> portfolio impact -> metrics.
   The G3 gate and solver feasibility use `min_person_days`; M2 and reported deficits use `likely_person_days`
   (ADR-007 §1); role-only requirements are covered by a matching role profile. When `horizon` is
   omitted from `POST /runs`, the window is taken from the brief
   (`requested_start_week`/`deadline_week`, else ISO weeks found in its text).
7. Explainer: reasons from structured facts (reason codes).
8. UI: compare variants, explanations, constraint editor -> `POST /api/v1/runs/{id}/recalculate`.
   Approving a variant records the decision and registers the derived project (`POST /api/v1/projects`)
   so it appears in the Projects portfolio.

## Repository layout
```
backend/          Go: cmd/{api,seed}, internal/{domain,handler,service,repository,validator,schedule,
                  explain,metrics,mlclient,runinput,migrations,platform}; platform/auth holds the JWT provider,
                  domain/data/ embeds the versioned calendar mirrors
ml/               FastAPI: app/{extract,optimize,llm}, app/embed.py, app/main.py, tests/
frontend/         React + TS (Vite, Ant Design): src/{app,pages,components,api,mocks,types,hooks,utils}, e2e/ (Playwright)
db/migrations/    goose migrations; seed data is loaded by backend/cmd/seed (`make seed`)
contracts/        openapi.yaml (UI<->Go), ml-openapi.yaml (Go<->ML), taxonomy.yaml, reference.yaml,
                  calendar-ru-YYYY.yaml, spectral.yaml, json-schema/: single source of truth
.github/workflows/ ci.yml: contracts, backend, ML, frontend and full-stack E2E jobs
eval/             gold briefs, synthetic generator, benchmark/metric scripts, reports
deploy/           docker-compose.yml (+ override), docker-compose.e2e.yml (+ e2e/llm_stub.py), Dockerfile.{backend,ml,frontend}, nginx gateway
docs/             PLAN.md (task status), ADR/, METRICS.md
.opencode/skills/ local agent skills (git-ignored, see "Agent skills")
```

## Data model (implemented tables)
- Reference: `level_descriptor`, `role`, `competency`, `competency_synonym`, `role_competency`,
  `grade_band`, `weight_preset`.
- Resources: `employee`, `employee_competency`, `project`, `assignment`, `absence`.
- Brief: `brief`, `requirement`, `requirement_assumption`, `requirement_evidence`.
- Runs: `team_run`, `run_snapshot`, `run_snapshot_candidate`, `team_variant`, `variant_member`,
  `variant_weekly_assignment`, `variant_deficit`, `variant_recommended_action`,
  `variant_active_constraint`, `variant_excluded_candidate`.
- Audit and decisions: `audit_log`, `constraint_override`, `decision`, `explanation`.
- Migration `00010` stores the full run-input snapshot payload (`run_snapshot.data_jsonb`) and
  authoritative response payloads, so runs, variants, recalculation lineage, explanations,
  decisions and audit events survive a restart (including `GET /runs/{id}/inputs`); a decision and its
  audit event commit in one transaction. Migration `00011` adds `brief.data_jsonb` with the full
  extracted brief (requirements, review split, diagnostics), so a new run can be created after a
  restart without re-uploading the brief. `PgComputedRunRepo.CreateComputed` writes the run, snapshot,
  variants and both explanation locales in a single transaction, so a mid-write failure cannot leave
  an orphan run. The HS256 token provider in `platform/auth` derives the acting principal; every
  decision/audit actor is server-side and client identity assertions are ignored.
  M8 attributes pulled hours only to portfolio projects that
  share staff with the proposed team (`employee_ids`). The in-memory maps in `WorkflowService` remain
  the no-DB fallback for unit tests. M8 portfolio inputs are derived from active projects and
  their capacity matrix; `deficit_weeks` is a counterfactual P50 rerun, omitted with a reason
  code when no capacity exists for the simulation. Go's ML client implements ADR-004
  per-endpoint timeouts, bounded retries and per-endpoint circuit breakers.

## Verification commands
```bash
make lint-contracts # spectral + JSON Schema validation (offline Python gate)
make fixtures      # regenerate eval/gold briefs and eval/synth scenarios (gitignored)
make test          # contracts + fixtures + Go tests + pytest (frontend excluded)
make lint-backend  # golangci-lint
make lint-ml       # ruff + black --check
make test-e2e      # full Docker stack + Playwright journey; needs frontend npm ci first
cd frontend && npm test -- --run && npm run lint && npm run build
```
The same gates run in GitHub Actions (`contracts`, `backend`, `ml`, `frontend`, `e2e` jobs). Auth is
configured via `AUTH_JWT_SECRET`, `AUTH_TOKEN_TTL_MINUTES` and `AUTH_USERS` (see `.env.example`);
the frontend uses `VITE_AUTH_LOGIN`/`VITE_AUTH_PASSWORD` to obtain a token in HTTP mode.

Runtime ports, gateway URLs and the mock/API switch (`VITE_API_BASE_URL`) are documented in `README.md`
and `frontend/README.md`; do not duplicate them here.

## Agent skills (`.opencode/skills`, local, git-ignored)
| Skill | Use when |
|---|---|
| `modern-web-guidance` | Start of any HTML/CSS/client-side JS work (dialogs, forms, a11y, CWV); vendored offline CLI |
| `frontend-design` | Choosing a visual direction, typography, non-templated layout |
| `unslop` | Removing "AI-looking" UI; polish pass on an existing screen |
| `emil-design-eng` | Component/animation craft and interaction details |
| `vercel-react-best-practices` | Writing, reviewing or refactoring React components |
| `web-design-guidelines` | UI/accessibility audit with `file:line` findings |
| `webapp-testing` | Driving a local app with Playwright scripts |

`modern-web-guidance` is vendored (no network): run
`node .opencode/skills/modern-web-guidance/modern-web.mjs search "<query>"` from the repo root.

## Hard rules for all agents
1. **Contract-first.** API change = edit `contracts/` first, then code. Never hand-edit generated code.
2. **Privacy.** Synthetic/anonymized data only. No names, salaries or real personal data in repo, logs, LLM prompts, fixtures. Rates via grades. Employee id = pseudonym.
3. **Determinism.** Duration/load/validation math: no LLM, no randomness (except Monte Carlo with fixed seed).
4. **Explainability.** Every output cites DB facts or a brief fragment. A claim without a source is a bug.
5. **Explicit assumptions.** Effort estimates carry assumptions and a range, never a single number.
6. **Hard constraints = 0 violations.** Never assign an unavailable or overloaded employee, whatever the weights.
7. **Definition of Done:** code + green tests + updated contract + linters + task ticked in `docs/PLAN.md` with a log line + affected metrics re-measured + `AGENTS.md` updated if structure, stack or conventions changed.
8. No `git push`, no data deletion, no edits outside your zone. Cross-zone work is routed through the orchestrator; in a single-agent session, report it instead of doing it silently.
9. **No filler comments.** Self-describing names of functions, types and variables carry the intent; do not restate code in a comment. Comment only the non-obvious "why": gates, formulas, external API quirks, workarounds, invariants. No commented-out code, no task/changelog notes in code, no banner comments. Doc comments only where they add information beyond the name.
10. **`AGENTS.md` is part of the change.** Any change to stack/versions, repository layout, pipeline or endpoints, data model, commands, conventions or agent skills updates the affected section of this file in the same task.
