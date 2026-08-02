-- Reverse of 000034: the ledger goes back to symbolic asset ids and the two old
-- asset stores are recreated.
--
-- This restores the SHAPE of the expand phase, not its contents. The UUIDs in
-- the ledger cannot be turned back into tickers by this file — the mapping
-- lives in asset_registry, and a rollback that invented symbols from it would
-- reintroduce exactly the (symbol, chain) collisions the forward migration
-- removes. Rolling back is therefore expected to be paired with an empty
-- ledger, the same state 000033 leaves behind.

DROP MATERIALIZED VIEW IF EXISTS position_wac;
DROP VIEW IF EXISTS tax_lots_effective;

-- Recreate the old stores first: the FKs below point at assets(id).
CREATE TABLE assets (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    symbol           VARCHAR(50) NOT NULL,
    name             VARCHAR(255) NOT NULL,
    coingecko_id     VARCHAR(100),
    chain_id         VARCHAR(50),
    contract_address VARCHAR(255),
    decimals         SMALLINT NOT NULL DEFAULT 18,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT assets_symbol_chain_unique UNIQUE (symbol, chain_id)
);
CREATE INDEX idx_assets_symbol ON assets(symbol);
CREATE UNIQUE INDEX idx_assets_coingecko_id ON assets(coingecko_id)
    WHERE coingecko_id IS NOT NULL;
CREATE UNIQUE INDEX idx_assets_onchain_identity ON assets (chain_id, contract_address)
    WHERE chain_id IS NOT NULL AND contract_address IS NOT NULL;

CREATE TABLE chain_assets (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    symbol           VARCHAR(50) NOT NULL,
    name             VARCHAR(255) NOT NULL DEFAULT '',
    chain_id         VARCHAR(50) NOT NULL,
    contract_address VARCHAR(255) NOT NULL DEFAULT '',
    decimals         SMALLINT NOT NULL DEFAULT 18,
    icon_url         TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chain_assets_symbol_chain_unique UNIQUE (symbol, chain_id)
);

-- Repoint the price FKs back at assets.
ALTER TABLE price_history
    DROP CONSTRAINT IF EXISTS price_history_asset_id_fkey;
ALTER TABLE price_history
    ADD CONSTRAINT price_history_asset_id_fkey
        FOREIGN KEY (asset_id) REFERENCES assets(id);

ALTER TABLE price_backfill_jobs
    DROP CONSTRAINT IF EXISTS price_backfill_jobs_asset_id_fkey;
ALTER TABLE price_backfill_jobs
    ADD CONSTRAINT price_backfill_jobs_asset_id_fkey
        FOREIGN KEY (asset_id) REFERENCES assets(id) ON DELETE CASCADE;

-- LP positions: back to the denormalized token pair.
DROP INDEX IF EXISTS idx_lp_positions_token_pair;
ALTER TABLE lp_positions
    ADD COLUMN token0_symbol   VARCHAR(50) NOT NULL DEFAULT '',
    ADD COLUMN token1_symbol   VARCHAR(50) NOT NULL DEFAULT '',
    ADD COLUMN token0_contract VARCHAR(255),
    ADD COLUMN token1_contract VARCHAR(255),
    ADD COLUMN token0_decimals SMALLINT NOT NULL DEFAULT 18,
    ADD COLUMN token1_decimals SMALLINT NOT NULL DEFAULT 18;
ALTER TABLE lp_positions
    DROP COLUMN token0_asset_id,
    DROP COLUMN token1_asset_id;

-- Lending position assets: back to a symbol plus its denormalized metadata.
ALTER TABLE lending_position_assets
    DROP CONSTRAINT IF EXISTS fk_lpa_asset;
DROP INDEX IF EXISTS idx_lpa_position_side_asset;
ALTER TABLE lending_position_assets
    ALTER COLUMN asset TYPE VARCHAR(50) USING asset::text;
ALTER TABLE lending_position_assets
    ADD COLUMN contract VARCHAR(255),
    ADD COLUMN decimals SMALLINT NOT NULL DEFAULT 18;
CREATE UNIQUE INDEX idx_lpa_position_side_asset
    ON lending_position_assets(position_id, side, asset);

-- Tax lots.
ALTER TABLE tax_lots DROP CONSTRAINT IF EXISTS fk_tax_lots_asset;
DROP INDEX IF EXISTS idx_tax_lots_fifo;
ALTER TABLE tax_lots
    ALTER COLUMN asset TYPE VARCHAR(50) USING asset::text;
CREATE INDEX idx_tax_lots_fifo
    ON tax_lots (account_id, asset, acquired_at ASC)
    WHERE quantity_remaining > 0;

-- Ledger core.
ALTER TABLE account_balances DROP CONSTRAINT IF EXISTS fk_account_balances_asset;
ALTER TABLE account_balances
    ALTER COLUMN asset_id TYPE VARCHAR(50) USING asset_id::text;

ALTER TABLE entries DROP CONSTRAINT IF EXISTS fk_entries_asset;
ALTER TABLE entries
    ALTER COLUMN asset_id TYPE VARCHAR(50) USING asset_id::text;

ALTER TABLE accounts DROP CONSTRAINT IF EXISTS fk_accounts_asset;
ALTER TABLE accounts
    ALTER COLUMN asset_id TYPE VARCHAR(50) USING asset_id::text;

-- Recreate the view and materialized view against the restored column types.
CREATE VIEW tax_lots_effective AS
SELECT
    tl.id,
    tl.transaction_id,
    tl.account_id,
    tl.asset,
    tl.quantity_acquired,
    tl.quantity_remaining,
    tl.acquired_at,
    tl.auto_cost_basis_per_unit,
    tl.auto_cost_basis_source,
    tl.override_cost_basis_per_unit,
    COALESCE(tl.override_cost_basis_per_unit, tl.auto_cost_basis_per_unit) AS effective_cost_basis_per_unit,
    tl.linked_source_lot_id,
    tl.created_at
FROM tax_lots tl;

CREATE MATERIALIZED VIEW position_wac AS
WITH tax_lots_resolved AS (
    SELECT tl.id, tl.account_id, tl.asset, tl.quantity_remaining
    FROM tax_lots tl
    WHERE tl.quantity_remaining > 0
      AND tl.price_status = 'resolved'
),
qty_all AS (
    SELECT tl.account_id, tl.asset, SUM(tl.quantity_remaining) AS total_quantity
    FROM tax_lots tl
    WHERE tl.quantity_remaining > 0
    GROUP BY tl.account_id, tl.asset
),
wac_resolved AS (
    SELECT
        tlr.account_id,
        tlr.asset,
        SUM(tlr.quantity_remaining) AS resolved_qty,
        SUM(tlr.quantity_remaining * tle.effective_cost_basis_per_unit) AS resolved_value
    FROM tax_lots_resolved tlr
    JOIN tax_lots_effective tle ON tle.id = tlr.id
    GROUP BY tlr.account_id, tlr.asset
)
SELECT
    qa.account_id,
    qa.asset,
    qa.total_quantity,
    CASE
        WHEN wr.resolved_qty IS NULL OR wr.resolved_qty = 0 THEN NULL
        ELSE TRUNC(wr.resolved_value / wr.resolved_qty, 0)
    END AS weighted_avg_cost
FROM qty_all qa
LEFT JOIN wac_resolved wr
  ON wr.account_id = qa.account_id AND wr.asset = qa.asset;

CREATE UNIQUE INDEX idx_position_wac_pk ON position_wac (account_id, asset);
