# Auto-Genesis Balances for Sync Negative Balance Errors

## Context

Zerion sync fails when a transaction tries to spend tokens the wallet doesn't have in the ledger yet. This happens because:
1. **Missing prior history** — 90-day lookback doesn't capture when tokens were originally acquired
2. **Same-block ordering** — transactions with identical `MinedAt` timestamps may sort sends before receives
3. **Incremental sync brittleness** — any negative balance error stops the entire sync (`break`)

The error from the user's report: the wallet has a tiny WETH balance from a prior partial sync, but the first new transaction tries to spend a much larger amount.

**Solution**: Auto-create "genesis balance" transactions to cover deficits, add same-block sort tiebreaker, and make incremental sync more resilient.

---

## Changes

### 1. Structured `NegativeBalanceError` in ledger

**File**: `apps/backend/internal/ledger/errors.go`

Add a structured error type so the sync service can programmatically extract deficit info instead of parsing error strings:

```go
type NegativeBalanceError struct {
    AccountID uuid.UUID
    AssetID   string
    Current   *big.Int
    Change    *big.Int
    NewBal    *big.Int  // the would-be negative balance
}
```

**File**: `apps/backend/internal/ledger/service.go` (~line 481)

Change `validateAccountBalances` to return `*NegativeBalanceError` instead of `fmt.Errorf(...)`. The wrapping at line 415 stays the same — callers use `errors.As()` to extract it.

### 2. New transaction type: `genesis_balance`

**File**: `apps/backend/internal/ledger/model.go`

- Add `TxTypeGenesisBalance TransactionType = "genesis_balance"`
- Add to `AllTransactionTypes()`, `IsValid()`, `Label()` (label: "Genesis Balance")

### 3. New handler: `internal/module/genesis/handler.go`

New package `internal/module/genesis/` with a single handler. Structurally identical to `TransferInHandler` but simpler (no wallet ownership check — sync already validated it).

**Entry pattern** (2 entries):
- DEBIT `wallet.{wallet_id}.{chain_id}.{asset_id}` → `asset_increase`
- CREDIT `income.genesis.{chain_id}.{asset_id}` → `income`

**Data fields**: `wallet_id`, `chain_id`, `asset_id`, `amount`, `decimals`, `usd_rate` (default "0")

### 4. Register handler in DI wiring

**File**: `apps/backend/cmd/api/main.go`

Add genesis handler registration alongside existing handlers.

### 5. Cost basis classification for genesis

**File**: `apps/backend/internal/ledger/taxlot_hook.go` (~line 191)

Update `classifyCostBasisSource()` to return `CostBasisGenesisApproximation` for `TxTypeGenesisBalance`. The `genesis_approximation` source already exists in the model and DB CHECK constraint.

### 6. Same-block sort tiebreaker

**File**: `apps/backend/internal/platform/sync/service.go` (~line 219)

Replace `sort.Slice` with `sort.SliceStable` and add secondary sort key:
- Primary: `MinedAt` ascending
- Secondary: operation priority (receives/claims first, sends/swaps last)

```go
func operationPriority(op OperationType) int {
    switch op {
    case OpReceive, OpClaim, OpMint:  return 0  // inflows first
    case OpDeposit, OpWithdraw:       return 1  // DeFi middle
    case OpTrade, OpExecute:          return 2  // swaps
    case OpSend, OpBurn:              return 3  // outflows last
    default:                          return 2
    }
}
```

### 7. Auto-genesis logic in sync service

**File**: `apps/backend/internal/platform/sync/service.go`

Replace the current error handling loop (lines 233-256) with a try-genesis-retry pattern:

```
for each transaction:
    err := processor.ProcessTransaction(ctx, w, tx)
    if err is NegativeBalanceError:
        for up to 3 retries:
            create genesis_balance tx for the deficit (abs(newBal) as amount)
            record genesis via ledgerSvc.RecordTransaction(...)
            retry original transaction
            if success: break
        if still failing: log + skip (count error)
    elif other error:
        log + skip (count error)
    else:
        advance cursor, processed++

    if consecutive errors > 5: break
```

Genesis transaction details:
- `source`: `"sync_genesis"`
- `externalID`: `"genesis:{walletID}:{chainID}:{assetID}"` (idempotent)
- `occurredAt`: 1 second before the failing transaction's `MinedAt`
- `usd_rate`: `"0"` (user can override via cost basis UI)

### 8. Update `isNegativeBalanceError` helper

**File**: `apps/backend/internal/platform/sync/port.go`

Update to also use `errors.As(*NegativeBalanceError)` for type-safe detection, keeping the string fallback for backward compatibility.

Add a helper to extract deficit info:

```go
func extractNegativeBalanceInfo(err error) *ledger.NegativeBalanceError {
    var nbe *ledger.NegativeBalanceError
    if errors.As(err, &nbe) {
        return nbe
    }
    return nil
}
```

### 9. Frontend: genesis_balance label

**File**: `apps/frontend/src/features/transactions/TransactionLotImpactSection.tsx`

Add `genesis_balance` to `sourceBadgeVariants` (similar to existing `genesis_approximation` handling).

No other frontend changes needed — the transaction list already renders all types via their `Label()`.

---

## Files to modify

| File | Change |
|------|--------|
| `apps/backend/internal/ledger/errors.go` | Add `NegativeBalanceError` struct with `Error()` method |
| `apps/backend/internal/ledger/service.go` | Return `NegativeBalanceError` from `validateAccountBalances` |
| `apps/backend/internal/ledger/model.go` | Add `TxTypeGenesisBalance` constant |
| `apps/backend/internal/ledger/taxlot_hook.go` | Handle `TxTypeGenesisBalance` in `classifyCostBasisSource` |
| `apps/backend/internal/module/genesis/handler.go` | **New file** — genesis balance handler |
| `apps/backend/cmd/api/main.go` | Register genesis handler |
| `apps/backend/internal/platform/sync/service.go` | Same-block sort tiebreaker + auto-genesis retry loop |
| `apps/backend/internal/platform/sync/port.go` | Type-safe `NegativeBalanceError` detection + extraction |
| `apps/frontend/src/features/transactions/TransactionLotImpactSection.tsx` | Add `genesis_balance` badge |

---

## Verification

1. **Unit tests**: Add test in `sync/service_test.go` for auto-genesis behavior — mock `RecordTransaction` to fail with `NegativeBalanceError` on first call, succeed on genesis, succeed on retry
2. **Build**: `cd apps/backend && go build ./...`
3. **Existing tests**: `cd apps/backend && go test ./internal/ledger/... ./internal/platform/sync/... ./internal/module/...`
4. **Manual test**: Trigger sync for a wallet with known negative balance scenario, verify genesis transaction is created and sync continues
5. **Check Loki logs**: `{service="backend", component="sync"} | json | msg=~"genesis"` — verify genesis creation is logged
