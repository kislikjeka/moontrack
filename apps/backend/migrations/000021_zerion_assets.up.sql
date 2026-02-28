CREATE TABLE zerion_assets (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    symbol           VARCHAR(50) NOT NULL,
    name             VARCHAR(255) NOT NULL DEFAULT '',
    chain_id         VARCHAR(50) NOT NULL,
    contract_address VARCHAR(255) NOT NULL DEFAULT '',
    decimals         SMALLINT NOT NULL,
    icon_url         TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT zerion_assets_symbol_chain_unique UNIQUE (symbol, chain_id)
);

CREATE INDEX idx_zerion_assets_symbol ON zerion_assets(UPPER(symbol));
