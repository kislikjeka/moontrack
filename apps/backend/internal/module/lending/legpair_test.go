package lending

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/pkg/money"
	"github.com/kislikjeka/moontrack/pkg/testasset"
)

// =============================================================================
// Issue #84 — leg pairs on the operations that carry a cost basis.
//
// The tax-lot hook shares one carry-over branch between internal transfers and
// lending supply/withdraw: in all three the user's own principal moves between
// two of their own accounts and no gain is realized. That branch used to find
// the two legs by asset equality, which cannot tell the principal apart from the
// gas paid in the same coin — a shape the live database already contains.
//
// Borrow, repay and claim get no marker: nothing crosses there. Neither does gas.
// =============================================================================

// pairOf reads the leg-pair marker off an entry.
func pairOf(e *ledger.Entry) (string, bool) {
	v, ok := e.Metadata[ledger.MetaLegPair].(string)
	return v, ok
}

// TestSupplyAndWithdraw_StampOneLegPair: the two operations whose acquisition
// inherits a basis must name their pair, and both sides must name the same one.
func TestSupplyAndWithdraw_StampOneLegPair(t *testing.T) {
	for name, gen := range map[string]func(*LendingTransaction, int, *LendingTransferItem) []*ledger.Entry{
		"supply":   generateSupplyItemEntries,
		"withdraw": generateWithdrawItemEntries,
	} {
		t.Run(name, func(t *testing.T) {
			entries := gen(baseTxn(), 0, baseItem())
			require.Len(t, entries, 2)

			first, ok := pairOf(entries[0])
			require.True(t, ok, "%s moves the user's own principal: its legs must be paired", name)
			require.NotEmpty(t, first)

			second, ok := pairOf(entries[1])
			require.True(t, ok)
			assert.Equal(t, first, second,
				"both legs of one movement share ONE marker, or the basis stops carrying")
		})
	}
}

// TestBorrowRepayClaim_AreNotLegPairs: these move value between the user and the
// protocol, or bring in new value. No lot crosses, so marking them would offer
// the hook a pairing that does not exist.
func TestBorrowRepayClaim_AreNotLegPairs(t *testing.T) {
	for name, gen := range map[string]func(*LendingTransaction, int, *LendingTransferItem) []*ledger.Entry{
		"borrow": generateBorrowItemEntries,
		"repay":  generateRepayItemEntries,
		"claim":  generateClaimItemEntries,
	} {
		t.Run(name, func(t *testing.T) {
			for _, e := range gen(baseTxn(), 0, baseItem()) {
				assert.NotContains(t, e.Metadata, ledger.MetaLegPair,
					"%s carries no lot across: a marker here would fabricate a carry-over", name)
			}
		})
	}
}

// TestSupplyGasInTheSameAsset_IsNotPartOfThePair is the collision the marker
// exists for, in the shape the live database already holds: 8 collateral
// supplies where gas, principal and collateral all carry one UUID.
//
// Both the gas credit and the principal credit are asset decreases on the same
// wallet account in the same asset — nothing but the marker separates them.
func TestSupplyGasInTheSameAsset_IsNotPartOfThePair(t *testing.T) {
	txn := baseTxn()
	item := baseItem()

	// The fee is paid in the very coin being supplied.
	txn.FeeAsset = item.AssetID
	txn.FeeAmount = money.NewBigIntFromInt64(21_000_000_000_000)
	txn.FeeUSDPrice = money.NewBigIntFromInt64(200_000_000_000)
	txn.FeeDecimals = 18

	entries := append(generateSupplyItemEntries(txn, 0, item), generateGasFeeEntries(txn)...)

	var decreases []*ledger.Entry
	for _, e := range entries {
		if e.EntryType == ledger.EntryTypeAssetDecrease && e.AssetID == testasset.ETH {
			decreases = append(decreases, e)
		}
	}
	require.Len(t, decreases, 2,
		"a supply paid for in the coin being supplied decreases the wallet twice: principal and gas")

	marked := 0
	for _, e := range decreases {
		if _, ok := pairOf(e); ok {
			marked++
		}
	}
	assert.Equal(t, 1, marked,
		"exactly one of the two decreases belongs to the pair. Marked, the gas leg could hand its "+
			"consumed lot to the collateral acquisition; today that is only survivable because FIFO "+
			"happens to consume the same lot twice, which is ordering, not an invariant")

	for _, e := range generateGasFeeEntries(txn) {
		assert.NotContains(t, e.Metadata, ledger.MetaLegPair)
	}
}

// TestMultiItemSupply_EachItemIsItsOwnPair: a supply of two assets is two
// movements, not one pool. Sharing a marker would let one item's disposal
// become the other item's basis.
func TestMultiItemSupply_EachItemIsItsOwnPair(t *testing.T) {
	txn := baseTxn()

	eth := baseItem()
	usdc := baseItem()
	usdc.AssetID = testasset.USDC

	ethPair, ok := pairOf(generateSupplyItemEntries(txn, 0, eth)[0])
	require.True(t, ok)
	usdcPair, ok := pairOf(generateSupplyItemEntries(txn, 0, usdc)[0])
	require.True(t, ok)

	assert.NotEqual(t, ethPair, usdcPair,
		"two assets supplied in one transaction are two movements; one marker across both would "+
			"cross their bases")
}

// TestSameAssetItems_AreDistinctPairs closes the hole a marker keyed on the
// asset alone would leave — the very collision the marker exists to remove,
// reintroduced one level down.
//
// buildLendingData copies EVERY leg into the item list, including
// opposite-direction ones, for the reason its own comment gives: "a real
// operation can still move principal both ways (a supply that also returns
// dust, a repay that reclaims excess)". The handler does not branch on
// Direction, so both legs become full supply pairs. Two items, one asset.
//
// Keyed on the asset, their markers would be equal, the hook would pool both
// disposals under one group, and the first acquisition would link to whichever
// disposal happened to land first — not to its own counterpart. The transaction
// would balance and nothing would fail.
func TestSameAssetItems_AreDistinctPairs(t *testing.T) {
	txn := baseTxn()

	principal := baseItem()
	dust := baseItem() // same AssetID, as a same-asset refund leg is

	principalPair, ok := pairOf(generateSupplyItemEntries(txn, 0, principal)[0])
	require.True(t, ok)
	dustPair, ok := pairOf(generateSupplyItemEntries(txn, 1, dust)[0])
	require.True(t, ok)

	assert.NotEqual(t, principalPair, dustPair,
		"two items in ONE asset are two movements with two counterparts; sharing a marker pools "+
			"their disposals and lets an acquisition link to a lot it never consumed")
}
