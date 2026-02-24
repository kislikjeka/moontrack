# API Refactoring: Move Logic to Backend & Remove Dead Code

## Context

API audit revealed several issues:
1. **Dead frontend code** — 5 out of 6 asset service functions, their types, and a hook are defined but never called
2. **Float math on financial data** — `WalletHoldings` uses `parseFloat()` + `.reduce()` to aggregate balances; `TransactionLotImpactSection` computes gain/loss with `parseFloat` — both violate financial precision requirements
3. **Client-side aggregation** — Frontend groups assets by `asset_id`, matches WAC positions via O(N*M) linear search, sorts lots with 3-level priority — all should be backend responsibility
4. **Redundant requests** — `WalletHoldings` makes a separate `GET /positions/wac` request to match WAC data that the backend already has when building portfolio

**Principle**: Backend computes and structures data; frontend only renders.

---

## Phase A: Dead Frontend Code Cleanup

No API changes. Frontend only.

### A1. `apps/frontend/src/services/asset.ts`
- **Remove** functions: `getAsset`, `listAssets`, `getPrice`, `getBatchPrices`, `getPriceHistory`
- **Remove** types: `PriceResponse`, `RegistryAsset`, `Asset` alias, `PriceHistoryPoint`, `PriceHistoryResponse`
- **Keep** only `search()`, import `Asset` type from `@/types/asset`

### A2. `apps/frontend/src/types/asset.ts`
- **Remove** interfaces: `PriceResponse`, `PriceHistoryPoint`, `PriceHistoryResponse`
- **Keep** `AssetType` and `Asset`

### A3. `apps/frontend/src/hooks/useAssetSearch.ts`
- **Remove** `useAsset` function (lines 18-24)

### A4. `apps/frontend/src/hooks/index.ts`
- **Remove** `useAsset` from re-export on line 5

> Backend endpoints (`/assets/{id}/price`, `/assets/prices`, `/assets/{id}/history`, `/assets`, `/assets/{id}`) stay — valid API surface for future use.

---

## Phase B: Pre-compute Gain/Loss on Backend

### B1. `apps/backend/internal/platform/taxlot/service.go`
- **Add** `RealizedGainLoss *big.Int` field to `DisposalDetail` struct (line 37-43)
- **Compute** in `GetLotImpactByTransaction` (after line 214):
  ```go
  // (ProceedsPerUnit - EffectiveCostBasis) * QuantityDisposed / 10^decimals
  // Both prices are USD scaled 10^8, qty is base units (10^decimals)
  // Result: USD scaled 10^8
  decimals := money.GetDecimals(lot.Asset)
  priceDiff := new(big.Int).Sub(d.ProceedsPerUnit, lot.EffectiveCostBasisPerUnit())
  gainLoss := new(big.Int).Mul(priceDiff, d.QuantityDisposed)
  divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
  gainLoss.Div(gainLoss, divisor)
  ```
- Add import `"github.com/kislikjeka/moontrack/pkg/money"`

### B2. `apps/backend/internal/transport/httpapi/handler/taxlot.go`
- **Add** `RealizedGainLoss string` field to `DisposalDetailResponse` (json: `"realized_gain_loss"`)
- **Serialize** in `GetTransactionLots`: `RealizedGainLoss: money.FormatUSD(d.RealizedGainLoss)`

### B3. `apps/frontend/src/types/taxlot.ts`
- **Add** `realized_gain_loss: string` to `DisposalDetail` interface

### B4. `apps/frontend/src/features/transactions/TransactionLotImpactSection.tsx`
- **Replace** lines 125-127:
  ```typescript
  // Before: float math
  const qty = parseFloat(disposal.quantity_disposed)
  const gainLoss = (parseFloat(disposal.proceeds_per_unit) - parseFloat(disposal.lot_cost_basis_per_unit)) * qty
  const isPositive = gainLoss >= 0

  // After: use pre-computed
  const isPositive = !disposal.realized_gain_loss.startsWith('-')
  ```
- **Update** display (line 137): `formatUSD(disposal.realized_gain_loss)` directly

---

## Phase C: Add `chain_id` Filter to `GET /lots` & Unify Sort

### C1. `apps/backend/internal/transport/httpapi/handler/taxlot.go`
- **Update** `TaxLotServiceInterface` — add `chainID string` param to `GetLotsByWallet`
- **In `GetLots` handler** — parse optional `chain_id` query param, pass to service

### C2. `apps/backend/internal/platform/taxlot/service.go`
- **Update** `GetLotsByWallet` signature — add `chainID string`
- **Move** chainMap population loop (lines 103-105) **before** sort (currently after)
- **Add** chain_id filtering after populating chainIDs
- **Update** sort to include chain grouping as first key (when `chainID == ""`)
- Add import `"strings"` for `strings.ToLower`

Sort order (matches what frontend currently does):
1. Chain grouping (alphabetical, only when no chain filter)
2. Open lots first (`QuantityRemaining > 0`)
3. FIFO (oldest `AcquiredAt` first, tie-break by `CreatedAt`)

### C3. `apps/frontend/src/services/taxlot.ts`
- **Update** `getLots` — accept optional `chainId?: string`, pass as `chain_id` query param

### C4. `apps/frontend/src/hooks/useTaxLots.ts`
- **Update** `useTaxLots` — accept `chainId?: string`, include in query key and pass to service

### C5. `apps/frontend/src/components/domain/LotDetailTable.tsx`
- **Pass** `chainId` to `useTaxLots(walletId, asset, chainId)` hook call
- **Remove** client-side filtering (line 34): `rawLots?.filter((lot) => lot.chain_id === chainId)`
- **Remove** client-side sort (lines 75-87): the `[...lots].sort(...)` block
- Render `lots` directly from hook data

---

## Phase D: Enrich Portfolio Holdings with Grouped Data + WAC

### D1. `apps/backend/internal/module/portfolio/service.go` — Domain types + interface
- **Add** `WACProvider` interface:
  ```go
  type WACProvider interface {
      GetWAC(ctx context.Context, userID uuid.UUID, walletID *uuid.UUID) ([]WACPosition, error)
  }
  type WACPosition struct {
      WalletID, AccountID uuid.UUID
      ChainID, Asset      string
      TotalQuantity, WeightedAvgCost *big.Int
  }
  ```
- **Add** domain types `HoldingGroup` and `ChainHolding` (with `*big.Int` fields + optional WAC)
- **Add** `Holdings []HoldingGroup` field to `WalletBalance` struct
- **Add** `wacProvider WACProvider` field to `PortfolioService` struct (nilable)
- **Update** `NewPortfolioService` signature — add `wacProvider WACProvider` param

### D2. `apps/backend/internal/module/portfolio/service.go` — Grouping logic
- In `GetPortfolioSummary`, **after** building `walletBalances` (around line 234), add post-processing:
  - Group each wallet's flat `Assets[]` by `AssetID` → `HoldingGroup`
  - Sum `Amount`, `USDValue` with `big.Int` (no float math)
  - Build `ChainHolding[]` within each group
  - If `wacProvider != nil`, call `GetWAC(ctx, userID, &walletID)` per wallet, match WAC to groups/chains
  - Sort holdings by total value descending

### D3. `apps/backend/internal/module/portfolio/wac_adapter.go` — NEW file
- Adapter from `*taxlot.Service` → `portfolio.WACProvider` interface
- Maps `taxlot.WACPosition` to `portfolio.WACPosition`, sets `IsAggregated` based on `AccountID == uuid.Nil`

### D4. `apps/backend/internal/transport/httpapi/handler/portfolio.go`
- **Add** `HoldingGroupResponse` and `ChainHoldingResponse` types (string fields, `wac` omitempty)
- **Add** `Holdings []HoldingGroupResponse` field to `WalletBalanceResponse` (keep `Assets` for backward compat)
- **In** `GetPortfolioSummary` — serialize `Holdings` alongside `Assets`

### D5. `apps/backend/cmd/api/main.go` — Wiring
- **Create** `wacAdapter := portfolio.NewWACAdapter(taxLotSvc)` after line 120
- **Update** `NewPortfolioService` call (line 168) — pass `wacAdapter`

### D6. `apps/backend/internal/module/portfolio/service_test.go`
- **Update** 3 calls to `NewPortfolioService` — pass `nil` as 4th arg

### D7. `apps/frontend/src/types/portfolio.ts`
- **Add** `HoldingGroup` and `ChainHolding` interfaces
- **Add** `holdings: HoldingGroup[]` to `WalletBalance`

### D8. `apps/frontend/src/features/wallets/WalletHoldings.tsx`
- **Change** props: `assets: AssetBalance[]` → `holdings: HoldingGroup[]`
- **Remove** `import { usePositionWAC }`
- **Remove** `usePositionWAC(walletId)` hook call
- **Remove** entire `useMemo` grouping block (lines 46-98)
- **Remove** local `AssetGroup` and `ChainBreakdown` interfaces
- **Import** `HoldingGroup`, `ChainHolding` from `@/types/portfolio`
- **Render** directly from `holdings` — use string values with `formatUSD`/`formatCrypto`

### D9. `apps/frontend/src/features/wallets/WalletDetailPage.tsx`
- **Change** line 54: `const holdings = walletBalance?.holdings || []`
- **Change** line 216: `<WalletHoldings walletId={id!} holdings={holdings} />`

### D10. `apps/frontend/src/features/wallets/WalletsPage.tsx`
- **Change** line 23: `assetCount: wb.holdings?.length || wb.assets.length` (graceful fallback)

---

## Files Summary

| Phase | File | Action |
|-------|------|--------|
| A | `apps/frontend/src/services/asset.ts` | Remove 5 functions, 5 types |
| A | `apps/frontend/src/types/asset.ts` | Remove 3 interfaces |
| A | `apps/frontend/src/hooks/useAssetSearch.ts` | Remove `useAsset` |
| A | `apps/frontend/src/hooks/index.ts` | Remove `useAsset` re-export |
| B | `apps/backend/internal/platform/taxlot/service.go` | Add `RealizedGainLoss` field + computation |
| B | `apps/backend/internal/transport/httpapi/handler/taxlot.go` | Add `realized_gain_loss` to response |
| B | `apps/frontend/src/types/taxlot.ts` | Add `realized_gain_loss` |
| B | `apps/frontend/src/features/transactions/TransactionLotImpactSection.tsx` | Use pre-computed value |
| C | `apps/backend/internal/transport/httpapi/handler/taxlot.go` | Add `chain_id` param, update interface |
| C | `apps/backend/internal/platform/taxlot/service.go` | Add chain filter + unified sort |
| C | `apps/frontend/src/services/taxlot.ts` | Pass `chainId` to API |
| C | `apps/frontend/src/hooks/useTaxLots.ts` | Add `chainId` param |
| C | `apps/frontend/src/components/domain/LotDetailTable.tsx` | Remove client-side filter/sort |
| D | `apps/backend/internal/module/portfolio/service.go` | Add WACProvider, HoldingGroup, grouping logic |
| D | `apps/backend/internal/module/portfolio/wac_adapter.go` | **NEW** — adapter |
| D | `apps/backend/internal/module/portfolio/service_test.go` | Update constructor calls |
| D | `apps/backend/internal/transport/httpapi/handler/portfolio.go` | Add Holdings response types |
| D | `apps/backend/cmd/api/main.go` | Wire WAC adapter |
| D | `apps/frontend/src/types/portfolio.ts` | Add HoldingGroup, ChainHolding |
| D | `apps/frontend/src/features/wallets/WalletHoldings.tsx` | Rewrite — use pre-grouped data |
| D | `apps/frontend/src/features/wallets/WalletDetailPage.tsx` | Pass `holdings` |
| D | `apps/frontend/src/features/wallets/WalletsPage.tsx` | Use `holdings.length` |

---

## Execution Strategy: Agents & Parallelism

### Wave 1 — Phase A + Phase B backend + Phase C backend (parallel)

Three agents can work simultaneously since they touch **no overlapping files**:

| Agent | Type | Scope | Files |
|-------|------|-------|-------|
| **agent-frontend-cleanup** | `general-purpose` | Phase A: dead frontend code | `services/asset.ts`, `types/asset.ts`, `hooks/useAssetSearch.ts`, `hooks/index.ts` |
| **agent-gainloss-backend** | `general-purpose` | Phase B backend (B1-B2): gain/loss computation | `taxlot/service.go` (DisposalDetail + computation), `handler/taxlot.go` (response type + serialization) |
| **agent-chainfilter-backend** | `general-purpose` | Phase C backend (C1-C2): chain_id filter + sort | `handler/taxlot.go` (interface + handler param), `taxlot/service.go` (filter + sort refactor) |

> **Conflict note**: agents B-backend and C-backend both touch `taxlot/service.go` and `handler/taxlot.go`. Use **git worktrees** (`isolation: "worktree"`) for these two agents, then merge sequentially — C after B since C changes the sort and B changes DisposalDetail (independent sections of the same files).

### Wave 2 — Phase B frontend + Phase C frontend (parallel, after Wave 1)

After backend changes are merged and `go build` passes:

| Agent | Type | Scope | Files |
|-------|------|-------|-------|
| **agent-gainloss-frontend** | `general-purpose` | Phase B frontend (B3-B4) | `types/taxlot.ts`, `TransactionLotImpactSection.tsx` |
| **agent-chainfilter-frontend** | `general-purpose` | Phase C frontend (C3-C5) | `services/taxlot.ts`, `hooks/useTaxLots.ts`, `LotDetailTable.tsx` |

No file overlaps — fully parallel.

### Wave 3 — Phase D (sequential, single agent)

Phase D is the most complex and touches many files across backend and frontend. Run as **one agent** to avoid merge conflicts:

| Agent | Type | Scope | Files |
|-------|------|-------|-------|
| **agent-holdings-enrichment** | `general-purpose` | Phase D: full stack | `portfolio/service.go`, `portfolio/wac_adapter.go` (NEW), `portfolio/service_test.go`, `handler/portfolio.go`, `cmd/api/main.go`, `types/portfolio.ts`, `WalletHoldings.tsx`, `WalletDetailPage.tsx`, `WalletsPage.tsx` |

### Wave 4 — Verification (single agent)

| Agent | Type | Scope |
|-------|------|-------|
| **agent-verify** | `general-purpose` | Run `go build`, `go test`, `tsc --noEmit`, `bun run test --run` |

### Summary

```
Wave 1 (parallel):  [A: frontend cleanup] + [B-backend: gain/loss] + [C-backend: chain filter]
                     ↓ merge B-backend first, then C-backend
Wave 2 (parallel):  [B-frontend: gain/loss UI] + [C-frontend: lot table UI]
Wave 3 (single):    [D: holdings + WAC enrichment — full stack]
Wave 4 (single):    [Verify: build + test all]
```

Total: **6 agents** across 4 waves, with max 3 agents running in parallel.

---

## Verification

After each wave:
```bash
cd apps/backend && go build ./...              # Must compile
cd apps/backend && go test ./... -v -short     # All tests pass
cd apps/frontend && bunx tsc --noEmit          # TypeScript OK
cd apps/frontend && bun run test --run         # Frontend tests pass
```

### Manual checks
- **Phase A**: Asset search in transaction form still works
- **Phase B**: Open any transaction with disposals — gain/loss column shows correct values
- **Phase C**: Expand wallet → asset → chain → lots table shows same data and order
- **Phase D**: Wallet detail page shows holdings with WAC, no separate `/positions/wac` request in Network tab; WalletsPage shows correct asset counts
