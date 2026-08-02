package portfolio

import (
	"context"
	"math/big"
)

// CoinGeckoPriceClient is the spot-price call this adapter needs, named in
// CoinGecko's own vocabulary (a slug, not an asset).
type CoinGeckoPriceClient interface {
	GetCurrentPrices(ctx context.Context, coinGeckoIDs []string) (map[string]*big.Int, error)
}

// PortfolioPriceAdapter values a holding from its ticker.
//
// It asks CoinGecko directly now (#59). It used to fall back, for any ticker
// outside the table below, to asset.Service.GetAssetsBySymbol and then take
// assets[0].CoinGeckoID — the first row of an arbitrarily ordered set of
// same-ticker assets. On a portfolio that is a live mispricing: two tokens
// sharing a ticker would both be valued at whichever one the database happened
// to return first.
//
// The fallback is not rebuilt against asset_registry. The registry is keyed on
// (chain, contract) exactly so a bare ticker cannot name an asset, and reviving
// a symbol-to-slug guess against it would restore the bug in a new place. What
// remains is the compiled-in table of unambiguous major coins.
//
// Unresolvable tickers value at ZERO, which is this adapter's pre-existing
// convention for "no price" (every error path already returns big.NewInt(0)) and
// is what the portfolio service expects. Valuing per-token holdings properly is
// the job of the registry-keyed price pipeline, not of a symbol guess.
type PortfolioPriceAdapter struct {
	cg CoinGeckoPriceClient
}

// NewPortfolioPriceAdapter creates a new portfolio price adapter.
func NewPortfolioPriceAdapter(cg CoinGeckoPriceClient) *PortfolioPriceAdapter {
	return &PortfolioPriceAdapter{cg: cg}
}

// GetPriceBySymbol resolves symbol -> CoinGecko slug -> price, returning zero
// when the ticker is not one it can resolve unambiguously.
func (a *PortfolioPriceAdapter) GetPriceBySymbol(ctx context.Context, symbol string) (*big.Int, error) {
	coinGeckoID := symbolToCoinGeckoID(symbol)
	if coinGeckoID == "" || a.cg == nil {
		return big.NewInt(0), nil
	}

	prices, err := a.cg.GetCurrentPrices(ctx, []string{coinGeckoID})
	if err != nil {
		return big.NewInt(0), nil
	}
	p, ok := prices[coinGeckoID]
	if !ok || p == nil {
		return big.NewInt(0), nil
	}
	return p, nil
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
