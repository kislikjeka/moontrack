# MoonTrack - Crypto Portfolio Tracker
# Just command runner - https://github.com/casey/just

# Load environment variables from .env file
set dotenv-load

# Default recipe to display help
default:
    @just --list

# =============================================================================
# Infrastructure Commands
# =============================================================================

# Start Docker infrastructure (PostgreSQL + Redis)
up:
    @echo "📋 Checking .env configuration..."
    @just env-validate
    @echo ""
    @just env-sync
    @echo ""
    @echo "🚀 Starting Docker infrastructure..."
    docker-compose up -d
    @echo ""
    @echo "✅ Infrastructure started"
    @echo "PostgreSQL: ${POSTGRES_HOST}:${POSTGRES_PORT}"
    @echo "Redis: ${REDIS_HOST}:${REDIS_PORT}"
    @echo "Database: ${POSTGRES_DB}"
    @echo ""
    @echo "Next: just migrate-up"

# Stop Docker infrastructure
down:
    docker-compose down
    @echo "✅ Infrastructure stopped"

# Restart Docker infrastructure
restart:
    docker-compose restart
    @echo "✅ Infrastructure restarted"

# View infrastructure logs
logs:
    docker-compose logs -f

# Check infrastructure status
status:
    docker-compose ps

# Clean infrastructure (removes volumes)
clean:
    docker-compose down -v
    @echo "✅ Infrastructure cleaned (volumes removed)"

# =============================================================================
# Database Commands
# =============================================================================

# Run database migrations (up)
migrate-up:
    cd apps/backend && migrate -database "${DATABASE_URL}" -path migrations up
    @echo "✅ Migrations applied"

# Rollback last migration
migrate-down:
    cd apps/backend && migrate -database "${DATABASE_URL}" -path migrations down 1
    @echo "✅ Last migration rolled back"

# Check migration version
migrate-version:
    cd apps/backend && migrate -database "${DATABASE_URL}" -path migrations version

# Create new migration
migrate-create name:
    cd apps/backend && migrate create -ext sql -dir migrations -seq {{name}}
    @echo "✅ Migration created: migrations/{{name}}"

# Reset database (drop all tables and re-run migrations)
db-reset:
    cd apps/backend && migrate -database "${DATABASE_URL}" -path migrations down -all || true
    cd apps/backend && migrate -database "${DATABASE_URL}" -path migrations up
    @echo "✅ Database reset complete"

# Connect to PostgreSQL database
db-connect:
    docker exec -it moontrack-postgres psql -U ${POSTGRES_USER} -d ${POSTGRES_DB}

# Connect to Redis
redis-connect:
    @if [ -n "${REDIS_PASSWORD}" ]; then \
        docker exec -it moontrack-redis redis-cli -a ${REDIS_PASSWORD}; \
    else \
        docker exec -it moontrack-redis redis-cli; \
    fi

# =============================================================================
# Backend Commands
# =============================================================================

# Install backend dependencies
backend-deps:
    cd apps/backend && go mod download
    cd apps/backend && go mod tidy
    @echo "✅ Backend dependencies installed"

# Run backend server (kills existing process on port 8080 first)
backend-run:
    @-lsof -ti:8080 | xargs kill -9 2>/dev/null || true
    cd apps/backend && go run cmd/api/main.go

# Restart backend server (rebuild and run)
backend-restart:
    @echo "🔄 Restarting backend..."
    @-lsof -ti:8080 | xargs kill -9 2>/dev/null || true
    @just backend-build
    cd apps/backend && ./bin/api

# Build backend binary
backend-build:
    cd apps/backend && go build -o bin/api cmd/api/main.go
    @echo "✅ Backend built: apps/backend/bin/api"

# Run backend unit tests (fast, no Docker required)
backend-test:
    cd apps/backend && go test ./... -v -short

# Run backend unit tests only (alias)
backend-test-unit:
    cd apps/backend && go test ./... -v -short

# Run backend integration tests (requires Docker)
backend-test-integration:
    @echo "🐳 Running integration tests (requires Docker)..."
    cd apps/backend && TESTCONTAINERS_RYUK_DISABLED=true go test ./... -v -tags=integration -count=1 -timeout=5m

# Run all backend tests (unit + integration)
backend-test-all:
    @echo "🧪 Running all backend tests..."
    @just backend-test-unit
    @just backend-test-integration
    @echo "✅ All backend tests passed!"

# Run backend tests with coverage
backend-coverage:
    cd apps/backend && go test ./... -cover -coverprofile=coverage.out
    cd apps/backend && go tool cover -html=coverage.out

# Format backend code
backend-fmt:
    cd apps/backend && go fmt ./...
    @echo "✅ Backend code formatted"

# Lint backend code
backend-lint:
    cd apps/backend && golangci-lint run || echo "golangci-lint not installed"

# Clean backend build artifacts
backend-clean:
    rm -rf apps/backend/bin
    rm -f apps/backend/coverage.out
    @echo "✅ Backend cleaned"

# =============================================================================
# Frontend Commands
# =============================================================================

# Install frontend dependencies
frontend-deps:
    cd apps/frontend && npm install
    @echo "✅ Frontend dependencies installed"

# Run frontend dev server
frontend-run:
    cd apps/frontend && npm run dev

# Build frontend for production
frontend-build:
    cd apps/frontend && npm run build
    @echo "✅ Frontend built: apps/frontend/dist"

# Run frontend tests
frontend-test:
    cd apps/frontend && npm test

# Run frontend tests with coverage
frontend-coverage:
    cd apps/frontend && npm test -- --coverage

# Lint frontend code
frontend-lint:
    cd apps/frontend && npm run lint

# Clean frontend build artifacts
frontend-clean:
    rm -rf apps/frontend/dist
    rm -rf apps/frontend/node_modules/.vite
    @echo "✅ Frontend cleaned"

# =============================================================================
# Development Workflow Commands
# =============================================================================

# Setup project (install all dependencies)
setup:
    @echo "🚀 Setting up MoonTrack project..."
    @echo ""
    @echo "Step 1: Checking .env file..."
    @just env-init || true
    @echo ""
    @echo "Step 2: Installing backend dependencies..."
    @just backend-deps
    @echo ""
    @echo "Step 3: Installing frontend dependencies..."
    @just frontend-deps
    @echo ""
    @echo "✅ Setup complete!"
    @echo ""
    @echo "Next steps:"
    @echo "  1. Edit .env file with your configuration"
    @echo "  2. Run: just up           (start infrastructure)"
    @echo "  3. Run: just migrate-up   (apply database migrations)"
    @echo "  4. Run: just dev          (start development servers)"
    @echo ""
    @echo "Quick start: just env && just verify"

# Start development environment (backend + frontend)
dev:
    @echo "🚀 Starting development servers..."
    @echo "Backend will run on http://localhost:8080"
    @echo "Frontend will run on http://localhost:5173"
    @echo ""
    @echo "Press Ctrl+C to stop"
    @just backend-run & just frontend-run

# Run all tests (backend + frontend)
test:
    @echo "🧪 Running all tests..."
    @just backend-test
    @just frontend-test
    @echo "✅ All tests complete!"

# Run all tests with coverage
test-coverage:
    @echo "🧪 Running tests with coverage..."
    @just backend-coverage
    @just frontend-coverage
    @echo "✅ Coverage reports generated!"

# Format all code (backend + frontend)
fmt:
    @just backend-fmt
    @echo "✅ All code formatted"

# Lint all code (backend + frontend)
lint:
    @just backend-lint
    @just frontend-lint
    @echo "✅ All code linted"

# Clean all build artifacts
clean-all:
    @just backend-clean
    @just frontend-clean
    @echo "✅ All build artifacts cleaned"

# Full reset (clean everything and setup from scratch)
reset:
    @just clean
    @just clean-all
    rm -rf apps/frontend/node_modules
    @echo "✅ Full reset complete. Run 'just setup' to reinstall."

# =============================================================================
# Verification Commands
# =============================================================================

# Verify project setup
verify:
    @echo "🔍 Verifying project setup..."
    @echo ""
    @echo "Checking .env file..."
    @just env-validate
    @echo ""
    @echo "Checking Go installation..."
    @go version || echo "❌ Go not installed"
    @echo ""
    @echo "Checking Node.js installation..."
    @node --version || echo "❌ Node.js not installed"
    @echo ""
    @echo "Checking npm installation..."
    @npm --version || echo "❌ npm not installed"
    @echo ""
    @echo "Checking Docker installation..."
    @docker --version || echo "❌ Docker not installed"
    @echo ""
    @echo "Checking docker-compose installation..."
    @docker-compose --version || echo "❌ docker-compose not installed"
    @echo ""
    @echo "Checking migrate CLI..."
    @migrate -version || echo "❌ golang-migrate not installed"
    @echo ""
    @echo "Checking infrastructure status..."
    @just status || echo "⚠️  Infrastructure not running (run: just up)"
    @echo ""
    @echo "✅ Verification complete!"

# Check code quality (format, lint, test)
check:
    @echo "🔍 Checking code quality..."
    @just fmt
    @just lint
    @just test
    @echo "✅ All checks passed!"

# =============================================================================
# Documentation Commands
# =============================================================================

# View project documentation
docs:
    @echo "📚 MoonTrack Documentation:"
    @echo ""
    @echo "General:"
    @echo "  README.md                                    - Project overview"
    @echo "  .specify/memory/constitution.md              - Project principles"
    @echo ""
    @echo "Feature Specs:"
    @echo "  specs/001-portfolio-tracker/spec.md          - Feature requirements"
    @echo "  specs/001-portfolio-tracker/plan.md          - Implementation plan"
    @echo "  specs/001-portfolio-tracker/tasks.md         - Task breakdown"
    @echo "  specs/001-portfolio-tracker/data-model.md    - Database schema"
    @echo "  specs/001-portfolio-tracker/research.md      - Technology decisions"
    @echo "  specs/001-portfolio-tracker/quickstart.md    - Development guide"
    @echo ""
    @echo "API:"
    @echo "  specs/001-portfolio-tracker/contracts/openapi.yaml - API specification"

# Open API documentation (requires OpenAPI viewer)
api-docs:
    @echo "📖 Opening API documentation..."
    @echo "View: specs/001-portfolio-tracker/contracts/openapi.yaml"
    @echo "Use Swagger Editor: https://editor.swagger.io"

# =============================================================================
# Git Commands
# =============================================================================

# Git status
git-status:
    git status

# Git diff
git-diff:
    git diff

# Add all changes and commit
git-commit message:
    git add .
    git commit -m "{{message}}"
    @echo "✅ Changes committed"

# Quick commit with default message
git-save:
    git add .
    git commit -m "WIP: Save progress"
    @echo "✅ Progress saved"

# =============================================================================
# Production Commands
# =============================================================================

# Build production artifacts
build-prod:
    @echo "🏗️ Building production artifacts..."
    @just backend-build
    @just frontend-build
    @echo "✅ Production build complete!"

# Run production build locally
run-prod:
    @echo "🚀 Running production build..."
    cd apps/backend && ./bin/api

# =============================================================================
# Utility Commands
# =============================================================================

# Show environment variables (from root .env)
env:
    @echo "📋 Environment Configuration:"
    @echo ""
    @echo "Database:"
    @echo "  POSTGRES_HOST=${POSTGRES_HOST}"
    @echo "  POSTGRES_PORT=${POSTGRES_PORT}"
    @echo "  POSTGRES_DB=${POSTGRES_DB}"
    @echo "  POSTGRES_USER=${POSTGRES_USER}"
    @echo "  POSTGRES_PASSWORD=***hidden***"
    @echo ""
    @echo "Redis:"
    @echo "  REDIS_HOST=${REDIS_HOST}"
    @echo "  REDIS_PORT=${REDIS_PORT}"
    @if [ -n "${REDIS_PASSWORD}" ]; then echo "  REDIS_PASSWORD=***set***"; else echo "  REDIS_PASSWORD=<empty>"; fi
    @echo ""
    @echo "API:"
    @echo "  API_HOST=${API_HOST}"
    @echo "  API_PORT=${API_PORT}"
    @echo "  ENV=${ENV}"
    @echo ""
    @echo "DATABASE_URL=${DATABASE_URL}"

# Initialize .env file from example
env-init:
    @if [ -f .env ]; then \
        echo "⚠️  .env file already exists!"; \
        echo "To recreate it, delete .env first or edit it manually"; \
    else \
        cp .env.example .env; \
        echo "✅ .env file created from .env.example"; \
        echo "📝 Please edit .env and update:"; \
        echo "   - POSTGRES_PASSWORD"; \
        echo "   - JWT_SECRET"; \
        echo "   - COINGECKO_API_KEY"; \
    fi

# Validate .env file
env-validate:
    @echo "🔍 Validating .env configuration..."
    @if [ ! -f .env ]; then \
        echo "❌ .env file not found! Run: just env-init"; \
        exit 1; \
    fi
    @if [ "${POSTGRES_PASSWORD}" = "postgres" ]; then \
        echo "⚠️  Warning: Using default postgres password"; \
    fi
    @if [ "${JWT_SECRET}" = "dev-secret-key-change-in-production-min-32-chars-long-please" ]; then \
        echo "⚠️  Warning: Using default JWT secret"; \
    fi
    @if [ -z "${COINGECKO_API_KEY}" ]; then \
        echo "⚠️  Warning: COINGECKO_API_KEY not set"; \
    fi
    @echo "✅ .env file exists and loaded"

# Sync .env to backend and frontend directories
env-sync:
    @./scripts/sync-env.sh

# Generate CoinGecko API key instructions
coingecko:
    @echo "🪙 CoinGecko API Setup:"
    @echo ""
    @echo "1. Visit: https://www.coingecko.com/en/api"
    @echo "2. Sign up for a free account"
    @echo "3. Get your Demo API key (free tier)"
    @echo "4. Add to apps/backend/.env:"
    @echo "   COINGECKO_API_KEY=your-api-key-here"
    @echo ""
    @echo "Free tier limits:"
    @echo "  - 30 calls/minute"
    @echo "  - 10,000 calls/month"
    @echo "  - Historical data access"

# Show project statistics
stats:
    @echo "📊 Project Statistics:"
    @echo ""
    @echo "Backend (Go):"
    @find apps/backend -name '*.go' | xargs wc -l | tail -1
    @echo ""
    @echo "Frontend (JS/JSX):"
    @find apps/frontend/src -name '*.js' -o -name '*.jsx' | xargs wc -l | tail -1 || echo "No files yet"
    @echo ""
    @echo "Migrations:"
    @ls -la apps/backend/migrations/ | grep -v "^d" | wc -l || echo "0"
    @echo ""
    @echo "Tests:"
    @find apps/backend -name '*_test.go' | wc -l || echo "0 backend tests"
    @find apps/frontend/tests -name '*.test.js*' | wc -l || echo "0 frontend tests"
