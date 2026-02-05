# MoonTrack - Crypto Portfolio Tracker
# Just command runner - https://github.com/casey/just

set dotenv-load

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
