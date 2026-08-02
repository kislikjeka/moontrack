package sync

import (
	"context"
	"math/big"

	"github.com/kislikjeka/moontrack/internal/platform/asset"
	"github.com/kislikjeka/moontrack/internal/platform/price"
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
func (a *SyncAssetAdapter) GetPriceBySymbol(ctx context.Context, symbol string) (*big.Int, error) {
	// Native assets have known CoinGecko IDs
	coinGeckoID := a.getNativeCoinGeckoID(symbol)
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

// ContractQuotabilityProbe is the price-side probe, stated in the price
// package's own vocabulary (chain and contract as plain strings).
type ContractQuotabilityProbe interface {
	IsQuotable(ctx context.Context, chain, contract string) error
}

// QuotabilityProbeAdapter adapts a contract-addressable price probe to the
// AssetKey vocabulary the knownness worker speaks.
//
// It exists so the price package never has to know what an AssetKey is: the
// dependency arrow runs sync → price, and this is the one place the two
// vocabularies meet.
type QuotabilityProbeAdapter struct {
	probe ContractQuotabilityProbe
}

// NewQuotabilityProbeAdapter wraps a price-side probe for the knownness worker.
func NewQuotabilityProbeAdapter(probe ContractQuotabilityProbe) *QuotabilityProbeAdapter {
	return &QuotabilityProbeAdapter{probe: probe}
}

// IsQuotable asks the price provider about an on-chain identity.
//
// A NATIVE key is refused outright, with ErrUnsupportedChain. The probe is
// addressed by contract and a native coin has none, so asking would be
// meaningless — and, more importantly, a native identity must never reach this
// path at all: level 1 grants it knownness by construction. Reaching here means
// the symbol check failed, i.e. this is a contract-less leg that is NOT the
// chain's coin, and the honest answer for it is "the provider cannot value
// this".
func (a *QuotabilityProbeAdapter) IsQuotable(ctx context.Context, key AssetKey) error {
	if key.IsNative() {
		return price.ErrUnsupportedChain
	}
	return a.probe.IsQuotable(ctx, key.Chain, key.Contract)
}

// getNativeCoinGeckoID returns CoinGecko ID for native chain assets
func (a *SyncAssetAdapter) getNativeCoinGeckoID(symbol string) string {
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
