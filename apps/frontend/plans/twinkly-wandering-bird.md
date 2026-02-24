# Filter LotDetailTable by chain_id

## Context

`WalletHoldings` has a 3-level drill-down: Asset → Chain → Tax Lots. When user expands a specific chain (e.g. "Base"), `LotDetailTable` is rendered — but it fetches ALL lots for the wallet+asset across all chains. The user sees Arbitrum lots inside the Base section, which is confusing and makes FIFO look broken.

## Changes

### 1. `LotDetailTable` — accept optional `chainId` prop, filter lots client-side

**File**: `apps/frontend/src/components/domain/LotDetailTable.tsx`

- Add optional `chainId?: string` to `LotDetailTableProps`
- When `chainId` is provided, filter `lots` by `lot.chain_id === chainId` before rendering
- Remove the "Chain" column (it's redundant when viewing lots under a specific chain)
- Revert sort to simple: open first, oldest first (no chain grouping needed)

### 2. `WalletHoldings` — pass `chainId` to `LotDetailTable`

**File**: `apps/frontend/src/features/wallets/WalletHoldings.tsx`  (line 312)

- Pass `chainId={chain.chainId}` to `<LotDetailTable>` in `ChainRows`

## Verification

1. `cd apps/frontend && bun run test --run`
2. Open Holdings → expand multi-chain asset → expand specific chain → confirm only that chain's lots appear
