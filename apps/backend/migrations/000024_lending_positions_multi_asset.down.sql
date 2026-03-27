-- Step 1: Re-add columns to lending_positions
ALTER TABLE lending_positions
    ADD COLUMN supply_asset VARCHAR(50),
    ADD COLUMN supply_amount NUMERIC(78,0) NOT NULL DEFAULT 0,
    ADD COLUMN supply_decimals SMALLINT NOT NULL DEFAULT 18,
    ADD COLUMN supply_contract VARCHAR(255),
    ADD COLUMN borrow_asset VARCHAR(50),
    ADD COLUMN borrow_amount NUMERIC(78,0) NOT NULL DEFAULT 0,
    ADD COLUMN borrow_decimals SMALLINT NOT NULL DEFAULT 18,
    ADD COLUMN borrow_contract VARCHAR(255),
    ADD COLUMN total_supplied NUMERIC(78,0) NOT NULL DEFAULT 0,
    ADD COLUMN total_withdrawn NUMERIC(78,0) NOT NULL DEFAULT 0,
    ADD COLUMN total_borrowed NUMERIC(78,0) NOT NULL DEFAULT 0,
    ADD COLUMN total_repaid NUMERIC(78,0) NOT NULL DEFAULT 0,
    ADD COLUMN total_supplied_usd NUMERIC(78,0) NOT NULL DEFAULT 0,
    ADD COLUMN total_withdrawn_usd NUMERIC(78,0) NOT NULL DEFAULT 0,
    ADD COLUMN total_borrowed_usd NUMERIC(78,0) NOT NULL DEFAULT 0,
    ADD COLUMN total_repaid_usd NUMERIC(78,0) NOT NULL DEFAULT 0;

-- Step 2: Restore data from lending_position_assets
UPDATE lending_positions lp SET
    supply_asset = a.asset,
    supply_amount = a.amount,
    supply_decimals = a.decimals,
    supply_contract = a.contract,
    total_supplied = a.total_in,
    total_withdrawn = a.total_out,
    total_supplied_usd = a.total_in_usd,
    total_withdrawn_usd = a.total_out_usd
FROM lending_position_assets a
WHERE a.position_id = lp.id AND a.side = 'supply';

UPDATE lending_positions lp SET
    borrow_asset = a.asset,
    borrow_amount = a.amount,
    borrow_decimals = a.decimals,
    borrow_contract = a.contract,
    total_borrowed = a.total_in,
    total_repaid = a.total_out,
    total_borrowed_usd = a.total_in_usd,
    total_repaid_usd = a.total_out_usd
FROM lending_position_assets a
WHERE a.position_id = lp.id AND a.side = 'borrow';

-- Step 3: Restore original unique index
DROP INDEX idx_lending_positions_unique_active;
CREATE UNIQUE INDEX idx_lending_positions_unique_active
    ON lending_positions(wallet_id, protocol, chain_id, supply_asset, borrow_asset)
    WHERE status = 'active';

-- Step 4: Drop lending_position_assets table
DROP TABLE lending_position_assets;
