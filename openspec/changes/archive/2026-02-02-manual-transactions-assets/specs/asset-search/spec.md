## ADDED Requirements

### Requirement: Search assets by query
The system SHALL provide an API endpoint to search cryptocurrency assets by name or symbol through CoinGecko API.

#### Scenario: Search by symbol returns matching assets
- **WHEN** user sends GET request to `/assets/search?q=BTC`
- **THEN** system returns list of assets containing "BTC" in name or symbol, sorted by market cap rank

#### Scenario: Search by name returns matching assets
- **WHEN** user sends GET request to `/assets/search?q=bitcoin`
- **THEN** system returns list of assets containing "bitcoin" in name, including Bitcoin and Bitcoin Cash

#### Scenario: Empty query returns error
- **WHEN** user sends GET request to `/assets/search?q=`
- **THEN** system returns 400 Bad Request with error message "query parameter is required"

#### Scenario: Query too short returns error
- **WHEN** user sends GET request to `/assets/search?q=a`
- **THEN** system returns 400 Bad Request with error message "query must be at least 2 characters"

### Requirement: Cache search results
The system SHALL cache search results in Redis for 24 hours to reduce CoinGecko API calls.

#### Scenario: Cached results are returned
- **WHEN** user searches for "ETH" and result exists in cache
- **THEN** system returns cached result without calling CoinGecko API

#### Scenario: Cache miss triggers API call
- **WHEN** user searches for "ETH" and result does not exist in cache
- **THEN** system calls CoinGecko Search API, stores result in cache with 24h TTL, and returns result

### Requirement: Limit search results
The system SHALL return maximum 10 assets per search query, sorted by market cap rank descending.

#### Scenario: Popular query returns top 10 results
- **WHEN** user searches for "coin" which matches hundreds of assets
- **THEN** system returns only top 10 assets by market cap rank

### Requirement: Search response includes CoinGecko ID
The system SHALL include CoinGecko ID in each search result to enable price lookups.

#### Scenario: Search result contains required fields
- **WHEN** user searches for "BTC"
- **THEN** each result contains: id (CoinGecko ID), symbol, name, and market_cap_rank

### Requirement: Handle CoinGecko API failures gracefully
The system SHALL return cached results or empty array when CoinGecko API is unavailable.

#### Scenario: API failure with cached results
- **WHEN** CoinGecko API is unavailable but cached results exist
- **THEN** system returns stale cached results with warning header

#### Scenario: API failure without cached results
- **WHEN** CoinGecko API is unavailable and no cached results exist
- **THEN** system returns empty array with 503 status and error message
