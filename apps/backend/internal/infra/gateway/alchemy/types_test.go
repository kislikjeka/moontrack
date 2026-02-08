package alchemy_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kislikjeka/moontrack/internal/infra/gateway/alchemy"
)

func TestTransferCategoriesForChain_L1_IncludesInternal(t *testing.T) {
	// Ethereum (1) and Polygon (137) support "internal" category
	l1Chains := []int64{1, 137}

	for _, chainID := range l1Chains {
		categories := alchemy.TransferCategoriesForChain(chainID)

		assert.Contains(t, categories, alchemy.CategoryExternal, "chain %d should include external", chainID)
		assert.Contains(t, categories, alchemy.CategoryInternal, "chain %d should include internal", chainID)
		assert.Contains(t, categories, alchemy.CategoryERC20, "chain %d should include erc20", chainID)
	}
}

func TestTransferCategoriesForChain_L2_ExcludesInternal(t *testing.T) {
	// L2 chains do NOT support "internal" category
	l2Chains := []int64{42161, 10, 8453, 43114, 56}

	for _, chainID := range l2Chains {
		categories := alchemy.TransferCategoriesForChain(chainID)

		assert.Contains(t, categories, alchemy.CategoryExternal, "chain %d should include external", chainID)
		assert.NotContains(t, categories, alchemy.CategoryInternal, "chain %d should NOT include internal", chainID)
		assert.Contains(t, categories, alchemy.CategoryERC20, "chain %d should include erc20", chainID)
	}
}
