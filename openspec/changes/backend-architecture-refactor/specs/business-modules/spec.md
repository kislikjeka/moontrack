## MODIFIED Requirements

### Requirement: Modules directory renamed to singular
The business modules directory SHALL be named `internal/module/` (singular), not `internal/modules/` (plural).

#### Scenario: Directory name
- **WHEN** looking for business feature modules
- **THEN** they SHALL be under `internal/module/`, not `internal/modules/`

### Requirement: Modules have flat internal structure
Each module SHALL use flat file-based structure without sub-packages like `domain/` or `handler/`.

#### Scenario: No domain sub-package
- **WHEN** examining any `module/*` package
- **THEN** there SHALL be no `domain/` subdirectory; domain types go in `model.go`

#### Scenario: No handler sub-package
- **WHEN** examining any `module/*` package
- **THEN** there SHALL be no `handler/` subdirectory; handlers go in `handler_*.go` files

#### Scenario: File-based splitting
- **WHEN** a module has multiple entities (e.g., income and outcome)
- **THEN** business logic SHALL be split by file name (`income.go`, `outcome.go`) not by subdirectory

### Requirement: Module naming uses short names
Module directories SHALL use concise names without redundant suffixes.

#### Scenario: Manual transaction module
- **WHEN** looking for manual transaction handling
- **THEN** it SHALL be at `module/manual/`, not `module/manual_transaction/`

#### Scenario: Asset adjustment module
- **WHEN** looking for asset adjustment handling
- **THEN** it SHALL be at `module/adjustment/`, not `module/asset_adjustment/`

### Requirement: Modules never import each other
Module packages (Layer 3) SHALL never import other module packages.

#### Scenario: No horizontal module imports
- **WHEN** analyzing imports in `module/manual/`
- **THEN** there SHALL be no imports from `module/portfolio/` or any other module

#### Scenario: Platform imports allowed
- **WHEN** analyzing imports in any `module/*` package
- **THEN** imports from `platform/*` and `ledger/` SHALL be allowed

### Requirement: Modules contain their own HTTP handlers
Each module SHALL contain its own HTTP handler files, not delegate to a central handler package.

#### Scenario: Handler in module
- **WHEN** looking for manual income HTTP handler
- **THEN** it SHALL be in `module/manual/handler_income.go`

#### Scenario: Test alongside handler
- **WHEN** looking for handler tests
- **THEN** they SHALL be in the same module package (e.g., `module/manual/handler_income_test.go`)

### Requirement: Module errors are module-specific only
Modules SHALL only define errors unique to that module; shared errors belong to lower layers.

#### Scenario: Module-specific error
- **WHEN** a module defines an error in its `errors.go`
- **THEN** it SHALL be for concepts unique to that module (e.g., `ErrDuplicateReference`)

#### Scenario: Reuse lower-layer errors
- **WHEN** a module encounters a ledger-related error condition
- **THEN** it SHALL return `ledger.ErrInsufficientBalance`, not redefine it
