## ADDED Requirements

### Requirement: Transaction list returns enriched items
The `GET /transactions` endpoint SHALL return `TransactionListItem` objects with enriched display fields instead of raw transaction data.

#### Scenario: List transactions returns enriched data
- **WHEN** authenticated user requests `GET /transactions`
- **THEN** response contains array of transactions with fields: `id`, `type`, `type_label`, `asset_id`, `asset_symbol`, `amount`, `display_amount`, `direction`, `wallet_id`, `wallet_name`, `status`, `occurred_at`

#### Scenario: Income transaction has correct enrichment
- **WHEN** transaction type is `manual_income`
- **THEN** `type_label` is "Income" AND `direction` is "in"

#### Scenario: Outcome transaction has correct enrichment
- **WHEN** transaction type is `manual_outcome`
- **THEN** `type_label` is "Outcome" AND `direction` is "out"

#### Scenario: Adjustment transaction has correct enrichment
- **WHEN** transaction type is `asset_adjustment`
- **THEN** `type_label` is "Adjustment" AND `direction` is "adjustment"

### Requirement: Display amount formatting
The system SHALL format `display_amount` from base units to human-readable format with asset symbol.

#### Scenario: Bitcoin amount formatting
- **WHEN** asset is BTC with amount 50000000 (satoshi)
- **THEN** `display_amount` is "0.5 BTC"

#### Scenario: Ethereum amount formatting
- **WHEN** asset is ETH with amount 1000000000000000000 (wei)
- **THEN** `display_amount` is "1 ETH"

#### Scenario: Whole number amount formatting
- **WHEN** amount has no fractional part
- **THEN** `display_amount` omits decimal point (e.g., "10 BTC" not "10.00000000 BTC")

### Requirement: Wallet name resolution
The system SHALL resolve `wallet_name` from wallet ID for each transaction.

#### Scenario: Wallet name included in response
- **WHEN** transaction is associated with a wallet
- **THEN** `wallet_name` contains the wallet's display name

#### Scenario: Missing wallet handled gracefully
- **WHEN** wallet cannot be found (deleted or inaccessible)
- **THEN** `wallet_name` is empty string AND transaction is still returned

### Requirement: Asset symbol derivation
The system SHALL derive `asset_symbol` from the asset ID.

#### Scenario: Asset symbol is uppercase
- **WHEN** asset_id is "btc" or "bitcoin"
- **THEN** `asset_symbol` is "BTC"

### Requirement: USD value inclusion
The system SHALL include `usd_value` when available from ledger entries.

#### Scenario: USD value present
- **WHEN** transaction has USD value recorded
- **THEN** `usd_value` field contains the value as string

#### Scenario: USD value absent
- **WHEN** transaction has no USD value
- **THEN** `usd_value` field is omitted from response

### Requirement: Pagination preserved
The response SHALL maintain pagination structure with `total`, `page`, and `page_size` fields.

#### Scenario: Paginated response structure
- **WHEN** user requests transactions with pagination
- **THEN** response contains `transactions` array plus `total`, `page`, `page_size` fields
