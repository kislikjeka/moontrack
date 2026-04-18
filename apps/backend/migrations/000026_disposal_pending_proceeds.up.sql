-- 000026_disposal_pending_proceeds.up.sql
--
-- Allow a disposal's proceeds_per_unit to be unresolved at write time so the
-- price-backfill worker can fill it in later (mirrors the pending price flow
-- on tax_lots). Without this, disposals against a token without a live price
-- were frozen at proceeds_per_unit=0 forever.

ALTER TABLE lot_disposals ALTER COLUMN proceeds_per_unit DROP NOT NULL;

ALTER TABLE lot_disposals
  ADD COLUMN IF NOT EXISTS proceeds_status VARCHAR(16) NOT NULL DEFAULT 'resolved';

-- Index to help the resolution worker find pending disposals quickly.
CREATE INDEX IF NOT EXISTS idx_lot_disposals_proceeds_pending
  ON lot_disposals (disposed_at)
  WHERE proceeds_status = 'pending';
