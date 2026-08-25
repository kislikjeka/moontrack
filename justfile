# MoonTrack - Crypto Portfolio Tracker
# Just command runner - https://github.com/casey/just

set dotenv-load
set shell := ["bash", "-c"]

default:
    @just --list

# =============================================================================
# Development
# =============================================================================

# Start full dev environment (postgres, redis, backend with hot-reload, frontend)
dev:
    @echo "Starting development environment..."
    @echo "Backend: http://localhost:8080"
    @echo "Frontend: http://localhost:5173"
    @echo ""
    docker-compose up -d postgres redis backend
    @echo ""
    @echo "Waiting for backend to be ready..."
    @sleep 3
    @just migrate-up || true
    @echo ""
    @echo "Starting frontend..."
    cd apps/frontend && bun run dev

# Start full dev environment with Grafana+Loki log stack
dev-logs:
    @echo "Starting development environment with log stack..."
    @echo "Backend: http://localhost:8080"
    @echo "Frontend: http://localhost:5173"
    @echo "Grafana: http://localhost:3001"
    @echo ""
    docker-compose --profile logs up -d postgres redis backend loki promtail grafana
    @echo ""
    @echo "Waiting for backend to be ready..."
    @sleep 3
    @just migrate-up || true
    @echo ""
    @echo "Starting frontend..."
    cd apps/frontend && bun run dev

# Open Grafana Explore in browser
grafana:
    open http://localhost:3001/explore

# Stop all containers
down:
    docker-compose down
    @echo "All services stopped"

# View logs (usage: just logs, just logs backend, just logs postgres)
logs service="":
    @if [ -z "{{service}}" ]; then \
        docker-compose logs -f; \
    else \
        docker-compose logs -f {{service}}; \
    fi

# Show container status
status:
    docker-compose ps

# Rebuild and restart backend container
backend-restart:
    docker-compose up -d --build backend
    @echo "Backend restarted"

# =============================================================================
# Database
# =============================================================================

# Apply database migrations
migrate-up:
    docker exec moontrack-backend migrate -database "postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable" -path migrations up
    @echo "Migrations applied"

# Rollback last migration
migrate-down:
    docker exec moontrack-backend migrate -database "postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable" -path migrations down 1
    @echo "Last migration rolled back"

# Create new migration file
migrate-create name:
    cd apps/backend && migrate create -ext sql -dir migrations -seq {{name}}
    @echo "Migration created: {{name}}"

# Clear all portfolio data (wallets, transactions, entries) but keep user accounts
db-clear-data:
    @echo "Clearing all portfolio data (keeping user accounts)..."
    docker exec moontrack-postgres psql -U ${POSTGRES_USER} -d ${POSTGRES_DB} -c "\
        DELETE FROM lot_override_history; \
        DELETE FROM lot_disposals; \
        DELETE FROM tax_lots; \
        DELETE FROM entries; \
        DELETE FROM account_balances; \
        DELETE FROM accounts; \
        DELETE FROM raw_transactions; \
        DELETE FROM transactions; \
        DELETE FROM lending_positions; \
        DELETE FROM lp_positions; \
        DELETE FROM wallets; \
    "
    @echo "All portfolio data cleared. User accounts and price history preserved."

# Reset database (drop all and re-migrate)
db-reset:
    docker exec moontrack-backend sh -c "migrate -database 'postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable' -path migrations down -all || true"
    docker exec moontrack-backend migrate -database "postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable" -path migrations up
    @echo "Database reset complete"

# Connect to PostgreSQL
db-connect:
    docker exec -it moontrack-postgres psql -U ${POSTGRES_USER} -d ${POSTGRES_DB}

# =============================================================================
# Reconciliation report (issue #61)
#
# "The balance adds up" as a verdict rather than a feeling: the red category is
# empty. Exit 0 = adds up, 1 = red rows, 2 = a check could not be run.
#
# The report reads the database and the provider's RAW JSON. It never syncs and
# never writes: a sync before the check would top the ledger up TO the positions,
# after which P↔L agrees because it was made to.
# =============================================================================

# Full report against the live provider (one balances call per chain)
reconcile-report wallet:
    cd apps/backend && go run ./cmd/reconcile-report -wallet {{wallet}}

# Capture the provider's raw response, then report on it in the same run.
# Every later run replays the snapshot for free — the provider budget is small
# and acceptance has to be repeatable on THE SAME data.
#
# This is the recipe that also asks whether the provider holds transactions
# NEWER than the collection cursor. It costs one extra call per chain, so it
# belongs here rather than on every run: the answer is frozen into the snapshot,
# and every replay then reproduces that verdict for free.
reconcile-snapshot wallet path="docs/reconcile/snapshot.json":
    cd apps/backend && go run ./cmd/reconcile-report -wallet {{wallet}} -probe-cursor -save-snapshot ../../{{path}}

# Replay a snapshot: no provider calls, byte-identical between runs.
# Diffing two runs is the main way this check is used in acceptance.
reconcile-replay wallet path="docs/reconcile/snapshot.json":
    cd apps/backend && go run ./cmd/reconcile-report -wallet {{wallet}} -snapshot ../../{{path}}

# The three checks that need no network. Always exits 2: without the provider
# there is no verdict, only the checks that did run.
reconcile-offline wallet:
    cd apps/backend && go run ./cmd/reconcile-report -wallet {{wallet}} -no-provider

# =============================================================================
# Offline replay loop (issue #85)
#
# "Reset the wallet's ledger, re-derive it from the raws already collected, then
# check the result" — the loop that turns a fix in the booking logic into a fix
# in the data. Transactions booked before the fix stay wrong until they are
# re-derived, and re-deriving is sound because the decision is a pure function
# of the collected raws (ADR-0002).
#
# No provider is contacted by any of the three steps, so the loop costs nothing
# and is repeatable on the same data.
#
# The wallet is a parameter for a reason: every step here is destructive to one
# wallet's ledger, and doing this by hand-typed SQL each time — which is how it
# was done before — is how the wrong wallet gets wiped.
# =============================================================================

# Reset one wallet's ledger, re-derive it from its raws, then run the offline checks.
replay-wallet wallet:
    @echo "=== 1/3 reset + re-derive: {{wallet}} ==="
    cd apps/backend && go run ./cmd/replay-pending -wallet {{wallet}} -wipe
    @echo ""
    @echo "=== 2/3 offline reconciliation ==="
    -cd apps/backend && go run ./cmd/reconcile-report -wallet {{wallet}} -no-provider
    @echo ""
    @echo "=== 3/3 chain segment vs registry chain (must be zero rows) ==="
    @just _chain-mismatch {{wallet}}

# The #70 control query: wallet accounts whose code chain segment disagrees with
# the chain the registry gives their asset. One asset addressed by two accounts
# sums to a plausible number, so no amount check sees it — only this one.
_chain-mismatch wallet:
    docker exec moontrack-postgres psql -U ${POSTGRES_USER} -d ${POSTGRES_DB} -c \
      "SELECT a.code, a.chain_id AS code_chain, ar.chain AS asset_chain, ar.symbol \
       FROM accounts a JOIN asset_registry ar ON ar.id = a.asset_id \
       WHERE a.type='CRYPTO_WALLET' AND a.wallet_id = '{{wallet}}' \
         AND a.chain_id IS DISTINCT FROM ar.chain;"

# =============================================================================
# Testing
# =============================================================================

# Run all tests (backend + frontend)
test:
    @echo "Running all tests..."
    @just backend-test
    @just frontend-test
    @echo "All tests passed"

# Run backend tests (with -race by default to catch data races in CI)
backend-test:
    cd apps/backend && go test ./... -v -short -race

# Run backend tests without the race detector (fast path, if you need it)
backend-test-fast:
    cd apps/backend && go test ./... -v -short

# Run frontend tests
frontend-test:
    cd apps/frontend && bun test

# Generate coverage reports
coverage:
    @echo "Generating coverage reports..."
    cd apps/backend && go test ./... -cover -coverprofile=coverage.out
    cd apps/backend && go tool cover -html=coverage.out -o coverage.html
    cd apps/frontend && bun test -- --coverage || true
    @echo "Coverage reports generated"

# =============================================================================
# Code Quality
# =============================================================================

# Format all code
fmt:
    cd apps/backend && go fmt ./...
    @echo "Code formatted"

# Install golangci-lint, built with the project's Go toolchain.
# Building it locally (rather than a prebuilt binary) keeps the linter's
# type checker on the same Go version as the compiler — a skew makes
# typecheck fail on valid code. See issue #65.
lint-install:
    cd apps/backend && go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
    @echo "golangci-lint installed"

# Lint all code
lint:
    #!/usr/bin/env bash
    set -euo pipefail
    if ! command -v golangci-lint >/dev/null 2>&1; then
        echo "golangci-lint not installed. Install it with:" >&2
        echo "  cd apps/backend && go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2" >&2
        exit 1
    fi
    cd apps/backend && golangci-lint run
    cd ../frontend && bun run lint
    echo "Linting complete"

# Run all checks (format, lint, test)
check:
    @echo "Running all checks..."
    @just fmt
    @just lint
    @just test
    @echo "All checks passed"

# =============================================================================
# Observability
# =============================================================================

# Build the Loki MCP server Docker image
loki-mcp-build:
    docker build -t moontrack-loki-mcp:latest ./infra/loki-mcp
    @echo "Loki MCP image built: moontrack-loki-mcp:latest"

# Check that Loki MCP image exists and Loki is reachable
loki-mcp-check:
    @echo "Checking Loki MCP setup..."
    @docker image inspect moontrack-loki-mcp:latest > /dev/null 2>&1 \
        && echo "✓ Docker image moontrack-loki-mcp:latest exists" \
        || (echo "✗ Docker image not found. Run: just loki-mcp-build" && exit 1)
    @curl -sf http://localhost:3100/ready > /dev/null 2>&1 \
        && echo "✓ Loki is reachable at localhost:3100" \
        || (echo "✗ Loki is not reachable. Run: just dev-logs" && exit 1)
    @echo "All checks passed!"

# =============================================================================
# Workflow
# =============================================================================

# Reset data, create test wallet, and trigger sync in one command
resync:
    @echo "=== Resetting & Syncing Test Wallet ==="
    @echo ""
    @echo "1. Clearing portfolio data..."
    @just db-clear-data
    @echo "   Removing stale test user (${RESYNC_EMAIL}) so register always succeeds..."
    @docker exec moontrack-postgres psql -U ${POSTGRES_USER} -d ${POSTGRES_DB} \
        -c "DELETE FROM users WHERE email='${RESYNC_EMAIL}';" > /dev/null
    @echo ""
    @echo "2. Restarting backend..."
    docker-compose up -d --build backend
    @echo "   Waiting for backend to be ready..."
    @i=0; while [ $i -lt 30 ]; do \
        if curl -sf http://localhost:8080/health > /dev/null 2>&1; then \
            echo "   Backend is ready!"; \
            break; \
        fi; \
        i=$((i + 1)); \
        if [ $i -eq 30 ]; then \
            echo "   ERROR: Backend failed to start"; \
            exit 1; \
        fi; \
        sleep 1; \
    done
    @echo ""
    @echo "3. Authenticating (${RESYNC_EMAIL})..."
    @TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/register \
        -H "Content-Type: application/json" \
        -d "{\"email\":\"${RESYNC_EMAIL}\",\"password\":\"${RESYNC_PASSWORD}\"}" \
        | grep -o '"token":"[^"]*"' | cut -d'"' -f4); \
    if [ -z "$TOKEN" ]; then \
        TOKEN=$(curl -sf -X POST http://localhost:8080/api/v1/auth/login \
            -H "Content-Type: application/json" \
            -d "{\"email\":\"${RESYNC_EMAIL}\",\"password\":\"${RESYNC_PASSWORD}\"}" \
            | grep -o '"token":"[^"]*"' | cut -d'"' -f4); \
    fi; \
    if [ -z "$TOKEN" ]; then \
        echo "   ERROR: Authentication failed"; \
        exit 1; \
    fi; \
    echo "   Authenticated."; \
    echo ""; \
    echo "4. Creating test wallet..."; \
    WALLET_ID=$(curl -sf -X POST http://localhost:8080/api/v1/wallets \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $TOKEN" \
        -d "{\"name\":\"test\",\"address\":\"${RESYNC_WALLET_ADDRESS}\"}" \
        | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4); \
    echo "   Wallet created: $WALLET_ID"; \
    echo ""; \
    echo "5. Triggering sync..."; \
    curl -sf -X POST "http://localhost:8080/api/v1/wallets/$WALLET_ID/sync" \
        -H "Authorization: Bearer $TOKEN" > /dev/null; \
    echo "   Sync triggered!"; \
    echo ""; \
    echo "=== Done! Wallet is syncing. ==="

# =============================================================================
# Data Migrations
# =============================================================================

# Enqueue backfill jobs for legacy zero-priced lots (dry-run by default)
backfill-legacy-prices-dry:
    cd apps/backend && go run ./cmd/backfill-legacy-prices -dry-run

# Enqueue backfill jobs for legacy zero-priced lots (DESTRUCTIVE - modifies data)
backfill-legacy-prices:
    cd apps/backend && go run ./cmd/backfill-legacy-prices -dry-run=false

# =============================================================================
# Setup & Utilities
# =============================================================================

# Initial project setup
setup:
    @echo "Setting up MoonTrack..."
    @if [ ! -f .env ]; then \
        cp .env.example .env; \
        echo ".env created from .env.example"; \
        echo "Edit .env to set POSTGRES_PASSWORD, JWT_SECRET, COINGECKO_API_KEY"; \
    else \
        echo ".env already exists"; \
    fi
    @echo ""
    @echo "Installing frontend dependencies..."
    cd apps/frontend && bun install
    @echo ""
    @echo "Setup complete!"
    @echo ""
    @echo "Next: just dev"

# Clean build artifacts
clean:
    docker-compose down -v
    rm -rf apps/backend/bin apps/backend/tmp apps/backend/coverage.out apps/backend/coverage.html
    rm -rf apps/frontend/dist apps/frontend/coverage
    @echo "Cleaned"

# Show environment configuration
env:
    @echo "Environment Configuration:"
    @echo ""
    @echo "Database:"
    @echo "  POSTGRES_HOST=${POSTGRES_HOST}"
    @echo "  POSTGRES_PORT=${POSTGRES_PORT}"
    @echo "  POSTGRES_DB=${POSTGRES_DB}"
    @echo "  POSTGRES_USER=${POSTGRES_USER}"
    @echo ""
    @echo "Redis:"
    @echo "  REDIS_HOST=${REDIS_HOST}"
    @echo "  REDIS_PORT=${REDIS_PORT}"
    @echo ""
    @echo "API:"
    @echo "  API_PORT=${API_PORT}"
    @echo "  ENV=${ENV}"
