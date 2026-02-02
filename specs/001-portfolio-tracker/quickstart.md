# Quickstart Guide: Crypto Portfolio Tracker

**Feature**: 001-portfolio-tracker
**Branch**: `001-portfolio-tracker`
**Date**: 2026-01-06

## Overview

This quickstart guide helps you set up the development environment and start implementing the crypto portfolio tracker feature following the constitution's Test-First Development principle.

## Prerequisites

### Required Software

- **Golang**: 1.21+ ([download](https://go.dev/dl/))
- **Node.js**: 18+ LTS ([download](https://nodejs.org/))
- **PostgreSQL**: 14+ ([download](https://www.postgresql.org/download/))
- **Redis**: 7+ (for price caching) ([download](https://redis.io/download/))
- **Git**: Latest version

### Recommended Tools

- **golang-migrate CLI**: For running database migrations
  ```bash
  go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
  ```
- **Postman** or **Insomnia**: For API testing (can use OpenAPI spec in `contracts/openapi.yaml`)
- **pgAdmin** or **TablePlus**: For database inspection

---

## Project Setup

### 1. Initialize Backend (Golang)

```bash
# Navigate to backend directory
cd apps/backend

# Initialize Go modules
go mod init github.com/yourusername/moontrack

# Install core dependencies
go get github.com/go-chi/chi/v5              # HTTP router
go get github.com/golang-jwt/jwt/v5          # JWT auth
go get github.com/jackc/pgx/v5               # PostgreSQL driver
go get github.com/golang-migrate/migrate/v4  # Migrations
go get github.com/redis/go-redis/v9          # Redis client

# Tidy dependencies
go mod tidy
```

### 2. Initialize Frontend (React)

```bash
# Navigate to frontend directory
cd apps/frontend

# Create React app with Vite (recommended for speed)
npm create vite@latest . -- --template react

# Or use Create React App
# npx create-react-app .

# Install dependencies
npm install react-router-dom         # Routing
npm install @tanstack/react-query    # Server state management
npm install axios                     # HTTP client
npm install zustand                   # Client state (install when needed)

# Install dev dependencies
npm install -D @testing-library/react @testing-library/jest-dom
```

### 3. Set Up PostgreSQL Database

```bash
# Create database
createdb moontrack_dev

# Or using psql
psql -U postgres
CREATE DATABASE moontrack_dev;
\q
```

### 4. Set Up Redis

```bash
# Start Redis server (macOS with Homebrew)
brew services start redis

# Or start manually
redis-server

# Verify Redis is running
redis-cli ping
# Should return: PONG
```

### 5. Create Environment Variables

Create `apps/backend/.env`:

```env
# Database
DATABASE_URL=postgres://postgres:password@localhost:5432/moontrack_dev?sslmode=disable

# Redis
REDIS_URL=localhost:6379
REDIS_PASSWORD=

# JWT
JWT_SECRET=your-secret-key-change-this-in-production-min-32-chars

# CoinGecko API
COINGECKO_API_KEY=your-demo-api-key-here

# Server
PORT=8080
ENV=development
```

**IMPORTANT**: Never commit `.env` to git! Add to `.gitignore`:

```bash
echo "apps/backend/.env" >> .gitignore
echo "apps/frontend/.env" >> .gitignore
```

Create `apps/frontend/.env`:

```env
VITE_API_BASE_URL=http://localhost:8080/api/v1
```

---

## Database Migrations

### 1. Create Migrations Directory

```bash
cd apps/backend
mkdir -p migrations
```

### 2. Create Initial Migration

```bash
migrate create -ext sql -dir migrations -seq create_schema
```

This creates two files:
- `migrations/000001_create_schema.up.sql`
- `migrations/000001_create_schema.down.sql`

### 3. Copy Schema from data-model.md

Copy the SQL schema from `specs/001-portfolio-tracker/data-model.md` into `000001_create_schema.up.sql`.

For the down migration (`000001_create_schema.down.sql`):

```sql
DROP TABLE IF EXISTS manual_price_overrides;
DROP TABLE IF EXISTS price_snapshots;
DROP TABLE IF EXISTS account_balances;
DROP TABLE IF EXISTS entries;
DROP TABLE IF EXISTS transactions;
DROP TABLE IF EXISTS accounts;
DROP TABLE IF EXISTS wallets;
DROP TABLE IF EXISTS users;
```

### 4. Run Migrations

```bash
# Run all up migrations
migrate -database "${DATABASE_URL}" -path migrations up

# Check migration status
migrate -database "${DATABASE_URL}" -path migrations version

# Rollback one migration (if needed)
migrate -database "${DATABASE_URL}" -path migrations down 1
```

---

## Project Structure Setup

Create the directory structure per `plan.md`:

```bash
cd apps/backend

# Core systems (foundational infrastructure)
mkdir -p internal/core/ledger/{domain,handler,service,repository,postgres}
mkdir -p internal/core/user/{domain,service,repository,auth}
mkdir -p internal/core/pricing/{coingecko,cache,service}

# Domain modules (business features)
mkdir -p internal/modules/manual_transaction/{domain,handler}
mkdir -p internal/modules/asset_adjustment/{domain,handler}
mkdir -p internal/modules/wallet/{domain,service,repository}
mkdir -p internal/modules/portfolio/service

# Shared utilities
mkdir -p internal/shared/{config,logger,database,errors}

# HTTP layer
mkdir -p internal/api/{handlers,middleware,router}

# Tests
mkdir -p tests/{contract,integration,unit}

# Main entry point
mkdir -p cmd/api
```

Frontend structure:

```bash
cd apps/frontend/src

mkdir -p components/{common,layout}
mkdir -p features/{auth,dashboard,wallets,transactions}
mkdir -p services
mkdir -p hooks
mkdir -p utils
mkdir -p types
mkdir -p tests/components
```

---

## Test-First Development Workflow

Per constitution Principle I, follow the Red-Green-Refactor cycle:

### Example: Creating User Registration

#### 1. RED: Write failing test first

`apps/backend/internal/core/user/service/user_service_test.go`:

```go
package service_test

import (
    "context"
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestUserService_Register(t *testing.T) {
    tests := []struct {
        name    string
        email   string
        password string
        wantErr bool
    }{
        {
            name:     "valid registration",
            email:    "user@example.com",
            password: "SecureP@ssw0rd",
            wantErr:  false,
        },
        {
            name:     "duplicate email",
            email:    "existing@example.com",
            password: "SecureP@ssw0rd",
            wantErr:  true,
        },
        {
            name:     "password too short",
            email:    "user@example.com",
            password: "short",
            wantErr:  true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            svc := setupUserService(t) // Helper to create service with test DB

            user, err := svc.Register(context.Background(), tt.email, tt.password)

            if tt.wantErr {
                require.Error(t, err)
                return
            }

            require.NoError(t, err)
            assert.NotEmpty(t, user.ID)
            assert.Equal(t, tt.email, user.Email)
            assert.NotEqual(t, tt.password, user.PasswordHash) // Must be hashed
        })
    }
}
```

Run test: `go test ./internal/core/user/service/... -v`

**Expected**: Test fails (RED) because implementation doesn't exist yet.

#### 2. GREEN: Write minimal implementation to pass test

`apps/backend/internal/core/user/service/user_service.go`:

```go
package service

import (
    "context"
    "errors"
    "fmt"
    "github.com/google/uuid"
    "golang.org/x/crypto/bcrypt"
)

type UserService struct {
    repo UserRepository
}

func (s *UserService) Register(ctx context.Context, email, password string) (*User, error) {
    // Validate password length
    if len(password) < 8 {
        return nil, errors.New("password must be at least 8 characters")
    }

    // Hash password
    hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
    if err != nil {
        return nil, fmt.Errorf("failed to hash password: %w", err)
    }

    // Create user
    user := &User{
        ID:           uuid.New(),
        Email:        email,
        PasswordHash: string(hash),
    }

    // Save to database
    if err := s.repo.Create(ctx, user); err != nil {
        return nil, fmt.Errorf("failed to create user: %w", err)
    }

    return user, nil
}
```

Run test: `go test ./internal/core/user/service/... -v`

**Expected**: Test passes (GREEN).

#### 3. REFACTOR: Clean up code

Extract validation, improve error handling, add logging if needed.

---

## Running the Application

### Start Backend Server

```bash
cd apps/backend
go run cmd/api/main.go
```

Expected output:
```
[INFO] Starting MoonTrack API server
[INFO] Database migrations up to date
[INFO] Server listening on :8080
```

### Start Frontend Dev Server

```bash
cd apps/frontend
npm run dev
```

Expected output:
```
VITE v5.x.x  ready in 500 ms
➜  Local:   http://localhost:5173/
```

### Verify Setup

1. **Health check**: `curl http://localhost:8080/health`
2. **Database**: `psql moontrack_dev -c "\dt"` (should show all tables)
3. **Redis**: `redis-cli ping` (should return PONG)
4. **Frontend**: Open http://localhost:5173 in browser

---

## Development Workflow

### 1. Choose a User Story

Start with highest priority from `spec.md`:
- **P1**: User Story 1 (View Portfolio Balance) or User Story 2 (Manage Wallets)
- **P2**: User Story 3 (Record Transactions) or User Story 4 (User Authentication)

Recommended order: Authentication → Wallets → Transactions → Portfolio View

### 2. Write Tests First

For each acceptance scenario, write:
1. **Contract tests**: HTTP API endpoint tests
2. **Integration tests**: Service + repository with test database
3. **Unit tests**: Business logic in isolation

### 3. Implement Feature

Follow Test-First Development (TDD):
- Write test → See it fail (RED)
- Implement code → See it pass (GREEN)
- Refactor → Keep tests passing

### 4. Verify Ledger Balance (Financial Features Only)

For any transaction-related code, MUST include test verifying:

```go
func TestTransaction_BalancesLedger(t *testing.T) {
    // ... create transaction

    entries, err := ledger.GetEntries(ctx, transaction.ID)
    require.NoError(t, err)

    // VERIFY INVARIANT: SUM(debit) = SUM(credit)
    debitSum := big.NewInt(0)
    creditSum := big.NewInt(0)

    for _, entry := range entries {
        if entry.DebitCredit == "DEBIT" {
            debitSum.Add(debitSum, entry.Amount)
        } else {
            creditSum.Add(creditSum, entry.Amount)
        }
    }

    assert.Equal(t, 0, debitSum.Cmp(creditSum), "ledger must balance")
}
```

### 5. Code Review Checklist

Before committing, verify:
- [ ] All tests pass: `go test ./... -v`
- [ ] No `float64` for financial amounts (grep check)
- [ ] All amounts use `*big.Int`
- [ ] Database uses `NUMERIC(78,0)` for amounts
- [ ] Ledger entries immutable (no UPDATE queries)
- [ ] JWT secret not committed
- [ ] Input validation on API boundaries
- [ ] Error messages don't leak sensitive info

---

## Key Files Reference

| File | Purpose |
|------|---------|
| `specs/001-portfolio-tracker/spec.md` | Feature requirements and user stories |
| `specs/001-portfolio-tracker/plan.md` | Implementation plan with tech stack |
| `specs/001-portfolio-tracker/research.md` | Library selection rationale |
| `specs/001-portfolio-tracker/data-model.md` | Database schema and entities |
| `specs/001-portfolio-tracker/contracts/openapi.yaml` | API specification |
| `.specify/memory/constitution.md` | Project constitution (NON-NEGOTIABLE principles) |

---

## Common Commands

### Backend

```bash
# Run tests
go test ./... -v

# Run tests with coverage
go test ./... -cover -coverprofile=coverage.out
go tool cover -html=coverage.out

# Run specific test
go test ./internal/ledger/service/... -run TestLedgerService_RecordTransaction -v

# Build
go build -o bin/api cmd/api/main.go

# Run
./bin/api

# Format code
go fmt ./...

# Lint (install golangci-lint first)
golangci-lint run
```

### Frontend

```bash
# Run dev server
npm run dev

# Run tests
npm test

# Build for production
npm run build

# Preview production build
npm run preview

# Lint
npm run lint
```

### Database

```bash
# Run migrations up
migrate -database "${DATABASE_URL}" -path migrations up

# Create new migration
migrate create -ext sql -dir migrations -seq migration_name

# Reset database (DROP + CREATE + MIGRATE)
dropdb moontrack_dev && createdb moontrack_dev && migrate -database "${DATABASE_URL}" -path migrations up
```

---

## Troubleshooting

### "Database connection refused"

- Check PostgreSQL is running: `pg_isready`
- Check DATABASE_URL in `.env`
- Check PostgreSQL logs: `tail -f /usr/local/var/log/postgres.log`

### "Redis connection refused"

- Check Redis is running: `redis-cli ping`
- Start Redis: `redis-server` or `brew services start redis`

### "Migration failed: dirty database"

```bash
# Force version (be careful!)
migrate -database "${DATABASE_URL}" -path migrations force <version>

# Or reset and re-run
migrate -database "${DATABASE_URL}" -path migrations down -all
migrate -database "${DATABASE_URL}" -path migrations up
```

### "Go module not found"

```bash
go mod tidy
go mod download
```

### "frontend CORS error"

Add CORS middleware to backend:

```go
import "github.com/go-chi/cors"

r.Use(cors.Handler(cors.Options{
    AllowedOrigins:   []string{"http://localhost:5173"},
    AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
    AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
    AllowCredentials: true,
}))
```

---

## Next Steps

1. **Implement Authentication** (User Story 4):
   - User registration endpoint
   - Login endpoint with JWT generation
   - JWT validation middleware

2. **Implement Wallet Management** (User Story 2):
   - Create wallet endpoint
   - List wallets endpoint
   - Get wallet details

3. **Implement Ledger Core**:
   - Account creation
   - Transaction handler registry
   - LedgerService with balance verification

4. **Implement Manual Transactions** (User Story 3):
   - ManualIncomeTransaction handler
   - ManualOutcomeTransaction handler
   - AssetAdjustmentTransaction handler

5. **Implement Portfolio View** (User Story 1):
   - CoinGecko integration with caching
   - Portfolio aggregation service
   - Balance calculation from ledger

6. **Frontend Components**:
   - Login/Registration forms
   - Dashboard with portfolio summary
   - Wallet list and detail views
   - Add transaction form

---

## Resources

- **Constitution**: `.specify/memory/constitution.md` (READ FIRST!)
- **Feature Spec**: `specs/001-portfolio-tracker/spec.md`
- **API Contract**: `specs/001-portfolio-tracker/contracts/openapi.yaml`
- **Data Model**: `specs/001-portfolio-tracker/data-model.md`

- **Go Chi Router**: https://github.com/go-chi/chi
- **golang-jwt**: https://github.com/golang-jwt/jwt
- **React Router**: https://reactrouter.com/
- **TanStack Query**: https://tanstack.com/query/latest
- **CoinGecko API**: https://docs.coingecko.com/reference/introduction

---

## Constitution Reminders

🔴 **NON-NEGOTIABLE**:
1. All financial amounts: `NUMERIC(78,0)` (DB) + `*big.Int` (Go) + string (JSON)
2. Every transaction: `SUM(debit) = SUM(credit)`
3. Ledger entries: IMMUTABLE (never UPDATE or DELETE)
4. Tests first, then implementation (TDD)
5. JWT secret in environment, never committed

✅ **Best Practices**:
- Start simple (YAGNI)
- Input validation at boundaries
- Structured logging
- Graceful error handling
- Security by design

---

**Ready to start? Begin with User Story 4 (Authentication) following the TDD workflow above. Happy coding! 🚀**
