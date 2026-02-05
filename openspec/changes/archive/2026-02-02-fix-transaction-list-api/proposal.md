## Why

The frontend `TransactionItem` component expects enriched transaction data (type_label, asset_symbol, display_amount, direction, wallet_name, usd_value) but the backend `GET /transactions` endpoint returns only basic `TransactionResponse` fields (id, type, source, status, occurred_at, recorded_at, raw_data). This causes the transaction list to display empty/missing information to users.

## What Changes

- **BREAKING** Modify `GET /transactions` response to return `TransactionListItem` with enriched fields instead of basic `TransactionResponse`
- Add new response struct `TransactionListItemResponse` with:
  - `type_label`: Human-readable type name ("Income", "Outcome", "Adjustment")
  - `asset_id`, `asset_symbol`: Asset identifier and symbol (e.g., "BTC")
  - `amount`, `display_amount`: Raw amount and formatted display string (e.g., "0.5 BTC")
  - `direction`: Transaction direction ("in", "out", "adjustment")
  - `wallet_id`, `wallet_name`: Wallet identifier and name
  - `usd_value`: Optional USD value at time of transaction
- Enrich transaction data by joining with ledger entries, wallets, and assets during list query
- Keep existing `GET /transactions/{id}` detail endpoint unchanged (already returns full detail)

## Capabilities

### New Capabilities

- `transaction-list-enrichment`: Backend enrichment of transaction list items with derived display fields from ledger entries, wallets, and assets

### Modified Capabilities

_None - this is a fix to match expected API contract, not a change to existing spec requirements_

## Impact

- **Backend**: `internal/api/handlers/transaction_handler.go` - new response struct and enrichment logic in `GetTransactions`
- **Backend**: May need repository method to fetch transactions with joined wallet/asset data
- **API Contract**: `GET /transactions` response structure changes (**BREAKING** for any other clients)
- **Frontend**: No changes needed - already expects the enriched format
