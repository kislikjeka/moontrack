# Fix: Cost Basis page returns 500 — WAC materialized view decimal parsing

## Context

The Cost Basis page (`/cost-basis`) shows empty state because `GET /api/v1/positions/wac` returns **HTTP 500** on every request. Loki logs confirm consistent 500s.

**Root cause**: The `position_wac` materialized view performs division (`SUM(qty * cost) / SUM(qty)`) on `NUMERIC(78,0)` columns. PostgreSQL auto-promotes the result to a decimal (`99285449.000000000000`). The Go code scans it as a string and parses with `big.Int.SetString()`, which **cannot parse decimal strings** → parse failure → 500.

**Why truncation is correct**: All USD values in the system are stored as `big.Int` scaled 10^8. Division is handled via `big.Int.Div()` (truncation) everywhere — in `CalcUSDValue()`, `weightedAvgCostBasis()`, and all module handlers. The `FormatUSD()` comment explicitly says "truncate, matching financial convention". WAC is an informational metric (actual PnL uses FIFO lots), and precision loss at 10^-8 scale is negligible.

## Changes

### 1. New migration: `000016_fix_position_wac_integer.up.sql`
- Recreate `position_wac` materialized view with `TRUNC(..., 0)` on the division result
- This matches the behavior of `big.Int.Div()` used in Go's `weightedAvgCostBasis()`

### 2. Defensive Go parsing: `infra/postgres/taxlot_repo.go`
- Add `truncateDecimal()` helper that strips `.xxx` suffix before `big.Int.SetString()`
- Apply to both `total_quantity` and `weighted_avg_cost` parsing in `GetWAC()`
- Defense in depth — even if someone changes the view later, parsing won't break

## Files to modify

- `apps/backend/migrations/000016_fix_position_wac_integer.up.sql` (new)
- `apps/backend/migrations/000016_fix_position_wac_integer.down.sql` (new)
- `apps/backend/internal/infra/postgres/taxlot_repo.go` — add `strings` import, `truncateDecimal()`, apply to WAC scan

## Verification

1. `cd apps/backend && go build ./...` — compilation check
2. `just migrate-up` — apply migration
3. Query DB directly: `SELECT * FROM position_wac` — verify integer values (no decimals)
4. Check Loki: `{service="backend"} | json | path="/api/v1/positions/wac"` — should return 200
5. Open Cost Basis page in browser — should show positions with WAC data
