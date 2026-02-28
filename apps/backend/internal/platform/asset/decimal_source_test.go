package asset

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// mockAssetRepo is a minimal mock of Repository for testing DecimalSource.
type mockAssetRepo struct {
	assets map[string]*Asset // keyed by "SYMBOL" or "SYMBOL:chainID"
	err    error
}

func (m *mockAssetRepo) GetBySymbol(_ context.Context, symbol string, chainID *string) (*Asset, error) {
	if m.err != nil {
		return nil, m.err
	}
	key := symbol
	if chainID != nil {
		key = symbol + ":" + *chainID
	}
	a, ok := m.assets[key]
	if !ok {
		return nil, nil
	}
	return a, nil
}

// Satisfy the rest of the Repository interface with stubs.
func (m *mockAssetRepo) GetByID(context.Context, uuid.UUID) (*Asset, error)         { return nil, nil }
func (m *mockAssetRepo) GetByCoinGeckoID(context.Context, string) (*Asset, error)    { return nil, nil }
func (m *mockAssetRepo) GetAllBySymbol(context.Context, string) ([]Asset, error)     { return nil, nil }
func (m *mockAssetRepo) Create(context.Context, *Asset) error                        { return nil }
func (m *mockAssetRepo) Search(context.Context, string, int) ([]Asset, error)        { return nil, nil }
func (m *mockAssetRepo) GetActiveAssets(context.Context) ([]Asset, error)             { return nil, nil }
func (m *mockAssetRepo) GetByChain(context.Context, string) ([]Asset, error)         { return nil, nil }

func TestDecimalSource_Found(t *testing.T) {
	repo := &mockAssetRepo{
		assets: map[string]*Asset{
			"USDC": {Symbol: "USDC", Decimals: 6},
		},
	}

	src := NewDecimalSource(repo)
	d, ok := src.GetDecimalsBySymbol(context.Background(), "USDC", "")

	assert.True(t, ok)
	assert.Equal(t, 6, d)
}

func TestDecimalSource_NotFound(t *testing.T) {
	repo := &mockAssetRepo{assets: map[string]*Asset{}}

	src := NewDecimalSource(repo)
	d, ok := src.GetDecimalsBySymbol(context.Background(), "UNKNOWN", "")

	assert.False(t, ok)
	assert.Equal(t, 0, d)
}

func TestDecimalSource_FallsBackToLowerCase(t *testing.T) {
	repo := &mockAssetRepo{
		assets: map[string]*Asset{
			"usdc": {Symbol: "usdc", Decimals: 6},
		},
	}

	src := NewDecimalSource(repo)
	// DecimalSource first tries UPPER, then lower
	d, ok := src.GetDecimalsBySymbol(context.Background(), "USDC", "")

	assert.True(t, ok)
	assert.Equal(t, 6, d)
}

func TestDecimalSource_DBError(t *testing.T) {
	repo := &mockAssetRepo{err: errors.New("connection refused")}

	src := NewDecimalSource(repo)
	d, ok := src.GetDecimalsBySymbol(context.Background(), "ETH", "")

	assert.False(t, ok)
	assert.Equal(t, 0, d)
}

func TestDecimalSource_WithChainID(t *testing.T) {
	chain := "ethereum"
	repo := &mockAssetRepo{
		assets: map[string]*Asset{
			"USDC:" + chain: {Symbol: "USDC", Decimals: 6},
		},
	}

	src := NewDecimalSource(repo)
	d, ok := src.GetDecimalsBySymbol(context.Background(), "USDC", chain)

	assert.True(t, ok)
	assert.Equal(t, 6, d)
}
