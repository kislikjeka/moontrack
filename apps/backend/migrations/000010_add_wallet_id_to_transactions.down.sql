DROP INDEX IF EXISTS idx_transactions_wallet_id;
ALTER TABLE transactions DROP COLUMN IF EXISTS wallet_id;
