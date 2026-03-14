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
# Testing
# =============================================================================

# Run all tests (backend + frontend)
test:
    @echo "Running all tests..."
    @just backend-test
    @just frontend-test
    @echo "All tests passed"

# Run backend tests
backend-test:
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

# Lint all code
lint:
    cd apps/backend && golangci-lint run || echo "golangci-lint not installed"
    cd apps/frontend && bun run lint || true
    @echo "Linting complete"

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
