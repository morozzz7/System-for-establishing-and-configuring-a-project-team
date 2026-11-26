COMPOSE_FILE := deploy/docker-compose.yml
COMPOSE_PROJECT_NAME := teamopt
COMPOSE_ENV_FILE := .env
COMPOSE ?= $(shell if command -v docker-compose >/dev/null 2>&1; then echo docker-compose; else echo "docker compose"; fi) -f $(COMPOSE_FILE) -p $(COMPOSE_PROJECT_NAME) --env-file $(COMPOSE_ENV_FILE)
E2E_COMPOSE ?= $(shell if command -v docker-compose >/dev/null 2>&1; then echo docker-compose; else echo "docker compose"; fi) -f $(COMPOSE_FILE) -f deploy/docker-compose.e2e.yml -p $(COMPOSE_PROJECT_NAME)-e2e --env-file $(COMPOSE_ENV_FILE)

.PHONY: up down build logs fixtures test test-e2e test-e2e-down lint-contracts lint-backend lint-ml seed status help

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# ----------------------------------------------------------- core

up: ## docker compose up --build -d, wait for healthy (90 s)
	@cp -n .env.example .env 2>/dev/null || true
	$(COMPOSE) up --build -d postgres
	@echo "Waiting for postgres..."
	@sleep 5
	@echo "Building backend with the current contracts before seeding..."
	$(COMPOSE) build backend
	@echo "Running seed (migrations + reference data)..."
	$(COMPOSE) run --rm --entrypoint /usr/local/bin/seed backend
	$(COMPOSE) up --build -d --wait --wait-timeout 90
	@echo ""
	@echo "=== All services healthy ==="
	@$(COMPOSE) ps

down: ## docker compose down -v (remove volumes)
	$(COMPOSE) down -v

build: ## docker compose build
	$(COMPOSE) build

logs: ## docker compose logs -f
	$(COMPOSE) logs -f

status: ## docker compose ps + health probes
	@$(COMPOSE) ps
	@echo ""
	@echo "--- Health probes (using .env ports) ---"
	@echo -n "Backend healthz: "; curl -sf http://localhost:$${BACKEND_PORT:-18080}/api/v1/healthz || echo "UNREACHABLE"
	@echo ""
	@echo -n "Backend readyz:  "; curl -sf http://localhost:$${BACKEND_PORT:-18080}/api/v1/readyz || echo "UNREACHABLE"
	@echo ""
	@echo -n "ML healthz:      "; curl -sf http://localhost:$${ML_PORT:-18000}/healthz || echo "UNREACHABLE"
	@echo ""
	@echo -n "Gateway:         "; curl -sf -o /dev/null -w "HTTP %{http_code}" http://localhost:$${GATEWAY_PORT:-9080}/ || echo "UNREACHABLE"
	@echo ""

# ----------------------------------------------------------- lint

lint-contracts: ## spectral + JSON Schema validation per contracts/README.md
	@echo "=== Spectral lint ==="
	npx --yes @stoplight/spectral-cli lint contracts/openapi.yaml contracts/ml-openapi.yaml \
		--ruleset contracts/spectral.yaml
	@echo ""
	@echo "=== JSON Schema validation ==="
	python3 contracts/validate_schemas.py

lint-backend: ## golangci-lint run in backend/
	cd backend && golangci-lint run ./...

lint-ml: ## ruff + black check in ml/
	cd ml && python3 -m ruff check . && python3 -m black --check .

# ----------------------------------------------------------- test

fixtures: ## regenerate gold briefs and synthetic scenarios used by ML tests
	@echo "=== Generating gold briefs ==="
	python3 eval/scripts/generate_gold.py
	@echo "=== Generating synthetic scenarios ==="
	python3 eval/synth/generator.py --seed 42

test: lint-contracts fixtures ## Run all tests: spectral, schemas, go test, pytest
	@echo ""
	@echo "=== Go tests ==="
	cd backend && go test ./...
	@echo ""
	@echo "=== ML tests ==="
	cd ml && python3 -m pytest tests/ -v

# ----------------------------------------------------------- e2e

test-e2e: ## full-stack Playwright journey (Docker stack + deterministic LLM stub)
	@test -x frontend/node_modules/.bin/playwright || { \
		echo "Playwright is not installed. Run: cd frontend && npm ci && npm run test:e2e:install"; \
		exit 1; \
	}
	@test -f $(COMPOSE_ENV_FILE) || cp .env.example $(COMPOSE_ENV_FILE)
	@$(E2E_COMPOSE) up --build -d postgres
	@sleep 5
	@echo "Building backend with the current contracts before seeding..."
	@$(E2E_COMPOSE) build backend
	@echo "Running seed (migrations + reference data)..."
	@$(E2E_COMPOSE) run --rm --entrypoint /usr/local/bin/seed backend
	@$(E2E_COMPOSE) up --build -d --wait --wait-timeout 120
	@echo ""
	@echo "=== Playwright full journey ==="
	@env_port="$$(sed -n 's/^GATEWAY_PORT=//p' $(COMPOSE_ENV_FILE) 2>/dev/null | tail -n 1)"; \
	  GATEWAY_PORT="$${GATEWAY_PORT:-$${env_port:-80}}"; \
	  export GATEWAY_PORT; \
	  gateway_url="http://127.0.0.1:$$GATEWAY_PORT"; \
	  echo "Gateway: $$gateway_url"; \
	  curl -sf -o /dev/null --max-time 5 "$$gateway_url/" || { \
	    echo "Gateway is unreachable at $$gateway_url" >&2; \
	    exit 1; \
	  }; \
	  cd frontend && npm run test:e2e

test-e2e-down: ## stop the e2e stack and remove its volumes
	@$(E2E_COMPOSE) down -v

# ----------------------------------------------------------- seed

seed: ## Apply migrations + load taxonomy/reference seed into the running database
	$(COMPOSE) run --rm --entrypoint /usr/local/bin/seed backend
