# Move all financial formatting from frontend to backend

## Context

Currently the backend returns USD values as raw `big.Int` strings scaled by 10^8 (e.g., `"4115226300"` means $41.15). The frontend has to know about this scaling factor and performs `Number(BigInt(value)) / 100000000` in **12+ places** across components — duplicated `formatUSDValue()` functions, inline BigInt conversions, and `.reduce()` aggregations. This violates separation of concerns: the frontend should only display, not compute financial values. The backend already has `money.FromBaseUnits()` for crypto amounts — we need the same for USD values.

## Plan

### Step 1: Add USD formatting helper to backend `pkg/money`

**File**: `apps/backend/pkg/money/usd.go`

Add `FormatUSD(value *big.Int) string` — converts big.Int (scaled by 10^8) to human-readable decimal string (e.g., `4115226300` → `"41.15"`). Reuse the same approach as `FromBaseUnits()` with decimals=8, but always keep exactly 2 decimal places for USD.

### Step 2: Update backend portfolio response DTOs & handler

**File**: `apps/backend/internal/transport/httpapi/handler/portfolio.go`

Change all USD value fields to return formatted strings instead of raw big.Int strings:

- `PortfolioSummaryResponse.TotalUSDValue`: `money.FormatUSD(summary.TotalUSDValue)` → e.g., `"41.15"`
- `AssetHoldingResponse.USDValue`: `money.FormatUSD(holding.USDValue)` → e.g., `"41.15"`
- `AssetHoldingResponse.CurrentPrice`: `money.FormatUSD(holding.CurrentPrice)` → e.g., `"82304.52"`
- `WalletBalanceResponse.TotalUSD`: `money.FormatUSD(w.TotalUSD)` → e.g., `"41.15"`
- `AssetBalanceResponse.USDValue`: `money.FormatUSD(asset.USDValue)` → e.g., `"41.15"`
- `AssetBalanceResponse.Price`: `money.FormatUSD(asset.Price)` → e.g., `"82304.52"`

Also update `GetAssetBreakdown` handler with the same changes.

### Step 3: Update backend transaction DTOs

**File**: `apps/backend/internal/module/transactions/dto.go`
**File**: `apps/backend/internal/module/transactions/service.go`

- `TransactionListItem.USDValue`: format with `money.FormatUSD()`
- `EntryResponse.USDValue`: format with `money.FormatUSD()`

In `service.go`:
- Line ~170: `usdValue = money.FormatUSD(fields.USDValue)` instead of `fields.USDValue.String()`
- Line ~226: `USDValue: money.FormatUSD(entry.USDValue)` instead of `entry.USDValue.String()`

### Step 4: Simplify frontend — remove all BigInt/10^8 conversions

Remove all `Number(BigInt(value)) / 100000000` patterns and duplicated `formatUSDValue()` functions. Values now arrive as plain decimal strings like `"41.15"` that can be directly passed to `formatUSD()` or `parseFloat()`.

**Files to modify:**

1. **`apps/frontend/src/features/dashboard/PortfolioSummary.tsx`**
   - Remove local `formatPortfolioUSD()` function (lines 9-23)
   - Use `formatUSD(portfolio.total_usd_value)` directly (the existing `formatUSD` from `lib/format.ts` already handles strings via `parseFloat`)

2. **`apps/frontend/src/features/dashboard/AssetDistributionChart.tsx`**
   - Remove local `formatAssetUSD()` function (lines 21-35)
   - Line 57: change `Number(BigInt(holding.usd_value)) / 100000000` → `parseFloat(holding.usd_value)`
   - Use `formatUSD()` from `lib/format.ts` for tooltip display

3. **`apps/frontend/src/features/dashboard/WalletsList.tsx`**
   - Line 57: change `Number(BigInt(balance)) / 100000000` → `parseFloat(balance)`

4. **`apps/frontend/src/features/dashboard/RecentTransactions.tsx`**
   - Line 61: change `${(Number(BigInt(tx.usd_value)) / 100000000).toFixed(2)}` → `${parseFloat(tx.usd_value).toFixed(2)}`

5. **`apps/frontend/src/features/dashboard/DashboardPage.tsx`**
   - Line 62: change `formatUSD(Number(BigInt(totalValue)) / 100000000)` → `formatUSD(totalValue)`

6. **`apps/frontend/src/features/wallets/WalletAssets.tsx`**
   - Remove local `formatUSDValue()` function (lines 17-31)
   - Use `formatUSD()` from `lib/format.ts` instead

7. **`apps/frontend/src/features/wallets/WalletDetailPage.tsx`**
   - Line 47: change `Number(BigInt(walletBalance.total_usd)) / 100000000` → `parseFloat(walletBalance.total_usd)`

8. **`apps/frontend/src/features/wallets/WalletsPage.tsx`**
   - Line 54: change `Number(BigInt(balanceInfo.total)) / 100000000` → `parseFloat(balanceInfo.total)`

9. **`apps/frontend/src/features/wallets/WalletTransactions.tsx`**
   - Remove local `formatUSDValue()` function (lines 21-35)
   - Use `formatUSD()` from `lib/format.ts` instead

10. **`apps/frontend/src/features/transactions/TransactionsPage.tsx`**
    - Remove local `formatUSDValue()` function (lines 21-35)
    - Use `formatUSD()` from `lib/format.ts` instead

11. **`apps/frontend/src/features/transactions/TransactionDetailPage.tsx`**
    - Remove local `formatUSDValue()` function (lines 12-26)
    - Use `formatUSD()` from `lib/format.ts` instead

12. **`apps/frontend/src/features/transactions/LedgerEntriesTable.tsx`**
    - Remove local `formatUSDValue()` function (lines 18-32)
    - Use `formatUSD()` from `lib/format.ts` instead
    - Lines 49-60: simplify `.reduce()` — use `parseFloat(entry.usd_value || '0')` instead of `Number(BigInt(...)) / 100000000`
    - Lines 135, 138: can use `formatUSD()` for totals

### Step 5: Update frontend TypeScript types (documentation only)

**File**: `apps/frontend/src/types/portfolio.ts`

Add comments to clarify that `usd_value`, `price`, `total_usd`, `current_price` are now human-readable decimal strings (e.g., `"41.15"`) not raw big.Int strings.

## Files Modified Summary

### Backend (3 files)
- `apps/backend/pkg/money/usd.go` — add `FormatUSD()` function
- `apps/backend/internal/transport/httpapi/handler/portfolio.go` — use `FormatUSD()` for all USD fields
- `apps/backend/internal/module/transactions/service.go` — use `FormatUSD()` for transaction/entry USD values

### Frontend (12 files)
- `apps/frontend/src/features/dashboard/PortfolioSummary.tsx`
- `apps/frontend/src/features/dashboard/AssetDistributionChart.tsx`
- `apps/frontend/src/features/dashboard/WalletsList.tsx`
- `apps/frontend/src/features/dashboard/RecentTransactions.tsx`
- `apps/frontend/src/features/dashboard/DashboardPage.tsx`
- `apps/frontend/src/features/wallets/WalletAssets.tsx`
- `apps/frontend/src/features/wallets/WalletDetailPage.tsx`
- `apps/frontend/src/features/wallets/WalletsPage.tsx`
- `apps/frontend/src/features/wallets/WalletTransactions.tsx`
- `apps/frontend/src/features/transactions/TransactionsPage.tsx`
- `apps/frontend/src/features/transactions/TransactionDetailPage.tsx`
- `apps/frontend/src/features/transactions/LedgerEntriesTable.tsx`
- `apps/frontend/src/types/portfolio.ts` (comments only)

## Verification

1. **Backend build**: `cd apps/backend && go build ./...`
2. **Backend tests**: `cd apps/backend && go test ./pkg/money/... -v` (test new `FormatUSD`)
3. **Frontend build**: `cd apps/frontend && bun run build`
4. **Frontend tests**: `cd apps/frontend && bun run test --run`
5. **End-to-end**: `just dev` — open dashboard, check that portfolio values, wallet balances, transaction values, and ledger entries display correctly with proper USD formatting
