# MoonTrack Development Guidelines

Auto-generated from all feature plans. Last updated: 2026-01-11

## Active Technologies

- Backend: Go 1.21+ with Chi router, PostgreSQL 14+, Redis 7+
- Frontend: React 18+ with Vite, TanStack Query, React Router v6
- Database: PostgreSQL with NUMERIC(78,0) for financial precision
- Authentication: JWT tokens with bcrypt password hashing
- Price Data: CoinGecko API with Redis caching

## Project Structure

```text
apps/
├── backend/                  # Go backend
│   ├── cmd/api/             # Main API entrypoint
│   ├── internal/
│   │   ├── core/            # Core systems (ledger, user, pricing)
│   │   ├── modules/         # Business features (wallet, portfolio, transactions)
│   │   ├── api/             # HTTP handlers, middleware, router
│   │   └── shared/          # Config, logger, database, errors
│   ├── migrations/          # SQL migrations
│   └── tests/               # Contract, integration, unit tests
└── frontend/                # React frontend
    ├── src/
    │   ├── features/        # Feature modules (auth, dashboard, wallets)
    │   ├── components/      # Reusable UI components
    │   └── services/        # API clients
    └── tests/               # Component tests
```

## Commands

### Infrastructure
```bash
just up              # Start Docker (PostgreSQL + Redis)
just down            # Stop Docker
just status          # Check infrastructure status
just logs            # View logs
```

### Database
```bash
just migrate-up      # Run migrations
just migrate-down    # Rollback last migration
just db-reset        # Drop all tables and re-migrate
just db-connect      # Connect to PostgreSQL
```

### Backend
```bash
just backend-run     # Run backend server
just backend-test    # Run all tests
just backend-build   # Build binary
cd apps/backend && go test ./...  # Run tests directly
```

### Frontend
```bash
just frontend-dev    # Run dev server
just frontend-build  # Build for production
just frontend-test   # Run tests
```

### Quality Checks
```bash
just check           # Run all checks (format, lint, test)
bash scripts/check-constitution-compliance.sh  # Constitution compliance
```

### Development
```bash
just dev             # Run both backend and frontend
just test            # Run all tests (backend + frontend)
```

## Code Style

### Backend (Go)
- Use `*big.Int` for ALL financial amounts (never float64)
- Database amounts use `NUMERIC(78,0)`
- Ledger entries are IMMUTABLE (no UPDATE/DELETE)
- Every transaction must balance: `SUM(debit) = SUM(credit)`
- Use structured logging with context
- Table-driven tests for unit tests
- JWT authentication for all protected endpoints

### Frontend (React)
- Use TypeScript for type safety
- TanStack Query for server state
- React hooks for local state
- Component-based architecture
- Props interface definitions

### Constitution Principles (NON-NEGOTIABLE)
1. **Precision**: Use `*big.Int` + `NUMERIC(78,0)` for amounts
2. **Immutability**: Ledger entries never change after creation
3. **Double-Entry**: All transactions must balance
4. **Test-First**: Write tests before implementation
5. **Security**: Environment variables for secrets, JWT auth, input validation

## Recent Changes

- 2026-01-11: Added Phase 6 (Portfolio View) - Dashboard with asset breakdown
- 2026-01-11: Added Phase 7 (Polish) - Rate limiting, health checks, security
- 2026-01-11: Added constitution compliance checks
- 2026-01-06: Initial project setup with authentication and wallet management

## API Endpoints

### Public
- POST /auth/register - User registration
- POST /auth/login - User login

### Protected (Requires JWT)
- GET /portfolio - Get portfolio summary
- GET /wallets - List user wallets
- POST /wallets - Create wallet
- GET /wallets/{id} - Get wallet details
- POST /transactions - Record transaction
- GET /transactions - List transactions

### Health
- GET /health - Basic health check
- GET /health/live - Liveness probe
- GET /health/ready - Readiness probe
- GET /health/detailed - Detailed health with dependencies

## Environment Variables

Required in `apps/backend/.env`:
```
DATABASE_URL=postgres://postgres:password@localhost:5432/moontrack_dev
REDIS_URL=localhost:6379
JWT_SECRET=your-secret-key-min-32-chars
COINGECKO_API_KEY=your-api-key
PORT=8080
ENV=development
```

## Testing

- Backend target: 80%+ coverage
- Frontend target: 70%+ coverage
- Constitution compliance must pass
- All tests must pass before merge

<!-- MANUAL ADDITIONS START -->
<!-- MANUAL ADDITIONS END -->
