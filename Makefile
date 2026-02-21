# ── Config ────────────────────────────────────────────────────────────
BINARY_SERVER  = server
BINARY_INGEST  = ingest
CMD_SERVER     = ./cmd/server
CMD_INGEST     = ./cmd/ingest
COMPOSE_DEV    = docker-compose.yaml
SEED_PAGES     = 5

# ── Go Development ────────────────────────────────────────────────────

.PHONY: build
build: ## Build all binaries
	go build -o $(BINARY_SERVER) $(CMD_SERVER)
	go build -o $(BINARY_INGEST) $(CMD_INGEST)

.PHONY: run
run: ## Run the server locally
	go run $(CMD_SERVER)

.PHONY: fmt
fmt: ## Format all Go source files
	gofmt -w .

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: fix
fix: ## Apply go fix rewrites
	go fix ./...

.PHONY: tidy
tidy: ## Tidy and verify module dependencies
	go mod tidy

.PHONY: test
test: ## Run unit tests
	go test ./...

.PHONY: test-integration
test-integration: ## Run integration tests (requires running Neo4j)
	@echo "TODO: integration tests not yet implemented"
	go test -tags=integration ./...

.PHONY: coverage
coverage: ## Run tests with coverage report
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

.PHONY: validate
validate: fmt vet ## Format, vet, and test
	$(MAKE) test

.PHONY: clean
clean: ## Remove built binaries and coverage artifacts
	rm -f $(BINARY_SERVER) $(BINARY_INGEST) coverage.out coverage.html

# ── Docker / Dev Environment ──────────────────────────────────────────

.PHONY: up
up: ## Start dev environment in background
	docker compose -f $(COMPOSE_DEV) up --build -d

.PHONY: watch
watch: ## Start dev environment with live reload
	docker compose -f $(COMPOSE_DEV) watch

.PHONY: down
down: ## Stop dev environment
	docker compose -f $(COMPOSE_DEV) down

.PHONY: reset
reset: ## Stop dev environment, remove volumes, restart clean
	docker compose -f $(COMPOSE_DEV) down -v
	docker compose -f $(COMPOSE_DEV) up -d

# ── Data ──────────────────────────────────────────────────────────────

.PHONY: ingest
ingest: ## Run full ingestion against local Neo4j
	@echo "TODO: ingestion not yet implemented"
	go run $(CMD_INGEST)

.PHONY: seed
seed: ## Ingest a small dataset for quick local dev
	@echo "TODO: seed not yet implemented"
	go run $(CMD_INGEST) --pages=$(SEED_PAGES)

# ── Help ──────────────────────────────────────────────────────────────

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help