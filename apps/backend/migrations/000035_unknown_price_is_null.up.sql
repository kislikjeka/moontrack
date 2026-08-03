-- Make "price unknown" representable, and stop the ledger from asserting a
-- price of zero it never had (#74).
--
-- Background: entries.usd_rate was NOT NULL, so a leg whose price the provider
-- did not supply had to be written as 0. Downstream, ledger.Entry.Validate()
-- rejected a nil rate for the same reason, which made the tax-lot hook's
-- price_status='pending' branch unreachable: every acquisition arrived with a
-- non-nil rate, so every lot was created 'resolved'. On live data that produced
-- 157/157 lots claiming a resolved cost basis of exactly 0, while the backfill
-- worker had already fetched 148 real prices that no lot was eligible to
-- receive — ListPendingLotsByAssetAndTime filters on price_status='pending'
-- and matched nothing.
--
-- NULL is the honest encoding: it distinguishes "the price is not known" from
-- "the asset was worth nothing", which zero cannot.

-- 1. usd_rate / usd_value become nullable. The CHECKs stay: they constrain the
--    value when there is one, and NULL passes a CHECK by SQL's own rules.
ALTER TABLE entries ALTER COLUMN usd_rate DROP NOT NULL;
ALTER TABLE entries ALTER COLUMN usd_value DROP NOT NULL;

-- 2. Existing rows written under the old encoding carry 0 where the intent was
--    "unknown". The two are indistinguishable by value alone, so this converts
--    the whole population: a genuine zero-value leg does not occur in practice
--    (a priced asset has a positive rate), and leaving a false 0 in place is
--    the more expensive error — it is exactly what froze cost basis.
UPDATE entries SET usd_rate = NULL, usd_value = NULL WHERE usd_rate = 0;

-- 2b. Both columns default to 0, which is the same false statement applied to
--     every future row that omits them. A lot with no known price must land as
--     NULL, so the defaults go.
ALTER TABLE tax_lots ALTER COLUMN auto_cost_basis_per_unit DROP DEFAULT;
ALTER TABLE lot_disposals ALTER COLUMN proceeds_per_unit DROP DEFAULT;

-- 3. Tax lots that claim 'resolved' with a zero basis are making the same false
--    statement. Move them back to 'pending' so the price-resolved hook becomes
--    eligible to fill them, and clear the retry clock so they are picked up on
--    the next pass rather than after a backoff they never earned.
--
--    Lots carrying a user override are left alone: the override is the
--    effective basis regardless of the auto column, and it is not ours to move.
UPDATE tax_lots
SET price_status = 'pending',
    auto_cost_basis_per_unit = NULL,
    price_next_retry_at = NULL,
    price_resolution_attempts = 0
WHERE price_status = 'resolved'
  AND COALESCE(auto_cost_basis_per_unit, 0) = 0
  AND override_cost_basis_per_unit IS NULL;

-- 4. Same statement, disposal side. proceeds_per_unit = 0 with
--    proceeds_status='resolved' asserts the disposal realised nothing.
UPDATE lot_disposals
SET proceeds_status = 'pending',
    proceeds_per_unit = NULL
WHERE proceeds_status = 'resolved'
  AND COALESCE(proceeds_per_unit, 0) = 0;

-- 5. Re-enqueue a backfill job for every asset/moment a now-pending lot needs.
--    The prices for many of these are already in price_history — the jobs that
--    fetched them were marked resolved while updating zero lots — so this is
--    what lets the existing 148 points reach the lots that were not eligible
--    for them at the time.
--
--    ON CONFLICT DO NOTHING because the (asset_id, target_time) pair is the
--    job's identity and a live job for it is as good as a new one.
INSERT INTO price_backfill_jobs (asset_id, target_time, status, attempts, next_attempt_at)
SELECT DISTINCT tl.asset, date_trunc('minute', tl.acquired_at), 'pending', 0, NOW()
FROM tax_lots tl
WHERE tl.price_status = 'pending'
ON CONFLICT (asset_id, target_time) DO NOTHING;

INSERT INTO price_backfill_jobs (asset_id, target_time, status, attempts, next_attempt_at)
SELECT DISTINCT tl.asset, date_trunc('minute', ld.disposed_at), 'pending', 0, NOW()
FROM lot_disposals ld
JOIN tax_lots tl ON tl.id = ld.lot_id
WHERE ld.proceeds_status = 'pending'
ON CONFLICT (asset_id, target_time) DO NOTHING;
