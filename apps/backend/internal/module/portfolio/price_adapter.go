package portfolio

import (
	"context"
	"math/big"

	"github.com/kislikjeka/moontrack/internal/platform/asset"
)

// AssetServiceInterface defines the asset service methods needed by the price adapter
type AssetServiceInterface interface {
	GetAssetsBySymbol(ctx context.Context, symbol string) ([]asset.Asset, error)
	GetCurrentPriceByCoinGeckoID(ctx context.Context, coinGeckoID string) (*big.Int, error)
}

// PortfolioPriceAdapter resolves asset symbols to CoinGecko IDs before price lookup.
type PortfolioPriceAdapter struct {
	assetSvc AssetServiceInterface
}

// NewPortfolioPriceAdapter creates a new portfolio price adapter.
func NewPortfolioPriceAdapter(assetSvc AssetServiceInterface) *PortfolioPriceAdapter {
	return &PortfolioPriceAdapter{assetSvc: assetSvc}
}

// GetPriceBySymbol resolves symbol → CoinGecko ID → price.
func (a *PortfolioPriceAdapter) GetPriceBySymbol(ctx context.Context, symbol string) (*big.Int, error) {
	coinGeckoID := symbolToCoinGeckoID(symbol)
	if coinGeckoID == "" {
		// ERC-20 fallback: look up by symbol in asset DB
		assets, err := a.assetSvc.GetAssetsBySymbol(ctx, symbol)
		if err != nil || len(assets) == 0 {
			return big.NewInt(0), nil
		}
		coinGeckoID = assets[0].CoinGeckoID
	}

	if coinGeckoID == "" {
		return big.NewInt(0), nil
	}

	price, err := a.assetSvc.GetCurrentPriceByCoinGeckoID(ctx, coinGeckoID)
	if err != nil {
		return big.NewInt(0), nil
	}

	return price, nil
}

// symbolToCoinGeckoID maps common native asset symbols to CoinGecko IDs.
func symbolToCoinGeckoID(symbol string) string {
	switch symbol {
	case "ETH":
		return "ethereum"
	case "BTC":
		return "bitcoin"
	case "MATIC":
		return "matic-network"
	case "AVAX":
		return "avalanche-2"
	case "BNB":
		return "binancecoin"
	case "SOL":
		return "solana"
	case "USDT":
		return "tether"
	case "USDC":
		return "usd-coin"
	case "XRP":
		return "ripple"
	case "ADA":
		return "cardano"
	case "DOGE":
		return "dogecoin"
	case "DOT":
		return "polkadot"
	case "LINK":
		return "chainlink"
	case "LTC":
		return "litecoin"
	case "BCH":
		return "bitcoin-cash"
	case "TON":
		return "the-open-network"
	case "SHIB":
		return "shiba-inu"
	case "TRX":
		return "tron"
	case "DAI":
		return "dai"
	case "WBTC":
		return "wrapped-bitcoin"
	default:
		return ""
	}
}
