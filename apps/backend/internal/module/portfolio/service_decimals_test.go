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

// usdcBaseNative and usdcBaseBridged are two contracts sharing one ticker on
// ONE chain — the duplicate the ambiguity flag exists for.
//
// "USDC on Base vs USDC on BNB" (above) is told apart by the chain the UI
// already shows. This pair cannot be: same ticker, same chain, two contracts.
// It is the real post-#58 case — a rebrand/migration such as USDC ↔ USDC.e, or
// a manual knownness override — since spam homoglyphs never reach the registry.
var (
	usdcBaseNative  = uuid.MustParse("c0000000-0000-4000-8000-0000000000c1")
	usdcBaseBridged = uuid.MustParse("c0000000-0000-4000-8000-0000000000c2")
)

// TestPortfolioSummary_SameSymbolSameChain_StaysTwoHoldingsAndIsFlagged pins the
// two acceptance criteria that together make a duplicate ticker survivable.
//
// Grouping is keyed on the registry id, so the two rows cannot collapse into one
// — the property that makes a duplicate visible without any extra mechanism.
// Visibility alone is not enough, though: two identically-labelled "USDC" rows
// carrying different amounts read as an application bug rather than as a fact
// about the wallet. SymbolAmbiguous plus the contract are what let the client
// qualify exactly those rows, and the flag is a property of the asset, so it
// does not depend on both rows happening to appear in the same response.
func TestPortfolioSummary_SameSymbolSameChain_StaysTwoHoldingsAndIsFlagged(t *testing.T) {
	ctx := context.Background()

	ledgerRepo := setupMockLedgerRepository()
	walletRepo := setupMockWalletRepository()
	priceService := setupMockPriceService()

	const nativeContract = "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"
	const bridgedContract = "0xd9aaec86b65d86f6a7b5b1b0c42ffa531710b6ca"

	svc := NewPortfolioService(ledgerRepo, walletRepo, priceService, nil, nil).
		WithAssetLookup(newStubAssetLookup().
			addAmbiguous(usdcBaseNative, "USDC", 6, nativeContract).
			addAmbiguous(usdcBaseBridged, "USDC", 6, bridgedContract))

	userID := uuid.New()
	walletID := uuid.New()
	walletRepo.SetMockWallets(userID, []*Wallet{{ID: walletID, UserID: userID, Name: "W"}})

	base := "base"
	nativeAcct, bridgedAcct := uuid.New(), uuid.New()
	ledgerRepo.SetMockAccounts(walletID, []*ledger.Account{
		{ID: nativeAcct, WalletID: &walletID, AssetID: usdcBaseNative, ChainID: &base},
		{ID: bridgedAcct, WalletID: &walletID, AssetID: usdcBaseBridged, ChainID: &base},
	})

	// Deliberately different amounts: identical rows would be indistinguishable
	// even to the test, which is the user-facing complaint being pinned.
	ledgerRepo.SetMockBalances(nativeAcct, []*ledger.AccountBalance{
		{AssetID: usdcBaseNative, Balance: big.NewInt(1_000_000_000)}, // 1000
	})
	ledgerRepo.SetMockBalances(bridgedAcct, []*ledger.AccountBalance{
		{AssetID: usdcBaseBridged, Balance: big.NewInt(250_000_000)}, // 250
	})

	priceService.SetMockPrice("USDC", big.NewInt(100000000)) // $1 * 10^8

	summary, err := svc.GetPortfolioSummary(ctx, userID)
	require.NoError(t, err)

	require.Len(t, summary.AssetHoldings, 2,
		"two contracts sharing a ticker on one chain must stay two holdings; "+
			"collapsing them would hide one balance behind the other's label")

	byID := map[uuid.UUID]AssetHolding{}
	for _, h := range summary.AssetHoldings {
		byID[h.AssetID] = h
	}

	for id, wantContract := range map[uuid.UUID]string{
		usdcBaseNative:  nativeContract,
		usdcBaseBridged: bridgedContract,
	} {
		h, ok := byID[id]
		require.True(t, ok, "holding for %s must be present under its own registry id", id)
		assert.Equal(t, "USDC", h.AssetSymbol, "the ticker still ships, as data")
		assert.True(t, h.SymbolAmbiguous,
			"the ticker does not name this asset uniquely on its chain, so the "+
				"client must be told to qualify it")
		assert.Equal(t, wantContract, h.AssetContract,
			"the contract is what the client qualifies the ticker with; without "+
				"it the flag says 'ambiguous' and offers nothing to disambiguate by")
	}

	// $1000 + $250, scaled by 10^8 — neither row swallowed the other.
	assert.Equal(t, 0, summary.TotalUSDValue.Cmp(big.NewInt(1250*100000000)),
		"total must count both contracts, got %s", summary.TotalUSDValue)
}

// TestPortfolioSummary_UnambiguousTickerIsNotFlagged is the negative half: the
// flag must be selective, or the UI qualifies every ticker with a contract and
// the disambiguation carries no information.
func TestPortfolioSummary_UnambiguousTickerIsNotFlagged(t *testing.T) {
	ctx := context.Background()

	ledgerRepo := setupMockLedgerRepository()
	walletRepo := setupMockWalletRepository()
	priceService := setupMockPriceService()

	svc := NewPortfolioService(ledgerRepo, walletRepo, priceService, nil, nil).
		WithAssetLookup(newStubAssetLookup().add(usdcBase, "USDC", 6))

	userID := uuid.New()
	walletID := uuid.New()
	walletRepo.SetMockWallets(userID, []*Wallet{{ID: walletID, UserID: userID, Name: "W"}})

	base := "base"
	acct := uuid.New()
	ledgerRepo.SetMockAccounts(walletID, []*ledger.Account{
		{ID: acct, WalletID: &walletID, AssetID: usdcBase, ChainID: &base},
	})
	ledgerRepo.SetMockBalances(acct, []*ledger.AccountBalance{
		{AssetID: usdcBase, Balance: big.NewInt(1_000_000_000)},
	})
	priceService.SetMockPrice("USDC", big.NewInt(100000000))

	summary, err := svc.GetPortfolioSummary(ctx, userID)
	require.NoError(t, err)

	require.Len(t, summary.AssetHoldings, 1)
	assert.False(t, summary.AssetHoldings[0].SymbolAmbiguous,
		"a ticker that names exactly one asset on its chain must not be flagged")
}
