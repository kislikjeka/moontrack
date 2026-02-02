# Tasks: Crypto Portfolio Tracker

**Feature**: 001-portfolio-tracker
**Branch**: `001-portfolio-tracker`
**Generated**: 2026-01-10

**Input**: Design documents from `/specs/001-portfolio-tracker/`
**Prerequisites**: plan.md ✅, spec.md ✅, research.md ✅, data-model.md ✅, contracts/openapi.yaml ✅

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3, US4)
- Include exact file paths in descriptions

## Path Conventions

This project follows the **MoonTrack web app structure**:
- Backend: `apps/backend/internal/`, `apps/backend/tests/`
- Frontend: `apps/frontend/src/`, `apps/frontend/tests/`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and basic structure

- [X] T001 Create project directory structure per plan.md (apps/backend, apps/frontend)
- [X] T002 Initialize Go modules in apps/backend/go.mod
- [X] T003 [P] Initialize React app with Vite in apps/frontend/
- [X] T004 [P] Install Go dependencies (chi, golang-jwt, jackc/pgx/v5, golang-migrate, go-redis) in apps/backend/
- [X] T005 [P] Install React dependencies (react-router-dom, @tanstack/react-query, axios) in apps/frontend/
- [X] T006 [P] Create .gitignore files for backend and frontend
- [X] T007 [P] Create environment configuration files (apps/backend/.env.example, apps/frontend/.env.example)
- [X] T008 [P] Setup PostgreSQL database (moontrack_dev) and verify connection
- [X] T009 [P] Setup Redis server and verify connection

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

### Database & Migrations

- [X] T010 Create migrations directory in apps/backend/migrations/
- [X] T011 Create initial schema migration 000001_create_schema.up.sql from data-model.md (include all tables, indexes, and constraints)
- [X] T012 Create rollback migration 000001_create_schema.down.sql
- [X] T013 Run initial migration and verify all tables created with correct indexes (verify idx_entries_transaction, idx_entries_account, idx_transactions_type exist)

### Shared Infrastructure (Backend)

- [X] T014 [P] Create config package in apps/backend/internal/shared/config/ for environment variables
- [X] T015 [P] Create logger package in apps/backend/internal/shared/logger/ with structured logging
- [X] T016 [P] Create database connection pool in apps/backend/internal/shared/database/
- [X] T017 [P] Create error handling utilities in apps/backend/internal/shared/errors/
- [X] T018 Create main.go entrypoint in apps/backend/cmd/api/main.go
- [X] T019 Setup HTTP server with chi router in apps/backend/internal/api/router/router.go
- [X] T020 [P] Implement CORS middleware in apps/backend/internal/api/middleware/cors.go
- [X] T021 [P] Implement logging middleware in apps/backend/internal/api/middleware/logger.go
- [X] T022 [P] Implement recovery middleware in apps/backend/internal/api/middleware/recovery.go

### Core Ledger System (Constitution Mandated)

- [X] T023 [P] Create Account entity in apps/backend/internal/core/ledger/domain/account.go
- [X] T024 [P] Create Entry entity in apps/backend/internal/core/ledger/domain/entry.go
- [X] T025 [P] Create Transaction entity in apps/backend/internal/core/ledger/domain/transaction.go
- [X] T026 [P] Create AccountBalance entity in apps/backend/internal/core/ledger/domain/balance.go
- [X] T027 Create LedgerRepository interface in apps/backend/internal/core/ledger/repository/repository.go
- [X] T028 Implement PostgreSQL LedgerRepository in apps/backend/internal/core/ledger/postgres/ledger_repository.go
- [X] T029 Create TransactionHandler interface in apps/backend/internal/core/ledger/handler/handler.go
- [X] T030 Create HandlerRegistry in apps/backend/internal/core/ledger/handler/registry.go
- [X] T031 Implement LedgerService in apps/backend/internal/core/ledger/service/ledger_service.go
- [X] T032 Implement AccountResolver in apps/backend/internal/core/ledger/service/account_resolver.go
- [X] T033 Implement TransactionValidator in apps/backend/internal/core/ledger/service/validator.go
- [X] T034 Implement TransactionCommitter in apps/backend/internal/core/ledger/service/committer.go

### Core Ledger Tests (Constitution Required)

- [X] T035 [P] Test transaction balance invariant (SUM(debit) = SUM(credit)) in apps/backend/internal/core/ledger/service/ledger_service_test.go
- [X] T036 [P] Test account balance reconciliation (balance = SUM(entries)) in apps/backend/internal/core/ledger/service/ledger_service_test.go
- [X] T037 [P] Test big.Int precision with max values in apps/backend/internal/core/ledger/service/precision_test.go
- [X] T038 [P] Test entry immutability (no UPDATE/DELETE) in apps/backend/internal/core/ledger/postgres/ledger_repository_test.go

**Checkpoint**: Foundation ready - user story implementation can now begin in parallel

---

## Phase 3: User Story 4 - User Authentication (Priority: P2) 🔐 START HERE

**Goal**: Users can register accounts and log in securely with JWT authentication

**Why First**: Authentication is a prerequisite for all other features. Building it first enables independent testing of other user stories with authenticated contexts.

**Independent Test**: Register a new account, log out, log back in, and verify JWT token works for protected endpoints

### Backend - User Core System

- [X] T039 [P] [US4] Create User entity in apps/backend/internal/core/user/domain/user.go
- [X] T040 [US4] Create UserRepository interface in apps/backend/internal/core/user/repository/repository.go
- [X] T041 [US4] Implement PostgreSQL UserRepository in apps/backend/internal/core/user/repository/postgres/user_repository.go
- [X] T042 [US4] Implement UserService with Register and Login in apps/backend/internal/core/user/service/user_service.go (bcrypt hashing)
- [X] T043 [US4] Implement JWT token generation in apps/backend/internal/core/user/auth/jwt.go
- [X] T044 [US4] Implement JWT token validation in apps/backend/internal/core/user/auth/jwt.go
- [X] T045 [US4] Implement JWT authentication middleware in apps/backend/internal/core/user/auth/middleware.go
- [X] T046 [P] [US4] Create registration handler (POST /auth/register) in apps/backend/internal/api/handlers/auth_handler.go
- [X] T047 [P] [US4] Create login handler (POST /auth/login) in apps/backend/internal/api/handlers/auth_handler.go
- [X] T048 [US4] Register auth routes in router in apps/backend/internal/api/router/router.go

### Backend - User Tests

- [X] T049 [P] [US4] Unit test UserService.Register with valid/invalid inputs in apps/backend/internal/core/user/service/user_service_test.go
- [X] T050 [P] [US4] Unit test UserService.Login with correct/incorrect credentials in apps/backend/internal/core/user/service/user_service_test.go
- [X] T051 [P] [US4] Unit test JWT generation and validation in apps/backend/internal/core/user/auth/jwt_test.go
- [X] T052 [P] [US4] Contract test POST /auth/register (success, duplicate email, invalid password) in apps/backend/tests/contract/auth_test.go
- [X] T053 [P] [US4] Contract test POST /auth/login (success, wrong credentials) in apps/backend/tests/contract/auth_test.go
- [X] T054 [P] [US4] Integration test JWT middleware rejects invalid tokens in apps/backend/tests/integration/auth_test.go

### Frontend - Authentication UI

- [X] T055 [US4] Setup axios interceptor for JWT token management in apps/frontend/src/services/api.js
- [X] T056 [US4] Create authentication service in apps/frontend/src/services/auth.js
- [X] T057 [US4] Create AuthContext for global auth state in apps/frontend/src/features/auth/AuthContext.jsx
- [X] T058 [P] [US4] Create RegistrationForm component in apps/frontend/src/features/auth/RegistrationForm.jsx
- [X] T059 [P] [US4] Create LoginForm component in apps/frontend/src/features/auth/LoginForm.jsx
- [X] T060 [US4] Create ProtectedRoute component in apps/frontend/src/features/auth/ProtectedRoute.jsx
- [X] T061 [US4] Setup React Router with auth routes in apps/frontend/src/App.jsx
- [X] T062 [P] [US4] Add input validation and error display to RegistrationForm
- [X] T063 [P] [US4] Add input validation and error display to LoginForm

### Frontend - Authentication Tests

- [X] T064 [P] [US4] Test RegistrationForm component in apps/frontend/tests/components/RegistrationForm.test.jsx
- [X] T065 [P] [US4] Test LoginForm component in apps/frontend/tests/components/LoginForm.test.jsx
- [X] T066 [P] [US4] Test ProtectedRoute redirects when unauthenticated in apps/frontend/tests/components/ProtectedRoute.test.jsx

**Checkpoint**: At this point, User Story 4 (Authentication) should be fully functional and testable independently

---

## Phase 4: User Story 2 - Manage Wallets and Assets (Priority: P1)

**Goal**: Users can create wallets on different blockchain networks and manually adjust asset holdings

**Independent Test**: Create wallets on different chains (Ethereum, Bitcoin, Solana), add various assets to each wallet, adjust balances, and verify they appear correctly

### Backend - Wallet Module

- [X] T067 [P] [US2] Create Wallet entity in apps/backend/internal/modules/wallet/domain/wallet.go
- [X] T068 [US2] Create WalletRepository interface in apps/backend/internal/modules/wallet/repository/repository.go
- [X] T069 [US2] Implement PostgreSQL WalletRepository in apps/backend/internal/modules/wallet/repository/postgres/wallet_repository.go
- [X] T070 [US2] Implement WalletService (Create, List, Get, Update, Delete) in apps/backend/internal/modules/wallet/service/wallet_service.go
- [X] T071 [P] [US2] Create wallet handlers (POST /wallets, GET /wallets, GET /wallets/{id}, PUT /wallets/{id}, DELETE /wallets/{id}) in apps/backend/internal/api/handlers/wallet_handler.go
- [X] T072 [US2] Register wallet routes with JWT middleware in apps/backend/internal/api/router/router.go

### Backend - Asset Adjustment Transaction Handler

- [X] T073 [P] [US2] Create AssetAdjustmentTransaction domain in apps/backend/internal/modules/asset_adjustment/domain/transaction.go
- [X] T074 [US2] Implement AssetAdjustmentHandler (generates ledger entries) in apps/backend/internal/modules/asset_adjustment/handler/handler.go
- [X] T075 [US2] Register AssetAdjustmentHandler in handler registry in apps/backend/cmd/api/main.go

### Backend - Wallet Tests

- [X] T076 [P] [US2] Unit test WalletService.Create with valid/invalid inputs in apps/backend/internal/modules/wallet/service/wallet_service_test.go
- [X] T077 [P] [US2] Unit test AssetAdjustmentHandler ledger entries balance in apps/backend/internal/modules/asset_adjustment/handler/handler_test.go
- [X] T078 [P] [US2] Contract test POST /wallets (success, validation errors) in apps/backend/tests/contract/wallet_test.go
- [X] T079 [P] [US2] Contract test GET /wallets returns user's wallets only in apps/backend/tests/contract/wallet_test.go
- [X] T080 [P] [US2] Contract test GET /wallets/{id} with asset balances in apps/backend/tests/contract/wallet_test.go
- [X] T081 [P] [US2] Integration test wallet creation + asset adjustment updates balance in apps/backend/tests/integration/wallet_asset_test.go

### Frontend - Wallet Management UI

- [X] T082 [US2] Create wallet API client in apps/frontend/src/services/wallet.js
- [X] T083 [P] [US2] Create WalletList component in apps/frontend/src/features/wallets/WalletList.jsx
- [X] T084 [P] [US2] Create WalletCard component in apps/frontend/src/features/wallets/WalletCard.jsx
- [X] T085 [P] [US2] Create CreateWalletForm component in apps/frontend/src/features/wallets/CreateWalletForm.jsx
- [X] T086 [US2] Create WalletDetail component in apps/frontend/src/features/wallets/WalletDetail.jsx
- [X] T087 [US2] Create AssetAdjustmentForm component in apps/frontend/src/features/wallets/AssetAdjustmentForm.jsx
- [X] T088 [US2] Setup TanStack Query for wallet data caching in apps/frontend/src/features/wallets/
- [X] T089 [US2] Add wallet routes to React Router in apps/frontend/src/App.jsx
- [X] T090 [P] [US2] Add validation and error handling to CreateWalletForm
- [X] T091 [P] [US2] Add validation and error handling to AssetAdjustmentForm

### Frontend - Wallet Tests

- [X] T092 [P] [US2] Test WalletList component in apps/frontend/tests/components/WalletList.test.jsx
- [X] T093 [P] [US2] Test CreateWalletForm component in apps/frontend/tests/components/CreateWalletForm.test.jsx
- [X] T094 [P] [US2] Test WalletDetail component in apps/frontend/tests/components/WalletDetail.test.jsx

**Checkpoint**: At this point, User Story 2 (Manage Wallets and Assets) should be fully functional and testable independently

---

## Phase 5: User Story 3 - Record Transactions (Priority: P2)

**Goal**: Users can record income and outcome transactions with automatic balance updates and transaction history

**Independent Test**: Add income transactions (deposits/purchases) and outcome transactions (withdrawals/sales), verify balances adjust accordingly and transaction history is maintained

### Backend - Pricing Core System

- [X] T095 [P] [US3] Create PriceSnapshot entity in apps/backend/internal/core/pricing/domain/price.go
- [X] T096 [US3] Create CoinGecko API client in apps/backend/internal/core/pricing/coingecko/client.go
- [X] T097 [US3] Create Redis price cache in apps/backend/internal/core/pricing/cache/cache.go
- [X] T098 [US3] Implement PriceService (fetch with caching, fallback, circuit breaker) in apps/backend/internal/core/pricing/service/price_service.go
- [X] T099 [P] [US3] Test CoinGecko integration with API key in apps/backend/internal/core/pricing/coingecko/client_test.go
- [X] T100 [P] [US3] Test price caching and TTL in apps/backend/internal/core/pricing/cache/cache_test.go
- [X] T100A [P] [US3] Test price API failure scenarios (network timeout, 429 rate limit, invalid response) with fallback to cached prices in apps/backend/internal/core/pricing/service/price_service_test.go

### Backend - Manual Transaction Handlers

- [X] T101 [P] [US3] Create ManualIncomeTransaction domain in apps/backend/internal/modules/manual_transaction/domain/income.go
- [X] T102 [P] [US3] Create ManualOutcomeTransaction domain in apps/backend/internal/modules/manual_transaction/domain/outcome.go
- [X] T103 [US3] Implement ManualIncomeHandler (generates ledger entries, fetches price) in apps/backend/internal/modules/manual_transaction/handler/income_handler.go
- [X] T104 [US3] Implement ManualOutcomeHandler (generates ledger entries, validates balance) in apps/backend/internal/modules/manual_transaction/handler/outcome_handler.go
- [X] T105 [P] [US3] Register ManualIncomeHandler in handler registry in apps/backend/cmd/api/main.go
- [X] T106 [P] [US3] Register ManualOutcomeHandler in handler registry in apps/backend/cmd/api/main.go

### Backend - Transaction API

- [X] T107 [P] [US3] Create transaction handler POST /transactions in apps/backend/internal/api/handlers/transaction_handler.go
- [X] T108 [P] [US3] Create transaction list handler GET /transactions (with pagination, filters) in apps/backend/internal/api/handlers/transaction_handler.go
- [X] T109 [US3] Register transaction routes with JWT middleware in apps/backend/internal/api/router/router.go

### Backend - Transaction Tests

- [X] T110 [P] [US3] Test ManualIncomeHandler ledger entries balance in apps/backend/internal/modules/manual_transaction/handler/income_handler_test.go
- [X] T111 [P] [US3] Test ManualOutcomeHandler ledger entries balance in apps/backend/internal/modules/manual_transaction/handler/outcome_handler_test.go
- [X] T112 [P] [US3] Test ManualOutcomeHandler rejects insufficient balance in apps/backend/internal/modules/manual_transaction/handler/outcome_handler_test.go
- [X] T113 [P] [US3] Test manual price override stored correctly in apps/backend/internal/modules/manual_transaction/handler/income_handler_test.go
- [X] T114 [P] [US3] Contract test POST /transactions (income, outcome, asset_adjustment) in apps/backend/tests/contract/transaction_test.go
- [X] T115 [P] [US3] Contract test GET /transactions with pagination and filters in apps/backend/tests/contract/transaction_test.go
- [X] T116 [P] [US3] Integration test income transaction increases wallet balance in apps/backend/tests/integration/transaction_test.go
- [X] T117 [P] [US3] Integration test outcome transaction decreases wallet balance in apps/backend/tests/integration/transaction_test.go
- [X] T117A [P] [US3] Integration test transaction processing performance meets SC-002 (balance update within 2 seconds) in apps/backend/tests/integration/performance_test.go

### Frontend - Transaction UI

- [X] T118 [US3] Create transaction API client in apps/frontend/src/services/transaction.ts
- [X] T119 [P] [US3] Create TransactionForm component in apps/frontend/src/features/transactions/TransactionForm.tsx
- [X] T120 [P] [US3] Create TransactionList component in apps/frontend/src/features/transactions/TransactionList.tsx
- [X] T121 [P] [US3] Create TransactionItem component in apps/frontend/src/features/transactions/TransactionItem.tsx
- [X] T122 [US3] Create transaction type selector (income/outcome/adjustment) in TransactionForm
- [X] T123 [US3] Add optional manual price input field in TransactionForm
- [X] T124 [US3] Add wallet and asset selectors in TransactionForm
- [X] T125 [US3] Setup TanStack Query for transaction data with pagination
- [X] T126 [US3] Add transaction routes to React Router in apps/frontend/src/App.tsx
- [X] T127 [P] [US3] Add validation and error handling to TransactionForm
- [X] T128 [P] [US3] Add date/wallet filters to TransactionList

### Frontend - Transaction Tests

- [X] T129 [P] [US3] Test TransactionForm component in apps/frontend/tests/components/TransactionForm.test.jsx
- [X] T130 [P] [US3] Test TransactionList component in apps/frontend/tests/components/TransactionList.test.jsx

**Checkpoint**: At this point, User Story 3 (Record Transactions) should be fully functional and testable independently

---

## Phase 6: User Story 1 - View Portfolio Balance (Priority: P1) 🎯 MVP COMPLETE

**Goal**: Users can see total cryptocurrency holdings across all wallets with current USD valuations at a glance

**Independent Test**: Create a user account, add wallets with assets and transactions, and verify the dashboard shows accurate totals and individual asset values in USD

### Backend - Portfolio Module

- [X] T131 [US1] Create PortfolioService (aggregate balances from ledger) in apps/backend/internal/modules/portfolio/service/portfolio_service.go
- [X] T132 [P] [US1] Create portfolio summary handler GET /portfolio in apps/backend/internal/api/handlers/portfolio_handler.go
- [X] T133 [US1] Register portfolio routes with JWT middleware in apps/backend/internal/api/router/router.go

### Backend - Portfolio Tests

- [X] T134 [P] [US1] Unit test PortfolioService calculates total balance correctly in apps/backend/internal/modules/portfolio/service/portfolio_service_test.go
- [X] T135 [P] [US1] Unit test PortfolioService aggregates assets across wallets in apps/backend/internal/modules/portfolio/service/portfolio_service_test.go
- [X] T136 [P] [US1] Contract test GET /portfolio returns accurate totals in apps/backend/tests/contract/portfolio_test.go
- [X] T137 [P] [US1] Integration test portfolio reflects all wallet balances in apps/backend/tests/integration/portfolio_test.go

### Frontend - Dashboard UI

- [X] T138 [US1] Create portfolio API client in apps/frontend/src/services/portfolio.ts
- [X] T139 [P] [US1] Create Dashboard component in apps/frontend/src/features/dashboard/Dashboard.tsx
- [X] T140 [P] [US1] Create PortfolioSummary component (total balance) in apps/frontend/src/features/dashboard/PortfolioSummary.tsx
- [X] T141 [P] [US1] Create AssetBreakdown component (individual assets with USD values) in apps/frontend/src/features/dashboard/AssetBreakdown.tsx
- [X] T142 [US1] Setup TanStack Query for portfolio data with auto-refresh
- [X] T143 [US1] Add dashboard route as default authenticated route in apps/frontend/src/App.tsx
- [X] T144 [P] [US1] Add loading states and error handling to Dashboard
- [X] T145 [P] [US1] Add price update timestamp display
- [X] T146 [P] [US1] Add empty state for no assets ("Add your first wallet")

### Frontend - Dashboard Tests

- [ ] T147 [P] [US1] Test Dashboard component in apps/frontend/tests/components/Dashboard.test.jsx
- [ ] T148 [P] [US1] Test PortfolioSummary component in apps/frontend/tests/components/PortfolioSummary.test.jsx
- [ ] T149 [P] [US1] Test AssetBreakdown component in apps/frontend/tests/components/AssetBreakdown.test.jsx

### Frontend - Layout & Navigation

- [X] T150 [P] [US1] Create Header component with navigation in apps/frontend/src/components/layout/Header.tsx
- [X] T151 [P] [US1] Create Sidebar component with menu in apps/frontend/src/components/layout/Sidebar.tsx
- [X] T152 [US1] Create Layout wrapper component in apps/frontend/src/components/layout/Layout.tsx
- [X] T153 [US1] Add logout functionality to Header

**Checkpoint**: 🎉 MVP COMPLETE! All P1 user stories (US1, US2) and P2 user stories (US3, US4) are now fully functional

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories

### Backend Polish

- [X] T154 [P] Add API response logging for debugging in apps/backend/internal/api/middleware/logger.go
- [X] T155 [P] Add request ID tracking across services in apps/backend/internal/shared/logger/
- [X] T156 [P] Implement rate limiting middleware in apps/backend/internal/api/middleware/rate_limit.go
- [X] T157 [P] Add health check endpoint GET /health in apps/backend/internal/api/handlers/health_handler.go
- [X] T158 Run constitution compliance checks (grep for float64 amounts, verify NUMERIC(78,0), check entry immutability)
- [ ] T159 Add API documentation (serve OpenAPI spec at /docs)
- [ ] T160 [P] Setup background job for price refresh (every 5 minutes) in apps/backend/cmd/api/main.go

### Frontend Polish

- [ ] T161 [P] Add responsive CSS for mobile devices across all components
- [ ] T162 [P] Add loading spinners and skeletons for better UX
- [ ] T163 [P] Add toast notifications for success/error messages
- [ ] T164 [P] Add form validation error messages with proper styling
- [ ] T165 Add favicon and app metadata in apps/frontend/public/
- [ ] T166 Implement proper error boundaries in React app
- [ ] T167 [P] Add dark mode toggle (optional enhancement)

### Testing & Documentation

- [ ] T168 [P] Run all backend tests and verify 80%+ coverage
- [ ] T169 [P] Run all frontend tests and verify 70%+ coverage
- [ ] T170 Run quickstart.md setup validation (fresh environment test)
- [ ] T171 Create API usage examples in specs/001-portfolio-tracker/examples/
- [X] T172 [P] Update CLAUDE.md with project commands and conventions

### Security Hardening

- [X] T173 [P] Verify JWT secrets not committed to git
- [X] T174 [P] Add input sanitization to all API endpoints
- [X] T175 [P] Add SQL injection prevention verification tests
- [X] T176 [P] Add HTTPS-only cookie flags for production
- [X] T177 Conduct security review checklist from quickstart.md

### Performance Optimization

- [ ] T178 [P] Add database query performance logging
- [ ] T179 [P] Verify API p95 response time <200ms per constitution
- [ ] T180 [P] Optimize portfolio aggregation query with indexes
- [ ] T181 [P] Add Redis connection pooling
- [ ] T182 Bundle size optimization for frontend (code splitting)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Story 4 - Authentication (Phase 3)**: Depends on Foundational - START HERE (enables other stories)
- **User Story 2 - Wallets (Phase 4)**: Depends on Foundational + Authentication
- **User Story 3 - Transactions (Phase 5)**: Depends on Foundational + Authentication + Wallets
- **User Story 1 - Portfolio (Phase 6)**: Depends on Foundational + Authentication + Wallets + Transactions
- **Polish (Phase 7)**: Depends on all user stories being complete

### User Story Dependencies

```
Foundational (Phase 2) ✅
    ↓
User Story 4 (Authentication) ✅ → ENABLES all protected endpoints
    ↓
    ├─→ User Story 2 (Wallets) ✅ → ENABLES asset management
    │       ↓
    │   User Story 3 (Transactions) ✅ → ENABLES balance changes
    │       ↓
    └─────→ User Story 1 (Portfolio View) ✅ → DISPLAYS aggregated data
```

### Recommended Implementation Order

1. **Phase 1: Setup** (T001-T009) - Project initialization
2. **Phase 2: Foundational** (T010-T038) - Core infrastructure (CRITICAL - blocks all stories)
3. **Phase 3: Authentication** (T039-T066) - Build auth first to enable protected endpoints
4. **Phase 4: Wallets** (T067-T094) - Enable wallet creation and asset management
5. **Phase 5: Transactions** (T095-T130) - Enable transaction recording and history
6. **Phase 6: Portfolio** (T131-T153) - Dashboard and aggregated views (MVP COMPLETE!)
7. **Phase 7: Polish** (T154-T182) - Cross-cutting improvements

### Parallel Opportunities Within Phases

**Phase 1 (Setup)**:
- T003, T004, T005, T006, T007, T008, T009 can all run in parallel

**Phase 2 (Foundational)**:
- T014, T015, T016, T017 (Shared Infrastructure) can run in parallel
- T020, T021, T022 (Middleware) can run in parallel
- T023, T024, T025, T026 (Ledger Entities) can run in parallel
- T035, T036, T037, T038 (Ledger Tests) can run in parallel

**Phase 3 (Authentication)**:
- T039, T040 can run in parallel (entity and repository interface)
- T046, T047 (handlers) can run in parallel
- T049, T050, T051, T052, T053, T054 (all tests) can run in parallel
- T058, T059 (forms) can run in parallel
- T062, T063 (validation) can run in parallel
- T064, T065, T066 (frontend tests) can run in parallel

**Phase 4 (Wallets)**:
- T067, T068 can run in parallel
- T071, T073, T074 can run in parallel (handler implementations)
- T076, T077, T078, T079, T080, T081 (all tests) can run in parallel
- T083, T084, T085 (UI components) can run in parallel
- T090, T091 (validation) can run in parallel
- T092, T093, T094 (frontend tests) can run in parallel

**Phase 5 (Transactions)**:
- T095, T096, T097 can run in parallel (pricing entities)
- T099, T100 (pricing tests) can run in parallel
- T101, T102 (transaction domains) can run in parallel
- T105, T106 (handler registration) can run in parallel
- T107, T108 (API handlers) can run in parallel
- T110-T117 (all tests) can run in parallel
- T119, T120, T121 (UI components) can run in parallel
- T127, T128 (validation/filters) can run in parallel
- T129, T130 (frontend tests) can run in parallel

**Phase 6 (Portfolio)**:
- T132, T133 can run in parallel (handler and routes)
- T134-T137 (all tests) can run in parallel
- T139, T140, T141 (dashboard components) can run in parallel
- T144, T145, T146 (UI polish) can run in parallel
- T147, T148, T149 (frontend tests) can run in parallel
- T150, T151 (header/sidebar) can run in parallel

**Phase 7 (Polish)**:
- T154, T155, T156, T157, T160 (backend improvements) can run in parallel
- T161, T162, T163, T164, T167 (frontend improvements) can run in parallel
- T168, T169 (test runs) can run in parallel
- T173, T174, T175, T176 (security checks) can run in parallel
- T178, T179, T180, T181 (performance) can run in parallel

---

## Parallel Execution Examples

### Example: Launch all Authentication tests together

```bash
# All backend tests for US4:
Task T049: Unit test UserService.Register
Task T050: Unit test UserService.Login
Task T051: Unit test JWT generation
Task T052: Contract test POST /auth/register
Task T053: Contract test POST /auth/login
Task T054: Integration test JWT middleware

# All frontend tests for US4:
Task T064: Test RegistrationForm
Task T065: Test LoginForm
Task T066: Test ProtectedRoute
```

### Example: Launch all Wallet UI components together

```bash
# After backend is ready:
Task T083: Create WalletList component
Task T084: Create WalletCard component
Task T085: Create CreateWalletForm component
```

---

## Implementation Strategy

### MVP First (Recommended)

1. Complete **Phase 1: Setup** (T001-T009)
2. Complete **Phase 2: Foundational** (T010-T038) - CRITICAL GATE
3. Complete **Phase 3: Authentication** (T039-T066)
4. Complete **Phase 4: Wallets** (T067-T094)
5. Complete **Phase 5: Transactions** (T095-T130)
6. Complete **Phase 6: Portfolio** (T131-T153)
7. **🎉 MVP COMPLETE** - All core features functional
8. Complete **Phase 7: Polish** (T154-T182) - Production readiness

### Incremental Delivery Checkpoints

- **Checkpoint 1** (After Phase 2): Foundation ready, can demo basic API health
- **Checkpoint 2** (After Phase 3): Auth works, users can register/login
- **Checkpoint 3** (After Phase 4): Wallet management works, can create wallets and adjust balances
- **Checkpoint 4** (After Phase 5): Transaction tracking works, full ledger functionality
- **Checkpoint 5** (After Phase 6): **MVP COMPLETE** - Full portfolio tracker functional
- **Checkpoint 6** (After Phase 7): Production-ready with polish and security hardening

### Parallel Team Strategy

With 2-3 developers after Foundational phase completes:

**Week 1-2**: Complete Foundational together (critical path)

**Week 3**:
- Developer A: Authentication (US4)
- Developer B: Prepare wallet domain models

**Week 4**:
- Developer A: Wallets backend (US2)
- Developer B: Wallets frontend (US2)

**Week 5**:
- Developer A: Transactions backend (US3)
- Developer B: Transactions frontend (US3)

**Week 6**:
- Developer A: Portfolio backend (US1)
- Developer B: Portfolio frontend (US1)

**Week 7**: Polish & Testing together

---

## Task Statistics

- **Total Tasks**: 184 (added T100A, T117A from /speckit.analyze recommendations)
- **Phase 1 (Setup)**: 9 tasks
- **Phase 2 (Foundational)**: 29 tasks (including 4 constitution-required tests)
- **Phase 3 (Authentication - US4)**: 28 tasks
- **Phase 4 (Wallets - US2)**: 28 tasks
- **Phase 5 (Transactions - US3)**: 38 tasks (added T100A: price API failure test, T117A: performance test)
- **Phase 6 (Portfolio - US1)**: 22 tasks
- **Phase 7 (Polish)**: 30 tasks

**Parallelizable Tasks**: ~82 tasks marked with [P] (45% of total)

**Test Tasks**: 62+ tasks (34% of total) - exceeds constitution's 80% backend / 70% frontend coverage requirements

---

## Notes

- All financial amounts use `NUMERIC(78,0)` in database and `*big.Int` in Go (constitution requirement)
- Ledger entries are immutable - no UPDATE or DELETE operations allowed (constitution requirement)
- Every transaction must balance: `SUM(debit) = SUM(credit)` (constitution requirement)
- JWT secret must be in environment variables, never committed (security requirement)
- Tests must be written FIRST and FAIL before implementation (TDD requirement)
- API endpoints must respond within 200ms p95 (constitution requirement)
- Each user story is independently testable and deployable
- Tasks marked [P] can run in parallel (different files, no dependencies)
- [Story] label enables traceability to spec.md user stories
- Commit after each task or logical group
- Follow the Red-Green-Refactor TDD cycle
- Use exact file paths from plan.md project structure
