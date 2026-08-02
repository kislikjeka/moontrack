-- Known-asset filter #58 (epic #50, decision #37): the knownness registry keyed
-- on (chain, contract).
--
-- The question this table answers is "may this asset enter the ledger", and it
-- is a SEPARATE question from "which UUID names this asset" (asset_registry,
-- #56). They are kept in separate tables because they have different lifetimes:
-- an identity is minted synchronously the first time a leg is seen and never
-- changes, while a verdict is reached ASYNCHRONOUSLY, may take a week of retries
-- to become terminal, and may later be overridden by hand.
--
-- WHY A LOCAL TABLE AT ALL. Level 2 of the resolve is "is this quotable at the
-- price provider", which is a network question, and the sync hot path may not
-- ask network questions. A synchronous probe has two failure modes and both are
-- unacceptable: the provider goes down and sync stops, or the provider goes down
-- and real tokens silently vanish from the ledger. So the probe runs in a
-- background worker and writes here, and sync only ever SELECTs. The hot path
-- stays offline.
--
-- WHY THE VERDICT IS NOT A BOOLEAN. `status` distinguishes three states that a
-- boolean would collapse:
--
--   * 'pending'  — queued, not yet decided. The leg is NOT in the ledger, but it
--                  is NOT spam either: it is waiting. This is the default.
--   * 'known'    — resolved known; the leg enters the ledger.
--   * 'unknown'  — resolved unknown; the leg does not enter the ledger.
--
-- Crucially, "checked and found unknown" is `status='unknown'`, which is reached
-- ONLY by exhausting the retry ladder (attempts >= price.MaxAttempts, ~7 days).
-- "Could not check" stays `status='pending'` with `attempts` NOT advancing —
-- a rate limit or a network blip must never spend an attempt, because then a
-- provider outage would convict real tokens. The reconciliation report (#61)
-- needs exactly this distinction to tell spam from a migration bug, so it is
-- materialized rather than derived.
--
-- `source` records WHICH level decided, so a verdict can be explained after the
-- fact rather than being an unattributable boolean.
CREATE TABLE asset_knownness (
    chain      VARCHAR(50)  NOT NULL,
    contract   VARCHAR(255) NOT NULL,

    -- 'pending' | 'known' | 'unknown'. See the note above on why three.
    status     VARCHAR(16)  NOT NULL DEFAULT 'pending',

    -- Which level of the resolve produced the current status: 'builtin' (the
    -- generated token list or a native coin), 'quotable' (the price provider),
    -- 'override' (a human), or '' while still pending.
    source     VARCHAR(16)  NOT NULL DEFAULT '',

    -- Level 3. NULL means "no human has spoken"; TRUE/FALSE outrank every
    -- automatic verdict. Kept as a nullable column rather than folded into
    -- `status` so that setting an override does not destroy the automatic
    -- verdict underneath it — flipping the override back must restore what the
    -- machine thought, not re-probe from scratch.
    override   BOOLEAN,

    -- Retry bookkeeping for the level-2 probe. Deliberately the same shape as
    -- price_backfill_jobs (000025): the worker reuses price.BackoffDelay and
    -- price.IsTerminalAttempt rather than growing a second retry policy that
    -- would drift from the first one.
    attempts        INT         NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_at       TIMESTAMPTZ,
    last_error      TEXT,

    -- Metadata carried for the probe and for the manual review surface. Symbol
    -- is NOT identity here any more than it is in asset_registry.
    symbol     VARCHAR(50)  NOT NULL DEFAULT '',

    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT now(),

    -- The identity IS the key — same shape and same total uniqueness as
    -- asset_registry, so the two tables join on exactly the pair that names an
    -- asset. Native rows carry contract = 'native', the #56 sentinel.
    CONSTRAINT asset_knownness_pkey PRIMARY KEY (chain, contract),

    CONSTRAINT chk_asset_knownness_chain_not_blank CHECK (chain <> ''),
    CONSTRAINT chk_asset_knownness_contract_not_blank CHECK (contract <> ''),
    CONSTRAINT chk_asset_knownness_status
        CHECK (status IN ('pending', 'known', 'unknown'))
);

-- The worker's claim query: the due, still-undecided rows, oldest first.
CREATE INDEX idx_asset_knownness_ready
    ON asset_knownness (next_attempt_at)
    WHERE status = 'pending';
