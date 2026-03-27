-- Step 0: Add lending_transfer to lot_disposals disposal_type constraint
ALTER TABLE lot_disposals DROP CONSTRAINT IF EXISTS lot_disposals_disposal_type_check;
ALTER TABLE lot_disposals ADD CONSTRAINT lot_disposals_disposal_type_check
    CHECK (disposal_type IN ('sale', 'internal_transfer', 'gas_fee', 'lending_transfer'));

ALTER TABLE tax_lots DROP CONSTRAINT IF EXISTS tax_lots_auto_cost_basis_source_check;
ALTER TABLE tax_lots ADD CONSTRAINT tax_lots_auto_cost_basis_source_check
    CHECK (auto_cost_basis_source IN ('swap_price', 'fmv_at_transfer', 'linked_transfer', 'genesis_approximation', 'lending_carry_over'));

-- Step 1: Create lending_position_assets table
CREATE TABLE lending_position_assets (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    position_id     UUID NOT NULL REFERENCES lending_positions(id) ON DELETE CASCADE,
    side            VARCHAR(10) NOT NULL CHECK (side IN ('supply', 'borrow')),
    asset           VARCHAR(50) NOT NULL,
    amount          NUMERIC(78,0) NOT NULL DEFAULT 0,
    decimals        SMALLINT NOT NULL DEFAULT 18,
    contract        VARCHAR(255),
    total_in        NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_out       NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_in_usd    NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_out_usd   NUMERIC(78,0) NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_lpa_position_side_asset
    ON lending_position_assets(position_id, side, asset);
CREATE INDEX idx_lpa_position_id ON lending_position_assets(position_id);

-- Step 2: Migrate existing data into lending_position_assets
-- Supply side (always present if supply_asset is not null)
INSERT INTO lending_position_assets (position_id, side, asset, amount, decimals, contract, total_in, total_out, total_in_usd, total_out_usd, created_at, updated_at)
SELECT
    id, 'supply', supply_asset, supply_amount, supply_decimals, supply_contract,
    total_supplied, total_withdrawn, total_supplied_usd, total_withdrawn_usd,
    created_at, updated_at
FROM lending_positions
WHERE supply_asset IS NOT NULL;

-- Borrow side (only if borrow_asset is not null)
INSERT INTO lending_position_assets (position_id, side, asset, amount, decimals, contract, total_in, total_out, total_in_usd, total_out_usd, created_at, updated_at)
SELECT
    id, 'borrow', borrow_asset, borrow_amount, borrow_decimals, borrow_contract,
    total_borrowed, total_repaid, total_borrowed_usd, total_repaid_usd,
    created_at, updated_at
FROM lending_positions
WHERE borrow_asset IS NOT NULL;

-- Step 3: Replace unique index BEFORE dropping columns (old index references supply_asset/borrow_asset)
DROP INDEX idx_lending_positions_unique_active;
CREATE UNIQUE INDEX idx_lending_positions_unique_active
    ON lending_positions(wallet_id, protocol, chain_id)
    WHERE status = 'active';

-- Step 4: Drop old columns from lending_positions
ALTER TABLE lending_positions
    DROP COLUMN supply_asset,
    DROP COLUMN supply_amount,
    DROP COLUMN supply_decimals,
    DROP COLUMN supply_contract,
    DROP COLUMN borrow_asset,
    DROP COLUMN borrow_amount,
    DROP COLUMN borrow_decimals,
    DROP COLUMN borrow_contract,
    DROP COLUMN total_supplied,
    DROP COLUMN total_withdrawn,
    DROP COLUMN total_borrowed,
    DROP COLUMN total_repaid,
    DROP COLUMN total_supplied_usd,
    DROP COLUMN total_withdrawn_usd,
    DROP COLUMN total_borrowed_usd,
    DROP COLUMN total_repaid_usd;
