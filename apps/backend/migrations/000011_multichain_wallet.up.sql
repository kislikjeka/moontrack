-- Wipe financial data (FK order matters)
TRUNCATE account_balances, entries, transactions, accounts, wallets CASCADE;

-- Remove chain_id from wallets
ALTER TABLE wallets DROP COLUMN IF EXISTS chain_id;

-- Drop old index
DROP INDEX IF EXISTS idx_wallets_user_chain_address;

-- New unique constraint: one address per user (no chain)
CREATE UNIQUE INDEX idx_wallets_user_address ON wallets(user_id, lower(address));
