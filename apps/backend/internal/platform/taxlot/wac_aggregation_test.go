package taxlot

import (
	"math/big"
	"testing"

	"github.com/google/uuid"
)

// usd scales a dollar figure to the 10^8 fixed point the ledger stores rates in.
func usd(dollars int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(dollars), big.NewInt(100_000_000))
}

// findAggregated returns the rolled-up row for an asset. Aggregated rows are the
// ones with no account: they span every chain the asset sits on.
func findAggregated(t *testing.T, positions []WACPosition, asset uuid.UUID) WACPosition {
	t.Helper()

	for _, p := range positions {
		if p.Asset == asset && p.AccountID == uuid.Nil {
			return p
		}
	}
	t.Fatalf("no aggregated position for asset %s", asset)
	return WACPosition{}
}

// TestAggregateWAC_AllPendingYieldsNilNotZero is the second half of #79, and the
// half that sequence alone would not have fixed. The per-chain rows correctly
// carry a nil WAC, but the rollup summed them into a fresh big.Int and divided,
// producing a hard zero that no amount of careful serialization can tell apart
// from a real one.
func TestAggregateWAC_AllPendingYieldsNilNotZero(t *testing.T) {
	walletID, asset := uuid.New(), uuid.New()

	aggregated := aggregateWACAcrossChains([]WACPosition{
		{WalletID: walletID, Asset: asset, AccountID: uuid.New(), ChainID: "ethereum",
			TotalQuantity: big.NewInt(1000), WeightedAvgCost: nil},
		{WalletID: walletID, Asset: asset, AccountID: uuid.New(), ChainID: "base",
			TotalQuantity: big.NewInt(2000), WeightedAvgCost: nil},
	})

	got := findAggregated(t, aggregated, asset)

	if got.WeightedAvgCost != nil {
		t.Errorf("WeightedAvgCost = %s, want nil — a position with no resolved price has an unknown cost, not a zero one",
			got.WeightedAvgCost)
	}
	// The quantity is known even when the cost is not; the rollup must not drop it.
	if got.TotalQuantity.Cmp(big.NewInt(3000)) != 0 {
		t.Errorf("TotalQuantity = %s, want 3000", got.TotalQuantity)
	}
}

// TestAggregateWAC_MixedUsesResolvedPortionOnly pins the divisor. The resolved
// chain holds 1000 units at $10; the pending chain contributes quantity but no
// cost. Dividing the $10,000 of known cost by the full 3000 units would report
// a WAC of $3.33 — a number derived from mixing known cost with unknown
// quantity, and lower than any price actually paid.
func TestAggregateWAC_MixedUsesResolvedPortionOnly(t *testing.T) {
	walletID, asset := uuid.New(), uuid.New()

	aggregated := aggregateWACAcrossChains([]WACPosition{
		{WalletID: walletID, Asset: asset, AccountID: uuid.New(), ChainID: "ethereum",
			TotalQuantity: big.NewInt(1000), WeightedAvgCost: usd(10)},
		{WalletID: walletID, Asset: asset, AccountID: uuid.New(), ChainID: "base",
			TotalQuantity: big.NewInt(2000), WeightedAvgCost: nil},
	})

	got := findAggregated(t, aggregated, asset)

	if got.WeightedAvgCost == nil {
		t.Fatal("WeightedAvgCost = nil, want $10 — one resolved chain is enough to know a cost")
	}
	if got.WeightedAvgCost.Cmp(usd(10)) != 0 {
		t.Errorf("WeightedAvgCost = %s, want %s ($10 — the resolved chain's cost, undiluted by the pending one)",
			got.WeightedAvgCost, usd(10))
	}
	if got.TotalQuantity.Cmp(big.NewInt(3000)) != 0 {
		t.Errorf("TotalQuantity = %s, want 3000", got.TotalQuantity)
	}
}

// TestAggregateWAC_AllResolvedWeightsByQuantity is the ordinary case: 1000 @ $10
// and 3000 @ $20 average to $17.50, weighted by quantity rather than by chain.
func TestAggregateWAC_AllResolvedWeightsByQuantity(t *testing.T) {
	walletID, asset := uuid.New(), uuid.New()

	aggregated := aggregateWACAcrossChains([]WACPosition{
		{WalletID: walletID, Asset: asset, AccountID: uuid.New(), ChainID: "ethereum",
			TotalQuantity: big.NewInt(1000), WeightedAvgCost: usd(10)},
		{WalletID: walletID, Asset: asset, AccountID: uuid.New(), ChainID: "base",
			TotalQuantity: big.NewInt(3000), WeightedAvgCost: usd(20)},
	})

	got := findAggregated(t, aggregated, asset)

	want := big.NewInt(1_750_000_000) // $17.50
	if got.WeightedAvgCost == nil || got.WeightedAvgCost.Cmp(want) != 0 {
		t.Errorf("WeightedAvgCost = %v, want %s ($17.50)", got.WeightedAvgCost, want)
	}
}

// TestAggregateWAC_KnownZeroStaysZero separates absence from a real zero once
// more: an asset acquired at no cost aggregates to zero, and that zero must not
// be turned into "unknown" by the nil-preserving change.
func TestAggregateWAC_KnownZeroStaysZero(t *testing.T) {
	walletID, asset := uuid.New(), uuid.New()

	aggregated := aggregateWACAcrossChains([]WACPosition{
		{WalletID: walletID, Asset: asset, AccountID: uuid.New(), ChainID: "ethereum",
			TotalQuantity: big.NewInt(1000), WeightedAvgCost: big.NewInt(0)},
	})

	got := findAggregated(t, aggregated, asset)

	if got.WeightedAvgCost == nil {
		t.Fatal("WeightedAvgCost = nil, want 0 — a known zero cost is not an unknown one")
	}
	if got.WeightedAvgCost.Sign() != 0 {
		t.Errorf("WeightedAvgCost = %s, want 0", got.WeightedAvgCost)
	}
}

// TestAggregateWAC_SeparatesAssetsAndWallets guards the grouping key while the
// accumulator is being changed: costs must not leak across assets or wallets.
func TestAggregateWAC_SeparatesAssetsAndWallets(t *testing.T) {
	walletA, walletB := uuid.New(), uuid.New()
	assetX, assetY := uuid.New(), uuid.New()

	aggregated := aggregateWACAcrossChains([]WACPosition{
		{WalletID: walletA, Asset: assetX, AccountID: uuid.New(), TotalQuantity: big.NewInt(100), WeightedAvgCost: usd(10)},
		{WalletID: walletA, Asset: assetY, AccountID: uuid.New(), TotalQuantity: big.NewInt(100), WeightedAvgCost: usd(20)},
		{WalletID: walletB, Asset: assetX, AccountID: uuid.New(), TotalQuantity: big.NewInt(100), WeightedAvgCost: nil},
	})

	if len(aggregated) != 3 {
		t.Fatalf("got %d aggregated rows, want 3 (one per wallet+asset)", len(aggregated))
	}

	for _, p := range aggregated {
		switch {
		case p.WalletID == walletA && p.Asset == assetX:
			if p.WeightedAvgCost.Cmp(usd(10)) != 0 {
				t.Errorf("walletA/assetX WAC = %s, want %s", p.WeightedAvgCost, usd(10))
			}
		case p.WalletID == walletA && p.Asset == assetY:
			if p.WeightedAvgCost.Cmp(usd(20)) != 0 {
				t.Errorf("walletA/assetY WAC = %s, want %s", p.WeightedAvgCost, usd(20))
			}
		case p.WalletID == walletB && p.Asset == assetX:
			if p.WeightedAvgCost != nil {
				t.Errorf("walletB/assetX WAC = %s, want nil — walletA's price must not leak across wallets", p.WeightedAvgCost)
			}
		}
	}
}
