# Fix: Data shape mismatch between ZerionProcessor and transfer handlers

## Context

Wallet `2f646418` is stuck in an infinite sync loop. Every 5 minutes sync starts, hits transaction `9cd52e6be6555c579156b48c8db78d01` (USDC receive on Base), fails with "invalid asset ID", and doesn't advance the cursor.

**Root cause**: `ZerionProcessor.buildTransferInData()` builds data with a `transfers` array (matching the swap handler's format), but `TransferInHandler` / `TransferOutHandler` expect flat top-level fields.

### What the processor sends:
```json
{
  "wallet_id": "...", "tx_hash": "...", "chain_id": 8453, "occurred_at": "...",
  "fee_asset": "ETH", "fee_amount": "6170464924595", ...
  "transfers": [
    {"asset_symbol": "USDC", "amount": "2910829", "decimals": 6,
     "contract_address": "0x833589...", "direction": "in",
     "sender": "0xad3b67...", "recipient": "0x1f2f93...", "usd_price": "99285449"}
  ]
}
```

### What the handler expects:
```json
{
  "wallet_id": "...", "asset_id": "USDC", "amount": "2910829", "decimals": 6,
  "chain_id": 8453, "tx_hash": "...", "block_number": 42233339,
  "from_address": "0xad3b67...", "contract_address": "0x833589...",
  "occurred_at": "...", "unique_id": "...", "usd_rate": "99285449"
}
```

Key mismatches:
- `asset_symbol` inside `transfers[]` vs `asset_id` at top level
- `amount`, `decimals`, `contract_address` nested vs flat
- `sender`/`recipient` vs `from_address`/`to_address`
- `usd_price` vs `usd_rate`
- Missing `block_number`, `unique_id`
- Also: `TransferOutHandler` expects `gas_amount`/`gas_usd_rate` at top level for fee entries

**Note**: The swap handler (`SwapTransaction`) correctly expects `transfers_in`/`transfers_out` arrays — `buildSwapData()` is compatible. Only `transfer_in` and `transfer_out` are broken.

## Plan

Fix `buildTransferInData()` and `buildTransferOutData()` in `zerion_processor.go` to produce flat data matching the handler models. Each transfer_in/transfer_out transaction has exactly one primary transfer.

### File: `apps/backend/internal/platform/sync/zerion_processor.go`

**1. `buildTransferInData()`** (line 175-179) — extract first transfer and flatten:
```go
func (p *ZerionProcessor) buildTransferInData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
    data := p.buildBaseData(w, tx)
    // Find the primary transfer (first "in" direction)
    for _, t := range tx.Transfers {
        if t.Direction == DirectionIn {
            data["asset_id"] = t.AssetSymbol
            data["amount"] = money.NewBigInt(t.Amount).String()
            data["decimals"] = t.Decimals
            data["contract_address"] = t.ContractAddress
            data["from_address"] = t.Sender
            data["unique_id"] = tx.ID
            if t.USDPrice != nil {
                data["usd_rate"] = t.USDPrice.String()
            }
            break
        }
    }
    // Fallback: if no "in" transfer found, use first transfer
    if _, ok := data["asset_id"]; !ok && len(tx.Transfers) > 0 {
        t := tx.Transfers[0]
        data["asset_id"] = t.AssetSymbol
        data["amount"] = money.NewBigInt(t.Amount).String()
        data["decimals"] = t.Decimals
        data["contract_address"] = t.ContractAddress
        data["from_address"] = t.Sender
        data["unique_id"] = tx.ID
        if t.USDPrice != nil {
            data["usd_rate"] = t.USDPrice.String()
        }
    }
    return data
}
```

**2. `buildTransferOutData()`** (line 181-185) — extract first transfer and add gas:
```go
func (p *ZerionProcessor) buildTransferOutData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
    data := p.buildBaseData(w, tx)
    // Find the primary transfer (first "out" direction)
    for _, t := range tx.Transfers {
        if t.Direction == DirectionOut {
            data["asset_id"] = t.AssetSymbol
            data["amount"] = money.NewBigInt(t.Amount).String()
            data["decimals"] = t.Decimals
            data["contract_address"] = t.ContractAddress
            data["to_address"] = t.Recipient
            data["unique_id"] = tx.ID
            if t.USDPrice != nil {
                data["usd_rate"] = t.USDPrice.String()
            }
            break
        }
    }
    // Fallback
    if _, ok := data["asset_id"]; !ok && len(tx.Transfers) > 0 {
        t := tx.Transfers[0]
        data["asset_id"] = t.AssetSymbol
        data["amount"] = money.NewBigInt(t.Amount).String()
        data["decimals"] = t.Decimals
        data["contract_address"] = t.ContractAddress
        data["to_address"] = t.Recipient
        data["unique_id"] = tx.ID
        if t.USDPrice != nil {
            data["usd_rate"] = t.USDPrice.String()
        }
    }
    // Gas fee as top-level fields (from buildBaseData fee_*)
    // fee_amount/fee_usd_price already set by buildBaseData
    // Map them to gas_amount/gas_usd_rate for TransferOutHandler
    if feeAmt, ok := data["fee_amount"]; ok {
        data["gas_amount"] = feeAmt
    }
    if feeRate, ok := data["fee_usd_price"]; ok {
        data["gas_usd_rate"] = feeRate
    }
    return data
}
```

**3. `buildInternalTransferData()`** (line 204-212) — same pattern, flatten the transfer:
```go
// Extract primary transfer (the "out" direction from source wallet)
for _, t := range tx.Transfers {
    if t.Direction == DirectionOut {
        data["asset_id"] = t.AssetSymbol
        data["amount"] = money.NewBigInt(t.Amount).String()
        data["decimals"] = t.Decimals
        data["contract_address"] = t.ContractAddress
        data["unique_id"] = tx.ID
        if t.USDPrice != nil {
            data["usd_rate"] = t.USDPrice.String()
        }
        break
    }
}
// Gas fee mapping
if feeAmt, ok := data["fee_amount"]; ok {
    data["gas_amount"] = feeAmt
}
if feeRate, ok := data["fee_usd_price"]; ok {
    data["gas_usd_rate"] = feeRate
}
if feeDec, ok := data["fee_decimals"]; ok {
    data["gas_decimals"] = feeDec
}
if feeAsset, ok := data["fee_asset"]; ok {
    data["native_asset_id"] = feeAsset
}
```

**4. DeFi handlers** (`buildDeFiDepositData`, `buildDeFiWithdrawData`, `buildDeFiClaimData`) — no module exists yet, skip for now. If they use the transfers array pattern like swap, they'll work when DeFi handlers are created.

### File: `apps/backend/internal/infra/gateway/zerion/client.go`

**5. Remove debug raw_body logging** — the always-on raw body logging was temporary for investigation. Revert to error-only logging:
```go
// Remove: c.logger.Debug("raw Zerion response", ...)
// Keep: error logging if unmarshal fails
```

## Verification

1. `cd apps/backend && go build ./...` — must compile
2. `cd apps/backend && go test ./internal/module/transfer/...` — existing tests must pass
3. `just backend-restart` — deploy
4. Check Loki logs: `{service="backend"} | json | component="sync" | wallet_id="2f646418-af9d-462a-a298-54cbbf0f5fc4"` — should see `transactions_processed > 0`
5. Verify no more "invalid asset ID" errors: `{service="backend"} | json | level="error" | error=~"invalid asset"`
