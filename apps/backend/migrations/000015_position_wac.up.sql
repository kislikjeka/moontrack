-- Materialized view: weighted average cost per (account, asset) for open lots
CREATE MATERIALIZED VIEW position_wac AS
SELECT
    tl.account_id,
    tl.asset,
    SUM(tl.quantity_remaining) AS total_quantity,
    CASE
        WHEN SUM(tl.quantity_remaining) = 0 THEN 0
        ELSE SUM(tl.quantity_remaining * tle.effective_cost_basis_per_unit) / SUM(tl.quantity_remaining)
    END AS weighted_avg_cost
FROM tax_lots tl
JOIN tax_lots_effective tle ON tl.id = tle.id
WHERE tl.quantity_remaining > 0
GROUP BY tl.account_id, tl.asset;

-- Unique index required for REFRESH MATERIALIZED VIEW CONCURRENTLY
CREATE UNIQUE INDEX idx_position_wac_pk ON position_wac (account_id, asset);
