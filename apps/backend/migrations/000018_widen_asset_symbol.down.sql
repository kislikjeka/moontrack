-- Drop dependent views before altering tax_lots.asset
DROP MATERIALIZED VIEW IF EXISTS position_wac;
DROP VIEW IF EXISTS tax_lots_effective;

-- Revert to VARCHAR(20)
ALTER TABLE tax_lots ALTER COLUMN asset TYPE VARCHAR(20);
ALTER TABLE assets ALTER COLUMN symbol TYPE VARCHAR(20);

-- Recreate tax_lots_effective view
CREATE VIEW tax_lots_effective AS
SELECT
    tl.id,
    tl.transaction_id,
    tl.account_id,
    tl.asset,
    tl.quantity_acquired,
    tl.quantity_remaining,
    tl.acquired_at,
    tl.auto_cost_basis_per_unit,
    tl.auto_cost_basis_source,
    tl.override_cost_basis_per_unit,
    COALESCE(tl.override_cost_basis_per_unit, tl.auto_cost_basis_per_unit) AS effective_cost_basis_per_unit,
    tl.linked_source_lot_id,
    tl.created_at
FROM tax_lots tl;

-- Recreate position_wac materialized view
CREATE MATERIALIZED VIEW position_wac AS
SELECT
    tl.account_id,
    tl.asset,
    SUM(tl.quantity_remaining) AS total_quantity,
    CASE
        WHEN SUM(tl.quantity_remaining) = 0 THEN 0
        ELSE TRUNC(SUM(tl.quantity_remaining * tle.effective_cost_basis_per_unit) / SUM(tl.quantity_remaining), 0)
    END AS weighted_avg_cost
FROM tax_lots tl
JOIN tax_lots_effective tle ON tl.id = tle.id
WHERE tl.quantity_remaining > 0
GROUP BY tl.account_id, tl.asset;

CREATE UNIQUE INDEX idx_position_wac_pk ON position_wac (account_id, asset);
