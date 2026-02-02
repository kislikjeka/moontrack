# Feature Specification: Crypto Portfolio Tracker

**Feature Branch**: `001-portfolio-tracker`
**Created**: 2026-01-06
**Status**: Draft
**Input**: User description: "I want to create an web app with basic functionality of portfolio tracker. Threre is should be an user account (could be without auth for now), in account you can create wallets on different chains and manually add Assets amount or simple income/outcome transactions. There is should be simple web interface and api on backend with basic functionality. Requirements for WEB: - Account view with current assets, USD prive and amount in USD - Total balance of Account - 'Add transaction' form (different types) - Registration form (basic auth) - Login form (basic auth) Beckend: - Ledger funcionality - Transaction service - API with JWT auth."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - View Portfolio Balance (Priority: P1)

A user wants to see their total cryptocurrency holdings across all wallets with current USD valuations at a glance. This provides immediate visibility into their total portfolio value and individual asset breakdown.

**Why this priority**: This is the core value proposition - users need to see what they own and how much it's worth. Without this, the application has no basic utility.

**Independent Test**: Can be fully tested by creating a user account, adding wallets with assets, and verifying the display shows accurate totals and individual asset values in USD.

**Acceptance Scenarios**:

1. **Given** a user has logged into their account, **When** they view their account dashboard, **Then** they see a total balance in USD across all wallets
2. **Given** a user has multiple assets in different wallets, **When** they view their account, **Then** they see each asset listed with its amount and current USD value
3. **Given** a user has no assets, **When** they view their account, **Then** they see a total balance of $0.00 with a message prompting them to add their first wallet or transaction

---

### User Story 2 - Manage Wallets and Assets (Priority: P1)

A user wants to create wallets for different blockchain networks and manually add their asset holdings to each wallet. This allows them to organize their holdings by blockchain and maintain an accurate inventory.

**Why this priority**: Users cannot track anything without being able to input their holdings. This is foundational functionality required before any tracking can occur.

**Independent Test**: Can be fully tested by creating wallets on different chains (e.g., Ethereum, Bitcoin, Solana), adding various assets to each wallet, and verifying they appear correctly in the portfolio view.

**Acceptance Scenarios**:

1. **Given** a user is logged in, **When** they create a new wallet specifying a blockchain network, **Then** the wallet is created and appears in their account
2. **Given** a user has created a wallet, **When** they manually add an asset with a specific amount to that wallet, **Then** the asset appears in the wallet with the correct quantity
3. **Given** a user has assets in a wallet, **When** they update an asset amount, **Then** the portfolio balance reflects the updated amount
4. **Given** a user selects a wallet, **When** they view wallet details, **Then** they see all assets held in that specific wallet

---

### User Story 3 - Record Transactions (Priority: P2)

A user wants to record income and outcome transactions for their assets to track how their portfolio changes over time. This creates an audit trail and helps understand portfolio movements.

**Why this priority**: While viewing current holdings (P1) is essential, tracking how those holdings change through transactions adds significant value for portfolio management and tax purposes.

**Independent Test**: Can be fully tested by adding income transactions (deposits/purchases) and outcome transactions (withdrawals/sales) and verifying the portfolio balance adjusts accordingly and transaction history is maintained.

**Acceptance Scenarios**:

1. **Given** a user is logged in, **When** they submit an "Add Transaction" form with transaction type (income/outcome), asset, amount, and wallet, **Then** the transaction is recorded and the wallet balance updates accordingly
2. **Given** a user has recorded transactions, **When** they view their transaction history, **Then** they see all transactions listed chronologically with type, asset, amount, and date
3. **Given** a user adds an income transaction for 10 ETH, **When** they view their wallet, **Then** the ETH balance increases by 10
4. **Given** a user adds an outcome transaction for 5 BTC, **When** they view their wallet, **Then** the BTC balance decreases by 5

---

### User Story 4 - User Authentication (Priority: P2)

A user wants to register for an account and log in securely so their portfolio data is private and persistent across sessions.

**Why this priority**: While the system could work without auth initially, secure authentication is necessary for production use and data privacy. It's not P1 because the core portfolio tracking features can be built and tested without it.

**Independent Test**: Can be fully tested by registering a new account, logging out, logging back in, and verifying the user's portfolio data persists and is accessible only to that user.

**Acceptance Scenarios**:

1. **Given** a new user visits the application, **When** they complete the registration form with email and password, **Then** an account is created and they are logged in
2. **Given** a registered user, **When** they submit valid credentials on the login form, **Then** they are authenticated and can access their portfolio
3. **Given** a logged-in user, **When** their session expires or they log out, **Then** they must re-authenticate to access their portfolio
4. **Given** a user attempts to log in with incorrect credentials, **When** they submit the login form, **Then** they receive an error message and are not authenticated

---

### Edge Cases

- What happens when a blockchain network becomes unavailable or is not supported?
- How does the system handle negative balances (e.g., more outcome transactions than assets available)? **Answer**: System MUST prevent outcome transactions that would result in negative balances by validating sufficient balance before recording the transaction.
- What happens when USD price data is unavailable for an asset? **Answer**: See FR-016 (use cached prices or display warning) and FR-008 (allow manual price override).
- How does the system handle simultaneous transactions affecting the same wallet?
- What happens when a user tries to register with an email that already exists?
- How does the system handle invalid or expired JWT tokens?
- What happens when a user deletes a wallet that has transaction history?
- What happens when a user manually overrides a price and the asset's market price changes significantly? **Answer**: Manual prices are immutable once recorded in ledger (per constitution Principle IV).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST allow users to register an account with email and password
- **FR-002**: System MUST authenticate users via email/password and issue JWT tokens for session management
- **FR-003**: System MUST allow authenticated users to create wallets associated with specific blockchain networks
- **FR-004**: System MUST allow users to manually specify asset amounts within each wallet
- **FR-005**: System MUST support recording income transactions (deposits/purchases) that increase asset balances
- **FR-006**: System MUST support recording outcome transactions (withdrawals/sales) that decrease asset balances, and MUST validate sufficient balance exists before recording the transaction (preventing negative balances)
- **FR-007**: System MUST display current asset amounts for each asset in a user's portfolio
- **FR-008**: System MUST automatically fetch current USD prices from public APIs (e.g., CoinGecko, CoinMarketCap) for asset valuation with a 5-minute refresh interval. Users MAY manually override prices when creating transactions for assets without available price data. The system MUST display price source (API-fetched vs manually-specified) in transaction history for audit purposes. Price API failures MUST be handled gracefully by using cached prices or displaying a warning when prices cannot be fetched.
- **FR-009**: System MUST calculate and display total portfolio balance in USD across all wallets
- **FR-010**: System MUST persist all user data including accounts, wallets, assets, and transactions
- **FR-011**: System MUST implement ledger functionality to maintain accurate balance calculations
- **FR-012**: System MUST provide a REST API for all portfolio management operations
- **FR-013**: System MUST secure API endpoints using JWT authentication
- **FR-014**: System MUST validate all user inputs on both client and server sides (including email format validation, password strength requirements per data-model.md, and amount value validation)
- **FR-015**: System MUST maintain transaction history for audit purposes
- **FR-016**: (MERGED INTO FR-008) ~~System MUST handle API failures gracefully by using cached prices or displaying a warning when prices cannot be fetched~~
- **FR-017**: (MERGED INTO FR-008) ~~System MUST allow users to manually specify USD price when creating transactions, and MUST display price source (manual vs API) in transaction history for audit purposes~~

### Key Entities

- **User**: Represents an account holder with email, password, and ownership of wallets and transactions
- **Wallet**: Represents a collection of assets on a specific blockchain network, belongs to one user
- **Asset (Cryptocurrency Type)**: Represents a cryptocurrency or token symbol (e.g., BTC, ETH, USDC). In the ledger implementation, asset balances are tracked via Account entities with asset_id fields.
- **Transaction**: Represents a movement of assets, with type (income/outcome), amount, asset, wallet, timestamp, and optional notes
- **Blockchain Network**: Represents a supported blockchain as a string identifier (e.g., "ethereum", "bitcoin", "solana", "polygon"). Networks are stored as VARCHAR fields in the wallets table, enabling easy addition of new networks without schema changes.

## Assumptions

- Users will primarily track well-known cryptocurrencies with available price data from public APIs
- Manual transaction entry is acceptable; blockchain integration for automatic transaction import is out of scope for this version
- Users are responsible for the accuracy of manually entered transaction data
- Password recovery/reset functionality will be added in a future iteration
- Multi-factor authentication is out of scope for the initial version
- The system will support major blockchain networks (Bitcoin, Ethereum, Solana, Polygon, etc.) with the ability to add more networks as needed
- USD is the only fiat currency for portfolio valuation in this version
- Price data refresh rate from APIs will follow the API provider's rate limits and caching best practices

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Users can register, log in, create a wallet, add an asset, and view their portfolio balance in under 3 minutes
- **SC-002**: Users can add a transaction and see their updated balance reflected immediately (within 2 seconds - validated by integration tests with timing assertions)
- **SC-003**: System displays accurate portfolio totals that match the sum of all individual asset values within $0.01 margin of error (validated by ledger balance reconciliation tests)
- **SC-004**: 95% of users successfully complete their first transaction without errors on their first attempt
- **SC-005**: All API endpoints respond within 500ms under normal load conditions
- **SC-006**: Users can access their portfolio data from any device by logging in, with 100% data consistency
