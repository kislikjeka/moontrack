# Lending Frontend Integration Design

**Date:** 2026-03-14
**Status:** Approved

## Problem

Backend has full AAVE/lending support (5 transaction types, lending positions, API endpoints) but the frontend is completely unaware — no types, no UI, no transaction badges/filters.

## Scope

- Read-only display (no manual creation of lending transactions)
- Lending positions in wallet detail page (new tab, like LP)
- Minimal card info: asset, balance, status
- Transaction type support: badges + filters for all 5 lending types

## Approach

Mirror the LP Positions pattern with separate components (no shared DeFi abstraction).

## Design

### 1. Types

**`types/lendingPosition.ts`**
```ts
export interface LendingPosition {
  id: string
  wallet_id: string
  chain_id: string
  protocol: string
  supply_asset: string
  supply_amount: string
  supply_decimals: number
  borrow_asset?: string
  borrow_amount: string
  borrow_decimals?: number
  status: 'active' | 'closed'
  opened_at: string
  closed_at?: string
}
```

**`types/transaction.ts`** — add to `TransactionType` union:
- `lending_supply`, `lending_withdraw`, `lending_borrow`, `lending_repay`, `lending_claim`

### 2. Service & Hook

**`services/lendingPosition.ts`**
- `getLendingPositions(walletId, status?)` → `GET /api/v1/lending/positions?wallet_id=...&status=...`
- `getLendingPosition(id)` → `GET /api/v1/lending/positions/{id}`

**`hooks/useLendingPositions.ts`**
- `useLendingPositions(walletId, status?)` — query key `['lending-positions', walletId, status]`

### 3. Components

**`LendingPositionCard`** (expandable, like LP):
- Collapsed: supply asset icon + name, protocol, status badge (active/closed), chain icon, current supply balance
- If borrow exists: show borrow asset + balance
- Expanded: opened_at, closed_at

**`LendingPositionsSection`** (like LP):
- Title "Lending Positions" with Landmark icon
- Tabs: All / Active / Closed
- Sort: active first, then by opened_at desc
- Standard loading/error/empty states

**`TransactionTypeBadge`** — add 5 lending entries:
- `lending_supply` → "Supply" (ArrowDownToLine, liquidity)
- `lending_withdraw` → "Withdraw" (ArrowUpFromLine, liquidity)
- `lending_borrow` → "Borrow" (ArrowDownLeft, profit)
- `lending_repay` → "Repay" (ArrowUpRight, loss)
- `lending_claim` → "Claim" (Coins, liquidity)

**`TransactionFilters`** — add all lending + LP types to filter list.

### 4. Integration

**`WalletDetailPage`** — new tab "Lending" after "LP Positions":
```
Holdings | Transactions | LP Positions | Lending
```
`<LendingPositionsSection walletId={id!} />` in TabsContent.
