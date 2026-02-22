# Fix: Ledger Entries Show $0 USD Values

## Context

Ledger entries display Debit/Credit as $0.00 in the UI despite Zerion API providing valid price data. The Zerion prices flow correctly through the adapter and processor layers, but the **USD value calculation formula in all handlers divides by too much**, producing near-zero values. The frontend then divides by 1e8 again (correct per convention), resulting in $0.00.

### The Convention (established by portfolio, frontend, and all display code)
- `usd_rate` = price in USD, scaled by 1e8. Example: $2000.00 → `200000000000`
- `usd_value` = dollar value, scaled by 1e8. Example: $2000.00 → `200000000000`
- Frontend: `Number(BigInt(value)) / 1e8` to display

### The Bug
All handlers use `10^(decimals + 8)` as divisor instead of `10^decimals`:

```
WRONG:  usd_value = (amount * usd_rate) / 10^(decimals + 8)  → whole dollars (loses precision, frontend shows $0)
CORRECT: usd_value = (amount * usd_rate) / 10^(decimals)      → dollars * 1e8 (matches convention)
```

Proof: 1 ETH at $2000 (amount=10^18, decimals=18, usd_rate=200000000000):
- Wrong:   `(10^18 * 200000000000) / 10^26 = 2000` → frontend: `2000 / 1e8 = $0.00002`
- Correct: `(10^18 * 200000000000) / 10^18 = 200000000000` → frontend: `200000000000 / 1e8 = $2000.00`

## Changes

### 1. Fix transfer handlers — change `decimals+8` to `decimals`

**`apps/backend/internal/module/transfer/handler_in.go:115`**
```go
// FROM:
divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(txn.Decimals+8)), nil)
// TO:
divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(txn.Decimals)), nil)
```

**`apps/backend/internal/module/transfer/handler_out.go:116`** — same fix for main transfer

**`apps/backend/internal/module/transfer/handler_out.go:180`** — gas fee: change `18+8` to `18`

**`apps/backend/internal/module/transfer/handler_internal.go:126`** — main transfer: change `txn.Decimals+8` to `txn.Decimals`

**`apps/backend/internal/module/transfer/handler_internal.go:199`** — gas fee: change `gasDecimals+8` to `gasDecimals`

### 2. Fix swap handler — single shared function

**`apps/backend/internal/module/swap/handler.go:294`**
```go
// FROM:
divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals+8)), nil)
// TO:
divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
```

Also update the comment on line 288 from `(amount * usdRate) / 10^(decimals+8)` to `(amount * usdRate) / 10^decimals`.

### 3. Fix adjustment handler — different bug (divides by `10^8` instead of `10^decimals`)

**`apps/backend/internal/module/adjustment/handler.go:187-196`**

Change `calculateUSDValue` signature to accept `decimals`:
```go
func calculateUSDValue(amount, usdRate *big.Int, decimals int) *big.Int {
    if usdRate == nil || usdRate.Sign() == 0 {
        return big.NewInt(0)
    }
    value := new(big.Int).Mul(amount, usdRate)
    divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
    return value.Div(value, divisor)
}
```

Update 4 call sites (lines 111, 126, 142, 157) to pass `tx.Decimals`.

### 4. Update model comment

**`apps/backend/internal/ledger/model.go:236`**
```go
// FROM:
USDValue *big.Int // amount * usd_rate / 10^8
// TO:
USDValue *big.Int // (amount * usd_rate) / 10^decimals — USD value scaled by 10^8
```

### 5. Fix adjustment test expected values

**`apps/backend/internal/module/adjustment/handler_test.go`**

The test `TestAssetAdjustmentHandler_USDValueCalculation` has wrong expected values:
- Line 432: ETH test — change `"2000000000000000000000"` to `"200000000000"` (= 2000 * 1e8)
- Line 439: BTC test — `"2000000000000"` is actually already correct for the new formula: `(50000000 * 4000000000000) / 10^8 = 2000000000000`. Wait — with the new formula using decimals=8: `(50000000 * 4000000000000) / 10^8 = 2000000000000`. This value is 2000000000000 / 1e8 = $20,000. But 0.5 BTC * $40,000 = $20,000. So this value is already correct for `10^decimals` formula! No change needed here.
- Line 432 only: change to `"200000000000"` — that's `(10^18 * 200000000000) / 10^18 = 200000000000`, which is 200000000000 / 1e8 = $2,000. Correct!

### 6. No data migration

Existing entries with wrong values will be fixed on next re-sync. No migration needed.

## Files to Modify

| File | Change |
|------|--------|
| `apps/backend/internal/module/transfer/handler_in.go` | `decimals+8` → `decimals` (line 115) |
| `apps/backend/internal/module/transfer/handler_out.go` | `decimals+8` → `decimals` (line 116), `18+8` → `18` (line 180) |
| `apps/backend/internal/module/transfer/handler_internal.go` | `decimals+8` → `decimals` (line 126), `gasDecimals+8` → `gasDecimals` (line 199) |
| `apps/backend/internal/module/swap/handler.go` | `decimals+8` → `decimals` (line 294), update comment (line 288) |
| `apps/backend/internal/module/adjustment/handler.go` | Add `decimals` param to `calculateUSDValue`, use `10^decimals` |
| `apps/backend/internal/module/adjustment/handler_test.go` | Fix ETH expected value on line 432 |
| `apps/backend/internal/ledger/model.go` | Update USDValue comment (line 236) |

## Verification

1. `cd apps/backend && go build ./...` — must compile
2. `cd apps/backend && go test ./internal/module/...` — all tests pass
3. Re-sync wallets to get new entries with correct USD values
4. Check `GET /transactions/{id}` → entries should have non-zero `usd_value`
5. Frontend Ledger Entries table should show correct dollar amounts
