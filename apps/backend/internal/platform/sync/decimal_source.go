package sync

import "context"

// DecimalSource adapts ZerionAssetRepository to money.AssetDecimalSource.
// It queries the zerion_assets table for token metadata discovered during sync.
type DecimalSource struct {
	repo ZerionAssetRepository
}

// NewDecimalSource creates a new DecimalSource backed by zerion_assets.
func NewDecimalSource(repo ZerionAssetRepository) *DecimalSource {
	return &DecimalSource{repo: repo}
}

// GetDecimalsBySymbol looks up decimals in the zerion_assets table.
func (s *DecimalSource) GetDecimalsBySymbol(ctx context.Context, symbol, chainID string) (int, bool) {
	asset, err := s.repo.GetBySymbol(ctx, symbol, chainID)
	if err != nil || asset == nil {
		return 0, false
	}
	return asset.Decimals, true
}
