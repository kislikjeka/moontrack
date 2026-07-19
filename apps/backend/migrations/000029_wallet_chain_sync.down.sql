-- Reverse issue #27: drop the per-(wallet, chain) sync-state table. The wallet
-- chain set lived entirely in these rows, so dropping the table removes it; the
-- wallet-level sync columns (unchanged by the up migration) remain the source of
-- truth after a rollback.
DROP INDEX IF EXISTS idx_wallet_chain_sync_wallet;
DROP TABLE IF EXISTS wallet_chain_sync;
