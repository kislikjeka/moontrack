-- raw_transactions: stores raw Zerion transactions for two-phase sync
CREATE TABLE raw_transactions (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    wallet_id         UUID NOT NULL REFERENCES wallets(id) ON DELETE CASCADE,
    zerion_id         VARCHAR(255) NOT NULL,
    tx_hash           VARCHAR(255) NOT NULL,
    chain_id          VARCHAR(50) NOT NULL,
    operation_type    VARCHAR(50) NOT NULL,
    mined_at          TIMESTAMPTZ NOT NULL,
    status            VARCHAR(20) NOT NULL DEFAULT 'confirmed',
    raw_json          JSONB NOT NULL,
    processing_status VARCHAR(20) NOT NULL DEFAULT 'pending'
                      CHECK (processing_status IN ('pending', 'processed', 'skipped', 'error')),
    processing_error  TEXT,
    ledger_tx_id      UUID REFERENCES transactions(id),
    is_synthetic      BOOLEAN NOT NULL DEFAULT false,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at      TIMESTAMPTZ,
    UNIQUE(wallet_id, zerion_id)
);

CREATE INDEX idx_raw_tx_wallet_mined ON raw_transactions(wallet_id, mined_at ASC);
CREATE INDEX idx_raw_tx_wallet_pending ON raw_transactions(wallet_id, mined_at ASC)
    WHERE processing_status = 'pending';

-- Wallet: add sync_phase and collect_cursor_at for two-phase sync
ALTER TABLE wallets
    ADD COLUMN sync_phase VARCHAR(20) NOT NULL DEFAULT 'idle'
    CHECK (sync_phase IN ('idle', 'collecting', 'reconciling', 'processing', 'synced')),
    ADD COLUMN collect_cursor_at TIMESTAMPTZ;

-- Wipe function for replay capability (deletes in FK-safe order)
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
