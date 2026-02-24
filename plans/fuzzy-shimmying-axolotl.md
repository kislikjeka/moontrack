# Fix: Auto-Create Zerion Assets & Replace Hardcoded Decimal Lookup

## Context

Tokens synced from Zerion (FLUID, wDINO, aEthWETH, WETH, GM, fUSDC) display wildly incorrect amounts (billions instead of actual values). Root cause: `money.GetDecimals()` is a hardcoded map of 26 assets, defaulting to 8 decimals for everything else. Zerion provides correct decimals in `FungibleInfo.Implementations`, but this data is discarded after sync — no asset records are created.

**Goal**: Auto-create asset records from Zerion data during sync, then use them for decimal resolution everywhere.

## Approach

1. **New `zerion_assets` table** — stores token metadata discovered from Zerion (symbol, name, chain, contract, decimals, icon). Separate from existing `assets` table (which requires CoinGecko ID).
2. **Auto-create during sync** — Collector and Reconciler extract asset metadata from Zerion API responses and upsert into `zerion_assets`.
3. **`DecimalResolver`** — cascading lookup: `assets` table → `zerion_assets` table → hardcoded fallback. In-memory cache.
4. **Replace all 8 call sites** of `money.GetDecimals()`.

## Implementation

### 1. Migration: `zerion_assets` table

`migrations/000021_zerion_assets.up.sql`:
```sql
CREATE TABLE zerion_assets (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    symbol           VARCHAR(50) NOT NULL,
    name             VARCHAR(255) NOT NULL DEFAULT '',
    chain_id         VARCHAR(50) NOT NULL,
    contract_address VARCHAR(255) NOT NULL DEFAULT '',
    decimals         SMALLINT NOT NULL,
    icon_url         TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT zerion_assets_symbol_chain_unique UNIQUE (symbol, chain_id)
);
CREATE INDEX idx_zerion_assets_symbol ON zerion_assets(UPPER(symbol));
```

### 2. New: ZerionAsset model + repo

**`internal/platform/sync/zerion_asset.go`** — model struct (Symbol, Name, ChainID, ContractAddress, Decimals, IconURL)

**`internal/platform/sync/zerion_asset_port.go`** — interface:
- `Upsert(ctx, *ZerionAsset) error` — ON CONFLICT (symbol, chain_id) DO UPDATE
- `GetBySymbol(ctx, symbol, chainID string) (*ZerionAsset, error)` — empty chainID = any chain
- `GetAllBySymbol(ctx, symbol) ([]ZerionAsset, error)`

**`internal/infra/postgres/zerion_asset_repo.go`** — Postgres implementation

### 3. Enrich DecodedTransfer with name/icon

**`internal/platform/sync/port.go`** — add `AssetName string` and `IconURL string` to:
- `DecodedTransfer`
- `DecodedFee`
- `OnChainPosition`

**`internal/infra/gateway/zerion/adapter.go`** — populate from `FungibleInfo.Name` and `FungibleInfo.Icon.URL` in:
- `convertTransfer()`
- `convertFee()`
- `GetPositions()`

### 4. Auto-create assets during sync

**`internal/platform/sync/collector.go`**:
- Add `zerionAssetRepo ZerionAssetRepository` field
- New `extractAssets(ctx, txs)` method: iterates transfers/fees, deduplicates by symbol:chain, calls `Upsert`
- Called right after fetching from Zerion API, before storing raw txs

**`internal/platform/sync/reconciler.go`**:
- Add `zerionAssetRepo ZerionAssetRepository` field
- After fetching positions, upsert asset metadata for each position

**`internal/platform/sync/service.go`**:
- Update `NewService` signature to accept `ZerionAssetRepository`
- Pass to `NewCollector` and `NewReconciler`

### 5. DecimalResolver (cascading + cached)

**`pkg/money/resolver.go`** (new):
```go
type AssetDecimalSource interface {
    GetDecimalsBySymbol(ctx context.Context, symbol, chainID string) (int, bool)
}

type DecimalResolver struct {
    sources []AssetDecimalSource  // ordered by priority
    cache   map[string]int        // "SYMBOL" or "SYMBOL:chain_id"
    mu      sync.RWMutex
}
```
- `Resolve(ctx, symbol, chainID) int` — check cache → try sources in order → fallback `GetDecimals()`
- `ResolveSymbolOnly(ctx, symbol) int` — convenience (chainID="")

**`internal/platform/asset/decimal_source.go`** (new):
- Adapts `asset.Repository` → `AssetDecimalSource` (queries `assets` table)

**`internal/platform/sync/decimal_source.go`** (new):
- Adapts `ZerionAssetRepository` → `AssetDecimalSource` (queries `zerion_assets` table)

### 6. Replace 8 call sites

| Call site | File | Change |
|-----------|------|--------|
| Portfolio: asset totals | `module/portfolio/service.go:183` | `s.resolver.ResolveSymbolOnly(ctx, assetID)` |
| Portfolio: wallet entries | `module/portfolio/service.go:213` | `s.resolver.Resolve(ctx, entry.AssetID, entry.ChainID)` |
| Portfolio: breakdown | `module/portfolio/service.go:290` | `s.resolver.ResolveSymbolOnly(ctx, assetID)` |
| Portfolio handler | `handler/portfolio.go:85` | Use `holding.Decimals` (add field to `AssetHolding`) |
| WAC positions | `handler/taxlot.go:248` | `h.resolver.ResolveSymbolOnly(r.Context(), p.Asset)` |
| Disposals | `handler/taxlot.go:296` | `h.resolver.ResolveSymbolOnly(r.Context(), d.LotAsset)` |
| Tax lot response | `handler/taxlot.go:321` | Pass `decimals int` param, caller resolves |
| Display amount | `module/transactions/service.go:241` | Make method on service, use resolver |

### 7. DI wiring: `cmd/api/main.go`

```go
zerionAssetRepo := postgres.NewZerionAssetRepository(db.Pool)
assetDecimalSrc := asset.NewDecimalSource(assetRepo)
zerionDecimalSrc := sync.NewDecimalSource(zerionAssetRepo)
decimalResolver := money.NewDecimalResolver(assetDecimalSrc, zerionDecimalSrc)
```

Pass `zerionAssetRepo` to sync service, `decimalResolver` to portfolio/transaction services and taxlot handler.

### 8. Tests

- Mock `ZerionAssetRepository` in `sync/test_helpers_test.go`
- Update `NewService`/`NewCollector`/`NewReconciler` calls in sync tests
- Mock `DecimalResolver` or use real one with stub sources for portfolio/transaction tests
- Unit test `DecimalResolver`: cascade, cache, fallback

## Files summary

| File | Action |
|------|--------|
| `migrations/000021_zerion_assets.up.sql` | **New** |
| `migrations/000021_zerion_assets.down.sql` | **New** |
| `internal/platform/sync/zerion_asset.go` | **New** |
| `internal/platform/sync/zerion_asset_port.go` | **New** |
| `internal/platform/sync/decimal_source.go` | **New** |
| `internal/infra/postgres/zerion_asset_repo.go` | **New** |
| `pkg/money/resolver.go` | **New** |
| `internal/platform/asset/decimal_source.go` | **New** |
| `internal/platform/sync/port.go` | Modify (add AssetName, IconURL fields) |
| `internal/infra/gateway/zerion/adapter.go` | Modify (populate name/icon) |
| `internal/platform/sync/collector.go` | Modify (add asset extraction) |
| `internal/platform/sync/reconciler.go` | Modify (add asset extraction) |
| `internal/platform/sync/service.go` | Modify (pass zerionAssetRepo) |
| `internal/module/portfolio/service.go` | Modify (add resolver, add Decimals to AssetHolding) |
| `internal/transport/httpapi/handler/portfolio.go` | Modify (use holding.Decimals) |
| `internal/transport/httpapi/handler/taxlot.go` | Modify (add resolver) |
| `internal/module/transactions/service.go` | Modify (add resolver) |
| `cmd/api/main.go` | Modify (wire everything) |
| Test files for sync, portfolio, taxlot | Modify |

## What does NOT change

- `assets` table schema — untouched
- `pkg/money/decimals.go` — kept as final fallback
- Ledger core — no changes
- Frontend — already uses decimals from API

## Verification

1. `just migrate-up` — creates `zerion_assets` table
2. `cd apps/backend && go build ./...` — must compile
3. `cd apps/backend && go test ./... -v -short` — all tests pass
4. Sync a wallet: `POST /wallets/{id}/sync`
5. Check DB: `SELECT symbol, chain_id, decimals FROM zerion_assets ORDER BY symbol;` — should have entries
6. Check portfolio UI: FLUID, wDINO, aEthWETH, WETH, GM show correct amounts
7. Verify existing assets (ETH, BTC, USDC) still display correctly
