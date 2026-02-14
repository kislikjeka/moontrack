# Plan: Remove all Alchemy references from codebase

## Context

Zerion is now the sole sync provider (per ADR-001). The Alchemy integration is a legacy fallback that is no longer needed. We need to clean out all Alchemy code, config, and types while keeping the Zerion-only sync path intact.

## Sync status — how it works with Zerion only

The wallet sync state-machine stays unchanged: `pending → syncing → synced | error`.

Zerion path uses **time-based cursor** (`LastSyncAt`), not block numbers:
1. `GetWalletsForSync()` — finds wallets with `pending` or `error` status
2. `ClaimWalletForSync()` — atomically sets `syncing`
3. Uses `LastSyncAt` as "since" cursor (or `InitialSyncLookback` = 90 days for first sync)
4. Fetches decoded transactions via Zerion API
5. On success: `SetSyncCompletedAt(walletID, lastMinedAt)` → `synced` + updates `last_sync_at`
6. On error: `SetSyncError(walletID, errMsg)` → `error`

What we remove: `LastSyncBlock` (block-based cursor), `SetSyncCompleted` (with block number). If `ZERION_API_KEY` is not set, sync simply doesn't start (instead of falling back to Alchemy).

## Changes

### 1. Delete Alchemy gateway package
Delete entire directory:
- `apps/backend/internal/infra/gateway/alchemy/` (client.go, types.go, adapter.go, client_test.go, types_test.go)

### 2. Delete chains config (Alchemy-only)
- Delete `apps/backend/config/chains.yaml`
- Delete `apps/backend/pkg/config/chains.go` (entire file — `ChainsConfig`, `Chain`, `GetAlchemyNetwork`, `LoadChainsConfig` all Alchemy-only)

### 3. Clean up sync package — remove block-based path

**`internal/platform/sync/service.go`**:
- Remove `syncWalletAlchemy()` method (lines 275-368)
- Simplify `syncWallet()` — it should just call `syncWalletZerion()` directly (no fallback)
- Remove `blockchainClient` field from `Service` struct
- Remove `blockchainClient` parameter from `NewService()`
- Update comment on `syncWallet`

**`internal/platform/sync/port.go`**:
- Remove `Transfer` struct (lines 30-45) and related types (`TransferDirection`, `TransferType` constants used only by it)
  - **Note**: `TransferDirection` (`DirectionIn`/`DirectionOut`) is also used by `DecodedTransfer` — keep it!
  - Remove: `TransferType` (line 22-28), `Transfer` struct (lines 30-45), `BlockchainClient` interface (lines 47-60)
  - Remove `SetSyncCompleted` from `WalletRepository` interface (the one with block number)

**`internal/platform/sync/config.go`**:
- Remove `InitialSyncBlockLookback` and `MaxBlocksPerSync` from `Config` struct and `DefaultConfig()`
- Remove their validation in `Validate()`

**`internal/platform/sync/processor.go`** — delete entirely (Alchemy-only processor)
**`internal/platform/sync/processor_test.go`** — delete entirely

### 4. Clean up wallet layer

**`internal/platform/wallet/port.go`**:
- Remove `SetSyncCompleted(ctx, walletID, lastBlock, syncAt)` method from `Repository` interface

**`internal/platform/wallet/model.go`**:
- Remove `LastSyncBlock *int64` field from `Wallet` struct

**`internal/infra/postgres/wallet_repo.go`**:
- Remove `SetSyncCompleted()` method implementation
- Remove `last_sync_block` from SELECT queries in wallet scans (if selected)

### 5. Clean up main.go wiring

**`apps/backend/cmd/api/main.go`**:
- Remove `alchemy` import
- Remove entire `if cfg.AlchemyAPIKey != ""` block (lines 170-180)
- Remove `blockchainClient` variable declaration
- Pass `nil` instead of `blockchainClient` to `sync.NewService()` — actually, remove the parameter entirely since Service no longer needs it
- Update fallback log message: only mention ZERION_API_KEY
- Remove `syncProviderName()` function (just log "zerion" directly)

### 6. Clean up config

**`apps/backend/pkg/config/config.go`**:
- Remove `AlchemyAPIKey` and `ChainsConfigPath` fields from `Config` struct
- Remove their loading from env vars

**`apps/backend/.env.example`**:
- Remove `ALCHEMY_API_KEY` line and comment
- Remove `CHAINS_CONFIG_PATH` line

### 7. Clean up docker-compose.yml
- Remove `ALCHEMY_API_KEY` and `CHAINS_CONFIG_PATH` from backend environment

### 8. DB migration — drop `last_sync_block` column
Create new migration to drop the `last_sync_block` column from `wallets` table (it was only used for Alchemy block-based cursor).

### 9. Documentation cleanup
- `docs/adr/001-alchemy-zerion-integration.md` — keep as historical record, no changes needed (it documents the decision to move away from Alchemy)
- `docs/prd/blockchain-data-integration.md` — leave as-is (historical)

## Files to delete
- `apps/backend/internal/infra/gateway/alchemy/` (entire directory)
- `apps/backend/config/chains.yaml`
- `apps/backend/pkg/config/chains.go`
- `apps/backend/internal/platform/sync/processor.go`
- `apps/backend/internal/platform/sync/processor_test.go`

## Files to modify
- `apps/backend/cmd/api/main.go`
- `apps/backend/internal/platform/sync/service.go`
- `apps/backend/internal/platform/sync/port.go`
- `apps/backend/internal/platform/sync/config.go`
- `apps/backend/internal/platform/wallet/port.go`
- `apps/backend/internal/platform/wallet/model.go`
- `apps/backend/internal/infra/postgres/wallet_repo.go`
- `apps/backend/pkg/config/config.go`
- `apps/backend/.env.example`
- `docker-compose.yml`

## New files
- `apps/backend/migrations/NNNN_drop_last_sync_block.up.sql`
- `apps/backend/migrations/NNNN_drop_last_sync_block.down.sql`

## Verification
1. `cd apps/backend && go build ./...` — must compile cleanly
2. `cd apps/backend && go test ./internal/platform/sync/...` — sync tests pass
3. `cd apps/backend && go test ./...` — all tests pass
4. Grep for "alchemy" (case-insensitive) — only docs/ADR references remain
