package sync

import (
	"context"
	"math/big"

	"github.com/kislikjeka/moontrack/internal/platform/asset"
)

// SyncAssetAdapter adapts asset.Service for sync operations
type SyncAssetAdapter struct {
	assetSvc *asset.Service
}

// NewSyncAssetAdapter creates a new adapter
func NewSyncAssetAdapter(assetSvc *asset.Service) *SyncAssetAdapter {
	return &SyncAssetAdapter{assetSvc: assetSvc}
}

// GetPriceBySymbol returns the current USD price for an asset by symbol
// Maps symbol to CoinGecko ID and fetches price
func (a *SyncAssetAdapter) GetPriceBySymbol(ctx context.Context, symbol string, chainID int64) (*big.Int, error) {
	// Native assets have known CoinGecko IDs
	coinGeckoID := a.getNativeCoinGeckoID(symbol, chainID)
	if coinGeckoID == "" {
		// For ERC-20 tokens, try to find by symbol
		// This is a best-effort lookup
		assets, err := a.assetSvc.GetAssetsBySymbol(ctx, symbol)
		if err != nil || len(assets) == 0 {
			return nil, nil // Price unavailable - graceful degradation
		}
		coinGeckoID = assets[0].CoinGeckoID
	}

	if coinGeckoID == "" {
		return nil, nil
	}

	price, err := a.assetSvc.GetCurrentPriceByCoinGeckoID(ctx, coinGeckoID)
	if err != nil {
		return nil, nil // Graceful degradation
	}

	return price, nil
}

// getNativeCoinGeckoID returns CoinGecko ID for native chain assets
func (a *SyncAssetAdapter) getNativeCoinGeckoID(symbol string, chainID int64) string {
	switch symbol {
	case "ETH":
		return "ethereum"
	case "MATIC":
		return "matic-network"
	case "AVAX":
		return "avalanche-2"
	case "BNB":
		return "binancecoin"
	default:
		return ""
	}
}
