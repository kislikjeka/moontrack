---
name: backend-architecture
description: This skill should be used when the user asks to "create a new module", "add a new package", "design a new service", "add a new feature to backend", "create handler", "add repository", "design domain model", "refactor architecture", "check layer compliance", "review module structure", or needs guidance on Go backend architecture, package structure, layer dependencies, or module organization for MoonTrack.
---

# Backend Architecture Design

Design and implement new modules, packages, and services following MoonTrack's layered architecture rules.

## When to Use

- Creating a new business module (e.g., staking, lending, swaps)
- Adding a new platform service (e.g., notification, analytics)
- Designing domain models, repositories, or handlers
- Structuring code to follow dependency layer rules
- Reviewing architecture compliance

## Architecture Overview

MoonTrack uses a strict 6-layer architecture with downward-only dependencies:

```
Layer 5: transport     → HTTP handlers, CLI, workers
Layer 4: ingestion     → Classifier → Dispatcher
Layer 3: module        → Business features (per-protocol)
Layer 2: platform      → Shared business domains
Layer 1: ledger        → Double-entry core
Layer 0: infra         → Adapters to external world
```

**Key constraint:** Dependencies flow strictly downward. No upward or horizontal imports between modules.

## Module Design Process

### Step 1: Determine Layer Placement

Identify where the new code belongs:

| New Code Type | Layer | Location |
|---------------|-------|----------|
| Protocol-specific feature (GMX, Aave, etc.) | 3 | `module/<protocol>/` |
| Shared business domain (asset, wallet, user) | 2 | `platform/<domain>/` |
| External API client | 0 | `infra/gateway/<provider>/` |
| Database repository | 0 | `infra/postgres/` |
| HTTP endpoint | 5 | `transport/httpapi/` or in module handler |

### Step 2: Design Module Structure

For a new **business module** (Layer 3), create a flat structure:

```
module/<name>/
├── model.go           # Domain types
├── errors.go          # Module-specific errors ONLY
├── service.go         # Business logic (or split: income.go, outcome.go)
├── handler.go         # HTTP handler (or split: handler_income.go, handler_outcome.go)
└── handler_test.go    # Handler tests
```

**Critical rules:**
- No sub-packages inside modules (no `domain/`, `handler/` folders)
- Split by entity in file names, not by folder
- Module may import `ledger` and any `platform/*` service
- Module **never** imports another module

### Step 3: Define Ports and Interfaces

Interfaces live in the **consumer's** package:

```go
// platform/asset/port.go
type Repository interface {
    GetByID(ctx context.Context, id string) (*Asset, error)
    // ...
}

type PriceProvider interface {
    GetPrice(ctx context.Context, symbol string) (*Price, error)
}
```

Infrastructure implements these interfaces:

```go
// infra/postgres/asset_repo.go
type AssetRepo struct{ db *pgxpool.Pool }
// implements asset.Repository
```

### Step 4: Handle Errors Correctly

Errors belong to the layer that **owns the concept**:

| Error Type | Owner | Example |
|------------|-------|---------|
| Financial invariants | `ledger` | `ErrInsufficientBalance` |
| Asset domain | `platform/asset` | `ErrAssetNotFound` |
| Module-specific | `module/<name>` | `ErrDuplicateReference` |

**Ownership test:** If the same error could occur in two modules, it belongs to a lower layer.

Modules should re-use errors from lower layers:

```go
// module/manual/income.go
func (s *Service) RecordIncome(...) error {
    // ...
    return ledger.ErrInsufficientBalance  // re-use, don't redefine
}
```

### Step 5: Wire in main.go

All dependency injection happens in `cmd/api/main.go`:

```go
// Create infrastructure
db := postgres.NewPool(cfg.DatabaseURL)
assetRepo := postgres.NewAssetRepo(db)

// Create domain services
assetSvc := asset.NewService(assetRepo, priceProvider)

// Create modules
manualModule := manual.NewService(ledgerSvc, assetSvc, walletSvc)

// Register handlers
router.Mount("/manual", manualModule.Handler())
```

No package creates its own dependencies. Everything is injected.

## Validation Checklist

Before committing, verify:

1. **"Who do I import?"** — only packages from same layer or below
2. **No horizontal module imports** — `module/gmx` never imports `module/manual`
3. **No `shared/` or `common/`** — every type has a domain owner
4. **Interfaces in domain, implementations in infra**
5. **Gateway in infra, not in domain** — `infra/gateway/*`
6. **`ledger` imports nothing** from the project
7. **No sub-packages inside modules** — split by file name
8. **Error ownership** — if error could occur in two modules, it belongs to lower layer

## Quick Reference: Allowed Imports

| Package | May Import |
|---------|------------|
| `ledger` | stdlib only |
| `platform/*` | `ledger`, own package only |
| `module/*` | `ledger`, `platform/*`, own package only |
| `infra/*` | `ledger`, `platform/*`, `module/*`, own package |
| `transport` | `module/*`, `ingestion` |

## Additional Resources

### Reference Files

For complete architecture rules and folder structure:
- **`references/architecture-rules.md`** - Full dependency matrix, folder structure, all 8 rules with examples

## Example: Creating a New Module

To create a "staking" module for tracking staked assets:

1. **Create directory**: `mkdir internal/module/staking`

2. **Create model.go**:
```go
package staking

type StakePosition struct {
    ID        string
    WalletID  string
    AssetID   string
    Amount    ledger.Amount
    Validator string
    StakedAt  time.Time
}
```

3. **Create errors.go** (module-specific only):
```go
package staking

var (
    ErrValidatorNotFound = errors.New("validator not found")
    ErrUnstakePending    = errors.New("unstake already pending")
)
```

4. **Create service.go**:
```go
package staking

type Service struct {
    ledger    *ledger.Service
    assets    *asset.Service
    wallets   *wallet.Service
    repo      Repository  // interface defined here
}

func (s *Service) Stake(ctx context.Context, req StakeRequest) error {
    // Business logic using injected dependencies
}
```

5. **Create handler.go**:
```go
package staking

func (s *Service) Handler() http.Handler {
    r := chi.NewRouter()
    r.Post("/stake", s.handleStake)
    r.Post("/unstake", s.handleUnstake)
    return r
}
```

6. **Wire in main.go**:
```go
stakingRepo := postgres.NewStakingRepo(db)
stakingSvc := staking.NewService(ledgerSvc, assetSvc, walletSvc, stakingRepo)
router.Mount("/staking", stakingSvc.Handler())
```

This ensures correct layering, dependency injection, and follows all architecture rules.
