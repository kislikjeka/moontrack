# Issue #26 — Wire Noves as sole sync provider; remove Zerion (contract)

Contract step of the Zerion→Noves migration (parent #23). Wire Noves as the only
sync provider, delete Zerion, de-vendor DB/source vocabulary, forward migration
000028 to rename + TRUNCATE sync data + reset sync state, verify one chain
end-to-end. Later tickets (#27–#33) own per-chain fan-out tables, cursors,
idempotency, and bridge stitching — NOT in scope here.

Decisions (confirmed with user):
- Build a Noves **positions provider** (balances endpoint) so reconcile stays in
  the pipeline per the AC. `/evm/{chain}/tokens/balancesOf/{addr}` verified working.
- De-vendor `source = 'noves'` (flip literal + wipe function + TRUNCATE).

## Plan

### 1. Noves positions provider (adapter seam) — TDD
- [ ] `noves/types.go`: add `BalanceItem` + `BalancesResponse` types (top-level
      array of `{balance, usdValue, token{symbol,name,decimals,address,price}}`;
      also the `{detail}` too-many-tokens error object).
- [ ] `noves/client.go`: add `GetBalances(ctx, chain, address) ([]BalanceItem, error)`;
      handle the `detail` error body (surface as error, don't crash on non-array).
- [ ] `noves/positions.go` (or extend adapter.go): `GetPositions(ctx, address)` on
      `SyncAdapter` — fan out over `wallet.GetSupportedChains()`, map each domain
      chain → noves slug, convert balances → `[]sync.OnChainPosition`. Reuse
      `amountToBaseUnits` / `isNativeAddress` / `normalizeContract`. Skip zero/dust.
- [ ] `var _ sync.PositionDataProvider = (*SyncAdapter)(nil)`.
- [ ] Tests: `positions_test.go` off `testdata/balances.json` fixture (native +
      USDC + cbBTC, mixed decimals). Assert base-unit conversion, native contract="",
      per-chain ChainID stamping. Add too-many-tokens error-path test.

### 2. De-vendor source + DB names (code)
- [ ] `sync/model.go`: `sourceName = "noves"`; `db:"zerion_id"` → `db:"external_id"`.
- [ ] `infra/postgres/raw_tx_repo.go`: `zerion_id` → `external_id` (columns +
      ON CONFLICT `(wallet_id, external_id)`).
- [ ] `infra/postgres/sync_asset_repo.go`: `zerion_assets` → `chain_assets`.
- [ ] Update comments referencing zerion_assets/zerion_id in decimal_source.go,
      collector.go (cosmetic, keep accurate).

### 3. Forward migration 000028 (de-vendor + truncate + reset)
- [ ] `000028_devendor_sync_noves.up.sql`:
      - `ALTER TABLE raw_transactions RENAME COLUMN zerion_id TO external_id;`
        + rename the `UNIQUE(wallet_id, zerion_id)` constraint.
      - `ALTER TABLE zerion_assets RENAME TO chain_assets;` (+ rename indexes/constraints).
      - `CREATE OR REPLACE FUNCTION wipe_wallet_ledger` → `source IN ('noves','sync_genesis')`.
      - TRUNCATE sync-derived financial data: delete from transactions/entries/
        tax_lots/lot_disposals/raw_transactions/account_balances for
        `source IN ('zerion','sync_genesis')` (FK-safe order); truncate chain_assets.
      - Reset wallet sync state: `sync_status='pending'`, clear `last_sync_at`,
        `sync_error`, `collect_cursor_at`, `sync_phase='idle'`. KEEP users + wallets.
- [ ] `000028_...down.sql`: reverse renames (data truncation is not reversible —
      document that; down only restores names + wipe-function literal).

### 4. Wire Noves in DI; remove Zerion
- [ ] `pkg/config/config.go`: add `NovesAPIKey` (`getEnv("NOVES_API_KEY","")`);
      remove `ZerionAPIKey`.
- [ ] `cmd/api/main.go`: import noves not zerion; gate on `cfg.NovesAPIKey`;
      `novesClient := noves.NewClient(...)`; `adapter := noves.NewSyncAdapter(novesClient)`;
      pass `adapter, adapter` (tx + pos); log "provider":"noves"; warn on missing key.
- [ ] Delete `internal/infra/gateway/zerion/` entirely.
- [ ] `price/model.go` + `price_reader.go`: `SourceZerion` is a **price** source
      (CoinGecko/backfill pipeline), independent of sync provider — leave as-is
      (out of scope; price pipeline untouched per #23). Note in review.

### 5. Docs
- [ ] `docs/sync-and-price-flow.md`: rewrite Zerion/Alchemy sections → Noves;
      remove stale syncWalletAlchemy fallback; Alchemy already gone per ADR-001.

### 6. Verify
- [ ] `go build ./...` passes.
- [ ] `go test ./internal/platform/sync/... ./internal/infra/gateway/noves/... -short`
      (rename any stale mockZerion references in sync tests if they break).
- [ ] Full backend suite `go test ./... -short`.
- [ ] End-to-end single-chain: register user, add wallet, run migration, sync a
      real Base wallet via Noves; confirm collect→reconcile→process→ledger/tax-lots.
- [ ] `/code-review`, then commit.

## Review

Implemented all of #26 plus two bugs surfaced during the real end-to-end sync:

**Delivered**
- Noves **positions provider**: `noves/GetBalances` client method + `SyncAdapter.GetPositions`
  fanning out over enabled chains via `/evm/{chain}/tokens/balancesOf/{addr}`, converting
  decimal balances → `OnChainPosition`. New `positions.go` + `positions_test.go` + `testdata/balances.json`.
  Handles the `{detail}` too-many-tokens error envelope.
- De-vendored: `sourceName='noves'`; `zerion_id`→`external_id`; `zerion_assets`→`chain_assets` (repos + tags).
- Migration `000028_devendor_sync_noves` (up+down): renames (column/table/constraint/pk/index),
  rewrites `wipe_wallet_ledger` to `source IN ('noves','sync_genesis')`, TRUNCATEs sync-derived
  data for `source IN ('zerion','sync_genesis')`, resets wallet sync state, keeps users+wallets.
  Verified apply + full reversibility against real Postgres.
- DI: `config.NovesAPIKey` (removed `ZerionAPIKey`); `main.go` injects the Noves adapter for both
  ports; deleted `infra/gateway/zerion/`. Backend boots with `provider:noves`.
- Docs: `sync-and-price-flow.md` rewritten to Noves (two-phase pipeline, no embedded prices);
  no active Zerion/Alchemy references.
- Updated all sync/postgres test `source` literals `"zerion"`→`"noves"`.

**Two bugs fixed during E2E (both blocked real sync):**
1. `defaultPageSize` was 100 → Noves rejects with HTTP 400 (`pageSize ∈ [1,50]`). Set to 50.
2. Per user request + #23 Enabled set: reduced `supportedEVMChains` from 7 → **ethereum, base,
   arbitrum**. Adapter stays Compatible with more (noves/chains.go unchanged).

**E2E result (real Base wallet, single chain):** collect→reconcile→process→ledger/tax-lots all ran.
`source='noves'` transfer_in/out, swaps (lp_deposit/withdraw/claim_fees), lending_supply; 3 genesis
(from the new positions provider); 16 tax lots; `external_id = chain:txHash`. 3 lending
negative-balance errors are pre-existing accounting edge cases (MT-SYNC-12/#14 ordering), not from this work.

**Known pre-existing (not in scope, confirmed on clean `main`):** two integration tests fail —
`TestPriceReader_*` (32-char test contract fails EVM validator) and `TestLedgerRepository_*`
(inserts dropped `wallets.chain_id`). Both predate this branch.
