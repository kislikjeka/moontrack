# Plan: DeFi Handler Audit Fixes — Tests & Defensive Improvements

## Context

Three independent audit agents reviewed the DeFi handlers (deposit, withdraw, claim) for transactional safety, mathematical correctness, and test coverage. Results:

- **Transactional safety**: PASS — all double-entry, atomicity, idempotency, and negative balance checks are correct
- **Mathematical correctness**: PASS — all CalcUSDValue calls, price fallback formula, and big.Int operations are correct
- **Test coverage**: Multiple HIGH and MEDIUM gaps found

No production code bugs were found. This plan addresses **test gaps** and **two minor defensive code improvements** identified by the auditors.

## Changes

### 1. Test Fixes in `handler_test.go`

**File**: `apps/backend/internal/module/defi/handler_test.go`

#### HIGH priority

**A. Add `assertEntriesBalanced` to 4 tests that generate entries but don't verify balance:**
- `TestDeFiDepositHandler_USDPriceFallback` (line ~294)
- `TestDeFiDepositHandler_EntryMetadata` (line ~338)
- `TestDeFiWithdrawHandler_EntryMetadata` (line ~650)
- `TestDeFiClaimHandler_EntryMetadata` (line ~868)

**B. USD price fallback — verify exact computed values (not just `Sign() > 0`):**

Replace weak assertions in `TestDeFiDepositHandler_USDPriceFallback` with exact math:
```
OUT: amount=1000000, decimals=8, usd_price=6700000000000
totalOutUSDValue = CalcUSDValue(1000000, 6700000000000, 8) = 67000000000

IN: amount=1000000, decimals=8
fallback usdRate = (67000000000 * 10^8) / 1000000 = 6700000000000
usdValue = CalcUSDValue(1000000, 6700000000000, 8) = 67000000000
```
Assert: `inWalletEntry.USDRate == 6700000000000` and `inWalletEntry.USDValue == 67000000000`

**C. Add USD fallback test with DIFFERENT amounts (IN ≠ OUT):**

New test `TestDeFiDepositHandler_USDPriceFallback_DifferentAmounts`:
```
OUT: cbBTC amount=981547, decimals=8, usd_price=6795302440668
  → totalOutUSDValue = (981547 * 6795302440668) / 10^8 = 66727350686

IN: aBascbBTC amount=981581, decimals=8, usd_price=0
  → fallback usdRate = (66727350686 * 10^8) / 981581 = 6795070571349
  → usdValue = CalcUSDValue(981581, 6795070571349, 8) = 66725044288
```
Assert exact computed values. The ~$0.02 difference vs totalOutUSDValue is expected integer truncation.

**D. Add zero-amount validation test:**

New subtest in `TestDeFiDepositHandler_Validate_MissingFields`:
```go
{name: "zero amount", amount: "0", expectedErr: defi.ErrInvalidAmount}
```

**E. Add authorization test for all 3 handlers:**

New test `TestDeFiHandlers_Unauthorized` — table-driven for deposit, withdraw, claim:
- Create wallet with userID_A
- Set context with userID_B via `middleware.GetUserIDFromContext`
- Assert `ErrUnauthorized`

Reference pattern: `apps/backend/internal/module/swap/handler.go:87-91` (same auth check, also untested in swap, but we'll add it for DeFi)

#### MEDIUM priority

**F. Add multi-asset DeFi test (2 OUT + 2 IN):**

New test `TestDeFiDepositHandler_MultiAsset_Balance` matching swap's `TestSwapHandler_MultiAsset` pattern. Verify 8 entries, balance holds.

**G. Expand withdraw validation tests to match deposit:**

Add missing subtests to `TestDeFiWithdrawHandler_Validate_MissingFields`:
- `missing tx_hash`
- `missing chain_id`
- `negative amount`
- `missing asset symbol`

### 2. Defensive Code Improvement in `model.go`

**File**: `apps/backend/internal/module/defi/model.go`

**H. Add `Decimals >= 0` check in `DeFiTransfer.Validate()`:**

Math auditor found: negative decimals would cause `CalcUSDValue` to panic (10^negative = 0, division by zero). While theoretical (real tokens always have decimals 0-18), it's a cheap guard.

```go
func (t *DeFiTransfer) Validate() error {
    ...
    if t.Decimals < 0 {
        return ErrInvalidDecimals
    }
    ...
}
```

Add `ErrInvalidDecimals` to `errors.go`.

## Files to modify

| File | Changes |
|---|---|
| `apps/backend/internal/module/defi/handler_test.go` | Items A-G: balance assertions, exact USD math, auth tests, multi-asset, zero-amount, withdraw validation |
| `apps/backend/internal/module/defi/model.go` | Item H: `Decimals >= 0` guard in `DeFiTransfer.Validate()` |
| `apps/backend/internal/module/defi/errors.go` | Item H: Add `ErrInvalidDecimals` |

## What we're NOT fixing (and why)

- **Authorization bypass in sync context** (finding 5.2) — by design, sync runs as a system process without JWT context. Not a vulnerability in current architecture.
- **Nil USDPrice JSON deserialization** — production path (zerion_processor) always sends `"0"`, never null. Low risk.
- **Missing `occurred_at` validation** — same gap exists in swap/transfer handlers. Would be a cross-cutting change, not DeFi-specific.
- **Negative USDPrice guard** — practically impossible from upstream Zerion data. Lowest priority.

## Verification

1. `cd apps/backend && go build ./...` — must compile
2. `go test -v ./internal/module/defi/...` — all tests pass
3. `go test ./...` — no regressions
