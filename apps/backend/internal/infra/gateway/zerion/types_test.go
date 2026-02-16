package zerion_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kislikjeka/moontrack/internal/infra/gateway/zerion"
)

func TestChainMaps_Consistency(t *testing.T) {
	// Every entry in ZerionChainToID must have a reverse mapping in IDToZerionChain
	for chain, id := range zerion.ZerionChainToID {
		reverse, ok := zerion.IDToZerionChain[id]
		assert.True(t, ok, "IDToZerionChain missing entry for chain ID %d (chain: %s)", id, chain)
		assert.Equal(t, chain, reverse, "reverse mapping mismatch for chain ID %d", id)
	}

	// Every entry in IDToZerionChain must have a reverse mapping in ZerionChainToID
	for id, chain := range zerion.IDToZerionChain {
		reverse, ok := zerion.ZerionChainToID[chain]
		assert.True(t, ok, "ZerionChainToID missing entry for chain %s (ID: %d)", chain, id)
		assert.Equal(t, id, reverse, "reverse mapping mismatch for chain %s", chain)
	}

	// Maps must have the same length
	assert.Equal(t, len(zerion.ZerionChainToID), len(zerion.IDToZerionChain))
}

func TestChainMaps_ExpectedChains(t *testing.T) {
	expected := map[string]int64{
		"ethereum":  1,
		"polygon":   137,
		"arbitrum":  42161,
		"optimism":  10,
		"base":      8453,
		"avalanche": 43114,
		"bsc":       56,
	}

	for chain, id := range expected {
		actualID, ok := zerion.ZerionChainToID[chain]
		assert.True(t, ok, "missing chain %s", chain)
		assert.Equal(t, id, actualID)

		actualChain, ok := zerion.IDToZerionChain[id]
		assert.True(t, ok, "missing chain ID %d", id)
		assert.Equal(t, chain, actualChain)
	}
}

func TestErrUnsupportedChain(t *testing.T) {
	assert.NotNil(t, zerion.ErrUnsupportedChain)
	assert.Contains(t, zerion.ErrUnsupportedChain.Error(), "unsupported chain")
}
