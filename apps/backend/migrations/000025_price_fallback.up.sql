-- 000025_price_fallback.up.sql

-- 1. Relax assets constraints; add on-chain identity uniqueness
ALTER TABLE assets ALTER COLUMN coingecko_id DROP NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_assets_onchain_identity
  ON assets (chain_id, contract_address)
  WHERE chain_id IS NOT NULL AND contract_address IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_assets_coingecko_id
  ON assets (coingecko_id)
  WHERE coingecko_id IS NOT NULL;

-- 2. Extend tax_lots with price status tracking
ALTER TABLE tax_lots
  ADD COLUMN IF NOT EXISTS price_status VARCHAR(16) NOT NULL DEFAULT 'resolved',
  ADD COLUMN IF NOT EXISTS price_resolution_attempts INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS price_next_retry_at TIMESTAMPTZ;

ALTER TABLE tax_lots ALTER COLUMN auto_cost_basis_per_unit DROP NOT NULL;

CREATE INDEX IF NOT EXISTS idx_tax_lots_price_status_retry
  ON tax_lots (price_status, price_next_retry_at)
  WHERE price_status = 'pending';

-- 3. Backfill job queue
CREATE TABLE IF NOT EXISTS price_backfill_jobs (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  asset_id        UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
  target_time     TIMESTAMPTZ NOT NULL,
  status          VARCHAR(16) NOT NULL DEFAULT 'pending',
  attempts        INT NOT NULL DEFAULT 0,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  locked_at       TIMESTAMPTZ,
  last_error      TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  resolved_at     TIMESTAMPTZ,
  CONSTRAINT uq_price_backfill_jobs_asset_time UNIQUE (asset_id, target_time)
);

CREATE INDEX IF NOT EXISTS idx_price_backfill_jobs_ready
  ON price_backfill_jobs (next_attempt_at)
  WHERE status = 'pending';

-- 4. price_history: make source explicit
ALTER TABLE price_history ALTER COLUMN source SET NOT NULL;
ALTER TABLE price_history ALTER COLUMN source DROP DEFAULT;
