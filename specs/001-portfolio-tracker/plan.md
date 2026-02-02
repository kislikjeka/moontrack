# Implementation Plan: Crypto Portfolio Tracker

**Branch**: `001-portfolio-tracker` | **Date**: 2026-01-06 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/001-portfolio-tracker/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command. See `.specify/templates/commands/plan.md` for the execution workflow.

## Summary

Build a web-based cryptocurrency portfolio tracker that allows users to register accounts, create wallets on different blockchain networks, manually track asset holdings, and record income/outcome transactions. The system will display real-time USD valuations, total portfolio balance, and transaction history through a simple web interface backed by a REST API with JWT authentication. The backend will implement ledger functionality using double-entry accounting principles to ensure accurate balance tracking.

## Technical Context

**Language/Version**:
- Backend: Golang 1.21+ (per constitution)
- Frontend: React 18+ (per constitution)

**Primary Dependencies**:
- Backend:
  - HTTP Router: chi v5 (github.com/go-chi/chi/v5) - stdlib-compatible, lightweight
  - JWT Auth: golang-jwt/jwt v5 - security-focused with CVE-2025-30204 fixes
  - Migrations: golang-migrate/migrate v4 - production-proven with database locking
  - PostgreSQL Driver: pgx
- Frontend:
  - State Management: React Hooks (useState, useContext) + Zustand (when needed)
  - Routing: React Router v6 - battle-tested, feature-complete
  - API Client: TanStack Query + axios - server state separation with caching
- Price API: CoinGecko API (Free Demo Plan) - 30 calls/min, historical data access

**Storage**: PostgreSQL 14+ with NUMERIC(78,0) for financial amounts (per constitution)

**Testing**:
- Backend: Go standard testing package with table-driven tests (per constitution)
- Frontend: React Testing Library (per constitution)
- Target: 80% backend coverage, 70% frontend coverage (per constitution)

**Target Platform**: Web application (Linux server + modern browsers: Chrome 90+, Firefox 88+, Safari 14+, Edge 90+)

**Project Type**: Web application with separate backend/frontend (per constitution structure)

**Performance Goals**:
- API response time: <500ms under normal load (per feature spec SC-005)
- Balance update latency: <2 seconds (per feature spec SC-002)
- Expected concurrent users: 100-1,000 users (MVP target)
- Price API: 30 calls/min limit, supports 50-100 active users with caching

**Constraints**:
- API endpoints must respond within 200ms p95 (per constitution)
- Must handle uint256 amounts (up to 2^256-1) for blockchain compatibility (per constitution)
- Stateless API design (per constitution)
- Browser compatibility: Modern browsers with ES6+ support
- Mobile responsiveness: Responsive CSS design (no separate mobile app)

**Scale/Scope**:
- Initial MVP targets: 100-1,000 users, 50 wallets per user, 1,000 transactions per user
- Supported cryptocurrencies: Top 100 by market cap (expandable via CoinGecko's 13,000+ coverage)
- Database optimized for these scales with appropriate indexes

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

### I. Test-First Development ✅
- **Status**: PASS
- **Verification**: Feature spec includes detailed acceptance scenarios for all user stories (US-001 through US-004)
- **Action**: Tests must be written before implementation code for each user story

### II. Security by Design ✅
- **Status**: PASS
- **Requirements in Spec**:
  - FR-002: JWT authentication for session management
  - FR-013: API endpoints secured with JWT
  - FR-014: Input validation on client and server
- **Action**: Security review required for auth implementation, secrets management, and input validation

### III. Simplicity First ✅
- **Status**: PASS
- **Rationale**: Feature spec explicitly states "basic functionality", "simple web interface", and manual transaction entry (no blockchain integration). Aligns with YAGNI principle.
- **Action**: Resist adding features beyond spec (e.g., multi-currency support, blockchain integration, password recovery)

### IV. Precision & Immutability (NON-NEGOTIABLE) ✅
- **Status**: PASS
- **Requirements**:
  - All financial amounts use `math/big.Int` in Go code (NEVER float64 or shopspring/decimal)
  - Database schema uses `NUMERIC(78,0)` for all amount columns
  - Store amounts in base units (wei, satoshi, lamports)
  - Historical amounts are immutable
- **Verification Point**: Phase 1 data model design, Phase 2 implementation review
- **Quality Gate**: grep codebase for `float64.*amount|shopspring` before merge

### V. Double-Entry Accounting (NON-NEGOTIABLE) ✅
- **Status**: PASS
- **Requirements**:
  - FR-011: Ledger functionality for accurate balance calculations
  - Every transaction creates balanced ledger entries (debit = credit)
  - Verify `SUM(DEBIT) = SUM(CREDIT)` before commit
  - Account balances must match ledger sum
- **Verification Point**: Phase 1 ledger design, mandatory balance tests in Phase 2
- **Quality Gate**: All ledger operations have tests verifying balance invariant

### VI. Handler Registry Pattern (NON-NEGOTIABLE) ✅
- **Status**: PASS
- **Requirements**:
  - Transaction types (income, outcome, manual asset adjustments) must use handler pattern
  - Each transaction type module under `internal/modules/`
  - Handlers implement `TransactionHandler[T]` interface
  - Zero changes to ledger core when adding transaction types
- **Verification Point**: Phase 1 architecture design
- **Quality Gate**: New transaction types must not modify ledger core code

### Pre-Implementation Quality Gates
- [x] Feature specification approved (provided)
- [x] Test scenarios defined and approved (in spec.md)
- [x] Security implications reviewed (JWT auth, input validation, secrets management documented in research.md and quickstart.md)
- [x] Complexity justified (simple/basic per spec)
- [x] Ledger design reviewed, double-entry verified (data-model.md defines complete double-entry system with invariant tests)

### Phase 0 Research Completed ✅

All clarification items resolved. See [research.md](./research.md) for detailed rationale:

1. ✅ HTTP router: chi v5
2. ✅ JWT library: golang-jwt/jwt v5
3. ✅ Database migrations: golang-migrate/migrate v4
4. ✅ State management: React Hooks + Zustand (when needed)
5. ✅ Routing: React Router v6
6. ✅ API client: TanStack Query + axios
7. ✅ Price API: CoinGecko (Free Demo Plan)
8. ✅ Browser compatibility: Modern browsers (Chrome 90+, Firefox 88+, Safari 14+, Edge 90+)
9. ✅ Mobile responsiveness: Responsive CSS
10. ✅ Scale targets: 100-1K users, 50 wallets/user, 1K transactions/user

## Project Structure

### Documentation (this feature)

```text
specs/[###-feature]/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
moontrack/
├── apps/
│   ├── backend/                    # Golang modular monolith
│   │   ├── cmd/
│   │   │   └── api/               # Main API server entrypoint
│   │   │       └── main.go
│   │   ├── internal/
│   │   │   ├── core/              # Core systems (foundational infrastructure)
│   │   │   │   ├── ledger/        # Double-entry ledger system (constitution-mandated)
│   │   │   │   │   ├── domain/    # Entry, Transaction, Journal, Balance entities
│   │   │   │   │   ├── handler/   # Handler registry and interface
│   │   │   │   │   ├── service/   # LedgerService, AccountResolver, Validator, Committer
│   │   │   │   │   ├── repository/ # Repository interfaces
│   │   │   │   │   └── postgres/  # PostgreSQL implementations
│   │   │   │   ├── user/          # User identity and authentication
│   │   │   │   │   ├── domain/    # User entity
│   │   │   │   │   ├── service/   # UserService (registration, login)
│   │   │   │   │   ├── repository/ # UserRepository interface + postgres impl
│   │   │   │   │   └── auth/      # JWT generation, validation, middleware
│   │   │   │   └── pricing/       # Price fetching and caching
│   │   │   │       ├── coingecko/ # CoinGecko API client
│   │   │   │       ├── cache/     # Redis caching layer
│   │   │   │       └── service/   # PriceService (orchestrates API + cache)
│   │   │   ├── modules/           # Domain modules (business features)
│   │   │   │   ├── manual_transaction/    # Manual income/outcome transactions
│   │   │   │   │   ├── domain/            # ManualTransaction struct
│   │   │   │   │   └── handler/           # ManualTransactionHandler
│   │   │   │   ├── asset_adjustment/      # Manual asset balance adjustments
│   │   │   │   │   ├── domain/
│   │   │   │   │   └── handler/
│   │   │   │   ├── wallet/                # Wallet management
│   │   │   │   │   ├── domain/            # Wallet entity
│   │   │   │   │   ├── service/           # WalletService
│   │   │   │   │   └── repository/        # WalletRepository
│   │   │   │   └── portfolio/             # Portfolio aggregation and views
│   │   │   │       └── service/           # PortfolioService (aggregates from ledger)
│   │   │   ├── shared/            # Shared utilities
│   │   │   │   ├── config/        # Configuration management
│   │   │   │   ├── logger/        # Structured logging
│   │   │   │   ├── database/      # Database connection pool
│   │   │   │   └── errors/        # Error handling utilities
│   │   │   └── api/               # HTTP layer
│   │   │       ├── handlers/      # HTTP handlers per module
│   │   │       ├── middleware/    # HTTP middleware (CORS, logging, recovery)
│   │   │       └── router/        # Route registration
│   │   ├── pkg/                   # Public libraries (if needed)
│   │   ├── migrations/            # SQL migrations
│   │   └── tests/
│   │       ├── contract/          # API contract tests
│   │       ├── integration/       # Module integration tests
│   │       └── unit/              # Unit tests (alongside source files)
│   └── frontend/                  # React application
│       ├── public/
│       ├── src/
│       │   ├── components/        # Reusable UI components
│       │   │   ├── common/        # Buttons, inputs, cards, etc.
│       │   │   └── layout/        # Header, sidebar, layout wrappers
│       │   ├── features/          # Feature-based modules
│       │   │   ├── auth/          # Login, registration forms
│       │   │   ├── dashboard/     # Portfolio dashboard, total balance
│       │   │   ├── wallets/       # Wallet creation, wallet detail views
│       │   │   └── transactions/  # Add transaction form, transaction history
│       │   ├── services/          # API clients and external services
│       │   │   ├── api.ts         # HTTP client configuration
│       │   │   ├── auth.ts        # Authentication service
│       │   │   └── portfolio.ts   # Portfolio/wallet API calls
│       │   ├── hooks/             # Custom React hooks
│       │   ├── utils/             # Utility functions
│       │   ├── types/             # TypeScript type definitions
│       │   ├── App.tsx
│       │   └── main.tsx
│       └── tests/
│           └── components/        # Component tests
└── .specify/                      # Project documentation (existing)
```

**Structure Decision**: Web application structure following MoonTrack constitution standards. Backend uses modular monolith organized into three layers:

1. **Core systems** (`internal/core/`): Foundational infrastructure used across all features
   - `ledger/`: Double-entry accounting system (constitution-mandated)
   - `user/`: User identity, authentication, authorization
   - `pricing/`: Price fetching and caching (CoinGecko + Redis)

2. **Domain modules** (`internal/modules/`): Business features and transaction types
   - `manual_transaction/`: Income/outcome transaction handlers
   - `asset_adjustment/`: Balance adjustment handlers
   - `wallet/`: Wallet management
   - `portfolio/`: Portfolio aggregation and views

3. **Infrastructure** (`internal/shared/`, `internal/api/`): Cross-cutting concerns
   - Configuration, logging, database, errors
   - HTTP handlers, middleware, routing

Frontend follows feature-based organization for scalability.

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

No constitutional violations. Feature aligns with Simplicity First principle - building only what is explicitly specified in the feature spec.

---

## Phase 1 Design Complete ✅

**Date**: 2026-01-06

### Artifacts Generated

1. ✅ **research.md**: Technology stack decisions with rationale
2. ✅ **data-model.md**: Complete database schema and entity definitions
3. ✅ **contracts/openapi.yaml**: REST API specification
4. ✅ **quickstart.md**: Development environment setup guide

### Constitution Re-Evaluation (Post-Design)

All NON-NEGOTIABLE principles verified in design artifacts:

#### I. Test-First Development ✅
- **Status**: PASS
- **Evidence**: quickstart.md includes TDD workflow with Red-Green-Refactor examples
- **Action Required**: Follow TDD in implementation phase

#### II. Security by Design ✅
- **Status**: PASS
- **Evidence**:
  - JWT authentication flow defined in openapi.yaml
  - Password hashing (bcrypt) specified in data-model.md
  - Input validation rules defined for all entities
  - Environment variable usage for secrets documented in quickstart.md
- **Action Required**: Implement security checklist from quickstart.md

#### III. Simplicity First ✅
- **Status**: PASS
- **Evidence**:
  - Libraries chosen for minimal complexity (chi over gin, React Hooks over Redux)
  - Only features from spec.md included
  - No premature abstractions
- **Maintained**: Zero scope creep

#### IV. Precision & Immutability ✅
- **Status**: PASS
- **Evidence**:
  - All amount columns use NUMERIC(78,0) in schema
  - data-model.md mandates *big.Int in Go code
  - Entries table has no UPDATE operations
  - JSON responses use strings for amounts
- **Quality Gates**: grep verification commands in quickstart.md

#### V. Double-Entry Accounting ✅
- **Status**: PASS
- **Evidence**:
  - Complete ledger system designed (accounts, transactions, entries)
  - Transaction types generate balanced entries
  - Invariant tests specified in data-model.md
  - Account balances reconcile from ledger
- **Quality Gates**: Balance verification tests required for all transaction types

#### VI. Handler Registry Pattern ✅
- **Status**: PASS
- **Evidence**:
  - Transaction handler modules defined (manual_transaction, asset_adjustment)
  - Each module isolated under internal/modules/
  - Handler interface pattern documented
  - Extensible design (add types without modifying ledger core)

### Design Changes After Review

**Change 1**: Removed `manual_price_overrides` table (Simplicity First principle)

**Rationale**:
- FR-008 "manually override prices" satisfied by optional `usd_rate` field in transaction creation
- FR-017 reinterpreted: "view price source" (manual vs API) shown in transaction metadata, not "revert override"
- Aligns with Immutability principle: once price recorded in ledger entry, it's permanent
- Use case: obscure tokens without CoinGecko price → user specifies price manually when creating transaction
- Simpler implementation: no additional table, no temporal logic, no state management

**Impact**:
- ✅ Reduced complexity (one less table)
- ✅ Better immutability compliance
- ✅ Clearer audit trail (price_source in raw_data JSONB)
- ✅ All functional requirements still met

---

**Change 2**: Reorganized backend structure into `core/` and `modules/` (Architecture clarity)

**Rationale**:
- **Core systems** (`internal/core/`): Foundational infrastructure used everywhere
  - `ledger/`: Double-entry accounting (constitution-mandated core)
  - `user/`: Identity, authentication, authorization (every feature needs users)
  - `pricing/`: Price fetching and caching (shared service)
- **Domain modules** (`internal/modules/`): Business features that USE core systems
  - `manual_transaction/`, `asset_adjustment/`: Transaction type handlers
  - `wallet/`: Wallet management business logic
  - `portfolio/`: Portfolio aggregation views
- **Separation of concerns**: Core = "how the system works", Modules = "what the system does"

**Impact**:
- ✅ Clearer architectural boundaries (core vs features)
- ✅ Easier to understand dependencies (modules depend on core, not vice versa)
- ✅ Better testability (mock core systems for module tests)
- ✅ Aligns with constitution: ledger + user + pricing are fundamental, not features

### Ready for Implementation

All design gates passed. Proceed to implementation using `/speckit.tasks` to generate tasks.md.

**Next Command**: `/speckit.tasks` (generates actionable task list from plan and data model)
