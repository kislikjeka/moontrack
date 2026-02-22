-- Fix: weighted_avg_cost division produces decimal values that break big.Int parsing in Go.
-- TRUNC to integer to match NUMERIC(78,0) convention used throughout the system.
DROP MATERIALIZED VIEW IF EXISTS position_wac;

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
