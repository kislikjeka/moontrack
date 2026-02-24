# Fix: Zerion sync drops NFT-involved transactions (missing fungible outflows)

## Context

A Uniswap V3 LP multicall on Base (2026/02/09) sent -0.4949 ETH and -496.24 USDC to mint an LP NFT. MoonTrack never recorded these fungible outflows — the entire transaction is invisible.

**Root cause (5 Whys):**
1. Transaction never stored as `RawTransaction` — never entered the pipeline
2. Zerion API didn't return it in the response
3. **`filter[asset_types]=fungible`** in `client.go:135` excludes transactions involving NFTs at the transaction level (not transfer level)
4. MoonTrack has no NFT data models (`types.go` has `FungibleInfo` but no `NftInfo`)
5. LP NFTs were out of scope — complex asset class

**Impact:** Fungible balances are overstated. Any DeFi interaction minting/burning an NFT (Uniswap V3/V4 LP, etc.) silently drops the associated fungible flows.

---

## Implementation Plan

**Strategy:** Widen the Zerion API filter to `fungible,nft`, then skip NFT transfers in the adapter so only fungible transfers enter the pipeline. The classifier gets a safety net for zero-transfer transactions.

### Step 1: Add `NftInfo` type to Zerion types

**File:** `apps/backend/internal/infra/gateway/zerion/types.go`

Add `NftInfo` struct and `NftInfo` field on `ZTransfer`:

```go
// NftInfo describes an NFT involved in a transfer
type NftInfo struct {
    ContractAddress string `json:"contract_address"`
    TokenID         string `json:"token_id"`
    Name            string `json:"name"`
    Interface       string `json:"interface"` // "erc721" or "erc1155"
}
```

Add to `ZTransfer`:
```go
NftInfo *NftInfo `json:"nft_info"` // non-nil for NFT transfers
```

### Step 2: Widen the API filter

**File:** `apps/backend/internal/infra/gateway/zerion/client.go` (line 135)

```go
// Before:
params.Set("filter[asset_types]", "fungible")
// After:
params.Set("filter[asset_types]", "fungible,nft")
```

### Step 3: Filter NFT transfers in the adapter

**File:** `apps/backend/internal/infra/gateway/zerion/adapter.go` — `convertTransaction()` (lines 63-67)

Replace the transfer loop to skip NFT transfers:

```go
transfers := make([]sync.DecodedTransfer, 0, len(td.Attributes.Transfers))
for _, zt := range td.Attributes.Transfers {
    if zt.FungibleInfo == nil {
        continue // skip NFT transfers — only track fungible assets
    }
    dt := convertTransfer(zt, chain)
    transfers = append(transfers, dt)
}
```

### Step 4: Safety net in classifier for zero-transfer transactions

**File:** `apps/backend/internal/platform/sync/classifier.go` — `Classify()` (line 15)

Add early return before the switch, so NFT-only transactions (now arriving with zero fungible transfers) are skipped instead of hitting handler validation errors like `ErrNoTransfers`:

```go
func (c *Classifier) Classify(tx DecodedTransaction) ledger.TransactionType {
    if len(tx.Transfers) == 0 {
        return "" // no fungible transfers to process (e.g., NFT-only transaction)
    }
    switch tx.OperationType {
    // ... unchanged
    }
}
```

This is safe because:
- `OpApprove` already returns `""` (no transfers)
- `classifyExecute` already checks `len == 0`
- No handler can produce balanced entries with zero transfers anyway

---

## Files Modified (4 files, ~15 lines changed)

| File | Change |
|------|--------|
| `apps/backend/internal/infra/gateway/zerion/types.go` | Add `NftInfo` struct + field on `ZTransfer` |
| `apps/backend/internal/infra/gateway/zerion/client.go` | `"fungible"` → `"fungible,nft"` |
| `apps/backend/internal/infra/gateway/zerion/adapter.go` | Skip NFT transfers in `convertTransaction()` |
| `apps/backend/internal/platform/sync/classifier.go` | Early return `""` for empty transfers |

## Future-Proofing for LP Position Tracking

The `NftInfo` struct added in Step 1 ensures Zerion responses deserialize correctly. The full raw JSON (including `nft_info`) is stored in `raw_transactions.raw_json` by the Collector. When LP position tracking is added later:

- The historical NFT data is already preserved in raw_transactions
- A new `nft_info` field can be added to `DecodedTransfer` in `sync/model.go`
- A new handler (e.g., `lp_position_open`/`lp_position_close`) can be registered
- The adapter's NFT filter (`FungibleInfo == nil → skip`) becomes a feature gate to remove

No re-sync from Zerion needed — just reprocess existing raw_transactions.

## Verification

1. `cd apps/backend && go build ./...` — must compile
2. `cd apps/backend && go test ./internal/platform/sync/... -v` — existing tests pass
3. `cd apps/backend && go test ./internal/infra/gateway/zerion/... -v` — existing tests pass
4. Manual: re-sync the wallet, verify the Uniswap V3 multicall's ETH+USDC outflows now appear as a `defi_deposit` transaction
5. Confirm NFT-only transactions (pure NFT transfers/mints) are silently skipped without errors
