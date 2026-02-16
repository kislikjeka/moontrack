# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

# MoonTrack

Crypto portfolio tracker with double-entry accounting.

## Tech Stack

- **Backend**: Go 1.21+ (Chi router, PostgreSQL 14+, Redis 7+)
- **Frontend**: React 18+ (Vite, TanStack Query, React Router v6)
- **Database**: PostgreSQL with NUMERIC(78,0) for financial precision
- **Auth**: JWT + bcrypt
- **Prices**: CoinGecko API with Redis caching

## Backend Architecture

Layered architecture with strict dependency rules (outer layers depend on inner):

```
apps/backend/internal/
├── ledger/           # Core: double-entry accounting engine
├── platform/         # Services: user, wallet, asset (business logic)
├── module/           # Handlers: manual income/outcome, adjustment
├── transport/        # HTTP: handlers, middleware, router
└── infra/            # Infrastructure: postgres, redis, coingecko
```

**Layer dependencies**: `transport` → `module` → `platform` → `ledger` ← `infra`

### Handler Registry Pattern

New transaction types are added as modules without modifying ledger core:

1. Create handler implementing `ledger.Handler` interface in `internal/module/`
2. Implement `Type()`, `Handle()`, `ValidateData()` methods
3. `GenerateEntries()` must produce balanced entries (SUM(debit) = SUM(credit))
4. Register in `cmd/api/main.go`: `handlerRegistry.Register(myHandler)`

See `internal/module/manual/handler_income.go` for reference implementation.

## Commands

```bash
# Infrastructure
just up / down / status / logs

# Database
just migrate-up / migrate-down / db-reset / db-connect
just migrate-create name    # Create new migration

# Development
just dev                    # Run both backend and frontend
just backend-run            # Backend only
just frontend-run           # Frontend only

# Build & Test
just backend-build / backend-test
just frontend-build / frontend-test
just check                  # All checks (format, lint, test)

# Run specific tests
cd apps/backend && go test -run TestName ./internal/path/...
cd apps/backend && go test -v ./internal/ledger/...          # All ledger tests
```

## API Endpoints

**Public:**
- `POST /auth/register` - Registration
- `POST /auth/login` - Login

**Protected (JWT):**
- `GET /portfolio` - Portfolio summary
- `GET/POST /wallets` - List/create wallets
- `GET /wallets/{id}` - Wallet details
- `GET/POST /transactions` - List/record transactions

**Health:**
- `GET /health` - Basic health
- `GET /health/ready` - Readiness probe
- `GET /health/detailed` - Full health with dependencies

## Environment Variables

`apps/backend/.env`:
```
DATABASE_URL=postgres://postgres:password@localhost:5432/moontrack_dev
REDIS_URL=localhost:6379
JWT_SECRET=your-secret-key-min-32-chars
COINGECKO_API_KEY=your-api-key
PORT=8080
ENV=development
```

## Project Setup / Environment

This project uses Go (backend) and TypeScript (frontend). Docker is used for the dev environment with TimescaleDB/Postgres. The backend uses DI wiring. Always use Docker-internal hostnames (e.g., `postgres` not `localhost`) in DATABASE_URL when running inside Docker containers.

## Key Principles

1. **Financial Precision**: Use `NUMERIC(78,0)` in DB, `math/big.Int` in Go, never float64
2. **Double-Entry Accounting**: Every transaction creates balanced ledger entries (debit = credit)
3. **Handler Registry**: New transaction types as modules without touching ledger core
4. **Security**: Input validation, SQL injection protection, secrets never in code
5. **Simplicity**: YAGNI, no premature abstractions

## Git & Commits

When committing changes, always commit ALL modified files unless explicitly told otherwise. Do not selectively commit only plan/doc files — include all code changes.

## Architecture & Design

Before proposing architectural changes, thoroughly read existing ADRs, PRDs, and architecture docs in the repo to ensure proposals don't contradict established patterns (e.g., lot-based accounting, existing DI wiring, entity hierarchy).

## Go Development

After making code changes in Go, always run `go build ./...` before considering the task done. Fix ALL compilation errors before moving on. Do not leave sessions with broken builds.

## Testing

When running tests or dev servers, never use interactive/watch mode. Always use single-run flags (e.g., `bun test --run`, not `bun test` in watch mode). If a command hangs, kill it immediately rather than waiting.

## Code Quality

When fixing lint/type errors, fix the actual underlying code issues. Never simplify or disable lint rules, remove type checks, or suppress errors unless explicitly asked to.

## Skills & Workflows

When a skill template or existing skill exists for a task (e.g., skill-development skill), always use it as the base. Check `.claude/skills/` before creating new skills from scratch.
