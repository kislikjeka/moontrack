# MoonTrack - Crypto Portfolio Tracker

A web-based cryptocurrency portfolio tracker built with Go (backend) and React (frontend).

## Project Status

**Phase 1: Setup** - ✅ Completed (7/9 tasks)

Remaining setup tasks:
- T008: Setup PostgreSQL database (moontrack_dev) and verify connection
- T009: Setup Redis server and verify connection

## Quick Start

### Prerequisites

- **Go**: 1.21+
- **Node.js**: 18+ LTS
- **PostgreSQL**: 14+ (via SSH tunnel or Docker)
- **Redis**: 7+ (via SSH tunnel or Docker)
- **golang-migrate**: CLI tool for database migrations
- **Just** (optional): Command runner - https://github.com/casey/just

### Using Just Commands (Recommended)

This project includes a `justfile` with convenient commands:

```bash
# View all available commands
just

# Setup project (install dependencies)
just setup

# Start infrastructure (Docker)
just up

# Run database migrations
just migrate-up

# Start development servers (backend + frontend)
just dev

# Run all tests
just test

# View project documentation
just docs
```

### 1. Infrastructure Setup

You have two options:

#### Option A: Use Docker (Recommended for local development)

```bash
# Start PostgreSQL and Redis
docker-compose up -d

# Verify services are running
docker-compose ps
```

#### Option B: Use Existing SSH Tunnels

Your current setup uses SSH tunnels:
- PostgreSQL: `localhost:5432`
- Redis: `localhost:6379`

Update `apps/backend/.env` with the correct PostgreSQL password.

### 2. Database Setup

```bash
# Navigate to backend directory
cd apps/backend

# Create the database (if using Docker)
# Skip this if using existing tunneled database

# Run migrations
migrate -database "${DATABASE_URL}" -path migrations up

# Verify migration status
migrate -database "${DATABASE_URL}" -path migrations version
```

**Note**: Update `DATABASE_URL` in `apps/backend/.env` with the correct password.

Example:
```env
DATABASE_URL=postgres://postgres:YOUR_PASSWORD@localhost:5432/moontrack_dev?sslmode=disable
```

### 3. Environment Configuration

**MoonTrack uses a single `.env` file in the project root** for all configuration.

```bash
# Create .env from template
just env-init

# Or manually
cp .env.example .env
```

**Edit `.env` and update these values:**

```env
# Change database password
POSTGRES_PASSWORD=your_secure_password

# Change JWT secret (min 32 chars)
JWT_SECRET=your-very-long-secure-jwt-secret-key-here

# Add CoinGecko API key (get free at https://www.coingecko.com/en/api)
COINGECKO_API_KEY=your-api-key
```

**View current configuration:**

```bash
just env              # Display all settings (passwords hidden)
just env-validate     # Check configuration
```

The `.env` file is automatically used by:
- Docker Compose (PostgreSQL & Redis)
- Just commands (all development tasks)
- Backend & Frontend (auto-synced)

See **[ENV.md](ENV.md)** for complete environment configuration guide.

### 4. Run the Application

#### Backend

```bash
cd apps/backend

# Run the server
go run cmd/api/main.go

# Or build and run
go build -o bin/api cmd/api/main.go
./bin/api
```

Server will start on `http://localhost:8080`

#### Frontend

```bash
cd apps/frontend

# Start development server
npm run dev

# Build for production
npm run build
```

Frontend will be available at `http://localhost:5173`

## Project Structure

```
moontrack/
├── apps/
│   ├── backend/          # Go backend
│   │   ├── cmd/api/      # Main entry point
│   │   ├── internal/     # Internal packages
│   │   │   ├── core/     # Core systems (ledger, user, pricing)
│   │   │   ├── modules/  # Business features
│   │   │   ├── shared/   # Shared utilities
│   │   │   └── api/      # HTTP handlers
│   │   ├── migrations/   # Database migrations
│   │   └── tests/        # Tests
│   └── frontend/         # React frontend
│       ├── src/          # Source code
│       │   ├── components/
│       │   ├── features/
│       │   └── services/
│       └── tests/        # Component tests
├── specs/                # Feature specifications
└── docker-compose.yml    # Docker infrastructure
```

## Development Workflow

This project follows **Test-First Development** (TDD):

1. Write failing test (RED)
2. Implement minimal code to pass (GREEN)
3. Refactor and improve (REFACTOR)

See `specs/001-portfolio-tracker/quickstart.md` for detailed TDD examples.

## Testing

### Backend Tests

```bash
cd apps/backend

# Run all tests
go test ./... -v

# Run with coverage
go test ./... -cover -coverprofile=coverage.out
go tool cover -html=coverage.out

# Run specific package tests
go test ./internal/core/ledger/... -v
```

### Frontend Tests

```bash
cd apps/frontend

# Run tests
npm test

# Run with coverage
npm test -- --coverage
```

## Database Migrations

```bash
cd apps/backend

# Create new migration
migrate create -ext sql -dir migrations -seq migration_name

# Run migrations
migrate -database "${DATABASE_URL}" -path migrations up

# Rollback one migration
migrate -database "${DATABASE_URL}" -path migrations down 1

# Check current version
migrate -database "${DATABASE_URL}" -path migrations version
```

## Documentation

- **Feature Spec**: `specs/001-portfolio-tracker/spec.md`
- **Implementation Plan**: `specs/001-portfolio-tracker/plan.md`
- **Data Model**: `specs/001-portfolio-tracker/data-model.md`
- **API Contract**: `specs/001-portfolio-tracker/contracts/openapi.yaml`
- **Quickstart Guide**: `specs/001-portfolio-tracker/quickstart.md`
- **Tasks Breakdown**: `specs/001-portfolio-tracker/tasks.md`
- **Project Constitution**: `.specify/memory/constitution.md`

## Next Steps

### Immediate Tasks (T008-T009)

1. **Update** `apps/backend/.env` with correct PostgreSQL password
2. **Run** database migrations: `migrate -database "${DATABASE_URL}" -path migrations up`
3. **Verify** PostgreSQL connection works
4. **Verify** Redis connection works

### Phase 2: Foundational (Starting Next)

After completing setup (T001-T009), proceed to Phase 2:
- Create database schema and verify migrations
- Build shared infrastructure (config, logger, database, errors)
- Implement core ledger system (double-entry accounting)
- Setup HTTP server with middleware

See `specs/001-portfolio-tracker/tasks.md` for the complete task breakdown.

## Architecture Principles

This project follows strict architectural principles defined in `.specify/memory/constitution.md`:

### NON-NEGOTIABLE Rules

1. **Test-First Development**: Write tests before implementation
2. **Security by Design**: JWT auth, input validation, secrets management
3. **Simplicity First**: YAGNI principle, avoid premature optimization
4. **Precision & Immutability**: All financial amounts use `NUMERIC(78,0)` + `*big.Int` (NEVER float64)
5. **Double-Entry Accounting**: Every transaction must balance (`SUM(debit) = SUM(credit)`)
6. **Handler Registry Pattern**: Transaction types as modular handlers

## Tech Stack

### Backend
- **Language**: Go 1.21+
- **HTTP Router**: chi v5
- **Database**: PostgreSQL 15+ with pgx driver
- **Cache**: Redis 7+
- **Auth**: JWT (golang-jwt/jwt v5)
- **Migrations**: golang-migrate/migrate v4

### Frontend
- **Framework**: React 18+
- **Build Tool**: Vite
- **Routing**: React Router v6
- **State**: React Hooks + Zustand (when needed)
- **API Client**: TanStack Query + axios

### External Services
- **Price API**: CoinGecko (Free Demo Plan)

## License

Private project - All rights reserved

## Support

For issues or questions, refer to:
- Project documentation in `specs/`
- Constitution in `.specify/memory/constitution.md`
- Quickstart guide in `specs/001-portfolio-tracker/quickstart.md`

## Just Command Quick Reference

```bash
# Environment
just env             # Show all config (passwords hidden)
just env-init        # Create .env from template
just env-validate    # Validate configuration
just env-sync        # Sync .env to apps

# Infrastructure
just up              # Start PostgreSQL + Redis
just down            # Stop infrastructure
just status          # Check infrastructure status
just logs            # View infrastructure logs

# Database
just migrate-up      # Apply migrations
just migrate-down    # Rollback last migration
just db-reset        # Reset database
just db-connect      # Connect to PostgreSQL

# Backend
just backend-run     # Run backend server
just backend-test    # Run backend tests
just backend-build   # Build backend binary
just backend-fmt     # Format Go code

# Frontend
just frontend-run    # Run frontend dev server
just frontend-test   # Run frontend tests
just frontend-build  # Build for production
just frontend-lint   # Lint frontend code

# Development
just setup           # Install all dependencies
just dev             # Start backend + frontend
just test            # Run all tests
just check           # Format, lint, and test
just verify          # Verify project setup

# Utilities
just docs            # Show documentation paths
just stats           # Show project statistics
just coingecko       # CoinGecko API setup instructions
```
