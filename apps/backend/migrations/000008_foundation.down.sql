-- Phase 1: Foundation rollback

-- 1. Drop stale sync index
DROP INDEX IF EXISTS idx_wallets_sync_started_at;

-- 2. Remove sync_started_at column
ALTER TABLE wallets DROP COLUMN IF EXISTS sync_started_at;

-- 3. Restore original accounts type CHECK constraint (without CLEARING)
ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_type_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_type_check
    CHECK (type IN ('CRYPTO_WALLET', 'INCOME', 'EXPENSE', 'GAS_FEE'));
