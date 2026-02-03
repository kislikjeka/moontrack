## 1. Backend: Assets Module Setup

- [x] 1.1 Create module directory structure `internal/modules/assets/{domain,service,api}`
- [x] 1.2 Define Asset and SearchResult structs in `domain/asset.go`
- [x] 1.3 Add SearchCoins method to CoinGecko client using `/search` endpoint

## 2. Backend: Asset Search Service

- [x] 2.1 Implement AssetService with Search method
- [x] 2.2 Add search result caching in Redis with 24h TTL (key: `asset:search:<query>`)
- [x] 2.3 Add query validation (min 2 chars, max 50 chars)
- [x] 2.4 Limit search results to top 10 by market cap rank
- [x] 2.5 Handle CoinGecko API failures with graceful degradation

## 3. Backend: Symbol Resolution & Price Lookup

- [x] 3.1 Implement ResolveSymbol method with Redis caching (7 day TTL)
- [x] 3.2 Implement GetPrice method using existing PriceService
- [x] 3.3 Add price response with metadata (price, source, updated_at)
- [x] 3.4 Pre-populate mappings for top 20 assets at startup

## 4. Backend: HTTP Handlers & Routes

- [x] 4.1 Create GET `/assets/search` handler with query parameter
- [x] 4.2 Create GET `/assets/{id}/price` handler
- [x] 4.3 Register routes in main router (protected by JWT)
- [x] 4.4 Update OpenAPI spec with new endpoints

## 5. Backend: Tests

- [x] 5.1 Write unit tests for AssetService.Search
- [x] 5.2 Write unit tests for AssetService.ResolveSymbol
- [x] 5.3 Write unit tests for AssetService.GetPrice
- [x] 5.4 Write HTTP handler tests for search endpoint
- [x] 5.5 Write HTTP handler tests for price endpoint

## 6. Frontend: Asset Search API Client

- [x] 6.1 Add searchAssets function to services/api
- [x] 6.2 Add getAssetPrice function to services/api
- [x] 6.3 Define TypeScript types for Asset and SearchResult

## 7. Frontend: Autocomplete Component

- [x] 7.1 Create AssetAutocomplete component with debounced search (300ms)
- [x] 7.2 Display search results with symbol, name, and market cap rank
- [x] 7.3 Handle loading and error states
- [x] 7.4 Support keyboard navigation (up/down arrows, enter)

## 8. Frontend: Transaction Form Integration

- [x] 8.1 Replace hardcoded asset selector with AssetAutocomplete
- [x] 8.2 Fetch and auto-fill USD price when asset is selected
- [x] 8.3 Show price source indicator (CoinGecko/manual)
- [x] 8.4 Allow manual price override with toggle
- [x] 8.5 Handle unknown assets with warning message
