-- Revert to the encoding where "unknown price" is indistinguishable from zero.
--
-- This is lossy by nature: NULL carries information (the price is not known)
-- that the NOT NULL column cannot hold, so collapsing it back to 0 destroys it.
-- That is the defect this migration existed to fix, not an accident of the
-- rollback.

UPDATE entries SET usd_rate = 0 WHERE usd_rate IS NULL;
UPDATE entries SET usd_value = 0 WHERE usd_value IS NULL;

ALTER TABLE entries ALTER COLUMN usd_rate SET NOT NULL;
ALTER TABLE entries ALTER COLUMN usd_value SET NOT NULL;

UPDATE tax_lots SET auto_cost_basis_per_unit = 0 WHERE auto_cost_basis_per_unit IS NULL;
UPDATE lot_disposals SET proceeds_per_unit = 0 WHERE proceeds_per_unit IS NULL;

ALTER TABLE tax_lots ALTER COLUMN auto_cost_basis_per_unit SET DEFAULT 0;
ALTER TABLE lot_disposals ALTER COLUMN proceeds_per_unit SET DEFAULT 0;
