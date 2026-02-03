## ADDED Requirements

### Requirement: Store price history in TimescaleDB
The system SHALL store historical price data in a TimescaleDB hypertable for efficient time-series queries.

#### Scenario: Price record contains required fields
- **WHEN** a price is recorded
- **THEN** record contains: time (timestamptz), asset_id (UUID), price_usd (NUMERIC(78,0) scaled by 10^8), volume_24h (optional), market_cap (optional), source

#### Scenario: Primary key is asset_id and time
- **WHEN** attempting to insert duplicate (asset_id, time) combination
- **THEN** system updates existing record (upsert behavior)

### Requirement: Configure TimescaleDB hypertable
The system SHALL configure price_history as a hypertable with 7-day chunks.

#### Scenario: Data is partitioned by time
- **WHEN** price data spans multiple weeks
- **THEN** TimescaleDB automatically creates separate chunks for each 7-day period

### Requirement: Compress old price data
The system SHALL compress price data older than 30 days to reduce storage.

#### Scenario: Compression policy is active
- **WHEN** chunk contains data older than 30 days
- **THEN** TimescaleDB automatically compresses the chunk, segmented by asset_id

### Requirement: Retain price data for 2 years
The system SHALL automatically drop price data older than 2 years.

#### Scenario: Retention policy removes old data
- **WHEN** price data is older than 2 years
- **THEN** TimescaleDB automatically drops the corresponding chunks

### Requirement: Record current price
The system SHALL provide method to record a price point for an asset.

#### Scenario: Record price with source
- **WHEN** calling RecordPrice(assetID, price, "coingecko")
- **THEN** system inserts record with current timestamp and specified source

#### Scenario: Record price with optional fields
- **WHEN** calling RecordPrice with volume and market_cap
- **THEN** system stores all provided fields

### Requirement: Get current price from history
The system SHALL provide method to get most recent price for an asset.

#### Scenario: Get latest price
- **WHEN** calling GetCurrentPrice(assetID) and prices exist
- **THEN** system returns most recent price record

#### Scenario: Get current price with no history
- **WHEN** calling GetCurrentPrice(assetID) and no prices recorded
- **THEN** system returns error "no price data available"

### Requirement: Get price at specific time
The system SHALL provide method to get price at a specific point in time.

#### Scenario: Get price at exact time
- **WHEN** calling GetPriceAt(assetID, timestamp) and exact record exists
- **THEN** system returns the matching price record

#### Scenario: Get price at time with interpolation
- **WHEN** calling GetPriceAt(assetID, timestamp) and no exact record exists
- **THEN** system returns the closest earlier price record

#### Scenario: Get price before any records
- **WHEN** calling GetPriceAt(assetID, timestamp) for time before first record
- **THEN** system returns error "no price data available for this time"

### Requirement: Get price history for time range
The system SHALL provide method to get price history between two timestamps.

#### Scenario: Get hourly prices
- **WHEN** calling GetPriceHistory(assetID, from, to, "1h")
- **THEN** system returns hourly price points from raw price_history table

#### Scenario: Get daily prices
- **WHEN** calling GetPriceHistory(assetID, from, to, "1d")
- **THEN** system returns daily OHLCV data from price_history_daily continuous aggregate

#### Scenario: Get weekly prices
- **WHEN** calling GetPriceHistory(assetID, from, to, "1w")
- **THEN** system returns weekly aggregated prices

#### Scenario: Get prices with empty range
- **WHEN** calling GetPriceHistory with range containing no data
- **THEN** system returns empty array

### Requirement: Create daily price aggregate
The system SHALL maintain a continuous aggregate for daily OHLCV prices.

#### Scenario: Aggregate contains OHLCV fields
- **WHEN** querying price_history_daily view
- **THEN** each row contains: asset_id, day, open, high, low, close, avg_volume

#### Scenario: Aggregate refreshes automatically
- **WHEN** new price data is inserted
- **THEN** continuous aggregate updates within 1 hour

### Requirement: Background price updater
The system SHALL run a background job to fetch and record prices for active assets.

#### Scenario: Updater runs periodically
- **WHEN** price updater is running
- **THEN** system fetches prices for all active assets every 5 minutes

#### Scenario: Updater batches API requests
- **WHEN** fetching prices for many assets
- **THEN** system batches requests to 50 assets per CoinGecko API call

#### Scenario: Updater handles API failures
- **WHEN** CoinGecko API returns error
- **THEN** system logs error and retries on next interval

### Requirement: Expose price history via REST API
The system SHALL provide REST endpoint for price history queries.

#### Scenario: GET /assets/:id/history returns price history
- **WHEN** user sends GET request to `/assets/{uuid}/history?from=2024-01-01&to=2024-01-31&interval=1d`
- **THEN** system returns daily OHLCV data for the specified period

#### Scenario: History endpoint validates parameters
- **WHEN** user sends GET request with invalid interval
- **THEN** system returns 400 Bad Request with error "interval must be 1h, 1d, or 1w"

#### Scenario: History endpoint limits range
- **WHEN** user sends GET request with range exceeding 1 year
- **THEN** system returns 400 Bad Request with error "time range cannot exceed 1 year"
