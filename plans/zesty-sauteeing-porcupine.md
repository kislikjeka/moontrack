# Raw Transaction Proxy: Two-Phase Sync

## Context

The current sync system processes Zerion transactions in real-time, but the ledger requires strictly chronological (old-to-new) ordering for correct balance validation, FIFO tax lots, and cost basis tracking. Zerion returns newest-first data, and within a single batch the system reverses the order -- but across multiple pages/syncs, transactions may arrive out of order.

The current workaround is auto-creating genesis transactions when negative balances are detected. This produces multiple synthetic lots with inaccurate cost basis (`usd_rate=0`), making PnL/tax data unreliable. Additionally, there's no way to replay/re-process transactions when handler logic changes.

**Solution:** Split sync into two phases -- first collect ALL raw transactions, then process them in correct order. A reconciliation step compares calculated flows with on-chain balances (via Zerion Positions API) and creates at most ONE genesis per asset to cover any history gaps.

## Decisions

- Reconciliation runs only on initial sync and manual reprocess (not periodic)
- Negative deltas (on-chain < calculated) are ignored and logged as warnings
- Reprocess HTTP endpoint is deferred -- we build only the internal capability
- Ledger core, handlers, and tax lot hook remain UNCHANGED

---

## Step 1: Database Migration

**File:** `apps/backend/migrations/000019_raw_transaction_proxy.up.sql` (NEW)
**File:** `apps/backend/migrations/000019_raw_transaction_proxy.down.sql` (NEW)

```sql
-- raw_transactions table
CREATE TABLE raw_transactions (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    wallet_id         UUID NOT NULL REFERENCES wallets(id) ON DELETE CASCADE,
    zerion_id         VARCHAR(255) NOT NULL,
    tx_hash           VARCHAR(255) NOT NULL,
    chain_id          VARCHAR(50) NOT NULL,
    operation_type    VARCHAR(50) NOT NULL,
    mined_at          TIMESTAMPTZ NOT NULL,
    status            VARCHAR(20) NOT NULL DEFAULT 'confirmed',
    raw_json          JSONB NOT NULL,
    processing_status VARCHAR(20) NOT NULL DEFAULT 'pending'
                      CHECK (processing_status IN ('pending', 'processed', 'skipped', 'error')),
    processing_error  TEXT,
    ledger_tx_id      UUID REFERENCES transactions(id),
    is_synthetic      BOOLEAN NOT NULL DEFAULT false,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at      TIMESTAMPTZ,
    UNIQUE(wallet_id, zerion_id)
);

CREATE INDEX idx_raw_tx_wallet_mined ON raw_transactions(wallet_id, mined_at ASC);
CREATE INDEX idx_raw_tx_wallet_pending ON raw_transactions(wallet_id, mined_at ASC)
    WHERE processing_status = 'pending';

-- Wallet: add sync_phase and collect_cursor_at
ALTER TABLE wallets
    ADD COLUMN sync_phase VARCHAR(20) NOT NULL DEFAULT 'idle'
    CHECK (sync_phase IN ('idle', 'collecting', 'reconciling', 'processing', 'synced')),
    ADD COLUMN collect_cursor_at TIMESTAMPTZ;

-- Wipe function for replay capability (deletes in FK-safe order)
CREATE OR REPLACE FUNCTION wipe_wallet_ledger(p_wallet_id UUID) RETURNS void AS $$
DECLARE
    v_tx_ids UUID[];
    v_account_ids UUID[];
BEGIN
    SELECT array_agg(id) INTO v_tx_ids
    FROM transactions
    WHERE wallet_id = p_wallet_id AND source IN ('zerion', 'sync_genesis');

    IF v_tx_ids IS NULL THEN RETURN; END IF;

    SELECT array_agg(id) INTO v_account_ids
    FROM accounts WHERE wallet_id = p_wallet_id;

    DELETE FROM lot_override_history
    WHERE lot_id IN (SELECT id FROM tax_lots WHERE transaction_id = ANY(v_tx_ids));

    DELETE FROM lot_disposals WHERE transaction_id = ANY(v_tx_ids);
    DELETE FROM tax_lots WHERE transaction_id = ANY(v_tx_ids);
    DELETE FROM entries WHERE transaction_id = ANY(v_tx_ids);
    DELETE FROM transactions WHERE id = ANY(v_tx_ids);

    IF v_account_ids IS NOT NULL THEN
        UPDATE account_balances
        SET balance = 0, usd_value = 0, last_updated = now()
        WHERE account_id = ANY(v_account_ids);
    END IF;

    UPDATE raw_transactions
    SET processing_status = 'pending', processing_error = NULL,
        ledger_tx_id = NULL, processed_at = NULL
    WHERE wallet_id = p_wallet_id;
END;
$$ LANGUAGE plpgsql;
```

---

## Step 2: Domain Models

**File:** `apps/backend/internal/platform/sync/model.go` (NEW)

New types:
- `SyncPhase` enum: `idle`, `collecting`, `reconciling`, `processing`, `synced`
- `ProcessingStatus` enum: `pending`, `processed`, `skipped`, `error`
- `RawTransaction` struct: ID, WalletID, ZerionID, TxHash, ChainID, OperationType, MinedAt, Status, RawJSON ([]byte), ProcessingStatus, ProcessingError, LedgerTxID, IsSynthetic, CreatedAt, ProcessedAt
- `AssetFlow` struct: ChainID, AssetID, ContractAddress, Decimals, Inflow (*big.Int), Outflow (*big.Int)
- `OnChainPosition` struct: ChainID, AssetSymbol, ContractAddress, Decimals, Quantity (*big.Int), USDPrice (*big.Int)

---

## Step 3: Raw Transaction Repository

**File:** `apps/backend/internal/platform/sync/raw_tx_port.go` (NEW)

Interface `RawTransactionRepository`:
- `UpsertRawTransaction(ctx, raw *RawTransaction) error` -- INSERT ON CONFLICT DO NOTHING
- `GetPendingByWallet(ctx, walletID) ([]*RawTransaction, error)` -- ORDER BY mined_at ASC
- `GetAllByWallet(ctx, walletID) ([]*RawTransaction, error)`
- `MarkProcessed(ctx, rawID, ledgerTxID) error`
- `MarkSkipped(ctx, rawID, reason) error`
- `MarkError(ctx, rawID, errMsg) error`
- `ResetProcessingStatus(ctx, walletID) error`
- `GetEarliestMinedAt(ctx, walletID) (*time.Time, error)`
- `DeleteSyntheticByWallet(ctx, walletID) error`

**File:** `apps/backend/internal/infra/postgres/raw_tx_repo.go` (NEW)

Postgres implementation following existing repo patterns (pgxpool, raw SQL, pgx.Row scanning).

---

## Step 4: Zerion Positions API

**File:** `apps/backend/internal/infra/gateway/zerion/types.go` (MODIFY)

Add types:
```go
type PositionResponse struct { Links Links; Data []PositionData }
type PositionData struct { Type, ID string; Attributes PositionAttributes; Relationships Relationships }
type PositionAttributes struct { PositionType string; Quantity Quantity; Value *float64; Price float64; FungibleInfo *FungibleInfo }
```

**File:** `apps/backend/internal/infra/gateway/zerion/client.go` (MODIFY)

Add method:
```go
func (c *Client) GetPositions(ctx, address string, chainIDs []string) ([]PositionData, error)
```
- Endpoint: `GET /v1/wallets/{address}/positions/`
- Params: `filter[position_types]=wallet`, `filter[chain_ids]=...`, `filter[trash]=only_non_trash`
- Same pagination pattern as GetTransactions (follow Links.Next)

**File:** `apps/backend/internal/infra/gateway/zerion/adapter.go` (MODIFY)

Add method implementing `PositionDataProvider`:
```go
func (a *SyncAdapter) GetPositions(ctx, address string) ([]OnChainPosition, error)
```
Converts Zerion positions to domain `OnChainPosition` type. Filters by `wallet.IsValidChain()`.

**File:** `apps/backend/internal/platform/sync/port.go` (MODIFY)

Add interface:
```go
type PositionDataProvider interface {
    GetPositions(ctx context.Context, address string) ([]OnChainPosition, error)
}
```

Extend `WalletRepository` with:
```go
SetSyncPhase(ctx, walletID, phase string) error
SetCollectCursor(ctx, walletID, cursor time.Time) error
WipeWalletLedger(ctx, walletID) error
```

---

## Step 5: Wallet Model & Repo Updates

**File:** `apps/backend/internal/platform/wallet/model.go` (MODIFY)

Add fields to Wallet struct:
```go
SyncPhase       string     `json:"sync_phase" db:"sync_phase"`
CollectCursorAt *time.Time `json:"collect_cursor_at,omitempty" db:"collect_cursor_at"`
```

**File:** `apps/backend/internal/infra/postgres/wallet_repo.go` (MODIFY)

- Update all SELECT queries to include `sync_phase`, `collect_cursor_at`
- Update all Scan calls for the new columns
- Add `SetSyncPhase()` method
- Add `SetCollectCursor()` method
- Add `WipeWalletLedger()` method -- calls `SELECT wipe_wallet_ledger($1)`

---

## Step 6: Collector

**File:** `apps/backend/internal/platform/sync/collector.go` (NEW)

```go
type Collector struct {
    zerionProvider TransactionDataProvider
    rawTxRepo      RawTransactionRepository
    walletRepo     WalletRepository
    logger         *logger.Logger
}
```

**`CollectAll(ctx, wallet)`** -- Initial full collection:
1. Set sync_phase = 'collecting'
2. Calculate `since = time.Now().Add(-config.InitialSyncLookback)`
3. Call `zerionProvider.GetTransactions(ctx, w.Address, since)`
4. For each DecodedTransaction: serialize to JSON, create RawTransaction, upsert
5. Update `collect_cursor_at` to max(mined_at)
6. Return count

**`CollectIncremental(ctx, wallet)`** -- New transactions only:
- Same flow but uses `wallet.CollectCursorAt` as `since` parameter

Key: serialization of `DecodedTransaction` to JSON for `raw_json` field. We store the domain struct, not the raw Zerion API response, so the Processor can deserialize directly.

---

## Step 7: Reconciler

**File:** `apps/backend/internal/platform/sync/reconciler.go` (NEW)

```go
type Reconciler struct {
    rawTxRepo   RawTransactionRepository
    posProvider PositionDataProvider
    logger      *logger.Logger
}
```

**`Reconcile(ctx, wallet)`**:
1. Set sync_phase = 'reconciling'
2. Delete existing synthetic raw txs: `rawTxRepo.DeleteSyntheticByWallet()`
3. Load all raw txs: `rawTxRepo.GetAllByWallet()`
4. Calculate net flows per (chain_id, asset_symbol):
   - Deserialize each RawJSON back to DecodedTransaction
   - For each transfer: accumulate Inflow (direction=in) or Outflow (direction=out)
   - Also count fee outflows for native asset
5. Fetch on-chain positions: `posProvider.GetPositions(ctx, w.Address)`
6. For each on-chain position:
   - Find matching flow by (chain_id, asset_symbol)
   - `delta = on_chain_quantity - (inflow - outflow)`
   - If `delta > 0`: create ONE synthetic genesis RawTransaction
     - `zerion_id = "genesis:{wallet_id}:{chain_id}:{asset}"`
     - `mined_at = earliest_raw_tx.mined_at - 1 second`
     - `is_synthetic = true`
     - `raw_json` = serialized DecodedTransaction with single "in" transfer for delta amount
   - If `delta < 0`: log warning, ignore (per design decision)
   - If `delta == 0`: skip (complete history)

**Important nuance:** The net flow calculation must handle amount precision correctly. Raw transactions store amounts in base units (wei, satoshi) as `*big.Int`. On-chain positions from Zerion come as `Quantity.Int` (also base units string). Direct comparison is valid.

---

## Step 8: Processor

**File:** `apps/backend/internal/platform/sync/processor.go` (NEW)

```go
type Processor struct {
    rawTxRepo       RawTransactionRepository
    walletRepo      WalletRepository
    zerionProcessor *ZerionProcessor  // reused from existing code
    ledgerSvc       LedgerService
    logger          *logger.Logger
}
```

**`ProcessAll(ctx, wallet)`**:
1. Set sync_phase = 'processing'
2. Get pending raws: `rawTxRepo.GetPendingByWallet(ctx, w.ID)` -- ordered by mined_at ASC
3. Sort with same logic as current service (stable sort: mined_at ASC, inflows before outflows)
4. For each raw tx:
   - Deserialize `RawJSON` back to `DecodedTransaction`
   - **If `is_synthetic` (genesis):**
     - Build genesis rawData map (same format as current `createGenesisBalance`)
     - Call `ledgerSvc.RecordTransaction(TxTypeGenesisBalance, "sync_genesis", ...)`
   - **Else (regular tx):**
     - Call `zerionProcessor.ProcessTransaction(ctx, w, decodedTx)` -- UNCHANGED
   - On success: `rawTxRepo.MarkProcessed(ctx, raw.ID, ledgerTxID)`
   - On duplicate error (23505): `rawTxRepo.MarkProcessed()` (idempotent)
   - On error: `rawTxRepo.MarkError(ctx, raw.ID, err.Error())`
   - Break after 5 consecutive errors
5. Update `last_sync_at` to latest successful mined_at
6. Set sync_phase = 'synced'

**Key:** `zerionProcessor.ProcessTransaction()` is reused as-is. All classification, internal transfer detection, raw data building, and ledger recording remain identical. The processor just feeds it `DecodedTransaction` objects from the DB instead of directly from the API.

---

## Step 9: Service Refactor

**File:** `apps/backend/internal/platform/sync/service.go` (MODIFY)

### New fields on Service
```go
type Service struct {
    // ... existing fields ...
    rawTxRepo   RawTransactionRepository  // NEW
    posProvider PositionDataProvider       // NEW
    collector   *Collector                 // NEW
    reconciler  *Reconciler               // NEW
    processor   *Processor                // NEW
}
```

### Modified NewService signature
```go
func NewService(
    config *Config,
    walletRepo WalletRepository,
    ledgerSvc LedgerService,
    assetSvc AssetService,
    logger *logger.Logger,
    zerionProvider TransactionDataProvider,
    posProvider PositionDataProvider,     // NEW
    rawTxRepo RawTransactionRepository,  // NEW
) *Service
```

Constructor creates Collector, Reconciler, Processor internally.

### Modified syncWallet()

Replace current implementation with:
```go
func (s *Service) syncWallet(ctx context.Context, w *wallet.Wallet) error {
    claimed := s.walletRepo.ClaimWalletForSync(ctx, w.ID)
    if !claimed { return nil }

    isInitial := w.LastSyncAt == nil

    if isInitial {
        // Phase 1: Collect all raw transactions
        s.collector.CollectAll(ctx, w)
        // Phase 2: Reconcile with on-chain (creates genesis raws)
        s.reconciler.Reconcile(ctx, w)
        // Phase 3: Process all in chronological order
        s.processor.ProcessAll(ctx, w)
    } else {
        // Incremental: collect new + process
        s.collector.CollectIncremental(ctx, w)
        s.processor.ProcessAll(ctx, w)
    }
}
```

### Keep old genesis flow as dead code initially
The `createGenesisBalance()` method and `operationPriority()` function stay in service.go. `operationPriority()` is reused by the Processor for sorting. `createGenesisBalance()` becomes unused but can be removed in a cleanup pass.

---

## Step 10: DI Wiring

**File:** `apps/backend/cmd/api/main.go` (MODIFY)

```go
// After walletRepo:
rawTxRepo := postgres.NewRawTransactionRepository(db.Pool)

// After zerionProvider:
// zerionAdapter already exists as SyncAdapter; it now also implements PositionDataProvider

// Modified sync service creation:
syncSvc = sync.NewService(
    syncConfig, walletRepo, ledgerSvc, syncAssetAdapter, log,
    zerionAdapter,  // TransactionDataProvider (existing)
    zerionAdapter,  // PositionDataProvider (new -- same adapter)
    rawTxRepo,      // RawTransactionRepository (new)
)
```

---

## Files Summary

### New files (10)
| File | Description |
|------|-------------|
| `migrations/000019_raw_transaction_proxy.up.sql` | Table + columns + wipe function |
| `migrations/000019_raw_transaction_proxy.down.sql` | Reverse migration |
| `internal/platform/sync/model.go` | Domain types (RawTransaction, SyncPhase, AssetFlow, OnChainPosition) |
| `internal/platform/sync/raw_tx_port.go` | RawTransactionRepository interface |
| `internal/infra/postgres/raw_tx_repo.go` | Postgres implementation |
| `internal/platform/sync/collector.go` | Phase 1: collect raw txs from Zerion |
| `internal/platform/sync/reconciler.go` | Phase 2: reconcile with on-chain balances |
| `internal/platform/sync/processor.go` | Phase 3: process raw txs through ledger |
| `internal/platform/sync/collector_test.go` | Unit tests |
| `internal/platform/sync/reconciler_test.go` | Unit tests |

### Modified files (8)
| File | Changes |
|------|---------|
| `internal/platform/sync/port.go` | Add PositionDataProvider interface, extend WalletRepository |
| `internal/platform/sync/service.go` | New fields, new constructor params, replace syncWallet() |
| `internal/platform/wallet/model.go` | Add SyncPhase + CollectCursorAt fields |
| `internal/infra/postgres/wallet_repo.go` | New columns in SELECT/Scan, 3 new methods |
| `internal/infra/gateway/zerion/types.go` | Add Position* types |
| `internal/infra/gateway/zerion/client.go` | Add GetPositions() method |
| `internal/infra/gateway/zerion/adapter.go` | Implement PositionDataProvider |
| `cmd/api/main.go` | Add rawTxRepo, pass new deps to NewService |

### Unchanged (critical -- by design)
| File | Reason |
|------|--------|
| `internal/ledger/service.go` | Core ledger stays intact |
| `internal/ledger/taxlot_hook.go` | Tax lot hook stays intact |
| `internal/ledger/model.go` | No model changes |
| `internal/module/genesis/handler.go` | Genesis handler reused as-is |
| `internal/platform/sync/zerion_processor.go` | Reused by Processor without changes |
| `internal/platform/sync/classifier.go` | Classification unchanged |
| All other module handlers | All handlers unchanged |

---

## Execution Plan: Iterations & Agents

### Iteration 1: Foundation Layer
**Goal:** Database schema + domain types + repository interfaces. After this iteration the project compiles and existing tests pass.

#### Agent A: Migration + Domain Models (sequential)
1. Write migration `000019_raw_transaction_proxy.up.sql` + `.down.sql`
2. Write `internal/platform/sync/model.go` (SyncPhase, ProcessingStatus, RawTransaction, AssetFlow, OnChainPosition)
3. Write `internal/platform/sync/raw_tx_port.go` (RawTransactionRepository interface)

#### Agent B: Wallet Model & Repo (parallel with A)
1. Modify `internal/platform/wallet/model.go` -- add SyncPhase + CollectCursorAt fields
2. Modify `internal/infra/postgres/wallet_repo.go`:
   - Update all SELECT/Scan to include `sync_phase`, `collect_cursor_at`
   - Add `SetSyncPhase()`, `SetCollectCursor()`, `WipeWalletLedger()` methods
3. Modify `internal/platform/sync/port.go` -- extend WalletRepository interface with 3 new methods

#### Agent C: Zerion Positions API (parallel with A and B)
1. Modify `internal/infra/gateway/zerion/types.go` -- add Position* types
2. Modify `internal/infra/gateway/zerion/client.go` -- add `GetPositions()` method
3. Modify `internal/infra/gateway/zerion/adapter.go` -- implement `GetPositions()` on SyncAdapter
4. Modify `internal/platform/sync/port.go` -- add `PositionDataProvider` interface

**Validation after Iteration 1:**
```
cd apps/backend && go build ./...          # Must compile
just backend-test                          # All existing tests pass
just migrate-up                            # Migration applies without errors
just db-connect                            # Verify: \d raw_transactions shows correct schema
                                           # Verify: \d wallets shows sync_phase, collect_cursor_at columns
                                           # Verify: \df wipe_wallet_ledger shows the function exists
```

**NOTE on port.go:** Agents B and C both modify `port.go`. Agent B adds WalletRepository methods, Agent C adds PositionDataProvider interface. These are non-overlapping changes (different sections of the file), but must be merged carefully. Assign Agent B to do WalletRepository changes first, Agent C adds PositionDataProvider after.

---

### Iteration 2: Infrastructure Layer
**Goal:** Postgres repository implementation for raw_transactions. After this iteration we have a working data layer with tests.

#### Agent D: Raw Transaction Repo Implementation
1. Write `internal/infra/postgres/raw_tx_repo.go`:
   - `NewRawTransactionRepository(pool *pgxpool.Pool)`
   - All methods from `RawTransactionRepository` interface
   - Follow exact patterns from `wallet_repo.go` and `ledger_repo.go`
   - Use `INSERT...ON CONFLICT (wallet_id, zerion_id) DO NOTHING` for UpsertRawTransaction
   - `GetPendingByWallet`: `SELECT ... WHERE processing_status = 'pending' ORDER BY mined_at ASC`
   - `WipeWalletLedger` on wallet_repo: `SELECT wipe_wallet_ledger($1)` (if not done in Iteration 1)

**Validation after Iteration 2:**
```
cd apps/backend && go build ./...          # Must compile
just backend-test                          # All existing tests pass
# Manually verify repo by inserting test data:
# INSERT INTO raw_transactions (wallet_id, zerion_id, tx_hash, chain_id, operation_type, mined_at, raw_json)
# VALUES (...) and query back
```

---

### Iteration 3: Business Logic Services
**Goal:** Collector, Reconciler, Processor -- the three sync phases. These are independent services that can be built in parallel.

#### Agent E: Collector (parallel with F and G)
1. Write `internal/platform/sync/collector.go`:
   - `NewCollector(zerionProvider, rawTxRepo, walletRepo, config, logger)`
   - `CollectAll(ctx, wallet)`:
     - Set sync_phase = 'collecting'
     - Fetch from Zerion using config.InitialSyncLookback
     - Serialize DecodedTransaction to JSON via `encoding/json.Marshal`
     - Upsert each as RawTransaction
     - Update collect_cursor_at
   - `CollectIncremental(ctx, wallet)`:
     - Same but uses wallet.CollectCursorAt as since
   - Helper: `decodedTxToRawTx(walletID, dt DecodedTransaction) *RawTransaction`
2. Write `internal/platform/sync/collector_test.go`:
   - Mock TransactionDataProvider returns known txs
   - Mock RawTransactionRepository captures upserts
   - Test: all txs stored with correct fields
   - Test: collect_cursor_at updated to max mined_at
   - Test: incremental uses CollectCursorAt not InitialSyncLookback
   - Test: empty response handled gracefully

#### Agent F: Reconciler (parallel with E and G)
1. Write `internal/platform/sync/reconciler.go`:
   - `NewReconciler(rawTxRepo, posProvider, walletRepo, logger)`
   - `Reconcile(ctx, wallet)`:
     - Set sync_phase = 'reconciling'
     - Delete old synthetics
     - Load all raw txs, deserialize each to DecodedTransaction
     - Calculate net flows: `map[chainID:assetSymbol]*AssetFlow`
     - Fetch on-chain positions
     - For each position: compute delta, create genesis raw if delta > 0
     - Ignore delta < 0 (log warning)
   - Helper: `calculateNetFlows(raws []*RawTransaction) (map[string]*AssetFlow, error)`
   - Helper: `buildGenesisRaw(walletID, chainID, asset, delta, earliestMinedAt) *RawTransaction`
2. Write `internal/platform/sync/reconciler_test.go`:
   - Test: delta > 0 creates exactly 1 genesis raw per asset
   - Test: delta == 0 creates no genesis
   - Test: delta < 0 logs warning, no adjustment created
   - Test: genesis mined_at = earliest_raw - 1 second
   - Test: multi-chain multi-asset scenario (3+ assets across 2 chains)
   - Test: empty raw txs + non-zero on-chain = genesis for each position
   - Test: re-reconciliation deletes old synthetics first
   - Test: net flow calculation includes fees as outflow

#### Agent G: Processor (parallel with E and F)
1. Write `internal/platform/sync/processor.go`:
   - `NewProcessor(rawTxRepo, walletRepo, zerionProcessor, ledgerSvc, logger)`
   - `ProcessAll(ctx, wallet)`:
     - Set sync_phase = 'processing'
     - Get pending raws ordered by mined_at ASC
     - Apply secondary sort: operationPriority (reuse from service.go)
     - For synthetic genesis: build rawData map, call ledgerSvc.RecordTransaction
     - For regular: call zerionProcessor.ProcessTransaction
     - Mark each raw as processed/skipped/error
     - Track lastSuccessfulMinedAt, update last_sync_at
     - Set sync_phase = 'synced'
   - Helper: `processGenesis(ctx, wallet, raw) (*uuid.UUID, error)` -- returns ledger tx ID
   - Helper: `processRegular(ctx, wallet, raw, dt DecodedTransaction) (*uuid.UUID, error)`
2. Write `internal/platform/sync/processor_test.go`:
   - Test: transactions processed in mined_at ASC order
   - Test: synthetic genesis processed via ledgerSvc.RecordTransaction
   - Test: regular tx processed via zerionProcessor.ProcessTransaction
   - Test: successful tx marked as processed with ledger_tx_id
   - Test: failed tx marked as error
   - Test: duplicate tx (23505) marked as processed (idempotent)
   - Test: 5+ consecutive errors stops processing
   - Test: last_sync_at updated to last successful mined_at

**Validation after Iteration 3:**
```
cd apps/backend && go build ./...          # Must compile
just backend-test                          # All tests pass (existing + new unit tests)
# Verify test coverage:
cd apps/backend && go test -v ./internal/platform/sync/... -count=1
# Expected: collector_test.go, reconciler_test.go, processor_test.go all PASS
```

---

### Iteration 4: Integration & Wiring
**Goal:** Wire everything together. Replace old syncWallet with new 3-phase flow. After this iteration the full sync works end-to-end.

#### Agent H: Service Refactor + DI Wiring (sequential, depends on E+F+G)
1. Modify `internal/platform/sync/service.go`:
   - Add new fields to Service: rawTxRepo, posProvider, collector, reconciler, processor
   - Update NewService signature: add `posProvider PositionDataProvider`, `rawTxRepo RawTransactionRepository`
   - In constructor: create Collector, Reconciler, Processor instances
   - **Replace `syncWallet()` body** with 3-phase flow:
     - Initial sync (LastSyncAt == nil): CollectAll → Reconcile → ProcessAll
     - Incremental sync: CollectIncremental → ProcessAll
   - Keep `operationPriority()` (used by Processor)
   - Keep `createGenesisBalance()` temporarily (mark as deprecated comment)
   - Update SyncWallet() if needed for manual trigger compatibility
2. Modify `cmd/api/main.go`:
   - Add `rawTxRepo := postgres.NewRawTransactionRepository(db.Pool)`
   - Update `sync.NewService()` call with new params
3. Modify existing `internal/platform/sync/service_test.go`:
   - Update mock setup for new NewService params
   - Add tests for initial vs incremental sync path selection

**Validation after Iteration 4:**
```
cd apps/backend && go build ./...          # Must compile
just backend-test                          # ALL tests pass

# Integration test with real infrastructure:
just dev                                   # Start full stack
# In another terminal:
# 1. Register user + create wallet via API
# 2. Trigger sync: POST /api/v1/wallets/{id}/sync
# 3. Watch logs: just logs | grep sync
# 4. Verify in DB:
just db-connect
SELECT sync_phase, sync_status, last_sync_at, collect_cursor_at FROM wallets;
SELECT count(*), processing_status FROM raw_transactions GROUP BY processing_status;
SELECT count(*), is_synthetic FROM raw_transactions GROUP BY is_synthetic;
SELECT count(*), type FROM transactions GROUP BY type;
SELECT id, asset, quantity_acquired, acquired_at, auto_cost_basis_source FROM tax_lots ORDER BY acquired_at;
```

---

### Iteration 5: End-to-End Validation
**Goal:** Full manual verification, edge case testing, cleanup.

#### Agent I: Final Validation (sequential)
1. E2E test: fresh wallet sync with real Zerion API
2. Verify reconciliation: compare ledger balances vs Zerion positions
3. Verify FIFO: tax lots ordered correctly (genesis first, then chronological)
4. Verify cost basis: genesis lots have `auto_cost_basis_source = 'genesis_approximation'`
5. Verify incremental: trigger second sync, confirm only new txs collected
6. Verify idempotency: re-trigger sync, confirm no duplicates
7. Loki logs verification:
   ```logql
   {service="backend", component="sync"} | json | msg=~".*phase.*|.*collect.*|.*reconcil.*|.*process.*"
   ```
8. Clean up deprecated code if everything passes

---

## Parallelization Summary

```
Iteration 1:  [Agent A: Migration+Models] ──┐
              [Agent B: Wallet changes]  ────┤── all parallel (port.go merge needed)
              [Agent C: Zerion positions] ───┘
                         │
                         ▼
Iteration 2:  [Agent D: Raw TX repo impl] ──── sequential (depends on Iter 1)
                         │
                         ▼
Iteration 3:  [Agent E: Collector]  ─────────┐
              [Agent F: Reconciler] ─────────┤── all parallel (independent services)
              [Agent G: Processor]  ─────────┘
                         │
                         ▼
Iteration 4:  [Agent H: Service + DI wiring] ── sequential (depends on Iter 3)
                         │
                         ▼
Iteration 5:  [Agent I: E2E validation] ──────── sequential (depends on Iter 4)
```

**Max parallelism:** 3 agents in Iteration 1, 3 agents in Iteration 3.
**Critical path:** A → D → (E|F|G) → H → I

---

## Validation Rules per Iteration

### After EVERY iteration:
- `cd apps/backend && go build ./...` -- **MUST compile, zero errors**
- `cd apps/backend && go vet ./...` -- **no vet warnings**
- `just backend-test` -- **all existing tests pass, no regressions**

### Iteration 1 specific:
- [ ] Migration applies: `just migrate-up` succeeds
- [ ] `\d raw_transactions` shows all columns with correct types
- [ ] `\d wallets` shows `sync_phase` (VARCHAR, DEFAULT 'idle') and `collect_cursor_at` (TIMESTAMPTZ)
- [ ] `\df wipe_wallet_ledger` shows function signature `(uuid) -> void`
- [ ] New Go types compile without unused import warnings
- [ ] PositionDataProvider interface defined in port.go
- [ ] WalletRepository interface extended with 3 new methods

### Iteration 2 specific:
- [ ] RawTransactionRepository fully implemented (all interface methods)
- [ ] Upsert idempotency: inserting same zerion_id twice doesn't error
- [ ] GetPendingByWallet returns results ordered by mined_at ASC
- [ ] No direct SQL in business logic layer (repo pattern respected)

### Iteration 3 specific:
- [ ] Collector unit tests pass: mock provider → mock repo, all fields correct
- [ ] Reconciler unit tests pass: delta calculations correct for multi-asset scenarios
- [ ] Processor unit tests pass: genesis processed via ledgerSvc, regular via zerionProcessor
- [ ] Net flow calculation accounts for fees (native asset outflow)
- [ ] Genesis raw mined_at = earliest_raw.mined_at - 1 second (never epoch/zero)
- [ ] No genesis created for delta <= 0

### Iteration 4 specific:
- [ ] NewService accepts and wires all new dependencies
- [ ] syncWallet() uses 3-phase flow for initial sync
- [ ] syncWallet() uses 2-phase flow for incremental sync
- [ ] main.go compiles with updated DI wiring
- [ ] Existing service_test.go tests updated and passing
- [ ] `operationPriority()` accessible to Processor (exported or in shared file)

### Iteration 5 specific:
- [ ] Fresh wallet sync completes: sync_phase goes idle→collecting→reconciling→processing→synced
- [ ] raw_transactions table populated with correct data
- [ ] Synthetic genesis records: at most 1 per (wallet, chain, asset)
- [ ] Ledger balances match Zerion positions API (delta = 0 for all assets)
- [ ] Tax lots: genesis lots have earliest acquired_at, correct FIFO ordering
- [ ] Incremental sync: only new txs added, no re-processing of old
- [ ] Double-sync: idempotent, no duplicates in raw_transactions or ledger
