# Phase 5: Tax Lots & Cost Basis — Implementation Plan

## Context

MoonTrack needs lot-based cost basis tracking to support tax reporting (realized/unrealized PnL, FIFO disposal). Every asset acquisition creates a tax lot; every disposal consumes lots in FIFO order. Tax lot operations run **inside** the ledger DB transaction for atomicity.

**Scope (this phase):** Core engine only — domain model, migration, repository, FIFO algorithm, hook integration, DI wiring, genesis lots. Override API, WAC materialized view, and clearing monitor are deferred.

**Key decisions:**
- TaxLot domain lives in `internal/ledger/` (tightly coupled to ledger transaction flow)
- Repository implementation in `internal/infra/postgres/` (shares `txContextKey` for same DB tx)
- FIFO insufficient lots → warn and continue (don't block transactions)
- Clearing monitor deferred to a separate phase

---

## Gap Analysis (vs Original Spec)

| # | Issue | Resolution |
|---|-------|------------|
| 1 | **Migration numbering**: spec says 000009/000010, current highest is 000012 | Use 000013/000014 |
| 2 | **`ctxKey` is private** to `postgres` package — TaxLotRepo must share it | Place `taxlot_repo.go` in `infra/postgres/` package (same as `ledger_repo.go`) |
| 3 | **"PostCommitHook" naming** misleading — runs BEFORE commit | Rename to `PostBalanceHook` (runs after balance updates, before CommitTx) |
| 4 | **DeFi handlers don't exist** (Phase 4 not done) | Hook uses entry-type-based classification, not tx-type-based. Works with any tx type automatically |
| 5 | **Gas fees create disposals** — spec doesn't mention this | Gas fee entries (`entry_type: "gas_payment"` in metadata) on CRYPTO_WALLET accounts are real disposals. Add `DisposalTypeGasFee` |
| 6 | **`tax_lots.asset` is `TEXT`**, but `accounts.asset_id` is `VARCHAR(20)` | Use `VARCHAR(20)` for consistency |
| 7 | **Genesis lots cost basis** — `ab.usd_value` is current price, not historical | Use latest entry's `usd_rate` for the account+asset as better approximation. Mark source as `genesis_approximation` |
| 8 | **Cost basis per unit** — spec unclear which Entry field to use | Use `Entry.USDRate` (price per whole unit, scaled 10^8). This IS the cost basis per unit at acquisition time |
| 9 | **Internal transfer lot linking** with multi-lot FIFO | Link to first consumed lot. Known approximation — users can override |
| 10 | **Disposal ordering within a transaction** | Process entries in natural order (handlers generate transfer entries before gas entries) |
| 11 | **Account lookups in hook** are expensive per-entry | Cache accounts in local map within hook execution |
| 12 | **`disposal_type` CHECK constraint** missing `gas_fee` | Add `'gas_fee'` to the CHECK constraint |
| 13 | **`auto_cost_basis_source` CHECK** missing `genesis_approximation` | Add to CHECK constraint |

---

## Iterations

### Iteration 1: Domain Model + Database Migration

**Goal:** Data layer foundation — types, interfaces, tables.

**New files:**

- `apps/backend/internal/ledger/taxlot_model.go` — Domain types:
  ```
  CostBasisSource: swap_price | fmv_at_transfer | linked_transfer | genesis_approximation
  DisposalType: sale | internal_transfer | gas_fee
  TaxLot struct (ID, TransactionID, AccountID, Asset, QuantityAcquired/Remaining,
                 AcquiredAt, AutoCostBasisPerUnit, AutoCostBasisSource,
                 OverrideCostBasisPerUnit, OverrideReason, OverrideAt,
                 LinkedSourceLotID, CreatedAt)
  LotDisposal struct (ID, TransactionID, LotID, QuantityDisposed,
                      ProceedsPerUnit, DisposalType, DisposedAt, CreatedAt)
  LotOverrideHistory struct (ID, LotID, PreviousCostBasis, NewCostBasis, Reason, ChangedAt)
  ```

- `apps/backend/internal/ledger/taxlot_port.go` — Repository interface:
  ```
  TaxLotRepository interface {
      CreateTaxLot(ctx, *TaxLot) error
      GetTaxLot(ctx, uuid) (*TaxLot, error)
      GetOpenLotsFIFO(ctx, accountID, asset) ([]*TaxLot, error)  // SELECT...FOR UPDATE ORDER BY acquired_at ASC
      UpdateLotRemaining(ctx, lotID, newRemaining) error
      CreateDisposal(ctx, *LotDisposal) error
      GetLotsByAccount(ctx, accountID, asset) ([]*TaxLot, error)
      GetDisposalsByTransaction(ctx, txID) ([]*LotDisposal, error)
      GetDisposalsByLot(ctx, lotID) ([]*LotDisposal, error)
      // Override
      UpdateOverride(ctx, lotID, costBasis, reason) error
      ClearOverride(ctx, lotID) error
      CreateOverrideHistory(ctx, *LotOverrideHistory) error
      GetOverrideHistory(ctx, lotID) ([]*LotOverrideHistory, error)
  }
  ```

- `apps/backend/internal/ledger/taxlot_errors.go` — Error sentinels:
  ```
  ErrInsufficientLots = errors.New("insufficient lots for disposal")
  ErrLotNotFound      = errors.New("tax lot not found")
  ```

- `apps/backend/migrations/000013_tax_lots.up.sql` — Tables:
  - `tax_lots` — partial index `idx_tax_lots_fifo` (account_id, asset, acquired_at WHERE quantity_remaining > 0)
  - `lot_disposals` — indexes on transaction_id and lot_id
  - `lot_override_history` — index on lot_id
  - `tax_lots_effective` VIEW (COALESCE override > linked_source > auto)
  - CHECK constraints include `genesis_approximation` and `gas_fee`

- `apps/backend/migrations/000013_tax_lots.down.sql` — DROP VIEW + tables

**Tests:** Model validation unit tests (positive quantities, valid sources/types)

---

### Iteration 2: Repository Implementation ‖ Iteration 3: Hook Infrastructure ‖ Iteration 4: FIFO Algorithm

> These 3 iterations can run **in parallel** after Iteration 1.

#### Iteration 2: Repository Implementation

**New file:** `apps/backend/internal/infra/postgres/taxlot_repo.go`

- `TaxLotRepository` struct with `pool *pgxpool.Pool`
- Reuses `txContextKey` and `getQueryer(ctx)` pattern from `ledger_repo.go` (same package)
- Critical method: `GetOpenLotsFIFO` — `SELECT ... FOR UPDATE ORDER BY acquired_at ASC`
- All `*big.Int` fields stored/read as strings to/from NUMERIC(78,0) columns
- Nullable fields (`OverrideCostBasisPerUnit`, `LinkedSourceLotID`) use `pgtype` or `*big.Int`/`*uuid.UUID`

**Tests:** Integration tests (`//go:build integration`):
- CRUD operations for lots, disposals, override history
- FIFO ordering verification
- Filtering (only open lots with remaining > 0)
- Constraint violations (negative remaining, etc.)

#### Iteration 3: Hook Infrastructure in Committer

**Modified file:** `apps/backend/internal/ledger/service.go`

Changes:
1. Add type at package level (after imports):
   ```go
   // PostBalanceHook runs inside the DB transaction after balance updates,
   // before CommitTx. If it returns an error, the transaction rolls back.
   type PostBalanceHook func(ctx context.Context, tx *Transaction) error
   ```

2. Add field to `transactionCommitter` struct (line ~492):
   ```go
   postBalanceHooks []PostBalanceHook
   ```

3. Modify `commit()` (line ~501) — insert hook execution between `updateBalances` and `CommitTx`:
   ```go
   // After updateBalances (line 529), before CommitTx (line 534):
   for _, hook := range c.postBalanceHooks {
       if err := hook(txCtx, tx); err != nil {
           return fmt.Errorf("post-balance hook failed: %w", err)
       }
   }
   ```

4. Add public method on `Service`:
   ```go
   func (s *Service) RegisterPostBalanceHook(hook PostBalanceHook) {
       s.committer.postBalanceHooks = append(s.committer.postBalanceHooks, hook)
   }
   ```

**Tests:**
- Hook called after successful balance update
- Hook error causes full transaction rollback (entries, balances — nothing persisted)
- Multiple hooks called in registration order
- No hooks registered — existing behavior unchanged (regression)

#### Iteration 4: FIFO Algorithm

**New file:** `apps/backend/internal/ledger/taxlot_fifo.go`

Pure algorithm, testable with mock repository:
```go
func DisposeFIFO(
    ctx context.Context,
    repo TaxLotRepository,
    accountID uuid.UUID,
    asset string,
    quantity *big.Int,
    proceedsPerUnit *big.Int,
    disposalType DisposalType,
    transactionID uuid.UUID,
    disposedAt time.Time,
) ([]*LotDisposal, error)
```

Logic:
1. `repo.GetOpenLotsFIFO(ctx, accountID, asset)` — locked rows
2. Iterate oldest-first: `disposeQty = min(lot.QuantityRemaining, remaining)`
3. Create `LotDisposal`, update `lot.QuantityRemaining -= disposeQty`
4. If `remaining > 0` after all lots: return `ErrInsufficientLots`

**New file:** `apps/backend/internal/ledger/taxlot_fifo_test.go`

Tests (mock-based, no DB):
- Single lot, exact disposal (100 → 100)
- Single lot, partial disposal (100 → 60, remaining 40)
- Multi-lot FIFO ordering (Lot A=50 @Jan, Lot B=80 @Feb, dispose 70 → A=0, B=60)
- Multi-lot full consumption (dispose everything)
- Insufficient lots → `ErrInsufficientLots`, no side effects
- Zero lots available → `ErrInsufficientLots`
- Empty quantity → no-op

---

### Iteration 5: Tax Lot Hook Implementation

**Depends on:** Iterations 2, 3, 4

**New file:** `apps/backend/internal/ledger/taxlot_hook.go`

```go
func NewTaxLotHook(repo TaxLotRepository, ledgerRepo Repository, log *logger.Logger) PostBalanceHook
```

Hook logic (entry-type-based, NOT transaction-type-based):
1. For each entry, lookup account (cached in local map)
2. Skip non-`CRYPTO_WALLET` accounts
3. Classify: `EntryTypeAssetIncrease` → acquisition, `EntryTypeAssetDecrease` → disposal
4. **Process disposals first** (consume existing lots via FIFO):
   - Determine `DisposalType`: check `metadata["entry_type"] == "gas_payment"` → `gas_fee`; tx type `internal_transfer` → `internal_transfer`; else → `sale`
   - Determine `proceedsPerUnit`: use `entry.USDRate`
   - Call `DisposeFIFO(...)`
   - **On `ErrInsufficientLots`: log warning, continue** (don't fail transaction)
5. **Process acquisitions** (create new lots):
   - `AutoCostBasisPerUnit` = `entry.USDRate`
   - `AutoCostBasisSource`: swap → `swap_price`; internal_transfer → `linked_transfer`; else → `fmv_at_transfer`
   - For internal_transfer: set `LinkedSourceLotID` to first disposed lot's ID (from step 4 result)
   - Call `repo.CreateTaxLot(...)`

**New file:** `apps/backend/internal/ledger/taxlot_hook_test.go`

Tests (mock repo + mock ledger repo):
- `transfer_in`: creates 1 lot with `fmv_at_transfer`, no disposals
- `transfer_out`: FIFO disposal with `sale` type, no lots created
- `transfer_out` with gas: 2 disposals — main asset as `sale`, native as `gas_fee`
- `swap`: disposal for sold asset + lot for bought asset
- `internal_transfer`: disposal with `internal_transfer` + lot with `linked_transfer` + `LinkedSourceLotID`
- Non-wallet entries (income, clearing) → skipped
- Insufficient lots → warning logged, transaction NOT failed
- Transaction with no CRYPTO_WALLET entries → no-op

---

### Iteration 6: DI Wiring + Integration Tests

**Depends on:** Iteration 5

**Modified file:** `apps/backend/cmd/api/main.go`

After `ledgerSvc` creation (line ~107), before handler registrations (line ~110):
```go
taxLotRepo := postgres.NewTaxLotRepository(db.Pool)
taxLotHook := ledger.NewTaxLotHook(taxLotRepo, ledgerRepo, log)
ledgerSvc.RegisterPostBalanceHook(taxLotHook)
log.Info("TaxLot hook registered")
```

**New file:** `apps/backend/internal/ledger/taxlot_integration_test.go` (`//go:build integration`)

End-to-end tests with real Postgres:
- RecordTransaction(transfer_in) → tax lot created with correct fields
- RecordTransaction(transfer_out) after transfer_in → lot remaining decreases
- RecordTransaction(swap) → lot created for bought + disposal for sold
- RecordTransaction(internal_transfer) → source lot disposed, dest lot created with link
- RecordTransaction(transfer_out) with gas → 2 disposals (main + gas)
- Sequential: transfer_in(100) → transfer_out(60) → verify remaining=40 → transfer_out(40) → remaining=0
- Insufficient lots: transfer_out without prior transfer_in → warning, transaction still succeeds
- Verify: `go build ./...` passes

---

### Iteration 7: Genesis Lots Migration

**Can run in parallel with** Iteration 6, but should be **deployed after** Iteration 6.

**New files:**

- `apps/backend/migrations/000014_genesis_lots.up.sql`:
  ```sql
  INSERT INTO tax_lots (transaction_id, account_id, asset, quantity_acquired,
                        quantity_remaining, acquired_at, auto_cost_basis_per_unit,
                        auto_cost_basis_source)
  SELECT
      sub.first_tx_id,
      ab.account_id,
      ab.asset_id,
      ab.balance,
      ab.balance,
      sub.first_occurred_at,
      COALESCE(sub.latest_usd_rate, 0),
      'genesis_approximation'
  FROM account_balances ab
  JOIN accounts a ON ab.account_id = a.id
  CROSS JOIN LATERAL (
      SELECT t.id as first_tx_id, t.occurred_at as first_occurred_at
      FROM transactions t
      JOIN entries e ON e.transaction_id = t.id
      WHERE e.account_id = ab.account_id AND e.asset_id = ab.asset_id
      ORDER BY t.occurred_at ASC LIMIT 1
  ) sub
  CROSS JOIN LATERAL (
      SELECT e.usd_rate as latest_usd_rate
      FROM entries e
      WHERE e.account_id = ab.account_id AND e.asset_id = ab.asset_id
      ORDER BY e.occurred_at DESC LIMIT 1
  ) rate
  WHERE a.type = 'CRYPTO_WALLET'
    AND ab.balance > 0
    AND NOT EXISTS (
        SELECT 1 FROM tax_lots tl
        WHERE tl.account_id = ab.account_id AND tl.asset = ab.asset_id
    );
  ```
  Uses latest entry's `usd_rate` (not `ab.usd_value / ab.balance` which loses precision). Idempotent via `NOT EXISTS`.

- `apps/backend/migrations/000014_genesis_lots.down.sql`:
  ```sql
  DELETE FROM tax_lots
  WHERE auto_cost_basis_source = 'genesis_approximation'
    AND quantity_remaining = quantity_acquired
    AND NOT EXISTS (SELECT 1 FROM lot_disposals ld WHERE ld.lot_id = tax_lots.id);
  ```

**Tests:**
- Integration: verify lots created for accounts with positive balances
- Integration: idempotency — running twice creates no duplicates
- Verify genesis lots have `genesis_approximation` source

---

## Dependency Graph & Parallelism

```
Iteration 1: Domain + Migration
      │
      ├──────────────────────────────────┐
      │                                  │
      ▼                    ▼             ▼
Iteration 2:       Iteration 3:   Iteration 4:
Repository         Hook Infra     FIFO Algorithm
(infra/postgres)   (service.go)   (pure logic)
      │                  │             │
      └──────────────────┴─────────────┘
                         │
                         ▼
                  Iteration 5: Hook
                         │
              ┌──────────┤
              ▼          ▼
      Iteration 6:  Iteration 7:
      DI Wiring     Genesis Migration
      + Integration (deploy AFTER 6)
```

**Parallel groups:**
- **Group A** (after Iter 1): Iterations 2, 3, 4 — fully independent, can be 3 parallel agents
- **Group B** (after Iter 5): Iterations 6 and 7 — independent but 7 deploys after 6

---

## Critical Files

| File | Action | Purpose |
|------|--------|---------|
| `internal/ledger/service.go` | MODIFY | Add `PostBalanceHook` type, inject into committer, modify `commit()` |
| `internal/ledger/taxlot_model.go` | CREATE | Domain types: TaxLot, LotDisposal, LotOverrideHistory |
| `internal/ledger/taxlot_port.go` | CREATE | TaxLotRepository interface |
| `internal/ledger/taxlot_errors.go` | CREATE | Error sentinels |
| `internal/ledger/taxlot_fifo.go` | CREATE | FIFO disposal algorithm |
| `internal/ledger/taxlot_hook.go` | CREATE | PostBalanceHook implementation |
| `internal/infra/postgres/taxlot_repo.go` | CREATE | Postgres TaxLotRepository (reuses `txContextKey` + `getQueryer`) |
| `cmd/api/main.go` | MODIFY | Wire TaxLotRepo + register hook |
| `migrations/000013_tax_lots.up.sql` | CREATE | Schema: tax_lots, lot_disposals, lot_override_history, tax_lots_effective view |
| `migrations/000014_genesis_lots.up.sql` | CREATE | Backfill lots for existing balances |

**Reuse existing code:**
- `pkg/money.CalcUSDValue()` — USD calculation helper
- `infra/postgres/ledger_repo.go` `getQueryer(ctx)` pattern — for TaxLotRepository
- `ledger.Entry.USDRate` — cost basis / proceeds per unit
- `ledger.AccountTypeCryptoWallet` — filter for lot-eligible accounts

---

## Verification

1. `cd apps/backend && go build ./...` — compiles
2. `cd apps/backend && go test ./internal/ledger/...` — unit tests pass
3. `just migrate-up` — migrations 000013 + 000014 apply cleanly
4. `just migrate-down` then `just migrate-up` — reversible
5. Integration tests: `cd apps/backend && go test -tags integration ./internal/ledger/...`
6. Manual test: `just dev` → create wallet via API → sync blockchain transactions → verify tax lots created in DB:
   ```sql
   SELECT id, asset, quantity_acquired, quantity_remaining, auto_cost_basis_source
   FROM tax_lots ORDER BY acquired_at;
   ```
7. Verify existing tests pass: `cd apps/backend && go test ./...`
