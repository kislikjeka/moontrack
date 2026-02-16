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

