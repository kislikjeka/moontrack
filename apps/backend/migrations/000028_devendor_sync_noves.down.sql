-- Reverse the 000028 de-vendor renames and restore the 'zerion' wipe literal.
--
-- NOTE: the data truncation in the up migration is NOT reversible — the wiped
-- sync-derived transactions/entries/tax_lots/lot_disposals/raw_transactions and
-- zeroed balances cannot be reconstructed here. Rolling back restores the schema
-- names only; re-running the up migration (or a fresh Noves sync) repopulates data.

-- Restore chain_assets -> zerion_assets (+ named constraint, PK and index).
ALTER INDEX idx_chain_assets_symbol RENAME TO idx_zerion_assets_symbol;
ALTER INDEX chain_assets_pkey RENAME TO zerion_assets_pkey;
ALTER TABLE chain_assets
    RENAME CONSTRAINT chain_assets_symbol_chain_unique
    TO zerion_assets_symbol_chain_unique;
ALTER TABLE chain_assets RENAME TO zerion_assets;

-- Restore raw_transactions.external_id -> zerion_id (+ its unique constraint).
ALTER TABLE raw_transactions
    RENAME CONSTRAINT raw_transactions_wallet_id_external_id_key
    TO raw_transactions_wallet_id_zerion_id_key;
ALTER TABLE raw_transactions RENAME COLUMN external_id TO zerion_id;

-- Restore the 'zerion' source literal in wipe_wallet_ledger (matches 000019).
CREATE OR REPLACE FUNCTION wipe_wallet_ledger(p_wallet_id UUID) RETURNS void AS $$
DECLARE
    v_tx_ids UUID[];
    v_account_ids UUID[];
BEGIN
    SELECT array_agg(id) INTO v_tx_ids
    FROM transactions
    WHERE wallet_id = p_wallet_id AND source IN ('zerion', 'sync_genesis');

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
