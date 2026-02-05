-- Rollback blockchain sync migration

-- 1. Revert transaction types
UPDATE transactions SET type = 'manual_income' WHERE type = 'transfer_in';
UPDATE transactions SET type = 'manual_outcome' WHERE type = 'transfer_out';

-- 2. Revert accounts chain_id to VARCHAR
ALTER TABLE accounts
    ALTER COLUMN chain_id TYPE VARCHAR(50) USING
        CASE chain_id
            WHEN 1 THEN 'ethereum'
            WHEN 137 THEN 'polygon'
            WHEN 42161 THEN 'arbitrum'
            WHEN 10 THEN 'optimism'
            WHEN 43114 THEN 'avalanche'
            WHEN 56 THEN 'binance-smart-chain'
            WHEN 8453 THEN 'base'
            ELSE 'ethereum'
        END;

-- 3. Remove sync indexes and constraints
DROP INDEX IF EXISTS idx_wallets_sync_status;
DROP INDEX IF EXISTS idx_wallets_user_chain_address;
ALTER TABLE wallets DROP CONSTRAINT IF EXISTS chk_sync_status;

-- 4. Remove sync columns from wallets
ALTER TABLE wallets
    DROP COLUMN IF EXISTS sync_error,
    DROP COLUMN IF EXISTS last_sync_at,
    DROP COLUMN IF EXISTS last_sync_block,
    DROP COLUMN IF EXISTS sync_status;

-- 5. Make address nullable again
ALTER TABLE wallets ALTER COLUMN address DROP NOT NULL;

-- 6. Revert chain_id to VARCHAR
ALTER TABLE wallets
    ALTER COLUMN chain_id TYPE VARCHAR(50) USING
        CASE chain_id
            WHEN 1 THEN 'ethereum'
            WHEN 137 THEN 'polygon'
            WHEN 42161 THEN 'arbitrum'
            WHEN 10 THEN 'optimism'
            WHEN 43114 THEN 'avalanche'
            WHEN 56 THEN 'binance-smart-chain'
            WHEN 8453 THEN 'base'
            ELSE 'ethereum'
        END;
