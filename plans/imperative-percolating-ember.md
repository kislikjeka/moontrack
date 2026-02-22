# Plan: Observability Skill Validation & Improvement

## Context

The observability-debugging skill (`.claude/skills/observability-debugging/SKILL.md`) was validated against the live Loki instance and the actual backend codebase. **5 bugs and several structural gaps were found** — the skill contains incorrect component names, wrong field names, case-sensitive level mismatches, missing MCP tools documentation, and no query optimization guidance. These issues cause queries to silently return no results or miss data.

## Validation Results Summary

### Bug 1: Level label values are UPPERCASE (all query examples affected)
- **Loki label values**: `DEBUG`, `INFO`, `WARN`, `ERROR` (confirmed via `loki_label_values`)
- **Skill examples**: `level="error"`, `level=~"error|warn"` (lowercase — **broken as stream selectors**)
- `{service="backend", level="error"}` → **no results**
- `{service="backend", level="ERROR"}` → **returns results**
- Note: `| json | level="error"` happens to work due to Loki's `level_extracted` field collision behavior, but this is fragile and undocumented

### Bug 2: Component table is wrong (6 missing, 3 nonexistent)
**Listed in skill but NOT in Loki**: `http`, `auth`, `price`
**Missing from skill (confirmed in Loki)**: `cache`, `coingecko`, `price_updater`, `taxlot_hook`, `transfer`, `zerion`, `asset`

### Bug 3: `ip` field doesn't exist
Actual field name is `remote_addr` (format: `IP:port`)

### Bug 4: Missing useful log fields
Not documented: `bytes`, `user_agent`, `remote_addr`, `source`, `wallet_id`, `asset_id`, `account_id`, `account_code`, `address`, `status_code`, `count`

### Bug 5: No query optimization guidance
`level` and `component` are promoted to indexed Loki labels by Promtail, but the skill uses `| json` pipeline for them everywhere — missing the faster stream selector approach

### Bug 6: `start`/`end` time format documentation is wrong
- Skill claims: "RFC3339 or relative like `1h`"
- **Relative time (`1h`, `24h`) does NOT work** — returns "unsupported time format"
- **Unix timestamps also don't work**
- Accepted formats (tested):
  - RFC3339: `2026-02-22T00:00:00Z` ✅
  - Date-only: `2026-02-22` ✅
- Max query range: 30d1h (Loki server limit)

### Structural gaps
- Only `loki_query` tool documented; `loki_label_names` and `loki_label_values` are undocumented
- No mention of Loki's 30d query time range limit
- Missing debugging scenarios for price fetching, tax lots, circuit breakers

---

## Implementation Plan

### Step 1: Rewrite SKILL.md

File: `.claude/skills/observability-debugging/SKILL.md`

#### 1.1 MCP Tools section — expand to cover all 3 tools
- Rename to "MCP Tools" (plural)
- Add `loki_label_names` — list available labels (useful for discovery)
- Add `loki_label_values` — get values for a label (useful for discovering components, levels)
- Fix `start`/`end` parameter docs:
  - Remove claim about relative time (`1h`) — **doesn't work**
  - Document accepted formats: RFC3339 (`2026-02-22T12:00:00Z`) and date-only (`2026-02-22`)
  - Default: 1h ago (server-side default when omitted)
  - Note 30d max query range (Loki server limit, returns error if exceeded)

#### 1.2 LogQL Quick Reference — fix all examples

**Stream selectors**: fix to use UPPERCASE levels and valid components:
```logql
{service="backend"}                        # All backend logs
{service="backend", component="ledger"}    # Ledger component only
{service="backend", level="ERROR"}         # Errors only (indexed label)
```

**JSON pipeline**: fix level case, add optimization note:
```logql
{service="backend"} | json | level="ERROR"                    # Filter by level
{service="backend"} | json | status >= 500                    # 5xx responses
```

**Combining filters**: use stream selectors for indexed fields:
```logql
{service="backend", component="sync", level="ERROR"}
{service="backend", level=~"ERROR|WARN"} | json | duration_ms > 500
```

#### 1.3 Add new section: Query Optimization
Insert between LogQL reference and Components table:
- Explain indexed labels (`service`, `level`, `component`) vs JSON pipeline fields
- Rule: put `level` and `component` in `{...}`, everything else after `| json`
- Note: HTTP middleware logs have no `component` label — filter by `method`/`path`/`status` instead

#### 1.4 Backend Components table — complete rewrite
Replace with verified list (11 components from Loki over 30d):

| Component | Layer | Description |
|-----------|-------|-------------|
| `ledger` | ledger | Core double-entry accounting engine |
| `taxlot_hook` | ledger | FIFO tax lot creation & disposal |
| `wallet` | platform | Wallet CRUD & ownership |
| `user` | platform | User service |
| `asset` | platform | Asset registry, price lookups, circuit breaker |
| `price_updater` | platform | Background price refresh job |
| `sync` | platform | Wallet sync (Zerion blockchain import) |
| `transfer` | module | Transfer in/out/internal handlers |
| `coingecko` | infra | CoinGecko price API client |
| `zerion` | infra | Zerion blockchain data API client |
| `cache` | infra | Redis price cache |

Note: `swap`, `adjustment`, `taxlot` components exist in code but haven't appeared in Loki yet (may appear with usage).

Add note about HTTP middleware logs having no `component`.

#### 1.5 Log Fields table — fix and expand
- Remove `ip` → replace with `remote_addr`
- Fix `level` values to UPPERCASE
- Add `duration_ms` type as `int` (not `float`)
- Split into two groups:
  - **Common fields**: `level`, `component`, `msg`, `source`, `time`, `request_id`, `user_id`, `error`
  - **HTTP request fields**: `method`, `path`, `status`, `duration_ms`, `bytes`, `remote_addr`, `user_agent`
  - **Domain-specific fields**: `tx_id`, `wallet_id`, `asset_id`, `account_id`, `address`, `status_code`, `count`

#### 1.6 Debugging Scenarios — fix queries and add new scenarios

Fix existing 7 scenarios (all level case fixes + stream selector optimization):
- **Find errors**: `{service="backend", level="ERROR"}`
- **500 errors**: keep `| json | status >= 500` (not a label)
- **Failed transactions**: `{service="backend", component="ledger", level="ERROR"}`
- **Sync issues**: `{service="backend", component="sync", level=~"ERROR|WARN"}`
- **Slow requests**: keep `| json | duration_ms > 1000`
- **User trace**: `{service="backend", level="ERROR"} | json | user_id="..."`
- **Auth issues**: remove `component="auth"` (doesn't exist) → use `| json | path=~"/auth/.*" | status >= 400`

Add 2 new scenarios:
- **Price fetching issues**: `{service="backend", component=~"coingecko|price_updater|cache", level=~"WARN|ERROR"}`
- **Tax lot issues**: `{service="backend", component="taxlot_hook", level="WARN"}`

#### 1.7 Debugging Workflow — enhance
Add step 0: "Discover" — use `loki_label_names` and `loki_label_values` to explore

### Step 2: Update CLAUDE.md observability section

File: `CLAUDE.md` (lines ~129-154)

- Fix `**MCP tool:**` → `**MCP tools:**` and add `loki_label_names`, `loki_label_values`
- Fix **Key labels**: add `component`, `level` explicitly
- Fix **Key JSON fields**: remove `ip`, add `remote_addr`, `bytes`, `user_agent`
- Fix **Components**: replace `http`, `auth`, `price` with actual components
- Fix **Common queries**: use UPPERCASE level values and stream selectors

### Step 3: Verify all changes via Loki MCP

Run each example query from the updated skill to confirm it works against live Loki.

---

## Files to Modify

1. `.claude/skills/observability-debugging/SKILL.md` — full rewrite
2. `CLAUDE.md` — update observability section (lines ~129-154)

## Reference Files (read-only)

- `apps/backend/pkg/logger/logger.go` — slog config, uppercase levels
- `apps/backend/internal/transport/httpapi/middleware/logger.go` — HTTP fields (no `component`)
- `infra/promtail/promtail-config.yml` — label promotion config
- `infra/loki/loki-config.yml` — Loki limits

## Verification

1. Run `loki_label_values` for `component` and `level` — confirm all documented values match
2. Run each debugging scenario query from the skill against Loki — confirm results or "no logs" (not errors)
3. Verify stream selector queries with UPPERCASE levels return results
4. Verify `{service="backend", component="auth"}` returns nothing (confirming removal was correct)
5. Check that CLAUDE.md examples are consistent with SKILL.md
