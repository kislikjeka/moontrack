# Auto-Genesis Balances for Sync Negative Balance Errors

## Context

Zerion sync fails when a transaction tries to spend tokens the wallet doesn't have in the ledger yet (missing prior history, same-block ordering, etc.). The current implementation was partially done but had an architecture violation: `ledgerSvc` was added directly to `sync.Service`, but it should only exist in `ZerionProcessor`.

**This plan corrects the approach** — all ledger operations stay in `ZerionProcessor`, and `Service` only orchestrates.

**Reconciliation strategy**: Genesis transactions are permanent. If later a full re-sync produces the "real" transaction, the user corrects manually via the existing cost basis UI or asset adjustment. No auto-void logic needed.

## Step 0: Revert all previous changes

```bash
git checkout -- apps/backend/
rm -rf apps/backend/internal/module/genesis/
```

This resets all files to the clean `main` state.

## Step 1: `NegativeBalanceError` struct in ledger

**File**: `apps/backend/internal/ledger/errors.go`

Add structured error type with `AccountID`, `AssetID`, `Current`, `Change`, `NewBal` fields and `Error()` method. Same as before — this is correct.

**File**: `apps/backend/internal/ledger/service.go` (~line 488)

Change `validateAccountBalances` to return `&NegativeBalanceError{...}` instead of `fmt.Errorf(...)`.

## Step 2: `TxTypeGenesisBalance` in model

**File**: `apps/backend/internal/ledger/model.go`

- Add `TxTypeGenesisBalance TransactionType = "genesis_balance"`
- Add to `AllTransactionTypes()`, `IsValid()`, `Label()` ("Genesis Balance")

**File**: `apps/backend/internal/ledger/model_test.go`

- Update type count from 10 → 11

## Step 3: Genesis handler module

**File**: `apps/backend/internal/module/genesis/handler.go` (new)

Same handler as before — implements `ledger.Handler`, generates 2 balanced entries:
- DEBIT `wallet.{wallet_id}.{chain_id}.{asset_id}` → `asset_increase`
- CREDIT `income.genesis.{chain_id}.{asset_id}` → `income`

## Step 4: Register handler + taxlot classification

**File**: `apps/backend/cmd/api/main.go` — register genesis handler
**File**: `apps/backend/internal/ledger/taxlot_hook.go` — add `TxTypeGenesisBalance → CostBasisGenesisApproximation`

## Step 5: Same-block sort tiebreaker

**File**: `apps/backend/internal/platform/sync/service.go` (~line 219)

Replace `sort.Slice` with `sort.SliceStable` + secondary sort by `operationPriority()`. The `operationPriority` function is added to `service.go` (no new imports needed — `OperationType` is in the same package).

## Step 6: Genesis creation in `ZerionProcessor` (NOT `Service`)

**File**: `apps/backend/internal/platform/sync/zerion_processor.go`

Add method to `ZerionProcessor`:

```go
func (p *ZerionProcessor) CreateGenesisBalance(ctx context.Context, w *wallet.Wallet, failingTx DecodedTransaction, assetID string, deficit *big.Int) error {
    externalID := fmt.Sprintf("genesis:%s:%s:%s", w.ID.String(), failingTx.ChainID, assetID)
    occurredAt := failingTx.MinedAt.Add(-1 * time.Second)
    rawData := map[string]interface{}{
        "wallet_id": w.ID.String(),
        "chain_id":  failingTx.ChainID,
        "asset_id":  assetID,
        "amount":    deficit.String(),
        "decimals":  0,
        "usd_rate":  "0",
    }
    _, err := p.ledgerSvc.RecordTransaction(ctx, ledger.TxTypeGenesisBalance, "sync_genesis", &externalID, occurredAt, rawData)
    if err != nil {
        if isDuplicateError(err) {
            return nil
        }
        return fmt.Errorf("failed to record genesis balance: %w", err)
    }
    return nil
}
```

This keeps ledger operations in `ZerionProcessor` — same place all other `RecordTransaction` calls live. **`Service` struct does NOT get `ledgerSvc`.**

## Step 7: Deficit extraction in `port.go` (no ledger leak)

**File**: `apps/backend/internal/platform/sync/port.go`

`port.go` already imports `ledger`. Add a sync-package wrapper type so `service.go` never touches `ledger` types:

```go
// DeficitInfo holds parsed negative-balance details.
// Defined in sync package so service.go doesn't need to import ledger.
type DeficitInfo struct {
    AssetID string
    Deficit *big.Int // abs(would-be negative balance)
}

// extractDeficitInfo extracts deficit details from a NegativeBalanceError.
// Returns nil if err is not a structured NegativeBalanceError.
func extractDeficitInfo(err error) *DeficitInfo {
    var nbe *ledger.NegativeBalanceError
    if errors.As(err, &nbe) {
        return &DeficitInfo{
            AssetID: nbe.AssetID,
            Deficit: new(big.Int).Abs(nbe.NewBal),
        }
    }
    return nil
}
```

Also update `isNegativeBalanceError` to use `errors.As` with string fallback.

## Step 8: Auto-genesis retry loop in `Service`

**File**: `apps/backend/internal/platform/sync/service.go`

Replace the sync loop (lines 233-256) with try-genesis-retry pattern. `service.go` does NOT import `ledger` — uses `isNegativeBalanceError()` and `extractDeficitInfo()` from `port.go`, and calls `s.zerionProcessor.CreateGenesisBalance(...)`.

Only new import needed: `"math/big"` (for `DeficitInfo.Deficit` type).

```
for each tx:
    err := s.zerionProcessor.ProcessTransaction(ctx, w, tx)
    if err && isNegativeBalanceError(err):
        for up to 3 retries:
            info := extractDeficitInfo(err)
            if info == nil: break  // unstructured error, can't extract
            gErr := s.zerionProcessor.CreateGenesisBalance(ctx, w, tx, info.AssetID, info.Deficit)
            if gErr: log + break
            genesisCreated++
            err = s.zerionProcessor.ProcessTransaction(ctx, w, tx)  // retry
            if err == nil: break
            if !isNegativeBalanceError(err): break
        if success: advance cursor, processed++
        else: skip, consecutiveErrors++
    elif err:
        log + skip, consecutiveErrors++
    else:
        advance cursor, processed++
    if consecutiveErrors > 5: break
```

**Key**: `service.go` has zero `ledger` imports. All ledger-aware logic is in `port.go` (helpers) and `zerion_processor.go` (RecordTransaction calls).

## Step 9: Update sync test

**File**: `apps/backend/internal/platform/sync/service_test.go`

Update `TestSyncWallet_IncrementalSync_StopsOnNegativeBalanceError` — the old test expected sync to hard-stop on negative balance during incremental sync. Now it skips (with genesis attempt) and continues. Update mocks and assertions to reflect new behavior.

## Files to modify

| File | Change |
|------|--------|
| `internal/ledger/errors.go` | Add `NegativeBalanceError` struct |
| `internal/ledger/service.go` | Return `NegativeBalanceError` from `validateAccountBalances` |
| `internal/ledger/model.go` | Add `TxTypeGenesisBalance` constant |
| `internal/ledger/model_test.go` | Update type count 10→11 |
| `internal/ledger/taxlot_hook.go` | Handle `TxTypeGenesisBalance` in `classifyCostBasisSource` |
| `internal/module/genesis/handler.go` | **New file** — genesis balance handler |
| `cmd/api/main.go` | Register genesis handler |
| `internal/platform/sync/zerion_processor.go` | Add `CreateGenesisBalance` method |
| `internal/platform/sync/port.go` | `DeficitInfo` wrapper type + `extractDeficitInfo()` + update `isNegativeBalanceError()` |
| `internal/platform/sync/service.go` | Sort tiebreaker + auto-genesis retry loop (NO ledger import, uses port.go helpers) |
| `internal/platform/sync/service_test.go` | Update incremental sync test |

## Verification

1. `cd apps/backend && go build ./...`
2. `cd apps/backend && go test ./internal/ledger/...`
3. `cd apps/backend && go test ./internal/module/...`
4. `cd apps/backend && go test ./internal/platform/sync/...`
