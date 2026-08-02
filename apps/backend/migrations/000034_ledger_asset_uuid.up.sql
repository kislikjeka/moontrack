-- Ledger identity moves to the registry UUID #59 (epic #50, decision #35).
--
-- This is the CONTRACT phase. 000031 created asset_registry alongside the old
-- stores and left the ledger on symbolic asset_ids; here the ledger moves onto
-- the registry, the old stores are dropped, and the symbolic identity stops
-- existing anywhere.
--
-- 000033 emptied every table touched below, so each ALTER ... TYPE runs against
-- zero rows. That is what makes a bare type change correct here: there is no
-- USING clause converting old values because there are no old values, and any
-- row that did survive would fail the cast loudly rather than being silently
-- coerced into a wrong identity.
--
-- WHY THE FK MATTERS BEYOND TIDINESS. Today the link between an asset and the
-- ledger is a string join on symbol, written out in three separate queries with
-- the predicate `ass.chain_id IS NOT DISTINCT FROM acc.chain_id`. That predicate
-- treats NULL as equal to NULL, and one of those queries
-- (ResolvePendingDisposals) is deliberately cross-tenant, so a wrong match
-- writes one user's cost basis into another user's lots. A UUID FK makes the
-- match an identity comparison that cannot be ambiguous, and makes a dangling
-- reference a write failure instead of a silent mismatch.

-- ---------------------------------------------------------------------------
-- 1. Drop the dependent view and materialized view.
--
-- Both read tax_lots.asset, so neither can survive the column's type change.
-- They are recreated verbatim in step 5 apart from the column type they carry
-- through — there is no definitional change here, only a rebuild forced by the
-- dependency. Dropping and recreating position_wac has precedent: 000016,
-- 000018 and 000027 each did exactly this.
-- ---------------------------------------------------------------------------
DROP MATERIALIZED VIEW IF EXISTS position_wac;
DROP VIEW IF EXISTS tax_lots_effective;

-- ---------------------------------------------------------------------------
-- 2. Ledger core: accounts, entries, account_balances.
--
-- account_balances.asset_id is part of the primary key, so its type change
-- rewrites the PK index; with the table empty this is free.
-- ---------------------------------------------------------------------------
ALTER TABLE accounts
    ALTER COLUMN asset_id TYPE UUID USING asset_id::uuid;
ALTER TABLE accounts
    ADD CONSTRAINT fk_accounts_asset
        FOREIGN KEY (asset_id) REFERENCES asset_registry(id);

ALTER TABLE entries
    ALTER COLUMN asset_id TYPE UUID USING asset_id::uuid;
ALTER TABLE entries
    ADD CONSTRAINT fk_entries_asset
        FOREIGN KEY (asset_id) REFERENCES asset_registry(id);

ALTER TABLE account_balances
    ALTER COLUMN asset_id TYPE UUID USING asset_id::uuid;
ALTER TABLE account_balances
    ADD CONSTRAINT fk_account_balances_asset
        FOREIGN KEY (asset_id) REFERENCES asset_registry(id);

-- ---------------------------------------------------------------------------
-- 3. Tax lots.
--
-- tax_lots.asset was a VARCHAR carrying a ticker and NO chain — the chain could
-- only be recovered by joining through accounts, which is why the pending-lot
-- and disposal queries had to join at all. The UUID carries (chain, contract)
-- by construction, so the chain stops being a separate thing to remember.
-- ---------------------------------------------------------------------------
ALTER TABLE tax_lots
    ALTER COLUMN asset TYPE UUID USING asset::uuid;
ALTER TABLE tax_lots
    ADD CONSTRAINT fk_tax_lots_asset
        FOREIGN KEY (asset) REFERENCES asset_registry(id);

-- The FIFO index is rebuilt because its second column changed type.
DROP INDEX IF EXISTS idx_tax_lots_fifo;
CREATE INDEX idx_tax_lots_fifo
    ON tax_lots (account_id, asset, acquired_at ASC)
    WHERE quantity_remaining > 0;

-- ---------------------------------------------------------------------------
-- 4. Positional tables.
--
-- These move WITH the ledger rather than after it (decision in #59: "everything
-- at once"). Leaving symbols in the presentational tables would have been
-- cheaper, but they are joined against the ledger when rendered, so a mixed
-- representation would recreate the string join in a new place — the precise
-- thing this migration removes.
-- ---------------------------------------------------------------------------

-- lending_position_assets: the asset becomes a UUID, and the uniqueness key
-- changes meaning as a result.
--
-- THIS IS THE ONE KEY THE WIPE DOES NOT FIX, because it is not about the values
-- in the rows but about how many rows are REPRESENTABLE. The old key
-- (position_id, side, asset) with asset as a ticker means a position holding
-- two different tokens that both call themselves USDC can only store one of
-- them: the second write collides with the first and overwrites it. That is not
-- a data problem a TRUNCATE clears — it is a shape problem, and it survives any
-- amount of clean data. Under the UUID the same key admits both, because two
-- contracts are two UUIDs however their tickers read.
ALTER TABLE lending_position_assets
    ALTER COLUMN asset TYPE UUID USING asset::uuid;
ALTER TABLE lending_position_assets
    ADD CONSTRAINT fk_lpa_asset
        FOREIGN KEY (asset) REFERENCES asset_registry(id);

DROP INDEX IF EXISTS idx_lpa_position_side_asset;
CREATE UNIQUE INDEX idx_lpa_position_side_asset
    ON lending_position_assets(position_id, side, asset);

-- The denormalized contract/decimals columns are dropped: both are attributes
-- of the asset, and the asset is now a foreign key to the row that holds them.
-- Keeping them would be a second copy of registry metadata that nothing keeps
-- in step — the same duplication that let chain_assets overwrite decimals and
-- corrupt base-unit conversions.
ALTER TABLE lending_position_assets
    DROP COLUMN contract,
    DROP COLUMN decimals;

-- lp_positions: the token pair becomes two registry references.
--
-- token{0,1}_symbol/_contract/_decimals collapse into one UUID each, for the
-- same reason as above. The symbol columns were NOT NULL and the contract
-- columns nullable — a native token in a pair was spelled as a NULL contract,
-- one of the four spellings of nativeness that the 'native' sentinel replaces.
ALTER TABLE lp_positions
    ADD COLUMN token0_asset_id UUID REFERENCES asset_registry(id),
    ADD COLUMN token1_asset_id UUID REFERENCES asset_registry(id);

ALTER TABLE lp_positions
    DROP COLUMN token0_symbol,
    DROP COLUMN token1_symbol,
    DROP COLUMN token0_contract,
    DROP COLUMN token1_contract,
    DROP COLUMN token0_decimals,
    DROP COLUMN token1_decimals;

-- The pair is how an LP position without an NFT id is found again, so it is
-- indexed. Not unique: the same pair can legitimately be held twice on one
-- chain through different protocols or as separate positions.
CREATE INDEX idx_lp_positions_token_pair
    ON lp_positions(wallet_id, chain_id, token0_asset_id, token1_asset_id);

-- ---------------------------------------------------------------------------
-- 5. Recreate the view and the materialized view.
--
-- Bodies are unchanged from 000013 (tax_lots_effective) and 000027
-- (position_wac); they are repeated here only because step 1 had to drop them.
-- ---------------------------------------------------------------------------
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

-- REFRESH MATERIALIZED VIEW CONCURRENTLY requires a unique index.
CREATE UNIQUE INDEX idx_position_wac_pk ON position_wac (account_id, asset);

-- ---------------------------------------------------------------------------
-- 6. Retire the old asset stores.
--
-- Both are dropped rather than left in place. Leaving them would leave two
-- writable spellings of asset identity in the schema, which is what the expand
-- phase tolerated temporarily and what this phase exists to end. price_history
-- and price_backfill_jobs carry FKs into assets(id) and are repointed at the
-- registry first.
-- ---------------------------------------------------------------------------
ALTER TABLE price_history
    DROP CONSTRAINT IF EXISTS price_history_asset_id_fkey;
ALTER TABLE price_history
    ADD CONSTRAINT price_history_asset_id_fkey
        FOREIGN KEY (asset_id) REFERENCES asset_registry(id);

ALTER TABLE price_backfill_jobs
    DROP CONSTRAINT IF EXISTS price_backfill_jobs_asset_id_fkey;
ALTER TABLE price_backfill_jobs
    ADD CONSTRAINT price_backfill_jobs_asset_id_fkey
        FOREIGN KEY (asset_id) REFERENCES asset_registry(id) ON DELETE CASCADE;

DROP TABLE IF EXISTS chain_assets;
DROP TABLE IF EXISTS assets;

-- ---------------------------------------------------------------------------
-- 7. The registry's symbol index was an expand-phase crutch.
--
-- 000031 created idx_asset_registry_symbol ON (UPPER(symbol)) and said so in a
-- comment: it existed "only to serve symbol lookups during the expand phase,
-- while the ledger still addresses assets by symbol". The ledger no longer
-- does. The index is kept, not dropped, because the API contract work (#42)
-- needs exactly this lookup to compute the ambiguous-ticker flag globally
-- across the registry — a symbol lookup that is presentation, not identity.
-- ---------------------------------------------------------------------------
