# Plan: Structured Logging Coverage for Backend

## Context

Backend service has minimal logging — only HTTP middleware, startup, and partial sync service coverage. Core business logic (ledger, platform services, module handlers) and infrastructure (Zerion/CoinGecko API clients, Redis cache) are completely silent. Additionally, the `PriceUpdater` uses stdlib `log` instead of the project's `slog`-based logger, and the sync service uses raw `*slog.Logger` instead of the `*logger.Logger` wrapper.

This plan adds structured DEBUG/INFO/ERROR logging across all layers with focus on the ledger core and wallet sync, and sets up Grafana + Loki for local log viewing.

---

## Step 1: Enhance Logger Package

**File**: `apps/backend/pkg/logger/logger.go`

### 1a. Add source location (file:line)
- Enable `AddSource: true` in `slog.HandlerOptions`
- Strip directory prefix in `ReplaceAttr` — keep only `filename:line`

### 1b. Add `LOG_FORMAT` env var
- Separate format choice from env: `LOG_FORMAT=json` forces JSON even in development (needed for Loki inside Docker)
- When `LOG_FORMAT` is not set, keep current behavior (JSON for production, text for development)
- When `LOG_FORMAT=json`, use JSON handler but preserve DEBUG level if `env != "production"`

### 1c. Fix context key propagation
- Define `RequestIDKey` and `UserIDKey` as typed context keys in `pkg/logger`
- Update `WithContext()` to extract both `request_id` and `user_id` from context using these keys

### 1d. Add convenience method
- `WithDuration(d time.Duration) *Logger` — adds `"duration_ms"` field

---

## Step 2: Middleware Context Propagation

### 2a. Logger middleware (`internal/transport/httpapi/middleware/logger.go`)
- After chi's `RequestID` middleware runs, copy `chimiddleware.GetReqID(ctx)` into our `logger.RequestIDKey` context key
- Add `request_id` to the HTTP request log line
- Log at different levels by status code: 5xx = ERROR, 4xx = WARN, rest = INFO

### 2b. JWT middleware (`internal/transport/httpapi/middleware/jwt.go`)
- After setting `UserIDKey` (line 135), also set `logger.UserIDKey` in context with the string representation of user UUID

### 2c. Recovery middleware (`internal/transport/httpapi/middleware/recovery.go`)
- Add `request_id` field to panic recovery log

---

## Step 3: Update DI Wiring in main.go

**File**: `apps/backend/cmd/api/main.go`

Pass `*logger.Logger` (the `log` variable) to all service constructors that will be updated:
- `ledger.NewService(ledgerRepo, handlerRegistry, log)`
- `user.NewService(userRepo, log)`
- `wallet.NewService(walletRepo, log)`
- `asset.NewService(..., log)`
- `sync.NewService(..., log, ...)` — change from `log.Logger` to `log`
- All module handlers: pass `log` to their constructors
- `coingecko.NewClient(apiKey, log)`
- `zerion.NewClient(apiKey, log)`
- `infraRedis.NewCache(redisClient, log)`
- `asset.NewPriceUpdater(..., config)` — change `PriceUpdaterConfig.Logger` from `*log.Logger` to `*logger.Logger`

---

## Step 4: Core Ledger Logging (Highest Priority)

**File**: `apps/backend/internal/ledger/service.go`

### Service struct
- Add `logger *logger.Logger` field
- Update `NewService` to accept and store logger with `component=ledger`
- Pass logger to `accountResolver`, `transactionValidator`, `transactionCommitter`

### RecordTransaction() — 7-step pipeline
| Step | Level | Message | Key Fields |
|------|-------|---------|------------|
| Entry | — | Create child logger | `tx_type`, `source`, `external_id` |
| 1. Handler not found | ERROR | `"handler not found"` | `tx_type` |
| 2. Validation failed | ERROR | `"transaction data validation failed"` | error |
| 3. Entry generation failed | ERROR | `"entry generation failed"` | error |
| 4. Transaction created | INFO | `"recording transaction"` | `tx_id`, `entry_count`, `occurred_at` |
| 5. Account resolution failed | ERROR | `"account resolution failed"` | `tx_id`, error |
| 6. Validation failed | ERROR | `"transaction validation failed"` | `tx_id`, error |
| 7. Commit failed | ERROR | `"transaction commit failed"` | `tx_id`, error |
| Success | INFO | `"transaction recorded"` | `tx_id`, `entry_count`, `duration_ms` |

### accountResolver
- DEBUG: `"resolving account"` — `account_code`, `asset_id`
- DEBUG: `"account resolved"` — `account_code`, `account_id`
- ERROR: on any resolution failure

### transactionValidator
- ERROR: `"transaction not balanced"` — `debit_sum`, `credit_sum`
- ERROR: `"negative balance would result"` — `account_id`, `asset_id`, `current`, `change`, `new`

### transactionCommitter
- DEBUG: `"beginning db transaction"`, `"persisting transaction"`, `"updating balances"`, `"db transaction committed"` — all with `tx_id`
- DEBUG: `"applying balance change"` — `account_id`, `asset_id`, `current`, `change`, `new`
- WARN: on rollback

---

## Step 5: Sync Service & Processor Logging

**Files**: `internal/platform/sync/service.go`, `internal/platform/sync/zerion_processor.go`

### Standardize logger type
- Change `*slog.Logger` to `*logger.Logger` in both `Service` and `ZerionProcessor`
- Update `NewService` and `NewZerionProcessor` signatures
- Existing log calls work since `*logger.Logger` embeds `*slog.Logger`

### Additional log points for Service
| Location | Level | Message | Fields |
|----------|-------|---------|--------|
| SyncWallet (manual) | INFO | `"manual sync triggered"` | `wallet_id` |
| Sync window determined | DEBUG | `"sync window"` | `wallet_id`, `since`, `is_initial` |
| Cursor updated | DEBUG | `"sync cursor advanced"` | `wallet_id`, `new_cursor` |
| Sync error stored | WARN | `"sync error persisted"` | `wallet_id`, `error` |

### Additional log points for ZerionProcessor
| Location | Level | Message | Fields |
|----------|-------|---------|--------|
| Transaction classified | DEBUG | `"transaction classified"` | `tx_hash`, `op_type`, `tx_type` |
| Internal transfer detected | DEBUG | `"internal transfer detected"` | `tx_hash`, `source_wallet`, `dest_wallet` |
| Record success | DEBUG | `"transaction recorded to ledger"` | `tx_hash`, `tx_type`, `external_id` |

---

## Step 6: Platform Services Logging

### user/service.go
- Add `logger *logger.Logger` with `component=user`
- INFO: `"user registered"` (user_id), `"user logged in"` (user_id)
- WARN: `"login failed"`, `"registration attempt for existing email"`
- ERROR: replace `fmt.Printf` on line 85 with `logger.Error("failed to update last login")`

### wallet/service.go
- Add `logger *logger.Logger` with `component=wallet`
- INFO: `"wallet created"`, `"wallet updated"`, `"wallet deleted"` — `wallet_id`, `user_id`
- WARN: `"unauthorized wallet access"` — `wallet_id`, `user_id`

### asset/service.go
- Add `logger *logger.Logger` with `component=asset`
- DEBUG: price fetch attempts with cache hit/miss tracking
- WARN: `"using stale price"`, `"circuit breaker open"`

### asset/updater.go
- **Replace `*log.Logger` with `*logger.Logger`** in both struct and config
- Replace all `Printf`/`Println` calls with structured slog calls
- Add `component=price_updater` tag
- INFO: cycle start/complete with counts and duration
- ERROR: batch fetch failures, individual price record failures

---

## Step 7: Module Handlers Logging

All handlers: add `logger *logger.Logger` to struct, update constructors.

### transfer handlers (handler_in.go, handler_out.go, handler_internal.go)
- DEBUG: `"handling transfer"` — `tx_type`, `wallet_id`
- DEBUG: `"transfer entries generated"` — `entry_count`, `asset_id`

### swap/handler.go
- DEBUG: `"handling swap"` — `wallet_id`, `transfers_in`, `transfers_out`, `has_fee`
- DEBUG: `"swap entries generated"` — `entry_count`

### adjustment/handler.go
- DEBUG: `"handling adjustment"` — `wallet_id`, `asset_id`
- INFO: `"adjustment entries generated"` — `direction` (increase/decrease), `difference`

---

## Step 8: Infrastructure Logging

### zerion/client.go
- Add `logger *logger.Logger` with `component=zerion`
- DEBUG: `"API request"` — `method`, `endpoint`, `chain_id`
- DEBUG: `"API response"` — `status_code`, `duration_ms`
- WARN: `"rate limited, retrying"` — `attempt`, `backoff_ms`
- ERROR: `"rate limit exhausted"`, `"API error"` — `status_code`
- INFO: `"transactions fetched"` — `address`, `count`, `duration_ms`

### coingecko/client.go
- Add `logger *logger.Logger` with `component=coingecko`
- DEBUG: `"fetching prices"` — `asset_count`
- DEBUG: `"prices fetched"` — `asset_count`, `duration_ms`
- WARN: `"rate limited"` — `retry_after`
- ERROR: `"API error"` — `status_code`

### redis/cache.go
- Add `logger *logger.Logger` with `component=cache`
- DEBUG only (high frequency): `"cache hit"`, `"cache miss"` — `asset_id`
- ERROR: `"cache error"` — `operation`, `asset_id`, `error`

---

## Step 9: Grafana + Loki Stack

### New files to create
- `infra/loki/loki-config.yml` — minimal local config (in-memory ring, filesystem storage)
- `infra/promtail/promtail-config.yml` — Docker SD, JSON pipeline stage extracting `level`, `component`, `request_id`, `user_id`, `tx_id`
- `infra/grafana/provisioning/datasources/loki.yml` — auto-provision Loki as default datasource

### docker-compose.yml additions
Add three services:
- **loki** (`grafana/loki:3.0.0`) — port 3100, healthcheck
- **promtail** (`grafana/promtail:3.0.0`) — reads Docker socket, ships to Loki
- **grafana** (`grafana/grafana:11.0.0`) — port 3001, anonymous admin access (local dev only)

Add volumes: `loki_data`, `grafana_data`

Add `LOG_FORMAT: json` to backend environment in docker-compose (so Promtail can parse JSON logs).

### Justfile additions
- `dev-logs` — starts full stack including Loki/Promtail/Grafana, prints Grafana URL
- `grafana` — opens Grafana Explore in browser

### Key LogQL queries (for reference)
```logql
# All errors
{container="moontrack-backend"} | json | level="ERROR"

# Ledger pipeline
{container="moontrack-backend"} | json | component="ledger"

# Wallet sync flow
{container="moontrack-backend"} | json | component=~"sync|zerion"

# Specific transaction trace
{container="moontrack-backend"} | json | tx_id="<uuid>"

# Slow HTTP requests
{container="moontrack-backend"} | json | duration_ms > 500
```

---

## Structured Field Conventions

| Field | Type | Used By |
|-------|------|---------|
| `component` | string | All services (ledger, sync, user, wallet, asset, zerion, coingecko, cache, price_updater) |
| `request_id` | string | HTTP-originated requests |
| `user_id` | string | Authenticated requests |
| `wallet_id` | string | Wallet operations, sync, transfers |
| `tx_id` | string | Ledger transactions |
| `tx_type` | string | Transaction recording |
| `external_id` | string | Zerion transaction ID |
| `duration_ms` | int64 | Timed operations |
| `entry_count` | int | Ledger entries per transaction |
| `asset_id` | string | Asset/price operations |
| `chain_id` | string | Blockchain operations |
| `tx_hash` | string | Blockchain transaction hash |
| `error` | string | Error details |

---

## Files to Modify (in order)

| # | File | Change |
|---|------|--------|
| 1 | `pkg/logger/logger.go` | AddSource, context keys, LOG_FORMAT, WithDuration |
| 2 | `internal/transport/httpapi/middleware/logger.go` | Context propagation, level-based logging |
| 3 | `internal/transport/httpapi/middleware/jwt.go` | Add logger.UserIDKey to context |
| 4 | `internal/transport/httpapi/middleware/recovery.go` | Add request_id |
| 5 | `internal/ledger/service.go` | Full pipeline logging |
| 6 | `internal/platform/sync/service.go` | Change to *logger.Logger, add log points |
| 7 | `internal/platform/sync/zerion_processor.go` | Change to *logger.Logger, add log points |
| 8 | `internal/platform/user/service.go` | Add logger, fix fmt.Printf |
| 9 | `internal/platform/wallet/service.go` | Add logger |
| 10 | `internal/platform/asset/service.go` | Add logger |
| 11 | `internal/platform/asset/updater.go` | Replace *log.Logger with *logger.Logger |
| 12 | `internal/module/adjustment/handler.go` | Add logger |
| 13 | `internal/module/transfer/handler_in.go` | Add logger |
| 14 | `internal/module/transfer/handler_out.go` | Add logger |
| 15 | `internal/module/transfer/handler_internal.go` | Add logger |
| 16 | `internal/module/swap/handler.go` | Add logger |
| 17 | `internal/infra/gateway/zerion/client.go` | Add logger |
| 18 | `internal/infra/gateway/coingecko/client.go` | Add logger |
| 19 | `internal/infra/redis/cache.go` | Add logger |
| 20 | `cmd/api/main.go` | Update all DI wiring |
| 21 | `docker-compose.yml` | Add Loki, Promtail, Grafana + LOG_FORMAT |
| 22 | `justfile` | Add dev-logs and grafana commands |

## New Files

| File | Purpose |
|------|---------|
| `infra/loki/loki-config.yml` | Loki server config |
| `infra/promtail/promtail-config.yml` | Log shipping config |
| `infra/grafana/provisioning/datasources/loki.yml` | Grafana datasource |

---

## Verification

1. `cd apps/backend && go build ./...` — no compilation errors
2. `cd apps/backend && go test ./... -short` — no test regressions
3. `just dev-logs` — full stack starts including Grafana
4. Open `http://localhost:3001/explore` — Grafana shows Loki datasource
5. Trigger manual wallet sync via API — verify full log trail appears in Grafana:
   - HTTP request with request_id
   - Sync service start → Zerion fetch → process transactions → ledger recording → cursor update
6. Create manual transaction — verify ledger pipeline: validation → entries → accounts → balance → commit
7. Check that DEBUG logs appear in development, only INFO+ in production
