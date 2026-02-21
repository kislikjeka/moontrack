-- Tax lots: tracks cost basis for every asset acquisition on CRYPTO_WALLET accounts
CREATE TABLE tax_lots (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id              UUID NOT NULL REFERENCES transactions(id),
    account_id                  UUID NOT NULL REFERENCES accounts(id),
    asset                       VARCHAR(20) NOT NULL,
    quantity_acquired           NUMERIC(78,0) NOT NULL CHECK (quantity_acquired > 0),
    quantity_remaining          NUMERIC(78,0) NOT NULL CHECK (quantity_remaining >= 0),
    acquired_at                 TIMESTAMPTZ NOT NULL,
    auto_cost_basis_per_unit    NUMERIC(78,0) NOT NULL DEFAULT 0,
    auto_cost_basis_source      VARCHAR(30) NOT NULL CHECK (auto_cost_basis_source IN (
                                    'swap_price', 'fmv_at_transfer', 'linked_transfer', 'genesis_approximation'
                                )),
    override_cost_basis_per_unit NUMERIC(78,0),
    override_reason              TEXT,
    override_at                  TIMESTAMPTZ,
    linked_source_lot_id         UUID REFERENCES tax_lots(id),
    created_at                   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_remaining_le_acquired CHECK (quantity_remaining <= quantity_acquired)
);

-- FIFO query: open lots for a given account+asset, oldest first, with row locking
CREATE INDEX idx_tax_lots_fifo
    ON tax_lots (account_id, asset, acquired_at ASC)
    WHERE quantity_remaining > 0;

-- Lookups by transaction
CREATE INDEX idx_tax_lots_transaction ON tax_lots (transaction_id);

-- Lot disposals: each row records consumption of one lot during a disposal event
CREATE TABLE lot_disposals (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id    UUID NOT NULL REFERENCES transactions(id),
    lot_id            UUID NOT NULL REFERENCES tax_lots(id),
    quantity_disposed NUMERIC(78,0) NOT NULL CHECK (quantity_disposed > 0),
    proceeds_per_unit NUMERIC(78,0) NOT NULL DEFAULT 0,
    disposal_type     VARCHAR(20) NOT NULL CHECK (disposal_type IN (
                          'sale', 'internal_transfer', 'gas_fee'
                      )),
    disposed_at       TIMESTAMPTZ NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_lot_disposals_transaction ON lot_disposals (transaction_id);
CREATE INDEX idx_lot_disposals_lot        ON lot_disposals (lot_id);

-- Override audit trail
CREATE TABLE lot_override_history (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lot_id             UUID NOT NULL REFERENCES tax_lots(id),
    previous_cost_basis NUMERIC(78,0),
    new_cost_basis     NUMERIC(78,0) NOT NULL,
    reason             TEXT NOT NULL,
    changed_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_lot_override_history_lot ON lot_override_history (lot_id);

-- Convenience view: effective cost basis (override wins over auto)
CREATE VIEW tax_lots_effective AS
SELECT
    tl.id,
    tl.transaction_id,
    tl.account_id,
    tl.asset,
    tl.quantity_acquired,
    tl.quantity_remaining,
    tl.acquired_at,
    tl.auto_cost_basis_per_unit,
    tl.auto_cost_basis_source,
    tl.override_cost_basis_per_unit,
    COALESCE(tl.override_cost_basis_per_unit, tl.auto_cost_basis_per_unit) AS effective_cost_basis_per_unit,
    tl.linked_source_lot_id,
    tl.created_at
FROM tax_lots tl;
