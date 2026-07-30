package portfolio

import (
	"context"
	"math/big"
	"testing"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/pkg/money"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// chainDecimalsSource answers with the decimals of a specific (symbol, chain)
// contract, which is what the real per-chain asset store does. Keyed on both,
// so a lookup that forgets the chain misses rather than silently borrowing
// another chain's scale.
type chainDecimalsSource struct {
	byPair map[string]int
}

func (s *chainDecimalsSource) GetDecimalsBySymbol(_ context.Context, symbol, chainID string) (int, bool) {
	d, ok := s.byPair[symbol+":"+chainID]
	return d, ok
}

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

	svc := NewPortfolioService(ledgerRepo, walletRepo, priceService, nil, nil)
	svc.resolver = money.NewDecimalResolver(&chainDecimalsSource{
		byPair: map[string]int{
			"USDC:base": 6,
			"USDC:bsc":  18,
		},
	})

	userID := uuid.New()
	walletID := uuid.New()
	walletRepo.SetMockWallets(userID, []*Wallet{{ID: walletID, UserID: userID, Name: "W"}})

	baseChain, bscChain := "base", "bsc"
	baseAcct, bscAcct := uuid.New(), uuid.New()
	ledgerRepo.SetMockAccounts(walletID, []*ledger.Account{
		{ID: baseAcct, WalletID: &walletID, AssetID: "USDC", ChainID: &baseChain},
		{ID: bscAcct, WalletID: &walletID, AssetID: "USDC", ChainID: &bscChain},
	})

	// 1000 USDC on each chain, each in its own chain's base units.
	baseAmount := big.NewInt(1_000_000_000) // 1000 * 10^6
	bscAmount, _ := new(big.Int).SetString("1000000000000000000000", 10)

	ledgerRepo.SetMockBalances(baseAcct, []*ledger.AccountBalance{
		{AssetID: "USDC", Balance: baseAmount},
	})
	ledgerRepo.SetMockBalances(bscAcct, []*ledger.AccountBalance{
		{AssetID: "USDC", Balance: bscAmount},
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
		assert.Equal(t, "USDC", h.AssetID)
		want := big.NewInt(1000 * 100000000)
		assert.Equal(t, 0, h.USDValue.Cmp(want),
			"holding on chain %q should be $1000, got %s", h.ChainID, h.USDValue)
	}
}
