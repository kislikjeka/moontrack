## MODIFIED Requirements

### Requirement: Search assets by query
The system SHALL search assets first in the local Asset Registry, then fall back to CoinGecko API for new assets.

#### Scenario: Search by symbol returns matching assets from registry
- **WHEN** user sends GET request to `/assets/search?q=BTC`
- **THEN** system first searches local Asset Registry for assets containing "BTC" in name or symbol, sorted by market cap rank

#### Scenario: Search returns results from registry with chain info
- **WHEN** user searches for "USDC"
- **THEN** system returns all USDC assets from registry with chain_id field to distinguish between Ethereum, Solana, Polygon variants

#### Scenario: Search falls back to CoinGecko for unknown assets
- **WHEN** user searches for asset not in registry and no results found locally
- **THEN** system queries CoinGecko API, optionally creates new asset records, and returns results

#### Scenario: Empty query returns error
- **WHEN** user sends GET request to `/assets/search?q=`
- **THEN** system returns 400 Bad Request with error message "query parameter is required"

#### Scenario: Query too short returns error
- **WHEN** user sends GET request to `/assets/search?q=a`
- **THEN** system returns 400 Bad Request with error message "query must be at least 2 characters"

### Requirement: Cache search results
The system SHALL cache CoinGecko search results in Redis for 24 hours, but prefer Asset Registry results.

#### Scenario: Registry results returned without cache
- **WHEN** user searches for "ETH" and assets exist in registry
- **THEN** system returns registry results immediately without checking CoinGecko cache

#### Scenario: CoinGecko results cached on fallback
- **WHEN** user searches for asset not in registry and CoinGecko is queried
- **THEN** system caches CoinGecko result with 24h TTL for subsequent searches

### Requirement: Limit search results
The system SHALL return maximum 10 assets per search query, sorted by market cap rank descending.

#### Scenario: Popular query returns top 10 results
- **WHEN** user searches for "coin" which matches many assets
- **THEN** system returns only top 10 assets by market cap rank

### Requirement: Search response includes asset UUID
The system SHALL include internal asset UUID in each search result instead of CoinGecko ID.

#### Scenario: Search result contains required fields
- **WHEN** user searches for "BTC"
- **THEN** each result contains: id (internal UUID), symbol, name, market_cap_rank, chain_id, and coingecko_id (for reference)

### Requirement: Handle CoinGecko API failures gracefully
The system SHALL return registry results when CoinGecko API is unavailable.

#### Scenario: API failure returns registry results only
- **WHEN** CoinGecko API is unavailable
- **THEN** system returns assets from local registry matching the query

#### Scenario: API failure with no registry results
- **WHEN** CoinGecko API is unavailable and no matching assets in registry
- **THEN** system returns empty array with warning "external search unavailable"
