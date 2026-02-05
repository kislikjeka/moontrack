# MoonTrack — Architecture Rules

## Dependency Layers

Dependencies flow **strictly downward**. No upward or horizontal imports between modules.

```
┌─────────────────────────────────────────────────┐
│  transport       HTTP handlers, CLI, workers     │  Layer 5
├─────────────────────────────────────────────────┤
│  ingestion       Classifier → Dispatcher         │  Layer 4
├─────────────────────────────────────────────────┤
│  module          Business features (per-protocol) │  Layer 3
├─────────────────────────────────────────────────┤
│  platform        Shared business domains          │  Layer 2
├─────────────────────────────────────────────────┤
│  ledger          Double-entry core                │  Layer 1
├─────────────────────────────────────────────────┤
│  infra           Adapters to external world       │  Layer 0
└─────────────────────────────────────────────────┘
```

**Allowed imports:**

| Package may import →   | ledger | platform/* | module/* | ingestion | infra/* | transport |
|------------------------|--------|------------|----------|-----------|---------|-----------|
| **ledger**             | —      | ✗          | ✗        | ✗         | ✗       | ✗         |
| **platform/***         | ✓      | own only   | ✗        | ✗         | ✗       | ✗         |
| **module/***           | ✓      | ✓          | own only | types only| ✗       | ✗         |
| **ingestion**          | ✗      | ✗          | ✗ (iface)| —         | ✗       | ✗         |
| **infra/***            | ✓      | ✓          | ✓        | ✗         | own only| ✗         |
| **transport**          | ✗      | ✗          | ✓        | ✓         | ✗       | —         |
| **cmd/server (main)**  | ✓      | ✓          | ✓        | ✓         | ✓       | ✓         |

Key constraints:
- `ledger` imports **nothing** from the project (only stdlib)
- `platform/*` services may import `ledger` but never each other (e.g. `asset` cannot import `wallet`)
- `module/*` packages **never** import each other (`gmx` cannot import `manual`)
- `infra/*` implements interfaces defined in domain packages — dependency is inverted
- `ingestion` depends on `TxHandler` interface, not on concrete modules
- All wiring happens in `cmd/server/main.go`

---

## Folder Structure

```
folio/
├── cmd/
│   └── server/
│       └── main.go                    # DI wiring, starts HTTP server
│
├── internal/
│   │
│   ├── ledger/                        # ── Layer 1: CORE ──
│   │   ├── model.go                   #   Account, Entry, Journal
│   │   ├── amount.go                  #   Value object: Amount{Value, Decimals}
│   │   ├── repository.go             #   Interface: Repository
│   │   ├── service.go                 #   PostDoubleEntry, GetBalance
│   │   └── errors.go                  #   ErrInsufficientBalance, ErrUnbalancedEntry...
│   │
│   ├── platform/                      # ── Layer 2: SHARED BUSINESS DOMAINS ──
│   │   ├── asset/
│   │   │   ├── model.go               #   Asset, Price
│   │   │   ├── port.go                #   Interfaces: Repository, Cache, PriceProvider
│   │   │   ├── service.go             #   GetAsset, GetPrice, Resolve
│   │   │   └── errors.go              #   ErrAssetNotFound, ErrPriceStale...
│   │   │
│   │   ├── wallet/
│   │   │   ├── model.go
│   │   │   ├── port.go
│   │   │   ├── service.go
│   │   │   └── errors.go
│   │   │
│   │   └── user/
│   │       ├── model.go
│   │       ├── port.go
│   │       ├── service.go
│   │       └── errors.go
│   │
│   ├── module/                        # ── Layer 3: BUSINESS MODULES ──
│   │   ├── manual/
│   │   │   ├── model.go               #   ManualIncome, ManualOutcome
│   │   │   ├── errors.go              #   Module-specific errors ONLY
│   │   │   ├── income.go              #   Business logic: income
│   │   │   ├── outcome.go             #   Business logic: outcome
│   │   │   ├── handler_income.go      #   HTTP handler: income endpoints
│   │   │   ├── handler_income_test.go
│   │   │   ├── handler_outcome.go     #   HTTP handler: outcome endpoints
│   │   │   └── handler_outcome_test.go
│   │   │
│   │   ├── liquidity/
│   │   │   ├── model.go
│   │   │   ├── errors.go
│   │   │   ├── service.go
│   │   │   ├── handler.go
│   │   │   └── handler_test.go
│   │   │
│   │   ├── gmx/
│   │   │   ├── model.go
│   │   │   ├── errors.go
│   │   │   ├── service.go
│   │   │   ├── handler.go
│   │   │   └── handler_test.go
│   │   │
│   │   └── portfolio/
│   │       ├── service.go             #   Aggregation, reporting
│   │       ├── handler.go
│   │       └── handler_test.go
│   │
│   ├── ingestion/                     # ── Layer 4: TX ROUTING ──
│   │   ├── types.go                   #   RawTransaction (unified input)
│   │   ├── classifier.go             #   Rules → TxCategory
│   │   ├── dispatcher.go             #   Category → TxHandler
│   │   └── pipeline.go               #   Orchestrates: dedup → classify → dispatch
│   │
│   ├── infra/                         # ── Layer 0: INFRASTRUCTURE ──
│   │   ├── postgres/
│   │   │   ├── conn.go                #   Connection pool setup
│   │   │   ├── ledger_repo.go         #   implements ledger.Repository
│   │   │   ├── asset_repo.go          #   implements asset.Repository
│   │   │   ├── wallet_repo.go         #   implements wallet.Repository
│   │   │   ├── user_repo.go           #   implements user.Repository
│   │   │   └── migrations/
│   │   │
│   │   ├── redis/
│   │   │   └── cache.go               #   implements asset.Cache (generic)
│   │   │
│   │   └── gateway/                   #   External API clients
│   │       ├── coingecko/
│   │       │   └── client.go          #   implements asset.PriceProvider
│   │       ├── debank/
│   │       │   └── client.go          #   Fetches raw txs → ingestion.RawTransaction
│   │       └── gmx/
│   │           └── client.go          #   implements gmx module port
│   │
│   └── transport/                     # ── Layer 5: ENTRY POINTS ──
│       ├── httpapi/
│       │   ├── router.go              #   Mounts module handlers
│       │   ├── middleware.go          #   Auth, logging, recovery
│       │   └── response.go            #   Shared HTTP helpers
│       └── worker/
│           ├── price_sync.go          #   Periodic price updates
│           └── tx_sync.go             #   Pulls txs from DeBank → pipeline
│
├── pkg/                               # ── Pure utilities (no business logic) ──
│   ├── decimal/
│   └── httputil/
│
├── go.mod
└── go.sum
```

---

## Rules

### R1: No `shared/` package

There is no `shared/`, `common/`, or `utils/` package. If a type is used by multiple packages, it belongs to the **lowest domain that defines it**:

| Type           | Lives in         | Reason                            |
|----------------|------------------|-----------------------------------|
| `Amount`       | `ledger`         | Value object of the financial core |
| `AssetID`      | `platform/asset` | Defined by asset domain            |
| `UserID`       | `platform/user`  | Defined by user domain             |
| `RawTransaction` | `ingestion`    | Input format for the pipeline      |

If something is truly domain-free (e.g., a decimal wrapper), it goes in `pkg/`.

### R2: Interfaces live in the consumer's package

Each domain defines its own port interfaces:

```go
// platform/asset/port.go
type Repository interface { ... }
type Cache interface { ... }
type PriceProvider interface { ... }
```

Infrastructure packages implement these interfaces:

```go
// infra/postgres/asset_repo.go
import "folio/internal/platform/asset"

type AssetRepo struct{ db *pgxpool.Pool }
// implements asset.Repository
```

The domain never imports `infra`. The dependency is **inverted**.

### R3: Modules are isolated

Each `module/*` package:
- Has its own `model.go`, `service.go` (or split by entity: `income.go`, `outcome.go`), `handler*.go`
- May import `ledger` and any `platform/*` service
- **Never** imports another module
- Exposes `TxHandler` interface for ingestion:

```go
// Defined in ingestion/dispatcher.go
type TxHandler interface {
    CanHandle(category TxCategory) bool
    Handle(ctx context.Context, tx RawTransaction) error
}
```

If two modules need to share logic, extract it into `platform/` or `ledger`.

**Module internal structure — flat, no sub-packages:**

```
module/manual/
├── model.go                  # Domain types: ManualIncome, ManualOutcome
├── errors.go                 # Module-specific errors ONLY
├── income.go                 # Business logic for income
├── outcome.go                # Business logic for outcome
├── handler_income.go         # HTTP handler for income endpoints
├── handler_income_test.go
├── handler_outcome.go        # HTTP handler for outcome endpoints
└── handler_outcome_test.go
```

Do **not** create `domain/` or `handler/` sub-packages inside a module. Split by entity in file names instead. This keeps everything in one Go package (one `package manual` declaration) while maintaining clear separation by file.

When to split files:
- Business logic differs by entity (income vs outcome) → separate `income.go`, `outcome.go`
- Handler has enough endpoints per entity → separate `handler_income.go`, `handler_outcome.go`
- Module is small with single responsibility → keep `service.go`, `handler.go`

### R4: Gateway is infrastructure

API clients (CoinGecko, DeBank, GMX) live in `infra/gateway/`. They are adapters — they implement domain interfaces or produce domain types. They do **not** contain business logic.

```
infra/gateway/coingecko/client.go  → implements asset.PriceProvider
infra/gateway/debank/client.go     → returns ingestion.RawTransaction
infra/gateway/gmx/client.go        → implements gmx module's port
```

### R5: Ingestion pipeline owns routing

The flow for external transactions:

```
Gateway (fetch raw) → Pipeline (dedup → classify → dispatch) → Module (handle)
```

- `classifier.go` — pure functions, rule-based, no side effects
- `dispatcher.go` — maps `TxCategory` → `TxHandler`, no business logic
- `pipeline.go` — orchestrates the flow, owns deduplication
- Modules register as handlers via DI in `main.go`

### R6: Transport is thin

`transport/httpapi/` only:
- Mounts module handlers on routes
- Applies middleware (auth, logging, CORS)
- Provides shared HTTP response helpers

It does **not** contain business logic. Module-specific HTTP handlers live inside their module (`module/manual/handler.go`).

### R7: All wiring in `main.go`

`cmd/server/main.go` is the only place that:
- Creates infrastructure (DB pool, Redis, API clients)
- Creates domain services with injected dependencies
- Creates modules with injected services
- Registers handlers with the dispatcher
- Starts HTTP server and workers

No package creates its own dependencies. Everything is injected.

### R8: Errors belong to the layer that defines the concept

Errors live in `errors.go` of the package that **owns the concept**, not in the package that triggers the error.

```go
// ledger/errors.go — financial invariants
var (
    ErrInsufficientBalance = errors.New("insufficient balance")
    ErrAccountNotFound     = errors.New("account not found")
    ErrUnbalancedEntry     = errors.New("entries do not balance")
)

// platform/asset/errors.go — asset domain errors
var (
    ErrAssetNotFound = errors.New("asset not found")
    ErrPriceStale    = errors.New("price data is stale")
)

// module/manual/errors.go — ONLY what is unique to this module
var (
    ErrDuplicateReference = errors.New("duplicate transaction reference")
)
```

**Ownership test:** if the same error could occur in two modules, it does not belong to either module — push it down to `ledger`, `platform/*`, or `pkg/`.

Modules should **re-use** errors from lower layers, not redefine them:

```go
// module/manual/income.go
func (s *Service) RecordIncome(...) error {
    // ...
    return ledger.ErrInsufficientBalance  // re-use, don't redefine
}
```

Transport layer maps domain errors to HTTP status codes in **one place**:

```go
// transport/httpapi/errors.go
func MapError(err error) int {
    switch {
    case errors.Is(err, ledger.ErrInsufficientBalance):
        return http.StatusBadRequest
    case errors.Is(err, ledger.ErrAccountNotFound),
         errors.Is(err, asset.ErrAssetNotFound):
        return http.StatusNotFound
    default:
        return http.StatusInternalServerError
    }
}
```

---

## Validation Checklist

Before committing, verify for every file:

1. **"Who do I import?"** — only packages from same layer or below
2. **No horizontal module imports** — `module/gmx` never imports `module/manual`
3. **No `shared/` or `common/`** — every type has a domain owner
4. **Interfaces in domain, implementations in infra** — `port.go` in domain, `*_repo.go` in `infra/postgres/`
5. **Gateway in infra, not in domain** — `infra/gateway/*`, never `platform/gateway/`
6. **`ledger` imports nothing** from the project — it is the innermost core
7. **No sub-packages inside modules** — split by file name (`income.go`, `handler_income.go`), not by folder
8. **Error ownership** — if an error could occur in two modules, it belongs to a lower layer
