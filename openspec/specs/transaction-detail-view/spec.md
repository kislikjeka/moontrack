## ADDED Requirements

### Requirement: Transaction detail page displays full transaction information

The system SHALL provide a dedicated page at `/transactions/:id` that displays complete transaction details including type-specific parameters and all associated ledger entries.

#### Scenario: User navigates to transaction detail
- **WHEN** user clicks on a transaction in the transaction list
- **THEN** system navigates to `/transactions/:id` and displays full transaction details

#### Scenario: Transaction header shows key information
- **WHEN** transaction detail page loads
- **THEN** page displays transaction type, status, date, and primary amount prominently

### Requirement: Transaction detail page shows ledger entries

The system SHALL display all ledger entries associated with a transaction, showing the double-entry accounting view with debit and credit entries.

#### Scenario: Ledger entries displayed in table format
- **WHEN** transaction detail page loads successfully
- **THEN** page displays a table with columns: Account, Debit/Credit, Amount, Asset, USD Value

#### Scenario: Entries show account context
- **WHEN** ledger entry belongs to a wallet account
- **THEN** entry displays wallet name and account type (e.g., "Main Wallet - Asset Account")

#### Scenario: Entry amounts preserve precision
- **WHEN** displaying ledger entry amounts
- **THEN** amounts display full precision without floating-point rounding (using string formatting)

### Requirement: Transaction detail page handles loading and error states

The system SHALL provide appropriate feedback during data loading and when errors occur.

#### Scenario: Loading state displayed
- **WHEN** transaction data is being fetched
- **THEN** page displays loading indicator

#### Scenario: Error state for not found
- **WHEN** transaction does not exist or user lacks access
- **THEN** page displays user-friendly error message with option to return to transaction list

#### Scenario: Error state for network failure
- **WHEN** API request fails due to network error
- **THEN** page displays error message with retry option

### Requirement: Transaction list displays formatted transaction data

The system SHALL display transactions in a clear, readable format showing type, asset, amount, wallet, and date instead of raw JSON data.

#### Scenario: Transaction row shows key fields
- **WHEN** transaction list renders a transaction
- **THEN** row displays: transaction type (human-readable), asset symbol, formatted amount, wallet name, and date

#### Scenario: Amount displayed with correct sign
- **WHEN** displaying income transaction
- **THEN** amount shows positive value with appropriate styling (e.g., green color)

#### Scenario: Amount displayed with correct sign for outcome
- **WHEN** displaying outcome transaction
- **THEN** amount shows negative value with appropriate styling (e.g., red color)

#### Scenario: Transaction row is clickable
- **WHEN** user clicks on a transaction row
- **THEN** system navigates to transaction detail page

### Requirement: Transaction list handles empty state

The system SHALL display appropriate message when user has no transactions.

#### Scenario: Empty state message
- **WHEN** authenticated user has no transactions
- **THEN** page displays helpful message indicating no transactions exist and suggesting to add one
