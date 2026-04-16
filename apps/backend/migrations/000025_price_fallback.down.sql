-- 000025_price_fallback.down.sql

ALTER TABLE price_history ALTER COLUMN source SET DEFAULT 'coingecko';

DROP TABLE IF EXISTS price_backfill_jobs;

DROP INDEX IF EXISTS idx_tax_lots_price_status_retry;

ALTER TABLE tax_lots
  DROP COLUMN IF EXISTS price_next_retry_at,
  DROP COLUMN IF EXISTS price_resolution_attempts,
  DROP COLUMN IF EXISTS price_status;

ALTER TABLE tax_lots ALTER COLUMN auto_cost_basis_per_unit SET NOT NULL;

DROP INDEX IF EXISTS idx_assets_coingecko_id;
DROP INDEX IF EXISTS idx_assets_onchain_identity;

ALTER TABLE assets ALTER COLUMN coingecko_id SET NOT NULL;
