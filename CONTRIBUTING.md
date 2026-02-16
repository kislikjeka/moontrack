# Contributing to MoonTrack

## Development Workflow

### 1. Check Available Commands

```bash
# View all just commands
just
```

### 2. Quick Start Development

```bash
# First time setup
just setup           # Install dependencies
just up              # Start infrastructure
just migrate-up      # Apply database migrations

# Daily development
just dev             # Start backend + frontend servers
```

### 3. Before Committing

```bash
# Run quality checks
just check           # Format, lint, and test all code
```

## Test-First Development (TDD)

This project follows **strict TDD** - tests MUST be written before implementation.

### TDD Workflow (Red-Green-Refactor)

1. **RED**: Write a failing test
   ```bash
   # Create test file
   touch apps/backend/internal/core/user/service/user_service_test.go

   # Write test
   # Run test and verify it fails
   just backend-test
   ```

2. **GREEN**: Write minimal code to pass
   ```bash
   # Implement feature
   # Run test and verify it passes
   just backend-test
   ```

3. **REFACTOR**: Improve code while keeping tests green
   ```bash
   # Clean up code
   just backend-fmt
   just backend-test
   ```

## Architecture Guidelines

### Backend Structure

```
apps/backend/internal/
├── core/           # Core systems (foundation)
│   ├── ledger/    # Double-entry accounting
│   ├── user/      # Authentication & users
│   └── pricing/   # Price fetching & caching
├── modules/        # Business features
│   ├── wallet/
│   ├── portfolio/
│   └── manual_transaction/
├── shared/         # Utilities
└── api/            # HTTP layer
```

### Frontend Structure

```
apps/frontend/src/
├── components/     # Reusable UI components
├── features/       # Feature modules
│   ├── auth/
│   ├── dashboard/
│   ├── wallets/
│   └── transactions/
├── services/       # API clients
└── hooks/          # Custom React hooks
```

## Code Standards

### Backend (Go)

- **Financial amounts**: ALWAYS use `*big.Int`, NEVER `float64`
- **Database**: All amounts use `NUMERIC(78,0)`
- **Testing**: Use table-driven tests
- **Errors**: Wrap errors with context using `fmt.Errorf`
- **Format**: Run `just backend-fmt` before committing

### Frontend (React)

- **State**: Start with hooks, add Zustand only when needed
- **Server state**: Use TanStack Query
- **Testing**: React Testing Library
- **Format**: Run `just frontend-lint` before committing

## Database Migrations

### Creating Migrations

```bash
# Create new migration
just migrate-create add_new_table

# Edit migration files
# apps/backend/migrations/NNNNNN_add_new_table.up.sql
# apps/backend/migrations/NNNNNN_add_new_table.down.sql

# Apply migration
just migrate-up

# Test rollback
just migrate-down
just migrate-up
```

### Migration Guidelines

- **Never** modify existing migrations that have been applied
- **Always** create a new migration for schema changes
- **Test** both up and down migrations
- **Use transactions** where possible

## NON-NEGOTIABLE Rules

From `.specify/memory/constitution.md`:

### 1. Test-First Development
- ✅ Write test FIRST
- ✅ See it FAIL (RED)
- ✅ Write code to PASS (GREEN)
- ✅ REFACTOR while keeping tests green

### 2. Security by Design
- ✅ Never commit secrets (use `.env`)
- ✅ Validate all inputs
- ✅ Use prepared statements for SQL
- ✅ Hash passwords with bcrypt

### 3. Simplicity First
- ✅ Follow YAGNI (You Aren't Gonna Need It)
- ✅ Avoid premature optimization
- ✅ Keep it simple

### 4. Precision & Immutability
- ✅ Financial amounts: `NUMERIC(78,0)` + `*big.Int`
- ✅ NEVER use `float64` for money
- ✅ Ledger entries are IMMUTABLE (no UPDATE/DELETE)

### 5. Double-Entry Accounting
- ✅ Every transaction: `SUM(debit) = SUM(credit)`
- ✅ Verify balance in tests
- ✅ Account balance = SUM(entries)

### 6. Handler Registry Pattern
- ✅ Transaction types as handlers
- ✅ Implement `TransactionHandler` interface
- ✅ Register in handler registry
- ✅ Zero changes to ledger core

## Git Workflow

### Commit Messages

Follow conventional commits:

```bash
feat: add user registration endpoint
fix: correct balance calculation in ledger
test: add tests for wallet creation
docs: update API documentation
refactor: simplify transaction handler
```

### Using Just for Git

```bash
# Quick save progress
just git-save

# Commit with message
just git-commit "feat: add authentication middleware"

# Check status
just git-status
```

## Testing

### Backend Tests

```bash
# Run all tests
just backend-test

# Run specific package
cd apps/backend && go test ./internal/core/user/... -v

# With coverage
just backend-coverage
```

### Frontend Tests

```bash
# Run all tests
just frontend-test

# With coverage
just frontend-coverage

# Watch mode
cd apps/frontend && bun test -- --watch
```

## Documentation

### Required Documentation

When adding new features:

1. **Update tasks.md**: Mark tasks as completed
2. **API Contract**: Update `contracts/openapi.yaml`
3. **Code Comments**: Document complex logic
4. **README**: Update if adding new setup steps

### Documentation Locations

- **Feature specs**: `specs/001-portfolio-tracker/`
- **API docs**: `specs/001-portfolio-tracker/contracts/`
- **Architecture**: `.specify/memory/constitution.md`
- **Setup**: `README.md` and `specs/001-portfolio-tracker/quickstart.md`

## Troubleshooting

### Database Issues

```bash
# Reset database
just db-reset

# Check migration status
just migrate-version

# View database logs
just logs
```

### Build Issues

```bash
# Clean all build artifacts
just clean-all

# Reinstall dependencies
just setup
```

### Infrastructure Issues

```bash
# Restart Docker services
just restart

# View logs
just logs

# Check status
just status
```

## Getting Help

1. Check `README.md` for setup instructions
2. Review `specs/001-portfolio-tracker/quickstart.md` for detailed workflows
3. Read `.specify/memory/constitution.md` for architectural principles
4. Use `just docs` to see all documentation paths
5. Run `just verify` to check your environment setup

## Code Review Checklist

Before submitting code:

- [ ] Tests written FIRST and passing
- [ ] Code formatted (`just fmt`)
- [ ] Code linted (`just lint`)
- [ ] No `float64` for financial amounts
- [ ] Ledger transactions balance
- [ ] No secrets committed
- [ ] Documentation updated
- [ ] Migrations tested (up and down)

Run the full check:

```bash
just check
```

## Performance Guidelines

- **API Response Time**: Target <200ms p95 (constitution requirement)
- **Database Queries**: Use indexes for common queries
- **Caching**: Use Redis for price data (60s TTL)
- **Frontend**: Code splitting for large components

## Questions?

Run `just docs` to see all available documentation.
