# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

# MoonTrack

Crypto portfolio tracker with double-entry accounting.

## Tech Stack

- **Backend**: Go 1.24+ (Chi router, PostgreSQL 14+, Redis 7+)
- **Frontend**: React 18+ (Vite, TanStack Query, React Router v6, Zustand, Radix UI + Tailwind)
- **Database**: PostgreSQL (TimescaleDB) with NUMERIC(78,0) for financial precision
- **Auth**: JWT + bcrypt
- **Prices**: CoinGecko API with Redis caching
- **Blockchain Sync**: Zerion API (decoded transactions)

## Backend Architecture

Layered architecture with strict dependency rules (outer layers depend on inner):

```
apps/backend/internal/
├── ledger/           # Core: double-entry accounting engine
├── platform/         # Services: user, wallet, asset, taxlot, sync
├── module/           # Handlers & HTTP facades: manual, transfer, swap, defi, genesis, adjustment, portfolio, transactions
├── transport/        # HTTP: router, middleware (JWT, rate-limit, CORS)
└── infra/            # Infrastructure: postgres repos, redis cache, coingecko/zerion API clients
```

**Layer dependencies**: `transport` → `module` → `platform` → `ledger` ← `infra`

### Handler Registry Pattern

New transaction types are added as modules without modifying ledger core:

1. Create handler implementing `ledger.Handler` interface in `internal/module/`
2. Implement `Type()`, `Handle()`, `ValidateData()` methods
3. `GenerateEntries()` must produce balanced entries (SUM(debit) = SUM(credit))
4. Register in `cmd/api/main.go`: `handlerRegistry.Register(myHandler)`

See `internal/module/transfer/handler_transfer_in.go` for reference implementation.

**Registered transaction types**: `transfer_in`, `transfer_out`, `internal_transfer`, `manual_income`, `manual_outcome`, `asset_adjustment`, `swap`, `defi_deposit`, `defi_withdraw`, `defi_claim`, `genesis_balance`

### DI Wiring

All dependency injection is manual in `cmd/api/main.go`. The wiring order:
1. Infra (postgres pool, redis client, API clients)
2. Repositories (postgres repos)
3. Core services (ledger, handler registry)
4. Platform services (user, wallet, asset, taxlot, sync)
5. Post-balance hooks (TaxLotHook for cost basis tracking)
6. Module handlers (registered into handler registry)
7. HTTP handlers and router

### Tax Lot System

Built on top of the ledger via `TaxLotHook` (post-balance hook):
- **TaxLot**: Created for every asset acquisition, tracks quantity remaining
- **LotDisposal**: FIFO-ordered asset dispositions with PnL
- **Effective Cost Basis Priority**: User override > linked source lot > auto-calculated
- **WAC**: Materialized view with lazy refresh strategy

## Commands

```bash
# Infrastructure
just up / down / status / logs

# Database
just migrate-up / migrate-down / db-reset / db-connect
just migrate-create name    # Create new migration
just db-clear-data          # Clear portfolio data, keep user accounts

# Development
just dev                    # Backend (Docker) + frontend (local Vite)
just dev-logs               # Same + Grafana/Loki/Promtail stack

# Build & Test
just test                   # All tests (backend + frontend)
just backend-test           # cd apps/backend && go test ./... -v -short
just frontend-test          # cd apps/frontend && bun test
just fmt                    # Format (go fmt)
just lint                   # Lint (golangci-lint + eslint)
just check                  # fmt + lint + test

# Run specific tests
cd apps/backend && go test -run TestName ./internal/path/...
cd apps/backend && go test -v ./internal/ledger/...
```

## API Endpoints (all under `/api/v1`)

**Public:**
- `POST /auth/register`, `POST /auth/login`

**Protected (JWT):**
- `GET/POST /wallets`, `GET/PUT/DELETE /wallets/{id}`, `POST /wallets/{id}/sync`
- `GET/POST /transactions`, `GET /transactions/{id}`, `GET /transactions/{id}/lots`
- `GET /portfolio`, `GET /portfolio/assets`
- `GET /lots`, `PUT /lots/{id}/override`, `GET /positions/wac`
- `GET /assets`, `GET /assets/search`, `POST /assets/prices`, `GET /assets/{id}`, `GET /assets/{id}/price`, `GET /assets/{id}/history`

**Health:**
- `GET /health`, `GET /health/live`, `GET /health/ready`, `GET /health/detailed`

## Environment

Root `.env` file (copy from `.env.example`). Key variables:
- `DATABASE_URL`, `REDIS_URL` — infra connections
- `JWT_SECRET` — min 32 chars
- `COINGECKO_API_KEY` — price data (free tier)
- `ZERION_API_KEY` — blockchain sync (omit to disable sync)
- `VITE_API_BASE_URL` — frontend API target

Docker uses internal hostnames (`postgres`, `redis`). Backend runs in Docker with hot-reload (air). Frontend runs locally with Vite.

## Key Principles

1. **Financial Precision**: Use `NUMERIC(78,0)` in DB, `math/big.Int` in Go, never float64
2. **Double-Entry Accounting**: Every transaction creates balanced ledger entries (debit = credit)
3. **Handler Registry**: New transaction types as modules without touching ledger core
4. **Security**: Input validation, SQL injection protection, secrets never in code
5. **Simplicity**: YAGNI, no premature abstractions

## Git & Commits

When committing changes, always commit ALL modified files unless explicitly told otherwise. Do not selectively commit only plan/doc files — include all code changes.

## Architecture & Design

Before proposing architectural changes, thoroughly read existing ADRs, PRDs, and architecture docs in `docs/` to ensure proposals don't contradict established patterns (e.g., lot-based accounting, existing DI wiring, entity hierarchy).

## Go Development

After making code changes in Go, always run `go build ./...` before considering the task done. Fix ALL compilation errors before moving on. Do not leave sessions with broken builds.

## Testing

When running tests or dev servers, never use interactive/watch mode. Always use single-run flags (e.g., `bun test --run`, not `bun test` in watch mode). If a command hangs, kill it immediately rather than waiting.

## Code Quality

When fixing lint/type errors, fix the actual underlying code issues. Never simplify or disable lint rules, remove type checks, or suppress errors unless explicitly asked to.

## Observability (Loki MCP)

Query backend logs directly via the `loki` MCP server (LogQL over Loki).

**Prerequisites:**
1. `just dev-logs` — start Grafana+Loki+Promtail stack
2. `just loki-mcp-build` — build the Loki MCP Docker image (one-time)
3. Restart Claude Code to connect the MCP server

**MCP tools:**
- `loki_query` — execute LogQL queries against Loki
- `loki_label_names` — list available labels (discovery)
- `loki_label_values` — get values for a label (e.g., `component`, `level`)

**Indexed labels** (use in stream selectors `{...}`): `service`, `level`, `component`

**Key JSON fields** (use `| json` pipeline): `request_id`, `user_id`, `tx_id`, `msg`, `duration_ms`, `status`, `method`, `path`, `error`, `remote_addr`, `bytes`, `user_agent`

**Level values are UPPERCASE:** `DEBUG`, `INFO`, `WARN`, `ERROR`

**Components:** `ledger`, `taxlot_hook`, `wallet`, `user`, `asset`, `price_updater`, `sync`, `transfer`, `coingecko`, `zerion`, `cache`

**Common queries:**
```logql
{service="backend", level="ERROR"}                             # All errors
{service="backend"} | json | status >= 500                     # 5xx responses
{service="backend"} | json | request_id="<id>"                 # Trace request
{service="backend", component="ledger", level="ERROR"}         # Ledger errors
```

See `.claude/skills/observability-debugging/SKILL.md` for full LogQL reference and debugging workflows.

## Skills & Workflows

When a skill template or existing skill exists for a task (e.g., skill-development skill), always use it as the base. Check `.claude/skills/` before creating new skills from scratch.
