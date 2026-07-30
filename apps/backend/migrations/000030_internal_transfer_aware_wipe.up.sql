-- Noves migration #7 (issue #31): make wipe_wallet_ledger internal-transfer-aware.
--
-- Idempotency is global per on-chain event (external_id = chain:txHash under
-- UNIQUE(source, external_id)), so ONE ledger transaction can be shared by two
-- of the user's wallets. An internal transfer is the standard case: it is
-- recorded once, owned by the outgoing (source) side, and stamped with that
-- side's id in transactions.wallet_id.
--
-- The previous body scoped itself to `transactions.wallet_id = p_wallet_id`,
-- which splits the shared transaction's two participants apart:
--
--   * wiping the DESTINATION deleted nothing (it owns no row) yet still zeroed
--     that wallet's balances — leaving the surviving internal_transfer's credit
--     entries contradicting a zeroed balance, unrecoverable because the
--     incoming side is skipped by design and never re-records it.
--   * wiping the SOURCE deleted the transaction along with the entries that
--     credited the destination, but never re-pended the destination's raws —
--     so nothing on either side would re-derive it.
--
-- The scope becomes "any raw_transaction of this wallet references this ledger
-- transaction", unioned with the transactions this wallet owns. Because the
-- non-owning side's raw now carries ledger_tx_id (issue #31, Go side), wiping
-- either participant reaches the shared transaction, and every raw of every
-- wallet that referenced it is re-pended so replay re-derives the same result.

CREATE OR REPLACE FUNCTION wipe_wallet_ledger(p_wallet_id UUID) RETURNS void AS $$
DECLARE
    v_tx_ids UUID[];
    v_account_ids UUID[];
BEGIN
    -- Scope: transactions this wallet owns, PLUS transactions any of this
    -- wallet's raws reference (the shared-event case, e.g. the incoming side of
    -- an internal transfer or a duplicate-skipped raw).
    SELECT array_agg(DISTINCT id) INTO v_tx_ids
    FROM (
        SELECT id
        FROM transactions
        WHERE wallet_id = p_wallet_id AND source IN ('noves', 'sync_genesis')

        UNION

        SELECT t.id
        FROM transactions t
        JOIN raw_transactions r ON r.ledger_tx_id = t.id
        WHERE r.wallet_id = p_wallet_id AND t.source IN ('noves', 'sync_genesis')
    ) AS scoped;

    SELECT array_agg(id) INTO v_account_ids
    FROM accounts WHERE wallet_id = p_wallet_id;

    IF v_tx_ids IS NOT NULL THEN
        DELETE FROM lot_override_history
        WHERE lot_id IN (SELECT id FROM tax_lots WHERE transaction_id = ANY(v_tx_ids));

        DELETE FROM lot_disposals WHERE transaction_id = ANY(v_tx_ids);
        DELETE FROM tax_lots WHERE transaction_id = ANY(v_tx_ids);
        DELETE FROM entries WHERE transaction_id = ANY(v_tx_ids);

        -- Re-pend EVERY wallet's raws that referenced the deleted transactions,
        -- not just this wallet's. A shared transaction has two referencing raws;
        -- re-pending only one side would leave the other marked processed
        -- against a transaction that no longer exists, and replay would never
        -- re-derive it. This also clears the FK before the delete below.
        UPDATE raw_transactions
        SET processing_status = 'pending', processing_error = NULL,
            ledger_tx_id = NULL, processed_at = NULL
        WHERE ledger_tx_id = ANY(v_tx_ids);

        DELETE FROM transactions WHERE id = ANY(v_tx_ids);
    END IF;

    IF v_account_ids IS NOT NULL THEN
        UPDATE account_balances
        SET balance = 0, usd_value = 0, last_updated = now()
        WHERE account_id = ANY(v_account_ids);
    END IF;

    -- Re-pend this wallet's own raws unconditionally. Note this runs even when
    -- the wallet had no ledger transactions at all: the old early RETURN left a
    -- wallet whose raws were all skipped/errored stuck in that state forever,
    -- with no way to replay them.
    UPDATE raw_transactions
    SET processing_status = 'pending', processing_error = NULL,
        ledger_tx_id = NULL, processed_at = NULL
    WHERE wallet_id = p_wallet_id;
END;
$$ LANGUAGE plpgsql;

-- The wipe and the processor both traverse raw_transactions by ledger_tx_id;
-- without an index that is a sequential scan of the whole raw store.
CREATE INDEX IF NOT EXISTS idx_raw_tx_ledger_tx_id
    ON raw_transactions(ledger_tx_id)
    WHERE ledger_tx_id IS NOT NULL;
