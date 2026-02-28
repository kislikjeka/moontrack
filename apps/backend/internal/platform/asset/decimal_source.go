package asset

import (
	"context"
	"strings"
)

// DecimalSource adapts asset.Repository to money.AssetDecimalSource.
// It queries the assets table (which has CoinGecko-registered assets).
type DecimalSource struct {
	repo Repository
}

// NewDecimalSource creates a new DecimalSource.
func NewDecimalSource(repo Repository) *DecimalSource {
	return &DecimalSource{repo: repo}
}

// GetDecimalsBySymbol looks up decimals in the assets table by symbol.
func (s *DecimalSource) GetDecimalsBySymbol(ctx context.Context, symbol, chainID string) (int, bool) {
	var chainPtr *string
	if chainID != "" {
		chainPtr = &chainID
	}

	asset, err := s.repo.GetBySymbol(ctx, strings.ToUpper(symbol), chainPtr)
	if err != nil || asset == nil {
		// Try lowercase (CoinGecko IDs are lowercase)
		asset, err = s.repo.GetBySymbol(ctx, strings.ToLower(symbol), chainPtr)
		if err != nil || asset == nil {
			return 0, false
		}
	}

	return asset.Decimals, true
}
