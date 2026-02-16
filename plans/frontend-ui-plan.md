# Frontend UI Plan: Zerion Integration & Lot-Based Cost Tracking

**Date:** 2026-02-07
**Status:** Draft
**References:** ADR-001 (Zerion), ADR Lot-Based Cost Basis, PRD Blockchain Data Integration, PRD Lot-Based Cost Tracking

---

## 1. Current State Analysis

### Existing Pages & Routes
| Route | Page | Description |
|-------|------|-------------|
| `/dashboard` | DashboardPage | Stats cards, portfolio summary, asset distribution chart, wallets list, recent transactions |
| `/wallets` | WalletsPage | List of all wallets with WalletCard components |
| `/wallets/:id` | WalletDetailPage | Wallet header + sync status, total value, tabs (Assets / Transactions) |
| `/transactions` | TransactionsPage | Table with filters (wallet, type), pagination |
| `/transactions/new` | TransactionFormPage | Manual asset_adjustment form |
| `/transactions/:id` | TransactionDetailPage | Transaction header, details card, ledger entries table |
| `/settings` | SettingsPage | Profile and password sections |
| `/login`, `/register` | Auth pages | JWT login/register |

### Design System
- **UI Library**: shadcn/ui (Card, Table, Badge, Dialog, Tabs, Select, Button, etc.)
- **Styling**: Tailwind CSS with CSS custom properties for theming
- **Design tokens**: Light/dark themes in `globals.css` with semantic colors (`--profit`, `--loss`, `--tx-swap`, `--tx-liquidity`, etc.)
- **Badge variants**: Already has `profit`, `loss`, `swap`, `liquidity`, `gmPool`, `transfer`, `bridge`
- **Fonts**: Inter (sans), JetBrains Mono (mono)
- **Domain components**: `PnLValue` (with color coding), `StatCard`, `AssetIcon`, `TransactionTypeBadge`, `WalletCard`, `AddressDisplay`, `SyncStatusBadge`

### API Integration Patterns
- **HTTP client**: Axios with JWT interceptor (`services/api.ts`)
- **Data fetching**: TanStack Query hooks in `hooks/` directory
- **Services**: `services/portfolio.ts`, `services/transaction.ts`, `services/wallet.ts`, `services/auth.ts`, `services/asset.ts`
- **Types**: Centralized in `types/` with barrel export
- **Pattern**: `useQuery` for reads, `useMutation` with `invalidateQueries` for writes
- **USD values**: Stored as scaled integers (x 10^8), formatted in components via `BigInt(value) / 100000000`

### Current Transaction Types
Only 4 types: `transfer_in`, `transfer_out`, `internal_transfer`, `asset_adjustment`

---

## 2. New Routes & Pages

### 2.1 DeFi Positions Page

**Route:** `/defi-positions`
**Sidebar:** Add "DeFi" nav item (between Wallets and Transactions)

**Layout:**
```
┌─────────────────────────────────────────────────────────────┐
│  DeFi Positions                              [Wallet filter] │
│  Active positions across your wallets                        │
├─────────────────────────────────────────────────────────────┤
│  Summary Stats (3 cards):                                    │
│  ┌──────────┐  ┌──────────────────┐  ┌───────────────┐     │
│  │ Total    │  │ Active Positions │  │ Protocols     │     │
│  │ Value    │  │ 12               │  │ 4             │     │
│  │ $45,230  │  │                  │  │               │     │
│  └──────────┘  └──────────────────┘  └───────────────┘     │
├─────────────────────────────────────────────────────────────┤
│  Position Cards (grouped by protocol):                       │
│                                                              │
│  Uniswap V3                                                  │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ ETH/USDC Pool                                        │   │
│  │ Protocol: Uniswap V3  |  Chain: Ethereum  |  LP      │   │
│  │                                                       │   │
│  │ Underlying:  1.5 ETH ($3,750)  +  3,750 USDC        │   │
│  │ LP Token:    0.0045                                   │   │
│  │ Total Value: $7,500                                   │   │
│  │ Wallet: My Main Wallet                                │   │
│  └──────────────────────────────────────────────────────┘   │
│                                                              │
│  GMX V2                                                      │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ GM: ETH/USDC                                         │   │
│  │ Protocol: GMX V2  |  Chain: Arbitrum  |  GM Pool      │   │
│  │                                                       │   │
│  │ GM Tokens: 150.5 ($12,040)                           │   │
│  │ Wallet: Arbitrum Wallet                               │   │
│  └──────────────────────────────────────────────────────┘   │
│                                                              │
│  Empty state: "No DeFi positions found. Positions will       │
│  appear after your wallets sync DeFi activity."              │
└─────────────────────────────────────────────────────────────┘
```

**Data source:** `GET /portfolio/defi-positions` (new endpoint, returns Zerion positions)

### 2.2 Cost Basis / Tax Lots Page

**Route:** `/cost-basis`
**Sidebar:** Add "Cost Basis" nav item (after Transactions)

**Layout:**
```
┌─────────────────────────────────────────────────────────────┐
│  Cost Basis                       [Wallet filter] [Asset ▾] │
│  Track cost basis and tax lots for your assets               │
├─────────────────────────────────────────────────────────────┤
│  Asset Overview Table:                                       │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ Asset  │ Holdings │  WAC   │ Current │ Unreal. PnL  │   │
│  │ ETH    │ 5.2      │ $2,100 │ $2,500  │ +$2,080      │   │
│  │ USDC   │ 10,500   │ $1.00  │ $1.00   │ $0           │   │
│  │ ARB    │ 500      │ $1.20  │ $1.45   │ +$125        │   │
│  └──────────────────────────────────────────────────────┘   │
│  (Click row to expand lot detail)                            │
│                                                              │
│  Expanded Lot Detail (for ETH):                              │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ Lot #  │ Acquired   │ Qty   │ Remain │ Cost/Unit     │   │
│  │        │            │       │        │ Eff. │ Source  │   │
│  │──────────────────────────────────────────────────────│   │
│  │ 1      │ 01.01.2025 │ 3.0   │ 2.0    │ $1,800 │ swap │   │
│  │        │ Swap via Uniswap V3                  [Edit]  │   │
│  │ 2      │ 15.01.2025 │ 5.0   │ 3.2    │ $2,300 │ fmv  │   │
│  │        │ Transfer from Binance                [Edit]  │   │
│  └──────────────────────────────────────────────────────┘   │
│                                                              │
│  Empty state: "No tax lots yet. Cost basis is tracked        │
│  automatically when assets are acquired through swaps        │
│  or transfers."                                              │
└─────────────────────────────────────────────────────────────┘
```

### 2.3 PnL Report Page

**Route:** `/pnl`
**Sidebar:** Add "PnL" nav item (after Cost Basis)

**Layout:**
```
┌─────────────────────────────────────────────────────────────┐
│  Profit & Loss                  [Date range] [Asset filter] │
│  Realized and unrealized gains/losses                        │
├─────────────────────────────────────────────────────────────┤
│  Summary Cards:                                              │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │ Realized PnL │  │ Unrealized   │  │ Total PnL    │      │
│  │ +$1,250      │  │ +$2,080      │  │ +$3,330      │      │
│  │ ▲ 15 trades  │  │ 3 assets     │  │              │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
├─────────────────────────────────────────────────────────────┤
│  Tabs: [Realized] [Unrealized]                               │
│                                                              │
│  Realized Tab (disposal history):                            │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ Date       │ Asset │ Qty  │ Cost  │ Proceeds │ PnL   │   │
│  │ 05.02.2025 │ ETH   │ 1.0  │$1,800 │ $2,500   │+$700  │   │
│  │ 03.02.2025 │ ARB   │ 100  │$120   │ $145     │+$25   │   │
│  └──────────────────────────────────────────────────────┘   │
│                                                              │
│  Unrealized Tab (current positions):                         │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ Asset │ Holdings │ Avg Cost │ Current │ Unreal. PnL  │   │
│  │ ETH   │ 5.2      │ $2,100   │ $2,500  │ +$2,080      │   │
│  │ ARB   │ 500      │ $1.20    │ $1.45   │ +$125        │   │
│  └──────────────────────────────────────────────────────┘   │
│                                                              │
│  Empty state: "No PnL data yet. PnL is calculated when      │
│  you sell or swap assets."                                   │
└─────────────────────────────────────────────────────────────┘
```

---

## 3. Modified Pages

### 3.1 Transaction History Page (`TransactionsPage`)

**Changes:**
1. **Extend `TransactionType`** to include new DeFi types: `swap`, `defi_deposit`, `defi_withdraw`, `defi_claim`
2. **Update `TransactionTypeBadge`** to render new types with proper icons and badge variants:
   - `swap` → swap variant (green), icon: `ArrowLeftRight` or `Repeat`
   - `defi_deposit` → liquidity variant (purple), icon: `Download` or `CirclePlus`
   - `defi_withdraw` → liquidity variant (purple), icon: `Upload` or `CircleMinus`
   - `defi_claim` → profit variant (green), icon: `Gift` or `Coins`
3. **Add protocol badge** to transaction rows — small text badge next to type showing protocol name (e.g., "Uniswap V3", "GMX")
4. **Update `TransactionFilters`** — add new types to the type filter dropdown
5. **Transaction table** — for swap transactions, show both assets (e.g., "1 ETH → 2,500 USDC" instead of single asset)

**Mockup for swap row:**
```
| Type              | Wallet     | Asset           | Amount            | Value    | Date       |
| [Swap] Uniswap   | Main       | ETH → USDC      | -1 ETH / +2500   | $2,500   | 05.02.2025 |
```

### 3.2 Transaction Detail Page (`TransactionDetailPage`)

**Changes:**
1. **Protocol info section** — for DeFi transactions, show:
   - Protocol name and icon/badge
   - Operation type (human-readable)
   - Chain name
2. **Swap detail layout** — for swaps, show "Sold" and "Bought" sections side by side:
   ```
   ┌──────────────────┐    ┌──────────────────┐
   │ Sold             │    │ Bought           │
   │ 1 ETH            │ →  │ 2,500 USDC       │
   │ @ $2,500         │    │ @ $1.00          │
   └──────────────────┘    └──────────────────┘
   ```
3. **Tax lot link** — show "View Tax Lot" link that navigates to cost-basis page filtered to this asset
4. **Cost basis info** — show cost basis and realized PnL if this transaction involved a disposal

### 3.3 Dashboard Page (`DashboardPage`)

**Changes:**
1. **Add PnL stat card** — replace or add 5th stat card showing total unrealized PnL (using existing `PnLValue` component with green/red coloring)
2. **Portfolio summary enhancement** — add "Cost Basis" and "Unrealized PnL" rows to the PortfolioSummary card
3. **Quick links** — add link to DeFi Positions page in the navigation buttons area

### 3.4 Wallet Detail Page (`WalletDetailPage`)

**Changes:**
1. **Add "DeFi" tab** alongside existing "Assets" and "Transactions" tabs — shows DeFi positions for this specific wallet
2. **Enhance Assets table** — add columns:
   - **Avg Cost (WAC)**: weighted average cost per unit
   - **Unrealized PnL**: current value minus cost basis
3. **Cost basis info** — each asset row links to cost-basis page filtered by this wallet + asset

### 3.5 Sidebar Navigation (`Sidebar`)

**Changes:** Add new nav items:
```typescript
const navItems = [
  { to: '/dashboard', label: 'Dashboard', icon: LayoutDashboard },
  { to: '/wallets', label: 'Wallets', icon: Wallet },
  { to: '/defi-positions', label: 'DeFi', icon: Layers },       // NEW
  { to: '/transactions', label: 'Transactions', icon: ArrowLeftRight },
  { to: '/cost-basis', label: 'Cost Basis', icon: Calculator },   // NEW
  { to: '/pnl', label: 'PnL', icon: TrendingUp },                // NEW
  { to: '/settings', label: 'Settings', icon: Settings },
]
```

---

## 4. New Components

### 4.1 DeFi Position Card (`components/domain/DefiPositionCard.tsx`)

Renders a single DeFi position. Used on DeFi Positions page and Wallet Detail DeFi tab.

```typescript
interface DefiPositionCardProps {
  position: DefiPosition
  className?: string
}

// Shows: protocol badge, chain badge, position type (LP/Staking/Lending),
// underlying tokens with amounts and USD values, total value, wallet name
```

**Protocol badge**: Small colored badge with protocol name. Reuse existing Badge variants or add a `protocol` variant.

### 4.2 Extended Transaction Type Badge

Update existing `TransactionTypeBadge` to support new types. Add protocol sub-badge.

```typescript
// Extended typeConfig
const typeConfig: Record<TransactionType, { label, icon, variant }> = {
  // existing...
  swap:           { label: 'Swap',     icon: Repeat,     variant: 'swap' },
  defi_deposit:   { label: 'Deposit',  icon: ArrowDownToLine, variant: 'liquidity' },
  defi_withdraw:  { label: 'Withdraw', icon: ArrowUpFromLine, variant: 'liquidity' },
  defi_claim:     { label: 'Claim',    icon: Gift,       variant: 'profit' },
}
```

### 4.3 Protocol Badge (`components/domain/ProtocolBadge.tsx`)

Small inline badge showing protocol name (e.g., "Uniswap V3", "GMX V2").

```typescript
interface ProtocolBadgeProps {
  protocol: string
  size?: 'sm' | 'default'
  className?: string
}
```

### 4.4 Cost Basis Override Dialog (`components/domain/CostBasisOverrideDialog.tsx`)

Modal dialog for editing cost basis on a tax lot.

```
┌─────────────────────────────────────────────────┐
│  Override Cost Basis                        [X]  │
│                                                   │
│  Lot: 3.0 ETH acquired on 01.01.2025            │
│  Auto cost basis: $1,800.00 (source: swap_price) │
│                                                   │
│  New cost basis per unit:                         │
│  ┌──────────────────────────────────┐            │
│  │ $ 1,750.00                       │            │
│  └──────────────────────────────────┘            │
│                                                   │
│  Reason:                                          │
│  ┌──────────────────────────────────┐            │
│  │ Actual purchase price on Binance │            │
│  └──────────────────────────────────┘            │
│                                                   │
│  Note: This will update PnL calculations for     │
│  all disposals from this lot.                    │
│                                                   │
│              [Cancel]  [Save Override]            │
└─────────────────────────────────────────────────┘
```

Uses shadcn `Dialog`, `Input`, `Label`, `Button`. Calls `useOverrideCostBasis` mutation.

### 4.5 PnL Summary Card (`components/domain/PnlSummaryCard.tsx`)

Compact card showing realized + unrealized PnL. Uses existing `PnLValue` component for color coding.

```typescript
interface PnlSummaryCardProps {
  realizedPnl: string   // scaled integer
  unrealizedPnl: string // scaled integer
  className?: string
}
```

### 4.6 Lot Detail Table (`components/domain/LotDetailTable.tsx`)

Expandable table showing individual tax lots for an asset. Each row shows lot info and has an "Edit" button to open the override dialog.

```typescript
interface LotDetailTableProps {
  lots: TaxLot[]
  onOverride: (lotId: string) => void
}
```

### 4.7 Disposal History Table (`components/domain/DisposalHistoryTable.tsx`)

Table showing lot disposals with calculated PnL (proceeds - cost basis) x quantity.

```typescript
interface DisposalHistoryTableProps {
  disposals: LotDisposal[]
}
```

### 4.8 Link Transfer Dialog (`components/domain/LinkTransferDialog.tsx`)

Dialog to link a `transfer_in` to a `transfer_out` from another wallet (for internal transfer cost basis propagation).

```
┌─────────────────────────────────────────────────┐
│  Link Transfer                             [X]   │
│                                                   │
│  This transfer_in:                                │
│  5.0 ETH received on 01.02.2025                  │
│  Wallet: Arbitrum Wallet                          │
│                                                   │
│  Link to outgoing transfer:                       │
│  ┌──────────────────────────────────┐            │
│  │ ▼ Select matching transfer_out   │            │
│  │   5.0 ETH sent 01.02.2025       │            │
│  │   Main Wallet → Arbitrum         │            │
│  └──────────────────────────────────┘            │
│                                                   │
│  Effect: Cost basis will be inherited from the    │
│  source wallet's lot ($2,100 per ETH).           │
│                                                   │
│              [Cancel]  [Link Transfers]           │
└─────────────────────────────────────────────────┘
```

### 4.9 Swap Detail Section (`features/transactions/SwapDetailSection.tsx`)

For swap transaction detail page — shows sold/bought side by side with arrows.

```typescript
interface SwapDetailSectionProps {
  transaction: TransactionDetail // extended with swap-specific fields
}
```

---

## 5. New Types

### 5.1 Type Changes (`types/transaction.ts`)

```typescript
// Extended transaction types
export type TransactionType =
  | 'transfer_in'
  | 'transfer_out'
  | 'internal_transfer'
  | 'asset_adjustment'
  | 'swap'              // NEW: DEX swap
  | 'defi_deposit'      // NEW: LP deposit, staking, lending supply
  | 'defi_withdraw'     // NEW: LP withdraw, unstake, lending withdraw
  | 'defi_claim'        // NEW: Rewards claim

// Extended TransactionListItem
export interface TransactionListItem {
  // ...existing fields...
  protocol?: string          // NEW: "Uniswap V3", "GMX V2", etc.
  operation_type?: string    // NEW: Zerion operation_type for display
  // For swaps: additional fields
  sold_asset_symbol?: string
  sold_amount?: string
  bought_asset_symbol?: string
  bought_amount?: string
}

// Extended TransactionDetail
export interface TransactionDetail extends TransactionListItem {
  // ...existing fields...
  // Swap-specific
  sold_asset_id?: string
  sold_display_amount?: string
  sold_usd_value?: string
  bought_asset_id?: string
  bought_display_amount?: string
  bought_usd_value?: string
  // DeFi context
  protocol?: string
  chain_name?: string
}
```

### 5.2 New Type File (`types/defi.ts`)

```typescript
export interface DefiPosition {
  id: string
  wallet_id: string
  wallet_name: string
  chain_id: number
  protocol: string
  position_type: 'lp' | 'staking' | 'lending' | 'gm_pool'
  name: string                        // "ETH/USDC Pool"
  underlying_tokens: UnderlyingToken[]
  total_usd_value: string             // scaled integer
  updated_at: string
}

export interface UnderlyingToken {
  asset_id: string
  symbol: string
  amount: string           // display amount
  usd_value: string        // scaled integer
}

export interface DefiPositionsResponse {
  positions: DefiPosition[]
  total_value: string       // scaled integer
}
```

### 5.3 New Type File (`types/taxlot.ts`)

```typescript
export interface TaxLot {
  id: string
  transaction_id: string
  account_id: string
  asset: string
  quantity_acquired: string          // scaled integer
  quantity_remaining: string         // scaled integer
  acquired_at: string
  auto_cost_basis_per_unit: string   // scaled integer (USD x 10^8)
  auto_cost_basis_source: 'swap_price' | 'fmv_at_transfer' | 'linked_transfer'
  override_cost_basis_per_unit?: string
  override_reason?: string
  override_at?: string
  effective_cost_basis_per_unit: string  // computed by backend
  linked_source_lot_id?: string
}

export interface LotDisposal {
  id: string
  transaction_id: string
  lot_id: string
  quantity_disposed: string
  proceeds_per_unit: string          // scaled integer
  disposal_type: 'sale' | 'internal_transfer'
  disposed_at: string
  // Computed on read:
  effective_cost_basis_per_unit: string
  realized_pnl: string              // (proceeds - cost) * qty
}

export interface PositionWAC {
  account_id: string
  asset: string
  total_quantity: string
  weighted_avg_cost: string          // scaled integer
}

export interface PnlReport {
  realized_pnl: string              // total, scaled integer
  unrealized_pnl: string            // total, scaled integer
  disposals: LotDisposal[]
  positions: PositionWAC[]
}

export interface OverrideCostBasisRequest {
  lot_id: string
  cost_basis_per_unit: string        // scaled integer
  reason: string
}

export interface LinkTransferRequest {
  transfer_in_transaction_id: string
  transfer_out_transaction_id: string
}

export interface LotOverrideHistory {
  id: string
  lot_id: string
  previous_cost_basis?: string
  new_cost_basis?: string
  reason?: string
  changed_at: string
}
```

---

## 6. New API Hooks (TanStack Query)

### 6.1 DeFi Positions (`hooks/useDefiPositions.ts`)

```typescript
export function useDefiPositions(walletId?: string) {
  return useQuery<DefiPositionsResponse>({
    queryKey: ['defi-positions', walletId],
    queryFn: () => defiService.getPositions(walletId),
  })
}
```

### 6.2 Tax Lots (`hooks/useTaxLots.ts`)

```typescript
export function useTaxLots(accountId: string, asset: string) {
  return useQuery<TaxLot[]>({
    queryKey: ['tax-lots', accountId, asset],
    queryFn: () => taxLotService.getLots(accountId, asset),
    enabled: !!accountId && !!asset,
  })
}

export function useLotDisposals(lotId: string) {
  return useQuery<LotDisposal[]>({
    queryKey: ['lot-disposals', lotId],
    queryFn: () => taxLotService.getDisposals(lotId),
    enabled: !!lotId,
  })
}

export function useLotOverrideHistory(lotId: string) {
  return useQuery<LotOverrideHistory[]>({
    queryKey: ['lot-override-history', lotId],
    queryFn: () => taxLotService.getOverrideHistory(lotId),
    enabled: !!lotId,
  })
}
```

### 6.3 PnL Report (`hooks/usePnlReport.ts`)

```typescript
export function usePnlReport(params?: {
  account_id?: string
  asset?: string
  start_date?: string
  end_date?: string
}) {
  return useQuery<PnlReport>({
    queryKey: ['pnl-report', params],
    queryFn: () => pnlService.getReport(params),
  })
}
```

### 6.4 Cost Basis Override Mutation (`hooks/useTaxLots.ts`)

```typescript
export function useOverrideCostBasis() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (data: OverrideCostBasisRequest) =>
      taxLotService.overrideCostBasis(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tax-lots'] })
      queryClient.invalidateQueries({ queryKey: ['pnl-report'] })
      queryClient.invalidateQueries({ queryKey: ['portfolio'] })
    },
  })
}
```

### 6.5 Link Transfer Mutation (`hooks/useTaxLots.ts`)

```typescript
export function useLinkTransfer() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (data: LinkTransferRequest) =>
      taxLotService.linkTransfer(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tax-lots'] })
      queryClient.invalidateQueries({ queryKey: ['pnl-report'] })
      queryClient.invalidateQueries({ queryKey: ['transactions'] })
    },
  })
}
```

### 6.6 WAC Per Asset (`hooks/usePositionWAC.ts`)

```typescript
export function usePositionWAC(accountId?: string) {
  return useQuery<PositionWAC[]>({
    queryKey: ['position-wac', accountId],
    queryFn: () => taxLotService.getWAC(accountId),
    enabled: !!accountId,
  })
}
```

---

## 7. New API Services

### 7.1 DeFi Service (`services/defi.ts`)

```typescript
// GET /portfolio/defi-positions?wallet_id={walletId}
export const defiService = {
  async getPositions(walletId?: string): Promise<DefiPositionsResponse> {
    const response = await api.get('/portfolio/defi-positions', {
      params: walletId ? { wallet_id: walletId } : undefined,
    })
    return response.data
  },
}
```

### 7.2 Tax Lot Service (`services/taxlot.ts`)

```typescript
export const taxLotService = {
  // GET /tax-lots?account_id={id}&asset={asset}
  async getLots(accountId: string, asset: string): Promise<TaxLot[]> { ... },

  // GET /tax-lots/{lotId}/disposals
  async getDisposals(lotId: string): Promise<LotDisposal[]> { ... },

  // GET /tax-lots/{lotId}/override-history
  async getOverrideHistory(lotId: string): Promise<LotOverrideHistory[]> { ... },

  // PUT /tax-lots/{lotId}/override
  async overrideCostBasis(data: OverrideCostBasisRequest): Promise<void> { ... },

  // POST /transfers/link
  async linkTransfer(data: LinkTransferRequest): Promise<void> { ... },

  // GET /position-wac?account_id={id}
  async getWAC(accountId?: string): Promise<PositionWAC[]> { ... },
}
```

### 7.3 PnL Service (`services/pnl.ts`)

```typescript
export const pnlService = {
  // GET /pnl?account_id={id}&asset={asset}&start_date={}&end_date={}
  async getReport(params?: {
    account_id?: string
    asset?: string
    start_date?: string
    end_date?: string
  }): Promise<PnlReport> { ... },
}
```

---

## 8. New Backend API Endpoints Required

These endpoints must be implemented by the backend team:

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/portfolio/defi-positions` | DeFi positions from Zerion, optional `?wallet_id=` |
| GET | `/tax-lots` | List tax lots, filter by `account_id`, `asset` |
| GET | `/tax-lots/:id/disposals` | Disposal history for a lot |
| GET | `/tax-lots/:id/override-history` | Override audit trail for a lot |
| PUT | `/tax-lots/:id/override` | Set/clear cost basis override |
| POST | `/transfers/link` | Link transfer_in to transfer_out |
| GET | `/position-wac` | WAC per asset, optional `?account_id=` |
| GET | `/pnl` | PnL report with optional filters |

**Note:** Existing endpoints (`/portfolio`, `/transactions`, `/transactions/:id`) will be extended to include new fields (protocol, cost_basis, swap details) rather than creating new endpoints.

---

## 9. UX Flows

### 9.1 Override Cost Basis

```
User navigates to Cost Basis page
  → Sees asset overview table with WAC per asset
  → Clicks on ETH row to expand lot detail
  → Sees individual tax lots with effective cost basis
  → Clicks [Edit] on lot #2 (transfer_in, FMV-based)
  → CostBasisOverrideDialog opens
  → Enters actual purchase price and reason
  → Clicks [Save Override]
  → Toast: "Cost basis updated"
  → Lot table refreshes, WAC recalculates
  → PnL values across the app update
```

### 9.2 Link Transfers (Internal Transfer Recognition)

```
User navigates to Cost Basis page
  → Sees a transfer_in lot with FMV-based cost
  → Clicks [Link Transfer] button on the lot row
  → LinkTransferDialog opens
  → System shows matching transfer_out candidates (same asset, similar amount, close timestamp)
  → User selects the matching outgoing transfer
  → Preview shows: "Cost basis will change from $2,500 (FMV) to $2,100 (from source lot)"
  → Clicks [Link Transfers]
  → Toast: "Transfers linked. Cost basis updated."
  → Lot now shows linked_source_lot, cost basis inherited
```

### 9.3 DeFi Positions Browsing

```
User navigates to DeFi Positions (sidebar)
  → Sees all active positions grouped by protocol
  → Can filter by wallet using dropdown
  → Clicks on position card for more detail (future: expand with historical chart)
  → Protocol badge links to protocol's official page (future)
```

### 9.4 PnL Review

```
User navigates to PnL page
  → Sees summary: realized PnL, unrealized PnL, total
  → Realized tab: disposal history with per-disposal PnL
  → Unrealized tab: current positions with WAC vs current price
  → Can filter by date range and asset
  → Clicking a disposal row links to the transaction detail page
```

---

## 10. Design Considerations

### Loading States
- All new pages use skeleton components (matching existing pattern from `DashboardSkeleton`, `TransactionsSkeleton`)
- Individual data sections show inline skeletons when loading (e.g., lot detail within cost basis page)
- DeFi positions page: skeleton cards matching position card layout

### Empty States
- **DeFi Positions**: Icon (Layers) + "No DeFi positions found" + "Positions will appear after your wallets sync DeFi activity"
- **Cost Basis**: Icon (Calculator) + "No tax lots yet" + "Cost basis is tracked automatically when assets are acquired"
- **PnL**: Icon (TrendingUp) + "No PnL data yet" + "PnL is calculated when you sell or swap assets"
- Match existing empty state pattern from TransactionsPage (centered icon + text + optional CTA button)

### Error Handling
- API errors: Toast notification via Sonner (existing pattern)
- Failed cost basis override: Show inline error in dialog, don't close
- Network errors: React Query retry (already configured: `retry: 1`)
- Stale data: Show "last updated" timestamp on WAC/PnL sections

### Mobile Responsiveness
- Cost Basis table: horizontal scroll on mobile, or collapse to card-based layout
- PnL table: horizontal scroll with sticky first column
- DeFi position cards: stack vertically, full width
- Override dialog: full width on mobile (existing Dialog behavior)
- All new pages follow existing responsive patterns (e.g., `sm:flex-row` for header layout)

### PnL Color Coding
- Reuse existing `text-profit` / `text-loss` classes and `PnLValue` component
- Positive PnL: green (`--profit: 142 76% 36%`)
- Negative PnL: red (`--loss: 0 84% 50%`)
- Zero PnL: muted (`text-muted-foreground`)
- Background tints for PnL cells: `bg-profit-bg` / `bg-loss-bg` (subtle, existing design tokens)

### Design Token Additions
No new CSS variables needed. The existing design system already has:
- `--tx-swap`, `--tx-liquidity`, `--tx-gm-pool` for DeFi transaction types
- `--profit`, `--loss` for PnL coloring
- Badge variants: `swap`, `liquidity`, `gmPool` already defined
- Add new badge variant for `defi_claim`: reuse `profit` variant (green) since it's reward income

---

## 11. File Structure (New Files)

```
src/
├── types/
│   ├── defi.ts                          # DefiPosition, UnderlyingToken types
│   └── taxlot.ts                        # TaxLot, LotDisposal, PositionWAC, PnlReport types
├── services/
│   ├── defi.ts                          # DeFi positions API calls
│   ├── taxlot.ts                        # Tax lots, overrides, WAC API calls
│   └── pnl.ts                           # PnL report API calls
├── hooks/
│   ├── useDefiPositions.ts              # useDefiPositions hook
│   ├── useTaxLots.ts                    # useTaxLots, useOverrideCostBasis, useLinkTransfer
│   ├── usePnlReport.ts                  # usePnlReport hook
│   └── usePositionWAC.ts               # usePositionWAC hook
├── features/
│   ├── defi/
│   │   └── DefiPositionsPage.tsx        # DeFi positions page
│   ├── cost-basis/
│   │   └── CostBasisPage.tsx            # Cost basis / tax lots page
│   ├── pnl/
│   │   └── PnlPage.tsx                  # PnL report page
│   └── transactions/
│       └── SwapDetailSection.tsx         # Swap detail view for tx detail page
└── components/
    └── domain/
        ├── DefiPositionCard.tsx          # DeFi position card
        ├── ProtocolBadge.tsx             # Protocol name badge
        ├── CostBasisOverrideDialog.tsx   # Override cost basis modal
        ├── LinkTransferDialog.tsx        # Link transfer_in/out modal
        ├── LotDetailTable.tsx           # Tax lot list per asset
        ├── DisposalHistoryTable.tsx      # Disposal history with PnL
        └── PnlSummaryCard.tsx           # Compact PnL summary
```

### Modified Files

```
src/
├── types/
│   ├── transaction.ts                   # Add new TransactionType variants, protocol field
│   ├── portfolio.ts                     # Add cost_basis, unrealized_pnl to AssetHolding
│   └── index.ts                         # Add defi, taxlot exports
├── app/
│   └── App.tsx                          # Add 3 new routes
├── components/
│   ├── layout/
│   │   ├── Sidebar.tsx                  # Add 3 new nav items
│   │   └── MobileSidebar.tsx            # Add 3 new nav items
│   └── domain/
│       └── TransactionTypeBadge.tsx     # Add swap, defi_deposit, defi_withdraw, defi_claim
├── features/
│   ├── dashboard/
│   │   ├── DashboardPage.tsx            # Add PnL stat card
│   │   └── PortfolioSummary.tsx         # Add cost basis / unrealized PnL
│   ├── transactions/
│   │   ├── TransactionsPage.tsx         # Protocol badge in table rows, swap display
│   │   ├── TransactionDetailPage.tsx    # Protocol section, swap detail, cost basis link
│   │   └── TransactionFilters.tsx       # Add new transaction types to filter
│   └── wallets/
│       ├── WalletDetailPage.tsx         # Add DeFi tab, cost basis columns
│       └── WalletAssets.tsx             # Add WAC and unrealized PnL columns
└── hooks/
    └── index.ts                         # Re-export new hooks
```

---

## 12. Implementation Phases

### Phase A: Foundation (Types + Services + Hooks)
- [ ] Create `types/defi.ts` and `types/taxlot.ts`
- [ ] Extend `types/transaction.ts` with new types
- [ ] Extend `types/portfolio.ts` with cost basis fields
- [ ] Create `services/defi.ts`, `services/taxlot.ts`, `services/pnl.ts`
- [ ] Create all new hooks
- [ ] Update `types/index.ts` barrel export

### Phase B: Transaction Enhancements (depends on backend new tx types)
- [ ] Update `TransactionTypeBadge` with new DeFi types
- [ ] Create `ProtocolBadge` component
- [ ] Update `TransactionFilters` with new types
- [ ] Update `TransactionsPage` for swap row display
- [ ] Create `SwapDetailSection` for transaction detail
- [ ] Update `TransactionDetailPage` with protocol info and swap detail

### Phase C: DeFi Positions Page (depends on backend DeFi positions endpoint)
- [ ] Create `DefiPositionCard` component
- [ ] Create `DefiPositionsPage`
- [ ] Add route in `App.tsx`
- [ ] Add nav item in `Sidebar.tsx` and `MobileSidebar.tsx`

### Phase D: Cost Basis & Tax Lots (depends on backend lot endpoints)
- [ ] Create `LotDetailTable` component
- [ ] Create `CostBasisOverrideDialog` component
- [ ] Create `LinkTransferDialog` component
- [ ] Create `CostBasisPage`
- [ ] Add route in `App.tsx`
- [ ] Add nav item in Sidebar

### Phase E: PnL Report (depends on backend PnL endpoint)
- [ ] Create `PnlSummaryCard` component
- [ ] Create `DisposalHistoryTable` component
- [ ] Create `PnlPage`
- [ ] Add route in `App.tsx`
- [ ] Add nav item in Sidebar

### Phase F: Dashboard & Wallet Enhancements
- [ ] Update `DashboardPage` with PnL stat card
- [ ] Update `PortfolioSummary` with cost basis info
- [ ] Update `WalletDetailPage` with DeFi tab
- [ ] Update `WalletAssets` with WAC and unrealized PnL columns

---

## 13. Dependencies on Backend

| Frontend Feature | Backend Dependency |
|------------------|--------------------|
| New transaction types in UI | Backend returns new `type` values in transaction list/detail |
| Protocol badge | Backend includes `protocol` field in transaction responses |
| Swap detail display | Backend includes swap-specific fields (sold/bought) in transaction detail |
| DeFi Positions page | `GET /portfolio/defi-positions` endpoint |
| Cost Basis page | `GET /tax-lots`, `PUT /tax-lots/:id/override` endpoints |
| PnL page | `GET /pnl` endpoint |
| WAC display | `GET /position-wac` endpoint |
| Link transfers | `POST /transfers/link` endpoint |
| Dashboard PnL | Portfolio summary includes `unrealized_pnl` field |
| Wallet assets WAC | Portfolio/wallet response includes WAC per asset |

**Critical path:** Backend must extend transaction responses with `protocol` and swap fields before frontend can show DeFi context in existing pages. New pages can be built with mock data and connected when endpoints are ready.
