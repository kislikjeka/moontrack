## ADDED Requirements

### Requirement: Get current price by CoinGecko ID
The system SHALL provide an API endpoint to get current USD price for an asset using its CoinGecko ID.

#### Scenario: Get price for valid asset
- **WHEN** user sends GET request to `/assets/bitcoin/price`
- **THEN** system returns current USD price as scaled integer (multiplied by 10^8)

#### Scenario: Get price for invalid asset
- **WHEN** user sends GET request to `/assets/invalid-id/price`
- **THEN** system returns 404 Not Found with error message "asset not found"

### Requirement: Resolve symbol to CoinGecko ID
The system SHALL maintain a mapping from asset symbols to CoinGecko IDs for common assets.

#### Scenario: Resolve known symbol
- **WHEN** system needs to get price for symbol "BTC"
- **THEN** system resolves "BTC" to CoinGecko ID "bitcoin"

#### Scenario: Cache symbol mapping
- **WHEN** symbol is resolved via CoinGecko Search API
- **THEN** system caches the mapping for 7 days

#### Scenario: Symbol not found
- **WHEN** system cannot resolve symbol to CoinGecko ID
- **THEN** system returns error indicating symbol is not supported

### Requirement: Use existing price service
The system SHALL use existing PriceService for price lookups to leverage caching and circuit breaker.

#### Scenario: Price retrieved from cache
- **WHEN** price exists in Redis cache (within 60s TTL)
- **THEN** system returns cached price without API call

#### Scenario: Price retrieved from CoinGecko
- **WHEN** price not in cache
- **THEN** system fetches from CoinGecko API, caches result, and returns price

### Requirement: Return price with metadata
The system SHALL return price along with source and timestamp.

#### Scenario: Price response includes metadata
- **WHEN** user requests price for "bitcoin"
- **THEN** response includes: price (string), source ("coingecko" or "cache"), updated_at (ISO timestamp)

### Requirement: Validate asset on transaction creation
The system SHALL validate that asset exists in CoinGecko before creating transaction.

#### Scenario: Valid asset accepted
- **WHEN** user creates transaction with asset_id "BTC" that resolves to valid CoinGecko ID
- **THEN** transaction is created successfully with CoinGecko ID stored

#### Scenario: Unknown asset with warning
- **WHEN** user creates transaction with asset_id "CUSTOM" that cannot be resolved
- **THEN** transaction is created with warning "price cannot be fetched automatically"

### Requirement: Pre-populate popular assets mapping
The system SHALL pre-populate symbol mappings for top 20 cryptocurrencies at startup.

#### Scenario: Popular symbols available immediately
- **WHEN** user searches for "BTC", "ETH", "USDC" immediately after server start
- **THEN** system returns results from pre-populated cache without API call
