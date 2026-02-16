-- Phase 1: Foundation migration
-- Adds CLEARING account type, sync_started_at column for stale recovery

-- 1. Add CLEARING to accounts type CHECK constraint
ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_type_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_type_check
    CHECK (type IN ('CRYPTO_WALLET', 'INCOME', 'EXPENSE', 'GAS_FEE', 'CLEARING'));

-- 2. Add sync_started_at column for stale sync recovery
ALTER TABLE wallets ADD COLUMN IF NOT EXISTS sync_started_at TIMESTAMPTZ;

-- 3. Index for finding stale syncing wallets
CREATE INDEX IF NOT EXISTS idx_wallets_sync_started_at ON wallets (sync_started_at)
    WHERE sync_status = 'syncing';
