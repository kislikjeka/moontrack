# Reusable TransactionListTable with Tax Lot Info

## Context

The wallet detail page (`/wallets/:id`) has a Transactions tab that shows a simplified table without expandable tax lot info. The main Transactions page (`/transactions`) has full-featured expandable rows with `TransactionLotImpactSection` (acquired lots, consumed lots FIFO, cost basis override). The user wants the wallet tab to have the same tax lot capabilities, and the solution should be a shared component to avoid duplication.

Currently ~120 lines of table rendering logic (expand/collapse, row cells, lot impact section, pagination) are duplicated or missing between `TransactionsPage.tsx` and `WalletTransactions.tsx`.

## Plan

### Step 1: Create `TransactionListTable.tsx`

**New file:** `apps/frontend/src/features/transactions/TransactionListTable.tsx`

Extract the shared table from `TransactionsPage.tsx` (lines 81-205) into a presentational component:

```typescript
interface TransactionListTableProps {
  transactions: TransactionListItem[]
  total?: number
  page?: number
  pageSize?: number
  onPageChange?: (page: number) => void
  showWalletColumn?: boolean  // default: true
}
```

- Manages `expandedRows` state internally (purely visual, no consumer needs it)
- Renders Table with configurable columns (Wallet column conditional via `showWalletColumn`)
- Expanded rows render `TransactionLotImpactSection` (already reusable, no changes needed)
- Pagination shown only when `total` and `onPageChange` are provided and `total > pageSize`
- No Card wrapper — consumers compose their own chrome around it

### Step 2: Add `showWalletFilter` prop to `TransactionFilters.tsx`

**File:** `apps/frontend/src/features/transactions/TransactionFilters.tsx`

```typescript
interface TransactionFiltersProps {
  filters: FiltersType
  onFiltersChange: (filters: FiltersType) => void
  showWalletFilter?: boolean  // default: true
}
```

Wrap the wallet `<Select>` in `{showWalletFilter !== false && (...)}`. Backward-compatible — existing usage unchanged.

### Step 3: Refactor `TransactionsPage.tsx`

**File:** `apps/frontend/src/features/transactions/TransactionsPage.tsx`

- Remove inline table, `expandedRows` state, `toggleRow` function
- Replace with `<TransactionListTable>` inside existing Card
- Keep: page header, TransactionFilters, empty state, skeleton — unchanged
- Visual output identical to current

### Step 4: Refactor `WalletDetailPage.tsx`

**File:** `apps/frontend/src/features/wallets/WalletDetailPage.tsx`

- Add local `txFilters` state with `wallet_id` pinned to the wallet id
- Add `<TransactionFilters showWalletFilter={false}>` in Transactions tab
- Replace `<WalletTransactions>` with Card + `<TransactionListTable showWalletColumn={false}>`
- Add pagination support via `txFilters` state

### Step 5: Delete `WalletTransactions.tsx`

**File:** `apps/frontend/src/features/wallets/WalletTransactions.tsx` — delete

Fully replaced by `TransactionListTable` used directly in `WalletDetailPage`.

## Files

| File | Action |
|------|--------|
| `apps/frontend/src/features/transactions/TransactionListTable.tsx` | Create |
| `apps/frontend/src/features/transactions/TransactionFilters.tsx` | Edit (add `showWalletFilter` prop) |
| `apps/frontend/src/features/transactions/TransactionsPage.tsx` | Refactor (use TransactionListTable) |
| `apps/frontend/src/features/wallets/WalletDetailPage.tsx` | Refactor (use TransactionListTable + filters) |
| `apps/frontend/src/features/wallets/WalletTransactions.tsx` | Delete |

No backend changes. No changes to: `TransactionLotImpactSection.tsx`, hooks, services, types.

## Verification

1. `cd apps/frontend && bun run build` — no compilation errors
2. Open `/transactions` — expandable rows with tax lot info work as before
3. Open `/wallets/:id` → Transactions tab — expandable rows with tax lot info now present
4. Wallet tab shows type filter only (no wallet filter), no Wallet column
5. Pagination works in both contexts
6. Cost basis override dialog works from both locations
