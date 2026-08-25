package transfer_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/module/transfer"
	"github.com/kislikjeka/moontrack/pkg/logger"
	"github.com/kislikjeka/moontrack/pkg/money"
	"github.com/kislikjeka/moontrack/pkg/testasset"
)

// =============================================================================
// Issue #84 — two identities across a bridge, and an explicit leg pair.
//
// Two defects that cannot be fixed apart:
//
//  1. Addressing. Identity is (chain, contract), so the two legs of a bridge
//     hold DIFFERENT assets by definition. The model carried one flat UUID and
//     two chains, and the handler spliced the destination chain onto the source
//     asset's id — a code that is well-formed and addresses an account that
//     should not exist. On the live database both bridge transactions were
//     defective, with the accounts crossed over; the split balances summed to a
//     plausible number, so only a cardinality check ever saw it (#70).
//
//  2. Pairing. The tax-lot hook matched disposals to acquisitions by asset
//     equality, which held only BECAUSE of defect 1. Fixing the addressing gives
//     the legs their two rightful identities and, with them, no shared key — the
//     carry-over would break silently, the destination lot would open at market
//     price, and the balance would still tie out.
//
// The pair is therefore stamped, not inferred: ledger.MetaLegPair, on both
// principal legs and on nothing else.
// =============================================================================

// destAsset is ETH as it exists on the destination chain: a different registry
// row, because a token bridged to another chain is another contract.
var destAsset = testasset.ETHOnArbitrum

// bridgeData is a cross-chain internal transfer that names BOTH identities.
func bridgeData(srcID, dstID uuid.UUID) map[string]interface{} {
	data := xcData(srcID, dstID, xcSourceChain, xcDestChain)
	data["dest_asset_id"] = destAsset.String()
	return data
}

// -----------------------------------------------------------------------------
// The chain segment of a code must name the chain of the asset in that code
// -----------------------------------------------------------------------------

// TestInternalTransfer_Bridge_CodeChainMatchesAssetChain is the direct assertion
// of #70's acceptance: on each side, the chain in the account code and the chain
// the asset belongs to are the same chain. The defect produced base-USDC filed
// under an arbitrum code and arbitrum-USDC under a base code.
func TestInternalTransfer_Bridge_CodeChainMatchesAssetChain(t *testing.T) {
	ctx := context.Background()
	userID, srcID, dstID := uuid.New(), uuid.New(), uuid.New()

	handler := transfer.NewInternalTransferHandler(xcWalletRepo(userID, srcID, dstID), logger.NewDefault("test"))

	entries, err := handler.Handle(ctx, bridgeData(srcID, dstID))
	require.NoError(t, err)

	debit, credit := legs(t, entries)

	// The arriving leg is booked against the ARRIVING asset, not the departing
	// one. This is the field the sync layer used to compute and throw away.
	assert.Equal(t, destAsset, debit.AssetID,
		"the destination leg must carry the destination chain's asset")
	assert.Equal(t, testasset.ETH, credit.AssetID,
		"the source leg must keep the source chain's asset")
	require.NotEqual(t, debit.AssetID, credit.AssetID,
		"a bridge moves value between two assets; equal ids here would be the #70 shape")

	assert.Equal(t, fmt.Sprintf("wallet.%s.%s.%s", dstID, xcDestChain, destAsset), debit.Metadata["account_code"],
		"destination code: destination chain paired with the DESTINATION asset")
	assert.Equal(t, fmt.Sprintf("wallet.%s.%s.%s", srcID, xcSourceChain, testasset.ETH), credit.Metadata["account_code"],
		"source code: source chain paired with the SOURCE asset")

	// Stated once more as the invariant rather than as two literals, because
	// this is the check the reconciliation report runs on the live database:
	// for every entry, the chain segment of its code and the chain of its asset
	// agree.
	for _, e := range []*ledger.Entry{debit, credit} {
		code := e.Metadata["account_code"].(string)
		chain := e.Metadata["chain_id"].(string)
		assert.True(t, strings.HasSuffix(code, chain+"."+e.AssetID.String()),
			"code %q must end in {chain}.{asset} for its own chain %q and its own asset %s", code, chain, e.AssetID)
	}
}

// TestInternalTransfer_SameChain_OneIdentity: without a destination asset the
// transfer is same-chain and both legs are genuinely the same asset. Every raw
// written before the destination identity existed looks like this, and it must
// keep working byte for byte.
func TestInternalTransfer_SameChain_OneIdentity(t *testing.T) {
	ctx := context.Background()
	userID, srcID, dstID := uuid.New(), uuid.New(), uuid.New()

	handler := transfer.NewInternalTransferHandler(xcWalletRepo(userID, srcID, dstID), logger.NewDefault("test"))

	entries, err := handler.Handle(ctx, xcData(srcID, dstID, "", ""))
	require.NoError(t, err)

	debit, credit := legs(t, entries)

	assert.Equal(t, testasset.ETH, debit.AssetID)
	assert.Equal(t, testasset.ETH, credit.AssetID)
	assert.Equal(t, debit.Metadata[ledger.MetaLegPair], credit.Metadata[ledger.MetaLegPair],
		"a same-chain transfer is a leg pair too — the marker is not a bridge-only device")
}

// -----------------------------------------------------------------------------
// The pair marker: on both principal legs, on neither gas leg
// -----------------------------------------------------------------------------

// TestInternalTransfer_Bridge_LegsStampedAsOnePair: the hook has to find the two
// legs by data, because it runs AFTER the entries are written and can see
// nothing of the order the handler built them in.
func TestInternalTransfer_Bridge_LegsStampedAsOnePair(t *testing.T) {
	ctx := context.Background()
	userID, srcID, dstID := uuid.New(), uuid.New(), uuid.New()

	handler := transfer.NewInternalTransferHandler(xcWalletRepo(userID, srcID, dstID), logger.NewDefault("test"))

	entries, err := handler.Handle(ctx, bridgeData(srcID, dstID))
	require.NoError(t, err)

	debit, credit := legs(t, entries)

	pair, ok := debit.Metadata[ledger.MetaLegPair].(string)
	require.True(t, ok, "the arriving leg must carry a pair marker")
	require.NotEmpty(t, pair)
	assert.Equal(t, pair, credit.Metadata[ledger.MetaLegPair],
		"both legs of one movement share ONE marker — that identity is what carries the basis")
}

// TestInternalTransfer_GasInSameAssetIsNotPartOfThePair is the collision the
// marker exists for. Bridging the native coin books TWO asset decreases on the
// source wallet in the SAME asset: the principal and the gas. Nothing about the
// asset, the chain, or the account tells them apart — only the marker does.
func TestInternalTransfer_GasInSameAssetIsNotPartOfThePair(t *testing.T) {
	ctx := context.Background()
	userID, srcID, dstID := uuid.New(), uuid.New(), uuid.New()

	handler := transfer.NewInternalTransferHandler(xcWalletRepo(userID, srcID, dstID), logger.NewDefault("test"))

	// Native-coin bridge: the fee is paid in the very asset being moved.
	data := bridgeData(srcID, dstID)
	data["gas_amount"] = money.NewBigIntFromInt64(21000000000000).String()
	data["gas_usd_rate"] = money.NewBigIntFromInt64(200000000000).String()
	data["gas_decimals"] = 18
	data["native_asset_id"] = testasset.ETH.String()

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 4)

	// Two decreases, same asset, same wallet, same chain — exactly the shape
	// that no asset- or chain-based inference can resolve.
	var decreases []*ledger.Entry
	for _, e := range entries {
		if e.EntryType == ledger.EntryTypeAssetDecrease && e.AssetID == testasset.ETH {
			decreases = append(decreases, e)
		}
	}
	require.Len(t, decreases, 2,
		"a native-coin bridge decreases the source wallet's ETH twice: principal and gas")

	marked := 0
	for _, e := range decreases {
		if _, ok := e.Metadata[ledger.MetaLegPair]; ok {
			marked++
		}
	}
	assert.Equal(t, 1, marked,
		"exactly one of the two decreases belongs to the pair; a marked gas leg could hand its "+
			"consumed lot to the destination acquisition and move the basis onto the wrong lot")

	for _, e := range entries {
		if e.EntryType == ledger.EntryTypeGasFee {
			assert.NotContains(t, e.Metadata, ledger.MetaLegPair,
				"the gas expense leg is not half of any movement")
		}
	}
}
