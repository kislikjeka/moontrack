DROP INDEX IF EXISTS idx_wallets_user_address;
ALTER TABLE wallets ADD COLUMN IF NOT EXISTS chain_id BIGINT NOT NULL DEFAULT 1;
CREATE UNIQUE INDEX idx_wallets_user_chain_address ON wallets(user_id, chain_id, lower(address));
