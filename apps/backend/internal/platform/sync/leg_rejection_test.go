package sync_test

import (
	"context"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	pkgsync "github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// =============================================================================
// Issue #60 — rejection is recorded on the LEG, and the reconciliation delta
// stops counting legs that were rejected.
//
// Every fixture here is taken from the first real resync (#59), whose three red
// chains are the failure this ticket removes:
//
//	arbitrum | ETH  on_chain=21083263648000    calculated=-662337582090000
//	base     | USDC on_chain=13888232          calculated=25000000000018233539
//	ethereum | ETH  on_chain=22656926547054    calculated=-1013792350274497
//
// The base/USDC line is the one that is entirely a defect: the wallet's REAL
// USDC nets to exactly 13888232 base units, matching the position to the unit.
// The rest of that figure is two other contracts that happen to also be called
// "USDC" being summed into the same symbol-keyed bucket.
// =============================================================================

const (
	// aTokenCbBTCBase is aBascbBTC, the Aave receipt minted against a cbBTC
	// supply. From the #59 attribution: knownness verdict `known` (the price
	// provider quotes it), position 0.11219239, ledger balance none. That
	// combination is what flagged the chain — the position filter correctly
	// declines to exclude a known asset, and the receipt rule had already
	// removed the leg.
	aTokenCbBTCBase = "0xbdb9300b7cde636d9cd4aff00f6f009ffbbc8ee6"

	// spamUSDC18Base is the 18-decimal contract calling itself "USDC" that put
	// 25000000000000000000 into the real USDC's net flow. Measured on the live
	// database: 1 inbound leg, 25e18 base units.
	spamUSDC18Base = "0xd177e58b02e68b2d13f792288385db41569b104f"
)

// newRejectionReconciler wires a Reconciler over a fixed set of raws and
// positions on `base`, with the known-asset filter attached, and reports which
// chains ended up flagged.
//
// It returns the flagged chains rather than a bare count because the acceptance
// criteria are about a chain NOT going into sync error — an assertion that must
// name the chain, or it would pass on a reconciler that flagged nothing because
// it examined nothing.
func newRejectionReconciler(
	t *testing.T,
	reg pkgsync.KnownnessRegistry,
	raws []*pkgsync.RawTransaction,
	positions []pkgsync.OnChainPosition,
) (*pkgsync.Reconciler, *MockWalletRepository, *wallet.Wallet) {
	t.Helper()
	log := logger.New("test", os.Stdout)

	w := newTestWallet(uuid.New(), "0x9afc000000000000000000000000000000000000")

	rawTxRepo := new(MockRawTransactionRepository)
	rawTxRepo.On("GetAllByWallet", mock.Anything, w.ID).Return(raws, nil)

	posProvider := new(MockPositionDataProvider)
	posProvider.On("GetPositions", mock.Anything, w.Address, "base").Return(positions, nil)

	walletRepo := new(MockWalletRepository)
	walletRepo.On("SetSyncPhase", mock.Anything, w.ID, mock.Anything).Return(nil)
	walletRepo.On("GetChainSyncRows", mock.Anything, w.ID).
		Return([]wallet.WalletChainSync{{Chain: "base"}}, nil)
	walletRepo.On("SetChainSyncError", mock.Anything, w.ID, mock.Anything, mock.Anything).
		Return(nil).Maybe()

	r := pkgsync.NewReconciler(rawTxRepo, posProvider, walletRepo,
		pkgsync.NewKnownAssetFilter(reg), log)
	return r, walletRepo, w
}

// flaggedChains reports every chain the reconciler put into sync error, with
// the message, so a test can assert on WHAT was flagged rather than only how
// many.
func flaggedChains(repo *MockWalletRepository) map[string]string {
	out := map[string]string{}
	for _, call := range repo.Calls {
		if call.Method != "SetChainSyncError" {
			continue
		}
		chain, _ := call.Arguments.Get(2).(string)
		msg, _ := call.Arguments.Get(3).(string)
		out[chain] = msg
	}
	return out
}

// rawWithLegs builds a stored raw from a decoded transaction, the same way the
// collector does: the raw's JSON IS the decoded transaction, which is why a leg
// the provider adapter dropped is not in it while a rejection the adapter
// recorded is. That asymmetry is the whole reason the receipt rule needed the
// rejection written down rather than re-derived.
func rawWithLegs(t *testing.T, dt pkgsync.DecodedTransaction) *pkgsync.RawTransaction {
	t.Helper()
	return &pkgsync.RawTransaction{
		ID:               uuid.New(),
		ExternalID:       dt.ID,
		TxHash:           dt.TxHash,
		ChainID:          dt.ChainID,
		OperationType:    string(dt.OperationType),
		MinedAt:          dt.MinedAt,
		Status:           "confirmed",
		RawJSON:          marshalDecodedTx(dt),
		ProcessingStatus: pkgsync.ProcessingStatusPending,
	}
}

// -----------------------------------------------------------------------------
// MANDATORY TEST 1 — a wallet with a rejected receipt gets NO chain sync error.
// -----------------------------------------------------------------------------

// TestReconcile_RejectedReceipt_DoesNotFlagChain is the port statement of the
// ticket's first mandatory case, and it is the case that could not be fixed by
// re-resolving legs.
//
// A lending supply arrives as two legs: the principal `deposited` leaving the
// wallet, and the receipt `collateralSharesMinted` coming back. The receipt rule
// (#57) drops the receipt INSIDE the provider adapter, before the raw is
// written, so the receipt's contract exists nowhere in the collected history —
// LegActions preserves that a receipt happened, not which asset it was. The
// receipt token is meanwhile real and quoted, so the known-asset filter says
// `known` (correctly) and the position filter does not exclude it.
//
// Result before this change: net flow zero, position 0.11219239 aBascbBTC,
// delta = the whole balance, chain flagged — on the first sync of any wallet
// that ever supplied to a lending market. The flag fired on behaviour the rule
// had produced deliberately.
func TestReconcile_RejectedReceipt_DoesNotFlagChain(t *testing.T) {
	ctx := context.Background()
	minedAt := time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC)

	reg := newFakeKnownnessRegistry()
	// Both assets are KNOWN. This is the crux: the receipt is not spam, and no
	// property of the token distinguishes it. Only the leg's action did, and
	// that fact now survives as a recorded rejection.
	reg.setStatus(pkgsync.NewAssetKey("base", realUSDCBase),
		pkgsync.KnownnessKnown, pkgsync.KnownnessSourceBuiltin)
	reg.setStatus(pkgsync.NewAssetKey("base", aTokenCbBTCBase),
		pkgsync.KnownnessKnown, pkgsync.KnownnessSourceQuotable)

	// The supply as the adapter now emits it: the principal leg present, the
	// receipt leg absent from Transfers but RECORDED in RejectedLegs.
	supply := pkgsync.DecodedTransaction{
		ID:            "base:0xsupply",
		TxHash:        "0xsupply",
		ChainID:       "base",
		OperationType: pkgsync.OpDeposit,
		MinedAt:       minedAt,
		Status:        "confirmed",
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol:     "USDC",
			ContractAddress: realUSDCBase,
			Decimals:        6,
			Amount:          big.NewInt(1_000_000),
			Direction:       pkgsync.DirectionOut,
		}},
		LegActions: []string{pkgsync.ActionDeposited, pkgsync.ActionCollateralSharesMinted},
		RejectedLegs: []pkgsync.RejectedLeg{{
			ChainID:         "base",
			ContractAddress: aTokenCbBTCBase,
			AssetSymbol:     "aBascbBTC",
			Amount:          big.NewInt(11_219_239),
			Direction:       pkgsync.DirectionIn,
			Reason:          pkgsync.RejectionReceipt,
			Action:          pkgsync.ActionCollateralSharesMinted,
		}},
	}

	// The provider reports the receipt as a position, because it IS one on
	// chain. The exact quantity from the #59 attribution.
	positions := []pkgsync.OnChainPosition{{
		ChainID:         "base",
		AssetSymbol:     "aBascbBTC",
		ContractAddress: aTokenCbBTCBase,
		Decimals:        8,
		Quantity:        big.NewInt(11_219_239),
	}}

	raws := []*pkgsync.RawTransaction{rawWithLegs(t, supply)}

	r, walletRepo, w := newRejectionReconciler(t, reg, raws, positions)
	res, err := r.Reconcile(ctx, w)
	require.NoError(t, err)

	assert.Empty(t, flaggedChains(walletRepo),
		"a position whose leg the receipt rule rejected is explained, not discrepant: "+
			"flagging it would fire on the rule working as designed")
	assert.Equal(t, 0, res.Flagged)

	// The position is not merely un-flagged — it is REPORTED as explained, with
	// the rule named. Silence would leave "not flagged" indistinguishable from
	// "not looked at", and #61 attributes this exact category from this field.
	require.Len(t, res.Explained, 1,
		"the receipt position must be carried with its attribution, not dropped in silence")
	assert.Equal(t, "aBascbBTC", res.Explained[0].AssetSymbol)
	assert.Equal(t, aTokenCbBTCBase, res.Explained[0].Contract)
	assert.Equal(t, []pkgsync.RejectionReason{pkgsync.RejectionReceipt}, res.Explained[0].Reasons)
}

// -----------------------------------------------------------------------------
// MANDATORY TEST 2 — a wallet with a rejected spam token gets NO sync error.
// -----------------------------------------------------------------------------

// TestReconcile_RejectedSpamToken_DoesNotFlagChain is the ticket's second
// mandatory case, written on the MIXED transaction that is the ticket's own
// argument for why a per-transaction status cannot carry this fact.
//
// A spam leg differs from a receipt in where it is rejected. It survives into
// the raw — the adapter has no opinion about it — and is dropped later by the
// known-asset filter. On an INITIAL sync the pipeline runs collect → reconcile →
// process, so at reconcile time not one raw has been through that filter; the
// reconciler therefore re-derives the verdict from the same local table instead
// of waiting for a record that does not exist yet. That is precisely why the two
// rules could not share one mechanism.
//
// The transaction here carries BOTH a real USDC leg and a spam leg, so its raw
// is `processed` as a whole while one of its legs is not in the ledger. The
// assertion that carries the ticket is therefore on the REAL asset: its delta
// must be computed from its own legs only. A test that used a spam-only
// transaction would pass without any of this work, because the position filter
// (#58) already declines to compare a convicted position — it would prove the
// previous ticket, not this one.
func TestReconcile_RejectedSpamToken_DoesNotFlagChain(t *testing.T) {
	ctx := context.Background()
	minedAt := time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC)

	reg := newFakeKnownnessRegistry()
	reg.setStatus(pkgsync.NewAssetKey("base", realUSDCBase),
		pkgsync.KnownnessKnown, pkgsync.KnownnessSourceBuiltin)
	reg.setStatus(pkgsync.NewAssetKey("base", spamUSDCBase),
		pkgsync.KnownnessUnknown, pkgsync.KnownnessSourceQuotable)

	// One transaction, two legs: 1 real USDC in, and a homoglyph forgery
	// airdropped alongside it. Exactly the shape `processing_status` cannot
	// describe — the transaction is booked, one of its legs is not.
	mixed := pkgsync.DecodedTransaction{
		ID:            "base:0xmixed",
		TxHash:        "0xmixed",
		ChainID:       "base",
		OperationType: pkgsync.OpReceive,
		MinedAt:       minedAt,
		Status:        "confirmed",
		Transfers: []pkgsync.DecodedTransfer{
			{
				AssetSymbol:     "USDC",
				ContractAddress: realUSDCBase,
				Decimals:        6,
				Amount:          big.NewInt(1_000_000),
				Direction:       pkgsync.DirectionIn,
			},
			{
				AssetSymbol:     "UЅDС", // Cyrillic Ѕ and С
				ContractAddress: spamUSDCBase,
				Decimals:        6,
				Amount:          big.NewInt(6_525_810_000),
				Direction:       pkgsync.DirectionIn,
			},
		},
	}

	// On chain: the real coin at exactly what its own leg delivered, and the
	// spam still sitting there.
	positions := []pkgsync.OnChainPosition{
		{
			ChainID:         "base",
			AssetSymbol:     "USDC",
			ContractAddress: realUSDCBase,
			Decimals:        6,
			Quantity:        big.NewInt(1_000_000),
		},
		{
			ChainID:         "base",
			AssetSymbol:     "UЅDС",
			ContractAddress: spamUSDCBase,
			Decimals:        6,
			Quantity:        big.NewInt(6_525_810_000),
		},
	}

	raws := []*pkgsync.RawTransaction{rawWithLegs(t, mixed)}

	r, walletRepo, w := newRejectionReconciler(t, reg, raws, positions)
	res, err := r.Reconcile(ctx, w)
	require.NoError(t, err)

	assert.Empty(t, flaggedChains(walletRepo),
		"the real coin's position matches its own legs and the spam is convicted: "+
			"nothing here is a discrepancy, so the chain must not go into sync error")
	assert.Equal(t, 0, res.Flagged)

	// The real coin WAS compared — without this the test could pass by
	// comparing nothing at all.
	assert.Equal(t, 1, res.PositionsChecked,
		"the real coin must still be reconciled; only the spam leg drops out")
	assert.Equal(t, 1, res.SkippedUnknown,
		"the spam position is still counted and carried, never dropped in silence")

	// The spam leg is attributed to the rule that rejected it, from the same
	// computation the report reads.
	flows, err := pkgsync.NetFlows(ctx, raws, pkgsync.NewKnownAssetFilter(reg),
		logger.New("test", os.Stdout))
	require.NoError(t, err)
	for _, f := range flows {
		if f.Key == pkgsync.NewAssetKey("base", realUSDCBase) {
			assert.Equal(t, big.NewInt(1_000_000), f.NetFlow,
				"the real coin's flow counts its own leg and nothing else")
			assert.False(t, f.Explained())
		}
		if f.Key == pkgsync.NewAssetKey("base", spamUSDCBase) {
			assert.Equal(t, 0, f.NetFlow.Sign(),
				"a leg the filter rejected contributes nothing to any flow")
			assert.Equal(t, []pkgsync.RejectionReason{pkgsync.RejectionUnknownAsset}, f.RejectedBy)
		}
	}
}

// -----------------------------------------------------------------------------
// MANDATORY TEST 3 — a genuine discrepancy in a KNOWN token still flags.
// -----------------------------------------------------------------------------

// TestReconcile_GenuineDiscrepancyInKnownAsset_StillFlagsChain is the guard on
// the two above. Excluding rejected legs must narrow the delta to the assets the
// ledger actually books — it must not switch the check off.
//
// The fixture is the real arbitrum ETH line from #59, which the measurement
// showed is NOT a filtering artifact: in − out − fee reproduces `calculated`
// exactly from legs that are all native ETH, known by construction and rejected
// by nothing. That chain was right to be red then and must stay red now.
func TestReconcile_GenuineDiscrepancyInKnownAsset_StillFlagsChain(t *testing.T) {
	ctx := context.Background()
	minedAt := time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC)

	reg := newFakeKnownnessRegistry()
	reg.setStatus(pkgsync.NewAssetKey("base", realUSDCBase),
		pkgsync.KnownnessKnown, pkgsync.KnownnessSourceBuiltin)

	// 1 USDC received; nothing rejected anywhere in this history.
	recv := pkgsync.DecodedTransaction{
		ID:            "base:0xrecv",
		TxHash:        "0xrecv",
		ChainID:       "base",
		OperationType: pkgsync.OpReceive,
		MinedAt:       minedAt,
		Status:        "confirmed",
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol:     "USDC",
			ContractAddress: realUSDCBase,
			Decimals:        6,
			Amount:          big.NewInt(1_000_000),
			Direction:       pkgsync.DirectionIn,
		}},
	}

	// On chain there are 5 USDC. Delta = 4 USDC, far beyond the 10-base-unit
	// dust band, in an asset nothing explains away.
	positions := []pkgsync.OnChainPosition{{
		ChainID:         "base",
		AssetSymbol:     "USDC",
		ContractAddress: realUSDCBase,
		Decimals:        6,
		Quantity:        big.NewInt(5_000_000),
	}}

	raws := []*pkgsync.RawTransaction{rawWithLegs(t, recv)}

	r, walletRepo, w := newRejectionReconciler(t, reg, raws, positions)
	res, err := r.Reconcile(ctx, w)
	require.NoError(t, err)

	require.Equal(t, 1, res.Flagged,
		"a real discrepancy beyond dust in an unrejected, known asset must still flag")
	flagged := flaggedChains(walletRepo)
	require.Contains(t, flagged, "base")
	assert.Contains(t, flagged["base"], "USDC")
	assert.Empty(t, res.Explained,
		"nothing rejected this asset, so nothing may claim to explain it")
}

// -----------------------------------------------------------------------------
// REGRESSION — the symbol-keyed flow that produced the base/USDC red line.
// -----------------------------------------------------------------------------

// TestReconcile_SpamSharingTickerDoesNotPolluteRealAssetFlow pins the finding
// that the live database handed over, and it is the reason the flow is keyed by
// (chain, contract) rather than by ticker.
//
// Measured on the resynced database: base USDC legs group into THREE contracts,
// all three calling themselves "USDC" —
//
//	0x8335…2913 (6 dec)   in 17981216517, out 17967328285  → net 13888232
//	0xc5b4…8635 (6 dec)   in 4345307
//	0xd177…104f (18 dec)  in 25000000000000000000
//
// The real coin's net is 13888232, which equals the on-chain position to the
// unit. The symbol-keyed flow reported 25000000000018233539, the sum of all
// three, and the chain went to sync error over a number that was mostly other
// assets' amounts. A per-leg rejection alone would not have fixed this: the
// pollution came from ADDING UP TICKERS, so the key had to change too.
//
// This fixture reproduces the three contracts at their measured amounts.
func TestReconcile_SpamSharingTickerDoesNotPolluteRealAssetFlow(t *testing.T) {
	ctx := context.Background()
	minedAt := time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC)

	reg := newFakeKnownnessRegistry()
	reg.setStatus(pkgsync.NewAssetKey("base", realUSDCBase),
		pkgsync.KnownnessKnown, pkgsync.KnownnessSourceBuiltin)
	// The 18-decimal impostor is convicted spam.
	reg.setStatus(pkgsync.NewAssetKey("base", spamUSDC18Base),
		pkgsync.KnownnessUnknown, pkgsync.KnownnessSourceQuotable)

	leg := func(id, contract string, decimals int, amount *big.Int, dir pkgsync.TransferDirection) pkgsync.DecodedTransaction {
		return pkgsync.DecodedTransaction{
			ID:            id,
			TxHash:        id,
			ChainID:       "base",
			OperationType: pkgsync.OpReceive,
			MinedAt:       minedAt,
			Status:        "confirmed",
			Transfers: []pkgsync.DecodedTransfer{{
				AssetSymbol:     "USDC", // the SAME ticker on every one of them
				ContractAddress: contract,
				Decimals:        decimals,
				Amount:          amount,
				Direction:       dir,
			}},
		}
	}

	spam18 := new(big.Int)
	spam18.SetString("25000000000000000000", 10)

	raws := []*pkgsync.RawTransaction{
		rawWithLegs(t, leg("base:0xreal-in", realUSDCBase, 6, big.NewInt(17_981_216_517), pkgsync.DirectionIn)),
		rawWithLegs(t, leg("base:0xreal-out", realUSDCBase, 6, big.NewInt(17_967_328_285), pkgsync.DirectionOut)),
		rawWithLegs(t, leg("base:0xspam18", spamUSDC18Base, 18, spam18, pkgsync.DirectionIn)),
	}

	// Only the real coin still has a position; on chain it holds exactly the
	// net of its own legs.
	positions := []pkgsync.OnChainPosition{{
		ChainID:         "base",
		AssetSymbol:     "USDC",
		ContractAddress: realUSDCBase,
		Decimals:        6,
		Quantity:        big.NewInt(13_888_232),
	}}

	r, walletRepo, w := newRejectionReconciler(t, reg, raws, positions)
	res, err := r.Reconcile(ctx, w)
	require.NoError(t, err)

	assert.Empty(t, flaggedChains(walletRepo),
		"the real USDC net flow equals its on-chain position exactly; the chain went "+
			"red only because two other contracts sharing the ticker were summed into it")
	assert.Equal(t, 0, res.Flagged)
	assert.Equal(t, 1, res.PositionsChecked,
		"the real coin is compared, against its OWN flow")
}

// -----------------------------------------------------------------------------
// The exported seam the report (#61) builds F on.
// -----------------------------------------------------------------------------

// TestNetFlows_ExposesRejectionAttributionForTheReport pins acceptance criterion
// 6: the per-leg rejection has to be reachable from outside in a form the
// reconciliation report can build F from.
//
// The requirement behind it is that the report and the flag explain one fact the
// same way. That is guaranteed here by construction rather than by discipline:
// NetFlows and the reconciler call the same computation, so an asset the flag
// treats as explained cannot be a red row in the report.
func TestNetFlows_ExposesRejectionAttributionForTheReport(t *testing.T) {
	ctx := context.Background()
	minedAt := time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC)
	log := logger.New("test", os.Stdout)

	reg := newFakeKnownnessRegistry()
	reg.setStatus(pkgsync.NewAssetKey("base", realUSDCBase),
		pkgsync.KnownnessKnown, pkgsync.KnownnessSourceBuiltin)
	reg.setStatus(pkgsync.NewAssetKey("base", aTokenCbBTCBase),
		pkgsync.KnownnessKnown, pkgsync.KnownnessSourceQuotable)
	reg.setStatus(pkgsync.NewAssetKey("base", spamUSDCBase),
		pkgsync.KnownnessUnknown, pkgsync.KnownnessSourceQuotable)

	supply := pkgsync.DecodedTransaction{
		ID: "base:0xsupply", TxHash: "0xsupply", ChainID: "base",
		OperationType: pkgsync.OpDeposit, MinedAt: minedAt, Status: "confirmed",
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "USDC", ContractAddress: realUSDCBase, Decimals: 6,
			Amount: big.NewInt(1_000_000), Direction: pkgsync.DirectionOut,
		}},
		RejectedLegs: []pkgsync.RejectedLeg{{
			ChainID: "base", ContractAddress: aTokenCbBTCBase, AssetSymbol: "aBascbBTC",
			Amount: big.NewInt(11_219_239), Direction: pkgsync.DirectionIn,
			Reason: pkgsync.RejectionReceipt, Action: pkgsync.ActionCollateralSharesMinted,
		}},
	}
	spamTx := pkgsync.DecodedTransaction{
		ID: "base:0xspam", TxHash: "0xspam", ChainID: "base",
		OperationType: pkgsync.OpReceive, MinedAt: minedAt, Status: "confirmed",
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "UЅDС", ContractAddress: spamUSDCBase, Decimals: 6,
			Amount: big.NewInt(6_525_810_000), Direction: pkgsync.DirectionIn,
		}},
	}

	raws := []*pkgsync.RawTransaction{
		rawWithLegs(t, supply),
		rawWithLegs(t, spamTx),
	}

	flows, err := pkgsync.NetFlows(ctx, raws, pkgsync.NewKnownAssetFilter(reg), log)
	require.NoError(t, err)

	byKey := map[pkgsync.AssetKey]pkgsync.AssetNetFlow{}
	for _, f := range flows {
		byKey[f.Key] = f
	}

	// The principal: a real net flow, nothing explaining it away.
	real := byKey[pkgsync.NewAssetKey("base", realUSDCBase)]
	require.NotNil(t, real.NetFlow)
	assert.Equal(t, big.NewInt(-1_000_000), real.NetFlow)
	assert.False(t, real.Explained(), "a booked asset is not explained away")

	// The receipt: no flow at all, and the rule that removed it is named. This
	// is the row that lets #61 print "in P, not in L" as green instead of red.
	receipt := byKey[pkgsync.NewAssetKey("base", aTokenCbBTCBase)]
	require.True(t, receipt.Explained(),
		"a receipt whose every leg was rejected must still appear, with its reason")
	assert.Equal(t, []pkgsync.RejectionReason{pkgsync.RejectionReceipt}, receipt.RejectedBy)
	assert.Equal(t, 0, receipt.NetFlow.Sign())

	// The spam: rejected by the other rule, and distinguishable from the
	// receipt by which rule it was.
	spam := byKey[pkgsync.NewAssetKey("base", spamUSDCBase)]
	require.True(t, spam.Explained())
	assert.Equal(t, []pkgsync.RejectionReason{pkgsync.RejectionUnknownAsset}, spam.RejectedBy,
		"the two rules stay distinguishable: one is correct double-entry, "+
			"the other is a filter decision that must be listed by name")
	assert.Equal(t, 0, spam.NetFlow.Sign(),
		"a rejected leg contributes no flow to compare a position against")
}

// TestReconcile_PartiallyRejectedAsset_StillFlagsGenuineDiscrepancy is the
// guard against the inverse of the bug this ticket fixes, and it was written
// after review found the inverse actually present.
//
// The exemption is per ASSET while rejection is per LEG, so an asset can have
// legs on both sides of a rule: a receipt leg that was rejected and an ordinary
// leg that was booked. Excusing the whole asset because one leg of it was
// rejected would make it permanently unflaggable — and a real discrepancy in
// the part that WAS booked would vanish in silence. That is "молча заклеено",
// the outcome #49 switched genesis off to stop producing, reintroduced through
// the other door.
//
// The rule the code applies instead: a rejection excuses a missing balance only
// when there is no balance to miss — no leg of the asset was booked at all.
func TestReconcile_PartiallyRejectedAsset_StillFlagsGenuineDiscrepancy(t *testing.T) {
	ctx := context.Background()
	minedAt := time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC)

	reg := newFakeKnownnessRegistry()
	reg.setStatus(pkgsync.NewAssetKey("base", aTokenCbBTCBase),
		pkgsync.KnownnessKnown, pkgsync.KnownnessSourceQuotable)

	mixed := pkgsync.DecodedTransaction{
		ID:            "base:0xmixed-asset",
		TxHash:        "0xmixed-asset",
		ChainID:       "base",
		OperationType: pkgsync.OpReceive,
		MinedAt:       minedAt,
		Status:        "confirmed",
		// A leg of this asset that WAS booked: 5.0 at 8 decimals.
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol:     "aBascbBTC",
			ContractAddress: aTokenCbBTCBase,
			Decimals:        8,
			Amount:          big.NewInt(500_000_000),
			Direction:       pkgsync.DirectionIn,
		}},
		// And a leg of the SAME asset that was rejected as a receipt.
		RejectedLegs: []pkgsync.RejectedLeg{{
			ChainID:         "base",
			ContractAddress: aTokenCbBTCBase,
			AssetSymbol:     "aBascbBTC",
			Decimals:        8,
			Amount:          big.NewInt(11_219_239),
			Direction:       pkgsync.DirectionIn,
			Reason:          pkgsync.RejectionReceipt,
			Action:          pkgsync.ActionCollateralSharesMinted,
		}},
	}

	// On chain there are 999.99999999 — nothing like the 5.0 that was booked.
	positions := []pkgsync.OnChainPosition{{
		ChainID:         "base",
		AssetSymbol:     "aBascbBTC",
		ContractAddress: aTokenCbBTCBase,
		Decimals:        8,
		Quantity:        big.NewInt(99_999_999_999),
	}}

	raws := []*pkgsync.RawTransaction{rawWithLegs(t, mixed)}

	r, walletRepo, w := newRejectionReconciler(t, reg, raws, positions)
	res, err := r.Reconcile(ctx, w)
	require.NoError(t, err)

	require.Equal(t, 1, res.Flagged,
		"one rejected leg must not excuse an asset that also has a booked balance")
	assert.Equal(t, 1, res.PositionsChecked,
		"the asset stays under comparison, it is not dropped from reconciliation")
	assert.Empty(t, res.Explained,
		"an asset with a real ledger balance is not 'explained away' by a rejection")

	flagged := flaggedChains(walletRepo)
	require.Contains(t, flagged, "base")
	assert.Contains(t, flagged["base"], "delta=99499999999",
		"the delta is computed against the BOOKED legs only")

	// The report reaches the same verdict from the same computation — the
	// ticket's requirement that flag and report explain one fact identically.
	flows, err := pkgsync.NetFlows(ctx, raws, pkgsync.NewKnownAssetFilter(reg),
		logger.New("test", os.Stdout))
	require.NoError(t, err)
	require.Len(t, flows, 1)
	assert.True(t, flows[0].Booked)
	assert.False(t, flows[0].Explained(),
		"the report must not call explained what the flag calls a discrepancy")
	assert.Equal(t, big.NewInt(500_000_000), flows[0].NetFlow)
	assert.Equal(t, big.NewInt(11_219_239), flows[0].RejectedAmount,
		"the rejected magnitude is still reported, it simply does not excuse the asset")
}
