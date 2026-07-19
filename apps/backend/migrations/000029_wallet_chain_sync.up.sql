-- Noves migration #4 (issue #27): per-(wallet, chain) sync-state table.
--
-- The ROWS of wallet_chain_sync ARE the wallet chain set: sync and reconciliation
-- iterate exactly this set. Each row carries its own sync bookkeeping so chains
-- can advance independently (full independence + failure isolation lands in #28;
-- this migration establishes the table and seeds every wallet with the Enabled
-- set, eth/base/arbitrum).
--
-- Wallet identity is unchanged (UNIQUE(user_id, address)); the chain set is
-- orthogonal to identity, NOT a re-introduction of the chain_id dropped by 000011.

CREATE TABLE wallet_chain_sync (
    wallet_id         UUID NOT NULL REFERENCES wallets(id) ON DELETE CASCADE,
    chain             VARCHAR(50) NOT NULL,
    sync_status       VARCHAR(20) NOT NULL DEFAULT 'pending',
    sync_error        TEXT,
    sync_phase        VARCHAR(20) NOT NULL DEFAULT 'idle',
    collect_cursor_at TIMESTAMPTZ,
    last_sync_at      TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (wallet_id, chain),
    CONSTRAINT chk_wallet_chain_sync_status
        CHECK (sync_status IN ('pending', 'syncing', 'synced', 'error')),
    CONSTRAINT chk_wallet_chain_sync_phase
        CHECK (sync_phase IN ('idle', 'collecting', 'reconciling', 'processing', 'synced'))
);

-- Rollup queries hit this to derive the wallet-level status from its chain rows.
CREATE INDEX idx_wallet_chain_sync_wallet ON wallet_chain_sync(wallet_id);

-- Seed every existing wallet with the Enabled set. Each chain row starts from the
-- wallet's current sync bookkeeping (after 000028 that is pending/idle/NULL, i.e.
-- a clean from-scratch re-sync), so the wallet-level and per-chain views agree at
-- migration time.
INSERT INTO wallet_chain_sync (wallet_id, chain, sync_status, sync_error, sync_phase, collect_cursor_at, last_sync_at)
SELECT w.id, c.chain, w.sync_status, w.sync_error, w.sync_phase, w.collect_cursor_at, w.last_sync_at
FROM wallets w
CROSS JOIN (VALUES ('ethereum'), ('base'), ('arbitrum')) AS c(chain);
