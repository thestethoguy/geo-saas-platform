# =============================================================================
#  Geo SaaS Platform — Makefile
#  Usage: make <target>
# =============================================================================

# ── Config ───────────────────────────────────────────────────────────────────
PROJECT_NAME  := geo-saas-platform
API_DIR       := ./api
DB_DIR        := ./database
BINARY        := ./api/bin/server
GO_CMD        := go

# Colours for readable output
GREEN  := \033[0;32m
YELLOW := \033[0;33m
RESET  := \033[0m

.PHONY: help up down restart logs \
        db-shell redis-shell \
        run build tidy vet \
        ingest ingest-tidy \
        migrate-status clean

# ─────────────────────────────────────────────
# Default: list all targets
# ─────────────────────────────────────────────
help:
	@echo ""
	@echo "$(GREEN)Geo SaaS Platform — available commands$(RESET)"
	@echo "──────────────────────────────────────────"
	@echo "  $(YELLOW)make up$(RESET)           Start all Docker containers (Postgres, Redis, Typesense)"
	@echo "  $(YELLOW)make down$(RESET)         Stop and remove all containers"
	@echo "  $(YELLOW)make restart$(RESET)      down + up"
	@echo "  $(YELLOW)make logs$(RESET)         Tail logs from all containers"
	@echo "  $(YELLOW)make db-shell$(RESET)     Open psql shell inside Postgres container"
	@echo "  $(YELLOW)make redis-shell$(RESET)  Open redis-cli inside Redis container"
	@echo "  $(YELLOW)make run$(RESET)          Start the Go API server (requires containers up)"
	@echo "  $(YELLOW)make build$(RESET)        Compile the Go binary to ./api/bin/server"
	@echo "  $(YELLOW)make tidy$(RESET)         Run go mod tidy"
	@echo "  $(YELLOW)make vet$(RESET)          Run go vet"
	@echo "  $(YELLOW)make ingest$(RESET)       Run ETL ingestion script (CSV → Postgres + Typesense)"
	@echo "  $(YELLOW)make ingest-tidy$(RESET)  go mod tidy for the ingestion script"
	@echo "  $(YELLOW)make clean$(RESET)        Remove compiled binary"
	@echo ""

# ─────────────────────────────────────────────
# Docker Compose
# ─────────────────────────────────────────────
up:
	@echo "$(GREEN)▶ Starting infrastructure containers...$(RESET)"
	docker compose up -d --build
	@echo "$(GREEN)✓ Containers started. Health endpoints:$(RESET)"
	@echo "  Postgres  → localhost:5432"
	@echo "  Redis     → localhost:6379"
	@echo "  Typesense → http://localhost:8108/health"

down:
	@echo "$(YELLOW)▶ Stopping containers...$(RESET)"
	docker compose down

restart: down up

logs:
	docker compose logs -f --tail=100

# ─────────────────────────────────────────────
# Database helpers
# ─────────────────────────────────────────────
db-shell:
	@echo "$(GREEN)▶ Connecting to PostgreSQL shell...$(RESET)"
	docker exec -it geo_postgres psql -U geouser -d geosaas

redis-shell:
	@echo "$(GREEN)▶ Connecting to Redis CLI...$(RESET)"
	docker exec -it geo_redis redis-cli -a redispassword

# ─────────────────────────────────────────────
# Go Application
# ─────────────────────────────────────────────
run:
	@echo "$(GREEN)▶ Starting Geo SaaS API server...$(RESET)"
	cd $(API_DIR) && $(GO_CMD) run ./cmd/main.go

build:
	@echo "$(GREEN)▶ Building binary → $(BINARY)$(RESET)"
	mkdir -p $(API_DIR)/bin
	cd $(API_DIR) && $(GO_CMD) build -ldflags="-s -w" -o bin/server ./cmd/main.go
	@echo "$(GREEN)✓ Build complete: $(BINARY)$(RESET)"

tidy:
	@echo "$(GREEN)▶ Running go mod tidy...$(RESET)"
	cd $(API_DIR) && $(GO_CMD) mod tidy

vet:
	@echo "$(GREEN)▶ Running go vet...$(RESET)"
	cd $(API_DIR) && $(GO_CMD) vet ./...

clean:
	@echo "$(YELLOW)▶ Removing binary...$(RESET)"
	rm -f $(BINARY)

# ─────────────────────────────────────────────
# ETL / Data Ingestion
# ─────────────────────────────────────────────
INGEST_DIR := ./scripts/ingest

ingest:
	@echo "$(GREEN)▶ Running data ingestion (CSV → Postgres + Typesense)...$(RESET)"
	cd $(INGEST_DIR) && $(GO_CMD) run main.go

ingest-tidy:
	@echo "$(GREEN)▶ Running go mod tidy for ingestion script...$(RESET)"
	cd $(INGEST_DIR) && $(GO_CMD) mod tidy
