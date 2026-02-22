# Plan: Unified Holdings Component (Assets + Cost Basis)

## Context

The WalletDetailPage currently has an "Assets" tab showing per-chain asset balances, and a separate "Cost Basis" page (in sidebar navigation) showing WAC positions with expandable tax lots. These two views present overlapping information about the same data — what assets the user holds and at what cost. The goal is to merge them into a single "Holdings" tab on the wallet detail page with 3-level drill-down: Asset Group → Chain breakdown → Tax Lots.

## Architecture Overview

```
Level 1: Asset Group (ETH)     → Amount | Price | Value | WAC (aggregated)
  Level 2: Chain (ethereum)    → Amount | Value | WAC (per-chain)
    Level 3: Tax Lots          → #, Acquired, Qty, Remaining, Cost/Unit, Source, Actions
```

---

## Iteration 1: Backend + Frontend types (parallel agents)

**Goal**: Extend WAC endpoint to return `chain_id` and aggregated WAC positions. Update frontend types.

### Agent A: Backend WAC extension (general-purpose agent)

**Scope**: Backend-only changes to extend `GET /positions/wac` endpoint.

1. **`apps/backend/internal/platform/taxlot/service.go`**:
   - Add `ChainID string` field to `WACPosition` struct
   - In `GetWAC()`: build `accountToChainID` map from accounts (each `Account` has `ChainID *string`)
   - In `GetWAC()`: populate `ChainID` on each WACPosition
   - After building per-chain results: compute aggregated WAC per `(wallet_id, asset)` group:
     - `total_quantity = SUM(quantities)`, `weighted_avg_cost = SUM(qty * wac) / SUM(qty)` using `big.Int`
     - Append aggregated entries with `AccountID = uuid.Nil`, `ChainID = ""`

2. **`apps/backend/internal/transport/httpapi/handler/taxlot.go`**:
   - Add `ChainID string` and `IsAggregated bool` to `PositionWACResponse`
   - In `GetWAC` handler: populate new fields, set `IsAggregated = true` when `AccountID == uuid.Nil`

3. **Verify**: `cd apps/backend && go build ./...`

### Agent B: Frontend types + cleanup prep (general-purpose agent)

**Scope**: Update frontend types and remove Cost Basis page.

1. **`apps/frontend/src/types/taxlot.ts`**:
   - Add `chain_id?: string` and `is_aggregated?: boolean` to `PositionWAC`

2. **`apps/frontend/src/app/App.tsx`**:
   - Remove `import CostBasisPage` and `<Route path="/cost-basis" ...>` block

3. **`apps/frontend/src/components/layout/Sidebar.tsx`**:
   - Remove `Calculator` icon import and cost-basis nav item

4. **`apps/frontend/src/components/layout/MobileSidebar.tsx`**:
   - Remove `Calculator` icon import and cost-basis nav item

5. **Delete**: `apps/frontend/src/features/cost-basis/CostBasisPage.tsx`

6. **Verify**: `cd apps/frontend && bun run build`

**Parallelism**: Agents A and B are fully independent — backend and frontend changes have no dependencies.

---

## Iteration 2: WalletHoldings component (sequential, builds on Iteration 1)

**Goal**: Create the new 3-level Holdings component and integrate it into WalletDetailPage.

### Agent C: WalletHoldings component (general-purpose agent)

**Scope**: Create new component and update WalletDetailPage.

1. **Create `apps/frontend/src/features/wallets/WalletHoldings.tsx`**:
   - Props: `walletId: string`, `assets: AssetBalance[]`
   - Fetch WAC data: `usePositionWAC(walletId)` hook
   - `useMemo` to build 3-level data structure:
     - Group `assets` by `asset_id` → Level 1 (sum amounts, values)
     - Match with `wacPositions.filter(p => p.is_aggregated)` → Level 1 WAC
     - Per-chain rows from `assets` + `wacPositions.filter(p => !p.is_aggregated)` → Level 2
   - State management: `expandedAssets: Set<string>`, `expandedChains: Set<string>`
   - Level 1: Collapsible row — Asset icon, total Amount, Price, total Value, aggregated WAC
   - Level 2: Chain icon row — chain name, per-chain Amount, Value, WAC. Collapsible.
   - Level 3: Reuse `LotDetailTable` component (pass `walletId` + `asset`)
   - Reuse existing utilities: `formatAmount()` from WalletAssets, `formatUSD()` from `@/lib/format`
   - Reuse existing components: `AssetIcon`, `ChainIcon`, shadcn `Table`, `LotDetailTable`

2. **Update `apps/frontend/src/features/wallets/WalletDetailPage.tsx`**:
   - Replace `import { WalletAssets } from './WalletAssets'` with `import { WalletHoldings } from './WalletHoldings'`
   - Rename tab: `<TabsTrigger value="assets">Holdings</TabsTrigger>`
   - Replace `<WalletAssets assets={assets} />` with `<WalletHoldings walletId={id!} assets={assets} />`

3. **Delete**: `apps/frontend/src/features/wallets/WalletAssets.tsx`

4. **Verify**: `cd apps/frontend && bun run build`

---

## Reusable Components (no changes)

- `apps/frontend/src/components/domain/LotDetailTable.tsx` — tax lots table per wallet+asset
- `apps/frontend/src/components/domain/CostBasisOverrideDialog.tsx` — override modal
- `apps/frontend/src/hooks/useTaxLots.ts` — hooks (useTaxLots, usePositionWAC, useOverrideCostBasis)
- `apps/frontend/src/services/taxlot.ts` — API client

---

## Parallelism Summary

```
Iteration 1 (parallel):
  ├── Agent A: Backend WAC extension     ← independent
  └── Agent B: Frontend types + cleanup  ← independent

Iteration 2 (sequential, after Iteration 1):
  └── Agent C: WalletHoldings component  ← depends on A + B
```

Total: 2 iterations, 3 agents, 2 of which run in parallel.

---

## Files Summary

### Modified:
| File | Agent | Change |
|------|-------|--------|
| `apps/backend/internal/platform/taxlot/service.go` | A | Add ChainID, compute aggregated WAC |
| `apps/backend/internal/transport/httpapi/handler/taxlot.go` | A | Add chain_id, is_aggregated to response |
| `apps/frontend/src/types/taxlot.ts` | B | Add chain_id, is_aggregated to PositionWAC |
| `apps/frontend/src/app/App.tsx` | B | Remove cost-basis route |
| `apps/frontend/src/components/layout/Sidebar.tsx` | B | Remove cost-basis nav item |
| `apps/frontend/src/components/layout/MobileSidebar.tsx` | B | Remove cost-basis nav item |
| `apps/frontend/src/features/wallets/WalletDetailPage.tsx` | C | Replace WalletAssets → WalletHoldings, rename tab |

### New:
| File | Agent | Purpose |
|------|-------|---------|
| `apps/frontend/src/features/wallets/WalletHoldings.tsx` | C | Unified 3-level holdings component |

### Deleted:
| File | Agent | Reason |
|------|-------|--------|
| `apps/frontend/src/features/cost-basis/CostBasisPage.tsx` | B | Replaced by WalletHoldings |
| `apps/frontend/src/features/wallets/WalletAssets.tsx` | C | Replaced by WalletHoldings |

---

## Verification

1. **After Iteration 1**: `go build ./...` (Agent A) + `bun run build` (Agent B)
2. **After Iteration 2**: `bun run build` (Agent C)
3. **Manual E2E**:
   - Wallet detail → "Holdings" tab shows grouped assets with aggregated WAC
   - Expand asset → per-chain rows with chain icons and per-chain WAC
   - Expand chain → tax lots table with override button
   - Override flow works end-to-end
   - `/cost-basis` route gone, sidebar link gone
