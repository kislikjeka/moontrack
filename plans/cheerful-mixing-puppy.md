# Phase 3: Gap Analysis & Fix Plan

## Context
Phase 3 (Classifier + Zerion Sync Pipeline) is **mostly implemented**. The classifier, Zerion processor, config, service, ports, wallet repo, main.go wiring, and tests all exist and are functionally correct. However, a gap analysis against the plan reveals **2 critical issues** that must be fixed.

## Gaps Found

### GAP 1: Cursor Safety Bug (CRITICAL)
**File:** `apps/backend/internal/platform/sync/service.go` (lines 229-247)

**Plan says:** Advance cursor ONLY to the `MinedAt` of the last successfully committed transaction. If tx N fails, cursor stays at tx N-1's `MinedAt`. Track `lastSuccessfulMinedAt` per-transaction.

**Code does:** On first error, breaks and returns error. But if ALL transactions succeed, it calls `SetSyncCompletedAt(ctx, w.ID, time.Now())` — using wall-clock time instead of the last transaction's `MinedAt`. This means the cursor jumps ahead of all known transactions, potentially skipping transactions mined between the last tx and `time.Now()`. Also, the plan specifies that partial success should advance cursor to last successful tx, while the current code either advances to `time.Now()` (all succeed) or doesn't advance at all (any failure).

**Fix:** Rewrite `syncWalletZerion()` to track `lastSuccessfulMinedAt` as the plan specifies:
- On each successful tx, update `lastSuccessfulMinedAt = &tx.MinedAt`
- On failure, break but still advance cursor to `lastSuccessfulMinedAt` if any succeeded
- If all succeed, use last tx's `MinedAt` (not `time.Now()`)
- If no transactions returned and no errors, use `time.Now()` (wallet is up to date)
- If first tx fails, call `SetSyncError()` and don't advance cursor

### GAP 2: Classifier OpMint/OpBurn Mapping Mismatch
**File:** `apps/backend/internal/platform/sync/classifier.go` (lines 29-32)

**Plan says:** `OpMint` → `TxTypeDeFiDeposit`, `OpBurn` → `TxTypeDeFiWithdraw`

**Code does:** `OpMint` → `TxTypeTransferIn`, `OpBurn` → `TxTypeTransferOut`

**Decision:** The plan's mapping is semantically more correct for DeFi: minting LP tokens = depositing into protocol, burning LP tokens = withdrawing from protocol. However, the current mapping (transfer_in/transfer_out) also has merit for simple token mints/burns not related to DeFi (e.g., NFT minting). Given the plan explicitly specifies DeFi mapping and Phase 4 DeFi handlers depend on this classification, **follow the plan**.

**Fix:** Change classifier mappings + update test expectations.

### Non-Issues (verified as OK)
- **ExternalID without "zerion_" prefix**: Code uses `tx.ID` directly. The `source="zerion"` column + `UNIQUE(source, external_id)` already prevents collisions with Alchemy's `source="blockchain"` records. The "zerion_" prefix is redundant. **Keep as-is.**
- **Raw data format (nested vs flat)**: Code uses nested `transfers_in`/`transfers_out` arrays for swaps instead of flat `sold_asset_id` etc. This is actually better — DeFi transactions can have multiple in/out transfers. Phase 4 handlers will consume these structures. The plan's flat format was a simplification. **Keep as-is.**
- **Cache key without userID**: Code uses just `address` as cache key, but `ClearCache()` is called after each wallet. Since different users' wallets are processed in separate sync cycles, this is safe. **Keep as-is.**

## Implementation Steps

### Step 1: Fix Classifier OpMint/OpBurn Mapping
**File:** `apps/backend/internal/platform/sync/classifier.go`
- Change `OpMint` → `ledger.TxTypeDefiDeposit` (line 30)
- Change `OpBurn` → `ledger.TxTypeDefiWithdraw` (line 32)

**File:** `apps/backend/internal/platform/sync/classifier_test.go`
- Update test expectations: `"mint -> transfer_in"` → `"mint -> defi_deposit"` with `ledger.TxTypeDefiDeposit`
- Update test expectations: `"burn -> transfer_out"` → `"burn -> defi_withdraw"` with `ledger.TxTypeDefiWithdraw`

### Step 2: Fix Cursor Safety in syncWalletZerion
**File:** `apps/backend/internal/platform/sync/service.go`
Rewrite `syncWalletZerion()` method (lines 189-257) to implement the plan's cursor safety:

```go
func (s *Service) syncWalletZerion(ctx context.Context, w *wallet.Wallet) error {
    // ... (claim + fetch stay the same) ...

    // Process each transaction, track last successful cursor
    var lastSuccessfulMinedAt *time.Time
    var processErrors []error
    for _, tx := range transactions {
        if err := s.zerionProcessor.ProcessTransaction(ctx, w, tx); err != nil {
            s.logger.Error("failed to process transaction",
                "wallet_id", w.ID, "tx_hash", tx.TxHash, "zerion_id", tx.ID, "error", err)
            processErrors = append(processErrors, err)
            break // CRITICAL: stop on first error
        }
        minedAt := tx.MinedAt
        lastSuccessfulMinedAt = &minedAt
    }

    // Update cursor ONLY to last successfully committed transaction
    if lastSuccessfulMinedAt != nil {
        if err := s.walletRepo.SetSyncCompletedAt(ctx, w.ID, *lastSuccessfulMinedAt); err != nil {
            return fmt.Errorf("failed to mark sync completed: %w", err)
        }
    } else if len(processErrors) == 0 {
        // No transactions and no errors = wallet is up to date
        if err := s.walletRepo.SetSyncCompletedAt(ctx, w.ID, time.Now()); err != nil {
            return fmt.Errorf("failed to mark sync completed: %w", err)
        }
    } else {
        // First transaction failed, cursor not advanced
        errMsg := fmt.Sprintf("sync failed on first transaction: %v", processErrors[0])
        _ = s.walletRepo.SetSyncError(ctx, w.ID, errMsg)
    }

    // Clear cache + log
    s.zerionProcessor.ClearCache()
    // ... logging ...
    return nil
}
```

## Verification

1. `cd apps/backend && go build ./...` — must pass with zero errors
2. `cd apps/backend && go test ./internal/platform/sync/...` — all tests pass
3. Verify classifier_test.go expectations match new mappings
4. Manual review: confirm cursor safety logic matches plan Section 4 exactly
