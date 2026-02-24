# Fix Gain/Loss Calculation in Consumed Lots Table

## Context

The "Consumed Lots (FIFO)" table in transaction details calculates Gain/Loss incorrectly. It computes `proceeds_per_unit - cost_basis_per_unit` (a per-unit difference), but this must be multiplied by `quantity_disposed` to get the actual total gain/loss.

**Example from screenshot:**
- Transfer Out: 0.0014965919826 ETH ($2.94 total value)
- Current (wrong): Gain/Loss = $1,965.80 - $2,042.10 = **-$76.30**
- Correct: Gain/Loss = (1,965.80 - 2,042.10) × 0.0014965919826 = **-$0.11**

## Root Cause

**File**: `apps/frontend/src/features/transactions/TransactionLotImpactSection.tsx` line 125

```typescript
// BUG: per-unit difference, not total gain/loss
const gainLoss = parseFloat(disposal.proceeds_per_unit) - parseFloat(disposal.lot_cost_basis_per_unit)
```

Backend returns correct per-unit values. The frontend simply forgets to multiply by quantity.

## Fix

In `TransactionLotImpactSection.tsx` line 125, change the calculation to:

```typescript
const qty = parseFloat(disposal.quantity_disposed)
const gainLoss = (parseFloat(disposal.proceeds_per_unit) - parseFloat(disposal.lot_cost_basis_per_unit)) * qty
```

**One line changed, one line added.** No backend changes needed.

## Verification

1. `cd apps/frontend && bun run build` — confirm no build errors
2. `cd apps/frontend && bun run test --run` — confirm no test failures
3. Manual check: open a transaction with consumed lots, verify Gain/Loss shows small dollar amounts proportional to the disposed quantity (not per-unit price differences)
