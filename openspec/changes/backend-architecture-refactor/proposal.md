## Why

The current backend structure in `apps/backend/internal/` does not follow the layered architecture defined in `docs/file-structure.md`. This leads to:
- Violations of dependency rules (shared package as catch-all, modules in wrong layers)
- Flat postgres repositories scattered across packages instead of consolidated in infra
- Module sub-packages instead of flat file-based structure
- Missing ingestion layer for transaction routing

Restructuring now will establish clean boundaries before adding more business modules.

## What Changes

**Directory Structure:**
- Rename `core/ledger/` → `ledger/` (top-level, Layer 1)
- Move `core/asset_registry/` → `platform/asset/` (Layer 2)
- Move `core/user/` → `platform/user/` (Layer 2, excluding auth)
- Move `modules/wallet/` → `platform/wallet/` (Layer 2, it's a shared domain not a module)
- Rename `modules/` → `module/` and flatten sub-packages (Layer 3)
- Create `infra/` to consolidate: postgres repos, redis cache, gateway clients, logger
- Move `api/` → `transport/httpapi/` (Layer 5)
- **BREAKING**: Delete `shared/` package — redistribute errors to domain owners, move utils to `pkg/`

**Package Changes:**
- Flatten module internal structure: remove `domain/`, `handler/` sub-folders, use file-based splitting
- Consolidate all postgres implementations into `infra/postgres/`
- Move `shared/cache/` → `infra/redis/`
- Move `gateway/coingecko/` → `infra/gateway/coingecko/`
- Move JWT/auth to `infra/auth/` or `transport/httpapi/`

**Import Fixes:**
- Ensure `ledger/` imports nothing from the project (only stdlib + external)
- Ensure `platform/*` packages never import each other
- Ensure `module/*` packages never import each other

## Capabilities

### New Capabilities
- `infra-layer`: Infrastructure adapters layer consolidating postgres, redis, gateway, and logger implementations

### Modified Capabilities
- `ledger`: Relocate to top-level, ensure pure imports
- `platform-domains`: Reorganize asset, user, wallet as Layer 2 shared business domains
- `business-modules`: Flatten module structure, correct layer placement

## Impact

**Code:**
- ~83 Go files need path/import updates
- All `import` statements referencing moved packages must be updated
- `cmd/api/main.go` wiring will change

**APIs:**
- No external API changes (paths/contracts stay the same)
- Internal package paths change completely

**Tests:**
- All test imports must be updated
- No test logic changes expected

**Dependencies:**
- No external dependency changes
- Internal dependency graph will follow strict layer rules after refactor
