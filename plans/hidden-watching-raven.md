# Fix: Tax Lots Sorting & Genesis Date Bug

## Context

Two bugs in the Holdings tax lots view:

1. **Lots out of order** — `GetLotsByWallet` (taxlot/service.go:65-87) iterates multiple chain accounts, fetches sorted lots per-account, then concatenates. The combined slice loses chronological order when lots span multiple chains.

2. **Genesis date "01.01.1"** — Genesis handler (module/genesis/handler.go) doesn't set `OccurredAt` on entries. TaxLotHook (taxlot_hook.go:151) copies `entry.OccurredAt` → `lot.AcquiredAt`, resulting in Go zero time `0001-01-01`. Other handlers (transfer, swap) correctly set `OccurredAt: txn.OccurredAt` from their data structs, but genesis struct lacks this field.

## Changes

### 1. Genesis handler — add OccurredAt to entries

**`apps/backend/internal/module/genesis/handler.go`**
- Add `OccurredAt time.Time` to `GenesisBalanceTransaction` struct
- Set `OccurredAt: txn.OccurredAt` on both entries in `generateEntries()`

**`apps/backend/internal/platform/sync/processor.go`** (~line 185-192)
- Add `"occurred_at": raw.MinedAt.Format(time.RFC3339)` to rawData map for genesis

The handler already does JSON marshal/unmarshal from rawData, so the `occurred_at` field will populate the struct automatically.

### 2. Sort combined lots in backend

**`apps/backend/internal/platform/taxlot/service.go`**

After the accounts loop in `GetLotsByWallet()` (after line 84), add:
```go
sort.Slice(allLots, func(i, j int) bool {
    return allLots[i].AcquiredAt.Before(allLots[j].AcquiredAt)
})
```
Add `"sort"` to imports.

### 3. Defensive sort in frontend

**`apps/frontend/src/components/domain/LotDetailTable.tsx`**

Before the `.map()` at line 72, sort lots:
```tsx
const sorted = [...lots].sort((a, b) =>
  new Date(a.acquired_at).getTime() - new Date(b.acquired_at).getTime()
)
```
Map over `sorted` instead of `lots`.

### 4. Fix existing data (migration)

**`apps/backend/migrations/000020_fix_genesis_lot_dates.up.sql`**

Update existing genesis lots to use the transaction's `occurred_at`:
```sql
UPDATE tax_lots tl
SET acquired_at = t.occurred_at
FROM transactions t
WHERE tl.transaction_id = t.id
  AND t.type = 'genesis_balance'
  AND tl.acquired_at < '0002-01-01';
```

Down migration: no-op (we don't want to revert to broken dates).

## Files to Modify

| File | Change |
|------|--------|
| `apps/backend/internal/module/genesis/handler.go` | Add `OccurredAt` to struct + set on entries |
| `apps/backend/internal/platform/sync/processor.go` | Add `occurred_at` to genesis rawData |
| `apps/backend/internal/platform/taxlot/service.go` | Sort combined lots by `AcquiredAt` |
| `apps/frontend/src/components/domain/LotDetailTable.tsx` | Defensive frontend sort |
| `apps/backend/migrations/000020_fix_genesis_lot_dates.up.sql` | Fix existing broken dates |
| `apps/backend/migrations/000020_fix_genesis_lot_dates.down.sql` | No-op down migration |

## Verification

1. `cd apps/backend && go build ./...`
2. `cd apps/backend && go test ./internal/platform/sync/... -v`
3. `cd apps/backend && go test ./internal/ledger/... -v`
4. `just migrate-up` — apply data fix for existing lots
5. UI: verify lots display in chronological order with correct genesis dates
