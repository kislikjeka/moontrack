package sync

import "context"

// DecimalSource adapts SyncAssetRepository to money.AssetDecimalSource.
// It queries the sync asset store (chain_assets table) for token metadata discovered during sync.
type DecimalSource struct {
	repo SyncAssetRepository
}

// NewDecimalSource creates a new DecimalSource backed by the sync asset store.
func NewDecimalSource(repo SyncAssetRepository) *DecimalSource {
	return &DecimalSource{repo: repo}
}

// GetDecimalsBySymbol looks up decimals in the sync asset store.
func (s *DecimalSource) GetDecimalsBySymbol(ctx context.Context, symbol, chainID string) (int, bool) {
	asset, err := s.repo.GetBySymbol(ctx, symbol, chainID)
	if err != nil || asset == nil {
		return 0, false
	}
	return asset.Decimals, true
}
