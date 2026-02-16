# Plan: Swap Handler + Account Race Fix + Cross-User Wallet Fix

## Context

The Zerion sync pipeline is ~85% complete. Transfers work end-to-end, but **swaps fail** because no handler is registered for `TxTypeSwap`. When ZerionProcessor encounters a swap, `ledgerSvc.RecordTransaction()` returns "no handler registered for transaction type: swap", the sync `break`s, and the cursor doesn't advance — causing an infinite retry loop on that wallet.

Additionally, two existing bugs need fixing: a TOCTOU race condition in account creation, and a dead `GetWalletsByAddress` method that doesn't scope by user.

**Goal**: Enable wallet sync for wallets containing both transfers and swaps.

---

## Fix 1: Remove `GetWalletsByAddress` (Cross-User Leak)

**Why**: `GetWalletsByAddress()` returns wallets for ALL users — a security footgun. Both Zerion and Alchemy processors already use the safe `GetWalletsByAddressAndUserID()`. This method is dead code.

### Files to modify

| File | Change |
|------|--------|
| `apps/backend/internal/platform/wallet/port.go` | Remove `GetWalletsByAddress` from `Repository` interface |
| `apps/backend/internal/platform/sync/port.go` | Remove `GetWalletsByAddress` from `WalletRepository` interface |
| `apps/backend/internal/infra/postgres/wallet_repo.go:274-316` | Delete `GetWalletsByAddress` method |
| `apps/backend/internal/platform/sync/processor_test.go` | Remove mock method for `GetWalletsByAddress` |

---

## Fix 2: Account Creation Race Condition (`INSERT...ON CONFLICT`)

**Why**: `resolveAccounts()` at `ledger/service.go:239-244` does check-then-create — two concurrent syncs creating the same account code (e.g. `clearing.1.ETH`) race and one fails on UNIQUE constraint. Critical for swaps since clearing accounts are shared.

### Files to modify

| File | Change |
|------|--------|
| `apps/backend/internal/ledger/port.go` | Add `GetOrCreateAccount(ctx, *Account) (*Account, error)` to `Repository` interface |
| `apps/backend/internal/infra/postgres/ledger_repo.go` | Implement `GetOrCreateAccount` using `INSERT...ON CONFLICT (code) DO NOTHING` then `SELECT` |
| `apps/backend/internal/ledger/service.go:232-297` | Rewrite `resolveAccounts` to use `GetOrCreateAccount` instead of check-then-create. Remove now-unused `createAccount` method |

### `GetOrCreateAccount` SQL pattern
```sql
INSERT INTO accounts (id, code, type, asset_id, wallet_id, chain_id, created_at, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (code) DO NOTHING
```
Then `GetAccountByCode(ctx, account.Code)` to return the canonical row.

### `resolveAccounts` new logic
For each entry:
1. `generateAccountCode(entry)` — unchanged
2. `parseAccountCode(code, entry)` — get type, walletID, chainID
3. Build `Account` candidate struct
4. Call `repo.GetOrCreateAccount(ctx, candidate)` — atomic, no race
5. Set `entry.AccountID = account.ID`

---

## Fix 3: Create Swap Handler

**Why**: `ZerionProcessor.buildSwapData()` already builds swap raw data with `transfers_in`/`transfers_out` arrays. Needs a handler to generate balanced ledger entries.

### Account code prefix for clearing

`ledger/service.go:308-320` `parseAccountCode` doesn't handle `clearing.` prefix. Add it as a case (before `default`). Note: the swap handler also sets explicit `account_type` metadata, so the prefix is a fallback safety net.

### New files to create

```
apps/backend/internal/module/swap/
├── handler.go       # SwapHandler implementing ledger.Handler
├── model.go         # SwapTransaction, SwapTransfer structs
├── errors.go        # Error variables
└── handler_test.go  # Unit tests
```

### Raw data shape (from `zerion_processor.go:180-195`)

```json
{
  "wallet_id": "uuid",
  "tx_hash": "0x...",
  "chain_id": 1,
  "occurred_at": "...",
  "protocol": "uniswap_v3",
  "fee_asset": "ETH",
  "fee_amount": "21000000000000",
  "fee_decimals": 18,
  "fee_usd_price": "200000000000",
  "transfers_in": [{"asset_symbol": "USDC", "amount": "100000000", "decimals": 6, "usd_price": "100000000", "contract_address": "0x...", ...}],
  "transfers_out": [{"asset_symbol": "ETH", "amount": "50000000000000000", "decimals": 18, "usd_price": "200000000000", ...}]
}
```

### Model structs (`model.go`)

- `SwapTransaction` — top-level with `WalletID`, `TxHash`, `ChainID`, `OccurredAt`, `Protocol`, `TransfersIn []SwapTransfer`, `TransfersOut []SwapTransfer`, fee fields
- `SwapTransfer` — `AssetSymbol`, `Amount *money.BigInt`, `Decimals int`, `USDPrice *money.BigInt`, `ContractAddress`, `Sender`, `Recipient`
- JSON tags must match zerion processor keys exactly: `transfers_in`, `transfers_out`, `asset_symbol`, `usd_price`, `fee_amount`, etc.

### Entry generation logic (`handler.go GenerateEntries`)

For each **outgoing** transfer (asset leaving wallet):
1. **CREDIT** `wallet.{walletID}.{assetSymbol}` — `EntryTypeAssetDecrease`
2. **DEBIT** `clearing.{chainID}.{assetSymbol}` — `EntryTypeClearing`

For each **incoming** transfer (asset entering wallet):
3. **DEBIT** `wallet.{walletID}.{assetSymbol}` — `EntryTypeAssetIncrease`
4. **CREDIT** `clearing.{chainID}.{assetSymbol}` — `EntryTypeClearing`

Gas fee (if present, same pattern as `transfer/handler_out.go:164-223`):
5. **DEBIT** `gas.{chainID}.{feeAsset}` — `EntryTypeGasFee`
6. **CREDIT** `wallet.{walletID}.{feeAsset}` — `EntryTypeAssetDecrease`

**Balance invariant**: SUM(all debit amounts) = SUM(all credit amounts) per entry pair. Each transfer pair is self-balancing. Gas pair is self-balancing.

USD value formula (reuse from transfer handlers):
```go
usdValue = (amount * usdRate) / 10^(decimals + 8)
```

### Metadata on entries

Wallet entries: `wallet_id`, `account_code`, `tx_hash`, `chain_id`, `swap_direction`, `contract_address`
Clearing entries: `account_code`, `account_type: "CLEARING"`, `chain_id` (as string), `tx_hash`, `swap_direction`
Gas entries: same pattern as `handler_out.go`

### Handler registration (`cmd/api/main.go`)

After existing handler registrations (~line 129), add:
```go
swapHandler := swap.NewSwapHandler(walletRepo)
handlerRegistry.Register(swapHandler)
```

### Tests (`handler_test.go`)

Follow pattern from `transfer/handler_test.go`:
1. `TestSwapHandler_Type` — returns `TxTypeSwap`
2. `TestSwapHandler_SimpleSwap_Balance` — ETH→USDC, verify SUM(debit) == SUM(credit)
3. `TestSwapHandler_WithGasFee_Balance` — swap + gas, verify balance
4. `TestSwapHandler_MultiAsset` — multiple transfers_in/out
5. `TestSwapHandler_Validate_MissingFields` — error cases
6. `TestSwapHandler_Validate_NoTransfers` — empty transfers

---

## Implementation Order

1. **Fix 1** — Remove `GetWalletsByAddress` (smallest, no deps)
2. **Fix 2** — `GetOrCreateAccount` + rewrite `resolveAccounts` (needed before swap works under concurrency)
3. **Fix 3** — Swap handler + `clearing.` prefix in parseAccountCode + registration in main.go + tests

---

## Verification

```bash
cd apps/backend

# Compile check after each fix
go build ./...

# Run affected test suites
go test ./internal/ledger/...
go test ./internal/module/swap/...
go test ./internal/module/transfer/...
go test ./internal/platform/sync/...
go test ./internal/infra/postgres/...

# Full test suite
go test ./...
```
