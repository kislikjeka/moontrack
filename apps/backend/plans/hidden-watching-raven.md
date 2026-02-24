# Fix: Tax Lots Sorting — Open First + Deterministic Order

## Context

The Holdings tax lots view shows lots in a confusing order. Two issues:

1. **Fully-disposed lots (remaining=0) mixed with open lots** — Users care about open lots first. Closed lots are historical noise and should be pushed to the bottom.

2. **Non-deterministic sort for same-timestamp lots** — Multiple lots can have identical `acquired_at` (same block/second). The DB queries use `ORDER BY acquired_at ASC` with no tiebreaker, so ordering is unstable. The Go `sort.Slice` also has no tiebreaker. The JSON serialization truncates to second precision (line 330 of `taxlot.go`), losing any sub-second differentiation the frontend could use.

Genesis cost basis ($1,850 "approximation") is **correct by design** — no change needed.

## Root Cause (5 Why)

**Why are lots in confusing order?**
→ Because closed lots (remaining=0) appear above open lots
→ Because there's no open/closed grouping in the sort
→ Because the sort only considers `acquired_at` timestamp
→ Because the original design assumed chronological-only sorting was sufficient
→ **Root cause**: Sort needs two-tier grouping (open first) + deterministic tiebreaker

**Why is ordering inconsistent across refreshes?**
→ Because multiple lots share the same `acquired_at` second
→ Because SQL `ORDER BY acquired_at ASC` is non-deterministic for equal timestamps
→ Because there's no secondary sort key (`id`, `created_at`)
→ **Root cause**: Missing tiebreaker column in ORDER BY

## Changes

### 1. DB queries: add `created_at ASC, id ASC` tiebreaker

**`apps/backend/internal/infra/postgres/taxlot_repo.go`**

Three queries need a secondary sort:

- `GetOpenLotsFIFO` (line 158): `ORDER BY acquired_at ASC` → `ORDER BY acquired_at ASC, created_at ASC, id ASC`
- `GetLotsByAccount` (line 201): same change
- `GetLotsByTransaction` (line 224): same change

Using `created_at ASC, id ASC` as tiebreaker — `created_at` reflects insertion order (which matches FIFO within the same block), `id` is the final tiebreaker for absolute determinism.

### 2. Service-level sort: open lots first + tiebreaker

**`apps/backend/internal/platform/taxlot/service.go`** (line 87-89)

Replace simple date sort:
```go
sort.Slice(allLots, func(i, j int) bool {
    iOpen := allLots[i].QuantityRemaining.Sign() > 0
    jOpen := allLots[j].QuantityRemaining.Sign() > 0
    if iOpen != jOpen {
        return iOpen // open lots first
    }
    if !allLots[i].AcquiredAt.Equal(allLots[j].AcquiredAt) {
        return allLots[i].AcquiredAt.Before(allLots[j].AcquiredAt)
    }
    return allLots[i].CreatedAt.Before(allLots[j].CreatedAt)
})
```

### 3. Frontend defensive sort: match backend logic

**`apps/frontend/src/components/domain/LotDetailTable.tsx`** (line 72)

```tsx
{[...lots].sort((a, b) => {
  const aOpen = parseFloat(a.quantity_remaining) > 0 ? 0 : 1
  const bOpen = parseFloat(b.quantity_remaining) > 0 ? 0 : 1
  if (aOpen !== bOpen) return aOpen - bOpen
  return new Date(a.acquired_at).getTime() - new Date(b.acquired_at).getTime()
}).map((lot, index) => {
```

Frontend can't tiebreak by `created_at` (not in the response), but `acquired_at` to the second + backend pre-sorting means this is sufficient as a defensive measure.

## Files to Modify

| File | Change |
|------|--------|
| `apps/backend/internal/infra/postgres/taxlot_repo.go` | Add `created_at ASC, id ASC` to 3 ORDER BY clauses |
| `apps/backend/internal/platform/taxlot/service.go` | Two-tier sort: open first, then by date+createdAt tiebreaker |
| `apps/frontend/src/components/domain/LotDetailTable.tsx` | Defensive sort: open first, then by date |

No migration needed — this is a query/sort-only change.

## Verification

1. `cd apps/backend && go build ./...`
2. `cd apps/backend && go test ./internal/ledger/... -v -short`
3. `cd apps/backend && go test ./internal/platform/sync/... -v -short`
4. UI: verify open lots appear at top, closed lots at bottom, both groups in chronological order
