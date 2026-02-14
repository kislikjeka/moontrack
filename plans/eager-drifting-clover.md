# Phase 3: Classifier + Zerion Sync Pipeline

## Context
Phase 3 connects the Zerion data provider (built in Phase 2) to the sync pipeline by building a classifier, Zerion processor, updating the sync service for time-based cursors, and wiring everything in main.go. This makes Zerion the active sync data source while preserving Alchemy as a fallback.

## Strategy: Revised NewService Approach
Per plan note (line 689), use the **revised approach**: modify `NewService()` to accept both providers as optional. Inside `syncWallet()`, check `s.zerionProvider != nil` to decide which sync path. This minimizes breaking changes — existing tests pass the `blockchainClient` as before, and new Zerion path is activated when `zerionProvider` is set.

## Implementation Steps

### Step 1: Config Update (`internal/platform/sync/config.go`)
- Add `InitialSyncLookback time.Duration` field (default: `2160 * time.Hour`)
- Mark existing block-based fields with deprecation comments
- Update `DefaultConfig()` to set `InitialSyncLookback`

### Step 2: WalletRepository — SetSyncCompletedAt
- **`internal/platform/sync/port.go`**: Add `SetSyncCompletedAt(ctx, walletID, syncAt)` to `WalletRepository` interface
- **`internal/platform/wallet/port.go`**: Add matching method to `wallet.Repository` interface
- **`internal/infra/postgres/wallet_repo.go`**: Implement — updates `sync_status=synced`, `last_sync_at`, clears `sync_error`, does NOT touch `last_sync_block`
- **Update mock in `processor_test.go`**: Add `SetSyncCompletedAt` to `MockWalletRepository`

### Step 3: Classifier (`internal/platform/sync/classifier.go` — NEW)
- Stateless `Classifier` struct
- `Classify(tx DecodedTransaction) ledger.TransactionType` — maps OperationType → TransactionType
- Private `classifyExecute(tx)` — infers type from transfer directions

### Step 4: ZerionProcessor (`internal/platform/sync/zerion_processor.go` — NEW)
- `ZerionProcessor` struct with `walletRepo`, `ledgerSvc`, `classifier`, `logger`, `addressCache`
- `ProcessTransaction(ctx, w, tx)` — classify, detect internal transfers, build raw data, record
- `isUserWallet()` / `getWalletByAddress()` — user-scoped lookup with cache (same pattern as existing Processor)
- Raw data builders: `buildTransferInData`, `buildTransferOutData`, `buildSwapData`, `buildInternalTransferData`, `buildDeFiDepositData`, `buildDeFiWithdrawData`, `buildDeFiClaimData`
- `ClearCache()` method
- Uses `isDuplicateError()` from processor.go (already exported at package level)

### Step 5: Sync Service Update (`internal/platform/sync/service.go`)
- Add `zerionProvider TransactionDataProvider` and `zerionProcessor *ZerionProcessor` fields to `Service` struct
- Modify `NewService()` signature: add `zerionProvider TransactionDataProvider` parameter (keep `blockchainClient` for backward compat)
- Add `syncWalletZerion()` — time-based sync with cursor safety (break on first error)
- Modify `syncWallet()` — dispatch to `syncWalletZerion()` if `zerionProvider != nil`, else existing block-based path
- Update `SyncWallet()` public method if needed

### Step 6: main.go Wiring (`cmd/api/main.go`)
- Add zerion import
- When `cfg.ZerionAPIKey != ""`: create Zerion client + adapter, pass to `NewService` as `zerionProvider`
- When only `cfg.AlchemyAPIKey != ""`: pass nil for `zerionProvider` (existing behavior)
- Update `NewService()` call to include new parameter

### Step 7: Tests
- **`classifier_test.go`** (NEW) — all OperationType mappings + classifyExecute cases
- **`zerion_processor_test.go`** (NEW) — transfer in/out, swap, internal transfer, approve skip, failed skip, duplicate handling, USD prices, gas fees, DeFi operations
- **Update `service_integration_test.go`** — fix `NewService()` call signature (add nil for zerionProvider)

### Step 8: Verification
- `go build ./...` — zero errors
- `go test ./internal/platform/sync/...` — all tests pass
- `go test ./...` (full suite, skip integration) — all pass

## Key Files
| File | Action |
|------|--------|
| `apps/backend/internal/platform/sync/config.go` | MODIFY |
| `apps/backend/internal/platform/sync/port.go` | MODIFY |
| `apps/backend/internal/platform/wallet/port.go` | MODIFY |
| `apps/backend/internal/infra/postgres/wallet_repo.go` | MODIFY |
| `apps/backend/internal/platform/sync/classifier.go` | NEW |
| `apps/backend/internal/platform/sync/zerion_processor.go` | NEW |
| `apps/backend/internal/platform/sync/service.go` | MODIFY |
| `apps/backend/cmd/api/main.go` | MODIFY |
| `apps/backend/internal/platform/sync/classifier_test.go` | NEW |
| `apps/backend/internal/platform/sync/zerion_processor_test.go` | NEW |
| `apps/backend/internal/platform/sync/processor_test.go` | MODIFY (mock update) |
| `apps/backend/internal/platform/sync/service_integration_test.go` | MODIFY (signature fix) |

## Parallelization Plan
Steps 1-4 are independent and can be implemented in parallel by subagents:
- **Agent A**: Steps 1+2 (Config + WalletRepo changes)
- **Agent B**: Step 3 (Classifier + classifier tests)
- **Agent C**: Step 4 (ZerionProcessor)

After validation, Steps 5-6 (Service + main.go) depend on all prior steps.
Step 7 tests depend on the implementations they test.

## Verification
```bash
cd apps/backend && go build ./...
cd apps/backend && go test -count=1 ./internal/platform/sync/...
cd apps/backend && go test -count=1 -short ./...
```
