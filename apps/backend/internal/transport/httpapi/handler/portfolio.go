package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/module/portfolio"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
	"github.com/kislikjeka/moontrack/pkg/money"
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
	WalletID   string                  `json:"wallet_id"`
	WalletName string                  `json:"wallet_name"`
	Assets     []AssetBalanceResponse  `json:"assets"`
	Holdings   []HoldingGroupResponse  `json:"holdings,omitempty"`
	TotalUSD   string                  `json:"total_usd"`
}

// AssetBalanceResponse represents an asset balance in a wallet
type AssetBalanceResponse struct {
	AssetID  string `json:"asset_id"`
	ChainID  string `json:"chain_id,omitempty"`
	Amount   string `json:"amount"`
	USDValue string `json:"usd_value"`
	Price    string `json:"price"`
}

// HoldingGroupResponse is the JSON representation of a holding group (asset across chains).
type HoldingGroupResponse struct {
	AssetID       string                 `json:"asset_id"`
	TotalAmount   string                 `json:"total_amount"`
	TotalUSDValue string                 `json:"total_usd_value"`
	Price         string                 `json:"price"`
	AggregatedWAC string                 `json:"aggregated_wac,omitempty"`
	Chains        []ChainHoldingResponse `json:"chains"`
}

// ChainHoldingResponse is the JSON representation of a per-chain holding.
type ChainHoldingResponse struct {
	ChainID  string `json:"chain_id"`
	Amount   string `json:"amount"`
	USDValue string `json:"usd_value"`
	WAC      string `json:"wac,omitempty"`
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

	// Convert to response format (convert big.Int to strings, amounts to human-readable)
	assetHoldings := make([]AssetHoldingResponse, len(summary.AssetHoldings))
	for i, holding := range summary.AssetHoldings {
		assetHoldings[i] = AssetHoldingResponse{
			AssetID:      holding.AssetID,
			TotalAmount:  money.FromBaseUnits(holding.TotalAmount, holding.Decimals),
			USDValue:     money.FormatUSD(holding.USDValue),
			CurrentPrice: money.FormatUSD(holding.CurrentPrice),
		}
	}

	walletBalances := make([]WalletBalanceResponse, len(summary.WalletBalances))
	for i, w := range summary.WalletBalances {
		assets := make([]AssetBalanceResponse, len(w.Assets))
		for j, asset := range w.Assets {
			assets[j] = AssetBalanceResponse{
				AssetID:  asset.AssetID,
				ChainID:  asset.ChainID,
				Amount:   money.FromBaseUnits(asset.Amount, asset.Decimals),
				USDValue: money.FormatUSD(asset.USDValue),
				Price:    money.FormatUSD(asset.Price),
			}
		}

		// Serialize holdings
		holdings := make([]HoldingGroupResponse, len(w.Holdings))
		for k, hg := range w.Holdings {
			decimals := hg.Decimals
			chains := make([]ChainHoldingResponse, len(hg.Chains))
			for l, ch := range hg.Chains {
				chains[l] = ChainHoldingResponse{
					ChainID:  ch.ChainID,
					Amount:   money.FromBaseUnits(ch.Amount, decimals),
					USDValue: money.FormatUSD(ch.USDValue),
				}
				if ch.WAC != nil {
					chains[l].WAC = money.FormatUSD(ch.WAC)
				}
			}
			holdings[k] = HoldingGroupResponse{
				AssetID:       hg.AssetID,
				TotalAmount:   money.FromBaseUnits(hg.TotalAmount, decimals),
				TotalUSDValue: money.FormatUSD(hg.TotalUSDValue),
				Price:         money.FormatUSD(hg.Price),
				Chains:        chains,
			}
			if hg.AggregatedWAC != nil {
				holdings[k].AggregatedWAC = money.FormatUSD(hg.AggregatedWAC)
			}
		}

		walletBalances[i] = WalletBalanceResponse{
			WalletID:   w.WalletID.String(),
			WalletName: w.WalletName,
			Assets:     assets,
			Holdings:   holdings,
			TotalUSD:   money.FormatUSD(w.TotalUSD),
		}
	}

	response := PortfolioSummaryResponse{
		TotalUSDValue:  money.FormatUSD(summary.TotalUSDValue),
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
				ChainID:  asset.ChainID,
				Amount:   money.FromBaseUnits(asset.Amount, asset.Decimals),
				USDValue: money.FormatUSD(asset.USDValue),
				Price:    money.FormatUSD(asset.Price),
			}
		}

		response[i] = WalletBalanceResponse{
			WalletID:   w.WalletID.String(),
			WalletName: w.WalletName,
			Assets:     assets,
			TotalUSD:   money.FormatUSD(w.TotalUSD),
		}
	}

	respondWithJSON(w, http.StatusOK, response)
}
