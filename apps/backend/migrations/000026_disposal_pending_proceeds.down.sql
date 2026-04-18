-- 000026_disposal_pending_proceeds.down.sql

DROP INDEX IF EXISTS idx_lot_disposals_proceeds_pending;

-- Before restoring NOT NULL, backfill any lingering NULLs with 0.
UPDATE lot_disposals SET proceeds_per_unit = 0 WHERE proceeds_per_unit IS NULL;

ALTER TABLE lot_disposals DROP COLUMN IF EXISTS proceeds_status;
ALTER TABLE lot_disposals ALTER COLUMN proceeds_per_unit SET NOT NULL;
