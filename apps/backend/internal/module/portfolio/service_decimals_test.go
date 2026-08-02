package portfolio

import (
	"context"
	"math/big"
	"testing"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// usdcBase and usdcBSC are the two registry rows behind one ticker.
//
// This is the shape #59 gives the hazard below: USDC on Base and USDC on BNB
// Chain are two contracts, so they are two registry ids, and nothing downstream
// can merge them by accident. Before the ticket they were the same string, and
// keeping them apart depended on every consumer remembering to carry the chain.
var (
	usdcBase = uuid.MustParse("c0000000-0000-4000-8000-0000000000b1")
	usdcBSC  = uuid.MustParse("c0000000-0000-4000-8000-0000000000b2")
)

// TestPortfolioSummary_SameSymbolDifferentDecimalsPerChain pins the rule that a
// holding is per (asset, chain), not per ticker.
//
// USDC is a real instance of the hazard: 6 decimals on Ethereum and Base, 18 on
// BNB Chain. Balances are raw base-unit integers, so 1000 USDC is 1e9 on one
// chain and 1e21 on the other. Summing those under one symbol and valuing the
// total with a single decimals figure overstates the holding by 10^12 — which
// is exactly what a real wallet holding both produced: a $25 trillion line.
func TestPortfolioSummary_SameSymbolDifferentDecimalsPerChain(t *testing.T) {
	ctx := context.Background()

	ledgerRepo := setupMockLedgerRepository()
	walletRepo := setupMockWalletRepository()
	priceService := setupMockPriceService()

	// Decimals come from the registry row each id names, so the 6-vs-18 split
	// is a property of the two rows rather than of a (symbol, chain) key the
	// caller has to remember to pass.
	svc := NewPortfolioService(ledgerRepo, walletRepo, priceService, nil, nil).
		WithAssetLookup(newStubAssetLookup().
			add(usdcBase, "USDC", 6).
			add(usdcBSC, "USDC", 18))

	userID := uuid.New()
	walletID := uuid.New()
	walletRepo.SetMockWallets(userID, []*Wallet{{ID: walletID, UserID: userID, Name: "W"}})

	baseChain, bscChain := "base", "bsc"
	baseAcct, bscAcct := uuid.New(), uuid.New()
	ledgerRepo.SetMockAccounts(walletID, []*ledger.Account{
		{ID: baseAcct, WalletID: &walletID, AssetID: usdcBase, ChainID: &baseChain},
		{ID: bscAcct, WalletID: &walletID, AssetID: usdcBSC, ChainID: &bscChain},
	})

	// 1000 USDC on each chain, each in its own chain's base units.
	baseAmount := big.NewInt(1_000_000_000) // 1000 * 10^6
	bscAmount, _ := new(big.Int).SetString("1000000000000000000000", 10)

	ledgerRepo.SetMockBalances(baseAcct, []*ledger.AccountBalance{
		{AssetID: usdcBase, Balance: baseAmount},
	})
	ledgerRepo.SetMockBalances(bscAcct, []*ledger.AccountBalance{
		{AssetID: usdcBSC, Balance: bscAmount},
	})

	priceService.SetMockPrice("USDC", big.NewInt(100000000)) // $1 * 10^8

	summary, err := svc.GetPortfolioSummary(ctx, userID)
	require.NoError(t, err)

	// 1000 + 1000 USDC at $1 = $2000, scaled by 10^8.
	expected := big.NewInt(2000 * 100000000)
	assert.Equal(t, 0, summary.TotalUSDValue.Cmp(expected),
		"two 1000-USDC holdings on chains with different decimals must total $2000, got %s "+
			"(a mismatch here means base units were summed across incompatible scales)",
		summary.TotalUSDValue)

	// Each chain is its own holding, valued with its own decimals.
	require.Len(t, summary.AssetHoldings, 2, "one holding per (asset, chain)")
	for _, h := range summary.AssetHoldings {
		assert.Equal(t, "USDC", h.AssetSymbol, "both rows render as USDC")
		assert.Contains(t, []uuid.UUID{usdcBase, usdcBSC}, h.AssetID,
			"each holding must name one of the two registry rows, never a merge of them")
		want := big.NewInt(1000 * 100000000)
		assert.Equal(t, 0, h.USDValue.Cmp(want),
			"holding on chain %q should be $1000, got %s", h.ChainID, h.USDValue)
	}
}
