# LP Positions Frontend — Design Document

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement the corresponding plan task-by-task.

**Goal:** Display LP (Liquidity Pool) positions in the wallet detail page with expandable cards, status filtering, and LP transaction type badges.

**Backend API (already implemented):**
- `GET /lp/positions?wallet_id=...&status=...&chain_id=...` — list positions
- `GET /lp/positions/{id}` — single position detail

---

## Architecture Decisions

1. **Navigation**: LP positions live as a section inside Wallet Detail (`/wallets/:id`), not a separate page — positions belong to wallets
2. **Display**: Cards with inline expand (not table, not modal, not separate page)
3. **Component location**: Inside `features/wallets/components/` (Approach A), not a separate feature module
4. **Closed positions**: Visible by default with All/Open/Closed filter toggle

---

## Data Layer

### TypeScript Types (`types/lpPosition.ts`)

```typescript
export interface LPPosition {
  id: string
  wallet_id: string
  chain_id: string
  protocol: string
  nft_token_id?: string
  contract_address?: string
  token0_symbol: string
  token1_symbol: string
  token0_decimals: number
  token1_decimals: number
  total_deposited_usd: string
  total_withdrawn_usd: string
  total_claimed_fees_usd: string
  remaining_token0: string
  remaining_token1: string
  status: 'open' | 'closed'
  opened_at: string
  closed_at?: string
  realized_pnl_usd?: string
  apr_bps?: number
}
```

All monetary values are strings (big numbers for financial precision).

### API Service (`services/lpPosition.ts`)

- `list(walletId: string, status?: string)` → GET `/lp/positions?wallet_id=...`
- `getById(id: string)` → GET `/lp/positions/{id}`

### Query Hook (`hooks/useLPPositions.ts`)

- `useLPPositions(walletId: string, status?: string)` — TanStack Query, 2-min stale time
- Query key: `['lp-positions', walletId, status]`

---

## Components

### LPPositionsSection

Location: `features/wallets/components/LPPositionsSection.tsx`

Embedded in `WalletDetailPage` after the Holdings section.

**Structure:**
- Header: "LP Positions" with count badge
- Filter: All / Open / Closed toggle buttons (shadcn Tabs or button group)
- List of `LPPositionCard` components
- Open positions sorted first, then closed

**States:**
- Loading: 2-3 Skeleton cards
- Empty: "No LP positions found" text (no CTA — positions come from sync)
- Error: standard alert

### LPPositionCard

Location: `features/wallets/components/LPPositionCard.tsx`

**Collapsed state (default):**
```
┌──────────────────────────────────────────────┐
│ [USDC] [ETH]  USDC / ETH    [Open]  [ETH]  ▼│
│ Uniswap V3                                   │
│                                               │
│ Deposited     Fees Earned     APR             │
│ $10,000       $500            2.50%           │
└──────────────────────────────────────────────┘
```

- Token pair with AssetIcon components
- Status badge: Open (green/profit variant), Closed (gray/secondary variant)
- ChainIcon for chain
- Protocol name in muted text
- 3 key metrics: Deposited USD, Fees Claimed USD, APR (bps / 100 → %)

**Expanded state (on click):**

Additional section below collapsed content:

```
│──────────────────────────────────────────────│
│ Deposited    Withdrawn    Fees Claimed       │
│ $10,000      $8,000       $500               │
│                                               │
│ Remaining Tokens:                             │
│ USDC   2,100.00                              │
│ ETH    1.020000                              │
│                                               │
│ Opened: Jun 15, 2025                         │
│ NFT ID: #12345                               │
│ PnL: +$500.00  (closed positions only)       │
└──────────────────────────────────────────────┘
```

- Full USD breakdown: deposited / withdrawn / fees
- Remaining tokens formatted using decimals from API
- Dates: opened_at, closed_at (for closed)
- NFT Token ID if present
- Realized PnL for closed positions (using PnLValue component)

---

## TransactionTypeBadge Updates

Add 3 new entries to `typeConfig` in `TransactionTypeBadge.tsx`:

| Type | Label | Icon | Variant |
|------|-------|------|---------|
| `lp_deposit` | LP Deposit | `ArrowDownToLine` | `liquidity` |
| `lp_withdraw` | LP Withdraw | `ArrowUpFromLine` | `liquidity` |
| `lp_claim_fees` | LP Claim | `Coins` | `liquidity` |

Update `TransactionType` in `types/transaction.ts` to include the 3 new types.

---

## Formatting

Add to `lib/format.ts`:

```typescript
formatTokenAmount(value: string, decimals: number): string
```

Converts raw big number string to human-readable token amount using the token's decimal precision.

APR formatting: `apr_bps / 100` → display as percentage (e.g., 250 bps → "2.50%").

---

## Files to Create/Modify

| Action | File |
|--------|------|
| Create | `types/lpPosition.ts` |
| Create | `services/lpPosition.ts` |
| Create | `hooks/useLPPositions.ts` |
| Create | `features/wallets/components/LPPositionsSection.tsx` |
| Create | `features/wallets/components/LPPositionCard.tsx` |
| Modify | `types/transaction.ts` — add LP types |
| Modify | `components/domain/TransactionTypeBadge.tsx` — add LP badges |
| Modify | `features/wallets/WalletDetailPage.tsx` — embed LPPositionsSection |
| Modify | `lib/format.ts` — add formatTokenAmount |
