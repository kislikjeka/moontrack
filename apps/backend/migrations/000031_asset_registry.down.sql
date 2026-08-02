-- Reverse issue #56: drop the (chain, contract) asset registry.
--
-- Safe to drop wholesale in the expand phase: nothing references asset_registry
-- yet. The ledger still runs on symbolic asset_ids and the old asset tables
-- (assets, chain_assets) were left untouched by the up migration, so removing
-- the registry returns the schema to its prior state with no data loss beyond
-- the registry's own rows, which sync re-derives on the next run.
DROP INDEX IF EXISTS idx_asset_registry_coingecko_id;
DROP INDEX IF EXISTS idx_asset_registry_symbol;
DROP TABLE IF EXISTS asset_registry;
