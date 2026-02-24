DROP FUNCTION IF EXISTS wipe_wallet_ledger(UUID);

ALTER TABLE wallets
    DROP COLUMN IF EXISTS collect_cursor_at,
    DROP COLUMN IF EXISTS sync_phase;

DROP INDEX IF EXISTS idx_raw_tx_wallet_pending;
DROP INDEX IF EXISTS idx_raw_tx_wallet_mined;
DROP TABLE IF EXISTS raw_transactions;
