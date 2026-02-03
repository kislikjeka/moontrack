## ADDED Requirements

### Requirement: Infrastructure layer consolidates adapters
The system SHALL have an `internal/infra/` directory that consolidates all infrastructure adapters including database connections, cache clients, and external API gateways.

#### Scenario: PostgreSQL repositories consolidated
- **WHEN** looking for any PostgreSQL repository implementation
- **THEN** it SHALL be located in `internal/infra/postgres/`

#### Scenario: Redis cache consolidated
- **WHEN** looking for Redis cache implementation
- **THEN** it SHALL be located in `internal/infra/redis/`

#### Scenario: External API gateways consolidated
- **WHEN** looking for external API client implementations (e.g., CoinGecko)
- **THEN** they SHALL be located in `internal/infra/gateway/<provider>/`

### Requirement: Infrastructure implements domain interfaces
Infrastructure packages SHALL implement interfaces defined in domain packages, not define their own interfaces.

#### Scenario: Repository implements domain port
- **WHEN** `infra/postgres/asset_repo.go` is created
- **THEN** it SHALL import `platform/asset` and implement `asset.Repository` interface

#### Scenario: Cache implements domain port
- **WHEN** `infra/redis/cache.go` is created
- **THEN** it SHALL implement cache interfaces defined in consuming domain packages

### Requirement: Infrastructure imports only downward
Infrastructure packages (Layer 0) SHALL only import from `ledger`, `platform/*`, and `module/*` packages, never from `transport` or `ingestion`.

#### Scenario: Valid infra imports
- **WHEN** analyzing imports in any `infra/*` package
- **THEN** imports SHALL only include stdlib, external dependencies, `ledger`, `platform/*`, `module/*`, or other `infra/*` packages

#### Scenario: No transport imports
- **WHEN** analyzing imports in any `infra/*` package
- **THEN** there SHALL be no imports from `transport/*` or `ingestion/*`

### Requirement: Database connection pooling in infra
The database connection pool setup SHALL be located in `internal/infra/postgres/conn.go`.

#### Scenario: Connection pool location
- **WHEN** looking for PostgreSQL connection pool initialization
- **THEN** it SHALL be in `infra/postgres/conn.go`, not in `shared/database/`

#### Scenario: Connection pool injected
- **WHEN** creating repository instances
- **THEN** the connection pool SHALL be passed via constructor injection from `main.go`
