package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/module/portfolio"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
)

// PortfolioServiceInterface defines the interface for portfolio operations
type PortfolioServiceInterface interface {
	GetPortfolioSummary(ctx context.Context, userID uuid.UUID) (*portfolio.PortfolioSummary, error)
	GetAssetBreakdown(ctx context.Context, userID uuid.UUID, assetID string) ([]portfolio.WalletBalance, error)
}

// PortfolioHandler handles portfolio-related HTTP requests
type PortfolioHandler struct {
	portfolioService PortfolioServiceInterface
}

// NewPortfolioHandler creates a new portfolio handler
func NewPortfolioHandler(portfolioService PortfolioServiceInterface) *PortfolioHandler {
	return &PortfolioHandler{
		portfolioService: portfolioService,
	}
}

// PortfolioSummaryResponse represents the portfolio summary API response
type PortfolioSummaryResponse struct {
	TotalUSDValue  string                  `json:"total_usd_value"` // String representation of big.Int
	TotalAssets    int                     `json:"total_assets"`
	AssetHoldings  []AssetHoldingResponse  `json:"asset_holdings"`
	WalletBalances []WalletBalanceResponse `json:"wallet_balances"`
	LastUpdated    string                  `json:"last_updated"` // ISO 8601
}

// AssetHoldingResponse represents an asset holding in the API response
type AssetHoldingResponse struct {
	AssetID      string `json:"asset_id"`
	TotalAmount  string `json:"total_amount"`  // String representation of big.Int
	USDValue     string `json:"usd_value"`     // String representation
	CurrentPrice string `json:"current_price"` // String representation
}

// WalletBalanceResponse represents a wallet balance in the API response
type WalletBalanceResponse struct {
	WalletID   string                 `json:"wallet_id"`
	WalletName string                 `json:"wallet_name"`
	ChainID    string                 `json:"chain_id"`
	Assets     []AssetBalanceResponse `json:"assets"`
	TotalUSD   string                 `json:"total_usd"`
}

// AssetBalanceResponse represents an asset balance in a wallet
type AssetBalanceResponse struct {
	AssetID  string `json:"asset_id"`
	Amount   string `json:"amount"`
	USDValue string `json:"usd_value"`
	Price    string `json:"price"`
}

// GetPortfolioSummary handles GET /portfolio
func (h *PortfolioHandler) GetPortfolioSummary(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context (set by JWT middleware)
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Get portfolio summary from service
	summary, err := h.portfolioService.GetPortfolioSummary(r.Context(), userID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to fetch portfolio summary")
		return
	}

	// Convert to response format (convert big.Int to strings)
	assetHoldings := make([]AssetHoldingResponse, len(summary.AssetHoldings))
	for i, holding := range summary.AssetHoldings {
		assetHoldings[i] = AssetHoldingResponse{
			AssetID:      holding.AssetID,
			TotalAmount:  holding.TotalAmount.String(),
			USDValue:     holding.USDValue.String(),
			CurrentPrice: holding.CurrentPrice.String(),
		}
	}

	walletBalances := make([]WalletBalanceResponse, len(summary.WalletBalances))
	for i, w := range summary.WalletBalances {
		assets := make([]AssetBalanceResponse, len(w.Assets))
		for j, asset := range w.Assets {
			assets[j] = AssetBalanceResponse{
				AssetID:  asset.AssetID,
				Amount:   asset.Amount.String(),
				USDValue: asset.USDValue.String(),
				Price:    asset.Price.String(),
			}
		}

		walletBalances[i] = WalletBalanceResponse{
			WalletID:   w.WalletID.String(),
			WalletName: w.WalletName,
			ChainID:    w.ChainID,
			Assets:     assets,
			TotalUSD:   w.TotalUSD.String(),
		}
	}

	response := PortfolioSummaryResponse{
		TotalUSDValue:  summary.TotalUSDValue.String(),
		TotalAssets:    summary.TotalAssets,
		AssetHoldings:  assetHoldings,
		WalletBalances: walletBalances,
		LastUpdated:    time.Now().Format(time.RFC3339),
	}

	respondWithJSON(w, http.StatusOK, response)
}

// GetAssetBreakdown handles GET /portfolio/assets/{assetID}
func (h *PortfolioHandler) GetAssetBreakdown(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Get asset ID from URL parameter
	assetID := r.URL.Query().Get("asset_id")
	if assetID == "" {
		respondWithError(w, http.StatusBadRequest, "asset_id is required")
		return
	}

	// Get asset breakdown from service
	walletBalances, err := h.portfolioService.GetAssetBreakdown(r.Context(), userID, assetID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to fetch asset breakdown")
		return
	}

	// Convert to response format
	response := make([]WalletBalanceResponse, len(walletBalances))
	for i, w := range walletBalances {
		assets := make([]AssetBalanceResponse, len(w.Assets))
		for j, asset := range w.Assets {
			assets[j] = AssetBalanceResponse{
				AssetID:  asset.AssetID,
				Amount:   asset.Amount.String(),
				USDValue: asset.USDValue.String(),
				Price:    asset.Price.String(),
			}
		}

		response[i] = WalletBalanceResponse{
			WalletID:   w.WalletID.String(),
			WalletName: w.WalletName,
			ChainID:    w.ChainID,
			Assets:     assets,
			TotalUSD:   w.TotalUSD.String(),
		}
	}

	respondWithJSON(w, http.StatusOK, response)
}
