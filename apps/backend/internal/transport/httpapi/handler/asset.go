package handler

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/platform/asset"
)

// AssetServiceInterface defines the interface for asset operations needed by AssetHandler
type AssetServiceInterface interface {
	GetAsset(ctx context.Context, id uuid.UUID) (*asset.Asset, error)
	GetAssetBySymbol(ctx context.Context, symbol string, chainID *string) (*asset.Asset, error)
	GetAssetsBySymbol(ctx context.Context, symbol string) ([]asset.Asset, error)
	GetAssetsByChain(ctx context.Context, chainID string) ([]asset.Asset, error)
	GetActiveAssets(ctx context.Context) ([]asset.Asset, error)
	SearchAssetsWithFallback(ctx context.Context, query string) ([]asset.Asset, error)
	GetCurrentPrice(ctx context.Context, assetID uuid.UUID) (*asset.PricePoint, error)
	GetBatchPrices(ctx context.Context, assetIDs []uuid.UUID) (map[uuid.UUID]*asset.PricePoint, error)
	GetPriceHistory(ctx context.Context, assetID uuid.UUID, from, to time.Time, interval asset.PriceInterval) ([]asset.PricePoint, error)
	GetCurrentPriceByCoinGeckoID(ctx context.Context, coinGeckoID string) (*big.Int, error)
	GetHistoricalPriceByCoinGeckoID(ctx context.Context, coinGeckoID string, date time.Time) (*big.Int, error)
	GetDecimals(ctx context.Context, coinGeckoID string) (int, error)
}

// AssetHandler handles asset-related HTTP requests
type AssetHandler struct {
	service AssetServiceInterface
}

// NewAssetHandler creates a new asset handler
func NewAssetHandler(service AssetServiceInterface) *AssetHandler {
	return &AssetHandler{
		service: service,
	}
}

// AssetResponse represents an asset in the API response
type AssetResponse struct {
	ID              string  `json:"id"`
	Symbol          string  `json:"symbol"`
	Name            string  `json:"name"`
	CoinGeckoID     string  `json:"coingecko_id"`
	Decimals        int     `json:"decimals"`
	AssetType       string  `json:"asset_type"`
	ChainID         *string `json:"chain_id,omitempty"`
	ContractAddress *string `json:"contract_address,omitempty"`
	MarketCapRank   *int    `json:"market_cap_rank,omitempty"`
	IsActive        bool    `json:"is_active"`
}

// PriceResponse represents a price in the API response
type PriceResponse struct {
	AssetID   string `json:"asset_id"`
	PriceUSD  string `json:"price_usd"`
	Source    string `json:"source"`
	Timestamp string `json:"timestamp"`
	IsStale   bool   `json:"is_stale,omitempty"`
}

// PriceHistoryResponse represents price history in the API response
type PriceHistoryResponse struct {
	AssetID  string              `json:"asset_id"`
	From     string              `json:"from"`
	To       string              `json:"to"`
	Interval string              `json:"interval"`
	Prices   []PricePointResponse `json:"prices"`
}

// PricePointResponse represents a single price point
type PricePointResponse struct {
	Timestamp string `json:"timestamp"`
	PriceUSD  string `json:"price_usd"`
}

// BatchPriceRequest represents a batch price lookup request
type BatchPriceRequest struct {
	AssetIDs []string `json:"asset_ids"`
}

// BatchPriceResponse represents a batch price lookup response
type BatchPriceResponse struct {
	Prices []PriceResponse `json:"prices"`
}

// GetAssetByID handles GET /api/assets/:id
func (h *AssetHandler) GetAssetByID(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid asset ID format")
		return
	}

	a, err := h.service.GetAsset(r.Context(), id)
	if err != nil {
		if errors.Is(err, asset.ErrAssetNotFound) {
			respondWithError(w, http.StatusNotFound, "asset not found")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to get asset")
		return
	}

	respondWithJSON(w, http.StatusOK, toAssetResponse(a))
}

// ListAssets handles GET /api/assets?symbol=&chain=
func (h *AssetHandler) ListAssets(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	chainID := r.URL.Query().Get("chain")

	var assets []asset.Asset
	var err error

	if symbol != "" {
		if chainID != "" {
			// Get specific asset by symbol and chain
			a, err := h.service.GetAssetBySymbol(r.Context(), symbol, &chainID)
			if err != nil {
				if errors.Is(err, asset.ErrAssetNotFound) {
					respondWithJSON(w, http.StatusOK, map[string][]AssetResponse{"assets": {}})
					return
				}
				respondWithError(w, http.StatusInternalServerError, "failed to get assets")
				return
			}
			assets = []asset.Asset{*a}
		} else {
			// Get all assets with this symbol
			assets, err = h.service.GetAssetsBySymbol(r.Context(), symbol)
		}
	} else if chainID != "" {
		// Get all assets on this chain
		assets, err = h.service.GetAssetsByChain(r.Context(), chainID)
	} else {
		// Get all active assets
		assets, err = h.service.GetActiveAssets(r.Context())
	}

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to list assets")
		return
	}

	response := make([]AssetResponse, len(assets))
	for i, a := range assets {
		response[i] = toAssetResponse(&a)
	}

	respondWithJSON(w, http.StatusOK, map[string][]AssetResponse{"assets": response})
}

// SearchAssets handles GET /api/assets/search?q=query
func (h *AssetHandler) SearchAssets(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		respondWithError(w, http.StatusBadRequest, "search query (q) is required")
		return
	}

	if len(query) < 2 {
		respondWithError(w, http.StatusBadRequest, "search query must be at least 2 characters")
		return
	}

	if len(query) > 50 {
		respondWithError(w, http.StatusBadRequest, "search query must not exceed 50 characters")
		return
	}

	// Search with fallback to external provider
	assets, err := h.service.SearchAssetsWithFallback(r.Context(), query)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to search assets")
		return
	}

	// Limit results to 10
	if len(assets) > 10 {
		assets = assets[:10]
	}

	response := make([]AssetResponse, len(assets))
	for i, a := range assets {
		response[i] = toAssetResponse(&a)
	}

	respondWithJSON(w, http.StatusOK, map[string][]AssetResponse{"assets": response})
}

// GetAssetPrice handles GET /api/assets/:id/price
func (h *AssetHandler) GetAssetPrice(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid asset ID format")
		return
	}

	pricePoint, err := h.service.GetCurrentPrice(r.Context(), id)
	if err != nil {
		if errors.Is(err, asset.ErrAssetNotFound) {
			respondWithError(w, http.StatusNotFound, "asset not found")
			return
		}
		if errors.Is(err, asset.ErrPriceUnavailable) || errors.Is(err, asset.ErrNoPriceData) {
			respondWithError(w, http.StatusNotFound, "price not available")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to get price")
		return
	}

	// Set stale header if price is stale
	SetPriceStaleHeader(w, pricePoint.IsStale)

	respondWithJSON(w, http.StatusOK, PriceResponse{
		AssetID:   id.String(),
		PriceUSD:  pricePoint.PriceUSD.String(),
		Source:    string(pricePoint.Source),
		Timestamp: pricePoint.Time.Format(time.RFC3339),
		IsStale:   pricePoint.IsStale,
	})
}

// GetBatchPrices handles POST /api/assets/prices
func (h *AssetHandler) GetBatchPrices(w http.ResponseWriter, r *http.Request) {
	var req BatchPriceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.AssetIDs) == 0 {
		respondWithJSON(w, http.StatusOK, BatchPriceResponse{Prices: []PriceResponse{}})
		return
	}

	if len(req.AssetIDs) > 100 {
		respondWithError(w, http.StatusBadRequest, "maximum 100 assets per request")
		return
	}

	// Parse UUIDs
	uuids := make([]uuid.UUID, 0, len(req.AssetIDs))
	for _, idStr := range req.AssetIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			continue // Skip invalid UUIDs
		}
		uuids = append(uuids, id)
	}

	prices, err := h.service.GetBatchPrices(r.Context(), uuids)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to get prices")
		return
	}

	response := make([]PriceResponse, 0, len(prices))
	for id, pricePoint := range prices {
		response = append(response, PriceResponse{
			AssetID:   id.String(),
			PriceUSD:  pricePoint.PriceUSD.String(),
			Source:    string(pricePoint.Source),
			Timestamp: pricePoint.Time.Format(time.RFC3339),
			IsStale:   pricePoint.IsStale,
		})
	}

	respondWithJSON(w, http.StatusOK, BatchPriceResponse{Prices: response})
}

// GetPriceHistory handles GET /api/assets/:id/history?from=&to=&interval=
func (h *AssetHandler) GetPriceHistory(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid asset ID format")
		return
	}

	// Parse query parameters
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	intervalStr := r.URL.Query().Get("interval")

	if fromStr == "" || toStr == "" {
		respondWithError(w, http.StatusBadRequest, "from and to parameters are required")
		return
	}

	from, err := time.Parse(time.RFC3339, fromStr)
	if err != nil {
		from, err = time.Parse("2006-01-02", fromStr)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "invalid from date format (use RFC3339 or YYYY-MM-DD)")
			return
		}
	}

	to, err := time.Parse(time.RFC3339, toStr)
	if err != nil {
		to, err = time.Parse("2006-01-02", toStr)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "invalid to date format (use RFC3339 or YYYY-MM-DD)")
			return
		}
		// If only date, set to end of day
		to = to.Add(24*time.Hour - time.Second)
	}

	// Default interval to daily
	if intervalStr == "" {
		intervalStr = "1d"
	}

	interval, err := asset.ParsePriceInterval(intervalStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "interval must be 1h, 1d, or 1w")
		return
	}

	// Validate time range
	maxRange := time.Hour * 24 * 365
	if to.Sub(from) > maxRange {
		respondWithError(w, http.StatusBadRequest, "time range cannot exceed 1 year")
		return
	}

	pricePoints, err := h.service.GetPriceHistory(r.Context(), id, from, to, interval)
	if err != nil {
		if errors.Is(err, asset.ErrAssetNotFound) {
			respondWithError(w, http.StatusNotFound, "asset not found")
			return
		}
		if errors.Is(err, asset.ErrInvalidTimeRange) {
			respondWithError(w, http.StatusBadRequest, "invalid time range")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to get price history")
		return
	}

	prices := make([]PricePointResponse, len(pricePoints))
	for i, pp := range pricePoints {
		prices[i] = PricePointResponse{
			Timestamp: pp.Time.Format(time.RFC3339),
			PriceUSD:  pp.PriceUSD.String(),
		}
	}

	respondWithJSON(w, http.StatusOK, PriceHistoryResponse{
		AssetID:  id.String(),
		From:     from.Format(time.RFC3339),
		To:       to.Format(time.RFC3339),
		Interval: intervalStr,
		Prices:   prices,
	})
}

// toAssetResponse converts a domain Asset to an API response
func toAssetResponse(a *asset.Asset) AssetResponse {
	return AssetResponse{
		ID:              a.ID.String(),
		Symbol:          a.Symbol,
		Name:            a.Name,
		CoinGeckoID:     a.CoinGeckoID,
		Decimals:        a.Decimals,
		AssetType:       string(a.AssetType),
		ChainID:         a.ChainID,
		ContractAddress: a.ContractAddress,
		MarketCapRank:   a.MarketCapRank,
		IsActive:        a.IsActive,
	}
}
