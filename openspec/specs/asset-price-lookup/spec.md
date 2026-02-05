## MODIFIED Requirements

### Requirement: Get current price by asset UUID
The system SHALL provide an API endpoint to get current USD price for an asset using its internal UUID.

#### Scenario: Get price for valid asset by UUID
- **WHEN** user sends GET request to `/assets/{uuid}/price`
- **THEN** system returns current USD price as scaled integer (multiplied by 10^8)

#### Scenario: Get price for invalid UUID
- **WHEN** user sends GET request to `/assets/invalid-uuid/price`
- **THEN** system returns 404 Not Found with error message "asset not found"

### Requirement: Resolve symbol to asset UUID
The system SHALL resolve asset symbols to internal UUIDs using the Asset Registry.

#### Scenario: Resolve unambiguous symbol
- **WHEN** system needs to get price for symbol "BTC"
- **THEN** system resolves "BTC" to internal UUID from Asset Registry

#### Scenario: Resolve ambiguous symbol
- **WHEN** system needs to get price for symbol "USDC" without chain specification
- **THEN** system returns error "ambiguous symbol: specify chain_id" with list of available chains

#### Scenario: Resolve symbol with chain
- **WHEN** system needs to get price for symbol "USDC" with chain_id "ethereum"
- **THEN** system resolves to the specific USDC asset on Ethereum

#### Scenario: Symbol not in registry
- **WHEN** system cannot resolve symbol to UUID in Asset Registry
- **THEN** system returns error "asset not found in registry"

### Requirement: Multi-layer price lookup
The system SHALL use a multi-layer strategy: Redis cache, Price History DB, CoinGecko API, stale cache.

#### Scenario: Price retrieved from Redis cache
- **WHEN** price exists in Redis cache (within 60s TTL)
- **THEN** system returns cached price without DB or API call

#### Scenario: Price retrieved from Price History
- **WHEN** price not in Redis cache but exists in price_history (within last 5 minutes)
- **THEN** system returns price from DB, updates Redis cache, and returns price

#### Scenario: Price retrieved from CoinGecko
- **WHEN** price not in cache or recent history
- **THEN** system fetches from CoinGecko API, saves to price_history, updates Redis cache, and returns price

#### Scenario: Fallback to stale cache
- **WHEN** CoinGecko API fails and no recent price in DB
- **THEN** system returns stale cached price (24h TTL) with warning header "X-Price-Stale: true"

#### Scenario: No price available
- **WHEN** all price sources fail
- **THEN** system returns 503 Service Unavailable with error "price unavailable"

### Requirement: Return price with metadata
The system SHALL return price along with source, timestamp, and asset information.

#### Scenario: Price response includes metadata
- **WHEN** user requests price for asset
- **THEN** response includes: price (string), source ("cache", "database", "coingecko"), updated_at (ISO timestamp), asset_id (UUID), symbol

### Requirement: Validate asset on transaction creation
The system SHALL validate that asset exists in Asset Registry before creating transaction.

#### Scenario: Valid asset UUID accepted
- **WHEN** user creates transaction with valid asset_id UUID
- **THEN** transaction is created successfully

#### Scenario: Valid symbol resolved to UUID
- **WHEN** user creates transaction with asset_id as symbol "BTC"
- **THEN** system resolves to UUID and creates transaction with UUID stored

#### Scenario: Unknown asset rejected
- **WHEN** user creates transaction with asset_id that cannot be resolved to registry asset
- **THEN** system returns 400 Bad Request with error "asset not found in registry"

### Requirement: Batch price lookup
The system SHALL support fetching prices for multiple assets in a single request.

#### Scenario: Batch price request
- **WHEN** user sends POST request to `/assets/prices` with array of asset UUIDs
- **THEN** system returns map of asset_id to price data

#### Scenario: Batch request with some invalid assets
- **WHEN** batch request includes some invalid UUIDs
- **THEN** system returns prices for valid assets and null for invalid ones
