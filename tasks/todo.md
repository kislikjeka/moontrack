# #27 — wallet_chain_sync table + Enabled-set default + chain fan-out

Parent: #23 (Noves migration). Scope: **strictly #27** — introduce the per-(wallet, chain)
sync-state table, make the collector/reconciler fan out over a wallet's *enabled chains read
from the rows*, and make wallet-level `sync_status` a derived rollup. Per-chain independent
cursors + failure isolation + resume-without-skip are **#28** and out of scope here.

## Design decisions

- The **rows of `wallet_chain_sync` ARE the wallet chain set.** One row per enabled chain.
  Columns: `wallet_id`, `chain` (domain slug), `collect_cursor_at`, `sync_status`, `sync_error`,
  `last_sync_at`, `sync_phase`, `created_at`, `updated_at`. PK `(wallet_id, chain)`.
- New wallet defaults its chain set to the **Enabled** set (`ethereum`, `base`, `arbitrum`) —
  seeded at wallet creation.
- Wallet identity stays `UNIQUE(user_id, address)` — unchanged. Chain set is orthogonal.
- Collector loops the **wallet's enabled chain rows** (not the global `GetSupportedChains()`),
  invoking the chain-aware provider per chain. Per-chain collect cursor is written as a
  side-effect (not yet independent-failure-isolated — that's #28).
- Reconciler runs **per enabled chain**: the reconciler drives the fan-out over the wallet's
  chain set and calls the position provider per chain, so a wallet reconciles only its enabled
  chains (moving the fan-out loop out of the Noves adapter into the reconciler).
- Wallet-level `sync_status` becomes a **derived rollup** over the chain rows:
  error if any errored; syncing if any syncing; else synced (pending if all pending).

## Tasks

- [ ] **Migration 000029** (`wallet_chain_sync`): create table; seed every existing wallet with
      the Enabled set (eth/base/arbitrum), copying its current wallet-level cursor/status as the
      starting per-chain state; down drops the table.
- [ ] **Domain model** (`wallet/model.go`): `WalletChainSync` struct; `EnabledChains()` returns the
      default Enabled set; keep `GetSupportedChains()`/`IsValidChain()` etc.
- [ ] **Rollup helper** (`wallet` pkg): `RollupStatus([]WalletChainSync) SyncStatus` — pure fn + unit tests.
- [ ] **Wallet repo** (`postgres/wallet_repo.go`): seed chain-set rows on `Create`; add
      `GetChainSyncRows(walletID)`, `SetChainCollectCursor(walletID, chain, cursor)`,
      `SetChainSyncPhase(walletID, chain, phase)`; derive+persist the rollup onto `wallets.sync_status`.
- [ ] **sync port** (`sync/port.go`): extend `WalletRepository` with the per-chain setters +
      `GetChainSyncRows`; extend `PositionDataProvider` to be **chain-aware**
      (`GetPositions(ctx, address, chain)`), so the reconciler owns the fan-out.
- [ ] **Collector** (`collector.go`): fan out over the wallet's chain-set rows; write per-chain
      cursor. Keep aggregate behavior otherwise.
- [ ] **Reconciler** (`reconciler.go`): fan out over the wallet's chain-set rows; call
      `posProvider.GetPositions(ctx, addr, chain)` per chain; per-chain flow/genesis unchanged.
- [ ] **Noves positions adapter** (`noves/positions.go`): make `GetPositions` chain-aware
      (single chain), dropping its internal fan-out loop (now owned by the reconciler).
- [ ] **Wiring** (`cmd/api/main.go`): verify `Create` seeding path; no new services expected.

## Tests (TDD at the two agreed seams)

- [ ] **Port seam** (`service_test.go` / new `chain_fanout_test.go`): a wallet with 3 enabled
      chain rows → provider invoked once per chain; txs on all 3 chains → ledger data on all 3.
- [ ] **Rollup unit test**: mixed chain statuses → correct wallet rollup.
- [ ] **Reconciler per-chain**: reconcile invokes positions per enabled chain; genesis per chain.
- [ ] Update existing mocks (`MockWalletRepository`, `MockPositionDataProvider`) for new methods.
- [ ] Full suite (`go test ./... -short`) green at the end.

## Review

Implemented #27 strictly (per user decision): table + chain-set fan-out + rollup; failure
isolation and per-chain independent incremental cursors deferred to #28.

**Delivered**
- **Migration 000029** (`wallet_chain_sync.up/down.sql`): `(wallet_id, chain)` PK table with
  per-chain `sync_status`/`sync_error`/`sync_phase`/`collect_cursor_at`/`last_sync_at` + check
  constraints + index; seeds every existing wallet with eth/base/arbitrum via `CROSS JOIN`.
  Verified up→down→up on the real dev DB (1 wallet → 3 rows; down drops table; re-up re-seeds).
- **Domain** (`wallet/model.go`): `WalletChainSync` struct; `RollupStatus()` pure fold
  (error>syncing>synced>pending, empty→pending) + `EnabledChains()`. Unit-tested (`rollup_test.go`).
- **Wallet repo** (`postgres/wallet_repo.go`): `Create` seeds the Enabled chain set in one tx;
  `GetChainSyncRows`, `SetChainSyncPhase`, `SetChainCollectCursor`; the three lifecycle setters
  (`ClaimWalletForSync`/`SetSyncCompletedAt`/`SetSyncError`) mirror status into the chain rows so
  `wallets.sync_status` stays a true rollup. Integration-tested (`wallet_chain_sync_test.go`, 4 tests).
- **sync port**: `WalletRepository` gains the three chain methods; `PositionDataProvider` is now
  chain-aware (`GetPositions(ctx, address, chain)`).
- **Collector**: fans out over the wallet's chain-set rows (not the global set), advancing each
  chain's own collect cursor + the wallet-level max (incremental baseline).
- **Reconciler**: owns the position fan-out over the wallet's chain set, calling the provider per
  enabled chain; genesis synthesis per chain unchanged.
- **Noves adapter**: `GetPositions` is single-chain (fan-out moved to the reconciler).
- **Port-seam tests** (`chain_fanout_test.go`): 3-enabled-chain wallet → provider invoked once per
  chain, raw stored + cursor advanced per chain; reconciler → genesis per chain.

**Verification**
- `go build ./...`, `go vet`, full `go test ./... -short` all green.
- Real-DB integration: 4 new wallet-chain-sync tests pass (seeding, per-chain setters, rollup
  invariant across lifecycle, migration seed shape). Migration up/down/up verified on dev DB.

**Not changed / deferred**
- Per-chain failure isolation + independent incremental cursors + resume-without-skip → **#28**.
- Wallet handler still advertises `GetSupportedChains()` (Compatible set); frontend/API chain-set
  editing is a #23 follow-up.

**Pre-existing failures (confirmed on clean `main`, not from this work)**
- `TestSyncService_*` integration tests panic: `setupIntegrationTest` passes `nil` rawTxRepo →
  collector nil. Broken before this branch (verified by stashing).
- `TestLedgerRepository_*` / `ledger_precision_test.go`: insert dropped `wallets.chain_id`.
- `TestPriceReader_*`: 32-char test contract fails EVM validator.
