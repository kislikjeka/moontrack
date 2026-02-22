# Phase 8 Post-Review Fixes

## Context

Phase 8 (Cost Basis & Tax Lots) was implemented and then reviewed by 5 independent agents (security, transactional correctness, frontend, architecture, SQL). This plan addresses all critical and high-severity findings. Medium issues are included where the fix is trivial.

---

## Fix 1: Transaction atomicity for OverrideCostBasis (CRITICAL)

**Problem**: `CreateOverrideHistory` + `UpdateOverride` are two separate DB calls with no transaction. Partial failure = corrupt audit trail.

**File**: `apps/backend/internal/platform/taxlot/service.go` — `OverrideCostBasis` method (lines 68-107)

**Fix**: Wrap in `ledgerRepo.BeginTx/CommitTx` with deferred `RollbackTx`. Add `sync` import for Issue #7.

```go
func (s *Service) OverrideCostBasis(ctx context.Context, userID uuid.UUID, lotID uuid.UUID, costBasis *big.Int, reason string) error {
    // Begin transaction for atomicity
    txCtx, err := s.ledgerRepo.BeginTx(ctx)
    if err != nil {
        return fmt.Errorf("failed to begin transaction: %w", err)
    }
    defer s.ledgerRepo.RollbackTx(txCtx)

    // Get the lot WITH row lock to prevent concurrent override races
    lot, err := s.taxLotRepo.GetTaxLotForUpdate(txCtx, lotID)
    if err != nil {
        if errors.Is(err, ledger.ErrLotNotFound) {
            return ErrLotNotFound
        }
        return fmt.Errorf("failed to get tax lot: %w", err)
    }

    // Verify ownership
    if _, err := s.verifyLotOwnership(txCtx, userID, lot.AccountID); err != nil {
        return err
    }

    // Create audit trail
    history := &ledger.LotOverrideHistory{...}
    if err := s.taxLotRepo.CreateOverrideHistory(txCtx, history); err != nil { ... }
    if err := s.taxLotRepo.UpdateOverride(txCtx, lotID, costBasis, reason); err != nil { ... }

    // Commit atomically
    if err := s.ledgerRepo.CommitTx(txCtx); err != nil {
        return fmt.Errorf("failed to commit override: %w", err)
    }
    return nil
}
```

---

## Fix 2: Add `GetTaxLotForUpdate` for row-level locking (HIGH)

**Problem**: No row lock during override — concurrent overrides cause incorrect `PreviousCostBasis` in audit trail.

**Files**:
- `apps/backend/internal/ledger/taxlot_port.go` — add `GetTaxLotForUpdate` to interface
- `apps/backend/internal/infra/postgres/taxlot_repo.go` — implement with `SELECT ... FOR UPDATE`

```go
// In taxlot_port.go interface:
GetTaxLotForUpdate(ctx context.Context, id uuid.UUID) (*TaxLot, error)

// In taxlot_repo.go: copy of GetTaxLot but query adds "FOR UPDATE" at the end
```

---

## Fix 3: Fix broken `ErrLotNotFound` error chain (HIGH)

**Problem**: Repo wraps `pgx.ErrNoRows`, handler checks `taxlot.ErrLotNotFound` — never matches, always 500.

**Files**:
- `apps/backend/internal/infra/postgres/taxlot_repo.go` — `GetTaxLot` and new `GetTaxLotForUpdate`: return `ledger.ErrLotNotFound` instead of wrapping `pgx.ErrNoRows`
- `apps/backend/internal/platform/taxlot/service.go` — translate `ledger.ErrLotNotFound` → `taxlot.ErrLotNotFound`
- `apps/backend/internal/platform/taxlot/service.go` — in `verifyWalletOwnership`, translate `wallet.ErrWalletNotFound` → `ErrWalletNotOwned`

```go
// taxlot_repo.go GetTaxLot:
if err == pgx.ErrNoRows {
    return nil, ledger.ErrLotNotFound  // was: fmt.Errorf("tax lot not found: %w", err)
}
```

---

## Fix 4: Input validation — negative cost basis + reason length (MEDIUM)

**File**: `apps/backend/internal/transport/httpapi/handler/taxlot.go` — `OverrideCostBasis` handler

After `money.ToBaseUnits` parsing, add:
```go
if costBasis.Sign() < 0 {
    respondWithError(w, http.StatusBadRequest, "cost_basis_per_unit must be non-negative")
    return
}
if len(req.Reason) > 1000 {
    respondWithError(w, http.StatusBadRequest, "reason must be 1000 characters or less")
    return
}
```

---

## Fix 5: WAC refresh throttling (HIGH)

**Problem**: `REFRESH MATERIALIZED VIEW CONCURRENTLY` on every request = DoS vector.

**File**: `apps/backend/internal/platform/taxlot/service.go`

Add a time-based throttle (30s minimum gap between refreshes):

```go
// Add fields to Service struct:
lastWACRefresh time.Time
wacRefreshMu   sync.Mutex

// Replace direct RefreshWAC call with:
func (s *Service) maybeRefreshWAC(ctx context.Context) error {
    s.wacRefreshMu.Lock()
    defer s.wacRefreshMu.Unlock()
    if time.Since(s.lastWACRefresh) < 30*time.Second {
        return nil
    }
    if err := s.taxLotRepo.RefreshWAC(ctx); err != nil {
        return err
    }
    s.lastWACRefresh = time.Now()
    return nil
}
```

---

## Fix 6: Frontend null safety (HIGH)

**Problem**: Go returns `null` for empty slices in JSON. `response.data.lots` / `response.data.positions` can be `null`.

**File**: `apps/frontend/src/services/taxlot.ts`

```ts
// getLots:
return response.data.lots || []

// getWAC:
return response.data.positions || []
```

---

## Fix 7: Frontend numeric validation + dialog cleanup (MEDIUM)

**File**: `apps/frontend/src/components/domain/CostBasisOverrideDialog.tsx`

1. Add `isValidNumber` check before submit
2. Fix Cancel button to go through `handleOpen(false)` for consistent state reset
3. Add `aria-label` to edit button in `LotDetailTable.tsx`

```ts
const isValidNumber = (v: string) => /^\d+(\.\d{0,8})?$/.test(v)

// disabled prop:
disabled={!isValidNumber(costBasis) || !reason.trim() || override.isPending}

// Cancel: onClick={() => handleOpen(false)} instead of onClick={() => onOpenChange(false)}
```

---

## Fix 8: Typed response structs (LOW, quick)

**File**: `apps/backend/internal/transport/httpapi/handler/taxlot.go`

Replace `map[string]interface{}` with typed structs:
```go
type TaxLotsListResponse struct {
    Lots []TaxLotResponse `json:"lots"`
}
type WACPositionsResponse struct {
    Positions []PositionWACResponse `json:"positions"`
}
```

---

## Fix 9: Edit button accessibility (MEDIUM)

**File**: `apps/frontend/src/components/domain/LotDetailTable.tsx`

Add `aria-label` to the Pencil button:
```tsx
<Button ... aria-label={`Override cost basis for lot ${index + 1}`}>
```

---

## Files Changed

| File | Fixes |
|------|-------|
| `internal/ledger/taxlot_port.go` | #2: Add `GetTaxLotForUpdate` |
| `internal/infra/postgres/taxlot_repo.go` | #2: Implement `GetTaxLotForUpdate`; #3: Fix `ErrLotNotFound` return |
| `internal/platform/taxlot/service.go` | #1: Tx wrapper; #3: Error translation; #5: WAC throttle |
| `internal/transport/httpapi/handler/taxlot.go` | #4: Validation; #8: Typed responses |
| `src/services/taxlot.ts` | #6: Null safety |
| `src/components/domain/CostBasisOverrideDialog.tsx` | #7: Numeric validation + dialog reset |
| `src/components/domain/LotDetailTable.tsx` | #9: Aria label |

## Verification

- `cd apps/backend && go build ./...`
- `cd apps/frontend && bun run build`
- `cd apps/frontend && bun run test --run`
