-- Clean slate #59 (epic #50, decisions #40 and #45): everything keyed on the
-- old symbolic asset identity is removed, so the schema change that follows in
-- 000034 has nothing left to convert.
--
-- WHY A WIPE AND NOT A MIGRATION. There is nothing to preserve. The only data
-- in this system that cannot be recomputed from the provider is a manual cost
-- basis override, and no override has ever been set (lot_override_history is
-- empty, confirmed on the live database before this migration was written).
-- Every other row is either a normalization of a provider response or something
-- derived from one. A value-preserving migration would therefore be a large
-- amount of careful mapping code whose only output is data we can re-fetch —
-- and every mapping rule in it would be a place for the old identity to survive
-- unnoticed, which is the exact failure this epic exists to remove.
--
-- WHY RAW TRANSACTIONS GO TOO. This is the part that is easy to get wrong.
-- `raw_transactions` reads like a vendor payload cache, so keeping it looks
-- free — resync from local data, no provider calls. It is not a vendor payload:
-- it is the output of the OLD adapter's normalization, in the old adapter's
-- shape (native spelled as the empty string, no per-leg action, contract
-- normalized by the pre-#56 rules). Replaying it would need a compatibility
-- layer at the read side, and a compatibility layer is precisely the quiet
-- place where the old identity survives.
--
-- The consequence is not optional. Insertion into raw_transactions is
-- idempotent on (chain, tx_hash), so leaving these rows behind does not cause
-- an error on resync — it causes the resync to SKIP them. Half the database
-- would silently stay in the old shape, with no failure anywhere. Resync must
-- come from the provider, so the local copy must be gone.
--
-- WHY PRICE HISTORY GOES WITH THE ASSET STORES, IN ONE STATEMENT. price_history
-- and price_backfill_jobs carry a non-cascading FK to assets(id), so dropping
-- the asset stores while keeping prices would be an ordering puzzle. Truncating
-- all of them in a single statement removes the ordering question entirely
-- rather than answering it.
--
-- Discarding the price history costs almost nothing, which is what makes this
-- the cheap option rather than the destructive one. The table holds two
-- unrelated populations: ~96.5k rows are five-minute SPOT ticks written by the
-- background updater — the population #39 identified as an active defect, the
-- one that poisons historical lookups — and only ~104 rows are genuine
-- transaction-time cost basis. Re-acquiring those 104 is ~104 provider requests
-- at worst, and re-acquiring them is strictly better than keeping them, because
-- they were captured through the lookup that #52 has since repaired.
--
-- WHAT SURVIVES: users and wallet definitions, deliberately. A wipe that forced
-- re-registration would be a worse operational story for no benefit — neither
-- table carries an asset identity. wallet_chain_sync cursors are reset (not
-- dropped) so the next sync is a full initial sync rather than an incremental
-- one anchored on a cursor whose transactions no longer exist.
--
-- MANUAL TRANSACTIONS DIE HERE, and this is accepted (#42). They are not in the
-- provider, so they cannot be re-fetched — but the asset UUID inside them comes
-- from the OLD `assets` table, which 000034 retires, so even a manual row that
-- survived the wipe would not resolve to anything afterwards.

-- One statement, so FK ordering between these tables is not a question that has
-- to be answered correctly. CASCADE follows FK references to tables not named
-- here; every such table is itself derived data.
TRUNCATE TABLE
    -- Ledger core.
    entries,
    account_balances,
    accounts,
    transactions,
    -- Tax lots and everything hanging off them.
    tax_lots,
    lot_disposals,
    lot_override_history,
    -- Provider-derived source data. See the note above on why this is not kept.
    raw_transactions,
    -- Positional tables. They carry asset identity in symbol form and are
    -- rebuilt by the resync.
    lp_positions,
    lending_positions,
    lending_position_assets,
    -- Price history and its job queue, together with the asset stores they
    -- point at. See the note above on the two populations.
    price_history,
    price_backfill_jobs,
    -- Both old asset stores. 000034 drops them outright; emptying them here
    -- keeps this statement the single place where data disappears.
    assets,
    chain_assets,
    -- The registry is rebuilt from scratch by the resync. It is emptied rather
    -- than kept because rows minted during the expand phase (#56) were written
    -- alongside the old stores and were never the authority for anything.
    asset_registry,
    asset_knownness
RESTART IDENTITY CASCADE;

-- The continuous aggregate over the price_history hypertable is deliberately
-- NOT dropped and recreated. Recreating it would be a schema change made for no
-- reason, and a chance to get its definition wrong. It stays DEFINED; what it
-- must not stay is POPULATED.
--
-- Truncating the underlying hypertable does NOT empty the aggregate. A
-- continuous aggregate keeps its own materialization, so buckets computed before
-- the wipe survive it — they are rows keyed by asset ids from the store this
-- migration just dropped, i.e. exactly the stale identity the clean slate exists
-- to remove, sitting in the one place a TRUNCATE of the source does not reach.
-- Measured on the live database: price_history at 0 rows, price_history_daily
-- still answering with 644.
--
-- TRUNCATE on the aggregate itself is the supported way to discard that
-- materialization (TimescaleDB refuses a TRUNCATE of the internal
-- materialization hypertable and points here). The view stays defined, its
-- refresh policy stays attached, and it repopulates from prices fetched after
-- the wipe. It is a separate statement from the one above because the aggregate
-- is not a plain table and cannot join that TRUNCATE list.
TRUNCATE TABLE price_history_daily;

-- Reset the per-chain sync cursors so the next sync is a full initial sync.
-- The wallet rows themselves survive; only their position in the provider's
-- history is cleared, because the transactions those cursors pointed past have
-- just been deleted.
-- Same reset 000028 performed when the provider changed, for the same reason:
-- wallet identity (user_id, address, name) is preserved, only the position in
-- the provider's history is cleared.
UPDATE wallet_chain_sync
SET sync_status       = 'pending',
    sync_phase        = 'idle',
    sync_error        = NULL,
    collect_cursor_at = NULL,
    last_sync_at      = NULL;

UPDATE wallets
SET sync_status       = 'pending',
    sync_phase        = 'idle',
    sync_error        = NULL,
    collect_cursor_at = NULL,
    last_sync_at      = NULL;
