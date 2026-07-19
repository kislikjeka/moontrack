# Issue #28 — Per-chain independent cursors + failure isolation

## Context

Post-#27, a wallet's chain set lives in `wallet_chain_sync` (one row per enabled chain, each already carrying its own `sync_status`/`sync_error`/`sync_phase`/`collect_cursor_at`/`last_sync_at` — the schema is complete, **no migration needed**). But the *behavior* still treats all chains as one unit:

- **Collector** (`collector.go`) loops chains, but on the **first** chain error it does `return count, err`, aborting the whole collect. That bubbles to `service.go:syncWallet`, which calls wallet-level `SetSyncError` — and that setter **mirrors error into every chain row**. So one chain's provider outage freezes and error-marks *all* chains.
- **Incremental `since`** comes from the wallet-level `w.CollectCursorAt` (the max across chains), so a lagging chain that failed last cycle is dragged forward past its own history by a faster chain's cursor → **skips transactions**.
- The three lifecycle setters (`ClaimWalletForSync`, `SetSyncCompletedAt`, `SetSyncError`) unconditionally overwrite **all** chain rows, so per-chain state can never diverge.

#28 makes each chain sync on its own timeline: a chain that errors is isolated (cursor unmoved, only *its* row → error), other chains advance their cursors and record ledger data, and the failed chain resumes from its own cursor next cycle without re-fetching or skipping others. Demoable through the existing `TransactionDataProvider` port seam.

**User-confirmed design choices:** wallet-level `sync_status` is **derived** from the chain rows via the existing `wallet.RollupStatus()` fold (activates the contract #27 built but left dead in the write path); retry is driven by that rollup — a partially-failed wallet rolls up to `error`, which `GetWalletsForSync` already re-selects, so no new selection query is added.

## Design

Isolation lives **inside the collector's per-chain loop**. The wallet-level mirror-writes in the lifecycle setters are replaced by per-chain writes plus a single rollup at the end of the cycle.

### 1. Port: per-chain lifecycle setters (`sync/port.go` + `postgres/wallet_repo.go`)

Add to the `WalletRepository` interface (and the postgres impl + the `MockWalletRepository` in `test_helpers_test.go`):

- `SetChainSyncError(ctx, walletID, chain, errMsg string) error` — set one chain row to `error`, leave `collect_cursor_at` untouched.
- `SetChainSyncCompleted(ctx, walletID, chain string, syncAt time.Time) error` — set one chain row to `synced`, `last_sync_at=syncAt`, `sync_error=NULL`, `sync_phase='idle'`.
- `RollupWalletSyncStatus(ctx, walletID) error` — read the wallet's chain rows, fold via `wallet.RollupStatus(rows)`, and write the result to `wallets.sync_status` (plus `sync_error`: the first errored chain's message, else NULL). One SQL round-trip; keeps `wallets.sync_status` a true derived rollup.

`SetChainCollectCursor` already exists and is per-chain — reused unchanged.

**Stop the all-chains mirroring:** `ClaimWalletForSync` keeps its "flip all chain rows to syncing" (claiming the wallet legitimately starts a fresh cycle for every chain). But `SetSyncCompletedAt` and `SetSyncError` must **no longer** blanket-write every chain row — the collector now owns per-chain outcome. Simplest elegant move: make `syncWallet` stop calling the wallet-level `SetSyncError`/`SetSyncCompletedAt` for the aggregate outcome and instead let per-chain setters + `RollupWalletSyncStatus` produce the wallet row. Keep `SetSyncError`/`SetSyncCompletedAt` for the reconciler/legacy callers but strip their chain-row mirror `UPDATE` so they only touch the `wallets` row (the reconciler's `markDegraded` becomes a per-chain concern below).

### 2. Collector: isolate per chain + per-chain `since` (`collector.go`)

Rework `collect()` so each chain is independent:

- Drive `since` **per chain** from that chain's own row: `cr.CollectCursorAt` (fall back to `w.LastSyncAt`/lookback only when the chain has no cursor). Remove the wallet-level `w.CollectCursorAt` baseline for the incremental path. This means `CollectAll`/`CollectIncremental` collapse: the per-chain row already tells us whether that chain has a cursor. Keep an `initial` flag only where the lookback window differs.
- Wrap each chain's fetch+store in a step that **cannot abort the loop**: on `GetTransactions` error (or a store failure that fails the whole chain), call `SetChainSyncError(walletID, cr.Chain, msg)`, log WARN, `continue` to the next chain — **do not** advance that chain's cursor, **do not** return.
- On chain success, advance **only that chain's** cursor via `SetChainCollectCursor` to its high-water mark (unchanged). **Drop** the wallet-level `SetCollectCursor(globalMax)` write — the wallet-level cursor is no longer the incremental baseline.
- Return an aggregate count + the set of chains that errored (or just count; the errored rows are already persisted). No `return count, err` on a single chain failure — the collector only returns a hard error for a wallet-wide failure (e.g. `GetChainSyncRows` itself fails).

### 3. Service: derive outcome, don't hard-set (`service.go:syncWallet`)

- After collect → reconcile → process, replace the wallet-level `SetSyncError`/final phase logic with per-chain completion + a single `RollupWalletSyncStatus(walletID)` call so `wallets.sync_status` reflects the fold.
- Chains that collected successfully get `SetChainSyncCompleted(chain, high-water)`; chains the collector marked `error` stay `error`. Process runs once over all pending raws (chain-agnostic, as today) — the failed chain simply contributed nothing new, so the others still record ledger data.
- Reconcile's `markDegraded` (a decimals/negative-delta discrepancy) is currently wallet-level. Scope it to the offending `pos.ChainID` via `SetChainSyncError` so a bad balance on one chain degrades only that chain, then let the rollup surface it. (Reconcile still aborts its own loop on a hard discrepancy, but only that chain is marked.)

### 4. Reconciler fan-out already isolates? (`reconciler.go`)

Reconcile currently aborts the whole reconcile on any one chain's `GetPositions` error. For #28's acceptance ("other chains still … record ledger data"), a position-fetch failure on chain X should isolate to chain X: mark chain X error, skip its genesis synthesis, continue reconciling the others. Apply the same wrap-and-continue pattern as the collector. (Genesis for a chain whose balance failed to load is correctly *not* synthesized.)

## Files to modify

| File | Change |
|---|---|
| `internal/platform/sync/port.go` | Add `SetChainSyncError`, `SetChainSyncCompleted`, `RollupWalletSyncStatus` to `WalletRepository`. |
| `internal/infra/postgres/wallet_repo.go` | Implement the three; strip the all-chains mirror `UPDATE` from `SetSyncCompletedAt`/`SetSyncError`. |
| `internal/platform/sync/collector.go` | Per-chain `since` from `cr.CollectCursorAt`; wrap-and-continue on chain error; drop wallet-level cursor write. |
| `internal/platform/sync/reconciler.go` | Wrap-and-continue on per-chain `GetPositions` error; scope `markDegraded` to the chain. |
| `internal/platform/sync/service.go` | Replace wallet-level set with per-chain completion + `RollupWalletSyncStatus`. |
| `internal/platform/sync/test_helpers_test.go` | Add the 3 new methods to `MockWalletRepository`. |

## Tests (TDD at the port seam — the confirmed #28 seam)

New `sync/chain_isolation_test.go` (package `sync_test`), driving the collector/reconciler against the fakes, mirroring `chain_fanout_test.go` style. Assert **external behavior**, not helper calls:

1. **One chain errors, others advance** — provider returns txs for `ethereum`/`arbitrum` but errors on `base`. Assert: raws stored for eth+arb; `SetChainCollectCursor` called for eth+arb, **not** base; `SetChainSyncError` called for base only; collector returns no hard error.
2. **Failed chain's cursor unmoved → resumes from own cursor** — seed `base` row with `CollectCursorAt=T0`; base errors this cycle; assert base cursor still `T0` (no `SetChainCollectCursor(base, …)`). Then a second cycle where base succeeds: assert `GetTransactions(base, since=T0)` — i.e. `since` came from base's own row, not a faster chain's advanced cursor.
3. **Per-chain `since` independence** — eth cursor `T2`, base cursor `T0`; assert `GetTransactions` invoked with `since=T2` for eth and `since=T0` for base in the same cycle (proves no cross-chain cursor bleed / no skip).
4. **No skip due to another chain's failure** — combine (2)+(3): base's contiguous history is fully re-requested from its own low cursor even though eth is far ahead.
5. **Rollup** — unit test (extend `wallet/rollup_test.go` only if a gap; `RollupStatus` itself is already tested) — assert the repo-level `RollupWalletSyncStatus` maps a mixed chain set (one error) → wallet `error`. This is integration-tagged (needs DB) alongside `wallet_chain_sync_test.go`.
6. **Reconciler isolation** — one chain's `GetPositions` errors; assert genesis synthesized for the other chains, `SetChainSyncError` for the failed one, no hard error returned.

Run: `cd apps/backend && go test ./internal/platform/sync/... -short` iteratively; `go build ./...` after each edit; full `just backend-test` at the end. Integration-tagged repo tests (rollup) via the existing integration harness if the DB is available.

## Verification

- **Unit/port seam (primary, demoable):** the new `chain_isolation_test.go` *is* the demo the ticket asks for — a fake provider erroring on one chain while others advance, all through the `TransactionDataProvider` port.
- **Build + full suite:** `go build ./...`, then `just backend-test`.
- **Acceptance-criteria mapping:** test 1 → "other chains still advance + record ledger data"; test 2 → "failed chain's cursor does not move; resumes from own cursor"; tests 3–4 → "no transaction skipped due to another chain's failure"; the whole file → "demoable via the port seam".
- Note pre-existing unrelated failures on main (`TestPriceReader_*`, `TestLedgerRepository_*` per memory) — not caused by this work.
- Finish with `/code-review`, then commit to a new branch off `main`.
