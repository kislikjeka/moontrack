package sync_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kislikjeka/moontrack/internal/platform/sync"
)

// mockSyncAssetRepo is a minimal mock for testing sync.DecimalSource.
type mockSyncAssetRepo struct {
	assets map[string]*sync.SyncAsset // keyed by "SYMBOL:chainID" or "SYMBOL:"
	err    error
}

func (m *mockSyncAssetRepo) Upsert(_ context.Context, _ *sync.SyncAsset) error { return nil }
func (m *mockSyncAssetRepo) GetAllBySymbol(_ context.Context, _ string) ([]sync.SyncAsset, error) {
	return nil, nil
}

func (m *mockSyncAssetRepo) GetBySymbol(_ context.Context, symbol, chainID string) (*sync.SyncAsset, error) {
	if m.err != nil {
		return nil, m.err
	}
	key := symbol + ":" + chainID
	a, ok := m.assets[key]
	if !ok {
		return nil, nil
	}
	return a, nil
}

func TestSyncDecimalSource_Found(t *testing.T) {
	repo := &mockSyncAssetRepo{
		assets: map[string]*sync.SyncAsset{
			"ETH:ethereum": {Symbol: "ETH", ChainID: "ethereum", Decimals: 18},
		},
	}

	src := sync.NewDecimalSource(repo)
	d, ok := src.GetDecimalsBySymbol(context.Background(), "ETH", "ethereum")

	assert.True(t, ok)
	assert.Equal(t, 18, d)
}

func TestSyncDecimalSource_NotFound(t *testing.T) {
	repo := &mockSyncAssetRepo{assets: map[string]*sync.SyncAsset{}}

	src := sync.NewDecimalSource(repo)
	d, ok := src.GetDecimalsBySymbol(context.Background(), "UNKNOWN", "")

	assert.False(t, ok)
	assert.Equal(t, 0, d)
}

func TestSyncDecimalSource_DBError(t *testing.T) {
	repo := &mockSyncAssetRepo{err: errors.New("db error")}

	src := sync.NewDecimalSource(repo)
	d, ok := src.GetDecimalsBySymbol(context.Background(), "ETH", "ethereum")

	assert.False(t, ok)
	assert.Equal(t, 0, d)
}

func TestSyncDecimalSource_EmptyChainID(t *testing.T) {
	repo := &mockSyncAssetRepo{
		assets: map[string]*sync.SyncAsset{
			"USDC:": {Symbol: "USDC", Decimals: 6},
		},
	}

	src := sync.NewDecimalSource(repo)
	d, ok := src.GetDecimalsBySymbol(context.Background(), "USDC", "")

	assert.True(t, ok)
	assert.Equal(t, 6, d)
}
