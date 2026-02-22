-- Revert accounts.chain_id from VARCHAR(50) back to BIGINT
ALTER TABLE accounts
    ALTER COLUMN chain_id TYPE BIGINT USING
        CASE chain_id
            WHEN 'ethereum'            THEN 1
            WHEN 'polygon'             THEN 137
            WHEN 'arbitrum'            THEN 42161
            WHEN 'optimism'            THEN 10
            WHEN 'base'                THEN 8453
            WHEN 'avalanche'           THEN 43114
            WHEN 'binance-smart-chain' THEN 56
            ELSE NULL
        END;
