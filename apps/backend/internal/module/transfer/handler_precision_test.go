package transfer_test

import (
	"context"
	"math/big"
	"testing"
	"time"

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
// Issue #86 — the arriving leg is denominated in the arriving asset's units.
//
// #84 made the two legs of a bridge two assets. Precision is a property of an
// asset, so it had to move with the identity and did not: the model carried one
// Decimals, and the arriving leg was credited a quantity counted in the
// DEPARTING asset's base units while being booked as the destination asset.
//
// The failure is silent by construction. Both halves stay internally consistent
// — the number balances, the account exists, the asset is right — and only the
// SCALE is wrong, by 10^Δ. Nothing that compares magnitudes can see it.
// =============================================================================

// pcData is a bridge of 24.446762 USDC, stated at `srcDecimals` on the source
// chain and arriving as `dstAsset` at `dstDecimals`.
func pcData(srcID, dstID, dstAsset uuid.UUID, srcDecimals, dstDecimals int) map[string]interface{} {
	amount := pcRescale(big.NewInt(24_446_762), 6, srcDecimals)

	data := map[string]interface{}{
		"source_wallet_id":      srcID.String(),
		"dest_wallet_id":        dstID.String(),
		"asset_id":              testasset.ETH.String(),
		"asset_symbol":          "USDC",
		"decimals":              srcDecimals,
		"amount":                money.NewBigInt(amount).String(),
		"usd_rate":              money.NewBigIntFromInt64(100_000_000).String(), // $1.00
		"chain_id":              xcSourceChain,
		"source_chain_id":       xcSourceChain,
		"dest_chain_id":         xcDestChain,
		"dest_asset_id":         dstAsset.String(),
		"dest_asset_symbol":     "USDC",
		"dest_decimals":         dstDecimals,
		"dest_contract_address": "0xdestcontract",
		"contract_address":      "0xsrccontract",
		"tx_hash":               "0xbridge",
		"block_number":          int64(555),
		"occurred_at":           time.Now().Add(-time.Hour).Format(time.RFC3339),
		"unique_id":             "base:0xbridge",
	}
	return data
}

func pcRescale(amount *big.Int, from, to int) *big.Int {
	switch {
	case to > from:
		return new(big.Int).Mul(amount, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(to-from)), nil))
	case to < from:
		return new(big.Int).Div(amount, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(from-to)), nil))
	default:
		return new(big.Int).Set(amount)
	}
}

func pcEntries(t *testing.T, data map[string]interface{}, userID, srcID, dstID uuid.UUID) []*ledger.Entry {
	t.Helper()
	handler := transfer.NewInternalTransferHandler(xcWalletRepo(userID, srcID, dstID), logger.NewDefault("test"))
	entries, err := handler.Handle(context.Background(), data)
	require.NoError(t, err)
	return entries
}

func pcPrincipal(t *testing.T, entries []*ledger.Entry) (debit, credit *ledger.Entry) {
	t.Helper()
	for _, e := range entries {
		switch e.EntryType {
		case ledger.EntryTypeAssetIncrease:
			debit = e
		case ledger.EntryTypeAssetDecrease:
			credit = e
		}
	}
	require.NotNil(t, debit)
	require.NotNil(t, credit)
	return debit, credit
}

// -----------------------------------------------------------------------------
// The quantity
// -----------------------------------------------------------------------------

// TestInternalTransfer_Rescale_ArrivingQuantityIsRestated covers both directions
// of the ticket's example plus the equal case that must not change.
func TestInternalTransfer_Rescale_ArrivingQuantityIsRestated(t *testing.T) {
	cases := []struct {
		name                   string
		srcDecimals, dstDeci   int
		wantSource, wantArrive *big.Int
	}{
		{
			name:        "6 to 18 — the ticket's worked example",
			srcDecimals: 6, dstDeci: 18,
			wantSource: big.NewInt(24_446_762),
			wantArrive: pcRescale(big.NewInt(24_446_762), 6, 18),
		},
		{
			name:        "18 to 6 — the same bridge run backwards",
			srcDecimals: 18, dstDeci: 6,
			wantSource: pcRescale(big.NewInt(24_446_762), 6, 18),
			wantArrive: big.NewInt(24_446_762),
		},
		{
			name:        "6 to 6 — every bridge in production today",
			srcDecimals: 6, dstDeci: 6,
			wantSource: big.NewInt(24_446_762),
			wantArrive: big.NewInt(24_446_762),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			userID, srcID, dstID := uuid.New(), uuid.New(), uuid.New()
			dstAsset := uuid.New()

			entries := pcEntries(t, pcData(srcID, dstID, dstAsset, tc.srcDecimals, tc.dstDeci), userID, srcID, dstID)
			debit, credit := pcPrincipal(t, entries)

			assert.Equal(t, tc.wantSource, credit.Amount,
				"the departing leg keeps the departing asset's units")
			assert.Equal(t, tc.wantArrive, debit.Amount,
				"the arriving leg states the SAME QUANTITY in the arriving asset's units. Carrying "+
					"the integer across instead is wrong by 10^Δ (#86)")

			// The invariant behind both: the two legs denote one quantity.
			assert.Equal(t,
				new(big.Rat).SetFrac(credit.Amount, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(tc.srcDecimals)), nil)),
				new(big.Rat).SetFrac(debit.Amount, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(tc.dstDeci)), nil)),
				"both legs must denote the same number of whole tokens")
		})
	}
}

// -----------------------------------------------------------------------------
// The USD value
// -----------------------------------------------------------------------------

// TestInternalTransfer_Rescale_USDValueIsPerLeg: usdValue was computed ONCE,
// from the departing leg's precision, and stamped on both entries. The rate is
// per whole token and so is shared; the value is derived by dividing by the
// asset's own decimals and so is not.
//
// A bridge is economically neutral, so the two legs must carry the SAME USD
// value — which, with two scales in play, is only true if each is computed from
// its own.
func TestInternalTransfer_Rescale_USDValueIsPerLeg(t *testing.T) {
	userID, srcID, dstID := uuid.New(), uuid.New(), uuid.New()
	dstAsset := uuid.New()

	entries := pcEntries(t, pcData(srcID, dstID, dstAsset, 6, 18), userID, srcID, dstID)
	debit, credit := pcPrincipal(t, entries)

	// 24.446762 USDC at $1.00, scaled 1e8.
	want := big.NewInt(2_444_676_200)

	require.NotNil(t, credit.USDValue)
	require.NotNil(t, debit.USDValue)

	assert.Equal(t, want, credit.USDValue, "the departing leg is worth $24.446762")
	assert.Equal(t, want, debit.USDValue,
		"the arriving leg is worth the same $24.446762 — a bridge moves value, it does not create "+
			"or destroy it. Deriving this from the DEPARTING leg's precision while the amount is "+
			"stated at the arriving one is the second form of #86")

	assert.Equal(t, credit.USDRate, debit.USDRate,
		"the rate is USD per WHOLE token, so a change of scale does not touch it")
}

// -----------------------------------------------------------------------------
// The balance
// -----------------------------------------------------------------------------

// TestInternalTransfer_Rescale_StillBalances is the constraint that decides the
// shape of the whole fix.
//
// The ledger's balance check sums RAW base-unit amounts across every entry,
// blind to asset and decimals (Transaction.VerifyBalance). Booking the two
// wallet legs directly against each other therefore forces ONE integer to serve
// both — which is exactly why the model carried a single Decimals, and why
// simply restating the arriving amount would make debit and credit differ by
// 10^Δ and the transaction be rejected outright.
//
// So at unequal scales each leg balances against a transit account in its own
// asset, the way a swap already does.
func TestInternalTransfer_Rescale_StillBalances(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		srcDecimals, dstDeci int
		wantEntries          int
	}{
		{"unequal scales get a clearing pair", 6, 18, 4},
		{"equal scales keep the two-entry shape", 6, 6, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			userID, srcID, dstID := uuid.New(), uuid.New(), uuid.New()
			entries := pcEntries(t, pcData(srcID, dstID, uuid.New(), tc.srcDecimals, tc.dstDeci), userID, srcID, dstID)

			require.Len(t, entries, tc.wantEntries)

			tx := &ledger.Transaction{Entries: entries}
			assert.NoError(t, tx.VerifyBalance(),
				"a transaction the ledger rejects records nothing at all — the bridge would not be "+
					"mis-booked, it would be lost")
		})
	}
}

// TestInternalTransfer_Rescale_ClearingNets: the transit account is a device for
// balancing, not a place value accumulates. Across the two clearing entries the
// USD value nets to zero, so no economic claim is invented.
func TestInternalTransfer_Rescale_ClearingNets(t *testing.T) {
	userID, srcID, dstID := uuid.New(), uuid.New(), uuid.New()
	entries := pcEntries(t, pcData(srcID, dstID, uuid.New(), 6, 18), userID, srcID, dstID)

	net := new(big.Int)
	var clearing int
	for _, e := range entries {
		if e.EntryType != ledger.EntryTypeClearing {
			continue
		}
		clearing++
		if e.IsDebit() {
			net.Add(net, e.USDValue)
		} else {
			net.Sub(net, e.USDValue)
		}
	}

	require.Equal(t, 2, clearing, "one transit entry per leg")
	assert.Zero(t, net.Sign(), "the transit account holds no value once the bridge is complete")
}

// TestInternalTransfer_Rescale_ClearingIsNotAWalletLeg: the clearing entries must
// not look like wallet movements, or the TaxLotHook — which considers
// CRYPTO_WALLET and COLLATERAL accounts — would treat them as a disposal and an
// acquisition and open lots against a transit account.
func TestInternalTransfer_Rescale_ClearingIsNotAWalletLeg(t *testing.T) {
	userID, srcID, dstID := uuid.New(), uuid.New(), uuid.New()
	entries := pcEntries(t, pcData(srcID, dstID, uuid.New(), 6, 18), userID, srcID, dstID)

	for _, e := range entries {
		if e.EntryType != ledger.EntryTypeClearing {
			continue
		}
		assert.Equal(t, "CLEARING", e.Metadata["account_type"])
		assert.NotContains(t, e.Metadata, "wallet_id",
			"a transit entry is not a movement of anybody's wallet balance")
		assert.NotContains(t, e.Metadata, ledger.MetaLegPair,
			"only the two WALLET legs are the pair the cost basis travels along; marking a transit "+
				"entry would offer the hook a disposal that never left a wallet")
	}
}

// -----------------------------------------------------------------------------
// The leg pair still carries the basis
// -----------------------------------------------------------------------------

// TestInternalTransfer_Rescale_LegPairSurvives: #84's marker is what carries the
// cost basis across the bridge now that the two legs are different assets.
// Introducing the transit entries must not disturb it.
func TestInternalTransfer_Rescale_LegPairSurvives(t *testing.T) {
	userID, srcID, dstID := uuid.New(), uuid.New(), uuid.New()
	entries := pcEntries(t, pcData(srcID, dstID, uuid.New(), 6, 18), userID, srcID, dstID)

	debit, credit := pcPrincipal(t, entries)

	pair, ok := debit.Metadata[ledger.MetaLegPair].(string)
	require.True(t, ok)
	require.NotEmpty(t, pair)
	assert.Equal(t, pair, credit.Metadata[ledger.MetaLegPair],
		"the two wallet legs remain one movement; without the marker the destination lot opens at "+
			"FMV and the basis is silently reset (#84)")
}

// -----------------------------------------------------------------------------
// The contract address
// -----------------------------------------------------------------------------

// TestInternalTransfer_ArrivingLegNamesItsOwnContract: a bridged token has a
// different contract on every chain, so the source chain's address does not
// hold this asset on the destination chain. Stamping it there is a false
// statement in the audit trail, not a missing one.
func TestInternalTransfer_ArrivingLegNamesItsOwnContract(t *testing.T) {
	userID, srcID, dstID := uuid.New(), uuid.New(), uuid.New()
	entries := pcEntries(t, pcData(srcID, dstID, uuid.New(), 6, 6), userID, srcID, dstID)

	debit, credit := pcPrincipal(t, entries)

	assert.Equal(t, "0xdestcontract", debit.Metadata["contract_address"],
		"the arriving leg names the contract that holds this asset on the destination chain")
	assert.Equal(t, "0xsrccontract", credit.Metadata["contract_address"],
		"the departing leg keeps the source chain's contract")
}

// TestInternalTransfer_ArrivingLegOmitsUnknownContract: when the destination
// contract is not known — the native coin has none at all — the field is absent
// rather than borrowed. An absent field says "none here"; a borrowed one lies.
func TestInternalTransfer_ArrivingLegOmitsUnknownContract(t *testing.T) {
	userID, srcID, dstID := uuid.New(), uuid.New(), uuid.New()

	data := pcData(srcID, dstID, uuid.New(), 18, 18)
	delete(data, "dest_contract_address")

	entries := pcEntries(t, data, userID, srcID, dstID)
	debit, credit := pcPrincipal(t, entries)

	assert.NotContains(t, debit.Metadata, "contract_address",
		"with no contract known for the destination chain the field is omitted; filling it with the "+
			"SOURCE chain's address states something false about where the asset lives (#86)")
	assert.Equal(t, "0xsrccontract", credit.Metadata["contract_address"],
		"the departing leg is unaffected — its contract is genuinely its own")
}

// TestInternalTransfer_SameChain_KeepsItsContract: on one chain there is one
// contract and it belongs to both legs. The rule must not strip a field that is
// correct.
func TestInternalTransfer_SameChain_KeepsItsContract(t *testing.T) {
	userID, srcID, dstID := uuid.New(), uuid.New(), uuid.New()

	data := pcData(srcID, dstID, uuid.New(), 6, 6)
	delete(data, "dest_chain_id")
	delete(data, "dest_asset_id")
	delete(data, "dest_decimals")
	delete(data, "dest_contract_address")

	entries := pcEntries(t, data, userID, srcID, dstID)
	require.Len(t, entries, 2)

	debit, credit := pcPrincipal(t, entries)
	assert.Equal(t, "0xsrccontract", debit.Metadata["contract_address"])
	assert.Equal(t, "0xsrccontract", credit.Metadata["contract_address"])
}
