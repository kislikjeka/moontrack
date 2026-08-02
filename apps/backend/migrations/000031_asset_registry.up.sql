-- Asset identity #56 (epic #50, decision #35): the asset registry keyed on
-- (chain, contract).
--
-- This is the EXPAND phase. The registry is created ALONGSIDE `assets` and
-- `chain_assets`, which keep working unchanged; the ledger stays on symbolic
-- asset_ids. Cutting the ledger over and retiring the old tables is the
-- contract phase (#59).
--
-- Why a NEW table rather than reshaping an existing one — neither existing
-- table can carry this identity as it stands:
--
--   * chain_assets is UNIQUE (symbol, chain_id) (000021) — precisely the hole
--     being closed. Two distinct contracts sharing the ticker USDC on one chain
--     collapse into a single row, and the second upsert overwrites the first
--     one's decimals, corrupting every base-unit conversion fed from it.
--   * assets carries coingecko-era legacy: assets_symbol_chain_unique (000002)
--     directly contradicts the new identity, coingecko_id is uniquely indexed
--     twice (000002, 000025), and idx_assets_onchain_identity is PARTIAL —
--     `WHERE chain_id IS NOT NULL AND contract_address IS NOT NULL` (000025) —
--     so native coins are excluded from uniqueness altogether and may duplicate
--     freely.
--
-- A new table gets NOT NULL and a TOTAL unique index from the first row, with
-- no constraint migration to get wrong.
--
-- The native coin carries contract = 'native', a literal. It replaces four
-- mutually inconsistent representations of nativeness that exist today: the
-- empty string in the provider adapter, the empty-string column default in
-- chain_assets, NULL in the Asset.IsNativeL1() predicate, and the hardcoded
-- "ETH" fallback that produces account codes like `gas.polygon.ETH`. A
-- pseudo-address (0xeeee…) was rejected because it LOOKS like a valid address:
-- a native leg mistakenly handled as a token would pass every shape check and
-- fail silently. The literal is visible in a log line, a SELECT and a debugger
-- immediately. An is_native flag was rejected because it creates a second
-- source of truth about nativeness that the schema cannot keep consistent.
--
-- Cross-chain splitting (one coin on several chains = several rows, several
-- UUIDs, several tax lots) is an accepted property of the composite key, not a
-- defect. Should coin-level grouping ever be needed it belongs a level up, via
-- the shared coingecko_id — never via the sentinel.

CREATE TABLE asset_registry (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chain        VARCHAR(50)  NOT NULL,
    contract     VARCHAR(255) NOT NULL,
    symbol       VARCHAR(50)  NOT NULL DEFAULT '',
    name         VARCHAR(255) NOT NULL DEFAULT '',
    decimals     SMALLINT     NOT NULL DEFAULT 0,
    coingecko_id VARCHAR(100),
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),

    -- TOTAL, not partial: the native row (chain, 'native') is covered by the
    -- same uniqueness as every token. This is the whole point of the sentinel
    -- being a literal rather than NULL — NULL is never equal to NULL, so a
    -- nullable contract column would leave natives unconstrained exactly as
    -- idx_assets_onchain_identity does today.
    CONSTRAINT asset_registry_chain_contract_unique UNIQUE (chain, contract),

    -- Neither half of the identity may be blank. An empty contract used to BE
    -- the native marker; under the literal it is a bug, and the check makes it
    -- a write failure rather than a silently duplicated identity.
    CONSTRAINT chk_asset_registry_chain_not_blank CHECK (chain <> ''),
    CONSTRAINT chk_asset_registry_contract_not_blank CHECK (contract <> '')
);

-- Symbol is metadata here, never identity — deliberately non-unique, since two
-- contracts sharing a ticker on one chain is the case that motivated the table.
-- The index only serves symbol lookups during the expand phase, while the
-- ledger still addresses assets by symbol.
CREATE INDEX idx_asset_registry_symbol ON asset_registry (UPPER(symbol));

-- Price resolution joins from the registry to the coingecko catalogue.
CREATE INDEX idx_asset_registry_coingecko_id ON asset_registry (coingecko_id)
    WHERE coingecko_id IS NOT NULL;
