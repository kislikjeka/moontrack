-- Add COLLATERAL and LIABILITY to accounts type constraint
ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_type_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_type_check
    CHECK (type IN ('CRYPTO_WALLET', 'INCOME', 'EXPENSE', 'GAS_FEE', 'CLEARING', 'COLLATERAL', 'LIABILITY'));

-- Create lending_positions table
CREATE TABLE lending_positions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id),
    wallet_id       UUID NOT NULL REFERENCES wallets(id),
    chain_id        VARCHAR(50) NOT NULL,
    protocol        VARCHAR(100) NOT NULL,

    supply_asset         VARCHAR(50),
    supply_amount        NUMERIC(78,0) NOT NULL DEFAULT 0,
    supply_decimals      SMALLINT NOT NULL DEFAULT 18,
    supply_contract      VARCHAR(255),

    borrow_asset         VARCHAR(50),
    borrow_amount        NUMERIC(78,0) NOT NULL DEFAULT 0,
    borrow_decimals      SMALLINT NOT NULL DEFAULT 18,
    borrow_contract      VARCHAR(255),

    total_supplied       NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_withdrawn      NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_borrowed       NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_repaid         NUMERIC(78,0) NOT NULL DEFAULT 0,

    total_supplied_usd   NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_withdrawn_usd  NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_borrowed_usd   NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_repaid_usd     NUMERIC(78,0) NOT NULL DEFAULT 0,

    interest_earned_usd  NUMERIC(78,0) NOT NULL DEFAULT 0,

    status          VARCHAR(20) NOT NULL DEFAULT 'active',
    opened_at       TIMESTAMPTZ NOT NULL,
    closed_at       TIMESTAMPTZ,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_lending_positions_user_id ON lending_positions(user_id);
CREATE INDEX idx_lending_positions_wallet_id ON lending_positions(wallet_id);
CREATE INDEX idx_lending_positions_status ON lending_positions(status);
CREATE UNIQUE INDEX idx_lending_positions_unique_active
    ON lending_positions(wallet_id, protocol, chain_id, supply_asset, borrow_asset)
    WHERE status = 'active';
