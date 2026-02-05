## MODIFIED Requirements

### Requirement: Platform layer contains shared business domains
The system SHALL have an `internal/platform/` directory containing shared business domains used by multiple modules.

#### Scenario: Asset domain location
- **WHEN** looking for asset/pricing domain logic
- **THEN** it SHALL be at `internal/platform/asset/`, not `internal/core/asset_registry/`

#### Scenario: User domain location
- **WHEN** looking for user domain logic (excluding auth middleware)
- **THEN** it SHALL be at `internal/platform/user/`, not `internal/core/user/`

#### Scenario: Wallet domain location
- **WHEN** looking for wallet domain logic
- **THEN** it SHALL be at `internal/platform/wallet/`, not `internal/modules/wallet/`

### Requirement: Platform packages have flat structure
Each platform domain package SHALL use flat file-based structure without sub-packages.

#### Scenario: Standard file layout
- **WHEN** examining any `platform/*` package
- **THEN** it SHALL contain files like `model.go`, `port.go`, `service.go`, `errors.go` without `domain/`, `repository/`, `service/` sub-folders

#### Scenario: No nested packages
- **WHEN** listing directories under any `platform/*` package
- **THEN** there SHALL be no subdirectories

### Requirement: Platform packages never import each other
Platform packages (Layer 2) SHALL never import other platform packages horizontally.

#### Scenario: No horizontal imports
- **WHEN** analyzing imports in `platform/asset/`
- **THEN** there SHALL be no imports from `platform/user/` or `platform/wallet/`

#### Scenario: Ledger imports allowed
- **WHEN** analyzing imports in any `platform/*` package
- **THEN** imports from `internal/ledger/` SHALL be allowed

### Requirement: Platform defines port interfaces
Each platform domain SHALL define its own port interfaces for repository, cache, and external providers.

#### Scenario: Port file exists
- **WHEN** examining a platform domain package
- **THEN** it SHALL have a `port.go` file defining interfaces like `Repository`, `Cache`, or provider interfaces

#### Scenario: Implementations in infra
- **WHEN** looking for implementations of platform port interfaces
- **THEN** they SHALL be in `internal/infra/` packages, not in the platform package itself

### Requirement: Domain errors owned by platform
Each platform domain SHALL define its own error types in an `errors.go` file.

#### Scenario: Asset errors
- **WHEN** looking for asset-related errors like `ErrAssetNotFound`
- **THEN** they SHALL be defined in `platform/asset/errors.go`

#### Scenario: User errors
- **WHEN** looking for user-related errors like `ErrUserNotFound`
- **THEN** they SHALL be defined in `platform/user/errors.go`

#### Scenario: No shared errors package
- **WHEN** looking for a `shared/errors/` package
- **THEN** it SHALL NOT exist; errors belong to their domain owners
