## Context

The MoonTrack backend currently uses a mixed architecture with:
- `core/` containing ledger, user, and asset_registry with nested sub-packages (domain/, handler/, repository/, service/)
- `modules/` containing business features with similar nested structure
- `shared/` acting as a catch-all for database, config, cache, logger, errors
- `api/` at the top level for HTTP transport
- `gateway/` at the top level for external API clients

The target architecture defined in `docs/file-structure.md` specifies a strict 6-layer system where dependencies flow downward only. Current violations include:
- `shared/` package exists (forbidden by R1)
- Modules have sub-packages instead of flat file structure (forbidden by R3/R7)
- PostgreSQL implementations scattered across packages instead of consolidated in `infra/postgres/`
- Gateway clients not in `infra/gateway/`
- No clear layer boundaries in import graph

## Goals / Non-Goals

**Goals:**
- Restructure to match the 6-layer architecture (ledger → platform → module → ingestion → infra → transport)
- Eliminate `shared/` package entirely
- Consolidate all PostgreSQL implementations into `infra/postgres/`
- Flatten module internal structure to single-package with file-based splitting
- Ensure `ledger/` imports nothing from the project (stdlib only)
- Enable static validation of layer boundaries via import analysis

**Non-Goals:**
- Changing business logic or domain models
- Adding new features or capabilities
- Modifying external API contracts or endpoints
- Changing database schema
- Implementing the ingestion layer (deferred — no current use case)

## Decisions

### D1: Phased migration approach (not big-bang)

**Decision:** Execute refactor in 4 ordered phases, each leaving the codebase compilable and testable.

**Rationale:** A big-bang refactor touching ~83 files risks merge conflicts, hard-to-debug failures, and blocked development. Phased approach allows:
- Verification at each step
- Easier rollback if issues arise
- Other work can continue between phases

**Alternatives considered:**
- Big-bang: Higher risk of cascading failures, harder to review
- Feature flags/dual paths: Unnecessary complexity for a pure refactor

**Phases:**
1. Create `infra/` layer and move postgres repos + redis cache + gateway clients
2. Flatten modules and rename `modules/` → `module/`
3. Create `platform/` layer from `core/` packages (asset, user, wallet)
4. Relocate `ledger/` to top-level, move `api/` → `transport/httpapi/`, delete `shared/`

### D2: Keep ledger handler registry pattern intact

**Decision:** The existing `handler.Register()` pattern for transaction types remains unchanged. Only file locations move.

**Rationale:** The handler registry is a strength of the current design — it allows adding transaction types without modifying ledger core. Moving files doesn't require changing this pattern.

**Alternatives considered:**
- Redesign handler interface: Would be scope creep; current interface works well

### D3: Redistribute `shared/` contents by domain ownership

**Decision:** Delete `shared/` and redistribute:

| Current location | New location | Rationale |
|------------------|--------------|-----------|
| `shared/database/` | `infra/postgres/conn.go` | Infrastructure adapter |
| `shared/cache/` | `infra/redis/` | Infrastructure adapter |
| `shared/config/` | `pkg/config/` | Domain-free utility |
| `shared/logger/` | `pkg/logger/` | Domain-free utility |
| `shared/errors/` | Distribute to domain owners | Each domain owns its errors (R8) |
| `shared/types/` | Domain packages or `pkg/` | Types belong to defining domain |

**Rationale:** R1 forbids catch-all packages. Every type must have a domain owner. Infrastructure belongs in `infra/`, utilities in `pkg/`, domain errors with their domain.

**Alternatives considered:**
- Rename `shared/` to `pkg/`: Would still violate domain ownership for errors and DB connection

### D4: Module flattening strategy

**Decision:** Convert nested module structure:
```
modules/manual_transaction/
├── domain/
│   └── income.go
└── handler/
    └── income_handler.go
```

To flat structure:
```
module/manual/
├── model.go         # from domain/*.go
├── income.go        # business logic
├── outcome.go
├── handler_income.go
└── handler_outcome.go
```

**Rationale:** R3 requires flat modules with file-based splitting. This reduces package count, simplifies imports, and keeps related code together.

**Alternatives considered:**
- Keep sub-packages: Violates architecture rules
- Single file per module: Would create very large files

### D5: Wallet moves to `platform/`, not `module/`

**Decision:** `modules/wallet/` moves to `platform/wallet/`.

**Rationale:** Wallet is a shared business domain used by multiple modules (portfolio, transactions, manual_transaction), not a standalone business feature. Per R3, if two modules need to share logic, it belongs in `platform/`.

**Alternatives considered:**
- Keep in `module/`: Would require wallet-to-wallet imports, violating layer rules

### D6: Defer ingestion layer

**Decision:** Do not create `internal/ingestion/` in this refactor.

**Rationale:** The codebase currently has no transaction classification or routing from external sources. The ingestion layer (classifier → dispatcher → pipeline) is designed for future DeBank/on-chain integration. Creating it now would be premature (YAGNI).

**Alternatives considered:**
- Create placeholder structure: Would add dead code

### D7: Auth stays with transport layer

**Decision:** Move `core/user/auth/` to `transport/httpapi/` (middleware), not `infra/auth/`.

**Rationale:** JWT auth in MoonTrack is purely HTTP middleware — it validates tokens and extracts user context for HTTP handlers. It doesn't implement domain interfaces. Transport is the appropriate layer.

**Alternatives considered:**
- `infra/auth/`: Would be appropriate if auth implemented a domain port, but it's purely HTTP-level
- `platform/user/`: Would couple HTTP concerns to user domain

## Risks / Trade-offs

**[Risk] Large number of import changes may introduce bugs**
→ Mitigation: Run `go build ./...` and `go test ./...` after each phase. Use `goimports -w` to auto-fix import paths. IDE refactoring tools can help with renames.

**[Risk] Merge conflicts with concurrent work**
→ Mitigation: Execute during low-activity period. Keep phases small and merge quickly. Communicate with team about timing.

**[Risk] Tests break due to internal package moves**
→ Mitigation: Tests move with their packages. Update import paths in test files. No test logic changes expected.

**[Trade-off] More packages to navigate**
→ Acceptance: The 6-layer structure adds package count but provides clear boundaries. Navigation cost is offset by predictable locations (all postgres code in one place, all modules flat).

**[Trade-off] `pkg/` introduces code outside `internal/`**
→ Acceptance: `pkg/` is standard Go convention for domain-free utilities. Only truly generic code (config loading, logging setup) goes there. Business logic stays in `internal/`.

## Migration Plan

### Phase 1: Create `infra/` layer

1. Create `internal/infra/postgres/`
2. Move connection pool setup from `shared/database/`
3. Consolidate all `*_repo.go` files from their current locations
4. Create `internal/infra/redis/` from `shared/cache/`
5. Create `internal/infra/gateway/coingecko/` from `gateway/coingecko/`
6. Update all imports referencing moved packages
7. Verify: `go build ./... && go test ./...`

### Phase 2: Flatten modules

1. Rename `modules/` → `module/`
2. For each module, flatten sub-packages into single package with file-based splitting
3. Rename `manual_transaction` → `manual`, `asset_adjustment` → `adjustment`
4. Update handler registrations and imports
5. Verify: `go build ./... && go test ./...`

### Phase 3: Create `platform/` layer

1. Create `internal/platform/asset/` from `core/asset_registry/`
2. Create `internal/platform/user/` from `core/user/` (excluding auth)
3. Create `internal/platform/wallet/` from `module/wallet/`
4. Flatten each into port.go, model.go, service.go, errors.go
5. Update all imports
6. Verify: `go build ./... && go test ./...`

### Phase 4: Final restructure

1. Move `core/ledger/` → `internal/ledger/`
2. Audit ledger imports — remove any project imports (stdlib only)
3. Move `api/` → `transport/httpapi/`
4. Move auth middleware to `transport/httpapi/middleware/`
5. Delete `shared/` — remaining items to `pkg/` (config, logger)
6. Delete empty `core/` directory
7. Update `cmd/api/main.go` wiring
8. Final verification: `go build ./... && go test ./...`

### Rollback Strategy

Each phase produces a working commit. If issues are discovered:
1. Revert to the last working phase commit
2. Investigate the issue
3. Re-attempt with fixes

No database migrations involved — rollback is purely code-level.
