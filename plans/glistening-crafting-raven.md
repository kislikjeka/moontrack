# Phase 1: Foundation — Critical Fixes & Infrastructure Prerequisites

## Context
Phase 1 addresses 5 critical architecture issues and lays infrastructure groundwork (new transaction/account/entry types, config, migration) for subsequent DeFi feature phases. No frontend changes.

## Parallelization Strategy

Three parallel work streams, then a final validation pass:

### Stream A: Ledger Model + Config + Duplicate Detection (independent files)
**Files:** `ledger/model.go`, `pkg/config/config.go`, `.env.example`, `processor.go` (isDuplicateError only)

1. **ledger/model.go** — Add 4 tx types (swap, defi_deposit, defi_withdraw, defi_claim) + update AllTransactionTypes/IsValid/Label. Add `AccountTypeClearing = "CLEARING"` + update Validate(). Add `EntryTypeClearing = "clearing"`.
2. **pkg/config/config.go** — Add `ZerionAPIKey string` field + `getEnv("ZERION_API_KEY", "")` in Load()
3. **.env.example** — Add `ZERION_API_KEY=your-zerion-api-key-here`
4. **processor.go** — Replace string-matching `isDuplicateError()` with `pgconn.PgError` code `23505` check. Add imports for `errors` and `github.com/jackc/pgx/v5/pgconn` (keep `strings` — used elsewhere).

### Stream B: Wallet Sync Infrastructure (coupled files)
**Files:** `wallet/model.go`, `sync/port.go`, `wallet/port.go`, `wallet_repo.go`, `service.go`, `processor.go` (user-scoped methods), `processor_test.go` (mock updates)

Items 1 (Atomic Claim), 2 (Stale Recovery), 8 (Cross-User Prevention) are coupled through Wallet struct + Scan sites. Done as single atomic unit:

1. **wallet/model.go** — Add `SyncStartedAt *time.Time` field after SyncError
2. **sync/port.go** — Add `ClaimWalletForSync` and `GetWalletsByAddressAndUserID` to WalletRepository interface
3. **wallet/port.go** — Add same 2 methods to Repository interface
4. **wallet_repo.go** — Update ALL 4 Scan sites (GetByID, GetByUserID, GetWalletsForSync, GetWalletsByAddress) to include `sync_started_at`. Update GetWalletsForSync WHERE to include stale syncing wallets. Implement `ClaimWalletForSync()` with atomic UPDATE...RETURNING. Implement `GetWalletsByAddressAndUserID()`.
5. **service.go** — Replace `SetSyncInProgress` call with `ClaimWalletForSync`, skip if returns false
6. **processor.go** — Update `isUserWallet()` and `getWalletByAddress()` to accept `userID` param and call `GetWalletsByAddressAndUserID`. Update callers: `classifyTransfer()` passes `w.UserID`, `recordInternalTransfer()` passes `w.UserID`.
7. **processor_test.go** — Add mock methods for `ClaimWalletForSync` and `GetWalletsByAddressAndUserID`. Update ALL existing test expectations from `GetWalletsByAddress` → `GetWalletsByAddressAndUserID` (10 tests affected).

### Stream C: Migration + New Test Files (independent)
**Files:** `migrations/000008_foundation.up.sql`, `migrations/000008_foundation.down.sql`, `ledger/model_test.go` (NEW), `alchemy/types_test.go` (NEW)

1. **000008_foundation.up.sql** — Add CLEARING to accounts CHECK constraint, add `sync_started_at` column, add index
2. **000008_foundation.down.sql** — Reverse all above
3. **ledger/model_test.go** — Test new tx types (IsValid, Label, AllTransactionTypes), CLEARING account validation
4. **alchemy/types_test.go** — Test TransferCategoriesForChain: L1 (1, 137) includes "internal", L2 (42161, 10, 8453, 43114, 56) excludes it
5. **processor_test.go** — Update idempotency test to use `pgconn.PgError{Code: "23505"}` instead of generic error

## Verification Checkpoints

1. **After all streams complete:** `cd apps/backend && go build ./...` — must pass with 0 errors
2. **Then:** `cd apps/backend && go test ./...` — must pass all existing + new tests
3. **Specific test commands:**
   - `go test -v ./internal/ledger/...` (new model tests)
   - `go test -v ./internal/platform/sync/...` (updated processor tests)
   - `go test -v ./internal/infra/gateway/alchemy/...` (new types tests)

## Final Validation
After all changes, run full check: `go build ./...` && `go test ./...` to confirm Phase 1 is complete.
