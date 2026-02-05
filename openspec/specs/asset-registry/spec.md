## ADDED Requirements

### Requirement: Store assets in PostgreSQL
The system SHALL store all asset metadata in a PostgreSQL `assets` table as the single source of truth.

#### Scenario: Asset record contains required fields
- **WHEN** an asset is stored in the registry
- **THEN** record contains: id (UUID), symbol, name, coingecko_id, decimals, asset_type, chain_id, contract_address, market_cap_rank, is_active, metadata, created_at, updated_at

#### Scenario: Asset has unique CoinGecko ID
- **WHEN** attempting to create asset with duplicate coingecko_id
- **THEN** system returns error "asset with this CoinGecko ID already exists"

#### Scenario: Asset has unique symbol-chain combination
- **WHEN** attempting to create asset with duplicate (symbol, chain_id) combination
- **THEN** system returns error "asset with this symbol and chain already exists"

### Requirement: Support multi-chain assets
The system SHALL store each deployment of a token on different chains as separate asset records.

#### Scenario: Same symbol on different chains
- **WHEN** USDC exists on Ethereum, Solana, and Polygon
- **THEN** system stores 3 separate asset records with different chain_id and coingecko_id values

#### Scenario: Native L1 assets have null chain_id
- **WHEN** asset is a native L1 coin (BTC, ETH, SOL)
- **THEN** asset record has chain_id set to NULL

#### Scenario: Token assets have chain_id and contract_address
- **WHEN** asset is an ERC-20/SPL token
- **THEN** asset record has chain_id (e.g., "ethereum") and contract_address populated

### Requirement: Get asset by ID
The system SHALL provide method to retrieve asset by its UUID.

#### Scenario: Get existing asset by ID
- **WHEN** calling GetAsset with valid UUID
- **THEN** system returns the asset record

#### Scenario: Get non-existent asset by ID
- **WHEN** calling GetAsset with unknown UUID
- **THEN** system returns error "asset not found"

### Requirement: Get asset by symbol with chain disambiguation
The system SHALL provide method to retrieve asset by symbol, requiring chain_id when symbol is ambiguous.

#### Scenario: Get unambiguous symbol
- **WHEN** calling GetAssetBySymbol("BTC", nil)
- **THEN** system returns the single BTC asset

#### Scenario: Get ambiguous symbol without chain
- **WHEN** calling GetAssetBySymbol("USDC", nil) and USDC exists on multiple chains
- **THEN** system returns error "ambiguous symbol: USDC exists on multiple chains" with list of options

#### Scenario: Get ambiguous symbol with chain
- **WHEN** calling GetAssetBySymbol("USDC", "ethereum")
- **THEN** system returns the USDC asset on Ethereum chain

### Requirement: Get all assets for a symbol
The system SHALL provide method to retrieve all assets matching a symbol across all chains.

#### Scenario: Get all chains for symbol
- **WHEN** calling GetAssetsBySymbol("USDC")
- **THEN** system returns array of all USDC assets (Ethereum, Solana, Polygon, etc.)

#### Scenario: Get symbol with single chain
- **WHEN** calling GetAssetsBySymbol("BTC")
- **THEN** system returns array with single BTC asset

### Requirement: Get asset by CoinGecko ID
The system SHALL provide method to retrieve asset by its CoinGecko ID.

#### Scenario: Get asset by valid CoinGecko ID
- **WHEN** calling GetAssetByCoinGeckoID("usd-coin")
- **THEN** system returns the USDC asset on Ethereum

#### Scenario: Get asset by unknown CoinGecko ID
- **WHEN** calling GetAssetByCoinGeckoID("unknown-coin")
- **THEN** system returns error "asset not found"

### Requirement: Create new asset
The system SHALL provide method to create new asset records.

#### Scenario: Create asset with all required fields
- **WHEN** calling CreateAsset with valid symbol, name, coingecko_id, decimals
- **THEN** system creates asset record and returns generated UUID

#### Scenario: Create asset with missing required field
- **WHEN** calling CreateAsset without coingecko_id
- **THEN** system returns validation error "coingecko_id is required"

### Requirement: Seed initial asset list
The system SHALL seed database with top 50 assets on first migration.

#### Scenario: Seed includes top cryptocurrencies
- **WHEN** database is initialized
- **THEN** system populates assets table with BTC, ETH, USDT, BNB, SOL, XRP, USDC, ADA, DOGE, TRX and other top 20 by market cap

#### Scenario: Seed includes multi-chain stablecoins
- **WHEN** database is initialized
- **THEN** system populates separate records for USDC and USDT on Ethereum, Solana, and Polygon

#### Scenario: Seed is idempotent
- **WHEN** seed runs multiple times
- **THEN** system skips existing assets based on coingecko_id

### Requirement: Expose asset registry via REST API
The system SHALL provide REST endpoints for asset operations.

#### Scenario: GET /assets/:id returns asset
- **WHEN** user sends GET request to `/assets/{uuid}`
- **THEN** system returns asset record in JSON format

#### Scenario: GET /assets with symbol filter
- **WHEN** user sends GET request to `/assets?symbol=USDC`
- **THEN** system returns all USDC assets across all chains

#### Scenario: GET /assets with chain filter
- **WHEN** user sends GET request to `/assets?chain=ethereum`
- **THEN** system returns all assets on Ethereum chain
