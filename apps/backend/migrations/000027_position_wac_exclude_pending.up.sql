-- 000027_position_wac_exclude_pending.up.sql
--
-- Fix: position_wac's weighted-average calculation was deflating for mixed
-- positions (some lots resolved, some pending). Postgres SUM() skips NULL
-- numerator terms but NOT the denominator — so a pending lot (with
-- effective_cost_basis_per_unit = NULL) added its quantity to SUM(qty) while
-- contributing nothing to SUM(qty * cost). Result: WAC = resolved_value /
-- (resolved_qty + pending_qty), which is strictly lower than the true WAC of
-- resolved_value / resolved_qty.
--
-- This migration rewrites the view to filter pending / unpriceable lots out
-- via a CTE before aggregating, so only resolved lots participate in both
-- numerator and denominator. Pending-only positions still appear with
-- total_quantity populated from the unfiltered lots, but WAC = NULL — matching
-- existing Go-side handling for all-pending positions.

DROP MATERIALIZED VIEW IF EXISTS position_wac;

CREATE MATERIALIZED VIEW position_wac AS
WITH tax_lots_resolved AS (
    SELECT tl.id, tl.account_id, tl.asset, tl.quantity_remaining
    FROM tax_lots tl
    WHERE tl.quantity_remaining > 0
      AND tl.price_status = 'resolved'
),
qty_all AS (
    SELECT tl.account_id, tl.asset, SUM(tl.quantity_remaining) AS total_quantity
    FROM tax_lots tl
    WHERE tl.quantity_remaining > 0
    GROUP BY tl.account_id, tl.asset
),
wac_resolved AS (
    SELECT
        tlr.account_id,
        tlr.asset,
        SUM(tlr.quantity_remaining) AS resolved_qty,
        SUM(tlr.quantity_remaining * tle.effective_cost_basis_per_unit) AS resolved_value
    FROM tax_lots_resolved tlr
    JOIN tax_lots_effective tle ON tle.id = tlr.id
    GROUP BY tlr.account_id, tlr.asset
)
SELECT
    qa.account_id,
    qa.asset,
    qa.total_quantity,
    CASE
        WHEN wr.resolved_qty IS NULL OR wr.resolved_qty = 0 THEN NULL
        ELSE TRUNC(wr.resolved_value / wr.resolved_qty, 0)
    END AS weighted_avg_cost
FROM qty_all qa
LEFT JOIN wac_resolved wr
  ON wr.account_id = qa.account_id AND wr.asset = qa.asset;

CREATE UNIQUE INDEX idx_position_wac_pk ON position_wac (account_id, asset);
