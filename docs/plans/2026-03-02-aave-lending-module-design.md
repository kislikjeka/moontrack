# AAVE Lending Module Design

**Date**: 2026-03-02
**Status**: Approved

## Overview

Add a dedicated lending module for AAVE (and future lending protocols) that models supply, borrow, repay, withdraw, and claim operations using proper double-entry accounting with collateral and liability accounts.

## Key Design Decisions

1. **Collateral account model** (not swap-like): Supplied assets remain "yours" — moved to a collateral account, not swapped for aTokens. Cost basis is preserved (not a taxable event).
2. **Liability accounts for debt**: Borrowed assets appear in your wallet (real on-chain balance) with a parallel liability account tracking the debt obligation.
3. **LendingPosition entity**: Separate aggregate table (like LPPosition for Uniswap V3) tracking supply/borrow amounts, interest earned/paid, position status.
4. **Cost basis for borrowed assets**: FMV at borrow time. Borrow is not taxable, but acquired assets get a cost basis.

## New Account Types

| Type | Code Format | Purpose |
|------|-------------|---------|
| `COLLATERAL` | `collateral.{protocol}.{wallet_id}.{chain_id}.{asset}` | Supplied assets locked in protocol |
| `LIABILITY` | `liability.{protocol}.{wallet_id}.{chain_id}.{asset}` | Borrowed debt obligation |

**Portfolio formula**: `Net Worth = SUM(wallet) + SUM(collateral) - SUM(liability)`

## Transaction Types & Ledger Entries

### lending_supply — Deposit collateral into AAVE

```
DEBIT  collateral.aave.{wID}.{chain}.{asset}    (collateral_increase)
CREDIT wallet.{wID}.{chain}.{asset}              (asset_decrease)
DEBIT  gas.{chain}.{feeAsset}                    (gas_fee)
CREDIT wallet.{wID}.{chain}.{feeAsset}           (asset_decrease)
```

Tax lots: transferred from wallet to collateral (cost basis preserved, like internal_transfer).

### lending_withdraw — Retrieve collateral from AAVE

```
DEBIT  wallet.{wID}.{chain}.{asset}              (asset_increase)
CREDIT collateral.aave.{wID}.{chain}.{asset}     (collateral_decrease)
DEBIT  gas.{chain}.{feeAsset}                    (gas_fee)
CREDIT wallet.{wID}.{chain}.{feeAsset}           (asset_decrease)
```

Tax lots: transferred back from collateral to wallet (cost basis preserved).

### lending_borrow — Borrow assets against collateral

```
DEBIT  wallet.{wID}.{chain}.{asset}              (asset_increase)
CREDIT liability.aave.{wID}.{chain}.{asset}      (liability_increase)
DEBIT  gas.{chain}.{feeAsset}                    (gas_fee)
CREDIT wallet.{wID}.{chain}.{feeAsset}           (asset_decrease)
```

Tax lots: created with FMV at borrow time (CostBasisFMVAtTransfer). Liability balance increases.

### lending_repay — Repay borrowed debt

```
DEBIT  liability.aave.{wID}.{chain}.{asset}      (liability_decrease)
CREDIT wallet.{wID}.{chain}.{asset}              (asset_decrease)
DEBIT  gas.{chain}.{feeAsset}                    (gas_fee)
CREDIT wallet.{wID}.{chain}.{feeAsset}           (asset_decrease)
```

Tax lots: normal FIFO disposal from wallet. Liability balance decreases.

### lending_claim — Claim rewards/interest

```
DEBIT  wallet.{wID}.{chain}.{asset}              (asset_increase)
CREDIT income.lending.{chain}.{asset}            (income)
DEBIT  gas.{chain}.{feeAsset}                    (gas_fee)
CREDIT wallet.{wID}.{chain}.{feeAsset}           (asset_decrease)
```

Tax lots: created with FMV (income is taxable at receipt).

## New Entry Types

```go
EntryTypeCollateralIncrease = "collateral_increase"
EntryTypeCollateralDecrease = "collateral_decrease"
EntryTypeLiabilityIncrease  = "liability_increase"
EntryTypeLiabilityDecrease  = "liability_decrease"
```

## TaxLotHook Changes

Supply/withdraw must carry cost basis (like internal_transfer):

- `collateral_increase` on supply: link to source lots from wallet disposal (carry basis)
- `collateral_decrease` on withdraw: link to source lots from collateral disposal (carry basis back)
- `liability_increase` on borrow: no tax lot (liability side)
- `liability_decrease` on repay: no tax lot (liability side)
- `asset_increase` on borrow: create tax lot with FMV
- `asset_decrease` on repay: normal FIFO disposal

New cost basis source: `CostBasisLendingCarryOver` for supply/withdraw lot transfers.

## LendingPosition Entity

```sql
CREATE TABLE lending_positions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    wallet_id UUID NOT NULL REFERENCES wallets(id),
    protocol VARCHAR(50) NOT NULL,
    chain_id VARCHAR(20) NOT NULL,
    supply_asset VARCHAR(20),
    supply_amount NUMERIC(78,0) DEFAULT 0,
    supply_decimals INT DEFAULT 18,
    borrow_asset VARCHAR(20),
    borrow_amount NUMERIC(78,0) DEFAULT 0,
    borrow_decimals INT DEFAULT 18,
    total_supplied NUMERIC(78,0) DEFAULT 0,
    total_withdrawn NUMERIC(78,0) DEFAULT 0,
    total_borrowed NUMERIC(78,0) DEFAULT 0,
    total_repaid NUMERIC(78,0) DEFAULT 0,
    interest_earned NUMERIC(78,0) DEFAULT 0,
    interest_paid NUMERIC(78,0) DEFAULT 0,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(wallet_id, protocol, chain_id, supply_asset, borrow_asset)
);
```

Updated via post-process hook after ledger recording (same pattern as LP positions).

## Zerion Integration

Current classifier routes AAVE operations to `defi_*` types. Changes needed:

- If `protocol == "AAVE"` and operation is deposit/withdraw/claim, route to `lending_supply`/`lending_withdraw`/`lending_claim`
- Borrow/Repay detection: Zerion may send borrows as `withdraw` and repays as `deposit` — differentiate by analyzing transfer directions (borrow = receive asset without sending equivalent)
- Add new builder functions: `buildLendingSupplyData`, `buildLendingBorrowData`, etc.

## Module Structure

```
apps/backend/internal/module/lending/
├── handler_supply.go
├── handler_withdraw.go
├── handler_borrow.go
├── handler_repay.go
├── handler_claim.go
├── entries.go
└── model.go

apps/backend/internal/platform/lendingposition/
├── model.go
├── repository.go
└── service.go
```

## DB Migration

1. ALTER accounts table CHECK constraint to include `'COLLATERAL'`, `'LIABILITY'`
2. CREATE lending_positions table
3. Add new transaction types to any relevant constraints
