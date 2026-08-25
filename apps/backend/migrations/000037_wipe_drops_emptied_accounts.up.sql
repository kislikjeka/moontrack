-- Issue #85: make wipe_wallet_ledger drop the accounts it empties.
--
-- The wipe deleted a wallet's transactions, entries and lots, then zeroed its
-- account balances but kept every accounts row. That leaves husks: an account
-- with no entries and no balance, still asserting that the wallet holds an
-- asset under a particular code.
--
-- Husks defeat exactly the checks that catch account splits. #70's malformed
-- cross-chain accounts survived a wipe-and-replay with zero entries, so the
-- acceptance query for the fix kept returning them even though every entry had
-- been re-derived onto the correct accounts. Cardinality checks read the
-- accounts table; if the wipe does not clean it, they report the past.
--
-- Everything else about the function is unchanged from 000030.

CREATE OR REPLACE FUNCTION wipe_wallet_ledger(p_wallet_id UUID) RETURNS void AS $$
DECLARE
    v_tx_ids UUID[];
    v_account_ids UUID[];
    v_lot_ids UUID[];
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
        -- Lots about to be deleted.
        SELECT array_agg(id) INTO v_lot_ids
        FROM tax_lots WHERE transaction_id = ANY(v_tx_ids);

        DELETE FROM lot_override_history
        WHERE lot_id IN (SELECT id FROM tax_lots WHERE transaction_id = ANY(v_tx_ids));

        DELETE FROM lot_disposals WHERE transaction_id = ANY(v_tx_ids);

        IF v_lot_ids IS NOT NULL THEN
            -- An internal transfer carries cost basis across wallets: the
            -- destination lot points at the source lot via
            -- linked_source_lot_id, and a disposal can consume a lot recorded
            -- under a different transaction. Either reference can outlive the
            -- lots being deleted here — the counterpart lot survives whenever
            -- it belongs to a transaction outside this wipe's scope (a manual
            -- entry, say). Without clearing them first, the DELETE below fails
            -- outright on tax_lots_linked_source_lot_id_fkey / lot_disposals_
            -- lot_id_fkey and the whole wipe aborts.
            --
            -- Clearing rather than preserving is correct: the link is cost-basis
            -- provenance derived during processing, and replay re-derives it
            -- from the raws. A surviving lot keeps its own basis; it just loses
            -- the pointer to a lot that no longer exists.
            UPDATE tax_lots SET linked_source_lot_id = NULL
            WHERE linked_source_lot_id = ANY(v_lot_ids);

            DELETE FROM lot_override_history WHERE lot_id = ANY(v_lot_ids);
            DELETE FROM lot_disposals WHERE lot_id = ANY(v_lot_ids);
        END IF;

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

        -- Drop the accounts the wipe just emptied.
        --
        -- An account is not independent data: it exists because an entry was
        -- booked to it, and the code it carries is a fact about the logic that
        -- booked it. Zeroing the balance while keeping the row therefore keeps
        -- a claim the ledger no longer makes — the account says "this wallet
        -- holds this asset under this code" with nothing left to support it.
        --
        -- That is what made #70 unfalsifiable on a replayed database. The
        -- defect wrote wallet accounts whose chain segment disagreed with the
        -- chain of the asset in the registry; the fix stopped writing them, and
        -- a replay proved it by booking every entry to the correctly-formed
        -- accounts instead. But the two malformed rows survived the wipe with
        -- zero entries and zero balance, and the acceptance query — which asks
        -- the accounts table, not the entries table — still returned them. A
        -- husk is indistinguishable from a live defect to every check that
        -- reads cardinality rather than amounts, which is precisely the class
        -- of check #61 added because amounts cannot see an account split.
        --
        -- Only accounts left with nothing pointing at them are deleted, so an
        -- account still referenced by a transaction outside this wipe's scope
        -- stays. Both referencing tables have to be checked: entries is
        -- RESTRICT and tax_lots is NO ACTION, so a surviving row in either
        -- would abort the whole wipe rather than skip the row.
        DELETE FROM accounts a
        WHERE a.id = ANY(v_account_ids)
          AND NOT EXISTS (SELECT 1 FROM entries e WHERE e.account_id = a.id)
          AND NOT EXISTS (SELECT 1 FROM tax_lots l WHERE l.account_id = a.id);
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
