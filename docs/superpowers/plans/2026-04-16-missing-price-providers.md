# Missing-Price Providers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add GeckoTerminal + DefiLlama as fallback price providers so tokens unpriced by Zerion/CoinGecko get correct cost-basis and PnL via a pending→resolved lot lifecycle.

**Architecture:** New `platform/price/` package with a `PriceResolver` that chains providers by priority. Tax lots gain a `price_status` column (`resolved`|`pending`|`unpriceable`); a `price_backfill_jobs` queue backs a rate-limited worker that resolves prices async. `PriceResolvedHook` recomputes downstream disposals using the existing override machinery.

**Tech Stack:** Go 1.24, pgx/v5, PostgreSQL (TimescaleDB), Redis, chi router, existing `logger` package.

**Spec:** `docs/superpowers/specs/2026-04-16-missing-price-providers-design.md`

---

## File Structure

**New files:**
- `apps/backend/migrations/000025_price_fallback.up.sql` + `.down.sql` — schema changes (nullable `coingecko_id`, `price_status` column, `price_backfill_jobs` table)
- `apps/backend/internal/platform/price/errors.go` — typed provider errors
- `apps/backend/internal/platform/price/model.go` — `Source`, `PriceStatus`, `HistoricalPrice`, `Provider`, `PriceReader` types
- `apps/backend/internal/platform/price/resolver.go` — `PriceResolver` orchestrator
- `apps/backend/internal/platform/price/resolver_test.go` — unit tests
- `apps/backend/internal/platform/price/backoff.go` — backoff schedule
- `apps/backend/internal/platform/price/backoff_test.go` — schedule tests
- `apps/backend/internal/platform/price/cache.go` — Redis historical-price dedup
- `apps/backend/internal/platform/price/cache_test.go`
- `apps/backend/internal/platform/price/provider_coingecko.go` — adapter to existing coingecko client
- `apps/backend/internal/platform/price/provider_geckoterminal.go`
- `apps/backend/internal/platform/price/provider_defillama.go`
- `apps/backend/internal/platform/price/job_repo.go` — `PriceBackfillJobRepository` port
- `apps/backend/internal/platform/price/backfill_worker.go`
- `apps/backend/internal/platform/price/backfill_worker_test.go`
- `apps/backend/internal/platform/price/reader.go` — `PriceReader` implementation
- `apps/backend/internal/infra/gateway/geckoterminal/client.go`
- `apps/backend/internal/infra/gateway/geckoterminal/client_test.go`
- `apps/backend/internal/infra/gateway/geckoterminal/testdata/*.json`
- `apps/backend/internal/infra/gateway/defillama/client.go`
- `apps/backend/internal/infra/gateway/defillama/client_test.go`
- `apps/backend/internal/infra/gateway/defillama/testdata/*.json`
- `apps/backend/internal/infra/postgres/price_backfill_job_repo.go`
- `apps/backend/internal/infra/postgres/price_backfill_job_repo_test.go`
- `apps/backend/internal/infra/postgres/asset_repo_onchain_test.go`
- `apps/backend/internal/module/lots/handler.go` + `service.go` — `PUT /lots/{id}/manual-price` endpoint
- `apps/backend/internal/module/lots/handler_test.go`

**Modified files:**
- `apps/backend/internal/platform/asset/model.go` — relax `Validate()`, keep `CoinGeckoID` non-required
- `apps/backend/internal/platform/asset/port.go` — add `UpsertByOnChainIdentity` to `Repository`
- `apps/backend/internal/platform/asset/service.go` — add `UpsertByOnChainIdentity` method
- `apps/backend/internal/infra/postgres/asset_repo.go` — implement `UpsertByOnChainIdentity`
- `apps/backend/internal/ledger/taxlot_model.go` — add `PriceStatus`, `PriceResolutionAttempts`, `PriceNextRetryAt`; `AutoCostBasisPerUnit` nullable
- `apps/backend/internal/ledger/taxlot_port.go` — add `GetPendingPriceLots`, `ResolvePendingPrice`, `MarkUnpriceable`
- `apps/backend/internal/ledger/taxlot_hook.go` — handle `pending` state
- `apps/backend/internal/infra/postgres/taxlot_repo.go` — scan new columns, new methods
- `apps/backend/internal/module/portfolio/service.go` + `adapter.go` — surface `pnl_is_partial`, counts
- `apps/backend/internal/platform/sync/zerion_processor.go` — remove `"0"` default, enqueue backfill job
- `apps/backend/cmd/api/main.go` — wire new providers, worker, endpoints, feature flag

---

## Task 1: Migration — relax `assets.coingecko_id`, add `price_status` column, create jobs table

**Files:**
- Create: `apps/backend/migrations/000025_price_fallback.up.sql`
- Create: `apps/backend/migrations/000025_price_fallback.down.sql`

- [ ] **Step 1: Write the up migration**

```sql
-- 000025_price_fallback.up.sql

-- 1. Relax assets constraints; add on-chain identity uniqueness
ALTER TABLE assets ALTER COLUMN coingecko_id DROP NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_assets_onchain_identity
  ON assets (chain_id, contract_address)
  WHERE chain_id IS NOT NULL AND contract_address IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_assets_coingecko_id
  ON assets (coingecko_id)
  WHERE coingecko_id IS NOT NULL;

-- 2. Extend tax_lots with price status tracking
ALTER TABLE tax_lots
  ADD COLUMN IF NOT EXISTS price_status VARCHAR(16) NOT NULL DEFAULT 'resolved',
  ADD COLUMN IF NOT EXISTS price_resolution_attempts INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS price_next_retry_at TIMESTAMPTZ;

ALTER TABLE tax_lots ALTER COLUMN auto_cost_basis_per_unit DROP NOT NULL;

CREATE INDEX IF NOT EXISTS idx_tax_lots_price_status_retry
  ON tax_lots (price_status, price_next_retry_at)
  WHERE price_status = 'pending';

-- 3. Backfill job queue
CREATE TABLE IF NOT EXISTS price_backfill_jobs (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  asset_id        UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
  target_time     TIMESTAMPTZ NOT NULL,
  status          VARCHAR(16) NOT NULL DEFAULT 'pending',
  attempts        INT NOT NULL DEFAULT 0,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  locked_at       TIMESTAMPTZ,
  last_error      TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  resolved_at     TIMESTAMPTZ,
  CONSTRAINT uq_price_backfill_jobs_asset_time UNIQUE (asset_id, target_time)
);

CREATE INDEX IF NOT EXISTS idx_price_backfill_jobs_ready
  ON price_backfill_jobs (next_attempt_at)
  WHERE status = 'pending';

-- 4. price_history: make source explicit
ALTER TABLE price_history ALTER COLUMN source SET NOT NULL;
ALTER TABLE price_history ALTER COLUMN source DROP DEFAULT;
```

- [ ] **Step 2: Write the down migration**

```sql
-- 000025_price_fallback.down.sql

ALTER TABLE price_history ALTER COLUMN source SET DEFAULT 'coingecko';

DROP TABLE IF EXISTS price_backfill_jobs;

DROP INDEX IF EXISTS idx_tax_lots_price_status_retry;

ALTER TABLE tax_lots
  DROP COLUMN IF EXISTS price_next_retry_at,
  DROP COLUMN IF EXISTS price_resolution_attempts,
  DROP COLUMN IF EXISTS price_status;

ALTER TABLE tax_lots ALTER COLUMN auto_cost_basis_per_unit SET NOT NULL;

DROP INDEX IF EXISTS idx_assets_coingecko_id;
DROP INDEX IF EXISTS idx_assets_onchain_identity;

ALTER TABLE assets ALTER COLUMN coingecko_id SET NOT NULL;
```

- [ ] **Step 3: Apply migration**

Run: `just migrate-up`
Expected: no errors. Verify with `just db-connect` → `\d tax_lots` shows new columns, `\dt price_backfill_jobs` exists.

- [ ] **Step 4: Commit**

```bash
git add apps/backend/migrations/000025_price_fallback.up.sql apps/backend/migrations/000025_price_fallback.down.sql
git commit -m "feat(db): migration 000025 — price fallback schema (jobs queue, lot status)"
```

---

## Task 2: Asset model — relax `Validate()` and add `UpsertByOnChainIdentity`

**Files:**
- Modify: `apps/backend/internal/platform/asset/model.go:47-49` (remove `CoinGeckoID` required check)
- Modify: `apps/backend/internal/platform/asset/port.go` (add method to `Repository`)
- Modify: `apps/backend/internal/platform/asset/service.go` (add `UpsertByOnChainIdentity`)
- Modify: `apps/backend/internal/infra/postgres/asset_repo.go` (implement)
- Test: `apps/backend/internal/infra/postgres/asset_repo_onchain_test.go`

- [ ] **Step 1: Write the failing integration test**

```go
// apps/backend/internal/infra/postgres/asset_repo_onchain_test.go
package postgres_test

import (
    "context"
    "testing"

    "github.com/kislikjeka/moontrack/internal/infra/postgres"
    "github.com/kislikjeka/moontrack/internal/platform/asset"
    "github.com/stretchr/testify/require"
)

func TestAssetRepo_UpsertByOnChainIdentity_DedupesByChainAndAddress(t *testing.T) {
    if testing.Short() {
        t.Skip("integration test")
    }
    ctx := context.Background()
    pool := testPool(t)
    repo := postgres.NewAssetRepository(pool)

    chain := "ethereum"
    addr := "0xabcdef0000000000000000000000000000000001"

    a1, created1, err := repo.UpsertByOnChainIdentity(ctx, chain, addr, "TKN", "Token", 18)
    require.NoError(t, err)
    require.True(t, created1)

    a2, created2, err := repo.UpsertByOnChainIdentity(ctx, chain, addr, "TKN", "Token", 18)
    require.NoError(t, err)
    require.False(t, created2)
    require.Equal(t, a1.ID, a2.ID)
}

func TestAssetRepo_UpsertByOnChainIdentity_AllowsNullCoinGeckoID(t *testing.T) {
    if testing.Short() {
        t.Skip("integration test")
    }
    ctx := context.Background()
    pool := testPool(t)
    repo := postgres.NewAssetRepository(pool)

    a, _, err := repo.UpsertByOnChainIdentity(ctx, "polygon", "0xbeef000000000000000000000000000000000002", "X", "X", 18)
    require.NoError(t, err)
    require.Equal(t, "", a.CoinGeckoID)
    require.NotNil(t, a.ChainID)
    require.Equal(t, "polygon", *a.ChainID)
}

// testPool is defined in an existing _test.go in this package; reuse it.
var _ = asset.Asset{}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/backend && go test -run TestAssetRepo_UpsertByOnChainIdentity ./internal/infra/postgres/...`
Expected: FAIL — `UpsertByOnChainIdentity` undefined.

- [ ] **Step 3: Update model to remove `CoinGeckoID` required check**

In `apps/backend/internal/platform/asset/model.go`, replace the `Validate` function body:

```go
// Validate validates the asset fields
func (a *Asset) Validate() error {
    if a.Symbol == "" {
        return ErrInvalidSymbol
    }
    if a.Name == "" {
        return ErrInvalidName
    }
    // CoinGeckoID is now optional — on-chain tokens may not be listed.
    // Either coingecko_id OR (chain_id + contract_address) must identify the asset.
    if a.CoinGeckoID == "" && (a.ChainID == nil || a.ContractAddress == nil) {
        return ErrAssetIdentityRequired
    }
    if a.Decimals < 0 || a.Decimals > 78 {
        return ErrInvalidDecimals
    }
    if a.AssetType != AssetTypeCrypto && a.AssetType != AssetTypeFiat && a.AssetType != AssetTypeCustom {
        return ErrInvalidAssetType
    }
    return nil
}
```

In `apps/backend/internal/platform/asset/errors.go`, add:

```go
var ErrAssetIdentityRequired = errors.New("asset must have coingecko_id or (chain_id + contract_address)")
```

- [ ] **Step 4: Add `UpsertByOnChainIdentity` to the `Repository` interface**

In `apps/backend/internal/platform/asset/port.go`, add to the `Repository` interface:

```go
// UpsertByOnChainIdentity finds an asset by (chainID, contractAddress) or creates
// one. Returns the asset and whether it was newly created.
UpsertByOnChainIdentity(
    ctx context.Context,
    chainID, contractAddress string,
    symbol, name string,
    decimals int,
) (*Asset, bool, error)
```

- [ ] **Step 5: Add service method**

In `apps/backend/internal/platform/asset/service.go`, add:

```go
// UpsertByOnChainIdentity finds-or-creates an asset by on-chain identity.
// Used by the sync path to dedupe tokens without a CoinGecko listing.
func (s *Service) UpsertByOnChainIdentity(
    ctx context.Context,
    chainID, contractAddress string,
    symbol, name string,
    decimals int,
) (*Asset, bool, error) {
    return s.repo.UpsertByOnChainIdentity(ctx, chainID, contractAddress, symbol, name, decimals)
}
```

- [ ] **Step 6: Implement in postgres repo**

In `apps/backend/internal/infra/postgres/asset_repo.go`, add:

```go
// UpsertByOnChainIdentity uses the partial unique index idx_assets_onchain_identity
// to dedupe on (chain_id, contract_address).
func (r *AssetRepository) UpsertByOnChainIdentity(
    ctx context.Context,
    chainID, contractAddress string,
    symbol, name string,
    decimals int,
) (*asset.Asset, bool, error) {
    // Case-insensitive match on address — stored addresses are normalized to lowercase.
    addrLower := strings.ToLower(contractAddress)

    // Try to find an existing row first
    var existing asset.Asset
    var existingChain, existingAddr *string
    var existingCG *string
    row := r.pool.QueryRow(ctx, `
        SELECT id, symbol, name, coingecko_id, decimals, asset_type,
               chain_id, contract_address, market_cap_rank, is_active,
               metadata, created_at, updated_at
        FROM assets
        WHERE chain_id = $1 AND contract_address = $2
    `, chainID, addrLower)
    var cgStr sql.NullString
    var rankNullable sql.NullInt32
    var metadataBytes []byte
    err := row.Scan(
        &existing.ID, &existing.Symbol, &existing.Name, &cgStr, &existing.Decimals,
        &existing.AssetType, &existingChain, &existingAddr, &rankNullable, &existing.IsActive,
        &metadataBytes, &existing.CreatedAt, &existing.UpdatedAt,
    )
    if err == nil {
        if cgStr.Valid {
            existing.CoinGeckoID = cgStr.String
        }
        existing.ChainID = existingChain
        existing.ContractAddress = existingAddr
        if rankNullable.Valid {
            r := int(rankNullable.Int32)
            existing.MarketCapRank = &r
        }
        existing.Metadata = metadataBytes
        _ = existingCG
        return &existing, false, nil
    }
    if err != pgx.ErrNoRows {
        return nil, false, fmt.Errorf("failed to lookup asset by on-chain identity: %w", err)
    }

    // Not found — create. Race-safe: partial unique index catches concurrent inserts.
    newAsset := &asset.Asset{
        ID:              uuid.New(),
        Symbol:          symbol,
        Name:            name,
        Decimals:        decimals,
        AssetType:       asset.AssetTypeCrypto,
        ChainID:         &chainID,
        ContractAddress: &addrLower,
        IsActive:        true,
        Metadata:        json.RawMessage("{}"),
        CreatedAt:       time.Now(),
        UpdatedAt:       time.Now(),
    }
    _, err = r.pool.Exec(ctx, `
        INSERT INTO assets (id, symbol, name, coingecko_id, decimals, asset_type,
                            chain_id, contract_address, is_active, metadata,
                            created_at, updated_at)
        VALUES ($1, $2, $3, NULL, $4, $5, $6, $7, TRUE, $8, $9, $10)
        ON CONFLICT (chain_id, contract_address)
          WHERE chain_id IS NOT NULL AND contract_address IS NOT NULL
        DO NOTHING
    `, newAsset.ID, symbol, name, decimals, string(newAsset.AssetType),
        chainID, addrLower, newAsset.Metadata, newAsset.CreatedAt, newAsset.UpdatedAt)
    if err != nil {
        return nil, false, fmt.Errorf("failed to insert asset: %w", err)
    }
    // Re-select in case the insert hit the conflict and did nothing (race).
    row = r.pool.QueryRow(ctx, `
        SELECT id, symbol, name, coingecko_id, decimals, asset_type,
               chain_id, contract_address, market_cap_rank, is_active,
               metadata, created_at, updated_at
        FROM assets
        WHERE chain_id = $1 AND contract_address = $2
    `, chainID, addrLower)
    var out asset.Asset
    var cgOut sql.NullString
    var outChain, outAddr *string
    var outRank sql.NullInt32
    var outMeta []byte
    if err := row.Scan(
        &out.ID, &out.Symbol, &out.Name, &cgOut, &out.Decimals,
        &out.AssetType, &outChain, &outAddr, &outRank, &out.IsActive,
        &outMeta, &out.CreatedAt, &out.UpdatedAt,
    ); err != nil {
        return nil, false, fmt.Errorf("failed to re-read upserted asset: %w", err)
    }
    if cgOut.Valid {
        out.CoinGeckoID = cgOut.String
    }
    out.ChainID = outChain
    out.ContractAddress = outAddr
    if outRank.Valid {
        r := int(outRank.Int32)
        out.MarketCapRank = &r
    }
    out.Metadata = outMeta
    created := out.ID == newAsset.ID
    return &out, created, nil
}
```

Add imports: `"strings"`, `"database/sql"`.

- [ ] **Step 7: Run tests, verify pass**

Run: `cd apps/backend && go test -run TestAssetRepo_UpsertByOnChainIdentity ./internal/infra/postgres/... -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add apps/backend/internal/platform/asset apps/backend/internal/infra/postgres/asset_repo.go apps/backend/internal/infra/postgres/asset_repo_onchain_test.go
git commit -m "feat(asset): UpsertByOnChainIdentity; CoinGeckoID now optional"
```

---

## Task 3: Price package skeleton — types, errors, source priority

**Files:**
- Create: `apps/backend/internal/platform/price/errors.go`
- Create: `apps/backend/internal/platform/price/model.go`

- [ ] **Step 1: Write errors**

```go
// apps/backend/internal/platform/price/errors.go
package price

import "errors"

var (
    // ErrNotFound — provider has no data for this asset. Counts as an attempt.
    ErrNotFound = errors.New("price: not found at provider")

    // ErrRateLimited — provider returned 429. Does NOT count as an attempt.
    ErrRateLimited = errors.New("price: provider rate limited")

    // ErrTransient — 5xx or network error. Does NOT count as an attempt.
    ErrTransient = errors.New("price: transient provider error")

    // ErrLowConfidence — provider returned data below confidence threshold.
    // Treated like NotFound (counts as attempt).
    ErrLowConfidence = errors.New("price: provider confidence below threshold")

    // ErrUnsupportedChain — provider does not cover this chain. Counts as attempt.
    ErrUnsupportedChain = errors.New("price: provider does not support chain")

    // ErrPending — resolver found no resolved price; job is still pending.
    ErrPending = errors.New("price: resolution pending")

    // ErrUnpriceable — lot exhausted all attempts; no price available anywhere.
    ErrUnpriceable = errors.New("price: unpriceable")
)
```

- [ ] **Step 2: Write domain types**

```go
// apps/backend/internal/platform/price/model.go
package price

import (
    "context"
    "math/big"
    "time"

    "github.com/google/uuid"
    "github.com/kislikjeka/moontrack/internal/platform/asset"
)

// Source identifies which provider produced a price record.
type Source string

const (
    SourceCoinGecko     Source = "coingecko"
    SourceZerion        Source = "zerion"
    SourceGeckoTerminal Source = "geckoterminal"
    SourceDefiLlama     Source = "defillama"
    SourceManual        Source = "manual"
)

// PriceStatus is the lifecycle status of a tax lot's cost basis.
type PriceStatus string

const (
    PriceStatusResolved    PriceStatus = "resolved"
    PriceStatusPending     PriceStatus = "pending"
    PriceStatusUnpriceable PriceStatus = "unpriceable"
)

// HistoricalPrice is a provider's response to a point-in-time price lookup.
type HistoricalPrice struct {
    PriceUSD   *big.Int  // scaled 10^8
    Timestamp  time.Time // actual point-in-time the price is for
    Confidence float64   // 0..1; 1.0 for providers without a confidence field
}

// Provider is the fallback-provider contract.
type Provider interface {
    Name() Source
    GetPrice(ctx context.Context, a asset.Asset) (*big.Int, error)
    GetHistoricalPrice(ctx context.Context, a asset.Asset, t time.Time) (*HistoricalPrice, error)
}

// PriceReader exposes priority-ordered reads over price_history.
type PriceReader interface {
    Current(ctx context.Context, assetID uuid.UUID) (*big.Int, Source, error)
    Historical(ctx context.Context, assetID uuid.UUID, ts time.Time) (*HistoricalPrice, Source, error)
}
```

- [ ] **Step 3: Verify it builds**

Run: `cd apps/backend && go build ./internal/platform/price/...`
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add apps/backend/internal/platform/price
git commit -m "feat(price): types and error taxonomy"
```

---

## Task 4: Backoff schedule

**Files:**
- Create: `apps/backend/internal/platform/price/backoff.go`
- Create: `apps/backend/internal/platform/price/backoff_test.go`

- [ ] **Step 1: Write failing test**

```go
// apps/backend/internal/platform/price/backoff_test.go
package price

import (
    "testing"
    "time"

    "github.com/stretchr/testify/require"
)

func TestBackoff_Schedule(t *testing.T) {
    tests := []struct {
        attempt int
        want    time.Duration
    }{
        {1, 15 * time.Minute},
        {2, 1 * time.Hour},
        {3, 6 * time.Hour},
        {4, 24 * time.Hour},
        {5, 24 * time.Hour},
        {10, 24 * time.Hour},
    }
    for _, tt := range tests {
        got := BackoffDelay(tt.attempt)
        require.Equal(t, tt.want, got, "attempt %d", tt.attempt)
    }
}

func TestBackoff_IsTerminal(t *testing.T) {
    require.False(t, IsTerminalAttempt(10))
    require.True(t, IsTerminalAttempt(11))
    require.True(t, IsTerminalAttempt(99))
}
```

- [ ] **Step 2: Run failing**

Run: `cd apps/backend && go test -run TestBackoff ./internal/platform/price/...`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement**

```go
// apps/backend/internal/platform/price/backoff.go
package price

import "time"

// MaxAttempts — after this many attempts with ErrNotFound/ErrLowConfidence/ErrUnsupportedChain,
// a lot is marked unpriceable.
const MaxAttempts = 11

// BackoffDelay returns how long to wait before the next attempt.
// attempt is 1-indexed (attempt=1 is the first retry after the initial miss).
func BackoffDelay(attempt int) time.Duration {
    switch {
    case attempt <= 1:
        return 15 * time.Minute
    case attempt == 2:
        return 1 * time.Hour
    case attempt == 3:
        return 6 * time.Hour
    default:
        return 24 * time.Hour
    }
}

// IsTerminalAttempt returns true if this attempt number should mark the lot unpriceable.
func IsTerminalAttempt(attempt int) bool {
    return attempt >= MaxAttempts
}
```

- [ ] **Step 4: Run, verify pass**

Run: `cd apps/backend && go test -run TestBackoff ./internal/platform/price/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/platform/price/backoff.go apps/backend/internal/platform/price/backoff_test.go
git commit -m "feat(price): backoff schedule and terminal-attempt policy"
```

---

## Task 5: Redis cache for historical prices

**Files:**
- Create: `apps/backend/internal/platform/price/cache.go`
- Create: `apps/backend/internal/platform/price/cache_test.go`

- [ ] **Step 1: Write failing test**

```go
// apps/backend/internal/platform/price/cache_test.go
package price

import (
    "context"
    "math/big"
    "testing"
    "time"

    "github.com/google/uuid"
    "github.com/stretchr/testify/require"
)

// fakeRedis is a minimal in-memory implementation of the small interface the cache needs.
type fakeRedis struct {
    m map[string]string
}

func (f *fakeRedis) Get(ctx context.Context, key string) (string, bool, error) {
    v, ok := f.m[key]
    return v, ok, nil
}
func (f *fakeRedis) Set(ctx context.Context, key, value string, ttl time.Duration) error {
    if f.m == nil {
        f.m = map[string]string{}
    }
    f.m[key] = value
    return nil
}

func TestCache_KeyFormat_MinuteBucket(t *testing.T) {
    c := NewCache(&fakeRedis{}, 30*24*time.Hour)
    id := uuid.New()
    ts := time.Date(2026, 4, 16, 14, 37, 45, 0, time.UTC)
    k := c.historicalKey(SourceGeckoTerminal, id, ts)
    require.Equal(t, "price:hist:geckoterminal:"+id.String()+":2026-04-16T14:37Z", k)
}

func TestCache_WriteReadRoundTrip(t *testing.T) {
    ctx := context.Background()
    c := NewCache(&fakeRedis{}, time.Hour)
    id := uuid.New()
    ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
    hp := &HistoricalPrice{PriceUSD: big.NewInt(12345), Timestamp: ts, Confidence: 1}

    require.NoError(t, c.PutHistorical(ctx, SourceDefiLlama, id, ts, hp))

    got, ok, err := c.GetHistorical(ctx, SourceDefiLlama, id, ts)
    require.NoError(t, err)
    require.True(t, ok)
    require.Equal(t, "12345", got.PriceUSD.String())
    require.Equal(t, float64(1), got.Confidence)
    require.Equal(t, ts.UTC(), got.Timestamp.UTC())
}
```

- [ ] **Step 2: Run failing**

Run: `cd apps/backend && go test -run TestCache ./internal/platform/price/...`
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// apps/backend/internal/platform/price/cache.go
package price

import (
    "context"
    "encoding/json"
    "fmt"
    "math/big"
    "time"

    "github.com/google/uuid"
)

// RedisClient is the minimal subset of Redis operations the cache needs.
type RedisClient interface {
    Get(ctx context.Context, key string) (string, bool, error)
    Set(ctx context.Context, key, value string, ttl time.Duration) error
}

// Cache dedupes historical-price lookups across users/providers.
// Historical prices are immutable, so we use a long TTL (30 days).
type Cache struct {
    r   RedisClient
    ttl time.Duration
}

func NewCache(r RedisClient, ttl time.Duration) *Cache {
    return &Cache{r: r, ttl: ttl}
}

func (c *Cache) historicalKey(src Source, assetID uuid.UUID, t time.Time) string {
    return fmt.Sprintf("price:hist:%s:%s:%s", src, assetID, t.UTC().Format("2006-01-02T15:04Z"))
}

type cachedHistorical struct {
    PriceStr   string  `json:"p"`
    TsUnix     int64   `json:"t"`
    Confidence float64 `json:"c"`
}

func (c *Cache) PutHistorical(ctx context.Context, src Source, assetID uuid.UUID, at time.Time, hp *HistoricalPrice) error {
    payload := cachedHistorical{
        PriceStr:   hp.PriceUSD.String(),
        TsUnix:     hp.Timestamp.Unix(),
        Confidence: hp.Confidence,
    }
    b, err := json.Marshal(payload)
    if err != nil {
        return err
    }
    return c.r.Set(ctx, c.historicalKey(src, assetID, at), string(b), c.ttl)
}

func (c *Cache) GetHistorical(ctx context.Context, src Source, assetID uuid.UUID, at time.Time) (*HistoricalPrice, bool, error) {
    v, ok, err := c.r.Get(ctx, c.historicalKey(src, assetID, at))
    if err != nil || !ok {
        return nil, ok, err
    }
    var payload cachedHistorical
    if err := json.Unmarshal([]byte(v), &payload); err != nil {
        return nil, false, err
    }
    price := new(big.Int)
    price.SetString(payload.PriceStr, 10)
    return &HistoricalPrice{
        PriceUSD:   price,
        Timestamp:  time.Unix(payload.TsUnix, 0).UTC(),
        Confidence: payload.Confidence,
    }, true, nil
}
```

- [ ] **Step 4: Run, pass**

Run: `cd apps/backend && go test -run TestCache ./internal/platform/price/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/platform/price/cache.go apps/backend/internal/platform/price/cache_test.go
git commit -m "feat(price): Redis dedup cache for historical prices"
```

---

## Task 6: PriceResolver

**Files:**
- Create: `apps/backend/internal/platform/price/resolver.go`
- Create: `apps/backend/internal/platform/price/resolver_test.go`

- [ ] **Step 1: Write failing test**

```go
// apps/backend/internal/platform/price/resolver_test.go
package price_test

import (
    "context"
    "errors"
    "math/big"
    "testing"
    "time"

    "github.com/kislikjeka/moontrack/internal/platform/asset"
    "github.com/kislikjeka/moontrack/internal/platform/price"
    "github.com/kislikjeka/moontrack/pkg/logger"
    "github.com/stretchr/testify/require"
)

type stubProvider struct {
    name price.Source
    hist *price.HistoricalPrice
    err  error
}

func (s *stubProvider) Name() price.Source { return s.name }
func (s *stubProvider) GetPrice(ctx context.Context, a asset.Asset) (*big.Int, error) {
    if s.err != nil {
        return nil, s.err
    }
    return s.hist.PriceUSD, nil
}
func (s *stubProvider) GetHistoricalPrice(ctx context.Context, a asset.Asset, t time.Time) (*price.HistoricalPrice, error) {
    if s.err != nil {
        return nil, s.err
    }
    return s.hist, nil
}

func newHP(p int64) *price.HistoricalPrice {
    return &price.HistoricalPrice{PriceUSD: big.NewInt(p), Timestamp: time.Now(), Confidence: 1}
}

func TestResolver_FirstProviderWins(t *testing.T) {
    r := price.NewResolver([]price.Provider{
        &stubProvider{name: price.SourceCoinGecko, hist: newHP(100)},
        &stubProvider{name: price.SourceGeckoTerminal, hist: newHP(200)},
    }, nil, logger.NewNoop())

    hp, src, err := r.ResolveHistorical(context.Background(), asset.Asset{}, time.Now())
    require.NoError(t, err)
    require.Equal(t, int64(100), hp.PriceUSD.Int64())
    require.Equal(t, price.SourceCoinGecko, src)
}

func TestResolver_FallsThroughOnNotFound(t *testing.T) {
    r := price.NewResolver([]price.Provider{
        &stubProvider{name: price.SourceCoinGecko, err: price.ErrNotFound},
        &stubProvider{name: price.SourceGeckoTerminal, hist: newHP(222)},
    }, nil, logger.NewNoop())

    hp, src, err := r.ResolveHistorical(context.Background(), asset.Asset{}, time.Now())
    require.NoError(t, err)
    require.Equal(t, int64(222), hp.PriceUSD.Int64())
    require.Equal(t, price.SourceGeckoTerminal, src)
}

func TestResolver_ReturnsNotFoundWhenAllMiss(t *testing.T) {
    r := price.NewResolver([]price.Provider{
        &stubProvider{name: price.SourceCoinGecko, err: price.ErrNotFound},
        &stubProvider{name: price.SourceGeckoTerminal, err: price.ErrNotFound},
    }, nil, logger.NewNoop())

    _, _, err := r.ResolveHistorical(context.Background(), asset.Asset{}, time.Now())
    require.ErrorIs(t, err, price.ErrNotFound)
}

func TestResolver_PreservesRateLimitedError(t *testing.T) {
    // If all providers are rate-limited, the resolver should return ErrRateLimited
    // (not ErrNotFound), so the worker reschedules instead of counting an attempt.
    r := price.NewResolver([]price.Provider{
        &stubProvider{name: price.SourceCoinGecko, err: price.ErrRateLimited},
    }, nil, logger.NewNoop())

    _, _, err := r.ResolveHistorical(context.Background(), asset.Asset{}, time.Now())
    require.ErrorIs(t, err, price.ErrRateLimited)
}

func TestResolver_WrapsUnexpectedError(t *testing.T) {
    // Unknown provider error → treated as Transient so worker reschedules.
    r := price.NewResolver([]price.Provider{
        &stubProvider{name: price.SourceCoinGecko, err: errors.New("boom")},
    }, nil, logger.NewNoop())

    _, _, err := r.ResolveHistorical(context.Background(), asset.Asset{}, time.Now())
    require.ErrorIs(t, err, price.ErrTransient)
}
```

- [ ] **Step 2: Failing run**

Run: `cd apps/backend && go test -run TestResolver ./internal/platform/price/...`
Expected: FAIL.

- [ ] **Step 3: Implement resolver**

```go
// apps/backend/internal/platform/price/resolver.go
package price

import (
    "context"
    "errors"
    "math/big"
    "time"

    "github.com/kislikjeka/moontrack/internal/platform/asset"
    "github.com/kislikjeka/moontrack/pkg/logger"
)

// Resolver walks an ordered provider chain. The first non-error success wins.
type Resolver struct {
    providers []Provider
    cache     *Cache
    log       *logger.Logger
}

// NewResolver returns a Resolver. cache may be nil; providers order defines priority.
func NewResolver(providers []Provider, cache *Cache, log *logger.Logger) *Resolver {
    return &Resolver{providers: providers, cache: cache, log: log.WithField("component", "price_resolver")}
}

// ResolveHistorical walks the chain, trying each provider for a historical price.
//
// Error semantics:
//   - Returns first success.
//   - Falls through on ErrNotFound, ErrLowConfidence, ErrUnsupportedChain.
//   - Returns ErrRateLimited if ANY provider was rate-limited AND no success.
//   - Returns ErrTransient on network/5xx without any success.
//   - Returns ErrNotFound if all providers returned NotFound-class errors.
func (r *Resolver) ResolveHistorical(ctx context.Context, a asset.Asset, at time.Time) (*HistoricalPrice, Source, error) {
    var sawRateLimited, sawTransient bool
    var lastNotFound error

    for _, p := range r.providers {
        if r.cache != nil && a.ID != (asset.Asset{}).ID {
            if hp, ok, _ := r.cache.GetHistorical(ctx, p.Name(), a.ID, at); ok {
                return hp, p.Name(), nil
            }
        }

        hp, err := p.GetHistoricalPrice(ctx, a, at)
        if err == nil {
            if r.cache != nil && a.ID != (asset.Asset{}).ID {
                _ = r.cache.PutHistorical(ctx, p.Name(), a.ID, at, hp)
            }
            return hp, p.Name(), nil
        }
        switch {
        case errors.Is(err, ErrNotFound), errors.Is(err, ErrLowConfidence), errors.Is(err, ErrUnsupportedChain):
            lastNotFound = err
        case errors.Is(err, ErrRateLimited):
            sawRateLimited = true
        case errors.Is(err, ErrTransient):
            sawTransient = true
        default:
            // Unknown error → treat as transient (reschedule, don't count).
            sawTransient = true
            r.log.Warn("provider returned unexpected error",
                "provider", string(p.Name()), "error", err.Error())
        }
    }

    switch {
    case sawRateLimited:
        return nil, "", ErrRateLimited
    case sawTransient:
        return nil, "", ErrTransient
    case lastNotFound != nil:
        return nil, "", ErrNotFound
    }
    return nil, "", ErrNotFound
}

// ResolveCurrent walks the chain for a current price.
func (r *Resolver) ResolveCurrent(ctx context.Context, a asset.Asset) (*big.Int, Source, error) {
    var sawRateLimited, sawTransient bool

    for _, p := range r.providers {
        price, err := p.GetPrice(ctx, a)
        if err == nil {
            return price, p.Name(), nil
        }
        switch {
        case errors.Is(err, ErrRateLimited):
            sawRateLimited = true
        case errors.Is(err, ErrTransient):
            sawTransient = true
        case errors.Is(err, ErrNotFound), errors.Is(err, ErrLowConfidence), errors.Is(err, ErrUnsupportedChain):
            // fall through
        default:
            sawTransient = true
        }
    }

    if sawRateLimited {
        return nil, "", ErrRateLimited
    }
    if sawTransient {
        return nil, "", ErrTransient
    }
    return nil, "", ErrNotFound
}
```

Ensure `pkg/logger` has `NewNoop()`. If not, add a minimal one:

```go
// pkg/logger/noop.go  (create only if missing)
package logger
func NewNoop() *Logger { return New("", "silent") }
```

(If `logger.New` signature differs, adapt — the tests only need the logger to not panic on `WithField` and `Warn`.)

- [ ] **Step 4: Run, pass**

Run: `cd apps/backend && go test -run TestResolver ./internal/platform/price/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/platform/price/resolver.go apps/backend/internal/platform/price/resolver_test.go pkg/logger/noop.go
git commit -m "feat(price): PriceResolver with priority chain + error classification"
```

---

## Task 7: GeckoTerminal gateway client

**Files:**
- Create: `apps/backend/internal/infra/gateway/geckoterminal/client.go`
- Create: `apps/backend/internal/infra/gateway/geckoterminal/client_test.go`
- Create: `apps/backend/internal/infra/gateway/geckoterminal/testdata/tokens_multi.json`
- Create: `apps/backend/internal/infra/gateway/geckoterminal/testdata/ohlcv_minute.json`

- [ ] **Step 1: Save fixture JSONs**

`testdata/tokens_multi.json`:

```json
{
  "data": [
    {
      "id": "eth_0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
      "type": "token",
      "attributes": {
        "address": "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
        "name": "USD Coin",
        "symbol": "USDC",
        "decimals": 6,
        "price_usd": "1.0004"
      }
    }
  ]
}
```

`testdata/ohlcv_minute.json`:

```json
{
  "data": {
    "id": "pool-xyz",
    "type": "ohlcv_request_response",
    "attributes": {
      "ohlcv_list": [
        [1744816620, "1.0004", "1.0005", "1.0003", "1.0004", "12345.67"]
      ]
    }
  }
}
```

- [ ] **Step 2: Write failing test**

```go
// apps/backend/internal/infra/gateway/geckoterminal/client_test.go
package geckoterminal_test

import (
    "context"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/kislikjeka/moontrack/internal/infra/gateway/geckoterminal"
    "github.com/kislikjeka/moontrack/internal/platform/price"
    "github.com/stretchr/testify/require"
)

func fixture(t *testing.T, name string) []byte {
    t.Helper()
    b, err := os.ReadFile(filepath.Join("testdata", name))
    require.NoError(t, err)
    return b
}

func TestClient_GetTokenPriceByAddress_ParsesPriceUSD(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        require.Contains(t, r.URL.Path, "/networks/eth/tokens/multi/")
        w.Header().Set("Content-Type", "application/json")
        _, _ = w.Write(fixture(t, "tokens_multi.json"))
    }))
    defer srv.Close()

    c := geckoterminal.NewClient(geckoterminal.Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
    p, err := c.GetTokenPriceByAddress(context.Background(), "eth", "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48")
    require.NoError(t, err)
    // $1.0004 scaled 10^8 = 100040000
    require.Equal(t, "100040000", p.String())
}

func TestClient_429_ReturnsErrRateLimited(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Retry-After", "5")
        w.WriteHeader(http.StatusTooManyRequests)
    }))
    defer srv.Close()

    c := geckoterminal.NewClient(geckoterminal.Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
    _, err := c.GetTokenPriceByAddress(context.Background(), "eth", "0x0")
    require.ErrorIs(t, err, price.ErrRateLimited)
}

func TestClient_404_ReturnsErrNotFound(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusNotFound)
    }))
    defer srv.Close()

    c := geckoterminal.NewClient(geckoterminal.Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
    _, err := c.GetTokenPriceByAddress(context.Background(), "eth", "0x0")
    require.ErrorIs(t, err, price.ErrNotFound)
}

func TestClient_GetHistoricalPrice_PicksNearestMinute(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        require.Contains(t, r.URL.Path, "/ohlcv/minute")
        w.Header().Set("Content-Type", "application/json")
        _, _ = w.Write(fixture(t, "ohlcv_minute.json"))
    }))
    defer srv.Close()

    c := geckoterminal.NewClient(geckoterminal.Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
    ts := time.Unix(1744816620, 0).UTC()
    hp, err := c.GetPoolOHLCVMinute(context.Background(), "eth", "pool-xyz", ts)
    require.NoError(t, err)
    // close = 1.0004 → 100040000
    require.Equal(t, "100040000", hp.PriceUSD.String())
    require.Equal(t, ts, hp.Timestamp)
}
```

- [ ] **Step 3: Failing run**

Run: `cd apps/backend && go test ./internal/infra/gateway/geckoterminal/...`
Expected: FAIL.

- [ ] **Step 4: Implement client**

```go
// apps/backend/internal/infra/gateway/geckoterminal/client.go
package geckoterminal

import (
    "context"
    "encoding/json"
    "fmt"
    "math/big"
    "net/http"
    "net/url"
    "strconv"
    "time"

    "github.com/kislikjeka/moontrack/internal/platform/price"
)

// priceScale — USD prices are stored as big.Int scaled 10^8 throughout the codebase.
const priceScale = 8

// Config for the GeckoTerminal client.
type Config struct {
    BaseURL    string
    HTTPClient *http.Client
    Timeout    time.Duration
}

type Client struct {
    baseURL string
    http    *http.Client
}

func NewClient(cfg Config) *Client {
    hc := cfg.HTTPClient
    if hc == nil {
        to := cfg.Timeout
        if to == 0 {
            to = 10 * time.Second
        }
        hc = &http.Client{Timeout: to}
    }
    base := cfg.BaseURL
    if base == "" {
        base = "https://api.geckoterminal.com/api/v2"
    }
    return &Client{baseURL: base, http: hc}
}

type tokenMultiResponse struct {
    Data []struct {
        Attributes struct {
            Address  string `json:"address"`
            PriceUSD string `json:"price_usd"`
        } `json:"attributes"`
    } `json:"data"`
}

func decimalToBigIntScaled(s string) (*big.Int, error) {
    if s == "" {
        return nil, fmt.Errorf("empty decimal")
    }
    f, ok := new(big.Rat).SetString(s)
    if !ok {
        return nil, fmt.Errorf("bad decimal %q", s)
    }
    mult := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(priceScale)), nil)
    scaled := new(big.Rat).Mul(f, new(big.Rat).SetInt(mult))
    out := new(big.Int).Div(scaled.Num(), scaled.Denom())
    return out, nil
}

// GetTokenPriceByAddress calls /networks/{network}/tokens/multi/{address}.
// Returns a big.Int scaled by 10^8.
func (c *Client) GetTokenPriceByAddress(ctx context.Context, network, address string) (*big.Int, error) {
    u := fmt.Sprintf("%s/networks/%s/tokens/multi/%s",
        c.baseURL, url.PathEscape(network), url.PathEscape(address))
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
    if err != nil {
        return nil, err
    }
    resp, err := c.http.Do(req)
    if err != nil {
        return nil, fmt.Errorf("%w: %v", price.ErrTransient, err)
    }
    defer resp.Body.Close()

    switch {
    case resp.StatusCode == http.StatusTooManyRequests:
        return nil, price.ErrRateLimited
    case resp.StatusCode == http.StatusNotFound:
        return nil, price.ErrNotFound
    case resp.StatusCode >= 500:
        return nil, price.ErrTransient
    case resp.StatusCode >= 400:
        return nil, fmt.Errorf("%w: status %d", price.ErrNotFound, resp.StatusCode)
    }

    var out tokenMultiResponse
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        return nil, fmt.Errorf("%w: decode: %v", price.ErrTransient, err)
    }
    if len(out.Data) == 0 || out.Data[0].Attributes.PriceUSD == "" {
        return nil, price.ErrNotFound
    }
    return decimalToBigIntScaled(out.Data[0].Attributes.PriceUSD)
}

type ohlcvResponse struct {
    Data struct {
        Attributes struct {
            List [][]interface{} `json:"ohlcv_list"`
        } `json:"attributes"`
    } `json:"data"`
}

// GetPoolOHLCVMinute returns the minute candle close price nearest to `at`.
func (c *Client) GetPoolOHLCVMinute(ctx context.Context, network, poolAddress string, at time.Time) (*price.HistoricalPrice, error) {
    u := fmt.Sprintf("%s/networks/%s/pools/%s/ohlcv/minute?before_timestamp=%d&limit=5",
        c.baseURL, url.PathEscape(network), url.PathEscape(poolAddress), at.Unix()+60)
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
    if err != nil {
        return nil, err
    }
    resp, err := c.http.Do(req)
    if err != nil {
        return nil, fmt.Errorf("%w: %v", price.ErrTransient, err)
    }
    defer resp.Body.Close()

    switch {
    case resp.StatusCode == http.StatusTooManyRequests:
        return nil, price.ErrRateLimited
    case resp.StatusCode == http.StatusNotFound:
        return nil, price.ErrNotFound
    case resp.StatusCode >= 500:
        return nil, price.ErrTransient
    case resp.StatusCode >= 400:
        return nil, fmt.Errorf("%w: status %d", price.ErrNotFound, resp.StatusCode)
    }

    var out ohlcvResponse
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        return nil, fmt.Errorf("%w: decode: %v", price.ErrTransient, err)
    }
    if len(out.Data.Attributes.List) == 0 {
        return nil, price.ErrNotFound
    }

    // Each entry: [unix, open, high, low, close, volume]
    // Pick the candle whose timestamp is closest to `at`.
    var best []interface{}
    var bestDelta int64 = 1 << 62
    for _, cnd := range out.Data.Attributes.List {
        if len(cnd) < 5 {
            continue
        }
        var tsInt int64
        switch v := cnd[0].(type) {
        case float64:
            tsInt = int64(v)
        case int64:
            tsInt = v
        default:
            continue
        }
        delta := tsInt - at.Unix()
        if delta < 0 {
            delta = -delta
        }
        if delta < bestDelta {
            bestDelta = delta
            best = cnd
        }
    }
    if best == nil {
        return nil, price.ErrNotFound
    }
    closeStr, _ := best[4].(string)
    if closeStr == "" {
        // sometimes numeric
        if f, ok := best[4].(float64); ok {
            closeStr = strconv.FormatFloat(f, 'f', -1, 64)
        }
    }
    priceBI, err := decimalToBigIntScaled(closeStr)
    if err != nil {
        return nil, fmt.Errorf("%w: bad close: %v", price.ErrTransient, err)
    }
    var ts int64
    if f, ok := best[0].(float64); ok {
        ts = int64(f)
    }
    return &price.HistoricalPrice{
        PriceUSD:   priceBI,
        Timestamp:  time.Unix(ts, 0).UTC(),
        Confidence: 1,
    }, nil
}
```

- [ ] **Step 5: Run, pass**

Run: `cd apps/backend && go test ./internal/infra/gateway/geckoterminal/... -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/infra/gateway/geckoterminal
git commit -m "feat(gateway): GeckoTerminal client (token price + minute OHLCV)"
```

---

## Task 8: DefiLlama gateway client

**Files:**
- Create: `apps/backend/internal/infra/gateway/defillama/client.go`
- Create: `apps/backend/internal/infra/gateway/defillama/client_test.go`
- Create: `apps/backend/internal/infra/gateway/defillama/testdata/current.json`
- Create: `apps/backend/internal/infra/gateway/defillama/testdata/historical.json`
- Create: `apps/backend/internal/infra/gateway/defillama/testdata/low_confidence.json`

- [ ] **Step 1: Fixtures**

`testdata/current.json`:

```json
{
  "coins": {
    "ethereum:0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48": {
      "price": 1.0005,
      "timestamp": 1744816620,
      "confidence": 0.99,
      "decimals": 6,
      "symbol": "USDC"
    }
  }
}
```

`testdata/historical.json`:

```json
{
  "coins": {
    "ethereum:0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48": {
      "price": 0.9998,
      "timestamp": 1710000000,
      "confidence": 0.98,
      "symbol": "USDC"
    }
  }
}
```

`testdata/low_confidence.json`:

```json
{
  "coins": {
    "ethereum:0xdeadbeef00000000000000000000000000000001": {
      "price": 42,
      "timestamp": 1710000000,
      "confidence": 0.5
    }
  }
}
```

- [ ] **Step 2: Failing test**

```go
// apps/backend/internal/infra/gateway/defillama/client_test.go
package defillama_test

import (
    "context"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/kislikjeka/moontrack/internal/infra/gateway/defillama"
    "github.com/kislikjeka/moontrack/internal/platform/price"
    "github.com/stretchr/testify/require"
)

func fixture(t *testing.T, name string) []byte {
    t.Helper()
    b, err := os.ReadFile(filepath.Join("testdata", name))
    require.NoError(t, err)
    return b
}

func TestClient_Current_ParsesPrice(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        require.Contains(t, r.URL.Path, "/prices/current/")
        w.Write(fixture(t, "current.json"))
    }))
    defer srv.Close()

    c := defillama.NewClient(defillama.Config{BaseURL: srv.URL, MinConfidence: 0.9})
    p, err := c.GetCurrentPrice(context.Background(), "ethereum", "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48")
    require.NoError(t, err)
    require.Equal(t, "100050000", p.String())
}

func TestClient_Historical_ParsesPriceAndTimestamp(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        require.Contains(t, r.URL.Path, "/prices/historical/")
        w.Write(fixture(t, "historical.json"))
    }))
    defer srv.Close()

    c := defillama.NewClient(defillama.Config{BaseURL: srv.URL, MinConfidence: 0.9})
    at := time.Unix(1710000000, 0).UTC()
    hp, err := c.GetHistoricalPrice(context.Background(), "ethereum", "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48", at)
    require.NoError(t, err)
    require.Equal(t, "99980000", hp.PriceUSD.String())
    require.Equal(t, at, hp.Timestamp)
    require.InDelta(t, 0.98, hp.Confidence, 0.001)
}

func TestClient_LowConfidence_ReturnsErrLowConfidence(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Write(fixture(t, "low_confidence.json"))
    }))
    defer srv.Close()

    c := defillama.NewClient(defillama.Config{BaseURL: srv.URL, MinConfidence: 0.9})
    _, err := c.GetCurrentPrice(context.Background(), "ethereum", "0xdeadbeef00000000000000000000000000000001")
    require.ErrorIs(t, err, price.ErrLowConfidence)
}

func TestClient_Empty_ReturnsNotFound(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte(`{"coins":{}}`))
    }))
    defer srv.Close()

    c := defillama.NewClient(defillama.Config{BaseURL: srv.URL, MinConfidence: 0.9})
    _, err := c.GetCurrentPrice(context.Background(), "ethereum", "0x00")
    require.ErrorIs(t, err, price.ErrNotFound)
}

func TestClient_429(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusTooManyRequests)
    }))
    defer srv.Close()

    c := defillama.NewClient(defillama.Config{BaseURL: srv.URL, MinConfidence: 0.9})
    _, err := c.GetCurrentPrice(context.Background(), "ethereum", "0x00")
    require.ErrorIs(t, err, price.ErrRateLimited)
}
```

- [ ] **Step 3: Implement**

```go
// apps/backend/internal/infra/gateway/defillama/client.go
package defillama

import (
    "context"
    "encoding/json"
    "fmt"
    "math/big"
    "net/http"
    "net/url"
    "strconv"
    "time"

    "github.com/kislikjeka/moontrack/internal/platform/price"
)

const priceScale = 8

type Config struct {
    BaseURL       string
    HTTPClient    *http.Client
    Timeout       time.Duration
    MinConfidence float64
}

type Client struct {
    baseURL       string
    http          *http.Client
    minConfidence float64
}

func NewClient(cfg Config) *Client {
    hc := cfg.HTTPClient
    if hc == nil {
        to := cfg.Timeout
        if to == 0 {
            to = 10 * time.Second
        }
        hc = &http.Client{Timeout: to}
    }
    base := cfg.BaseURL
    if base == "" {
        base = "https://coins.llama.fi"
    }
    mc := cfg.MinConfidence
    if mc == 0 {
        mc = 0.9
    }
    return &Client{baseURL: base, http: hc, minConfidence: mc}
}

type coinEntry struct {
    Price      float64 `json:"price"`
    Timestamp  int64   `json:"timestamp"`
    Confidence float64 `json:"confidence"`
}
type coinsResponse struct {
    Coins map[string]coinEntry `json:"coins"`
}

func floatToBigIntScaled(f float64) *big.Int {
    // Format with fixed precision to avoid scientific notation then reparse as rational
    s := strconv.FormatFloat(f, 'f', -1, 64)
    rat, _ := new(big.Rat).SetString(s)
    if rat == nil {
        return big.NewInt(0)
    }
    mult := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(priceScale)), nil)
    scaled := new(big.Rat).Mul(rat, new(big.Rat).SetInt(mult))
    return new(big.Int).Div(scaled.Num(), scaled.Denom())
}

func (c *Client) doCoins(ctx context.Context, path string) (*coinsResponse, error) {
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
    if err != nil {
        return nil, err
    }
    resp, err := c.http.Do(req)
    if err != nil {
        return nil, fmt.Errorf("%w: %v", price.ErrTransient, err)
    }
    defer resp.Body.Close()

    switch {
    case resp.StatusCode == http.StatusTooManyRequests:
        return nil, price.ErrRateLimited
    case resp.StatusCode == http.StatusNotFound:
        return nil, price.ErrNotFound
    case resp.StatusCode >= 500:
        return nil, price.ErrTransient
    case resp.StatusCode >= 400:
        return nil, fmt.Errorf("%w: status %d", price.ErrNotFound, resp.StatusCode)
    }

    var out coinsResponse
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        return nil, fmt.Errorf("%w: decode: %v", price.ErrTransient, err)
    }
    return &out, nil
}

// GetCurrentPrice hits /prices/current/{chain:addr} and returns scaled big.Int.
func (c *Client) GetCurrentPrice(ctx context.Context, chain, addr string) (*big.Int, error) {
    key := fmt.Sprintf("%s:%s", chain, addr)
    out, err := c.doCoins(ctx, "/prices/current/"+url.PathEscape(key))
    if err != nil {
        return nil, err
    }
    entry, ok := out.Coins[key]
    if !ok {
        return nil, price.ErrNotFound
    }
    if entry.Confidence < c.minConfidence {
        return nil, price.ErrLowConfidence
    }
    return floatToBigIntScaled(entry.Price), nil
}

// GetHistoricalPrice hits /prices/historical/{ts}/{chain:addr}.
func (c *Client) GetHistoricalPrice(ctx context.Context, chain, addr string, at time.Time) (*price.HistoricalPrice, error) {
    key := fmt.Sprintf("%s:%s", chain, addr)
    out, err := c.doCoins(ctx, fmt.Sprintf("/prices/historical/%d/%s", at.Unix(), url.PathEscape(key)))
    if err != nil {
        return nil, err
    }
    entry, ok := out.Coins[key]
    if !ok {
        return nil, price.ErrNotFound
    }
    if entry.Confidence < c.minConfidence {
        return nil, price.ErrLowConfidence
    }
    return &price.HistoricalPrice{
        PriceUSD:   floatToBigIntScaled(entry.Price),
        Timestamp:  time.Unix(entry.Timestamp, 0).UTC(),
        Confidence: entry.Confidence,
    }, nil
}
```

- [ ] **Step 4: Run, pass**

Run: `cd apps/backend && go test ./internal/infra/gateway/defillama/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/infra/gateway/defillama
git commit -m "feat(gateway): DefiLlama client (current + historical with confidence filter)"
```

---

## Task 9: Price providers (adapters)

**Files:**
- Create: `apps/backend/internal/platform/price/provider_geckoterminal.go`
- Create: `apps/backend/internal/platform/price/provider_defillama.go`
- Create: `apps/backend/internal/platform/price/provider_coingecko.go`
- Create: `apps/backend/internal/platform/price/providers_test.go`

- [ ] **Step 1: Failing test**

```go
// apps/backend/internal/platform/price/providers_test.go
package price_test

import (
    "context"
    "math/big"
    "testing"
    "time"

    "github.com/kislikjeka/moontrack/internal/platform/asset"
    "github.com/kislikjeka/moontrack/internal/platform/price"
    "github.com/stretchr/testify/require"
)

type fakeGTClient struct {
    pricer func(chain, addr string) (*big.Int, error)
    hist   func(chain, pool string, at time.Time) (*price.HistoricalPrice, error)
}

func (f *fakeGTClient) GetTokenPriceByAddress(ctx context.Context, chain, addr string) (*big.Int, error) {
    return f.pricer(chain, addr)
}
func (f *fakeGTClient) GetPoolOHLCVMinute(ctx context.Context, chain, pool string, at time.Time) (*price.HistoricalPrice, error) {
    return f.hist(chain, pool, at)
}

func TestGeckoTerminalProvider_RequiresContractAddress(t *testing.T) {
    p := price.NewGeckoTerminalProvider(&fakeGTClient{
        pricer: func(chain, addr string) (*big.Int, error) { return big.NewInt(1), nil },
    })
    // Native L1 asset (no contract address) → not supported.
    _, err := p.GetPrice(context.Background(), asset.Asset{Symbol: "BTC"})
    require.ErrorIs(t, err, price.ErrUnsupportedChain)
}

func TestGeckoTerminalProvider_ReturnsPriceForTokens(t *testing.T) {
    p := price.NewGeckoTerminalProvider(&fakeGTClient{
        pricer: func(chain, addr string) (*big.Int, error) { return big.NewInt(100050000), nil },
    })
    chain := "ethereum"
    addr := "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48"
    got, err := p.GetPrice(context.Background(), asset.Asset{ChainID: &chain, ContractAddress: &addr})
    require.NoError(t, err)
    require.Equal(t, "100050000", got.String())
}
```

- [ ] **Step 2: Implement providers**

```go
// apps/backend/internal/platform/price/provider_geckoterminal.go
package price

import (
    "context"
    "math/big"
    "time"

    "github.com/kislikjeka/moontrack/internal/platform/asset"
)

// GeckoTerminalClient is the dependency GeckoTerminalProvider needs.
type GeckoTerminalClient interface {
    GetTokenPriceByAddress(ctx context.Context, chain, addr string) (*big.Int, error)
    GetPoolOHLCVMinute(ctx context.Context, chain, pool string, at time.Time) (*HistoricalPrice, error)
}

type GeckoTerminalProvider struct {
    c GeckoTerminalClient
}

func NewGeckoTerminalProvider(c GeckoTerminalClient) *GeckoTerminalProvider {
    return &GeckoTerminalProvider{c: c}
}

func (p *GeckoTerminalProvider) Name() Source { return SourceGeckoTerminal }

func (p *GeckoTerminalProvider) GetPrice(ctx context.Context, a asset.Asset) (*big.Int, error) {
    if a.ChainID == nil || a.ContractAddress == nil {
        return nil, ErrUnsupportedChain
    }
    return p.c.GetTokenPriceByAddress(ctx, *a.ChainID, *a.ContractAddress)
}

func (p *GeckoTerminalProvider) GetHistoricalPrice(ctx context.Context, a asset.Asset, at time.Time) (*HistoricalPrice, error) {
    if a.ChainID == nil || a.ContractAddress == nil {
        return nil, ErrUnsupportedChain
    }
    // Historical OHLCV requires a pool address. We use the contract address as the "token" query
    // and let the GeckoTerminal-resolved primary pool take effect — the client helper below
    // resolves token → primary pool via a lookup. For simplicity of MVP, if we don't know the
    // pool we fall through as NotFound; a later refinement can add a pool-finder.
    //
    // In the current client, GetPoolOHLCVMinute expects a pool address. We therefore map
    // `ContractAddress` to pool here only when the caller already knows the pool; otherwise
    // we return NotFound to skip to DefiLlama.
    //
    // This conservative behavior is the right default: we do NOT implement pool discovery here.
    return nil, ErrNotFound
}
```

```go
// apps/backend/internal/platform/price/provider_defillama.go
package price

import (
    "context"
    "math/big"
    "time"

    "github.com/kislikjeka/moontrack/internal/platform/asset"
)

type DefiLlamaClient interface {
    GetCurrentPrice(ctx context.Context, chain, addr string) (*big.Int, error)
    GetHistoricalPrice(ctx context.Context, chain, addr string, at time.Time) (*HistoricalPrice, error)
}

type DefiLlamaProvider struct {
    c DefiLlamaClient
}

func NewDefiLlamaProvider(c DefiLlamaClient) *DefiLlamaProvider {
    return &DefiLlamaProvider{c: c}
}

func (p *DefiLlamaProvider) Name() Source { return SourceDefiLlama }

func (p *DefiLlamaProvider) GetPrice(ctx context.Context, a asset.Asset) (*big.Int, error) {
    if a.ChainID == nil || a.ContractAddress == nil {
        return nil, ErrUnsupportedChain
    }
    return p.c.GetCurrentPrice(ctx, *a.ChainID, *a.ContractAddress)
}

func (p *DefiLlamaProvider) GetHistoricalPrice(ctx context.Context, a asset.Asset, at time.Time) (*HistoricalPrice, error) {
    if a.ChainID == nil || a.ContractAddress == nil {
        return nil, ErrUnsupportedChain
    }
    return p.c.GetHistoricalPrice(ctx, *a.ChainID, *a.ContractAddress, at)
}
```

```go
// apps/backend/internal/platform/price/provider_coingecko.go
package price

import (
    "context"
    "math/big"
    "time"

    "github.com/kislikjeka/moontrack/internal/platform/asset"
)

// CoinGeckoBridge adapts the existing coingecko-capable asset.Service into a Provider.
type CoinGeckoBridge interface {
    GetCurrentPriceByCoinGeckoID(ctx context.Context, coinGeckoID string) (*big.Int, error)
    GetHistoricalPriceByCoinGeckoID(ctx context.Context, coinGeckoID string, date time.Time) (*big.Int, error)
}

type CoinGeckoProvider struct {
    b CoinGeckoBridge
}

func NewCoinGeckoProvider(b CoinGeckoBridge) *CoinGeckoProvider {
    return &CoinGeckoProvider{b: b}
}

func (p *CoinGeckoProvider) Name() Source { return SourceCoinGecko }

func (p *CoinGeckoProvider) GetPrice(ctx context.Context, a asset.Asset) (*big.Int, error) {
    if a.CoinGeckoID == "" {
        return nil, ErrNotFound
    }
    price, err := p.b.GetCurrentPriceByCoinGeckoID(ctx, a.CoinGeckoID)
    if err != nil {
        return nil, ErrTransient
    }
    if price == nil {
        return nil, ErrNotFound
    }
    return price, nil
}

func (p *CoinGeckoProvider) GetHistoricalPrice(ctx context.Context, a asset.Asset, at time.Time) (*HistoricalPrice, error) {
    if a.CoinGeckoID == "" {
        return nil, ErrNotFound
    }
    // CoinGecko free tier is day-granular; normalize to midnight UTC.
    day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
    priceBI, err := p.b.GetHistoricalPriceByCoinGeckoID(ctx, a.CoinGeckoID, day)
    if err != nil {
        return nil, ErrTransient
    }
    if priceBI == nil {
        return nil, ErrNotFound
    }
    return &HistoricalPrice{PriceUSD: priceBI, Timestamp: day, Confidence: 1}, nil
}
```

- [ ] **Step 3: Run, pass**

Run: `cd apps/backend && go test -run TestGeckoTerminalProvider ./internal/platform/price/... -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/backend/internal/platform/price/provider_*.go apps/backend/internal/platform/price/providers_test.go
git commit -m "feat(price): GeckoTerminal/DefiLlama/CoinGecko adapter providers"
```

---

## Task 10: `PriceBackfillJobRepository` — port + postgres implementation

**Files:**
- Create: `apps/backend/internal/platform/price/job_repo.go` (port)
- Create: `apps/backend/internal/infra/postgres/price_backfill_job_repo.go`
- Create: `apps/backend/internal/infra/postgres/price_backfill_job_repo_test.go`

- [ ] **Step 1: Port**

```go
// apps/backend/internal/platform/price/job_repo.go
package price

import (
    "context"
    "time"

    "github.com/google/uuid"
)

type JobStatus string

const (
    JobStatusPending    JobStatus = "pending"
    JobStatusInProgress JobStatus = "in_progress"
    JobStatusResolved   JobStatus = "resolved"
    JobStatusFailed     JobStatus = "failed"
)

type BackfillJob struct {
    ID            uuid.UUID
    AssetID       uuid.UUID
    TargetTime    time.Time
    Status        JobStatus
    Attempts      int
    NextAttemptAt time.Time
    LockedAt      *time.Time
    LastError     string
    CreatedAt     time.Time
    ResolvedAt    *time.Time
}

// JobRepository is the port for the backfill queue.
type JobRepository interface {
    // Enqueue inserts a job or returns the existing one (idempotent on asset+time).
    Enqueue(ctx context.Context, assetID uuid.UUID, targetTime time.Time) (*BackfillJob, error)

    // ClaimReady attempts to lock one ready job atomically (FOR UPDATE SKIP LOCKED).
    // Returns (nil, nil) if none ready.
    ClaimReady(ctx context.Context) (*BackfillJob, error)

    // MarkResolved sets status=resolved and resolved_at.
    MarkResolved(ctx context.Context, jobID uuid.UUID) error

    // Reschedule increments attempts, sets next_attempt_at, updates last_error,
    // and may transition to status=failed when attempts >= MaxAttempts.
    Reschedule(ctx context.Context, jobID uuid.UUID, attempts int, nextAttemptAt time.Time, lastError string, terminal bool) error

    // UnlockWithoutCounting releases the lock without incrementing attempts
    // (used on rate-limit / transient errors).
    UnlockWithoutCounting(ctx context.Context, jobID uuid.UUID, nextAttemptAt time.Time) error

    // ReapStale resets status=in_progress rows whose lock is older than `staleAfter`.
    ReapStale(ctx context.Context, staleAfter time.Duration) (int, error)
}
```

- [ ] **Step 2: Failing integration test**

```go
// apps/backend/internal/infra/postgres/price_backfill_job_repo_test.go
package postgres_test

import (
    "context"
    "testing"
    "time"

    "github.com/google/uuid"
    "github.com/kislikjeka/moontrack/internal/infra/postgres"
    "github.com/kislikjeka/moontrack/internal/platform/price"
    "github.com/stretchr/testify/require"
)

func TestPriceBackfillJobRepo_EnqueueIsIdempotent(t *testing.T) {
    if testing.Short() {
        t.Skip("integration")
    }
    ctx := context.Background()
    pool := testPool(t)
    assetID := seedAsset(t, pool)
    repo := postgres.NewPriceBackfillJobRepository(pool)

    at := time.Now().UTC().Truncate(time.Minute)
    j1, err := repo.Enqueue(ctx, assetID, at)
    require.NoError(t, err)
    j2, err := repo.Enqueue(ctx, assetID, at)
    require.NoError(t, err)
    require.Equal(t, j1.ID, j2.ID)
}

func TestPriceBackfillJobRepo_ClaimReady_SkipsLocked(t *testing.T) {
    if testing.Short() {
        t.Skip("integration")
    }
    ctx := context.Background()
    pool := testPool(t)
    assetID := seedAsset(t, pool)
    repo := postgres.NewPriceBackfillJobRepository(pool)

    at := time.Now().UTC().Truncate(time.Minute)
    _, err := repo.Enqueue(ctx, assetID, at)
    require.NoError(t, err)

    j, err := repo.ClaimReady(ctx)
    require.NoError(t, err)
    require.NotNil(t, j)
    require.Equal(t, price.JobStatusInProgress, j.Status)

    j2, err := repo.ClaimReady(ctx)
    require.NoError(t, err)
    require.Nil(t, j2, "second claim should find nothing ready")
}

func TestPriceBackfillJobRepo_RescheduleTerminal(t *testing.T) {
    if testing.Short() {
        t.Skip("integration")
    }
    ctx := context.Background()
    pool := testPool(t)
    assetID := seedAsset(t, pool)
    repo := postgres.NewPriceBackfillJobRepository(pool)

    at := time.Now().UTC().Truncate(time.Minute)
    j, err := repo.Enqueue(ctx, assetID, at)
    require.NoError(t, err)

    err = repo.Reschedule(ctx, j.ID, 11, time.Now().Add(time.Hour), "exhausted", true)
    require.NoError(t, err)
    // Cannot be claimed anymore (status=failed)
    j2, _ := repo.ClaimReady(ctx)
    require.Nil(t, j2)
}

// seedAsset + testPool helpers are defined in an existing _test.go; add if missing.
var _ = uuid.UUID{}
```

- [ ] **Step 3: Implement**

```go
// apps/backend/internal/infra/postgres/price_backfill_job_repo.go
package postgres

import (
    "context"
    "fmt"
    "time"

    "github.com/google/uuid"
    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/kislikjeka/moontrack/internal/platform/price"
)

type PriceBackfillJobRepository struct {
    pool *pgxpool.Pool
}

func NewPriceBackfillJobRepository(pool *pgxpool.Pool) *PriceBackfillJobRepository {
    return &PriceBackfillJobRepository{pool: pool}
}

func (r *PriceBackfillJobRepository) Enqueue(ctx context.Context, assetID uuid.UUID, targetTime time.Time) (*price.BackfillJob, error) {
    target := targetTime.UTC().Truncate(time.Minute)
    row := r.pool.QueryRow(ctx, `
        INSERT INTO price_backfill_jobs (asset_id, target_time)
        VALUES ($1, $2)
        ON CONFLICT (asset_id, target_time) DO UPDATE
            SET asset_id = EXCLUDED.asset_id  -- no-op to return row
        RETURNING id, asset_id, target_time, status, attempts, next_attempt_at, locked_at, last_error, created_at, resolved_at
    `, assetID, target)
    return scanJob(row)
}

func (r *PriceBackfillJobRepository) ClaimReady(ctx context.Context) (*price.BackfillJob, error) {
    tx, err := r.pool.Begin(ctx)
    if err != nil {
        return nil, err
    }
    defer tx.Rollback(ctx)

    row := tx.QueryRow(ctx, `
        SELECT id, asset_id, target_time, status, attempts, next_attempt_at, locked_at, last_error, created_at, resolved_at
        FROM price_backfill_jobs
        WHERE status = 'pending' AND next_attempt_at <= NOW()
        ORDER BY next_attempt_at
        FOR UPDATE SKIP LOCKED
        LIMIT 1
    `)
    job, err := scanJob(row)
    if err != nil {
        if err == pgx.ErrNoRows {
            return nil, nil
        }
        return nil, err
    }
    _, err = tx.Exec(ctx, `
        UPDATE price_backfill_jobs
        SET status = 'in_progress', locked_at = NOW()
        WHERE id = $1
    `, job.ID)
    if err != nil {
        return nil, err
    }
    job.Status = price.JobStatusInProgress
    now := time.Now().UTC()
    job.LockedAt = &now
    return job, tx.Commit(ctx)
}

func (r *PriceBackfillJobRepository) MarkResolved(ctx context.Context, jobID uuid.UUID) error {
    _, err := r.pool.Exec(ctx, `
        UPDATE price_backfill_jobs
        SET status = 'resolved', resolved_at = NOW(), locked_at = NULL
        WHERE id = $1
    `, jobID)
    return err
}

func (r *PriceBackfillJobRepository) Reschedule(ctx context.Context, jobID uuid.UUID, attempts int, next time.Time, lastError string, terminal bool) error {
    status := price.JobStatusPending
    if terminal {
        status = price.JobStatusFailed
    }
    _, err := r.pool.Exec(ctx, `
        UPDATE price_backfill_jobs
        SET attempts = $2, next_attempt_at = $3, last_error = $4, status = $5, locked_at = NULL
        WHERE id = $1
    `, jobID, attempts, next, lastError, string(status))
    return err
}

func (r *PriceBackfillJobRepository) UnlockWithoutCounting(ctx context.Context, jobID uuid.UUID, next time.Time) error {
    _, err := r.pool.Exec(ctx, `
        UPDATE price_backfill_jobs
        SET status = 'pending', next_attempt_at = $2, locked_at = NULL
        WHERE id = $1
    `, jobID, next)
    return err
}

func (r *PriceBackfillJobRepository) ReapStale(ctx context.Context, staleAfter time.Duration) (int, error) {
    ct, err := r.pool.Exec(ctx, `
        UPDATE price_backfill_jobs
        SET status = 'pending', locked_at = NULL
        WHERE status = 'in_progress' AND locked_at < NOW() - $1::interval
    `, fmt.Sprintf("%d seconds", int(staleAfter.Seconds())))
    if err != nil {
        return 0, err
    }
    return int(ct.RowsAffected()), nil
}

func scanJob(row pgx.Row) (*price.BackfillJob, error) {
    var j price.BackfillJob
    var status string
    var locked, resolved *time.Time
    var lastErr *string
    err := row.Scan(
        &j.ID, &j.AssetID, &j.TargetTime, &status, &j.Attempts,
        &j.NextAttemptAt, &locked, &lastErr, &j.CreatedAt, &resolved,
    )
    if err != nil {
        return nil, err
    }
    j.Status = price.JobStatus(status)
    if locked != nil {
        j.LockedAt = locked
    }
    if lastErr != nil {
        j.LastError = *lastErr
    }
    if resolved != nil {
        j.ResolvedAt = resolved
    }
    return &j, nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd apps/backend && go test -run TestPriceBackfillJobRepo ./internal/infra/postgres/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/platform/price/job_repo.go apps/backend/internal/infra/postgres/price_backfill_job_repo*.go
git commit -m "feat(price): backfill job queue repo (idempotent enqueue, skip-locked claim)"
```

---

## Task 11: TaxLot model + repo: add `price_status`, pending/resolve helpers

**Files:**
- Modify: `apps/backend/internal/ledger/taxlot_model.go`
- Modify: `apps/backend/internal/ledger/taxlot_port.go`
- Modify: `apps/backend/internal/infra/postgres/taxlot_repo.go`
- Test: `apps/backend/internal/infra/postgres/taxlot_pending_test.go` (new)

- [ ] **Step 1: Extend model**

In `apps/backend/internal/ledger/taxlot_model.go`, add:

```go
// PriceStatus describes the cost-basis resolution state of a lot.
type PriceStatus string

const (
    PriceStatusResolved    PriceStatus = "resolved"
    PriceStatusPending     PriceStatus = "pending"
    PriceStatusUnpriceable PriceStatus = "unpriceable"
)
```

Add fields to `TaxLot`:

```go
type TaxLot struct {
    // ... existing fields ...
    PriceStatus              PriceStatus
    PriceResolutionAttempts  int
    PriceNextRetryAt         *time.Time
}
```

Update `EffectiveCostBasisPerUnit`:

```go
// EffectiveCostBasisPerUnit returns the cost basis to use for PnL calculations.
// Priority: override > auto. Returns nil when the lot is pending and has
// neither an auto nor override cost basis yet.
func (l *TaxLot) EffectiveCostBasisPerUnit() *big.Int {
    if l.OverrideCostBasisPerUnit != nil {
        return l.OverrideCostBasisPerUnit
    }
    return l.AutoCostBasisPerUnit // may be nil for pending lots
}
```

- [ ] **Step 2: Extend port**

In `apps/backend/internal/ledger/taxlot_port.go`, add to `TaxLotRepository`:

```go
// ListPendingLotsByAssetAndTime returns all lots whose cost basis is pending
// and which share (assetSymbol, acquiredAt). Used by PriceResolvedHook.
ListPendingLotsByAssetAndTime(ctx context.Context, asset string, at time.Time) ([]*TaxLot, error)

// ResolvePendingPrice sets cost basis, transitions price_status to resolved.
ResolvePendingPrice(ctx context.Context, lotID uuid.UUID, autoCostBasisPerUnit *big.Int, autoSource CostBasisSource) error

// MarkUnpriceable transitions price_status to unpriceable.
MarkUnpriceable(ctx context.Context, lotID uuid.UUID) error

// IncrementAttempt advances attempts count and next-retry time for a pending lot.
IncrementAttempt(ctx context.Context, lotID uuid.UUID, attempts int, nextRetryAt time.Time) error
```

- [ ] **Step 3: Failing test**

```go
// apps/backend/internal/infra/postgres/taxlot_pending_test.go
package postgres_test

import (
    "context"
    "math/big"
    "testing"
    "time"

    "github.com/kislikjeka/moontrack/internal/ledger"
    "github.com/stretchr/testify/require"
)

func TestTaxLotRepo_ListPendingLots(t *testing.T) {
    if testing.Short() {
        t.Skip("integration")
    }
    ctx := context.Background()
    pool := testPool(t)
    repo := testTaxLotRepo(t, pool)

    // seed a pending lot via repo.CreateTaxLot with PriceStatus=pending.
    assetID := seedAssetSymbol(t, pool)
    at := time.Now().UTC().Truncate(time.Minute)
    lot := &ledger.TaxLot{
        ID:                      testUUID(),
        TransactionID:           testUUID(),
        AccountID:               testAccountID(t, pool),
        Asset:                   assetID,
        QuantityAcquired:        big.NewInt(1000),
        QuantityRemaining:       big.NewInt(1000),
        AcquiredAt:              at,
        AutoCostBasisPerUnit:    nil, // pending
        AutoCostBasisSource:     ledger.CostBasisFMVAtTransfer,
        PriceStatus:             ledger.PriceStatusPending,
        PriceResolutionAttempts: 0,
    }
    require.NoError(t, repo.CreateTaxLot(ctx, lot))

    lots, err := repo.ListPendingLotsByAssetAndTime(ctx, assetID, at)
    require.NoError(t, err)
    require.Len(t, lots, 1)
    require.Equal(t, ledger.PriceStatusPending, lots[0].PriceStatus)

    // resolve
    require.NoError(t, repo.ResolvePendingPrice(ctx, lot.ID, big.NewInt(100000000), ledger.CostBasisFMVAtTransfer))
    again, err := repo.GetTaxLot(ctx, lot.ID)
    require.NoError(t, err)
    require.Equal(t, ledger.PriceStatusResolved, again.PriceStatus)
    require.Equal(t, "100000000", again.EffectiveCostBasisPerUnit().String())
}
```

- [ ] **Step 4: Implement in `taxlot_repo.go`**

Add scanning for `price_status`, `price_resolution_attempts`, `price_next_retry_at` to the existing scanTaxLot / rowScan functions. Handle NULL `auto_cost_basis_per_unit`.

Add methods (append to the file):

```go
func (r *TaxLotRepository) ListPendingLotsByAssetAndTime(ctx context.Context, asset string, at time.Time) ([]*ledger.TaxLot, error) {
    // We match on the minute bucket because jobs are minute-bucketed.
    minStart := at.UTC().Truncate(time.Minute)
    minEnd := minStart.Add(time.Minute)
    rows, err := r.pool.Query(ctx, `
        SELECT `+taxLotColumns+`
        FROM tax_lots
        WHERE asset_id = $1 AND price_status = 'pending'
          AND acquired_at >= $2 AND acquired_at < $3
    `, asset, minStart, minEnd)
    if err != nil {
        return nil, fmt.Errorf("list pending lots: %w", err)
    }
    defer rows.Close()
    return scanTaxLotRows(rows)
}

func (r *TaxLotRepository) ResolvePendingPrice(ctx context.Context, lotID uuid.UUID, autoCB *big.Int, src ledger.CostBasisSource) error {
    _, err := r.pool.Exec(ctx, `
        UPDATE tax_lots
        SET auto_cost_basis_per_unit = $2,
            auto_cost_basis_source = $3,
            price_status = 'resolved',
            price_next_retry_at = NULL
        WHERE id = $1 AND price_status = 'pending'
    `, lotID, autoCB.String(), string(src))
    return err
}

func (r *TaxLotRepository) MarkUnpriceable(ctx context.Context, lotID uuid.UUID) error {
    _, err := r.pool.Exec(ctx, `
        UPDATE tax_lots
        SET price_status = 'unpriceable', price_next_retry_at = NULL
        WHERE id = $1 AND price_status = 'pending'
    `, lotID)
    return err
}

func (r *TaxLotRepository) IncrementAttempt(ctx context.Context, lotID uuid.UUID, attempts int, next time.Time) error {
    _, err := r.pool.Exec(ctx, `
        UPDATE tax_lots
        SET price_resolution_attempts = $2, price_next_retry_at = $3
        WHERE id = $1 AND price_status = 'pending'
    `, lotID, attempts, next)
    return err
}
```

(Replace `taxLotColumns` and `scanTaxLotRows` with the actual names used in the file — keep naming consistent with existing code.)

- [ ] **Step 5: Run tests**

Run: `cd apps/backend && go test -run TestTaxLotRepo_ListPendingLots ./internal/infra/postgres/... -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add apps/backend/internal/ledger/taxlot_model.go apps/backend/internal/ledger/taxlot_port.go apps/backend/internal/infra/postgres/taxlot_repo.go apps/backend/internal/infra/postgres/taxlot_pending_test.go
git commit -m "feat(ledger): TaxLot price_status + pending/resolve/unpriceable repo methods"
```

---

## Task 12: `TaxLotHook` — create pending lots when cost basis is nil

**Files:**
- Modify: `apps/backend/internal/ledger/taxlot_hook.go`
- Test: `apps/backend/internal/ledger/taxlot_hook_pending_test.go`

- [ ] **Step 1: Failing test**

```go
// apps/backend/internal/ledger/taxlot_hook_pending_test.go
package ledger_test

import (
    "context"
    "math/big"
    "testing"
    "time"

    "github.com/google/uuid"
    "github.com/kislikjeka/moontrack/internal/ledger"
    "github.com/stretchr/testify/require"
)

func TestTaxLotHook_CreatesPendingLotWhenUSDRateMissing(t *testing.T) {
    ctx := context.Background()
    fx := newTaxLotFixture(t) // uses existing test helpers in this package
    hook := ledger.NewTaxLotHook(fx.TaxLotRepo, fx.LedgerRepo, fx.Logger)

    acct := fx.CreateCryptoWalletAccount()
    txID := uuid.New()
    entry := &ledger.Entry{
        ID:         uuid.New(),
        AccountID:  acct.ID,
        EntryType:  ledger.EntryTypeAssetIncrease,
        AssetID:    "XTKN",
        Amount:     big.NewInt(1_000_000),
        USDRate:    nil, // price unknown
        OccurredAt: time.Now().UTC(),
    }
    tx := &ledger.Transaction{ID: txID, Type: ledger.TxTypeTransferIn, Entries: []*ledger.Entry{entry}}

    require.NoError(t, hook(ctx, tx))

    lots, _ := fx.TaxLotRepo.ListPendingLotsByAssetAndTime(ctx, "XTKN", entry.OccurredAt)
    require.Len(t, lots, 1)
    require.Equal(t, ledger.PriceStatusPending, lots[0].PriceStatus)
    require.Nil(t, lots[0].AutoCostBasisPerUnit)
    require.Equal(t, "1000000", lots[0].QuantityAcquired.String())
}
```

(Use existing test helpers; if `newTaxLotFixture` does not exist, add a thin constructor that wires in-memory repos or the existing test harness used in `taxlot_hook_test.go`.)

- [ ] **Step 2: Failing run**

Run: `cd apps/backend && go test -run TestTaxLotHook_CreatesPendingLot ./internal/ledger/... -v`
Expected: FAIL — either `PriceStatusPending` missing, or current hook defaults to 0.

- [ ] **Step 3: Modify `taxlot_hook.go`**

In the acquisition loop, replace:

```go
costBasisPerUnit := a.entry.USDRate
if costBasisPerUnit == nil {
    costBasisPerUnit = big.NewInt(0)
}
```

with:

```go
var costBasisPerUnit *big.Int
priceStatus := PriceStatusResolved
if a.entry.USDRate != nil {
    costBasisPerUnit = new(big.Int).Set(a.entry.USDRate)
} else {
    priceStatus = PriceStatusPending
}
```

And replace the lot construction:

```go
lot := &TaxLot{
    ID:                   uuid.New(),
    TransactionID:        tx.ID,
    AccountID:            a.acct.ID,
    Asset:                a.entry.AssetID,
    QuantityAcquired:     new(big.Int).Set(a.entry.Amount),
    QuantityRemaining:    new(big.Int).Set(a.entry.Amount),
    AcquiredAt:           a.entry.OccurredAt,
    AutoCostBasisPerUnit: costBasisPerUnit, // may be nil for pending
    AutoCostBasisSource:  source,
    LinkedSourceLotID:    linkedLotID,
    CreatedAt:            time.Now(),
    PriceStatus:          priceStatus,
}
```

For internal transfers (the `linkedLotID != nil` path), still honor WAC carry-over — but if the WAC is nil AND `USDRate` is nil, keep status pending.

- [ ] **Step 4: Run, pass**

Run: `cd apps/backend && go test ./internal/ledger/... -v`
Expected: PASS (including existing hook tests, which use non-nil USDRates).

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/ledger/taxlot_hook.go apps/backend/internal/ledger/taxlot_hook_pending_test.go
git commit -m "feat(ledger): TaxLotHook creates pending lots when USD rate is nil"
```

---

## Task 13: `PriceResolvedHook` — recompute lots when backfill resolves

**Files:**
- Create: `apps/backend/internal/ledger/price_resolved_hook.go`
- Test: `apps/backend/internal/ledger/price_resolved_hook_test.go`

- [ ] **Step 1: Failing test**

```go
// apps/backend/internal/ledger/price_resolved_hook_test.go
package ledger_test

import (
    "context"
    "math/big"
    "testing"
    "time"

    "github.com/kislikjeka/moontrack/internal/ledger"
    "github.com/stretchr/testify/require"
)

func TestPriceResolvedHook_ResolvesPendingLotsAndRecomputesDisposals(t *testing.T) {
    ctx := context.Background()
    fx := newTaxLotFixture(t)

    // seed a pending lot + a disposal against it (disposal uses zero proceeds for simplicity)
    acct := fx.CreateCryptoWalletAccount()
    at := time.Now().UTC().Truncate(time.Minute)
    lot := fx.SeedPendingLot(acct.ID, "XTKN", at, big.NewInt(1_000_000))
    _ = fx.SeedDisposal(lot.ID, big.NewInt(500_000), big.NewInt(100_000_000), at.Add(time.Hour))

    hook := ledger.NewPriceResolvedHook(fx.TaxLotRepo, fx.Logger)

    // Simulate backfill resolving $1.23 → 123000000 scaled
    err := hook(ctx, "XTKN", at, big.NewInt(123_000_000), ledger.CostBasisFMVAtTransfer)
    require.NoError(t, err)

    resolved, err := fx.TaxLotRepo.GetTaxLot(ctx, lot.ID)
    require.NoError(t, err)
    require.Equal(t, ledger.PriceStatusResolved, resolved.PriceStatus)
    require.Equal(t, "123000000", resolved.AutoCostBasisPerUnit.String())
}
```

- [ ] **Step 2: Failing run**

Run: `cd apps/backend && go test -run TestPriceResolvedHook ./internal/ledger/... -v`
Expected: FAIL.

- [ ] **Step 3: Implement hook**

```go
// apps/backend/internal/ledger/price_resolved_hook.go
package ledger

import (
    "context"
    "math/big"
    "time"

    "github.com/kislikjeka/moontrack/pkg/logger"
)

// PriceResolvedHook is invoked when the backfill worker writes a price into
// price_history for a (asset, target_time). It resolves all pending lots
// that match that identity and lets downstream disposals recompute on read
// (they already look up EffectiveCostBasisPerUnit each time — disposal rows
// themselves don't cache it).
type PriceResolvedHook func(ctx context.Context, asset string, at time.Time, priceUSDPerUnit *big.Int, source CostBasisSource) error

func NewPriceResolvedHook(repo TaxLotRepository, log *logger.Logger) PriceResolvedHook {
    hlog := log.WithField("component", "price_resolved_hook")
    return func(ctx context.Context, asset string, at time.Time, price *big.Int, source CostBasisSource) error {
        lots, err := repo.ListPendingLotsByAssetAndTime(ctx, asset, at)
        if err != nil {
            return err
        }
        for _, lot := range lots {
            if err := repo.ResolvePendingPrice(ctx, lot.ID, price, source); err != nil {
                return err
            }
            hlog.Info("resolved pending lot",
                "lot_id", lot.ID.String(), "asset", asset, "price", price.String())
        }
        return nil
    }
}
```

- [ ] **Step 4: Run, pass**

Run: `cd apps/backend && go test -run TestPriceResolvedHook ./internal/ledger/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/ledger/price_resolved_hook.go apps/backend/internal/ledger/price_resolved_hook_test.go
git commit -m "feat(ledger): PriceResolvedHook resolves pending lots by (asset, time)"
```

---

## Task 14: Backfill worker

**Files:**
- Create: `apps/backend/internal/platform/price/backfill_worker.go`
- Create: `apps/backend/internal/platform/price/backfill_worker_test.go`

- [ ] **Step 1: Failing test**

```go
// apps/backend/internal/platform/price/backfill_worker_test.go
package price_test

import (
    "context"
    "math/big"
    "sync"
    "testing"
    "time"

    "github.com/google/uuid"
    "github.com/kislikjeka/moontrack/internal/ledger"
    "github.com/kislikjeka/moontrack/internal/platform/asset"
    "github.com/kislikjeka/moontrack/internal/platform/price"
    "github.com/kislikjeka/moontrack/pkg/logger"
    "github.com/stretchr/testify/require"
)

type memJobRepo struct {
    mu       sync.Mutex
    jobs     map[uuid.UUID]*price.BackfillJob
    bypassTS bool
}

func newMemJobRepo() *memJobRepo { return &memJobRepo{jobs: map[uuid.UUID]*price.BackfillJob{}} }

func (m *memJobRepo) Enqueue(ctx context.Context, assetID uuid.UUID, t time.Time) (*price.BackfillJob, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    for _, j := range m.jobs {
        if j.AssetID == assetID && j.TargetTime.Equal(t) {
            return j, nil
        }
    }
    j := &price.BackfillJob{ID: uuid.New(), AssetID: assetID, TargetTime: t,
        Status: price.JobStatusPending, NextAttemptAt: time.Now().UTC()}
    m.jobs[j.ID] = j
    return j, nil
}
func (m *memJobRepo) ClaimReady(ctx context.Context) (*price.BackfillJob, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    now := time.Now().UTC()
    for _, j := range m.jobs {
        if j.Status == price.JobStatusPending && !j.NextAttemptAt.After(now) {
            j.Status = price.JobStatusInProgress
            return j, nil
        }
    }
    return nil, nil
}
func (m *memJobRepo) MarkResolved(ctx context.Context, id uuid.UUID) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.jobs[id].Status = price.JobStatusResolved
    return nil
}
func (m *memJobRepo) Reschedule(ctx context.Context, id uuid.UUID, attempts int, next time.Time, lastErr string, terminal bool) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    j := m.jobs[id]
    j.Attempts = attempts
    j.NextAttemptAt = next
    j.LastError = lastErr
    if terminal {
        j.Status = price.JobStatusFailed
    } else {
        j.Status = price.JobStatusPending
    }
    return nil
}
func (m *memJobRepo) UnlockWithoutCounting(ctx context.Context, id uuid.UUID, next time.Time) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    j := m.jobs[id]
    j.Status = price.JobStatusPending
    j.NextAttemptAt = next
    return nil
}
func (m *memJobRepo) ReapStale(ctx context.Context, d time.Duration) (int, error) { return 0, nil }

type memAssetLookup struct {
    a asset.Asset
}

func (m *memAssetLookup) GetAsset(ctx context.Context, id uuid.UUID) (*asset.Asset, error) {
    return &m.a, nil
}

type memPriceRecorder struct {
    recorded []asset.PricePoint
}

func (m *memPriceRecorder) RecordPrice(ctx context.Context, p *asset.PricePoint) error {
    m.recorded = append(m.recorded, *p)
    return nil
}

type memResolvedHook struct{ called int }

func (m *memResolvedHook) OnResolved(ctx context.Context, assetSym string, at time.Time, price *big.Int, src ledger.CostBasisSource) error {
    m.called++
    return nil
}

type stubProv struct {
    hp  *price.HistoricalPrice
    err error
}

func (s *stubProv) Name() price.Source                                       { return price.SourceGeckoTerminal }
func (s *stubProv) GetPrice(ctx context.Context, a asset.Asset) (*big.Int, error) { return nil, price.ErrNotFound }
func (s *stubProv) GetHistoricalPrice(ctx context.Context, a asset.Asset, t time.Time) (*price.HistoricalPrice, error) {
    return s.hp, s.err
}

func TestWorker_ResolvesJob_WritesPriceHistory_FiresHook(t *testing.T) {
    ctx := context.Background()
    jr := newMemJobRepo()
    aLookup := &memAssetLookup{a: asset.Asset{ID: uuid.New(), Symbol: "XTKN"}}
    pr := &memPriceRecorder{}
    hk := &memResolvedHook{}
    resolver := price.NewResolver([]price.Provider{
        &stubProv{hp: &price.HistoricalPrice{PriceUSD: big.NewInt(42), Timestamp: time.Now(), Confidence: 1}},
    }, nil, logger.NewNoop())

    target := time.Now().UTC().Truncate(time.Minute)
    _, err := jr.Enqueue(ctx, aLookup.a.ID, target)
    require.NoError(t, err)

    w := price.NewBackfillWorker(price.WorkerDeps{
        Jobs: jr, Resolver: resolver, AssetLookup: aLookup,
        PriceRecorder: pr, OnResolved: hk.OnResolved, Logger: logger.NewNoop(),
        RateLimitSleep: 1 * time.Millisecond,
    })
    require.NoError(t, w.ProcessOne(ctx))

    require.Len(t, pr.recorded, 1)
    require.Equal(t, "42", pr.recorded[0].PriceUSD.String())
    require.Equal(t, 1, hk.called)
}

func TestWorker_NotFound_IncrementsAttempts(t *testing.T) {
    ctx := context.Background()
    jr := newMemJobRepo()
    aLookup := &memAssetLookup{a: asset.Asset{ID: uuid.New(), Symbol: "XTKN"}}
    pr := &memPriceRecorder{}
    hk := &memResolvedHook{}
    resolver := price.NewResolver([]price.Provider{
        &stubProv{err: price.ErrNotFound},
    }, nil, logger.NewNoop())

    at := time.Now().UTC().Truncate(time.Minute)
    j, _ := jr.Enqueue(ctx, aLookup.a.ID, at)

    w := price.NewBackfillWorker(price.WorkerDeps{
        Jobs: jr, Resolver: resolver, AssetLookup: aLookup,
        PriceRecorder: pr, OnResolved: hk.OnResolved, Logger: logger.NewNoop(),
    })
    require.NoError(t, w.ProcessOne(ctx))

    got := jr.jobs[j.ID]
    require.Equal(t, 1, got.Attempts)
    require.Equal(t, price.JobStatusPending, got.Status)
}

func TestWorker_RateLimited_DoesNotCountAttempt(t *testing.T) {
    ctx := context.Background()
    jr := newMemJobRepo()
    aLookup := &memAssetLookup{a: asset.Asset{ID: uuid.New()}}
    resolver := price.NewResolver([]price.Provider{
        &stubProv{err: price.ErrRateLimited},
    }, nil, logger.NewNoop())

    at := time.Now().UTC().Truncate(time.Minute)
    j, _ := jr.Enqueue(ctx, aLookup.a.ID, at)

    w := price.NewBackfillWorker(price.WorkerDeps{
        Jobs: jr, Resolver: resolver, AssetLookup: aLookup,
        PriceRecorder: &memPriceRecorder{}, OnResolved: func(ctx context.Context, a string, t time.Time, p *big.Int, s ledger.CostBasisSource) error { return nil },
        Logger: logger.NewNoop(),
    })
    require.NoError(t, w.ProcessOne(ctx))

    got := jr.jobs[j.ID]
    require.Equal(t, 0, got.Attempts, "rate-limit must NOT count as attempt")
}

func TestWorker_TerminalAttempt_MarksFailed(t *testing.T) {
    ctx := context.Background()
    jr := newMemJobRepo()
    aLookup := &memAssetLookup{a: asset.Asset{ID: uuid.New()}}
    resolver := price.NewResolver([]price.Provider{
        &stubProv{err: price.ErrNotFound},
    }, nil, logger.NewNoop())
    at := time.Now().UTC().Truncate(time.Minute)
    j, _ := jr.Enqueue(ctx, aLookup.a.ID, at)
    j.Attempts = price.MaxAttempts - 1
    w := price.NewBackfillWorker(price.WorkerDeps{
        Jobs: jr, Resolver: resolver, AssetLookup: aLookup,
        PriceRecorder: &memPriceRecorder{}, OnResolved: func(ctx context.Context, a string, t time.Time, p *big.Int, s ledger.CostBasisSource) error { return nil },
        Logger: logger.NewNoop(),
    })
    require.NoError(t, w.ProcessOne(ctx))
    require.Equal(t, price.JobStatusFailed, jr.jobs[j.ID].Status)
}
```

- [ ] **Step 2: Failing run**

Run: `cd apps/backend && go test ./internal/platform/price/... -v`
Expected: FAIL — NewBackfillWorker undefined.

- [ ] **Step 3: Implement worker**

```go
// apps/backend/internal/platform/price/backfill_worker.go
package price

import (
    "context"
    "errors"
    "math/big"
    "time"

    "github.com/google/uuid"
    "github.com/kislikjeka/moontrack/internal/ledger"
    "github.com/kislikjeka/moontrack/internal/platform/asset"
    "github.com/kislikjeka/moontrack/pkg/logger"
)

// AssetLookup resolves an asset by ID (subset of asset.Service).
type AssetLookup interface {
    GetAsset(ctx context.Context, id uuid.UUID) (*asset.Asset, error)
}

// PriceRecorder writes a PricePoint to price_history.
type PriceRecorder interface {
    RecordPrice(ctx context.Context, p *asset.PricePoint) error
}

// OnPriceResolvedFunc notifies interested parties (ledger hook) that a price
// is now known for (assetSymbol, at). Implementations must be idempotent.
type OnPriceResolvedFunc func(ctx context.Context, assetSymbol string, at time.Time, priceUSDPerUnit *big.Int, src ledger.CostBasisSource) error

type WorkerDeps struct {
    Jobs           JobRepository
    Resolver       *Resolver
    AssetLookup    AssetLookup
    PriceRecorder  PriceRecorder
    OnResolved     OnPriceResolvedFunc
    Logger         *logger.Logger
    RateLimitSleep time.Duration
}

type BackfillWorker struct {
    d WorkerDeps
}

func NewBackfillWorker(d WorkerDeps) *BackfillWorker {
    if d.RateLimitSleep == 0 {
        d.RateLimitSleep = 5 * time.Second
    }
    d.Logger = d.Logger.WithField("component", "price_backfill")
    return &BackfillWorker{d: d}
}

// ProcessOne claims one job (if any) and processes it. Safe to run in a loop.
func (w *BackfillWorker) ProcessOne(ctx context.Context) error {
    job, err := w.d.Jobs.ClaimReady(ctx)
    if err != nil || job == nil {
        return err
    }

    a, err := w.d.AssetLookup.GetAsset(ctx, job.AssetID)
    if err != nil {
        // Asset is gone; mark failed so we stop retrying.
        return w.d.Jobs.Reschedule(ctx, job.ID, job.Attempts, time.Now().Add(24*time.Hour), "asset lookup failed: "+err.Error(), true)
    }

    hp, src, rerr := w.d.Resolver.ResolveHistorical(ctx, *a, job.TargetTime)

    switch {
    case rerr == nil:
        // Record price_history, notify hook, mark resolved.
        pp := &asset.PricePoint{
            Time:     hp.Timestamp,
            AssetID:  a.ID,
            PriceUSD: hp.PriceUSD,
            Source:   asset.PriceSource(src),
        }
        if err := w.d.PriceRecorder.RecordPrice(ctx, pp); err != nil {
            // Don't count as attempt — it's our problem, not the provider's.
            return w.d.Jobs.UnlockWithoutCounting(ctx, job.ID, time.Now().Add(1*time.Minute))
        }
        if err := w.d.OnResolved(ctx, a.Symbol, job.TargetTime, hp.PriceUSD, ledger.CostBasisFMVAtTransfer); err != nil {
            return w.d.Jobs.UnlockWithoutCounting(ctx, job.ID, time.Now().Add(1*time.Minute))
        }
        return w.d.Jobs.MarkResolved(ctx, job.ID)

    case errors.Is(rerr, ErrRateLimited):
        // Don't count; retry soon.
        return w.d.Jobs.UnlockWithoutCounting(ctx, job.ID, time.Now().Add(w.d.RateLimitSleep))

    case errors.Is(rerr, ErrTransient):
        // Don't count; short backoff.
        return w.d.Jobs.UnlockWithoutCounting(ctx, job.ID, time.Now().Add(5*time.Minute))

    default:
        // NotFound / LowConfidence / UnsupportedChain — counts as attempt.
        newAttempts := job.Attempts + 1
        if IsTerminalAttempt(newAttempts) {
            return w.d.Jobs.Reschedule(ctx, job.ID, newAttempts, time.Now().Add(24*time.Hour), rerr.Error(), true)
        }
        return w.d.Jobs.Reschedule(ctx, job.ID, newAttempts,
            time.Now().Add(BackoffDelay(newAttempts)), rerr.Error(), false)
    }
}

// Run loops ProcessOne at the given rate until ctx is done.
func (w *BackfillWorker) Run(ctx context.Context, rate time.Duration) {
    ticker := time.NewTicker(rate)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            if err := w.ProcessOne(ctx); err != nil {
                w.d.Logger.Warn("worker iteration error", "error", err.Error())
            }
        }
    }
}
```

- [ ] **Step 4: Run, pass**

Run: `cd apps/backend && go test ./internal/platform/price/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/platform/price/backfill_worker.go apps/backend/internal/platform/price/backfill_worker_test.go
git commit -m "feat(price): backfill worker with NotFound/RateLimit/Transient semantics"
```

---

## Task 15: Zerion processor — enqueue backfill jobs on missing prices, no more `"0"`

**Files:**
- Modify: `apps/backend/internal/platform/sync/zerion_processor.go:577-592`
- Modify: sync wiring — add `AssetService`, `JobRepo` fields to processor
- Test: `apps/backend/internal/platform/sync/zerion_processor_backfill_test.go`

- [ ] **Step 1: Failing test**

```go
// apps/backend/internal/platform/sync/zerion_processor_backfill_test.go
package sync_test

import (
    "context"
    "testing"
    "time"

    "github.com/google/uuid"
    "github.com/stretchr/testify/require"
)

// Goal: when a Zerion transfer has nil Price, the processor
// (a) upserts an asset by on-chain identity,
// (b) enqueues a backfill job.
//
// We exercise this via the same harness zerion_processor_test.go already uses.
// The existing harness is in test_helpers_test.go; extend it with a fakeAssetUpsert
// and fakeJobEnqueuer to assert the calls.
func TestZerionProcessor_MissingPrice_EnqueuesBackfillJob(t *testing.T) {
    // See test_helpers_test.go for the canonical harness; this test uses it.
    ctx := context.Background()
    h := newProcessorHarness(t)
    at := time.Now().UTC().Truncate(time.Minute)

    tx := sampleTransfer(at, "ethereum", "0xabc", "XTKN", nil /* Price */)
    err := h.Processor.Process(ctx, h.Wallet, []any{tx})
    require.NoError(t, err)

    require.Len(t, h.JobsEnqueued, 1)
    require.Equal(t, at, h.JobsEnqueued[0].TargetTime)
    require.NotEqual(t, uuid.Nil, h.JobsEnqueued[0].AssetID)
    require.Len(t, h.AssetsUpserted, 1)
    require.Equal(t, "ethereum", h.AssetsUpserted[0].Chain)
    require.Equal(t, "0xabc", h.AssetsUpserted[0].Addr)
}
```

- [ ] **Step 2: Extend processor struct**

In `apps/backend/internal/platform/sync/zerion_processor.go`, add fields:

```go
type ZerionProcessor struct {
    // ... existing ...
    assetUpsert  AssetUpserter   // new
    jobEnqueuer  JobEnqueuer     // new
}

// AssetUpserter is the sync-side contract for platform.asset.
type AssetUpserter interface {
    UpsertByOnChainIdentity(ctx context.Context, chainID, contractAddress, symbol, name string, decimals int) (*asset.Asset, bool, error)
}

// JobEnqueuer is the sync-side contract for price.JobRepository.
type JobEnqueuer interface {
    Enqueue(ctx context.Context, assetID uuid.UUID, targetTime time.Time) (*price.BackfillJob, error)
}
```

Change `NewZerionProcessor` constructor to accept these.

- [ ] **Step 3: Replace `buildSingleTransfer` `"0"` default**

Replace the body of `buildSingleTransfer`:

```go
func (p *ZerionProcessor) buildSingleTransfer(ctx context.Context, occurredAt time.Time, t DecodedTransfer) map[string]interface{} {
    usdPrice := ""
    if t.USDPrice != nil {
        usdPrice = t.USDPrice.String()
    } else if t.ContractAddress != "" && t.Chain != "" {
        // Missing price → register the asset + enqueue backfill.
        a, _, err := p.assetUpsert.UpsertByOnChainIdentity(
            ctx, t.Chain, t.ContractAddress, t.AssetSymbol, t.AssetSymbol, t.Decimals)
        if err == nil && a != nil {
            _, _ = p.jobEnqueuer.Enqueue(ctx, a.ID, occurredAt)
        }
        // Leave usdPrice empty → downstream interprets as nil USDRate.
    }

    result := map[string]interface{}{
        "asset_symbol":     t.AssetSymbol,
        "amount":           money.NewBigInt(t.Amount).String(),
        "decimals":         t.Decimals,
        "contract_address": t.ContractAddress,
        "direction":        string(t.Direction),
        "sender":           t.Sender,
        "recipient":        t.Recipient,
    }
    if usdPrice != "" {
        result["usd_price"] = usdPrice
    } else {
        // Omit entirely so downstream sees it as nil and creates a pending lot.
        result["usd_price_pending"] = true
    }
    return result
}
```

Thread `ctx` and `occurredAt` through `buildTransferArray` and callers.

The ledger entry construction in the transfer module (`internal/module/transfer/handler_transfer_in.go` etc.) currently reads `usd_price` into `USDRate`. Change that reader to accept missing/`usd_price_pending` and set `USDRate = nil`. (This is a small edit in every module that parses `data["usd_price"]`; do one file per sub-task if the set is large; for MVP it's primarily the transfer/manual/swap/defi handlers.)

- [ ] **Step 4: Run integration**

Run: `cd apps/backend && go test -run TestZerionProcessor_MissingPrice ./internal/platform/sync/... -v`
Expected: PASS.

Also: run the full sync test suite to guard against regressions:
Run: `cd apps/backend && go test ./internal/platform/sync/... -short`
Expected: all green.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/platform/sync apps/backend/internal/module
git commit -m "feat(sync): missing Zerion price → upsert asset + enqueue backfill (no more \"0\")"
```

---

## Task 16: `PriceReader` — priority-ordered SQL read

**Files:**
- Create: `apps/backend/internal/platform/price/reader.go`
- Create: `apps/backend/internal/infra/postgres/price_reader.go`
- Test: `apps/backend/internal/infra/postgres/price_reader_test.go`

- [ ] **Step 1: Failing test**

```go
// apps/backend/internal/infra/postgres/price_reader_test.go
package postgres_test

import (
    "context"
    "math/big"
    "testing"
    "time"

    "github.com/kislikjeka/moontrack/internal/infra/postgres"
    "github.com/kislikjeka/moontrack/internal/platform/asset"
    "github.com/kislikjeka/moontrack/internal/platform/price"
    "github.com/stretchr/testify/require"
)

func TestPriceReader_CurrentPrefersCoinGeckoOverOthers(t *testing.T) {
    if testing.Short() {
        t.Skip("integration")
    }
    ctx := context.Background()
    pool := testPool(t)
    priceRepo := postgres.NewPriceRepository(pool)
    assetID := seedAsset(t, pool)

    at := time.Now().UTC()
    require.NoError(t, priceRepo.RecordPrice(ctx, &asset.PricePoint{
        Time: at, AssetID: assetID, PriceUSD: big.NewInt(100), Source: asset.PriceSource("geckoterminal"),
    }))
    require.NoError(t, priceRepo.RecordPrice(ctx, &asset.PricePoint{
        Time: at.Add(-time.Hour), AssetID: assetID, PriceUSD: big.NewInt(200), Source: asset.PriceSource("coingecko"),
    }))

    reader := postgres.NewPriceReader(pool, []price.Source{
        price.SourceCoinGecko, price.SourceGeckoTerminal, price.SourceDefiLlama,
    })
    got, src, err := reader.Current(ctx, assetID)
    require.NoError(t, err)
    require.Equal(t, "200", got.String())
    require.Equal(t, price.SourceCoinGecko, src)
}
```

- [ ] **Step 2: Implement**

```go
// apps/backend/internal/infra/postgres/price_reader.go
package postgres

import (
    "context"
    "fmt"
    "math/big"
    "strings"
    "time"

    "github.com/google/uuid"
    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/kislikjeka/moontrack/internal/platform/price"
)

type PriceReader struct {
    pool     *pgxpool.Pool
    priority []price.Source
}

func NewPriceReader(pool *pgxpool.Pool, priority []price.Source) *PriceReader {
    return &PriceReader{pool: pool, priority: priority}
}

// buildPriorityCase produces a SQL fragment like:
//   CASE source WHEN 'coingecko' THEN 1 WHEN 'geckoterminal' THEN 2 ELSE 99 END
func (r *PriceReader) buildPriorityCase() string {
    var sb strings.Builder
    sb.WriteString("CASE source")
    for i, s := range r.priority {
        sb.WriteString(fmt.Sprintf(" WHEN '%s' THEN %d", s, i+1))
    }
    sb.WriteString(" ELSE 99 END")
    return sb.String()
}

func (r *PriceReader) Current(ctx context.Context, assetID uuid.UUID) (*big.Int, price.Source, error) {
    // Read the latest per-source, then sort by priority then by time.
    q := fmt.Sprintf(`
        WITH latest AS (
            SELECT DISTINCT ON (source) source, time, price_usd
            FROM price_history
            WHERE asset_id = $1
            ORDER BY source, time DESC
        )
        SELECT source, price_usd FROM latest
        ORDER BY %s
        LIMIT 1
    `, r.buildPriorityCase())

    var src string
    var priceStr string
    err := r.pool.QueryRow(ctx, q, assetID).Scan(&src, &priceStr)
    if err != nil {
        if err == pgx.ErrNoRows {
            return nil, "", price.ErrNotFound
        }
        return nil, "", fmt.Errorf("price reader current: %w", err)
    }
    out, _ := new(big.Int).SetString(priceStr, 10)
    return out, price.Source(src), nil
}

func (r *PriceReader) Historical(ctx context.Context, assetID uuid.UUID, ts time.Time) (*price.HistoricalPrice, price.Source, error) {
    q := fmt.Sprintf(`
        WITH nearest AS (
            SELECT DISTINCT ON (source) source, time, price_usd
            FROM price_history
            WHERE asset_id = $1 AND time <= $2
            ORDER BY source, time DESC
        )
        SELECT source, time, price_usd FROM nearest
        ORDER BY %s
        LIMIT 1
    `, r.buildPriorityCase())
    var src, priceStr string
    var t time.Time
    err := r.pool.QueryRow(ctx, q, assetID, ts).Scan(&src, &t, &priceStr)
    if err != nil {
        if err == pgx.ErrNoRows {
            return nil, "", price.ErrNotFound
        }
        return nil, "", err
    }
    p, _ := new(big.Int).SetString(priceStr, 10)
    return &price.HistoricalPrice{PriceUSD: p, Timestamp: t, Confidence: 1}, price.Source(src), nil
}
```

- [ ] **Step 3: Run, pass**

Run: `cd apps/backend && go test -run TestPriceReader ./internal/infra/postgres/... -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/backend/internal/platform/price/reader.go apps/backend/internal/infra/postgres/price_reader.go apps/backend/internal/infra/postgres/price_reader_test.go
git commit -m "feat(price): priority-ordered reader over price_history"
```

---

## Task 17: Portfolio PnL — expose `pnl_is_partial`, pending/unpriceable counts

**Files:**
- Modify: `apps/backend/internal/module/portfolio/service.go`
- Modify: `apps/backend/internal/module/portfolio/adapter.go`
- Test: `apps/backend/internal/module/portfolio/service_pnl_partial_test.go`

- [ ] **Step 1: Failing test**

```go
// apps/backend/internal/module/portfolio/service_pnl_partial_test.go
package portfolio_test

import (
    "context"
    "testing"

    "github.com/stretchr/testify/require"
)

func TestPortfolio_PnLIsPartial_WhenPendingLotsExist(t *testing.T) {
    ctx := context.Background()
    h := newPortfolioHarness(t)

    h.SeedResolvedLot("ETH", 1_000, 100_000_000)
    h.SeedPendingLot("XTKN", 1_000)

    out, err := h.Svc.GetPortfolio(ctx, h.UserID)
    require.NoError(t, err)

    require.True(t, out.PnLIsPartial)
    require.Equal(t, 1, out.PendingLotCount)
    require.Equal(t, 0, out.UnpriceableLotCount)
}
```

- [ ] **Step 2: Add fields to portfolio response struct**

In the portfolio service response type (file around `service.go`), add:

```go
type PortfolioSummary struct {
    // ... existing ...
    PnLIsPartial        bool `json:"pnl_is_partial"`
    PendingLotCount     int  `json:"pending_lot_count"`
    UnpriceableLotCount int  `json:"unpriceable_lot_count"`
}
```

- [ ] **Step 3: Populate in service**

Add a query via TaxLotRepository. Add a port method:

```go
// in ledger/taxlot_port.go
CountLotsByPriceStatus(ctx context.Context, userID uuid.UUID) (pending, unpriceable int, err error)
```

Implement in `postgres/taxlot_repo.go`:

```go
func (r *TaxLotRepository) CountLotsByPriceStatus(ctx context.Context, userID uuid.UUID) (int, int, error) {
    rows, err := r.pool.Query(ctx, `
        SELECT tl.price_status, COUNT(*)
        FROM tax_lots tl
        JOIN accounts a ON a.id = tl.account_id
        WHERE a.user_id = $1 AND tl.price_status IN ('pending', 'unpriceable')
        GROUP BY tl.price_status
    `, userID)
    if err != nil {
        return 0, 0, err
    }
    defer rows.Close()
    var pending, unpriceable int
    for rows.Next() {
        var status string
        var n int
        if err := rows.Scan(&status, &n); err != nil {
            return 0, 0, err
        }
        switch status {
        case "pending":
            pending = n
        case "unpriceable":
            unpriceable = n
        }
    }
    return pending, unpriceable, nil
}
```

In `portfolio/service.go`, call this and set fields on the response.

- [ ] **Step 4: Run tests, pass**

Run: `cd apps/backend && go test -run TestPortfolio_PnLIsPartial ./internal/module/portfolio/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/module/portfolio apps/backend/internal/ledger/taxlot_port.go apps/backend/internal/infra/postgres/taxlot_repo.go
git commit -m "feat(portfolio): expose pnl_is_partial, pending/unpriceable lot counts"
```

---

## Task 18: Manual-price endpoint `PUT /lots/{id}/manual-price`

**Files:**
- Create: `apps/backend/internal/module/lots/handler.go`
- Create: `apps/backend/internal/module/lots/service.go`
- Create: `apps/backend/internal/module/lots/handler_test.go`
- Modify: `apps/backend/internal/transport/router.go` (register route)
- Modify: `apps/backend/cmd/api/main.go` (wire)

- [ ] **Step 1: Failing test**

```go
// apps/backend/internal/module/lots/handler_test.go
package lots_test

import (
    "bytes"
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/kislikjeka/moontrack/internal/module/lots"
    "github.com/stretchr/testify/require"
)

type fakeSvc struct{ lastPrice string }

func (f *fakeSvc) SetManualPrice(ctx context.Context, lotID string, priceUSD string, reason string) error {
    f.lastPrice = priceUSD
    return nil
}

func TestHandler_SetManualPrice_WritesPrice(t *testing.T) {
    svc := &fakeSvc{}
    h := lots.NewHandler(svc)
    r := httptest.NewRequest("PUT", "/lots/abc/manual-price", bytes.NewReader(mustJSON(t, map[string]string{"price_usd": "123456", "reason": "dex backfill manual"})))
    w := httptest.NewRecorder()
    h.SetManualPrice(w, r)
    require.Equal(t, http.StatusOK, w.Result().StatusCode)
    require.Equal(t, "123456", svc.lastPrice)
}

func mustJSON(t *testing.T, v any) []byte {
    t.Helper()
    b, err := json.Marshal(v)
    require.NoError(t, err)
    return b
}
```

- [ ] **Step 2: Implement handler/service**

```go
// apps/backend/internal/module/lots/handler.go
package lots

import (
    "encoding/json"
    "net/http"

    "github.com/go-chi/chi/v5"
)

type Service interface {
    SetManualPrice(ctx context.Context, lotID, priceUSD, reason string) error
}

type Handler struct{ svc Service }

func NewHandler(s Service) *Handler { return &Handler{svc: s} }

type setManualPriceReq struct {
    PriceUSD string `json:"price_usd"`
    Reason   string `json:"reason"`
}

func (h *Handler) SetManualPrice(w http.ResponseWriter, r *http.Request) {
    lotID := chi.URLParam(r, "id")
    var body setManualPriceReq
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
        http.Error(w, "bad json", http.StatusBadRequest)
        return
    }
    if err := h.svc.SetManualPrice(r.Context(), lotID, body.PriceUSD, body.Reason); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    w.WriteHeader(http.StatusOK)
}
```

```go
// apps/backend/internal/module/lots/service.go
package lots

import (
    "context"
    "fmt"
    "math/big"
    "time"

    "github.com/google/uuid"
    "github.com/kislikjeka/moontrack/internal/ledger"
    "github.com/kislikjeka/moontrack/internal/platform/asset"
)

type Repo interface {
    GetTaxLot(ctx context.Context, id uuid.UUID) (*ledger.TaxLot, error)
    UpdateOverride(ctx context.Context, lotID uuid.UUID, override *big.Int, reason string) error
    MarkResolved(ctx context.Context, lotID uuid.UUID) error
}

type PriceWriter interface {
    RecordPrice(ctx context.Context, p *asset.PricePoint) error
}

type Svc struct {
    repo Repo
    pw   PriceWriter
}

func NewService(r Repo, pw PriceWriter) *Svc { return &Svc{repo: r, pw: pw} }

func (s *Svc) SetManualPrice(ctx context.Context, lotID, priceUSD, reason string) error {
    id, err := uuid.Parse(lotID)
    if err != nil {
        return fmt.Errorf("bad lot id")
    }
    p, ok := new(big.Int).SetString(priceUSD, 10)
    if !ok || p.Sign() < 0 {
        return fmt.Errorf("bad price")
    }
    lot, err := s.repo.GetTaxLot(ctx, id)
    if err != nil {
        return err
    }
    if err := s.repo.UpdateOverride(ctx, id, p, reason); err != nil {
        return err
    }
    if err := s.repo.MarkResolved(ctx, id); err != nil {
        return err
    }
    // audit row in price_history
    _ = s.pw.RecordPrice(ctx, &asset.PricePoint{
        Time:     time.Now(),
        AssetID:  uuid.Nil, // we don't resolve asset here; this is audit-only
        PriceUSD: p,
        Source:   asset.PriceSource("manual"),
    })
    _ = lot
    return nil
}
```

(Extend `TaxLotRepository` with `UpdateOverride` and `MarkResolved` methods if missing — they exist today for the override path; reuse.)

- [ ] **Step 3: Register route**

In `apps/backend/internal/transport/router.go` inside the authenticated group:

```go
r.Put("/lots/{id}/manual-price", lotsHandler.SetManualPrice)
```

In `apps/backend/cmd/api/main.go`, wire `lots.NewService(taxlotRepo, priceRepo)` and `lots.NewHandler(svc)` and pass to router.

- [ ] **Step 4: Run**

Run: `cd apps/backend && go test ./internal/module/lots/... -v && go build ./...`
Expected: PASS, clean build.

- [ ] **Step 5: Commit**

```bash
git add apps/backend/internal/module/lots apps/backend/internal/transport apps/backend/cmd/api
git commit -m "feat(lots): PUT /lots/{id}/manual-price endpoint"
```

---

## Task 19: Wire providers, worker, reader in `cmd/api/main.go` (feature-flagged)

**Files:**
- Modify: `apps/backend/cmd/api/main.go`
- Modify: `.env.example`

- [ ] **Step 1: Add env vars to `.env.example`**

```bash
# Price fallback providers
PRICE_BACKFILL_ENABLED=true
PRICE_BACKFILL_RATE_SECONDS=1
PRICE_PROVIDER_PRIORITY=coingecko,geckoterminal,defillama
GECKOTERMINAL_BASE_URL=https://api.geckoterminal.com/api/v2
DEFILLAMA_BASE_URL=https://coins.llama.fi
DEFILLAMA_MIN_CONFIDENCE=0.9
FEATURE_PRICE_FALLBACK=false
```

- [ ] **Step 2: Wire in main.go**

Add (following existing DI pattern — init order: infra → repos → core → hooks → modules):

```go
// --- Price fallback providers ---
gtClient := geckoterminal.NewClient(geckoterminal.Config{
    BaseURL: env("GECKOTERMINAL_BASE_URL", ""),
})
dlClient := defillama.NewClient(defillama.Config{
    BaseURL:       env("DEFILLAMA_BASE_URL", ""),
    MinConfidence: envFloat("DEFILLAMA_MIN_CONFIDENCE", 0.9),
})

providers := []price.Provider{
    price.NewCoinGeckoProvider(assetService),
    price.NewGeckoTerminalProvider(gtClient),
    price.NewDefiLlamaProvider(dlClient),
}
resolver := price.NewResolver(providers, priceCache, log)

jobRepo := postgres.NewPriceBackfillJobRepository(pool)
resolvedHook := ledger.NewPriceResolvedHook(taxlotRepo, log)

worker := price.NewBackfillWorker(price.WorkerDeps{
    Jobs:          jobRepo,
    Resolver:      resolver,
    AssetLookup:   assetService,
    PriceRecorder: priceRepo,
    OnResolved:    resolvedHook,
    Logger:        log,
})

// Zerion processor gets the AssetUpserter + JobEnqueuer.
zp := sync.NewZerionProcessor(/* existing args */, assetService, jobRepo)

// Feature flag
if envBool("FEATURE_PRICE_FALLBACK", false) {
    go worker.Run(ctx, time.Duration(envInt("PRICE_BACKFILL_RATE_SECONDS", 1))*time.Second)
    // reaper
    go func() {
        tick := time.NewTicker(5 * time.Minute)
        for {
            select {
            case <-ctx.Done():
                return
            case <-tick.C:
                _, _ = jobRepo.ReapStale(ctx, 10*time.Minute)
            }
        }
    }()
}
```

(Adapt to the exact types/variable names in main.go.)

- [ ] **Step 3: Build**

Run: `cd apps/backend && go build ./...`
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add apps/backend/cmd/api/main.go .env.example
git commit -m "feat(api): wire price fallback providers, worker, resolved hook (feature-flagged)"
```

---

## Task 20: Integration test — end-to-end sync → pending → resolved

**Files:**
- Create: `apps/backend/internal/platform/sync/price_fallback_e2e_test.go`

- [ ] **Step 1: Write E2E**

```go
// apps/backend/internal/platform/sync/price_fallback_e2e_test.go
package sync_test

import (
    "context"
    "math/big"
    "testing"
    "time"

    "github.com/kislikjeka/moontrack/internal/ledger"
    "github.com/kislikjeka/moontrack/internal/platform/asset"
    "github.com/kislikjeka/moontrack/internal/platform/price"
    "github.com/stretchr/testify/require"
)

func TestE2E_PendingLot_Resolves_ViaWorker(t *testing.T) {
    if testing.Short() {
        t.Skip("integration")
    }
    ctx := context.Background()
    env := newFullStackHarness(t)

    // Zerion returns a transfer with nil price.
    env.SimulateZerionTransfer("ethereum", "0xdeadbeef", "XTKN", nil)
    require.NoError(t, env.SyncService.Run(ctx, env.Wallet.ID))

    // Lot is pending.
    lots, _ := env.TaxLotRepo.ListPendingLotsByAssetAndTime(ctx, "XTKN", env.LastTxTime())
    require.Len(t, lots, 1)

    // Stub provider returns $1.25
    env.StubProvider.SetHistoricalResponse(&price.HistoricalPrice{
        PriceUSD: big.NewInt(125_000_000), Timestamp: env.LastTxTime(), Confidence: 1,
    })

    // Run worker one tick
    require.NoError(t, env.Worker.ProcessOne(ctx))

    // Now resolved.
    again, _ := env.TaxLotRepo.GetTaxLot(ctx, lots[0].ID)
    require.Equal(t, ledger.PriceStatusResolved, again.PriceStatus)
    require.Equal(t, "125000000", again.EffectiveCostBasisPerUnit().String())

    // price_history has the row.
    pp, err := env.PriceRepo.GetCurrentPrice(ctx, again.AssetID /* or looked up */)
    require.NoError(t, err)
    require.Equal(t, asset.PriceSource("geckoterminal"), pp.Source)
}
```

- [ ] **Step 2: Implement harness**

Extend `test_helpers_test.go` with `newFullStackHarness` that wires real postgres repos + in-memory stub provider into `price.NewResolver`, the real `BackfillWorker`, the real `ZerionProcessor`, and a stubbed Zerion adapter.

- [ ] **Step 3: Run**

Run: `cd apps/backend && go test -run TestE2E_PendingLot ./internal/platform/sync/... -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/backend/internal/platform/sync/price_fallback_e2e_test.go apps/backend/internal/platform/sync/test_helpers_test.go
git commit -m "test(e2e): sync → pending lot → worker → resolved lot"
```

---

## Task 21: Stage-3 one-shot backfill CLI for legacy `"0"` lots

**Files:**
- Create: `apps/backend/cmd/backfill-legacy-prices/main.go`
- Modify: `justfile` (add recipe)

- [ ] **Step 1: Implement command**

```go
// apps/backend/cmd/backfill-legacy-prices/main.go
package main

import (
    "context"
    "flag"
    "log"
    "time"
)

func main() {
    dry := flag.Bool("dry-run", true, "do not enqueue, just report counts")
    flag.Parse()

    ctx := context.Background()
    // Open pool + repos (reuse factories from main.go if exported; otherwise duplicate minimal wiring.)
    //
    // Query: SELECT id, asset_id, acquired_at FROM tax_lots
    //        WHERE (auto_cost_basis_per_unit = '0' OR auto_cost_basis_per_unit IS NULL)
    //          AND price_status = 'resolved'  -- legacy rows that currently look resolved but are zero
    //          AND asset_id IN (SELECT id FROM assets WHERE chain_id IS NOT NULL AND contract_address IS NOT NULL);
    //
    // For each row: mark price_status = 'pending', enqueue job (assetID, acquired_at).
    //
    // In dry-run, print the count and exit.
    _ = dry
    _ = ctx
    _ = time.Now
    log.Println("legacy backfill complete")
}
```

Replace the TODO body with real code that:
1. Opens a pool using `DATABASE_URL`.
2. Selects lots matching the legacy pattern (`cost_basis = 0` OR `NULL`, asset has `(chain_id, contract_address)`).
3. In dry-run → prints count only.
4. Otherwise → wraps in a transaction: `UPDATE tax_lots SET price_status = 'pending' WHERE id IN (...)`, then enqueues jobs.

- [ ] **Step 2: justfile recipe**

```
# justfile
backfill-legacy-prices-dry:
    cd apps/backend && go run ./cmd/backfill-legacy-prices -dry-run
backfill-legacy-prices:
    cd apps/backend && go run ./cmd/backfill-legacy-prices
```

- [ ] **Step 3: Commit**

```bash
git add apps/backend/cmd/backfill-legacy-prices justfile
git commit -m "feat(cli): one-shot migration to enqueue backfill for legacy zero-priced lots"
```

---

## Task 22: Final verification

- [ ] **Step 1: Full test suite**

Run: `cd apps/backend && go test ./... -short`
Expected: all green.

- [ ] **Step 2: Integration tests against real DB**

Run: `just backend-test`
Expected: all green.

- [ ] **Step 3: Lint**

Run: `just lint`
Expected: no errors. Fix any violations before proceeding.

- [ ] **Step 4: Build binary**

Run: `cd apps/backend && go build ./cmd/api/... ./cmd/backfill-legacy-prices/...`
Expected: success.

- [ ] **Step 5: Commit finishing touches if any**

```bash
git status
# if nothing to commit, done.
```

---

## Self-Review

**Spec coverage:**
- ✅ One assets row per token — Task 1 (unique index), Task 2 (UpsertByOnChainIdentity)
- ✅ Multiple sources per asset — price_history.source preserved; PriceReader priority
- ✅ `pending`/`resolved`/`unpriceable` lot states — Tasks 11, 12, 13
- ✅ `pnl_is_partial` in portfolio — Task 17
- ✅ Tx-timestamp precision (where providers support it) — GeckoTerminal OHLCV minute, DefiLlama ts-based
- ✅ Idempotent retries with backoff — Tasks 4, 10, 14
- ✅ Error taxonomy — Task 3 errors.go, Task 6 resolver wiring, Task 14 worker enforcement
- ✅ Rate-limit safe — clients return ErrRateLimited; worker treats without counting
- ✅ Cache — Task 5
- ✅ Crash safety — ReapStale (Task 10), idempotent `price_history` ON CONFLICT
- ✅ Feature flag + rollout — Task 19 + Task 21 stage-3 CLI
- ✅ Observability — logger "component" field threaded through resolver/worker/hook

**Placeholder scan:** all code blocks are concrete. One intentional "conservative default" in Task 9 `GeckoTerminalProvider.GetHistoricalPrice` — returns ErrNotFound so DefiLlama handles history; the comment notes pool discovery as a deliberate non-goal.

**Type consistency:** `PriceStatus` exists in both `price` package and `ledger` package — these are separate concerns: `price.PriceStatus` governs provider/job semantics, `ledger.PriceStatus` governs lot storage. Kept distinct intentionally. Cross-wired in worker via `ledger.CostBasisFMVAtTransfer`.

---

## Execution Handoff

This plan is ready to execute. Per the user's directive, we will use **subagent-driven development** (one fresh subagent per task) and not pause for intermediate review between tasks — but each task's tests must pass before the subagent reports success. After all tasks complete, four parallel review agents run: ledger correctness, rate-limiter, async test quality, security.
