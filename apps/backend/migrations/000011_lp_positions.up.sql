CREATE TABLE lp_positions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id),
    wallet_id       UUID NOT NULL REFERENCES wallets(id),
    chain_id        VARCHAR(50) NOT NULL,
    protocol        VARCHAR(100) NOT NULL,
    nft_token_id    VARCHAR(100),
    contract_address VARCHAR(255),

    token0_symbol   VARCHAR(50) NOT NULL,
    token1_symbol   VARCHAR(50) NOT NULL,
    token0_contract VARCHAR(255),
    token1_contract VARCHAR(255),
    token0_decimals SMALLINT NOT NULL,
    token1_decimals SMALLINT NOT NULL,

    total_deposited_usd    NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_withdrawn_usd    NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_claimed_fees_usd NUMERIC(78,0) NOT NULL DEFAULT 0,

    total_deposited_token0  NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_deposited_token1  NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_withdrawn_token0  NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_withdrawn_token1  NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_claimed_token0    NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_claimed_token1    NUMERIC(78,0) NOT NULL DEFAULT 0,

    status          VARCHAR(20) NOT NULL DEFAULT 'open',
    opened_at       TIMESTAMPTZ NOT NULL,
    closed_at       TIMESTAMPTZ,

    realized_pnl_usd NUMERIC(78,0),
    apr_bps          INTEGER,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_lp_positions_user ON lp_positions(user_id);
CREATE INDEX idx_lp_positions_wallet ON lp_positions(wallet_id);
CREATE UNIQUE INDEX idx_lp_positions_nft ON lp_positions(wallet_id, chain_id, nft_token_id)
    WHERE nft_token_id IS NOT NULL;
