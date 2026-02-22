---
name: observability-debugging
description: This skill should be used when the user asks to "debug logs", "find errors in logs", "trace a request", "check backend logs", "investigate 500 error", "find slow requests", "debug sync issues", "check Loki", "query logs", "search logs", or needs to investigate backend issues using Grafana Loki log aggregation. Provides LogQL query patterns, component labels, and debugging workflows for MoonTrack's structured logging.
---

# Observability & Debugging Skill

Debug MoonTrack backend issues using structured logs via Grafana Loki MCP integration.

## Prerequisites

1. Start the log stack: `just dev-logs`
2. Build the Loki MCP image: `just loki-mcp-build`
3. Verify setup: `just loki-mcp-check`

## MCP Tools

### `loki_query`

Execute LogQL queries against Loki. Main tool for debugging.

| Parameter | Description | Default |
|-----------|-------------|---------|
| `query` | LogQL query string | required |
| `limit` | Max log lines to return | 100 |
| `start` | Start time (RFC3339: `2026-02-22T00:00:00Z` or date: `2026-02-22`) | 1h ago |
| `end` | End time (same formats as `start`) | now |

**Time format notes:**
- Relative time (`1h`, `24h`) does NOT work — returns "unsupported time format"
- Accepted: RFC3339 (`2026-02-22T12:00:00Z`) or date-only (`2026-02-22`)
- Max query range: 30 days (Loki server limit — exceeding returns error)
- When omitted, defaults to last 1 hour (server-side default)

### `loki_label_names`

List all available label names. Useful for discovery.

| Parameter | Description | Default |
|-----------|-------------|---------|
| `start` | Start time | 1h ago |
| `end` | End time | now |

### `loki_label_values`

Get all values for a specific label. Useful for discovering components, levels, etc.

| Parameter | Description | Default |
|-----------|-------------|---------|
| `label` | Label name to get values for | required |
| `start` | Start time | 1h ago |
| `end` | End time | now |

## LogQL Quick Reference

### Stream Selectors

`service`, `level`, and `component` are **indexed Loki labels** (promoted by Promtail). Use them in stream selectors `{...}` for fast filtering — no `| json` needed.

```logql
{service="backend"}                        # All backend logs
{service="backend", component="ledger"}    # Ledger component only
{service="backend", level="ERROR"}         # Errors only (indexed label)
{service="backend", level=~"ERROR|WARN"}   # Errors and warnings
```

**Level values are UPPERCASE**: `DEBUG`, `INFO`, `WARN`, `ERROR`

### JSON Pipeline (structured logs)

For non-indexed fields, parse with `| json` then filter:

```logql
{service="backend"} | json | status >= 500                    # 5xx responses
{service="backend"} | json | duration_ms > 1000               # Slow requests (>1s)
{service="backend"} | json | request_id="abc-123"             # Trace specific request
{service="backend"} | json | user_id="uuid-here"              # User's requests
{service="backend"} | json | tx_id="uuid-here"                # Transaction trace
{service="backend"} | json | msg=~".*wallet.*"                # Message pattern match
```

### Combining Filters

Use stream selectors for indexed fields, `| json` for everything else:

```logql
{service="backend", component="sync", level="ERROR"}
{service="backend", level=~"ERROR|WARN"} | json | duration_ms > 500
{service="backend"} | json | method="POST" | path=~"/transactions.*" | status >= 400
```

## Query Optimization

### Indexed Labels vs JSON Fields

Three labels are promoted to indexed Loki labels by Promtail: `service`, `level`, `component`. **Always put these in stream selectors `{...}`** for fast filtering.

| Type | Fields | Where to filter |
|------|--------|-----------------|
| **Indexed labels** | `service`, `level`, `component` | `{service="backend", level="ERROR"}` |
| **JSON fields** | Everything else (`status`, `duration_ms`, `user_id`, etc.) | `\| json \| status >= 500` |

**Rule**: Put `level` and `component` in `{...}`, everything else after `| json`.

**Bad** (slow — parses JSON for every log line):
```logql
{service="backend"} | json | level="ERROR" | component="ledger"
```

**Good** (fast — filters at index level):
```logql
{service="backend", component="ledger", level="ERROR"}
```

### HTTP Middleware Logs

HTTP request/response logs (method, path, status, duration_ms) are emitted by the HTTP middleware and have **no `component` label**. Filter these by their JSON fields:

```logql
{service="backend"} | json | method="GET" | path=~"/wallets.*"
{service="backend"} | json | status >= 500
```

## Backend Components

Log entries include a `component` label (indexed) identifying the source:

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

**Note:** `swap`, `adjustment`, `taxlot` components exist in code but may not appear in Loki until exercised.

**Note:** HTTP middleware logs have no `component` — filter by `method`/`path`/`status` instead.

## Log Fields

### Common Fields

| Field | Type | Description |
|-------|------|-------------|
| `level` | string | `DEBUG`, `INFO`, `WARN`, `ERROR` (UPPERCASE) |
| `component` | string | Source component (see table above) |
| `msg` | string | Log message |
| `source` | string | Source file and line number |
| `time` | string | Timestamp |
| `request_id` | string | Unique request ID for tracing |
| `user_id` | string | Authenticated user UUID |
| `error` | string | Error message (when present) |

### HTTP Request Fields

Emitted by HTTP middleware (no `component` label):

| Field | Type | Description |
|-------|------|-------------|
| `method` | string | HTTP method (GET, POST, etc.) |
| `path` | string | Request path |
| `status` | int | HTTP status code |
| `duration_ms` | int | Request duration in milliseconds |
| `bytes` | int | Response body size in bytes |
| `remote_addr` | string | Client IP and port (format: `IP:port`) |
| `user_agent` | string | Client user agent string |

### Domain-Specific Fields

| Field | Type | Description |
|-------|------|-------------|
| `tx_id` | string | Transaction UUID |
| `wallet_id` | string | Wallet UUID |
| `asset_id` | string | Asset UUID |
| `account_id` | string | Account UUID |
| `account_code` | string | Ledger account code |
| `address` | string | Blockchain wallet address |
| `status_code` | int | External API status code |
| `count` | int | Item count in batch operations |

## Debugging Scenarios

### 1. Find Recent Errors

```logql
{service="backend", level="ERROR"}
```

Start here to see all recent errors across all components. Uses indexed label for fast results.

### 2. Investigate 500 Errors

```logql
# Find 500s
{service="backend"} | json | status >= 500

# Get the request_id from the result, then trace the full request:
{service="backend"} | json | request_id="<id-from-above>"
```

### 3. Debug Failed Transactions

```logql
# Find transaction errors (fast — both indexed)
{service="backend", component="ledger", level="ERROR"}

# Trace specific transaction
{service="backend"} | json | tx_id="<transaction-uuid>"
```

### 4. Debug Wallet Sync Issues

```logql
# Sync errors and warnings
{service="backend", component="sync", level=~"ERROR|WARN"}

# All sync activity for a wallet
{service="backend", component="sync"} | json | msg=~".*<wallet-id>.*"
```

### 5. Find Slow Requests

```logql
# Requests taking over 1 second
{service="backend"} | json | duration_ms > 1000

# Slow ledger operations
{service="backend", component="ledger"} | json | duration_ms > 500
```

### 6. User Activity Trace

```logql
# All errors from a specific user
{service="backend", level="ERROR"} | json | user_id="<user-uuid>"

# All requests from a specific user
{service="backend"} | json | user_id="<user-uuid>"
```

### 7. Authentication Issues

```logql
# Auth endpoint failures
{service="backend"} | json | path=~"/auth/.*" | status >= 400

# Failed logins specifically
{service="backend"} | json | path="/auth/login" | status >= 400
```

Note: There is no `auth` component in Loki — auth logs come from HTTP middleware.

### 8. Price Fetching Issues

```logql
# Price-related errors and warnings
{service="backend", component=~"coingecko|price_updater|cache", level=~"WARN|ERROR"}

# CoinGecko API errors specifically
{service="backend", component="coingecko", level="ERROR"}
```

### 9. Tax Lot Issues

```logql
# Tax lot warnings (e.g., insufficient lots, disposal issues)
{service="backend", component="taxlot_hook", level=~"WARN|ERROR"}
```

## Debugging Workflow

0. **Discover**: Use `loki_label_names` and `loki_label_values` to explore available labels and values
1. **Start broad**: Query all errors in a time window — `{service="backend", level="ERROR"}`
2. **Narrow down**: Add `component` to stream selector, or filter by `status`/`path` via `| json`
3. **Trace request**: Use `request_id` to follow a single request through all layers
4. **Check timing**: Look at `duration_ms` to identify bottlenecks
5. **Context**: Use `user_id`, `tx_id`, or `wallet_id` to gather related log entries

## Grafana UI

For visual exploration, open Grafana: `just grafana` (http://localhost:3001/explore)
