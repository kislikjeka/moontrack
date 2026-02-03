# MoonTrack

Crypto portfolio tracker with double-entry accounting.

## Tech Stack

- **Backend**: Go 1.21+ (Chi router, PostgreSQL 14+, Redis 7+)
- **Frontend**: React 18+ (Vite, TanStack Query, React Router v6)
- **Database**: PostgreSQL with NUMERIC(78,0) for financial precision
- **Auth**: JWT + bcrypt
- **Prices**: CoinGecko API with Redis caching

## Project Structure

```
apps/
├── backend/
│   ├── cmd/api/          # Main entrypoint
│   ├── internal/
│   │   ├── core/         # Ledger, user, pricing
│   │   ├── modules/      # Wallet, portfolio, transactions
│   │   ├── api/          # Handlers, middleware, router
│   │   └── shared/       # Config, logger, database
│   └── migrations/
└── frontend/
    └── src/
        ├── features/     # Auth, dashboard, wallets
        ├── components/   # Reusable UI
        └── services/     # API clients
```

## Commands

```bash
# Infrastructure
just up / down / status / logs

# Database
just migrate-up / migrate-down / db-reset / db-connect

# Development
just dev                    # Run both backend and frontend
just backend-run            # Backend only
just frontend-dev           # Frontend only

# Build & Test
just backend-build / backend-test
just frontend-build / frontend-test
just check                  # All checks (format, lint, test)
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

## Key Principles

1. **Financial Precision**: Use `NUMERIC(78,0)` in DB, `math/big.Int` in Go, never float64
2. **Double-Entry Accounting**: Every transaction creates balanced ledger entries (debit = credit)
3. **Handler Registry**: New transaction types as modules without touching ledger core
4. **Security**: Input validation, SQL injection protection, secrets never in code
5. **Simplicity**: YAGNI, no premature abstractions
