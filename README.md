# Optimal Project Team Formation System

Decision-support system for resource managers. It turns a technical brief into
structured requirements, finds feasible project-team variants against employee
competencies and availability, and explains the trade-offs. The system proposes;
a human makes the final decision.

The repository is privacy-safe by design: fixtures and examples use synthetic
data and pseudonymous IDs (`emp_*`, `prj_*`, `brf_*`). Cost is represented only
by grade coefficients, never by personal salary data.

## Concept

The end-to-end flow is:

```text
brief text/file
    -> ML extraction with evidence and effort ranges
    -> competency normalization and human review
    -> availability snapshot (capacity, assignments, absences)
    -> CP-SAT optimizer proposes 2-3 variants
    -> Go validator re-checks G1-G5 independently
    -> schedule engine calculates P50/P80 and portfolio impact
    -> metrics, deficits, explanations and human decision
```

The ML service structures language and proposes variants; it does not become
the authority for assignments. Go owns hard-constraint verification and
deterministic calculations. The schedule engine in
[`backend/internal/schedule`](backend/internal/schedule) uses fixed-seed
Monte Carlo over min/likely/max effort and calculates portfolio slip using the
M8 formula from [`docs/METRICS.md`](docs/METRICS.md).

## Stack

| Area | Technologies |
| --- | --- |
| Backend/core | Go, chi, pgx, Goose, slog |
| Database | PostgreSQL 16 + pgvector |
| ML service | Python 3.12, FastAPI, Pydantic, deterministic hashing embeddings, OR-Tools CP-SAT |
| LLM integration | OpenAI-compatible endpoint; OpenRouter is the MVP provider, optional |
| Frontend | React 18, TypeScript, Vite, Ant Design, React Router |
| Tests | Go test/testify/testcontainers, pytest, Vitest |
| Runtime | Docker Compose, nginx |
| Contracts | OpenAPI, ML OpenAPI, JSON Schema, YAML taxonomy/reference data |

## Requirements

For the full Compose setup:

- Docker Engine with Compose v2;
- GNU Make;
- curl (used by `make status`).

For local development without Docker:

- Go matching [`backend/go.mod`](backend/go.mod);
- Python 3.12 and pip;
- Node.js 20+ and npm.

An LLM key is not required for the deterministic tests, optimizer tests, or
embedding tests. It is needed only when the extraction pipeline is configured
to call an external OpenAI-compatible model.

## Quick start with Docker

```bash
cp .env.example .env          # keep real secrets out of git
make up                       # build, migrate, seed and wait for health
make status
```

The default host endpoints (standard ports, clean host):

| Service     | URL                                                    |
| ----------- | ------------------------------------------------------ |
| Gateway/UI  | [http://localhost/](http://localhost/)                 |
| Backend API | [http://localhost:8080/api/v1/](http://localhost:8080/api/v1/) |
| ML service  | [http://localhost:8000/](http://localhost:8000/)       |
| PostgreSQL  | `localhost:5432` (user: teamopt, db: teamopt)          |

**Main entry point is the Gateway at `http://localhost/`** — it routes:
- `/` → Frontend (React SPA)
- `/api/` → Backend (Go API, served at `/api/v1/` internally)
- `/ml/` → ML service (FastAPI)

**If standard ports are occupied**, use conflict-free ports in `.env`:
```bash
BACKEND_PORT=18080
ML_PORT=18000
FRONTEND_PORT=13000
POSTGRES_PORT=15432
GATEWAY_PORT=9080
```
Then access via Gateway: **http://localhost:9080/**

Container-internal ports never change: backend=8080, ml=8000, frontend=3000, postgres=5432.

Useful Compose commands:

```bash
make logs       # follow all service logs
make seed       # rerun migrations/reference seed against the running DB
make down       # stop services and remove the local Compose database volume
```

`make down` is intentionally destructive for local database state because it
uses `docker compose down -v`; use it only when the local synthetic database
can be recreated.

## Local development

Backend:

```bash
cd backend
go test ./...
go build ./...
```

ML service:

```bash
cd ml
python3 -m venv .venv
. .venv/bin/activate
python -m pip install -r requirements.txt
python -m pytest tests/ -q
uvicorn app.main:app --reload --port 8000
```

Frontend:

```bash
cd frontend
npm ci
npm run dev
npm test -- --run
npm run build
```

The frontend uses deterministic mocks by default. Set
`VITE_API_BASE_URL=http://localhost:8080/api/v1` before starting Vite to use
the backend brief/review/run flow instead:

```bash
VITE_API_BASE_URL=http://localhost:8080/api/v1 npm run dev
```

## Verification

From the repository root:

```bash
make test
```

This runs contract checks, Go tests and ML tests. Frontend checks are run
separately with `npm test -- --run`, `npm run lint` and `npm run build` from
`frontend/`. Docker-backed Go integration tests require a running Docker
daemon.

Contract documents are the single source of truth. Update
[`contracts/`](contracts/) before changing a cross-service API; generated code
must not be hand-edited. The contract-specific commands are documented in
[`contracts/README.md`](contracts/README.md).

## Repository map

```text
backend/       Go API, domain, repositories, validator, schedule engine
ml/            FastAPI extraction, embeddings and optimizer
frontend/      React/TypeScript UI
contracts/     OpenAPI, JSON Schema, taxonomy and reference data
db/            PostgreSQL migrations and seed support
eval/          gold briefs, synthetic scenarios and benchmarks
docs/          plan, metrics and architecture decisions
deploy/        Dockerfiles, Compose and nginx gateway
```

## Current implementation status

Completed foundations include the resource model and CRUD/capacity services,
LLM-first extraction with deterministic validation and evidence, deterministic
embeddings and taxonomy normalization baseline, CP-SAT optimizer v2, Go
validator G1-G5, the brief/review/run vertical slice, Russian UI screens, and
the Phase 4.1 schedule engine.

The current brief/run vertical slice keeps its workflow state in memory and
uses synthetic demo candidates; persistence and full run-result orchestration
remain subsequent integration work. The normalization implementation is an
exact-synonym baseline; pgvector ranking is the planned deployment
optimization. See [`docs/PLAN.md`](docs/PLAN.md) for the authoritative status
and [`docs/METRICS.md`](docs/METRICS.md) for metric definitions.
