# Plan: Unlimited History Sync via Zerion

## Context

Initial Zerion sync currently fetches only the last 90 days of transactions (`InitialSyncLookback: 2160h`). This means older transactions are missed, and the reconciler creates large genesis balances to cover the gap. The goal is to sync **all** historical transactions by default, with no time bound.

## Changes (4 files)

### 1. `apps/backend/internal/platform/sync/config.go`
- Default `InitialSyncLookback` to `0` (meaning "no limit")
- Update `Validate()`: allow `0`, only correct negative values to `0`
- Update field comment to document `0 = fetch all history`

### 2. `apps/backend/internal/platform/sync/collector.go`
- `CollectAll()`: use `time.Time{}` (zero value) instead of computing from config — full sync always fetches everything
- `CollectIncremental()` fallback (line 68): handle `InitialSyncLookback == 0` — use zero time instead of `time.Now().Add(-0)` which would equal "now"

### 3. `apps/backend/internal/infra/gateway/zerion/client.go`
- Make `filter[min_mined_at]` conditional: only set when `since.IsZero() == false`
- When omitted, Zerion API returns all history (standard REST semantics)

### 4. `apps/backend/internal/infra/gateway/zerion/client_test.go`
- Add test verifying that zero `since` omits `filter[min_mined_at]` from the request

## What stays unchanged
- `TransactionDataProvider` interface — already accepts `time.Time`, zero is valid
- Incremental sync with existing cursor — unaffected (cursor is always non-zero)
- Reconciler, Processor, DB schema — no changes needed
- Existing tests with explicit non-zero lookback — still pass

## Verification
1. `cd apps/backend && go build ./...` — must compile
2. `cd apps/backend && go test ./internal/platform/sync/... -v` — existing + new tests pass
3. `cd apps/backend && go test ./internal/infra/gateway/zerion/... -v` — client tests pass
4. Manual: trigger sync on a wallet, verify logs show `since: "beginning of time"` and transactions from before 90 days appear
