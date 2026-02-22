# Plan: Link Tax Lots to Transactions

## Context

Tax lots and transactions are already linked in the DB via `tax_lots.transaction_id` and `lot_disposals.transaction_id` FKs, but the UI treats them as completely separate entities. The Transaction Detail page shows only ledger entries; the Cost Basis page shows lots without linking back to transactions. Users cannot see which lots a transaction created or consumed, and cannot override cost basis from the transaction context.

**Goal**: Surface tax lot impact directly in the transaction UI — both in detail view and as expandable rows in the list — with the ability to override cost basis inline.

---

## Iteration 1: Backend — New Endpoint & Force WAC Refresh

### 1.1 Add `GetLotsByTransaction` to repository

**File**: `apps/backend/internal/ledger/taxlot_port.go`
- Add `GetLotsByTransaction(ctx context.Context, txID uuid.UUID) ([]*TaxLot, error)` to `TaxLotRepository` interface

**File**: `apps/backend/internal/infra/postgres/taxlot_repo.go`
- Implement `GetLotsByTransaction` — query `tax_lots WHERE transaction_id = $1 ORDER BY acquired_at ASC`
- Leverages existing index `idx_tax_lots_transaction`

**File**: `apps/backend/internal/ledger/taxlot_fifo_test.go` (mock at line 14)
- Add `GetLotsByTransaction` stub to `mockTaxLotRepo`

### 1.2 New service types and method

**File**: `apps/backend/internal/platform/taxlot/service.go`

New types:
```go
type TransactionLotImpact struct {
    AcquiredLots []*ledger.TaxLot
    Disposals    []*DisposalDetail
    HasLotImpact bool
}

type DisposalDetail struct {
    ledger.LotDisposal
    LotAsset                    string
    LotAcquiredAt               time.Time
    LotEffectiveCostBasisPerUnit *big.Int
    LotAutoSource               ledger.CostBasisSource
}
```

New method `GetLotImpactByTransaction(ctx, userID, txID)`:
1. Call `taxLotRepo.GetLotsByTransaction(txID)` — acquired lots
2. Call `taxLotRepo.GetDisposalsByTransaction(txID)` — disposals
3. For each disposal, call `taxLotRepo.GetTaxLot(disposal.LotID)` to enrich with lot metadata
4. Verify ownership: at least one lot/disposal's account must belong to user's wallet (reuse `verifyLotOwnership`)
5. Set `HasLotImpact = len(acquired) > 0 || len(disposals) > 0`

### 1.3 Force WAC refresh after override

**File**: `apps/backend/internal/platform/taxlot/service.go`

Add `ForceRefreshWAC(ctx)` method — same as `maybeRefreshWAC` but without the 30s throttle check.

Modify `OverrideCostBasis` — after successful `CommitTx`, call `ForceRefreshWAC`. Log warning on failure (non-fatal, WAC will catch up on next read).

### 1.4 HTTP handler and route

**File**: `apps/backend/internal/transport/httpapi/handler/taxlot.go`

Add to `TaxLotServiceInterface`:
```go
GetLotImpactByTransaction(ctx context.Context, userID, txID uuid.UUID) (*taxlot.TransactionLotImpact, error)
```

New response types:
```go
type TransactionLotImpactResponse struct {
    AcquiredLots []TaxLotResponse         `json:"acquired_lots"`
    Disposals    []DisposalDetailResponse  `json:"disposals"`
    HasLotImpact bool                      `json:"has_lot_impact"`
}

type DisposalDetailResponse struct {
    ID               string `json:"id"`
    LotID            string `json:"lot_id"`
    QuantityDisposed string `json:"quantity_disposed"`
    ProceedsPerUnit  string `json:"proceeds_per_unit"`
    DisposalType     string `json:"disposal_type"`
    DisposedAt       string `json:"disposed_at"`
    LotAsset         string `json:"lot_asset"`
    LotAcquiredAt    string `json:"lot_acquired_at"`
    LotCostBasis     string `json:"lot_cost_basis_per_unit"`
    LotAutoSource    string `json:"lot_auto_cost_basis_source"`
}
```

New handler: `GetTransactionLots(w, r)` — parses `{id}` from URL, calls service, maps to response.

**File**: `apps/backend/internal/transport/httpapi/router.go` (line 91-95)

Add route inside the tax lot block:
```go
r.Get("/transactions/{id}/lots", cfg.TaxLotHandler.GetTransactionLots)
```

### 1.5 Verification
- `cd apps/backend && go build ./...`
- `cd apps/backend && go test ./internal/...`
- Manual curl: `GET /api/v1/transactions/{transfer_in_tx_id}/lots` → acquired lots
- Manual curl: `GET /api/v1/transactions/{transfer_out_tx_id}/lots` → disposals
- Manual curl: `PUT /api/v1/lots/{id}/override` → check WAC refreshed immediately

---

## Iteration 2: Frontend — Transaction Detail + Override

### 2.1 New types

**File**: `apps/frontend/src/types/taxlot.ts`

Add:
```typescript
export interface DisposalDetail {
  id: string
  lot_id: string
  quantity_disposed: string
  proceeds_per_unit: string
  disposal_type: 'sale' | 'internal_transfer' | 'gas_fee'
  disposed_at: string
  lot_asset: string
  lot_acquired_at: string
  lot_cost_basis_per_unit: string
  lot_auto_cost_basis_source: CostBasisSource
}

export interface TransactionLotImpact {
  acquired_lots: TaxLot[]
  disposals: DisposalDetail[]
  has_lot_impact: boolean
}
```

### 2.2 API service method

**File**: `apps/frontend/src/services/taxlot.ts`

Add:
```typescript
async getTransactionLots(transactionId: string): Promise<TransactionLotImpact> {
  const response = await api.get<TransactionLotImpact>(`/transactions/${transactionId}/lots`)
  return response.data
}
```

### 2.3 New hook

**File**: `apps/frontend/src/hooks/useTaxLots.ts`

Add `useTransactionLots(transactionId: string)` — query key `['transaction-lots', transactionId]`.

Update `useOverrideCostBasis` `onSuccess` to also invalidate `['transaction-lots']`.

### 2.4 New component: `TransactionLotImpactSection`

**File**: `apps/frontend/src/features/transactions/TransactionLotImpactSection.tsx`

Structure:
- Calls `useTransactionLots(transactionId)`
- Returns `null` if no lot impact or loading
- **Acquired Lots card** — reuses column pattern from `LotDetailTable.tsx`:
  - Columns: #, Acquired, Qty Acquired, Remaining, Cost/Unit (with override indicator), Source, Edit button
  - Edit button opens `CostBasisOverrideDialog` (reuse existing component)
- **Consumed Lots (FIFO) card** — for disposals:
  - Columns: Lot Acquired Date, Asset, Qty Disposed, Cost Basis, Proceeds, Gain/Loss
  - Gain/loss per disposal: display-only (proceeds_per_unit - cost_basis) indicator
- Source badge variants reused from `LotDetailTable.tsx` (line 24-29)

### 2.5 Integrate into TransactionDetailPage

**File**: `apps/frontend/src/features/transactions/TransactionDetailPage.tsx`

After `<LedgerEntriesTable entries={transaction.entries} />` (line 128), add:
```tsx
<TransactionLotImpactSection transactionId={transaction.id} />
```

### 2.6 Verification
- `cd apps/frontend && bun run build`
- `cd apps/frontend && bun run lint`
- `cd apps/frontend && bun run test --run`
- Navigate to `/transactions/{id}` for a `transfer_in` → see "Acquired Lots" section
- Navigate to `/transactions/{id}` for a `transfer_out` → see "Consumed Lots" section
- Transaction with no lot impact → no extra section visible
- Override cost basis from lot → dialog opens, WAC updates immediately

---

## Iteration 3: Frontend — Expandable Rows in Transaction List

### 3.1 Add expandable row state

**File**: `apps/frontend/src/features/transactions/TransactionsPage.tsx`

Follow the pattern from `CostBasisPage.tsx` (lines 31-45, expandedAssets state):
- Add `expandedRows: Set<string>` state
- Add chevron column (first column)
- On row click, toggle expanded state instead of navigating (keep a separate "View" link or arrow)
- When expanded, render nested `<TableRow>` with `<TransactionLotImpactSection transactionId={tx.id} />`

**Note**: Currently each row is wrapped in `<Link to={/transactions/${tx.id}}>`. Need to restructure so chevron toggles expand while the row itself still navigates.

Design: Add a small chevron button in the first cell. Click on chevron = expand. Click elsewhere on row = navigate to detail (existing behavior preserved).

### 3.2 Verification
- Click chevron on a transaction row → lot section expands inline
- Click on row (not chevron) → navigates to detail page
- Expanding doesn't break pagination or filters
- Collapsing clears the expanded state

---

## Key Files Summary

| File | Change |
|------|--------|
| `apps/backend/internal/ledger/taxlot_port.go` | Add `GetLotsByTransaction` to interface |
| `apps/backend/internal/infra/postgres/taxlot_repo.go` | Implement `GetLotsByTransaction` |
| `apps/backend/internal/ledger/taxlot_fifo_test.go` | Add stub to mock (line 14-17) |
| `apps/backend/internal/platform/taxlot/service.go` | Add `GetLotImpactByTransaction`, `ForceRefreshWAC`, modify `OverrideCostBasis` |
| `apps/backend/internal/transport/httpapi/handler/taxlot.go` | Add `GetTransactionLots` handler + response types, extend service interface |
| `apps/backend/internal/transport/httpapi/router.go` | Add `GET /transactions/{id}/lots` route |
| `apps/frontend/src/types/taxlot.ts` | Add `DisposalDetail`, `TransactionLotImpact` types |
| `apps/frontend/src/services/taxlot.ts` | Add `getTransactionLots` method |
| `apps/frontend/src/hooks/useTaxLots.ts` | Add `useTransactionLots`, update invalidation |
| `apps/frontend/src/features/transactions/TransactionLotImpactSection.tsx` | **New** — lot impact section component |
| `apps/frontend/src/features/transactions/TransactionDetailPage.tsx` | Integrate `TransactionLotImpactSection` |
| `apps/frontend/src/features/transactions/TransactionsPage.tsx` | Add expandable rows (Iteration 3) |

## Reusable Existing Code

- `CostBasisOverrideDialog` (`components/domain/CostBasisOverrideDialog.tsx`) — reuse as-is
- `sourceBadgeVariants` from `LotDetailTable.tsx:24-29` — extract to shared constant or duplicate
- `formatUSD`, `formatCrypto`, `formatDate` from `lib/format` — reuse
- `toTaxLotResponse` helper in `handler/taxlot.go:240` — reuse for acquired lots serialization
- `money.FormatUSD`, `money.FromBaseUnits` from `pkg/money` — reuse for disposal formatting

---

## Agent Team Structure

| Agent | Type | Stream | Tasks |
|-------|------|--------|-------|
| `backend` | general-purpose | A | Iteration 1 — all backend changes |
| `frontend` | general-purpose | B | Iteration 2 — detail page integration |
| `frontend-list` | general-purpose | C (after B) | Iteration 3 — expandable rows in list |
| `validator` | general-purpose | After A+B | E2E verification: build, lint, test, manual checks |

**Parallelism**: Streams A and B can run in parallel (backend and frontend-detail). Stream C depends on `TransactionLotImpactSection` from B. Validator runs after A+B merge.

---

## End-to-End Verification

1. `just dev` — start backend + frontend
2. Sync a wallet (or use existing synced data)
3. Open `/transactions` — see transaction list
4. Click chevron on `transfer_in` → see acquired lot inline (Iteration 3)
5. Navigate to `/transactions/{id}` for `transfer_in` → see "Acquired Lots" card with override button
6. Click override → `CostBasisOverrideDialog` opens → apply override → toast success
7. Navigate to `/cost-basis` → WAC reflects override immediately
8. Navigate to `/transactions/{id}` for `transfer_out` → see "Consumed Lots (FIFO)" card with gain/loss
9. Transaction with no lot impact → no lot section visible
