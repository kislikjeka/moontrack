# Two-Phase Zerion Sync: Initial vs Incremental

## Context

Zerion wallet sync has two discovered issues:

1. **"invalid asset ID" error** (FIXED, uncommitted) — `buildTransferInData`/`buildTransferOutData`/`buildInternalTransferData` passed `data["transfers"]` array instead of flat fields (`asset_id`, `amount`, etc.) expected by handlers.

2. **Negative balance on outgoing transfers** — Zerion API returns transactions **newest-first**. An ETH send fails with `would have negative balance` because the older ETH receive hasn't been processed yet.

Simple sorting is insufficient because:
- Zerion paginates backwards in time; within a single page ordering can be fixed, but the fundamental problem is that **initial sync** of a wallet with existing history will always encounter transactions that depend on prior balance not yet in the ledger.
- **Incremental sync** (wallet already has history) should remain strict — negative balance errors indicate real problems.

**Solution**: Two-phase sync — relaxed initial sync that skips negative balance errors, strict incremental sync that stops on any error.

## Plan

### Step 1: Commit the existing fix (Issue 1)

Files already changed (uncommitted):
- `apps/backend/internal/platform/sync/zerion_processor.go` — flat fields instead of transfers array
- `apps/backend/internal/platform/sync/zerion_processor_test.go` — updated assertions

### Step 2: Add `isNegativeBalanceError()` helper

**File**: `apps/backend/internal/platform/sync/port.go`

```go
// isNegativeBalanceError checks if err is a ledger negative-balance rejection.
func isNegativeBalanceError(err error) bool {
    if err == nil {
        return false
    }
    msg := err.Error()
    return strings.Contains(msg, "would have negative balance") ||
        strings.Contains(msg, "balance would be negative")
}
```

Both error messages come from `ledger/service.go:466` and `ledger/service.go:578`.

### Step 3: Refactor `syncWallet()` with two-phase processing

**File**: `apps/backend/internal/platform/sync/service.go`

Add `"sort"` and `"strings"` imports.

#### 3a. Sort transactions oldest-first

After fetching transactions (line ~214), before processing loop:

```go
sort.Slice(transactions, func(i, j int) bool {
    return transactions[i].MinedAt.Before(transactions[j].MinedAt)
})
```

Both phases benefit from chronological ordering — it minimizes negative-balance hits during initial sync and ensures correct ordering during incremental sync.

#### 3b. Replace the processing loop (lines 217-233) with phase-aware logic

```go
isInitialSync := w.LastSyncAt == nil

var lastSuccessfulMinedAt *time.Time
var processErrors []error
processed := 0
skipped := 0

for _, tx := range transactions {
    if err := s.zerionProcessor.ProcessTransaction(ctx, w, tx); err != nil {
        if isInitialSync && isNegativeBalanceError(err) {
            // Initial sync: skip transactions that fail due to missing prior history
            s.logger.Warn("skipping transaction (negative balance during initial sync)",
                "wallet_id", w.ID,
                "tx_hash", tx.TxHash,
                "zerion_id", tx.ID,
                "error", err)
            skipped++
            continue
        }

        // Incremental sync OR non-balance error: stop immediately
        s.logger.Error("failed to process transaction, stopping sync",
            "wallet_id", w.ID,
            "tx_hash", tx.TxHash,
            "zerion_id", tx.ID,
            "error", err)
        processErrors = append(processErrors, err)
        break
    }
    minedAt := tx.MinedAt
    lastSuccessfulMinedAt = &minedAt
    processed++
}
```

#### 3c. Update cursor advancement (lines 235-251)

Add `skipped` to the summary log. Cursor logic stays the same — advance to last successful tx. If all transactions were skipped (no successful ones), don't advance cursor.

```go
s.logger.Info("wallet sync completed",
    "wallet_id", w.ID,
    "transactions_processed", processed,
    "transactions_skipped", skipped,
    "errors", len(processErrors),
    "is_initial_sync", isInitialSync)
```

### Step 4: Add unit test for two-phase behavior

**File**: `apps/backend/internal/platform/sync/service_test.go` (new file)

Uses existing mocks from `test_helpers_test.go` (`MockWalletRepository`, `MockLedgerService`).

Test cases:
1. **Initial sync skips negative balance errors** — wallet with `LastSyncAt == nil`, mock returns negative balance error for one tx, verify it continues processing remaining txs
2. **Incremental sync stops on negative balance error** — wallet with `LastSyncAt != nil`, verify processing stops on first error
3. **Transactions processed oldest-first** — provide transactions in reverse order, verify the mock receives them chronologically

## Files to Modify

| File | Change |
|------|--------|
| `apps/backend/internal/platform/sync/zerion_processor.go` | **already changed** (commit) |
| `apps/backend/internal/platform/sync/zerion_processor_test.go` | **already changed** (commit) |
| `apps/backend/internal/platform/sync/port.go` | Add `isNegativeBalanceError()` + `"strings"` import |
| `apps/backend/internal/platform/sync/service.go` | Sort + two-phase processing loop + `"sort"` import |
| `apps/backend/internal/platform/sync/service_test.go` | New unit tests for two-phase behavior |

## Verification

1. `cd apps/backend && go build ./...` — verify compilation
2. `cd apps/backend && go test ./internal/platform/sync/... -v` — run all sync tests
3. `cd apps/backend && go test ./internal/infra/gateway/zerion/... -v` — run adapter tests
4. Deploy and verify via Loki:
   ```logql
   {service="backend"} | json | component="sync" | wallet_id="2f646418-af9d-462a-a298-54cbbf0f5fc4"
   ```
   Expect: initial sync processes deposits, skips sends that fail on negative balance with `Warn` log, cursor advances, `skipped > 0` in summary.
