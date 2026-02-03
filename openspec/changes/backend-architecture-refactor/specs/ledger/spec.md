## MODIFIED Requirements

### Requirement: Ledger is top-level package
The ledger package SHALL be located at `internal/ledger/` as Layer 1, not nested under `core/`.

#### Scenario: Ledger location
- **WHEN** looking for the double-entry accounting core
- **THEN** it SHALL be at `internal/ledger/`, not `internal/core/ledger/`

#### Scenario: Flat structure
- **WHEN** examining the ledger package structure
- **THEN** it SHALL contain flat files (model.go, service.go, repository.go, errors.go) without sub-packages like `domain/`, `handler/`, `service/`

### Requirement: Ledger imports nothing from project
The ledger package SHALL import only Go standard library and external dependencies, never any other project packages.

#### Scenario: No project imports
- **WHEN** analyzing imports in any file under `internal/ledger/`
- **THEN** there SHALL be no imports starting with the project module path except stdlib and external deps

#### Scenario: Self-contained types
- **WHEN** ledger needs types like Amount or Entry
- **THEN** they SHALL be defined within the ledger package itself

### Requirement: Handler registry remains in ledger
The transaction handler registry pattern SHALL remain in the ledger package with the same interface.

#### Scenario: Handler interface preserved
- **WHEN** examining the ledger package
- **THEN** it SHALL export `TransactionHandler[T]` interface with `Validate()` and `ToEntries()` methods

#### Scenario: Registration mechanism preserved
- **WHEN** modules need to register transaction handlers
- **THEN** they SHALL use `handler.Register()` called from `main.go`

### Requirement: Ledger repository interface in ledger package
The ledger repository interface SHALL be defined in the ledger package itself, with implementation in `infra/postgres/`.

#### Scenario: Interface location
- **WHEN** looking for the ledger repository interface
- **THEN** it SHALL be in `internal/ledger/repository.go`

#### Scenario: Implementation location
- **WHEN** looking for the ledger repository implementation
- **THEN** it SHALL be in `internal/infra/postgres/ledger_repo.go`
