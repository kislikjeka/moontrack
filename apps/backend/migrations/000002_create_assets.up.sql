-- Enable TimescaleDB extension
CREATE EXTENSION IF NOT EXISTS timescaledb;

-- Assets table (Asset Registry)
CREATE TABLE assets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    symbol VARCHAR(20) NOT NULL,
    name VARCHAR(255) NOT NULL,
    coingecko_id VARCHAR(100) NOT NULL,
    decimals SMALLINT NOT NULL DEFAULT 8,
    asset_type VARCHAR(20) NOT NULL DEFAULT 'crypto',
    chain_id VARCHAR(20),
    contract_address VARCHAR(100),
    market_cap_rank INTEGER,
    is_active BOOLEAN NOT NULL DEFAULT true,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT assets_coingecko_unique UNIQUE (coingecko_id),
    CONSTRAINT assets_symbol_chain_unique UNIQUE (symbol, chain_id)
);

CREATE INDEX idx_assets_symbol ON assets(symbol);
CREATE INDEX idx_assets_coingecko_id ON assets(coingecko_id);
CREATE INDEX idx_assets_chain ON assets(chain_id) WHERE chain_id IS NOT NULL;
CREATE INDEX idx_assets_active ON assets(is_active) WHERE is_active = true;
