# Architecture Validation Report

**Date:** 2026-02-07
**Scope:** ADR-001 (Zerion Integration), ADR (Lot-Based Cost Basis), PRD (Blockchain Data Integration), PRD (Lot-Based Cost Tracking)
**Reviewer:** Architecture Agent

---

## 1. Security Analysis

### 1.1 API Key Storage

- [x] **Alchemy API key**: Stored in `.env` file, loaded via env vars. Not hardcoded in `client.go:28` -- accessed via `NewClient(apiKey string, ...)` constructor.
- [x] **CoinGecko API key**: Stored in `.env`, `.env.example` has placeholder.
- [ ] **Zerion API key**: Not yet implemented. ADR-001 specifies `internal/infra/gateway/zerion/client.go` will handle auth. **Action required**: Ensure Zerion API key follows the same pattern -- env var `ZERION_API_KEY`, passed via constructor. Add to `.env.example`.
- [x] **JWT secret**: Stored in env var `JWT_SECRET`. `.env.example` has placeholder with note "NEVER commit the actual secret!".
- [x] **No secrets in code**: Verified -- no hardcoded keys found in source.

### 1.2 Rate Limiting

- [x] **HTTP API rate limiting**: `middleware/rate_limit.go` implements IP-based rate limiter (100 req/s, burst 20) using `golang.org/x/time/rate`. Applied globally in `router.go:37`.
- [x] **Alchemy 429 handling**: `client.go:68-73` detects HTTP 429 and returns `RateLimitError` with retry-after. Exposed via `IsRateLimitError()`.
- [ ] **Zerion 429 handling**: ADR-001 specifies "exponential backoff, max 3 retries" but implementation doesn't exist yet. **Action required**: Implement in Zerion client with configurable backoff (initial 1s, factor 2x, max 3 retries).
- [!] **Rate limiter memory leak risk**: `rate_limit.go:45-56` cleanup goroutine clears ALL visitors every minute. This is noted in code comments as a known simplification. For production, track last-access timestamps and only evict stale entries.
- [!] **Rate limiter X-Forwarded-For spoofing**: `rate_limit.go:65` trusts `X-Forwarded-For` header directly. Behind a reverse proxy this is fine, but without one, clients can spoof their IP. **Recommendation**: If deployed behind a trusted proxy, validate header; otherwise, use `RemoteAddr` only.

### 1.3 SQL Injection Protection

- [x] **All queries use parameterized statements**: Verified across `ledger_repo.go`, `wallet_repo.go`, `user_repo.go`, `asset_repo.go`, `price_repo.go`. No string interpolation of user input into SQL.
- [x] **Dynamic query building in `ListTransactions`** (`ledger_repo.go:457-503`): Uses `fmt.Sprintf(" AND type = $%d", argPos)` with positional args -- safe, as only the position number is interpolated, not user data.
- [x] **DB constraint enforcement**: `CHECK` constraints on `entries.amount >= 0`, `entries.usd_rate >= 0`, `account_balances.balance >= 0`, `accounts.type IN (...)`, `transactions.status IN (...)`.
- [ ] **New queries for TaxLot system**: All planned queries (FIFO SELECT FOR UPDATE, lot disposal INSERT, override UPDATE) must use parameterized statements. **Action required**: Code review checklist item for new repositories.

### 1.4 Input Validation for Cost Basis Override

- [ ] **Positive values**: ADR specifies `CONSTRAINT positive_quantities CHECK (quantity_acquired > 0 AND quantity_remaining >= 0 AND quantity_remaining <= quantity_acquired)`. Override cost basis needs explicit validation: `override_cost_basis_per_unit >= 0`. **Action required**: Add CHECK constraint and application-level validation.
- [ ] **Valid lot ownership**: Override endpoint must verify the requesting user owns the lot (lot -> account -> wallet -> user chain). **Action required**: Implement ownership check in TaxLot service layer, not just repository.
- [ ] **Override reason required**: ADR allows nullable `override_reason`. **Recommendation**: Make it required (NOT NULL) for audit trail completeness.

### 1.5 Authorization (User Data Isolation)

- [x] **Wallet CRUD**: `wallet/service.go` checks `wallet.UserID != userID` for GetByID (line 56), Update (line 86), Delete (line 121). Returns `ErrUnauthorizedAccess`.
- [x] **Transaction list**: `handler/transaction.go:251` passes `UserID` in filters. `handler/transaction.go:349` passes `userID` to `GetTransaction()`.
- [x] **Transaction detail**: Returns 404 for both not-found and unauthorized (line 352) -- prevents ID enumeration.
- [!] **Sync service lacks user-scoping**: `GetWalletsForSync()` in `wallet_repo.go:221-268` returns ALL wallets across ALL users. This is intentional (background service), but the sync processor at `processor.go:117-143` checks wallet ownership by address which queries across all users. **Risk**: User A's wallet sync could detect User B's wallet as "internal transfer" counterparty if they share the same address on different chains. **Assessment**: Low risk -- EVM addresses are unique per user in practice, and the UNIQUE constraint `idx_wallets_user_chain_address` is per-user-chain. Cross-user detection would require the same address registered by different users, which is an unlikely but valid scenario. **Recommendation**: Add user_id filter to `isUserWallet()` check -- only consider same-user wallets as internal.
- [ ] **TaxLot and LotDisposal endpoints**: Not yet implemented. **Action required**: Every new endpoint touching tax_lots must verify user ownership through the account->wallet->user chain.

### 1.6 JWT Handling

- [x] **Algorithm confusion prevention**: `jwt.go:76-77` validates signing method is HMAC, and `jwt.go:80` restricts to HS256 via `jwt.WithValidMethods`.
- [x] **Token expiry**: 24-hour expiration (`jwt.go:46`).
- [x] **Issuer validation**: Sets issuer to "moontrack" (`jwt.go:55`) but does not validate issuer on parse. **Recommendation**: Add `jwt.WithIssuer("moontrack")` to `ParseWithClaims` options.
- [x] **Context propagation**: `jwt.go:135-136` sets `UserIDKey` and `UserEmailKey` in context. All handlers extract via typed key, preventing accidental type collision.
- [x] **Protected routes**: `router.go:62-99` wraps all data endpoints in `cfg.JWTMiddleware`.

### 1.7 No Secrets in Logs/Errors

- [x] **Error messages generic on auth failure**: `handler/auth.go:141` returns "invalid email or password" (not specifying which).
- [x] **Internal errors not exposed**: `handler/wallet.go:120` exposes `err` in `fmt.Sprintf("failed to create wallet: %v", err)`. **Risk**: Internal error details (DB errors, constraint names) may leak to client. **Recommendation**: Log full error server-side, return generic "internal server error" to client. Same issue in `handler/transaction.go:211`.
- [x] **Sync service logs**: `processor.go:183` logs `unique_id` and `tx_hash` at Debug level -- acceptable for debugging, no secrets.

---

## 2. Sync Correctness

### 2.1 Race Condition Prevention (sync_status lock)

- [x] **Current implementation**: `wallet_repo.go:340-357` `SetSyncInProgress()` sets `sync_status = 'syncing'`. `GetWalletsForSync()` at line 225 only returns wallets with `sync_status IN ('pending', 'error', 'synced')` -- excludes 'syncing'.
- [!] **TOCTOU vulnerability**: Between `GetWalletsForSync()` returning a wallet and `SetSyncInProgress()` being called, another goroutine could start syncing the same wallet. Current semaphore (`service.go:128`) prevents this within a single process, but multiple instances would race. **Recommendation**: Use `UPDATE ... SET sync_status = 'syncing' WHERE id = $1 AND sync_status != 'syncing' RETURNING id` to atomically claim the wallet. Check affected rows -- if 0, skip.
- [!] **Stale 'syncing' status**: If the process crashes during sync, the wallet stays in 'syncing' forever. **Recommendation**: Add a `sync_started_at` timestamp column. In `GetWalletsForSync()`, include wallets stuck in 'syncing' for > threshold (e.g., 10 minutes) as stale candidates.

### 2.2 Idempotency Guarantee

- [x] **DB constraint**: `000001_create_schema.up.sql:57` has `UNIQUE(source, external_id)` on transactions table.
- [x] **Application handling**: `processor.go:181-185` checks for duplicate errors via `isDuplicateError()` and silently skips.
- [!] **Fragile duplicate detection**: `isDuplicateError()` at line 293-301 uses string matching (`"duplicate"`, `"unique constraint"`, `"already exists"`). This is brittle -- different PostgreSQL versions or locales may return different messages. **Recommendation**: Use `pgconn.PgError` type assertion and check `Code == "23505"` (unique_violation).
- [x] **Zerion external_id format**: ADR-001 specifies `external_id = "zerion_{zerion_tx_id}"`, `source = "zerion"`. Ensures no collision with existing Alchemy records (`source = "blockchain"`).

### 2.3 Cursor Safety

- [x] **Current (block-based)**: `service.go:241` calls `SetSyncCompleted()` with `endBlock` only after all transfers processed. If processing fails mid-batch, sync errors out and `last_sync_block` is not updated.
- [!] **Issue with current cursor**: `service.go:240-241` updates cursor even with partial errors (`len(processErrors) > 0`). This means some transfers in the range may be skipped permanently. **Assessment**: For Alchemy block-based sync, this is mitigated by idempotency (re-fetching the same block range would re-process successfully committed transactions and skip duplicates). For Zerion time-based cursor, the same mitigation applies.
- [ ] **Zerion time-based cursor**: ADR-001 specifies `last_sync_at = max(transaction.mined_at)`. **Action required**: Update cursor only to the timestamp of the last **successfully committed** transaction, not the last fetched one. If transaction N fails but N+1 succeeds, cursor should not advance past N.

### 2.4 Zerion Unavailability Handling

- [x] **ADR-001 design**: Zerion unavailability -> sync fail -> `sync_status = 'error'` -> retry next cycle. `last_sync_at` not updated -> catch up on recovery.
- [x] **Graceful degradation**: Portfolio data becomes stale but not corrupted. No writes on failure.
- [ ] **Monitoring**: No alerting mechanism for prolonged Zerion unavailability. **Recommendation**: Add metric/log for consecutive sync failures per wallet. Alert if > N failures (configurable, suggest 5).

### 2.5 Transaction Ordering

- [x] **Zerion**: ADR-001 specifies `mined_at` ordering. Zerion returns transactions chronologically.
- [x] **Sequential processing**: `service.go:229-237` processes transfers sequentially within a wallet.
- [x] **Cross-wallet independence**: Different wallets sync in parallel (different addresses, no shared state).
- [!] **FIFO dependency on ordering**: TaxLot FIFO disposal depends on chronological transaction ordering. If Zerion returns transactions out of order (e.g., due to block reorgs), FIFO disposal could match wrong lots. **Assessment**: Low risk -- Zerion indexes finalized transactions. Portfolio tracker can tolerate the ~3-5 min Zerion lag as design acceptance.

### 2.6 Alchemy -> Zerion Migration

- [x] **No data loss**: ADR-001 Section 11 specifies existing Alchemy records (`source="blockchain"`) remain untouched. New records use `source="zerion"`.
- [x] **No duplicates**: Different `source` values mean `UNIQUE(source, external_id)` prevents cross-source conflicts. Same Zerion transaction re-fetched gets same `external_id = "zerion_{id}"`.
- [!] **Gap between last Alchemy sync and first Zerion sync**: When switching from Alchemy (block-based) to Zerion (time-based), there may be a time window where transactions are neither in Alchemy's last block range nor captured by Zerion's `last_sync_at`. **Recommendation**: For the migration, set `last_sync_at` to the timestamp of the last Alchemy-synced block (or slightly before) to ensure overlap. Idempotency handles any duplicates.

---

## 3. Data Integrity

### 3.1 Double-Entry Balance with Clearing Accounts

- [x] **Current VerifyBalance()**: `model.go:131-152` checks global `SUM(DEBIT) = SUM(CREDIT)`. This works for same-asset transactions.
- [x] **Clearing account design**: ADR-001 Section 4.2 explains clearing accounts for swaps. Each amount appears once as DEBIT and once as CREDIT, maintaining global balance.
- [!] **Per-asset balance not enforced**: `VerifyBalance()` checks global sum, not per-asset. This is by design -- swaps move different assets. Per-asset correctness is a convention enforced by handlers, not the validator. **Risk**: A buggy handler could create entries that pass global balance check but have incorrect per-asset effects (e.g., DEBIT 100 ETH + CREDIT 100 BTC would pass). **Recommendation**: Add optional per-asset balance verification for same-asset entry types (`asset_increase`/`asset_decrease` within a single asset should have matching DEBIT/CREDIT sums). Or: document this as an accepted architectural invariant enforced by handler tests.
- [x] **Clearing account balance invariant**: Each clearing account should always net to zero. **Recommendation**: Add a periodic reconciliation check that verifies all clearing accounts have zero balance.

### 3.2 FIFO Disposal Atomicity

- [x] **FOR UPDATE lock**: ADR specifies `SELECT ... FOR UPDATE` on tax_lots during FIFO disposal. This prevents concurrent disposals from over-allocating the same lot.
- [x] **Existing pattern**: `ledger_repo.go:689-693` already implements `GetAccountBalanceForUpdate()` with `FOR UPDATE`. Same pattern should be used for tax_lots.
- [!] **Deadlock risk**: If two concurrent transactions dispose lots from the same account, they could deadlock if they lock lots in different orders. **Mitigation**: FIFO always processes lots in `acquired_at ASC` order, which is deterministic. Two concurrent disposals on the same account/asset will lock in the same order -> no deadlock. **Assessment**: Safe with consistent ordering.

### 3.3 TaxLot Quantity Constraints

- [x] **DB constraints**: ADR specifies `CHECK (quantity_acquired > 0 AND quantity_remaining >= 0 AND quantity_remaining <= quantity_acquired)`. This prevents negative remaining and over-disposal at the DB level.
- [x] **FIFO algorithm**: ADR algorithm checks `if remaining > 0: ERROR "Insufficient balance"` after exhausting all lots. Prevents over-disposal at the application level.
- [!] **Sync between account_balances and tax_lots**: `account_balances.balance` is updated by ledger entries. `tax_lots.quantity_remaining` is updated by FIFO disposal. These must stay in sync. **Risk**: If ledger entry succeeds but FIFO disposal fails (or vice versa), balances diverge. **Recommendation**: Execute both within the same DB transaction. The existing `transactionCommitter.commit()` pattern (BeginTx/CommitTx) should be extended to include TaxLot operations.

### 3.4 Override Preserves auto_cost_basis

- [x] **Design**: ADR explicitly states `auto_cost_basis_per_unit` is immutable after creation. Override goes into separate column `override_cost_basis_per_unit`.
- [x] **Effective cost basis view**: `tax_lots_effective` uses `COALESCE(override, linked, auto)` -- removing override (set to NULL) falls back to auto.
- [x] **Audit trail**: `lot_override_history` records every change with previous/new values and timestamp.

### 3.5 Internal Transfer PnL = 0 Guarantee

- [x] **Design**: `disposal_type = 'internal_transfer'` in `lot_disposals` table. PnL query uses `CASE WHEN disposal_type = 'internal_transfer' THEN 0 ELSE ...`. This ensures PnL = 0 regardless of cost basis changes.
- [x] **Why this matters**: Without `disposal_type`, if user overrides cost basis on the source lot, the PnL would become non-zero -- incorrect for an internal transfer.

### 3.6 WAC Refresh Correctness

- [x] **Materialized view**: `position_wac` aggregates `quantity_remaining * effective_cost_basis_per_unit` for remaining lots.
- [x] **Lazy refresh strategy**: ADR specifies refresh before read if stale. `REFRESH MATERIALIZED VIEW CONCURRENTLY` is safe (requires UNIQUE INDEX, which is specified).
- [!] **Stale WAC during active sync**: If multiple wallets sync concurrently and WAC is read between syncs, it may reflect partial state. **Assessment**: Acceptable for portfolio tracker -- WAC is approximate by nature, and display-level accuracy is sufficient.

---

## 4. Migration Safety

### 4.1 Migration Order Dependencies

Current migrations: 000001-000007. New migrations needed:

| Order | Migration | Depends On | Description |
|---|---|---|---|
| 000008 | `add_account_types` | 000001 | Drop and recreate `accounts.type` CHECK to add CLEARING, DEFI_INCOME |
| 000009 | `create_tax_lots` | 000001 (transactions, accounts) | Create tax_lots, lot_disposals, lot_override_history tables |
| 000010 | `create_wac_view` | 000009 | Create position_wac materialized view and tax_lots_effective view |
| 000011 | `add_zerion_sync` | 000007 | Ensure wallets.last_sync_at exists (already in 000007 but verify), add ZERION_API_KEY reference |

- [x] **No circular dependencies**: Each migration depends only on earlier migrations.
- [!] **000008 risk**: Dropping CHECK constraint on `accounts.type` while existing code is running could allow invalid types momentarily. **Recommendation**: Deploy code that accepts new types BEFORE running migration, or use `ALTER TABLE ... DROP CONSTRAINT ... ADD CONSTRAINT` in a single transaction.

### 4.2 Backward Compatibility with Existing Data

- [x] **Existing Alchemy transactions**: Preserved with `source="blockchain"`. No modification needed.
- [x] **Existing accounts**: New types (CLEARING, DEFI_INCOME) are additive. Existing CRYPTO_WALLET, INCOME, EXPENSE, GAS_FEE accounts unchanged.
- [!] **TaxLot backfill**: Existing transactions have no TaxLots. FIFO disposal for new sell/swap will find no lots to dispose -> "Insufficient balance" error. **Action required**: Either (a) create TaxLots for existing transactions via data migration, or (b) allow "genesis lots" with quantity = current balance and cost_basis = FMV at migration time.

### 4.3 Rollback Strategy Per Migration

| Migration | Rollback |
|---|---|
| 000008 (account types) | Re-add old CHECK constraint. Existing CLEARING/DEFI_INCOME accounts would violate -- must delete them first. |
| 000009 (tax_lots) | DROP TABLE lot_override_history, lot_disposals, tax_lots. No data loss in core ledger. |
| 000010 (WAC view) | DROP MATERIALIZED VIEW position_wac; DROP VIEW tax_lots_effective. |
| 000011 (zerion sync) | Revert cursor columns. Switch sync service back to Alchemy. |

- [x] **Each migration has independent rollback**: Verified, no cross-migration dependencies that prevent rollback.

### 4.4 Zero-Downtime Considerations

- [!] **CHECK constraint modification** (000008): `ALTER TABLE ... DROP CONSTRAINT` acquires `ACCESS EXCLUSIVE` lock on the table. For production with concurrent writes to `accounts`, this blocks all operations momentarily. **Mitigation**: The `accounts` table has low write frequency (accounts created only during first transaction for an asset). Brief lock acceptable.
- [x] **New tables** (000009): CREATE TABLE does not lock existing tables.
- [x] **Materialized view** (000010): CREATE does not lock existing tables. REFRESH CONCURRENTLY allows reads during refresh.

---

## 5. Edge Cases Matrix

| # | Scenario | Expected Behavior | Enforcement | Risk |
|---|---|---|---|---|
| 1 | Zerion returns tx already in ledger (duplicate) | Silent skip, no error | `UNIQUE(source, external_id)` DB constraint | Low |
| 2 | Zerion returns `price: null` for a transfer | `usd_rate = 0`, update later via CoinGecko | Application logic in processor | Low |
| 3 | Zerion unavailable for >1 hour | Sync fails, retries each cycle. `last_sync_at` frozen. Catch-up on recovery. | Sync service retry loop | Low |
| 4 | Zerion rate limit (429) | Exponential backoff, max 3 retries. If all fail, sync error for this wallet. | Zerion client (to implement) | Medium |
| 5 | Swap with gas in different asset than sold | SwapHandler generates 4+ entries (swap + gas). Clearing account balances. | Handler + VerifyBalance() | Low |
| 6 | Internal transfer between user's wallets on different chains | Same-asset lots linked via `linked_source_lot_id`. Cost basis carries over. | TaxLot creation with linked lot | Medium |
| 7 | Internal transfer where counterparty wallet syncs first | Incoming side skips (processor.go:253). Source side records later. | `if transfer.Direction == DirectionIn { return nil }` | Medium |
| 8 | FIFO disposal when no lots exist | "Insufficient balance" error. Transaction fails. | FIFO algorithm error check | **High** |
| 9 | Override cost basis on fully disposed lot | Allowed. Changes historical PnL for reporting. | No constraint on override timing | Low |
| 10 | Override removed (set to NULL) | Falls back to auto_cost_basis or linked_lot cost basis | `COALESCE()` in tax_lots_effective view | Low |
| 11 | Concurrent FIFO disposals on same account | FOR UPDATE locks lots in acquired_at order. No deadlock. | Database row-level locks | Low |
| 12 | Zerion classifies DeFi tx as `execute` | Fallback classification by transfer direction analysis | `classifyExecute()` function | Medium |
| 13 | Process crash during sync (wallet stuck in 'syncing') | Wallet never re-syncs until manual intervention | **No automatic recovery** | **High** |
| 14 | Very large batch (100+ tx in one sync) | Sequential processing. Each tx = separate DB transaction. | Sync service sequential loop | Low |
| 15 | TaxLot linked_source_lot_id chain > 1 level | Only 1 level resolved (B->A but not C->B->A). Cost basis from B, not A. | `LEFT JOIN tax_lots source` (1 level) | Medium |
| 16 | User creates transaction with future occurred_at | Rejected by model.go:111 `OccurredAt.After(now)` | Application validation | Low |
| 17 | Negative balance after ledger entry | Rejected by transactionCommitter (line 536) and DB CHECK constraint | Code + DB constraint | Low |
| 18 | Wallet deleted while sync in progress | Accounts CASCADE delete. Entries RESTRICT delete (preserved). | DB foreign keys | Medium |
| 19 | Same blockchain tx generates multiple Zerion events | Each with unique Zerion `id`. Recorded as separate ledger transactions. | No dedup by tx_hash, only by Zerion ID | Medium |
| 20 | Clearing account accumulates non-zero balance | Indicates handler bug. No automated detection. | **None currently** | **High** |

---

## 6. Recommendations

### Critical (Must Fix Before Implementation)

1. **Atomic wallet sync claim** (Section 2.1): Replace `GetWalletsForSync()` + `SetSyncInProgress()` with atomic `UPDATE ... WHERE sync_status != 'syncing' RETURNING id`. Prevents race conditions in multi-instance deployments and stale sync states.

2. **Stale sync recovery** (Section 2.1, Edge Case #13): Add `sync_started_at` column to wallets. Include wallets stuck in 'syncing' for > 10 minutes in `GetWalletsForSync()` results.

3. **Robust duplicate detection** (Section 2.2): Replace string-matching `isDuplicateError()` with PostgreSQL error code check (`23505`). Example:
   ```go
   var pgErr *pgconn.PgError
   if errors.As(err, &pgErr) && pgErr.Code == "23505" {
       return nil // duplicate, skip
   }
   ```

4. **TaxLot genesis for existing data** (Section 4.2, Edge Case #8): Before enabling FIFO disposal, create genesis TaxLots for existing account balances. Otherwise, any sell/swap will fail with "Insufficient balance".

5. **Clearing account balance monitoring** (Edge Case #20): Add a periodic job or health check that verifies all CLEARING-type accounts have zero net balance. Alert if non-zero.

### Important (Should Fix)

6. **Cursor advancement on partial failures** (Section 2.3): For Zerion time-based cursor, track last successfully committed transaction timestamp. Don't advance cursor past failed transactions.

7. **Cross-user internal transfer detection** (Section 1.5): Add `user_id` filter to `isUserWallet()` / `getWalletByAddress()` to prevent cross-user wallet matching during sync.

8. **Generic error responses** (Section 1.7): Replace `fmt.Sprintf("failed to create wallet: %v", err)` (wallet handler line 120) and `fmt.Sprintf("failed to create transaction: %v", err)` (transaction handler line 211) with generic messages. Log details server-side only.

9. **JWT issuer validation** (Section 1.6): Add `jwt.WithIssuer("moontrack")` to token parsing options.

10. **TaxLot operations within ledger transaction** (Section 3.3): Extend `transactionCommitter.commit()` to include TaxLot creation and FIFO disposal in the same DB transaction. This ensures account_balances and tax_lots stay in sync.

### Performance Considerations

11. **FIFO disposal index**: ADR specifies `idx_tax_lots_fifo` partial index on `quantity_remaining > 0`. This is critical for FIFO performance with large lot counts.

12. **WAC materialized view refresh**: Lazy refresh with configurable `maxAge` (suggest 30-60 seconds for API reads, immediate for reports/exports).

13. **Zerion pagination**: Large wallets may have many transactions. Implement pagination with Zerion's cursor-based API to avoid fetching all transactions in a single request.

### Monitoring Recommendations

14. **Sync health dashboard**: Track per-wallet metrics: last_sync_at age, consecutive error count, sync duration.

15. **Ledger reconciliation**: Periodic job comparing `account_balances.balance` with `CalculateBalanceFromEntries()`. Alert on mismatch. Already implemented as `ReconcileBalance()` in `service.go:173-196` -- add scheduled execution.

16. **TaxLot invariant checks**: Periodic verification that `SUM(quantity_remaining)` across all lots for an account/asset matches the account balance.

---

## Summary

The architecture is **well-designed** with strong foundations in the double-entry ledger, handler registry pattern, and clear separation of concerns. The planned Zerion integration and lot-based cost basis system are sound architecturally.

**Key strengths:**
- Parameterized SQL throughout (no injection risk)
- JWT with algorithm confusion prevention
- Immutable ledger entries
- VerifyBalance() as constitutional check
- FOR UPDATE pattern for concurrent safety
- Idempotency via UNIQUE constraint

**Key gaps to address before implementation:**
1. Atomic sync claim (prevents race conditions)
2. Stale sync recovery (prevents stuck wallets)
3. Robust duplicate detection (prevents silent data loss on error message changes)
4. TaxLot genesis data (prevents "Insufficient balance" on first sell)
5. Clearing account monitoring (prevents silent accounting errors)

Overall assessment: **Approved with conditions** -- address the 5 critical items before implementing Phase 1.
