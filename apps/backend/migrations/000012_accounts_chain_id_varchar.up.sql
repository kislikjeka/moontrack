-- Convert accounts.chain_id from BIGINT back to VARCHAR(50)
-- Use Zerion chain names as the canonical format (consistent with assets.chain_id)
ALTER TABLE accounts
    ALTER COLUMN chain_id TYPE VARCHAR(50) USING
        CASE chain_id
            WHEN 1     THEN 'ethereum'
            WHEN 137   THEN 'polygon'
            WHEN 42161 THEN 'arbitrum'
            WHEN 10    THEN 'optimism'
            WHEN 8453  THEN 'base'
            WHEN 43114 THEN 'avalanche'
            WHEN 56    THEN 'binance-smart-chain'
            ELSE chain_id::text
        END;
