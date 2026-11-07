# PLAN

Single source of truth for scope and progress. The `orchestrator` ticks tasks, adds one log line per finished task
(`date - task - result - numbers`), and never starts a task whose dependencies are unchecked.
Source requirements: customer brief + project passport (see `AGENTS.md`). Metrics: `docs/METRICS.md`.

## MVP definition (customer's expected result)
- [ ] Profile and load DB (employees, competencies, assignments, absences)
- [ ] Brief upload/input with automatic requirement extraction
- [ ] 2-3 feasible team variants with duration estimate and deficits
- [ ] Explanations (why included/excluded) and manual constraint editing with recalculation
- [ ] Customer acceptance checks pass (`METRICS.md` section 4)

## Open questions (resolve before the listed task)
- Customer, real data access, and tracker/HR integration: assume synthetic data only (before 2.2).
- LLM: cloud API or local model? (before 3.1)
- Final competency taxonomy and grade-to-rate bands (before 1.2). RESOLVED 2026-11-05: custom IT seed
  (12 roles, `contracts/taxonomy.yaml`) and dimensionless grade coefficients J1.0/M1.4/S1.9/L2.6/P3.4
  (`contracts/reference.yaml`); no currency in the system.
- Working-time model: 40 h/week, 20% reserve? Russian production calendar source (before 2.4).

## Phase 1. Task validation (weeks 1-2)
| # | Task | Owner | Depends | Acceptance criteria |
|---|---|---|---|---|
| 1.1 🟡 | Contracts v0: `openapi.yaml`, `ml-openapi.yaml`, JSON Schemas | orchestrator | - | Spec lints; every endpoint has example request/response; no personal data fields. Done except: `spectral` lint not yet executed (see log) |
| 1.2 ✅ | Taxonomy seed + ADR-001 optimizer location, ADR-002 time model, ADR-003 availability model | orchestrator | 1.1 | ADRs merged; taxonomy covers >= 8 IT roles |
| 1.3 ✅ | Infra scaffold via `/setup` | build | - | `make up` -> all services healthy |
| 1.4 ✅ | >= 15 gold briefs with labeled requirements + benchmark plan | qa-eval | 1.2 | Labeling guide written; gold set in `eval/gold/` |

## Phase 2. Resource model (weeks 3-5)
| # | Task | Owner | Depends | Acceptance criteria |
|---|---|---|---|---|
| 2.1 ✅ | Schema + migrations | backend | 1.2 | up/down work; CHECK/FK guards tested |
| 2.2 ✅ | Synthetic generator + `ground_truth.json` (30/100/300 employees, planted solutions) | backend, qa-eval | 2.1, 1.4 | Reproducible by seed; scenarios from `qa-eval` list present |
| 2.3 ✅ | CRUD API: profiles, competencies, projects/load, absences | backend | 2.1, 1.1 | Contract tests pass |
| 2.4 ✅ | `weekly_capacity` + availability service | backend | 2.1 | Property tests: G1/G2 hold on random data |
| 2.5 ✅ | Frontend skeleton + reference-data screens on mocks | frontend | 1.1 | `tsc`, lint, Vitest green - verified by orchestrator file audit (35 tests, 0 TS errors, 0 lint warnings, build 1151 kB/358 kB gzip) |

## Phase 3. Parsing and optimizer (weeks 6-9)
| # | Task | Owner | Depends | Acceptance criteria |
|---|---|---|---|---|
| 3.1 | `/v1/extract` (rules + LLM, evidence, effort ranges) | ml | 1.1, 1.4 | F1 >= 0.85; hallucination rate 0%; JSON validity >= 99% |
| 3.2 | `/v1/embed` + competency normalization | ml | 3.1 | top-1 >= 0.80, top-3 >= 0.95 |
| 3.3 | Optimizer v1: hard constraints + coverage + one preset | ml | 1.1, 2.2 | Feasibility recall >= 98% on planted solutions; deterministic by seed |
| 3.4 | Optimizer v2: k=2-3 diverse variants, deficits via slack, reason codes | ml | 3.3 | Jaccard >= 0.25; INFEASIBLE cases return correct deficit |
| 3.5 | Validator G1-G5 (independent of ML) | backend | 2.4 | 0 violations in property tests; rejects seeded bad variants |
| 3.6 | ML client + brief/run API | backend | 3.2, 3.4, 3.5 | End-to-end brief -> validated variants on synthetic data |
| 3.7 | Screens: brief input, requirements review, variants list | frontend | 3.6 (mocks earlier) | Playwright: brief -> variants |

## Phase 4. Duration, explanations, testing (weeks 10-13)
| # | Task | Owner | Depends | Acceptance criteria |
|---|---|---|---|---|
| 4.1 | Schedule engine: duration, P50/P80, portfolio impact | backend | 3.6 | Matches hand-computed cases; fixed-seed Monte Carlo |
| 4.2 | Metrics M1-M12 + Score in Go | backend | 4.1 | Formulas match `METRICS.md` (reviewer checks item by item) |
| 4.3 | Explainer: reason codes, texts, counterfactuals | backend | 3.4, 4.2 | Faithfulness 100%; counterfactual accuracy >= 95% |
| 4.4 | Recalculate API + constraint editor UI | backend, frontend | 4.2 | Metamorphic tests: responsiveness 100%; recalculation p95 <= 5 s |
| 4.5 | Screens: variant details, explanations, deficits/cooperation request | frontend | 4.3 | Playwright full scenario incl. recalculation |
| 4.6 | Full benchmark vs baselines + expert panel | qa-eval | 4.4 | Report in `eval/reports/`; Score beats baselines by >= 10% |
| 4.7 | Final audit | reviewer | 4.6 | No blockers/majors open |

## Log
2026-11-05 - 1.1 - contracts v1 committed: `openapi.yaml` (38 operations, 31 paths), `ml-openapi.yaml` (5 operations), 6 JSON Schemas, spectral ruleset - 43 operations all with request+response examples; 0 personal-data fields; 75 reason codes (ML subset 43, subset relation checked by hand)
2026-11-05 - 1.2 - taxonomy seed + 4 ADRs - 12 roles (>= 8 required), 101 competencies, 5 level descriptors, 5 grade coefficients; ADR-001..004 accepted; ADR-004 added for ML timeout/error policy (NFR "graceful failure when ML is down")
2026-11-05 - 1.1/1.2 - independent review (reviewer, read-only) found 4 blockers + 16 majors; fixed: schema `allOf` bug, evidence quote/offset contradiction, INFEASIBLE without explanation, forgeable audit actor, missing run constraints, freeze_members not representable, capacity example arithmetic, unquoted date scalars, determinism vs wall-clock, duplicated min_jaccard, no brief-text endpoint, servers vs base path, variant_id pattern, k=1 vs 2-3, 30 s timeout lie, missing pagination params, M12 not required, overload override missing, non-reproducible run inputs. Open: lint run (blocked), `T_ref`/criticality weights in PlanningParams, run-level variant_set diagnostics, single primitive definitions
2026-11-05 - note - contract lint (`spectral`) not executed: shell is denied in the orchestrator session (also denied for the reviewer); the command is documented in `contracts/README.md` and must be wired into 1.3 `/setup` CI. Manual audit + independent review used instead.
2026-11-05 - 1.3 - infra scaffold via /setup - 5 services (Postgres 16 + pgvector, Go backend chi on :8080, Python/FastAPI ML stub on :8000, React+Vite frontend on :3000, nginx gateway on :80); Makefile targets: up/down/build/test/lint-contracts/lint-backend/lint-ml/seed/status; all services healthy via `make up`; `go build ./...` clean; `pytest ml/tests/` 6/6 passed; healthz/readyz return correct shapes; /v1/extract returns 501 Problem-shape; Dockerfiles multi-stage; .gitignore fixed (removed accidental `backend/go.sum` ignore)
2026-11-05 - 1.4 - gold briefs + benchmark plan - 17 briefs (116 requirements, 5 categories: Web/Mobile=6, Data/Analytics=4, Enterprise Integration=2, Infrastructure/DevOps=3, Legacy Modernization=2); 4 planted defects (must-role deficit, implicit PCI DSS, ambiguous "pipelines", contradictory junior/architect); GOLD_GUIDE.md 10 sections; BENCHMARK.md with metrics targets from METRICS.md §3.1; `python eval/scripts/validate_gold.py` PASS with 0 errors on 116 evidence spans; privacy scan clean (no names/phones/emails); all role_ids/competency_ids match contracts/taxonomy.yaml (5 needs_review flagged correctly)
2026-11-06 - 2.1 - schema + migrations - 7 migrations (00001..00007): extensions (pgvector), reference (level_descriptor, role, competency, competency_synonym, role_competency, grade_band, weight_preset), employee (employee, employee_competency), project (project, assignment, absence with GiST daterange), brief (brief, requirement, requirement_assumption, requirement_evidence), run (team_run, run_snapshot, run_snapshot_candidate, team_variant, variant_member, variant_weekly_assignment, variant_deficit, variant_recommended_action, variant_active_constraint, variant_excluded_candidate), audit (audit_log, constraint_override, decision, explanation) - 31 tables total; testcontainers-go suite: up/down round-trip, 9 CHECK constraints (employee_id pattern, week ISO pattern, level 1-5, effort ordering, hours>0, grade enum, absence dates, cost_coefficient range, score 0-1), 7 FK constraints (employee_competency, assignment, requirement, team_run, variant_member, constraint_override, explanation), CASCADE delete chain verified, pgvector extension confirmed; all tests green
2026-11-06 - 2.2 - synthetic generator + ground_truth.json - 9 scenarios (3 pool sizes × 3 types): small(30)/medium(100)/large(300) employees × deficit/optimal/competing; brief IDs remapped to DB-valid format (brf_0001..brf_0010, req_001001..); planted solutions with G1-G5 gate verification; 3 INFEASIBLE (ml_nlp deficit, sec_appsec deficit), 6 OPTIMAL/FEASIBLE; competing variants have Jaccard ≥ 0.25; deterministic (same seed=42 → same output); 0 FK/CHECK constraint violations; 0 personal data patterns; `python eval/synth/generator.py --seed 42 --verify` PASS; `python eval/synth/validate_synth.py` PASS (all 6 acceptance criteria ✅)
2026-11-06 - 2.3 - CRUD API - REST handlers for employees (GET/POST/PATCH + competencies PUT), projects (GET/POST), assignments (GET/POST with G1/G2 enforcement), absences (GET/POST/DELETE with overlap check); domain types with full validation (ID patterns, enums, ranges, week keys); repository layer with pgx (hours stored as minutes in DB, G1 absence check via daterange, G2 overload check via capacity formula); service layer with business rules (G1 unavailable, G2 overload, absence overlap, FK integrity, unique constraints); audit log for all mutations (employee_created/updated, competency_updated, project_created, assignment_created, absence_created/cancelled); migration 00008 extends audit actions; integration tests with testcontainers-go (pgvector/pgvector:pg16); 18 domain validation tests, 17 service unit tests (mock repos), 6 integration tests; all green; fixed docker dependency (testcontainers-go v0.45.0); coverage 56.9% overall (handler+service+domain)
2026-11-06 - 2.4 - weekly_capacity + availability service - domain types: WeeklyCapacity (available/assigned/absent/norm minutes, utilization_ratio), CapacityMatrix, week utilities (ParseWeek, WeekRange, WeekMonday, WorkingDaysInWeek, WeekContainsDate); repository: PgCapacityRepo with batch queries (GetWeeklyCapacity, GetCapacityMatrix, FindCandidatesByCompetency), effective_limit = min(norm×(1-reserve), norm×max_util), absent minutes = actual working days (Mon-Fri) in overlap × hours_per_day × 60; service: CapacityService with CheckAvailability (G1 absence + G2 overload enforcement), GetAvailableHours, FindAvailableEmployees (competency + availability filter); refactored AssignmentService.CreateBatch to delegate G1/G2 to CapacityService with overload override support; handlers: GET /employees/{id}/capacity and GET /capacity/matrix endpoints; property tests (4): G1NoAbsenceConflict (200 iterations), G2NoOverload (500 iterations), AvailableNonNegative (500 iterations), CapacityMonotonic (200 iterations); integration tests (7): BasicCapacityCalculation, G1_AbsenceRejection, G2_OverloadRejection, AbsenceReducesCapacity (partial week), FindAvailableEmployees, CapacityMatrix (batch), EdgeCase_ZeroAvailableAfterFullAssignment; all green; go vet clean; fixed TestMigrationOrder to include 00008_extend_audit.sql
2026-11-07 - 2.5 - frontend skeleton + reference-data screens on mocks - 19 source files under frontend/src/ (app/, pages/, components/, api/, mocks/, types/, hooks/, utils/); React Router v6 layout with 6 top-level routes + 5 reference sub-routes; 10 screens: Dashboard, Employees (list + filters + detail drawer w/ competencies/capacity table), Projects (list + create modal), Assignments (table), Absences (table + delete), Roles, Competencies (grouped by category in Collapse), Grades, Weight Presets (4 tabs matching METRICS.md), Planning Params (4 cards); DTOs (src/types/index.ts) match contracts/openapi.yaml 1:1 (snake_case); mock data: 30 employees (emp_0001..0030, all 12 roles from contracts/taxonomy.yaml, 4 grades), 5 projects (planned/active/done), 40 assignments (2026-W07..W09), 15 absences (vacation/sick/training/trip/other), 12 competencies, 5 grade bands (J/M/S/L/P, coefficients 1.0/1.4/1.9/2.6/3.4), 4 weight presets from METRICS.md §1, planning params (RU-production-calendar, 40h, 20% reserve, MC 2000/seed=42); API layer (src/api/client.ts) returns mocks with same signatures as future fetch client - swap requires editing only this file; 3 test files / 35 tests: week utils (9 cases), mock-data invariants (24 cases incl. no-overlap absences, FK integrity, regex check for emails/phones/full names), Layout rendering (2 cases); ESLint 9 flat config + @typescript-eslint + react-hooks + react-refresh; vitest 2 + jsdom + @testing-library/react; ISO-week helpers (utils/week.ts) ADR-002 compliant (Monday-based); Russian UI (antd ruRU locale), code/comments/docs in English; 0 personal-data patterns in fixtures; README.md (147 lines) with quick start, folder structure, mock→API migration guide; Dockerfile.frontend compatible (uses package-lock.json, npm install + npm run build inside node:20-alpine); .gitignore excludes node_modules/ and dist/; agent-reported: tsc --noEmit 0 errors; eslint 0 warnings; vitest 35/35 passed; vite build 4.01s -> dist/index.html 0.38 kB + dist/assets/*.js 1151 kB (358 kB gzip); orchestrator file-level audit confirmed: dist/, node_modules/, package-lock.json, eslint.config.js, vitest.config.ts, all 19 source files and README.md present and consistent with contract; note: shell denied in orchestrator session so command outputs are agent-reported but consistent with build artifacts on disk
