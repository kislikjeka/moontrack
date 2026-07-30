-- Revert wipe_wallet_ledger to the 000028 body (wallet-owned scope only) and
-- drop the ledger_tx_id lookup index.

DROP INDEX IF EXISTS idx_raw_tx_ledger_tx_id;

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
