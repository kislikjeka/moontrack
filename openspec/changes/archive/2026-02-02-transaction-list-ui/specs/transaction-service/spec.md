## ADDED Requirements

### Requirement: Transaction service provides aggregated transaction data

The system SHALL provide a dedicated transaction service module (`internal/modules/transactions/`) that aggregates transaction data from the ledger and formats it for API consumption. The service SHALL NOT modify ledger data or bypass ledger interfaces.

#### Scenario: Service loads transaction with ledger entries
- **WHEN** service receives request for transaction by ID
- **THEN** system returns transaction with all associated ledger entries (debit and credit)

#### Scenario: Service loads transaction list with display fields
- **WHEN** service receives request for transaction list
- **THEN** system returns transactions with human-readable fields (asset_symbol, display_amount, wallet_name)

### Requirement: Transaction detail endpoint returns full transaction data

The system SHALL expose `GET /transactions/:id` endpoint that returns a single transaction with its ledger entries and formatted display fields. The endpoint SHALL require JWT authentication.

#### Scenario: Successful transaction detail retrieval
- **WHEN** authenticated user requests `GET /transactions/:id` with valid transaction ID
- **THEN** system returns 200 with transaction object containing:
  - Transaction metadata (id, type, source, status, created_at)
  - Parsed transaction parameters from raw_data
  - Array of ledger entries with account info, debit/credit, amount, asset

#### Scenario: Transaction not found
- **WHEN** authenticated user requests `GET /transactions/:id` with non-existent ID
- **THEN** system returns 404 with error message

#### Scenario: Unauthorized access
- **WHEN** unauthenticated user requests `GET /transactions/:id`
- **THEN** system returns 401 with error message

### Requirement: Transaction list response includes display-friendly fields

The system SHALL extend `GET /transactions` response to include human-readable fields for each transaction. Amounts SHALL be returned as strings to preserve precision.

#### Scenario: Transaction list includes display fields
- **WHEN** authenticated user requests `GET /transactions`
- **THEN** each transaction in response includes:
  - `display_amount`: Formatted amount with asset decimals applied
  - `asset_symbol`: Human-readable asset symbol (e.g., "ETH", "BTC")
  - `wallet_name`: Name of the associated wallet (if applicable)

#### Scenario: Different transaction types formatted correctly
- **WHEN** transaction list includes income, outcome, and adjustment transactions
- **THEN** each type displays appropriate amount sign and description based on transaction semantics

### Requirement: Transaction service respects user authorization

The system SHALL only return transactions belonging to the authenticated user. Cross-user transaction access SHALL be prevented.

#### Scenario: User can only see own transactions
- **WHEN** authenticated user requests transaction list or detail
- **THEN** system returns only transactions associated with user's wallets

#### Scenario: User cannot access other user's transaction
- **WHEN** authenticated user requests transaction detail for another user's transaction ID
- **THEN** system returns 404 (not 403, to prevent ID enumeration)
