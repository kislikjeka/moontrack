# Missing-Price Providers: Fallback Pricing for Long-Tail Tokens

**Date:** 2026-04-16
**Status:** Fully deployed — as of 2026-04-16 this design is the sole production path. The `FEATURE_PRICE_FALLBACK` flag and the staged-rollout described in the "Rollout" section below are historical; the worker runs unconditionally and the flag has been removed from the codebase.
**Author:** brainstorm session, kislikjeka

## Problem

When a user syncs a wallet containing tokens that Zerion doesn't price (small-cap or DEX-only tokens), today's code path silently records `usd_price = "0"` into the ledger. Downstream:

- Tax lots are created with zero cost basis
- Disposal PnL shows 100% gain against a $0 cost basis
- The token is often not even promoted from `zerion_assets` to `assets`, making it permanently unpriceable
- CoinGecko's `/search` can't help either, because the price updater only queries by `coingecko_id` and long-tail tokens aren't listed

We need a fallback pricing pipeline so that **cost basis and PnL are correct** for every transaction, including ones Zerion and CoinGecko can't price.

## Goal

Every transaction ends up with a correct USD cost basis for tax-lot PnL, sourced from the best available provider.

## Invariants

1. **One `assets` row per real token.** Identity for on-chain tokens is `(chain_id, contract_address)`. For native coins (BTC, native SOL) it is `coingecko_id`. `coingecko_id` becomes an *optional* secondary identifier.
2. **One `asset_id`, many price sources.** The existing `price_history.source` column distinguishes `coingecko` / `geckoterminal` / `defillama` / `zerion` / `manual`. Reads pick by a global priority order (CoinGecko > GeckoTerminal > DefiLlama), configurable via env.
3. **No silent zeros.** A lot's `cost_basis_usd` is either (a) resolved with a real price, (b) explicitly `pending_price`, or (c) explicitly `unpriceable` with user override expected. Price `0` is no longer a legal default.
4. **PnL honesty.** Portfolio/PnL responses expose `pnl_is_partial: bool` when any included lot is in `pending_price` state. Lots in `unpriceable` state without override are excluded from PnL with counts surfaced to the UI.
5. **Transaction-timestamp precision.** Historical lookups resolve to the *transaction timestamp*, not daily close, where the provider supports it (GeckoTerminal minute OHLCV, DefiLlama timestamp-based). CoinGecko free-tier daily is a last resort for history and is recorded in the `source` column.
6. **Eventual correctness via idempotent retries.** Historical prices never change; the background sweep is safe to re-run, uses a dedup cache, and is rate-limit-aware.

## Non-goals

- No real-time current-price fallback during request-time portfolio reads — current prices continue to come from CoinGecko's 5-minute updater tick for majors. The "pending" state communicates transient incompleteness.
- No automated price-provider selection per asset — global priority order is sufficient for MVP.
- No market-cap / 24h volume for long-tail tokens — these columns remain empty for non-CoinGecko assets (GeckoTerminal returns pool-level volume which is not equivalent to aggregate volume).
- No on-chain RPC fallback (reading Uniswap pool state directly) — deferred; GeckoTerminal and DefiLlama together are expected to cover the long tail.

## Architecture

Working within the existing layered structure: `transport → module → platform → ledger ← infra`.

### New — `infra/gateway/`

- **`geckoterminal/client.go`** — HTTP client. Endpoints:
  - Current batch: `GET /networks/{network}/tokens/multi/{addresses}` (up to 30 addresses)
  - Historical: `GET /networks/{network}/pools/{pool_address}/ohlcv/minute`
  - Rate limit: 30 rpm, token-bucket with burst 5
  - 429 handling: read `Retry-After`, pause bucket until elapsed
- **`defillama/client.go`** — HTTP client. Endpoints:
  - Current: `GET /prices/current/{chain:addr,...}`
  - Historical: `GET /prices/historical/{unix_timestamp}/{chain:addr,...}`
  - Rejects entries with `confidence < DEFILLAMA_MIN_CONFIDENCE` (default 0.9) as `ErrLowConfidence`
  - Rate limit: 10 rps, burst 20

### New — `platform/price/`

New package consolidating price logic that is currently split across `asset/updater.go` and `module/portfolio/price_adapter.go`.

- **`resolver.go`** — `PriceResolver` orchestrating an ordered chain of `Provider`s. Given `(asset, timestamp?)` returns `Price | ErrPending | ErrUnpriceable`. Falls through providers on `ErrNotFound` / `ErrLowConfidence` / `ErrUnsupportedChain`; retries transients / rate-limits at the worker level.
- **`provider_coingecko.go`**, **`provider_geckoterminal.go`**, **`provider_defillama.go`** — thin adapters implementing `Provider`, wrapping the gateway clients and normalizing outputs to `HistoricalPrice { PriceUSD, Timestamp, Confidence }`.
- **`backfill_worker.go`** — goroutine consuming `price_backfill_jobs` table at a configurable rate (default 1 rps global). Claims jobs with `SELECT ... FOR UPDATE SKIP LOCKED`. On success: writes `price_history` row, fires `PriceResolvedHook`. On miss: reschedules with backoff; after 7 days → lot → `unpriceable`.
- **`cache.go`** — Redis historical-price dedup cache.
  - Key: `price:{source}:{asset_id}:{minute_bucket}` for intraday, `price:{source}:{asset_id}:{day_bucket}` for daily.
  - TTL: 30 days (historical prices are immutable).
  - Write-through on every successful provider call.

### Changed — `platform/asset/`

- `Asset.CoinGeckoID` → nullable.
- `(Asset.ChainID, Asset.ContractAddress)` become the primary on-chain identity key with a partial unique index (see schema).
- New method `UpsertByOnChainIdentity(ctx, chain, addr) (Asset, created bool)` — used by the sync path to dedupe.
- `asset/updater.go` delegates current-price writes to `PriceResolver` (still runs on 5-min tick, still CoinGecko-first for majors).

### Changed — `platform/sync/zerion_processor.go`

When Zerion returns `Price == nil` (the current silent-zero case):

1. `asset.UpsertByOnChainIdentity(chain, contract_addr)` — asset lands in `assets` table.
2. Ledger entry is created with cost basis in `pending_price` state (not `"0"`).
3. Enqueue a `price_backfill_jobs` row keyed by `(asset_id, tx_timestamp_minute_bucket)`.

When Zerion returns a price: unchanged from today. The price is still written as a `zerion`-sourced `price_history` row for provenance.

### Changed — `ledger/` (tax-lot hook)

- `TaxLotHook` recognizes the `pending_price` state: creates the lot with `quantity`, leaves `cost_basis_usd = NULL`, `price_status = 'pending'`. Does **not** create `LotDisposal` cost-basis entries yet.
- New **`PriceResolvedHook`** fires when `backfill_worker` writes a price. It:
  1. Looks up all pending lots for `(asset_id, target_time)`.
  2. Sets `cost_basis_usd = qty * price`, `price_status = 'resolved'`.
  3. Retriggers downstream `LotDisposal` cost-basis recomputation in FIFO order — reusing the **same machinery as the existing `cost_basis_override` path**, which already solves the "retroactively updated cost basis" problem.

### Changed — `module/portfolio/`

- `price_adapter.go` reads via `PriceReader` (backed by `price_repo` + priority CASE expression) instead of a direct repo lookup. Surfaces `ErrPending` / `ErrUnpriceable` distinctly instead of silently returning `big.NewInt(0)`.
- Portfolio response gains: `pnl_is_partial`, `pending_lot_count`, `unpriceable_lot_count`, `pending_asset_count`.

### Changed — `transport/`

- New endpoint `PUT /lots/{id}/manual-price` — user-submitted cost basis for `unpriceable` lots. Writes to the existing `cost_basis_override` column (same field the current "user override > linked source lot > auto-calculated" priority already consults), sets `price_status='resolved'`, and writes a `price_history` row with `source='manual'` for audit/provenance. Only the HTTP surface and the status transition are new; effective cost-basis computation is unchanged.

### End-to-end data flow

```
Zerion sync → processor (Price=nil)
  → asset.UpsertByOnChainIdentity → assets row exists
  → ledger.RecordTransaction with pending_price flag
  → TaxLotHook creates lot (quantity, cost_basis=NULL, status=pending)
  → enqueue price_backfill_jobs row (asset_id, tx_timestamp_minute_bucket)
  → sync returns fast

[background, rate-limited]
backfill_worker claims job → PriceResolver(asset, timestamp)
  → tries GeckoTerminal /ohlcv/minute → hit
  → write price_history(source=geckoterminal, time=actual_point_in_time)
  → PriceResolvedHook fires → lot.cost_basis = qty * price, status=resolved
  → recompute downstream LotDisposals (FIFO, reused override machinery)

[if all providers miss]
  → job rescheduled per backoff table
  → after 7 days and 11 attempts → lot.status = unpriceable
  → UI surfaces manual-price CTA → PUT /lots/{id}/manual-price
  → price_history(source=manual) row, lot.status = resolved
```

## Data Model

### Migrations (additive)

```sql
-- 1. Relax assets constraints, enforce on-chain identity uniqueness
ALTER TABLE assets ALTER COLUMN coingecko_id DROP NOT NULL;
CREATE UNIQUE INDEX idx_assets_onchain_identity
  ON assets (chain_id, contract_address)
  WHERE chain_id IS NOT NULL AND contract_address IS NOT NULL;
CREATE UNIQUE INDEX idx_assets_coingecko_id
  ON assets (coingecko_id)
  WHERE coingecko_id IS NOT NULL;

-- 2. Extend tax_lots with price status
ALTER TABLE tax_lots
  ADD COLUMN price_status VARCHAR(16) NOT NULL DEFAULT 'resolved',
    -- 'resolved' | 'pending' | 'unpriceable'
  ADD COLUMN price_resolution_attempts INT NOT NULL DEFAULT 0,
  ADD COLUMN price_next_retry_at TIMESTAMPTZ;
CREATE INDEX idx_tax_lots_price_status_retry
  ON tax_lots (price_status, price_next_retry_at)
  WHERE price_status = 'pending';
ALTER TABLE tax_lots ALTER COLUMN cost_basis_usd DROP NOT NULL;

-- 3. Backfill jobs queue
CREATE TABLE price_backfill_jobs (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  asset_id        UUID NOT NULL REFERENCES assets(id),
  target_time     TIMESTAMPTZ NOT NULL,  -- tx timestamp, minute-bucketed
  status          VARCHAR(16) NOT NULL DEFAULT 'pending',
                    -- 'pending' | 'in_progress' | 'resolved' | 'failed'
  attempts        INT NOT NULL DEFAULT 0,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  locked_at       TIMESTAMPTZ,
  last_error      TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  resolved_at     TIMESTAMPTZ,
  UNIQUE (asset_id, target_time)
);
CREATE INDEX idx_price_backfill_jobs_ready
  ON price_backfill_jobs (next_attempt_at)
  WHERE status = 'pending';

-- 4. price_history.source becomes explicit (no default)
ALTER TABLE price_history ALTER COLUMN source SET NOT NULL;
ALTER TABLE price_history ALTER COLUMN source DROP DEFAULT;
-- Allowed values (enforced in app):
-- 'coingecko' | 'zerion' | 'geckoterminal' | 'defillama' | 'manual'
```

**Why one job per `(asset_id, target_time)` not one job per lot:** several lots from the same sync can share a tx timestamp on the same asset (e.g., swap creates two legs). Dedup the lookups at the job layer; fan out price resolution to all affected lots via `PriceResolvedHook`.

### Domain types (new — `platform/price/`)

```go
type PriceStatus string
const (
    PriceResolved    PriceStatus = "resolved"
    PricePending     PriceStatus = "pending"
    PriceUnpriceable PriceStatus = "unpriceable"
)

type Source string
const (
    SourceCoinGecko     Source = "coingecko"
    SourceZerion        Source = "zerion"
    SourceGeckoTerminal Source = "geckoterminal"
    SourceDefiLlama     Source = "defillama"
    SourceManual        Source = "manual"
)

type Provider interface {
    Name() Source
    GetPrice(ctx context.Context, a asset.Asset) (*big.Int, error)
    GetHistoricalPrice(ctx context.Context, a asset.Asset, t time.Time) (*HistoricalPrice, error)
}

type HistoricalPrice struct {
    PriceUSD   *big.Int
    Timestamp  time.Time  // actual point-in-time, not the requested time
    Confidence float64    // 0-1 for DefiLlama; 1.0 for others
}

type PriceReader interface {
    Current(ctx context.Context, assetID uuid.UUID) (*big.Int, Source, error)
    Historical(ctx context.Context, assetID uuid.UUID, ts time.Time) (*HistoricalPrice, Source, error)
}
```

**`PriceReader.Current`** implementation is a single SQL read:

```sql
SELECT price_usd, source FROM price_history
WHERE asset_id = $1
ORDER BY
  CASE source
    WHEN 'coingecko' THEN 1
    WHEN 'geckoterminal' THEN 2
    WHEN 'defillama' THEN 3
    WHEN 'zerion' THEN 4
    WHEN 'manual' THEN 5
  END,
  time DESC
LIMIT 1;
```

The priority order (`CASE` branches) is derived from `PRICE_PROVIDER_PRIORITY` env at process start.

### Lot lifecycle

```
[lot created via sync, Zerion has price]
  → price_status=resolved immediately (cost_basis = qty * zerion_price)

[lot created via sync, Zerion missing price]
  → price_status=pending, cost_basis_usd=NULL
  → backfill_worker resolves price
  → PriceResolvedHook sets cost_basis_usd = qty * price, status=resolved
  → recompute downstream LotDisposals (FIFO preserved)

[backfill exhausts retries after 7 days]
  → price_status=unpriceable
  → UI surfaces manual-price CTA
  → user submits via PUT /lots/{id}/manual-price
  → cost_basis_usd = manual, status=resolved, price_history.source=manual
```

## Error Handling & Rate Limiting

### Error taxonomy (`platform/price/`)

```go
ErrRateLimited       // 429 — do NOT count as attempt; back off per Retry-After
ErrTransient         // network timeout, 5xx — do NOT count as attempt; short retry
ErrNotFound          // provider has no data — DOES count as attempt
ErrLowConfidence     // DefiLlama confidence < threshold — treated as NotFound
ErrUnsupportedChain  // provider doesn't cover this chain — DOES count as attempt
```

Only `ErrNotFound` / `ErrLowConfidence` / `ErrUnsupportedChain` increment `price_backfill_jobs.attempts`. `ErrRateLimited` and `ErrTransient` reschedule with a short delay without counting.

### Backoff schedule

| Attempt | Delay until next |
|---------|------------------|
| 1       | 15 min           |
| 2       | 1 h              |
| 3       | 6 h              |
| 4       | 24 h             |
| 5–10    | 24 h each        |
| 11      | `status=failed`, lot → `unpriceable` |

Total time from first miss to `unpriceable`: approximately 6 days 7 hours. Rounded to "~7 days" in user-facing copy.

### Rate limiters

Per-provider token-bucket, process-global:

- CoinGecko: 30 rpm (existing, unchanged)
- GeckoTerminal: 30 rpm, burst 5
- DefiLlama: 10 rps, burst 20

If the bucket is empty, the worker blocks up to `providerWaitTimeout` (default 5s). On timeout → treat as `ErrTransient` → reschedule.

### Cache behavior

- Check Redis *before* calling any provider.
- Key: `price:{source}:{asset_id}:{minute_bucket}` (intraday) or `{day_bucket}` (daily).
- TTL: 30 days (historical prices are immutable; cache is safe).
- Write-through on every successful provider call.
- `price_backfill_jobs` uniqueness on `(asset_id, target_time)` handles same-user dedup; the Redis cache handles cross-user dedup.

### Crash & restart safety

- Jobs claimed by `UPDATE price_backfill_jobs SET status='in_progress', locked_at=NOW() WHERE id=...` inside a transaction.
- A reaper goroutine (runs every 5 min) resets `status='pending'` for rows where `status='in_progress' AND locked_at < NOW() - 10 min`.
- Idempotent writes: `price_history` inserts use `ON CONFLICT (asset_id, time, source) DO NOTHING`.
- `PriceResolvedHook` is idempotent — same input → same cost basis, same downstream disposals.

### What the user sees

- Sync completes at the same speed as today, regardless of pending-price state.
- Portfolio response includes `pnl_is_partial: true`, `pending_lot_count`, `unpriceable_lot_count`. Frontend renders a banner: "N lots awaiting price resolution — PnL partial."
- Per-lot view exposes `price_status` and `price_source` on resolved lots.
- Unpriceable lots show a manual-entry CTA.

### Observability (Loki / structured logging)

Component label: `price_resolver`.

Structured fields: `asset_id`, `chain_id`, `contract_address`, `target_time`, `source_tried`, `source_succeeded`, `attempt_number`, `error_category`, `price_usd`, `confidence`.

Key LogQL queries:

```logql
{service="backend", component="price_resolver", level="ERROR"}
{service="backend", component="price_resolver"} | json | asset_id="<uuid>"
{service="backend", component="price_resolver"} | json | source_succeeded="geckoterminal"
```

Future counters (log-derivable; no Prometheus required for MVP):
- `price_backfill_jobs_pending` (gauge)
- `price_backfill_resolved_total{source=...}` (counter)
- `price_backfill_unpriceable_total` (counter)
- `provider_rate_limited_total{provider=...}` (counter)

## Testing

### Unit (`-short`)

- `platform/price/resolver_test.go` — mock provider chain: highest-priority success wins; falls through on `ErrNotFound`; returns `ErrPending` when all miss.
- `platform/price/backoff_test.go` — attempt N → delay matches schedule; attempt 11 → `unpriceable`.
- `infra/gateway/geckoterminal/client_test.go` — fixture parsing; 429 Retry-After handling; 404 → `ErrNotFound`.
- `infra/gateway/defillama/client_test.go` — fixture parsing; `confidence < 0.9` → `ErrLowConfidence`; empty `coins` map → `ErrNotFound`.
- `platform/price/cache_test.go` — minute-bucket key derivation; 30-day TTL; write-through.

### Ledger / lot integration (test DB)

- `TestLotCreated_PendingPrice` — Zerion `Price: nil` → lot status `pending`, `cost_basis IS NULL`, backfill job exists.
- `TestLotResolved_ViaBackfill` — seed pending lot, run worker with stub provider → lot resolved, correct `cost_basis`, `price_history` row.
- `TestLotResolved_RecomputesDownstreamDisposals` — pending lot + disposal → resolve price → both get correct PnL, FIFO preserved.
- `TestLotUnpriceable_AfterMaxAttempts` — stub `ErrNotFound` × advance clock → lot `unpriceable` after attempt 11.
- `TestManualPriceOverride_OnUnpriceable` — unpriceable lot + `PUT /lots/{id}/manual-price` → resolved, `source=manual`, downstream recompute.
- `TestAssetDedup_OnChainIdentity` — ingest same `(chain, contract_addr)` twice → single `assets` row.
- `TestAssetDedup_MergesWithExistingCoinGeckoAsset` — asset has `coingecko_id` *and* `(chain_id, contract_address)`; Zerion sync finds it by on-chain identity, no duplicate.

### Concurrency

- `TestBackfillWorker_ConcurrentClaims` — two workers race; `FOR UPDATE SKIP LOCKED` → only one wins.
- `TestPriceResolvedHook_ConcurrentResolutions` — two sources resolve same lot simultaneously; idempotent write wins, consistent state.
- `TestReaper_ReclaimsOrphanedJobs` — worker dies mid-job → reaper resets `in_progress → pending` after timeout.

### Contract (tagged `-tags=contract`, nightly CI)

- Each provider hit with a known token (e.g., USDC on Ethereum). Guards against provider API breaking changes.

### Frontend

- `pnl_is_partial` banner rendering.
- Manual-price form for unpriceable lots.

## Rollout

**Stage 1 — schema & backend logic, flag off.** Migrations applied; new code present; `FEATURE_PRICE_FALLBACK=false` means `zerion_processor` still writes `"0"` as today. New API endpoints return 501. Zero user-visible change. Bake 2–3 days.

**Stage 2 — enable for new syncs.** `FEATURE_PRICE_FALLBACK=true`. New Zerion transactions with missing prices go through the pending flow. Existing `"0"` data untouched. Monitor:
- `price_backfill_resolved_total` climbing (primary signal)
- `provider_rate_limited_total` staying near zero
- `{component="price_resolver", level="ERROR"}` near zero

**Stage 3 — historical backfill.** One-shot command: for every ledger entry with `usd_price='0'` where asset has `chain_id + contract_address`, enqueue a `price_backfill_job`. Throttled by the same worker. Hours to days to drain. Users' historical PnL gradually improves.

**Stage 4 — remove legacy path.** After Stage 3 reports 100% resolution-or-unpriceable, rip out the `"0"` default code and the feature flag.

## Configuration

```
PRICE_BACKFILL_ENABLED=true
PRICE_BACKFILL_RATE_RPS=1
PRICE_BACKFILL_WORKERS=1
PRICE_PROVIDER_PRIORITY=coingecko,geckoterminal,defillama
GECKOTERMINAL_BASE_URL=https://api.geckoterminal.com/api/v2
DEFILLAMA_BASE_URL=https://coins.llama.fi
DEFILLAMA_MIN_CONFIDENCE=0.9
```

No API keys required for GeckoTerminal or DefiLlama (both free, no auth).

## Out of scope (explicit)

- Per-asset preferred-source override (covered by global priority; add later only if priority misfires)
- On-chain RPC fallback via Uniswap pool reads (deferred; two HTTP providers expected to cover long tail)
- CoinGecko paid tier for hourly historical (routed through GeckoTerminal/DefiLlama instead)
- Native-coin pricing reconsideration — CoinGecko continues to own BTC/native-coin pricing and asset search/metadata
- Market cap and 24h volume for non-CoinGecko assets

## Research notes

Provider comparison (April 2026) that informed the design:

- **GeckoTerminal** (by CoinGecko) — 30 rpm free, no auth, 200+ chains, batch 30 tokens/call, OHLCV historical with minute granularity. Selected as primary fallback.
- **DefiLlama Coins API** — community adapters, 10 rps informal, no auth, timestamp-based historical. `confidence` field critical (reject `< 0.9`). Selected as secondary.
- **DexScreener** — 60 rpm free, no historical. Not selected (priority B requires historical).
- **Moralis / Alchemy / Covalent / CoinMarketCap / 1inch** — all evaluated and rejected for this use case (throttling, no contract-address lookup on free tier, or trial-only free plans).
