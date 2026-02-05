-- Blockchain Sync Migration
-- Updates wallets table for EVM blockchain sync support
-- Changes transaction types from manual_* to transfer_*

-- 1. Update wallets table for blockchain sync
-- Change chain_id from VARCHAR to BIGINT (EVM chain IDs: 1=ETH, 137=Polygon, etc.)
ALTER TABLE wallets
    ALTER COLUMN chain_id TYPE BIGINT USING
        CASE chain_id
            WHEN 'ethereum' THEN 1
            WHEN 'polygon' THEN 137
            WHEN 'arbitrum' THEN 42161
            WHEN 'optimism' THEN 10
            WHEN 'avalanche' THEN 43114
            WHEN 'binance-smart-chain' THEN 56
            WHEN 'base' THEN 8453
            ELSE 1  -- Default to Ethereum mainnet
        END;

-- Make address required (all wallets must have valid EVM address)
-- First, set a placeholder for any NULL addresses (should be cleaned up manually)
UPDATE wallets SET address = '0x0000000000000000000000000000000000000000' WHERE address IS NULL;
ALTER TABLE wallets ALTER COLUMN address SET NOT NULL;

-- Add sync-related columns
ALTER TABLE wallets
    ADD COLUMN sync_status VARCHAR(20) NOT NULL DEFAULT 'pending',
    ADD COLUMN last_sync_block BIGINT,
    ADD COLUMN last_sync_at TIMESTAMP,
    ADD COLUMN sync_error TEXT;

-- Add check constraint for sync_status
ALTER TABLE wallets
    ADD CONSTRAINT chk_sync_status
    CHECK (sync_status IN ('pending', 'syncing', 'synced', 'error'));

-- Create unique index: one address per chain per user
CREATE UNIQUE INDEX idx_wallets_user_chain_address
    ON wallets(user_id, chain_id, lower(address));

-- Index for finding wallets to sync
CREATE INDEX idx_wallets_sync_status ON wallets(sync_status);

-- 2. Update accounts table: change chain_id to BIGINT
ALTER TABLE accounts
    ALTER COLUMN chain_id TYPE BIGINT USING
        CASE chain_id
            WHEN 'ethereum' THEN 1
            WHEN 'polygon' THEN 137
            WHEN 'arbitrum' THEN 42161
            WHEN 'optimism' THEN 10
            WHEN 'avalanche' THEN 43114
            WHEN 'binance-smart-chain' THEN 56
            WHEN 'base' THEN 8453
            ELSE NULL
        END;

-- 3. Update transaction types: manual_* -> transfer_*
UPDATE transactions SET type = 'transfer_in' WHERE type = 'manual_income';
UPDATE transactions SET type = 'transfer_out' WHERE type = 'manual_outcome';
