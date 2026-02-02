# Data Model: Crypto Portfolio Tracker

**Feature**: 001-portfolio-tracker
**Date**: 2026-01-06
**Status**: Phase 1 Design

## Overview

This document defines the complete data model for the portfolio tracker, including entities, relationships, validation rules, and database schema. All financial amounts use `NUMERIC(78,0)` per constitution Principle IV to support blockchain-level precision.

## Core Entities

### User

Represents an account holder with authentication credentials and ownership of wallets.

**Attributes**:
- `id`: UUID (primary key)
- `email`: VARCHAR(255), UNIQUE, NOT NULL
- `password_hash`: VARCHAR(255), NOT NULL (bcrypt hash)
- `created_at`: TIMESTAMP, NOT NULL
- `updated_at`: TIMESTAMP, NOT NULL
- `last_login_at`: TIMESTAMP, NULL

**Validation Rules**:
- Email must be valid format (RFC 5322)
- Email must be unique across all users
- Password must be hashed with bcrypt (never store plaintext per Security by Design)
- Password minimum length: 8 characters (enforced before hashing)

**Relationships**:
- One user has many wallets (1:N)

**State Transitions**: N/A (users don't have state changes beyond field updates)

---

### Wallet

Represents a collection of assets on a specific blockchain network, owned by one user.

**Attributes**:
- `id`: UUID (primary key)
- `user_id`: UUID, NOT NULL (foreign key → users.id)
- `name`: VARCHAR(100), NOT NULL (user-friendly label, e.g., "My Main Wallet")
- `chain_id`: VARCHAR(50), NOT NULL (ethereum, bitcoin, solana, polygon, etc.)
- `address`: VARCHAR(255), NULL (optional blockchain address for reference)
- `created_at`: TIMESTAMP, NOT NULL
- `updated_at`: TIMESTAMP, NOT NULL

**Validation Rules**:
- User must exist
- Chain ID must be from supported list (ethereum, bitcoin, solana, polygon, binance-smart-chain, arbitrum, optimism, avalanche)
- Name must be unique per user
- Address format validation optional (different per blockchain)

**Relationships**:
- One wallet belongs to one user (N:1)
- One wallet has many accounts (ledger accounts for different assets) (1:N)
- One wallet appears in many ledger entries via accounts (1:N indirect)

**State Transitions**: N/A

---

### Account (Ledger)

Represents a ledger account for tracking balances. Per constitution, this is part of the double-entry accounting system.

**Attributes**:
- `id`: UUID (primary key)
- `code`: VARCHAR(255), UNIQUE, NOT NULL (human-readable: "wallet.{wallet_id}.{asset_id}")
- `type`: VARCHAR(50), NOT NULL (CRYPTO_WALLET, INCOME, EXPENSE, GAS_FEE)
- `asset_id`: VARCHAR(20), NOT NULL (BTC, ETH, USDC, etc.)
- `wallet_id`: UUID, NULL (foreign key → wallets.id, NULL for non-wallet accounts like INCOME)
- `chain_id`: VARCHAR(50), NULL (inherited from wallet or standalone)
- `created_at`: TIMESTAMP, NOT NULL
- `metadata`: JSONB, NULL (extensible metadata)

**Validation Rules**:
- Code must be unique across all accounts
- Type must be one of: CRYPTO_WALLET, INCOME, EXPENSE, GAS_FEE
- Asset ID must be valid cryptocurrency symbol
- CRYPTO_WALLET accounts must have wallet_id
- INCOME/EXPENSE/GAS_FEE accounts have wallet_id = NULL

**Relationships**:
- One account may belong to one wallet (N:1, optional)
- One account has many ledger entries (1:N)
- One account has one balance snapshot (1:1)

**Code Format Examples**:
- `wallet.550e8400-e29b-41d4-a716-446655440000.BTC` (crypto wallet account)
- `income.staking.ETH` (income account)
- `expense.gas.ethereum` (gas fee account)

---

### Transaction (Ledger)

Represents a financial transaction that generates ledger entries. This is the aggregate root for the double-entry system.

**Attributes**:
- `id`: UUID (primary key)
- `type`: VARCHAR(100), NOT NULL (manual_income, manual_outcome, asset_adjustment)
- `source`: VARCHAR(50), NOT NULL (manual, debank, etherscan)
- `external_id`: VARCHAR(255), NULL (blockchain tx hash or external system ID)
- `status`: VARCHAR(20), NOT NULL (COMPLETED, FAILED)
- `version`: INT, NOT NULL (optimistic locking)
- `occurred_at`: TIMESTAMP, NOT NULL (when transaction actually happened)
- `recorded_at`: TIMESTAMP, NOT NULL (when recorded in our system)
- `raw_data`: JSONB, NOT NULL (original transaction struct for audit)
- `metadata`: JSONB, NULL
- `error_message`: TEXT, NULL (if status = FAILED)

**Validation Rules**:
- Type must be registered transaction handler type
- Source + external_id must be unique (prevent duplicates)
- Status must be COMPLETED or FAILED
- occurred_at <= recorded_at
- COMPLETED transactions must have balanced ledger entries (SUM(debit) = SUM(credit))

**Relationships**:
- One transaction has many entries (1:N)

**State Transitions**:
```
[PENDING] → Validate → COMPLETED (if validation passes)
[PENDING] → Validate → FAILED (if validation fails)
```

Note: In MVP, transactions are created in COMPLETED or FAILED state (no PENDING state).

---

### Entry (Ledger)

Represents a single debit or credit entry in the double-entry ledger. Immutable per constitution Principle IV.

**Attributes**:
- `id`: UUID (primary key)
- `transaction_id`: UUID, NOT NULL (foreign key → transactions.id)
- `account_id`: UUID, NOT NULL (foreign key → accounts.id)
- `debit_credit`: VARCHAR(6), NOT NULL (DEBIT or CREDIT)
- `entry_type`: VARCHAR(50), NOT NULL (asset_increase, asset_decrease, income, expense, gas_fee)
- `amount`: NUMERIC(78,0), NOT NULL (amount in base units: wei, satoshi, lamports)
- `asset_id`: VARCHAR(20), NOT NULL (BTC, ETH, USDC)
- `usd_rate`: NUMERIC(78,0), NOT NULL (USD rate scaled by 10^8)
- `usd_value`: NUMERIC(78,0), NOT NULL (amount * usd_rate / 10^8)
- `occurred_at`: TIMESTAMP, NOT NULL (matches transaction occurred_at)
- `created_at`: TIMESTAMP, NOT NULL
- `metadata`: JSONB, NULL

**Validation Rules**:
- amount >= 0 (CHECK constraint)
- usd_rate >= 0 (CHECK constraint)
- usd_value >= 0 (CHECK constraint)
- debit_credit must be 'DEBIT' or 'CREDIT'
- Per transaction, SUM(debit amounts) = SUM(credit amounts) (enforced by LedgerService)

**Relationships**:
- One entry belongs to one transaction (N:1)
- One entry belongs to one account (N:1)

**Immutability**: Entries are NEVER updated or deleted (constitution Principle IV & V)

---

### AccountBalance

Denormalized table for performance. Stores current balance per account/asset.

**Attributes**:
- `account_id`: UUID, NOT NULL (foreign key → accounts.id, primary key part)
- `asset_id`: VARCHAR(20), NOT NULL (primary key part)
- `balance`: NUMERIC(78,0), NOT NULL (current balance in base units)
- `usd_value`: NUMERIC(78,0), NOT NULL (at current price, updated periodically)
- `last_updated`: TIMESTAMP, NOT NULL

**Validation Rules**:
- balance >= 0 (can't have negative balance in crypto wallet)
- Composite primary key (account_id, asset_id)
- Balance must equal SUM(entries) for that account/asset (verified in tests)

**Relationships**:
- One balance belongs to one account (N:1 via account_id)

**Update Strategy**:
- Recalculated from ledger entries after each transaction commit
- usd_value refreshed by background job using current prices

---

### PriceSnapshot

Stores historical USD prices for assets to reduce API calls.

**Attributes**:
- `id`: UUID (primary key)
- `asset_id`: VARCHAR(20), NOT NULL (BTC, ETH, USDC)
- `usd_price`: NUMERIC(78,0), NOT NULL (price scaled by 10^8, e.g., $45678.90 → 4567890000000)
- `source`: VARCHAR(50), NOT NULL ('coingecko', 'manual')
- `snapshot_date`: DATE, NOT NULL (date of price)
- `created_at`: TIMESTAMP, NOT NULL
- UNIQUE(asset_id, snapshot_date, source)

**Validation Rules**:
- usd_price >= 0
- Unique per asset/date/source combination

**Relationships**: None (reference data)

---

## Transaction Type Modules

Per constitution Principle VI (Handler Registry Pattern), each transaction type is a separate module.

### ManualIncomeTransaction

User manually records incoming assets (deposits, purchases, rewards).

**Fields** (in raw_data JSONB):
- `wallet_id`: UUID
- `asset_id`: VARCHAR(20)
- `amount`: string (big.Int serialized)
- `usd_rate`: string (big.Int serialized, scaled by 10^8) - **optional**, if null fetch from CoinGecko
- `occurred_at`: timestamp
- `notes`: string (optional)
- `price_source`: string (optional: "manual", "coingecko") - for audit trail

**Ledger Entries Generated** (2 entries):
1. DEBIT `wallet.{wallet_id}.{asset_id}` (asset_increase) - increases wallet balance
2. CREDIT `income.{asset_id}` (income) - records income source

**Validation**:
- Wallet must exist and belong to user
- amount > 0
- If usd_rate provided: usd_rate > 0
- If usd_rate null: fetch from CoinGecko API for occurred_at date

---

### ManualOutcomeTransaction

User manually records outgoing assets (withdrawals, sales, payments).

**Fields** (in raw_data JSONB):
- `wallet_id`: UUID
- `asset_id`: VARCHAR(20)
- `amount`: string
- `usd_rate`: string (optional, if null fetch from CoinGecko)
- `occurred_at`: timestamp
- `notes`: string (optional)
- `price_source`: string (optional: "manual", "coingecko")

**Ledger Entries Generated** (2 entries):
1. CREDIT `wallet.{wallet_id}.{asset_id}` (asset_decrease) - decreases wallet balance
2. DEBIT `expense.{asset_id}` (expense) - records expense

**Validation**:
- Wallet must exist and belong to user
- amount > 0
- Current balance >= amount (can't withdraw more than owned)
- If usd_rate provided: usd_rate > 0
- If usd_rate null: fetch from CoinGecko API for occurred_at date

---

### AssetAdjustmentTransaction

User manually adjusts asset balance (for initial setup or corrections).

**Fields** (in raw_data JSONB):
- `wallet_id`: UUID
- `asset_id`: VARCHAR(20)
- `new_balance`: string (target balance)
- `usd_rate`: string (optional, if null fetch from CoinGecko)
- `occurred_at`: timestamp
- `notes`: string (optional, reason for adjustment)
- `price_source`: string (optional: "manual", "coingecko")

**Ledger Entries Generated** (2 entries):
Calculate difference = new_balance - current_balance:
- If difference > 0: DEBIT wallet, CREDIT adjustment_income
- If difference < 0: CREDIT wallet, DEBIT adjustment_expense

**Validation**:
- Wallet must exist and belong to user
- new_balance >= 0
- If usd_rate provided: usd_rate > 0
- If usd_rate null: fetch from CoinGecko API for occurred_at date

---

## Entity Relationship Diagram

```
┌─────────┐
│  User   │
└────┬────┘
     │ 1:N
     ▼
┌─────────┐
│ Wallet  │
└────┬────┘
     │ 1:N
     ▼
┌──────────┐
│ Account  │◄───────────────┐
│ (Ledger) │                │
└────┬─────┘                │
     │ 1:N                  │ N:1
     ▼                      │
┌───────────┐          ┌────┴─────┐
│  Entry    │◄─────────│Transaction│
│ (Ledger)  │  1:N     │ (Ledger)  │
└───────────┘          └───────────┘

┌──────────────┐      ┌──────────────┐
│AccountBalance│      │PriceSnapshot │
│(Denormalized)│      │ (Reference)  │
└──────────────┘      └──────────────┘
```

---

## Database Schema (PostgreSQL)

```sql
-- Users table
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    last_login_at TIMESTAMP
);

CREATE INDEX idx_users_email ON users(email);

-- Wallets table
CREATE TABLE wallets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    chain_id VARCHAR(50) NOT NULL,
    address VARCHAR(255),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, name)
);

CREATE INDEX idx_wallets_user ON wallets(user_id);
CREATE INDEX idx_wallets_chain ON wallets(chain_id);

-- Accounts table (ledger)
CREATE TABLE accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code VARCHAR(255) UNIQUE NOT NULL,
    type VARCHAR(50) NOT NULL CHECK (type IN ('CRYPTO_WALLET', 'INCOME', 'EXPENSE', 'GAS_FEE')),
    asset_id VARCHAR(20) NOT NULL,
    wallet_id UUID REFERENCES wallets(id) ON DELETE CASCADE,
    chain_id VARCHAR(50),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    metadata JSONB
);

CREATE INDEX idx_accounts_code ON accounts(code);
CREATE INDEX idx_accounts_wallet ON accounts(wallet_id);
CREATE INDEX idx_accounts_type ON accounts(type);

-- Transactions table (ledger aggregate root)
CREATE TABLE transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type VARCHAR(100) NOT NULL,
    source VARCHAR(50) NOT NULL,
    external_id VARCHAR(255),
    status VARCHAR(20) NOT NULL CHECK (status IN ('COMPLETED', 'FAILED')),
    version INT NOT NULL DEFAULT 1,
    occurred_at TIMESTAMP NOT NULL,
    recorded_at TIMESTAMP NOT NULL DEFAULT NOW(),
    raw_data JSONB NOT NULL,
    metadata JSONB,
    error_message TEXT,
    UNIQUE(source, external_id)
);

CREATE INDEX idx_transactions_type ON transactions(type);
CREATE INDEX idx_transactions_source ON transactions(source);
CREATE INDEX idx_transactions_external_id ON transactions(external_id);
CREATE INDEX idx_transactions_occurred_at ON transactions(occurred_at);
CREATE INDEX idx_transactions_status ON transactions(status);

-- Entries table (ledger entries - IMMUTABLE)
CREATE TABLE entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id UUID NOT NULL REFERENCES transactions(id) ON DELETE RESTRICT,
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    debit_credit VARCHAR(6) NOT NULL CHECK (debit_credit IN ('DEBIT', 'CREDIT')),
    entry_type VARCHAR(50) NOT NULL,
    amount NUMERIC(78,0) NOT NULL CHECK (amount >= 0),
    asset_id VARCHAR(20) NOT NULL,
    usd_rate NUMERIC(78,0) NOT NULL CHECK (usd_rate >= 0),
    usd_value NUMERIC(78,0) NOT NULL CHECK (usd_value >= 0),
    occurred_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    metadata JSONB
);

CREATE INDEX idx_entries_transaction ON entries(transaction_id);
CREATE INDEX idx_entries_account ON entries(account_id);
CREATE INDEX idx_entries_occurred_at ON entries(occurred_at);
CREATE INDEX idx_entries_debit_credit ON entries(debit_credit);

-- Account balances table (denormalized for performance)
CREATE TABLE account_balances (
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    asset_id VARCHAR(20) NOT NULL,
    balance NUMERIC(78,0) NOT NULL CHECK (balance >= 0),
    usd_value NUMERIC(78,0) NOT NULL CHECK (usd_value >= 0),
    last_updated TIMESTAMP NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, asset_id)
);

CREATE INDEX idx_account_balances_account ON account_balances(account_id);

-- Price snapshots table (historical prices)
CREATE TABLE price_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id VARCHAR(20) NOT NULL,
    usd_price NUMERIC(78,0) NOT NULL CHECK (usd_price >= 0),
    source VARCHAR(50) NOT NULL,
    snapshot_date DATE NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE(asset_id, snapshot_date, source)
);

CREATE INDEX idx_price_snapshots_lookup ON price_snapshots(asset_id, snapshot_date);
CREATE INDEX idx_price_snapshots_asset ON price_snapshots(asset_id);
```

---

## Ledger Invariants (Constitution Principle V)

These invariants MUST be verified in tests:

### 1. Transaction Balance Invariant

For every transaction:
```sql
SELECT SUM(
    CASE
        WHEN debit_credit = 'DEBIT' THEN amount
        ELSE -amount
    END
) as balance
FROM entries
WHERE transaction_id = ?;
-- MUST equal 0
```

### 2. Account Balance Invariant

For every account/asset combination:
```sql
-- Balance from entries
SELECT SUM(
    CASE
        WHEN debit_credit = 'DEBIT' THEN amount
        ELSE -amount
    END
) as ledger_balance
FROM entries
WHERE account_id = ? AND asset_id = ?;

-- Must equal
SELECT balance FROM account_balances
WHERE account_id = ? AND asset_id = ?;
```

### 3. USD Value Consistency

For every entry:
```sql
-- usd_value should equal (amount * usd_rate) / 10^8
SELECT amount, usd_rate, usd_value,
       (amount * usd_rate / 100000000) as calculated_value
FROM entries
WHERE id = ?;
-- calculated_value MUST equal usd_value
```

---

## Data Integrity Rules

1. **No orphaned records**: All foreign keys have ON DELETE CASCADE or RESTRICT
2. **No negative balances**: CHECK constraints on all amount/balance fields
3. **No future dates**: occurred_at <= NOW() (enforced in application)
4. **Immutable ledger**: Entries and transactions NEVER updated after creation
5. **Unique external IDs**: (source, external_id) unique to prevent duplicate imports
6. **Balanced transactions**: Sum of debits = sum of credits (enforced by LedgerService before commit)

---

## Testing Requirements

Per constitution, every financial operation must have tests verifying:

1. ✅ Transaction balance: `SUM(debit) = SUM(credit)`
2. ✅ Account balance reconciliation: account_balances.balance = SUM(entries)
3. ✅ Precision: No rounding errors with big.Int arithmetic
4. ✅ Edge cases: Zero amounts, max uint256, precision boundaries
5. ✅ Immutability: Entries cannot be updated or deleted

---

## Migration Strategy

Initial migration creates all tables with indexes. Subsequent migrations for:
- Adding new transaction types (no schema changes needed, just handlers)
- Adding new account types (update CHECK constraint)
- Performance optimization (additional indexes based on query patterns)

First migration file: `001_create_schema.up.sql` (includes all tables above)
