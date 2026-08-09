package noves

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
)

// loadFixture reads a testdata JSON file into a Noves Transaction.
func loadFixture(t *testing.T, name string) Transaction {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err, "read fixture %s", name)
	var tx Transaction
	require.NoError(t, json.Unmarshal(data, &tx), "unmarshal fixture %s", name)
	return tx
}

// convert converts a fixture through the adapter with the given domain chain.
func convert(t *testing.T, name, domainChain string) sync.DecodedTransaction {
	t.Helper()
	dt, err := convertTransaction(loadFixture(t, name), domainChain)
	require.NoError(t, err)
	return dt
}

// bigStr is a shorthand to build a *big.Int from a decimal string in tests.
func bigStr(t *testing.T, s string) *big.Int {
	t.Helper()
	n, ok := new(big.Int).SetString(s, 10)
	require.True(t, ok, "bad big int %q", s)
	return n
}

// transferBySymbol finds the (first) transfer with the given asset symbol.
func transferBySymbol(transfers []sync.DecodedTransfer, symbol string) (sync.DecodedTransfer, bool) {
	for _, tr := range transfers {
		if tr.AssetSymbol == symbol {
			return tr, true
		}
	}
	return sync.DecodedTransfer{}, false
}

func TestInterfaceCompliance(t *testing.T) {
	var _ sync.TransactionDataProvider = (*SyncAdapter)(nil)
}

func TestConvert_Swap(t *testing.T) {
	dt := convert(t, "swap.json", "base")

	assert.Equal(t, sync.OpTrade, dt.OperationType)
	assert.Equal(t, "base", dt.ChainID)
	assert.Equal(t, "0xf7ae9d268cb73d4868b00b463d971879e9d37da577b359198d08d0b774564c9c", dt.TxHash)
	assert.Equal(t, "base:0xf7ae9d268cb73d4868b00b463d971879e9d37da577b359198d08d0b774564c9c", dt.ID)
	assert.Equal(t, "confirmed", dt.Status)
	assert.False(t, dt.NeedsReview)

	// paidGas must be filtered: 1 sent (USDC) + 1 received (cbBTC) = 2 transfers.
	require.Len(t, dt.Transfers, 2)

	usdc, ok := transferBySymbol(dt.Transfers, "USDC")
	require.True(t, ok)
	assert.Equal(t, sync.DirectionOut, usdc.Direction)
	assert.Equal(t, 6, usdc.Decimals)
	assert.Equal(t, "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913", usdc.ContractAddress)
	assert.Equal(t, bigStr(t, "120559701"), usdc.Amount) // 120.559701 * 1e6

	cbbtc, ok := transferBySymbol(dt.Transfers, "cbBTC")
	require.True(t, ok)
	assert.Equal(t, sync.DirectionIn, cbbtc.Direction)
	assert.Equal(t, bigStr(t, "199560"), cbbtc.Amount) // 0.0019956 * 1e8

	// Native fee: ETH, empty contract, converted to base units.
	require.NotNil(t, dt.Fee)
	assert.Equal(t, "ETH", dt.Fee.AssetSymbol)
	assert.Equal(t, 18, dt.Fee.Decimals)
	assert.Equal(t, bigStr(t, "1904071203646"), dt.Fee.Amount) // 0.000001904071203646 * 1e18
}

// TestConvert_LendingSupply is the receipt rule on a real Aave supply (#57).
//
// The provider sends two legs: the principal cbBTC leaving the wallet
// (`deposited`) and the aToken coming back (`collateralSharesMinted`). Only the
// principal becomes a transfer. Booking both recorded one supply twice — the
// measurement in #44 found exactly that in the ledger, the principal and the
// aToken sitting in `collateral.` for the same event, each pair internally
// balanced so double-entry could never catch it.
func TestConvert_LendingSupply(t *testing.T) {
	dt := convert(t, "lending_supply.json", "base")

	assert.Equal(t, sync.OpDeposit, dt.OperationType)

	// One transfer, not two: the principal. paidGas filtered, receipt dropped.
	require.Len(t, dt.Transfers, 1)
	principal := dt.Transfers[0]
	assert.Equal(t, "cbBTC", principal.AssetSymbol)
	assert.Equal(t, sync.DirectionOut, principal.Direction)

	// The receipt token is gone from the transfers entirely.
	_, ok := transferBySymbol(dt.Transfers, "aBascbBTC")
	assert.False(t, ok, "the aToken receipt must not survive as a transfer")

	// Its action does survive, on LegActions — that is what still identifies the
	// transaction as a lending supply once the leg itself is gone.
	assert.Contains(t, dt.LegActions, "collateralSharesMinted")
	assert.Contains(t, dt.LegActions, "deposited")

	// Classified by leg action, with no protocol name and no aToken ticker left.
	assert.Empty(t, dt.Protocol, "this fixture's protocol.name is null")
	assert.Equal(t, ledger.TxTypeLendingSupply, sync.NewClassifier().Classify(dt))
}

func TestConvert_TransferIn(t *testing.T) {
	dt := convert(t, "transfer_in.json", "base")

	assert.Equal(t, sync.OpReceive, dt.OperationType)
	assert.Equal(t, "Circle", dt.Protocol) // protocol.name present here
	require.Len(t, dt.Transfers, 1)
	assert.Equal(t, sync.DirectionIn, dt.Transfers[0].Direction)
	assert.Equal(t, "USDC", dt.Transfers[0].AssetSymbol)
	assert.Equal(t, bigStr(t, "1368154870"), dt.Transfers[0].Amount) // 1368.15487 * 1e6

	assert.Equal(t, ledger.TxTypeTransferIn, sync.NewClassifier().Classify(dt))
}

func TestConvert_BridgeReceive(t *testing.T) {
	dt := convert(t, "bridge_receive.json", "base")

	assert.Equal(t, sync.OpReceive, dt.OperationType)
	require.Len(t, dt.Transfers, 1)
	tr := dt.Transfers[0]
	assert.Equal(t, sync.DirectionIn, tr.Direction)
	assert.Equal(t, "ETH", tr.AssetSymbol)
	assert.Equal(t, sync.NativeContract, tr.ContractAddress)   // native → literal sentinel (#56)
	assert.Equal(t, bigStr(t, "69706000000000000"), tr.Amount) // 0.069706 * 1e18
}

func TestConvert_BridgeSendRoundtrip(t *testing.T) {
	// The bridge-as-swap case: LP token out + a different asset (ETH) back in.
	dt := convert(t, "bridge_send_roundtrip.json", "base")

	assert.Equal(t, sync.OpSend, dt.OperationType)

	// paidGas filtered; feesPaid (an ETH out leg) is NOT filtered.
	// sent legs kept: bridged GM token + feesPaid ETH = 2; received: ETH = 1.
	require.Len(t, dt.Transfers, 3)

	gm, ok := transferBySymbol(dt.Transfers, "GM: ETH/USD [WETH-USDC]")
	require.True(t, ok)
	assert.Equal(t, sync.DirectionOut, gm.Direction)
	assert.Equal(t, "0xfcff5015627b8ce9ceaa7f5b38a6679f65fe39a7", gm.ContractAddress)

	// An ETH leg comes back in (different asset than the one sent).
	var haveETHIn bool
	for _, tr := range dt.Transfers {
		if tr.AssetSymbol == "ETH" && tr.Direction == sync.DirectionIn {
			haveETHIn = true
		}
	}
	assert.True(t, haveETHIn, "expected an ETH received leg")
}

func TestConvert_BridgeSendPure(t *testing.T) {
	dt := convert(t, "bridge_send_pure.json", "base")

	assert.Equal(t, sync.OpSend, dt.OperationType)
	require.Len(t, dt.Transfers, 1) // only the bridged GM token out (paidGas filtered)
	assert.Equal(t, sync.DirectionOut, dt.Transfers[0].Direction)
}

func TestConvert_LPAddNFT(t *testing.T) {
	dt := convert(t, "lp_add_nft.json", "base")

	assert.Equal(t, sync.OpDeposit, dt.OperationType)
	// token-vs-nft split: the lpTokensMinted NFT leg is NOT a fungible transfer.
	// Fungible legs kept: USDC out, ETH out (liquidityAdded), ETH in (refund).
	require.Len(t, dt.Transfers, 3)
	for _, tr := range dt.Transfers {
		assert.NotEmpty(t, tr.AssetSymbol, "no NFT leg should be a fungible transfer")
	}

	// NFT position id captured from the nft.id JSON string. The lpTokensMinted
	// leg is BOTH the receipt and the carrier of this id, so the receipt rule
	// has to run after the id is taken — dropping the leg earlier would lose
	// the id that LP position tracking is keyed on.
	assert.Equal(t, "5325584", dt.NFTTokenID)

	// The receipt's action survives on LegActions even though the leg has no
	// symbol and no contract address at all — the NFT case (#57). This is the
	// case none of the removed symbol matchers could express.
	assert.Contains(t, dt.LegActions, "lpTokensMinted")
	assert.Contains(t, dt.LegActions, "liquidityAdded")

	// No protocol name is derived any more: protocol.name is null on this
	// fixture, and nothing scans party or NFT names for "Uniswap V3".
	assert.Empty(t, dt.Protocol)

	// Classified by the liquidityAdded leg action instead.
	assert.Equal(t, ledger.TxTypeLPDeposit, sync.NewClassifier().Classify(dt))
}

func TestConvert_LPRemove(t *testing.T) {
	dt := convert(t, "lp_remove.json", "base")

	assert.Equal(t, sync.OpWithdraw, dt.OperationType)
	require.Len(t, dt.Transfers, 1) // one-sided ETH received; paidGas-only sent filtered
	assert.Equal(t, sync.DirectionIn, dt.Transfers[0].Direction)
	assert.Contains(t, dt.LegActions, "liquidityRemoved")
	assert.Empty(t, dt.Protocol, "protocol.name is null; nothing guesses it any more")

	assert.Equal(t, ledger.TxTypeLPWithdraw, sync.NewClassifier().Classify(dt))
}

func TestConvert_LPRemoveUniV2(t *testing.T) {
	// Real Polygon QuickSwap v2 removeLiquidity: LP token (UNI-V2) burned out +
	// two tokens received. protocol.name is non-null ("QuickSwap") → passthrough.
	dt := convert(t, "lp_remove_univ2.json", "polygon")

	assert.Equal(t, sync.OpWithdraw, dt.OperationType)
	assert.Equal(t, "polygon", dt.ChainID)
	assert.Equal(t, "QuickSwap", dt.Protocol)

	// paidGas filtered AND the UNI-V2 LP receipt dropped by its lpTokenBurned
	// action: 2 received (USDC, agEUR) remain. `UNI-V2` is the ticker that
	// matched NONE of the four symbol matchers this change removed — it is not
	// `a`+uppercase, not variableDebt, not stableDebt — which is why the rule
	// had to move off ticker shapes and onto the action.
	require.Len(t, dt.Transfers, 2)

	_, ok := transferBySymbol(dt.Transfers, "UNI-V2")
	assert.False(t, ok, "the UNI-V2 LP receipt must not survive as a transfer")
	assert.Contains(t, dt.LegActions, "lpTokenBurned")

	usdc, ok := transferBySymbol(dt.Transfers, "USDC")
	require.True(t, ok)
	assert.Equal(t, sync.DirectionIn, usdc.Direction)
	assert.Equal(t, bigStr(t, "109853843065"), usdc.Amount) // 109853.843065 * 1e6

	agEUR, ok := transferBySymbol(dt.Transfers, "agEUR")
	require.True(t, ok)
	assert.Equal(t, sync.DirectionIn, agEUR.Direction)

	// Native POL fee sentinel → empty-contract native fee.
	require.NotNil(t, dt.Fee)
	assert.Equal(t, "POL", dt.Fee.AssetSymbol)

	// liquidityRemoved on the surviving legs → LP withdraw, on a protocol the
	// two removed hardcoded markers had never heard of.
	assert.Equal(t, "QuickSwap", dt.Protocol)
	assert.Equal(t, ledger.TxTypeLPWithdraw, sync.NewClassifier().Classify(dt))
}

func TestConvert_ClaimRewards(t *testing.T) {
	dt := convert(t, "claim_rewards.json", "base")

	// claimRewards maps to OpReceive so the classifier's OpReceive+claim-act
	// path (LPClaimFees / LendingClaim) fires.
	assert.Equal(t, sync.OpReceive, dt.OperationType)
	assert.Empty(t, dt.Protocol, "protocol.name is null on this fixture")
	assert.Contains(t, dt.Acts, "claim", "claim-ish type must add a 'claim' act")

	// rewardsReceived is PRINCIPAL, not a receipt: both reward legs survive.
	// This is the boundary the receipt rule draws — a reward is a genuine
	// acquisition and stays a position, unlike an aToken or an LP token (#57).
	require.Len(t, dt.Transfers, 2)
	for _, tr := range dt.Transfers {
		assert.Equal(t, sync.DirectionIn, tr.Direction)
	}
	assert.Contains(t, dt.LegActions, "rewardsReceived")
	assert.False(t, sync.IsReceiptLeg("rewardsReceived"),
		"a reward is an acquisition, not a receipt")
	usdc, ok := transferBySymbol(dt.Transfers, "USDC")
	require.True(t, ok)
	assert.Equal(t, sync.DirectionIn, usdc.Direction)

	// Books as the protocol-NEUTRAL defi_claim. The transaction carries no
	// action naming which market paid the reward, so claiming it was a Uniswap
	// V3 fee collection — which the old party-name scan did — asserted more
	// than the data supports.
	assert.Equal(t, ledger.TxTypeDefiClaim, sync.NewClassifier().Classify(dt))
}

func TestConvert_UnclassifiedBoth(t *testing.T) {
	dt := convert(t, "unclassified_both.json", "base")

	assert.Equal(t, sync.OpExecute, dt.OperationType)
	require.Len(t, dt.Transfers, 2) // USDC in + ETH out (paidGas filtered)

	// Both directions present → classifier infers a swap.
	assert.Equal(t, ledger.TxTypeSwap, sync.NewClassifier().Classify(dt))

	// mapOperationType collapses `unclassified` onto OpExecute, so the flag is
	// the only thing that survives to tell the sync layer this swap inference
	// rests on a shape the provider could not identify (issue #30).
	assert.True(t, dt.Unclassified, "an `unclassified` tx must be flagged across the port")
	assert.Equal(t, "unclassified", dt.ProviderType, "the provider's own type is carried verbatim")
}

// TestConvert_ClassifiedTypesAreNotFlagged: a transaction the provider did
// classify must not carry the unclassified flag, or the WARN trail drowns.
func TestConvert_ClassifiedTypesAreNotFlagged(t *testing.T) {
	for _, fixture := range []string{"swap.json", "claim_rewards.json"} {
		dt := convert(t, fixture, "base")
		assert.False(t, dt.Unclassified, "%s is provider-classified", fixture)
	}
}

func TestConvert_PrecisionLoss(t *testing.T) {
	dt := convert(t, "precision_loss.json", "base")

	assert.True(t, dt.NeedsReview, "amount with excess precision must flag for review")
	assert.NotEmpty(t, dt.ReviewReason)
	require.Len(t, dt.Transfers, 1)
	// 1.23456789 with 6 decimals truncates to 1234567 (value still returned).
	assert.Equal(t, bigStr(t, "1234567"), dt.Transfers[0].Amount)
}

func TestConvert_Failed(t *testing.T) {
	dt := convert(t, "failed.json", "base")

	assert.Equal(t, "failed", dt.Status)
	assert.Equal(t, sync.OpExecute, dt.OperationType) // failed maps to execute
	assert.Empty(t, dt.Transfers, "only a paidGas leg, filtered out")
}

func TestConvert_ChainMappingRoundtrip(t *testing.T) {
	// A base fixture converted under the ethereum domain slug must emit that slug.
	dt := convert(t, "swap.json", "ethereum")
	assert.Equal(t, "ethereum", dt.ChainID)
	assert.Equal(t, "ethereum:0xf7ae9d268cb73d4868b00b463d971879e9d37da577b359198d08d0b774564c9c", dt.ID)
}

// TestConvert_DecimalsZeroReceiptDoesNotPanic ensures a zero-decimals / empty
// symbol receipt token converts as identity without panicking.
func TestConvert_DecimalsZeroReceiptDoesNotPanic(t *testing.T) {
	zero := "0x0000000000000000000000000000000000000001"
	tx := Transaction{
		ClassificationData: ClassificationData{
			Type: "receiveToken",
			Received: []Transfer{
				{
					Action: "received",
					Amount: "1000",
					From:   Party{Address: strptr("0xabc")},
					To:     Party{Address: strptr("0xdef")},
					Token:  &Token{Symbol: "", Name: "", Address: zero, Decimals: 0},
				},
			},
		},
		RawTransactionData: RawTransactionData{
			TransactionHash: "0xhash",
			Timestamp:       1700000000,
		},
	}
	dt, err := convertTransaction(tx, "base")
	require.NoError(t, err)
	require.Len(t, dt.Transfers, 1)
	assert.Equal(t, bigStr(t, "1000"), dt.Transfers[0].Amount)
	assert.False(t, dt.NeedsReview)
}

// TestConvert_ReceiptLegWithoutSymbolOrAddress is the case that decided the
// design (#57): a receipt leg carrying NO symbol and NO contract address.
//
// Uniswap V3 mints its LP receipt as an NFT, so the leg arrives with `nft` set
// and `token` nil — there is no ticker to match a prefix against and no address
// to look up in a registry. Every one of the four symbol matchers this change
// removed was structurally incapable of seeing it. The action is the only thing
// the leg carries, and it is enough.
//
// The adapter must handle it by action and must not panic on the nil token.
func TestConvert_ReceiptLegWithoutSymbolOrAddress(t *testing.T) {
	tx := Transaction{
		ClassificationData: ClassificationData{
			Type: "addLiquidity",
			Sent: []Transfer{
				{
					Action: "liquidityAdded",
					Amount: "100",
					From:   Party{Address: strptr("0xabc")},
					To:     Party{Address: strptr("0xpool")},
					Token:  &Token{Symbol: "USDC", Name: "USD Coin", Address: "0xusdc", Decimals: 6},
				},
			},
			Received: []Transfer{
				{
					// No token, no symbol, no contract address — only an action
					// and an NFT id.
					Action: "lpTokensMinted",
					Amount: "1",
					From:   Party{Address: strptr("0x0000000000000000000000000000000000000000")},
					To:     Party{Address: strptr("0xabc")},
					NFT:    &NFT{ID: json.Number("7777"), Name: "Uniswap V3 Positions NFT-V1"},
				},
			},
		},
		RawTransactionData: RawTransactionData{
			TransactionHash: "0xnosymbol",
			Timestamp:       1700000000,
		},
	}

	dt, err := convertTransaction(tx, "base")
	require.NoError(t, err, "a leg with neither symbol nor address must not break conversion")

	// The receipt contributed no transfer; the principal is the only one.
	require.Len(t, dt.Transfers, 1)
	assert.Equal(t, "USDC", dt.Transfers[0].AssetSymbol)
	assert.Contains(t, dt.LegActions, "liquidityAdded")

	// The receipt's action survived even though the leg did not, and so did the
	// NFT id it was carrying.
	assert.Contains(t, dt.LegActions, "lpTokensMinted")
	assert.Equal(t, "7777", dt.NFTTokenID)

	// Classified from the action alone.
	assert.Equal(t, ledger.TxTypeLPDeposit, sync.NewClassifier().Classify(dt))
}

func strptr(s string) *string { return &s }
