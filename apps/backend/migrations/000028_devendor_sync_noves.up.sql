-- Noves migration #3 (contract): de-vendor the sync schema off Zerion and wipe
-- all sync-derived financial data so the wallet re-syncs from scratch under
-- Noves. User accounts and wallet definitions (address, name) are preserved;
-- only sync-derived rows are removed.
--
-- Migrations 000001..000027 are immutable history. This is a forward-only
-- de-vendor + data-clearing step; a full squash-to-baseline is a later cleanup
-- (see issue #23), never combined with a behavior change.

-- ---------------------------------------------------------------------------
-- 1. Wipe sync-derived financial data (FK-safe order).
--    Scope = ledger transactions produced by sync: source IN ('zerion','sync_genesis').
--    Manual/other-source transactions are untouched.
-- ---------------------------------------------------------------------------
DO $$
DECLARE
    v_tx_ids UUID[];
BEGIN
    SELECT array_agg(id) INTO v_tx_ids
    FROM transactions
    WHERE source IN ('zerion', 'sync_genesis');

    IF v_tx_ids IS NOT NULL THEN
        DELETE FROM lot_override_history
        WHERE lot_id IN (SELECT id FROM tax_lots WHERE transaction_id = ANY(v_tx_ids));

        DELETE FROM lot_disposals WHERE transaction_id = ANY(v_tx_ids);
        DELETE FROM tax_lots      WHERE transaction_id = ANY(v_tx_ids);
        DELETE FROM entries       WHERE transaction_id = ANY(v_tx_ids);

        -- raw_transactions.ledger_tx_id FKs into transactions; clear the whole
        -- raw store below, so first drop the reference to avoid FK violations.
        UPDATE raw_transactions SET ledger_tx_id = NULL
        WHERE ledger_tx_id = ANY(v_tx_ids);

        DELETE FROM transactions WHERE id = ANY(v_tx_ids);
    END IF;
END $$;

-- All raw transactions (real + synthetic genesis) are Zerion-sourced; drop them
-- so the collector re-fetches under Noves. Zero out sync-derived account balances.
TRUNCATE raw_transactions;
UPDATE account_balances SET balance = 0, usd_value = 0, last_updated = now();

-- Sync-discovered token metadata is provider-shaped; drop and let sync repopulate.
TRUNCATE zerion_assets;

-- ---------------------------------------------------------------------------
-- 2. De-vendor DB names.
-- ---------------------------------------------------------------------------
-- raw_transactions.zerion_id -> external_id (+ its unique constraint auto-name).
ALTER TABLE raw_transactions RENAME COLUMN zerion_id TO external_id;
ALTER TABLE raw_transactions
    RENAME CONSTRAINT raw_transactions_wallet_id_zerion_id_key
    TO raw_transactions_wallet_id_external_id_key;

-- zerion_assets -> chain_assets (+ its named constraint, PK and index).
ALTER TABLE zerion_assets RENAME TO chain_assets;
ALTER TABLE chain_assets
    RENAME CONSTRAINT zerion_assets_symbol_chain_unique
    TO chain_assets_symbol_chain_unique;
ALTER INDEX zerion_assets_pkey RENAME TO chain_assets_pkey;
ALTER INDEX idx_zerion_assets_symbol RENAME TO idx_chain_assets_symbol;

-- ---------------------------------------------------------------------------
-- 3. Rewrite wipe_wallet_ledger to the de-vendored source tag ('noves').
--    Same body as 000019, only the source literal changes.
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION wipe_wallet_ledger(p_wallet_id UUID) RETURNS void AS $$
DECLARE
    v_tx_ids UUID[];
    v_account_ids UUID[];
BEGIN
    SELECT array_agg(id) INTO v_tx_ids
    FROM transactions
    WHERE wallet_id = p_wallet_id AND source IN ('noves', 'sync_genesis');

    IF v_tx_ids IS NULL THEN RETURN; END IF;

    SELECT array_agg(id) INTO v_account_ids
    FROM accounts WHERE wallet_id = p_wallet_id;

    DELETE FROM lot_override_history
    WHERE lot_id IN (SELECT id FROM tax_lots WHERE transaction_id = ANY(v_tx_ids));

    DELETE FROM lot_disposals WHERE transaction_id = ANY(v_tx_ids);
    DELETE FROM tax_lots WHERE transaction_id = ANY(v_tx_ids);
    DELETE FROM entries WHERE transaction_id = ANY(v_tx_ids);
    DELETE FROM transactions WHERE id = ANY(v_tx_ids);

    IF v_account_ids IS NOT NULL THEN
        UPDATE account_balances
        SET balance = 0, usd_value = 0, last_updated = now()
        WHERE account_id = ANY(v_account_ids);
    END IF;

    UPDATE raw_transactions
    SET processing_status = 'pending', processing_error = NULL,
        ledger_tx_id = NULL, processed_at = NULL
    WHERE wallet_id = p_wallet_id;
END;
$$ LANGUAGE plpgsql;

-- ---------------------------------------------------------------------------
-- 4. Reset per-wallet sync state so every wallet re-syncs from scratch. Wallet
--    identity (user_id, address, name) is preserved.
-- ---------------------------------------------------------------------------
UPDATE wallets
SET sync_status      = 'pending',
    sync_phase       = 'idle',
    last_sync_at     = NULL,
    sync_error       = NULL,
    collect_cursor_at = NULL;
