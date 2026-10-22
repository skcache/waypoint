# Waypoint developer commands. Run `make` or `make help` for the list.

.DEFAULT_GOAL := help

API_DIR := apps/api
WEB_DIR := apps/web

# Load local overrides from .env when present (never committed; see .env.example).
ifneq (,$(wildcard .env))
include .env
export
endif

.PHONY: help dev-api dev-web dev-merchant merchant-reset db-up db-down db-migrate db-reset test test-api test-integration test-web lint lint-api lint-web build-api build-web demo-m5 clean

help: ## List available targets
	@grep -h -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-12s %s\n", $$1, $$2}'

dev-api: ## Run the Go API on http://localhost:8080
	cd $(API_DIR) && go run ./cmd/server

dev-web: ## Run the Next.js web app on http://localhost:3000
	cd $(WEB_DIR) && npm run dev

dev-merchant: ## Run the V2 merchant checkout fixture on http://localhost:8090
	cd $(API_DIR) && DATABASE_URL="$${DATABASE_URL:-postgres://waypoint:waypoint@localhost:5433/waypoint?sslmode=disable}" \
		MERCHANT_INCIDENT_DIR="$(CURDIR)/docs/incidents" go run ./cmd/merchant

merchant-reset: ## Clear merchant fixture orders/operations/events (keeps the catalog)
	cd $(API_DIR) && DATABASE_URL="$${DATABASE_URL:-postgres://waypoint:waypoint@localhost:5433/waypoint?sslmode=disable}" go run ./cmd/merchant --reset

db-up: ## Start local PostgreSQL (docker compose)
	docker compose up -d db

db-down: ## Stop and remove local containers
	docker compose down

db-migrate: ## Apply SQL migrations (DATABASE_URL or the local default)
	cd $(API_DIR) && DATABASE_URL="$${DATABASE_URL:-postgres://waypoint:waypoint@localhost:5433/waypoint?sslmode=disable}" go run ./cmd/migrate

db-reset: ## DESTRUCTIVE local reset: drop the db volume and start empty
	docker compose down -v
	docker compose up -d db
	@echo "volume recreated — run 'make db-migrate' to rebuild the schema"

test: test-api test-web ## Run the automated test suites that exist today

test-api: ## Go tests for the API
	cd $(API_DIR) && go test ./...

test-integration: ## Go tests with database integration REQUIRED (fails instead of skipping without PostgreSQL)
	cd $(API_DIR) && REQUIRE_DATABASE_TESTS=1 go test ./...

test-web: ## Workbench unit tests (review authorization, sanitized export guards)
	cd $(WEB_DIR) && npm test

lint: lint-api lint-web ## Run available linters

lint-api: ## Go vet plus gofmt check (including build-tagged test suites)
	cd $(API_DIR) && go vet ./... && go vet -tags 'f1_repair fault_reproduction' ./... && test -z "$$(gofmt -l .)"

lint-web: ## ESLint for the web app
	cd $(WEB_DIR) && npm run lint

build-api: ## Build the API binary to apps/api/bin/server
	cd $(API_DIR) && go build -o bin/server ./cmd/server

build-web: ## Production build of the web app
	cd $(WEB_DIR) && npm run build

demo-m5: ## Deterministic M5 demo checks and workbench entry point (no model calls, no payments)
	cd $(API_DIR) && go test ./internal/bench -run 'TestCommitted' -v
	cd $(WEB_DIR) && npm test
	@echo ""
	@echo "Workbench: make dev-web  ->  http://localhost:3000/incidents"
	@echo "Narrative: evidence/m5/DEMO.md"

clean: ## Remove local build outputs
	rm -rf $(API_DIR)/bin $(WEB_DIR)/.next
