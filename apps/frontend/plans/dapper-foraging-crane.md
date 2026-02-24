# Fix: Lot Sort Order + Nil Pointer Guard

## Context

After the API refactoring (Phases A-D), code review revealed two issues:

1. **Lot sort order is counterintuitive** — current sort is "open first, then FIFO ascending". User wants **newest-first (descending by acquired date)**. With FIFO disposal, oldest lots get consumed first, so closed lots naturally appear at the bottom — no need for explicit open/closed grouping.

2. **Nil pointer vulnerability** in realized gain/loss computation — `ProceedsPerUnit` or `EffectiveCostBasisPerUnit()` could be nil, causing a runtime panic.

## Changes

### 1. Fix lot sort order — newest first

**File:** `apps/backend/internal/platform/taxlot/service.go` — `GetLotsByWallet` method (lines ~111-132)

Replace sort logic:
```go
// Current: open first → FIFO ascending
// New: newest first (descending by AcquiredAt), tiebreak by CreatedAt descending
sort.Slice(allLots, func(i, j int) bool {
    if chainID == "" {
        ci := strings.ToLower(allLots[i].ChainID)
        cj := strings.ToLower(allLots[j].ChainID)
        if ci != cj {
            return ci < cj
        }
    }
    if !allLots[i].AcquiredAt.Equal(allLots[j].AcquiredAt) {
        return allLots[i].AcquiredAt.After(allLots[j].AcquiredAt) // newest first
    }
    return allLots[i].CreatedAt.After(allLots[j].CreatedAt) // newest first
})
```

- Remove open/closed grouping entirely
- Sort by `AcquiredAt` descending (newest first)
- Keep chain grouping for unfiltered view

### 2. Add nil guard in gain/loss computation

**File:** `apps/backend/internal/platform/taxlot/service.go` — `GetLotImpactByTransaction` method (around line 245)

Before the gain/loss math, add:
```go
if d.ProceedsPerUnit != nil && lot.EffectiveCostBasisPerUnit() != nil {
    // ... existing computation ...
    detail.RealizedGainLoss = gainLoss
}
// If nil, detail.RealizedGainLoss stays nil → FormatUSD returns "0.00"
```

## Verification

```bash
cd apps/backend && go build ./...
cd apps/backend && go test ./... -v -short
```

Manual: Open wallet → expand asset → expand chain → lots table should show newest lots at top, oldest at bottom. Closed (fully consumed) lots will naturally be at the bottom since FIFO consumes oldest first.
