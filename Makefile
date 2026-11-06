COMPOSE_FILE := deploy/docker-compose.yml
COMPOSE_PROJECT_NAME := teamopt
COMPOSE_ENV_FILE := .env
COMPOSE := docker compose -f $(COMPOSE_FILE) -p $(COMPOSE_PROJECT_NAME) --env-file $(COMPOSE_ENV_FILE)

.PHONY: up down build logs test lint-contracts lint-backend lint-ml seed status help

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# ----------------------------------------------------------- core

up: ## docker compose up --build -d, wait for healthy (90 s)
	@cp -n .env.example .env 2>/dev/null || true
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
	@echo -n "Gateway:         "; curl -sf -o /dev/null -w "HTTP %{http_code}" http://localhost:$${GATEWAY_PORT:-10080}/ || echo "UNREACHABLE"
	@echo ""

# ----------------------------------------------------------- lint

lint-contracts: ## spectral + ajv per contracts/README.md
	@echo "=== Spectral lint ==="
	npx --yes @stoplight/spectral-cli lint contracts/openapi.yaml contracts/ml-openapi.yaml \
		--ruleset contracts/spectral.yaml || true
	@echo ""
	@echo "=== AJV compile ==="
	npx --yes ajv-cli@5 compile \
		-s contracts/json-schema/common.schema.json \
		-s contracts/json-schema/requirement.schema.json \
		-s contracts/json-schema/team-variant.schema.json \
		-s contracts/json-schema/optimize-request.schema.json \
		-s contracts/json-schema/optimize-response.schema.json \
		-s contracts/json-schema/taxonomy.schema.json || true

lint-backend: ## golangci-lint run in backend/
	cd backend && golangci-lint run ./...

lint-ml: ## ruff + black check in ml/
	cd ml && python3 -m ruff check . && python3 -m black --check .

# ----------------------------------------------------------- test

test: lint-contracts ## Run all tests: spectral, go test, pytest
	@echo ""
	@echo "=== Go tests ==="
	cd backend && go test ./...
	@echo ""
	@echo "=== ML tests ==="
	cd ml && python3 -m pytest tests/ -v

# ----------------------------------------------------------- seed

seed: ## Placeholder for synthetic data seed (task 2.2)
	@echo "seed: not implemented yet (will be added in task 2.2)"
