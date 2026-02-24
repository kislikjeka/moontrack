# Fix DeFi Sync Errors: Handler Registration + VARCHAR(20) Constraint

## Context

Wallet sync for DeFi transactions is completely blocked by two errors found in Loki logs:

1. **`no handler registered for transaction type: defi_deposit`** (tx `e1cc4d90`) — DeFi handlers are fully implemented but the Docker container runs the old binary without them. The `cmd/api/main.go` changes are uncommitted.

2. **`value too long for type character varying(20)`** (tx `d8c236e8`) — Confirmed: `asset_id` is the **only** VARCHAR(20) column in the `accounts` table (verified via `information_schema.columns`). The symbol comes from Zerion API's `FungibleInfo.Symbol` field with zero length validation. DeFi LP/derivative tokens can have long symbols (existing data shows `aBascbBTC` at 9 chars, but unbounded from Zerion).

## Fix 1: Commit + Rebuild Backend

The DeFi handler code already exists uncommitted:
- `apps/backend/cmd/api/main.go` — adds `defi` import + registers 3 handlers (deposit/withdraw/claim)
- `apps/backend/internal/module/defi/` — complete handler implementations (untracked)

**Action:** Commit all pending DeFi code, rebuild, restart.

## Fix 2: Migration — Widen `asset_id` VARCHAR(20) → VARCHAR(50)

3 tables affected (all in `000001_create_schema.up.sql`):
- `accounts.asset_id` (line 33)
- `entries.asset_id` (line 74)
- `account_balances.asset_id` (line 90)

### New file: `apps/backend/migrations/000017_widen_asset_id.up.sql`
```sql
ALTER TABLE accounts ALTER COLUMN asset_id TYPE VARCHAR(50);
ALTER TABLE entries ALTER COLUMN asset_id TYPE VARCHAR(50);
ALTER TABLE account_balances ALTER COLUMN asset_id TYPE VARCHAR(50);
```

### New file: `apps/backend/migrations/000017_widen_asset_id.down.sql`
```sql
ALTER TABLE accounts ALTER COLUMN asset_id TYPE VARCHAR(20);
ALTER TABLE entries ALTER COLUMN asset_id TYPE VARCHAR(20);
ALTER TABLE account_balances ALTER COLUMN asset_id TYPE VARCHAR(20);
```

**Why VARCHAR(50):** Most symbols are 3-10 chars but LP tokens can be longer. 50 is generous. PostgreSQL widening a VARCHAR is an in-place metadata change (no table rewrite).

## Execution Steps

1. Create migration files `000017_widen_asset_id.{up,down}.sql`
2. Run `just migrate-up`
3. Commit everything (defi module + migration + main.go changes)
4. Rebuild + restart backend

## Verification

1. `just migrate-up` succeeds
2. `cd apps/backend && go build ./cmd/api/` compiles
3. Check Loki for handler registration:
   ```logql
   {service="backend"} |= "Registered defi"
   ```
4. Trigger wallet sync, verify no errors:
   ```logql
   {service="backend", level="ERROR", component="sync"} | json | wallet_id="6c4812ae-b226-4350-8ac6-5015c661e98f"
   ```
5. Verify DeFi transactions recorded:
   ```logql
   {service="backend", component="ledger"} | json | tx_type="defi_deposit"
   ```
