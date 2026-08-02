package sync_test

import (
	"context"
	"encoding/json"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/module/transfer"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// =============================================================================
// Issue #33 — Bridge stitching (ADR-0002), asserted at the port seam.
//
// A bridge of the user's own funds between two chains is ONE economic event that
// the provider decodes as two unrelated single-chain transactions. Left
// unstitched, the source leg books a disposal (phantom PnL, a lot consumed by
// FIFO) and the destination leg opens a fresh lot at market — destroying exactly
// the cost basis this product exists to compute.
//
// These tests drive real DecodedTransactions — the shape the Noves adapter
// produces, including the provider's own `sendToBridge`/`receiveFromBridge`
// type — through the stitcher and on to the ledger transaction it produces.
//
// The fixtures mirror REAL data captured from the calibration wallet
// (0x9afc…811B) on base/arbitrum, including the two awkward shapes that broke
// the naive rules: same-transaction dust refunds on genuine pure-sends, and a
// genuine bridge-as-swap that must never be stitched.
// =============================================================================

const (
	brWallet    = "0x9afcd847c633b820a2f291794d28d374b555811b"
	brBridgeCtr = "0x89c6340b1a1f4b25d36cd8b063d49045caf3f818"
	brBase      = "base"
	brArbitrum  = "arbitrum"
)

var brBaseTime = time.Date(2026, 2, 2, 10, 0, 0, 0, time.UTC)

// usdc renders a USDC amount (6 decimals) in base units.
func usdc(whole, micros int64) *big.Int {
	return big.NewInt(whole*1_000_000 + micros)
}

// sendLeg builds a `sendToBridge` decoded transaction: value leaves the wallet
// toward the bridge contract. Note the recipient is the BRIDGE, never the
// destination wallet — that is the empirical fact that forces receive-triggered
// matching.
func sendLeg(chain, hash string, amount *big.Int, symbol string, decimals int, at time.Time) sync.DecodedTransaction {
	return sync.DecodedTransaction{
		ID:            chain + ":" + hash,
		TxHash:        hash,
		ChainID:       chain,
		OperationType: sync.OpSend,
		ProviderType:  "sendToBridge",
		Acts:          []string{"sendToBridge", "bridged"},
		Transfers: []sync.DecodedTransfer{{
			AssetSymbol: symbol,
			Decimals:    decimals,
			Amount:      amount,
			Direction:   sync.DirectionOut,
			Sender:      brWallet,
			Recipient:   brBridgeCtr,
		}},
		MinedAt: at,
		Status:  "confirmed",
	}
}

// receiveLeg builds a `receiveFromBridge` decoded transaction: value arrives
// from the bridge into the wallet ("This wallet"). This is the trigger side.
func receiveLeg(chain, hash string, amount *big.Int, symbol string, decimals int, at time.Time) sync.DecodedTransaction {
	return sync.DecodedTransaction{
		ID:            chain + ":" + hash,
		TxHash:        hash,
		ChainID:       chain,
		OperationType: sync.OpReceive,
		ProviderType:  "receiveFromBridge",
		Acts:          []string{"receiveFromBridge", "bridged"},
		Transfers: []sync.DecodedTransfer{{
			AssetSymbol: symbol,
			Decimals:    decimals,
			Amount:      amount,
			Direction:   sync.DirectionIn,
			Sender:      brBridgeCtr,
			Recipient:   brWallet,
		}},
		MinedAt: at,
		Status:  "confirmed",
	}
}

// withInbound adds a same-transaction inbound leg back to the wallet — the shape
// that distinguishes a bridge-as-swap from a pure send (or, when the asset
// matches, is merely a dust refund).
func withInbound(tx sync.DecodedTransaction, symbol string, decimals int, amount *big.Int) sync.DecodedTransaction {
	tx.Transfers = append(tx.Transfers, sync.DecodedTransfer{
		AssetSymbol: symbol,
		Decimals:    decimals,
		Amount:      amount,
		Direction:   sync.DirectionIn,
		Sender:      brBridgeCtr,
		Recipient:   brWallet,
	})
	return tx
}

// -----------------------------------------------------------------------------
// AC1 — matching send + receive → ONE cross-chain internal transfer
// -----------------------------------------------------------------------------

// TestStitch_MatchingLegs_ProduceOneCrossChainInternalTransfer is the headline
// case, using the real calibration pair: 24.446762 USDC leaves arbitrum and
// 24.441577 USDC arrives on base 2 seconds later, the bridge having withheld
// 0.0212% as its fee.
//
// The send leg must become a cross-chain internal_transfer naming both chains,
// and the receive leg must be absorbed rather than recorded — otherwise the
// arrival opens a second lot and the same value is counted twice.
func TestStitch_MatchingLegs_ProduceOneCrossChainInternalTransfer(t *testing.T) {
	send := sendLeg(brArbitrum, "0xa8333fc5", usdc(24, 446762), "USDC", 6, brBaseTime)
	recv := receiveLeg(brBase, "0x61749a8f", usdc(24, 441577), "USDC", 6, brBaseTime.Add(2*time.Second))

	plan := sync.Stitch([]sync.DecodedTransaction{send, recv}, brWallet, brBaseTime.Add(time.Hour))

	require.Equal(t, sync.StitchAsSource, plan.Decision(0),
		"the send leg carries the movement: it must become the cross-chain internal transfer")
	assert.Equal(t, brBase, plan.DestinationChain(0),
		"the destination chain comes from the matched receive leg — nothing else knows it")
	require.Equal(t, sync.StitchSuppress, plan.Decision(1),
		"the receive leg must record nothing of its own, or the arrival opens a second lot "+
			"and the bridged value is counted twice")
}

// TestStitch_MatchedBridge_RecordsInternalTransferNotDisposal follows the
// stitched leg through the real TxBuilder to the ledger transaction, then
// through the REAL internal-transfer handler to the entries it generates.
//
// This is the chain of custody that makes the feature true rather than merely
// planned: the type must be internal_transfer (not transfer_out), the two legs
// must land on the two different chains, and they must balance.
func TestStitch_MatchedBridge_RecordsInternalTransferNotDisposal(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, brWallet)

	env := newIdemEnv(t, userID, map[string]*wallet.Wallet{brWallet: w})
	env.ledgerSvc.On("RecordTransaction", mock.Anything, ledger.TxTypeInternalTransfer, "noves",
		mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil).Once()

	send := sendLeg(brArbitrum, "0xa8333fc5", usdc(24, 446762), "USDC", 6, brBaseTime)
	send.DestChainID = brBase

	_, err := env.builder.ProcessStitchedBridge(ctx, w, send, usdc(24, 446762))
	require.NoError(t, err)

	require.Len(t, env.ledgerSvc.recordedTransactions, 1)
	rec := env.ledgerSvc.recordedTransactions[0]
	require.Equal(t, ledger.TxTypeInternalTransfer, rec.TxType,
		"a self-bridge is a move, not a sale: booking it as transfer_out realizes phantom PnL "+
			"and resets cost basis on the destination chain")
	assert.Equal(t, brArbitrum, rec.RawData["source_chain_id"])
	assert.Equal(t, brBase, rec.RawData["dest_chain_id"])
	assert.Equal(t, w.ID.String(), rec.RawData["source_wallet_id"])
	assert.Equal(t, w.ID.String(), rec.RawData["dest_wallet_id"],
		"a self-bridge moves one wallet's funds between chains: both sides are the same wallet")

	// The real handler must accept it and split it across the two chains.
	walletRepo := new(MockTransferWalletRepo)
	walletRepo.On("GetByID", mock.Anything, w.ID).Return(w, nil).Maybe()
	handler := transfer.NewInternalTransferHandler(walletRepo, logger.NewDefault("test"))

	entries, err := handler.Handle(ctx, rec.RawData)
	require.NoError(t, err, "the handler must accept a same-wallet transfer when the chains differ")

	var debit, credit *ledger.Entry
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
	assert.Equal(t, brBase, debit.Metadata["chain_id"], "the inflow books on the destination chain")
	assert.Equal(t, brArbitrum, credit.Metadata["chain_id"], "the outflow books on the source chain")
	assert.NotEqual(t, debit.Metadata["account_code"], credit.Metadata["account_code"],
		"different accounts is what makes the tax-lot hook carry the basis across rather than net to nothing")
	assert.Equal(t, credit.AssetID, debit.AssetID,
		"the hook pairs disposal to acquisition by asset")

	debitSum, creditSum := big.NewInt(0), big.NewInt(0)
	for _, e := range entries {
		if e.DebitCredit == ledger.Debit {
			debitSum.Add(debitSum, e.Amount)
		} else {
			creditSum.Add(creditSum, e.Amount)
		}
	}
	assert.Equal(t, 0, debitSum.Cmp(creditSum), "an unbalanced transaction never reaches the tax-lot hook")
}

// TestStitch_FeeToleranceBoundary pins the calibrated tolerance from both sides.
// The real worst-case bridge fee measured was 0.0212%; the tolerance is 1%.
func TestStitch_FeeToleranceBoundary(t *testing.T) {
	tests := []struct {
		name     string
		received *big.Int
		stitch   bool
		why      string
	}{
		{"exact", usdc(1000, 0), true, "a zero-fee bridge is real — one calibration pair had exactly 0 fee"},
		{"real observed fee 0.0212%", big.NewInt(999_788_000), true, "the worst fee actually observed must match"},
		{"just inside 1%", big.NewInt(990_100_000), true, "inside the calibrated tolerance"},
		{"just outside 1%", big.NewInt(989_000_000), false, "beyond the tolerance the pair is not credibly one bridge"},
		{"received exceeds sent", usdc(1000, 1), false, "a bridge withholds, never adds — more back than went out is a different event"},
		{"half", usdc(500, 0), false, "wildly short: two unrelated movements, stitching them would fabricate a transfer"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			send := sendLeg(brArbitrum, "0xsend", usdc(1000, 0), "USDC", 6, brBaseTime)
			recv := receiveLeg(brBase, "0xrecv", tc.received, "USDC", 6, brBaseTime.Add(time.Minute))

			plan := sync.Stitch([]sync.DecodedTransaction{send, recv}, brWallet, brBaseTime.Add(time.Hour))

			if tc.stitch {
				assert.Equal(t, sync.StitchAsSource, plan.Decision(0), tc.why)
				assert.Equal(t, sync.StitchSuppress, plan.Decision(1), tc.why)
			} else {
				assert.NotEqual(t, sync.StitchAsSource, plan.Decision(0), tc.why)
				assert.NotEqual(t, sync.StitchSuppress, plan.Decision(1),
					"an unmatched receive stays a standalone transfer_in")
			}
		})
	}
}

// -----------------------------------------------------------------------------
// AC2 — ambiguity (0 or >=2 candidates) → no stitch, both legs standalone
// -----------------------------------------------------------------------------

// TestStitch_TwoIdenticalCandidates_RefusesToStitch is the case the ADR cares
// most about. Two identical sends and one receive: any pairing is a guess, and a
// wrong guess destroys two real movements and fabricates a third. The stitcher
// must decline.
//
// `now` is past the window so the legs are RELEASED rather than held, which is
// what lets this assert their final resting state. Inside the window they would
// legitimately be held awaiting a clarifying counterpart; ambiguity is not a
// reason to stall forever, only a reason never to guess.
func TestStitch_TwoIdenticalCandidates_RefusesToStitch(t *testing.T) {
	txs := []sync.DecodedTransaction{
		sendLeg(brArbitrum, "0xsend1", usdc(500, 0), "USDC", 6, brBaseTime),
		sendLeg(brArbitrum, "0xsend2", usdc(500, 0), "USDC", 6, brBaseTime.Add(time.Minute)),
		receiveLeg(brBase, "0xrecv", usdc(499, 900000), "USDC", 6, brBaseTime.Add(10*time.Minute)),
	}

	plan := sync.Stitch(txs, brWallet, brBaseTime.Add(sync.BridgeMatchWindow+time.Hour))

	assert.Equal(t, sync.StitchNone, plan.Decision(0), "must not guess between two identical candidates")
	assert.Equal(t, sync.StitchNone, plan.Decision(1), "must not guess between two identical candidates")
	assert.Equal(t, sync.StitchNone, plan.Decision(2),
		"the receive is released as a standalone transfer_in — a false positive is worse than a phantom lot")
}

// TestStitch_NoCandidate_ReceiveIsHeldThenReleased: a receive whose source chain
// is not in the wallet's chain set never gets a candidate. Inside the window it
// is held, in case the source leg is simply collected later; past the window it
// must be released as an ordinary transfer_in rather than held forever, or the
// asset is stranded outside the ledger permanently.
func TestStitch_NoCandidate_ReceiveIsHeldThenReleased(t *testing.T) {
	txs := []sync.DecodedTransaction{
		receiveLeg(brBase, "0xrecv", usdc(251, 749084), "USDC", 6, brBaseTime),
	}

	inside := sync.Stitch(txs, brWallet, brBaseTime.Add(time.Hour))
	assert.Equal(t, sync.StitchHold, inside.Decision(0),
		"inside the window the source leg may still be collected, so booking the inflow now "+
			"would consume the raw and leave that send with nothing to match")

	past := sync.Stitch(txs, brWallet, brBaseTime.Add(sync.BridgeMatchWindow+time.Hour))
	assert.Equal(t, sync.StitchNone, past.Decision(0),
		"past the window the inflow is real and unexplained: record it rather than strand it")
}

// TestStitch_OneSendTwoReceives_ConsumesSendOnlyOnce guards the 1:1 invariant
// from the other direction. If a single send could satisfy two receives it would
// be stitched twice, conjuring value out of nothing.
func TestStitch_OneSendTwoReceives_ConsumesSendOnlyOnce(t *testing.T) {
	txs := []sync.DecodedTransaction{
		sendLeg(brArbitrum, "0xsend", usdc(500, 0), "USDC", 6, brBaseTime),
		receiveLeg(brBase, "0xrecvA", usdc(499, 900000), "USDC", 6, brBaseTime.Add(time.Minute)),
		receiveLeg(brBase, "0xrecvB", usdc(499, 900000), "USDC", 6, brBaseTime.Add(2*time.Minute)),
	}

	plan := sync.Stitch(txs, brWallet, brBaseTime.Add(2*time.Hour))

	suppressed := 0
	for i := 1; i <= 2; i++ {
		if plan.Decision(i) == sync.StitchSuppress {
			suppressed++
		}
	}
	assert.Equal(t, 1, suppressed,
		"one send can satisfy at most one receive; stitching it twice would fabricate value")
	assert.Equal(t, sync.StitchAsSource, plan.Decision(0))
	assert.Equal(t, sync.StitchSuppress, plan.Decision(1),
		"the OLDEST competing receive claims the send, so the outcome is reproducible on replay")
}

// TestStitch_DifferentAsset_NotStitched: a bridge moves the same asset. Value
// out in USDC and back in ETH is a swap, and a swap IS a disposal — erasing it
// would hide real PnL.
func TestStitch_DifferentAsset_NotStitched(t *testing.T) {
	txs := []sync.DecodedTransaction{
		sendLeg(brArbitrum, "0xsend", usdc(1000, 0), "USDC", 6, brBaseTime),
		receiveLeg(brBase, "0xrecv", big.NewInt(3e17), "ETH", 18, brBaseTime.Add(time.Minute)),
	}

	plan := sync.Stitch(txs, brWallet, brBaseTime.Add(sync.BridgeMatchWindow+2*time.Hour))

	assert.NotEqual(t, sync.StitchAsSource, plan.Decision(0))
	assert.Equal(t, sync.StitchNone, plan.Decision(1))
}

// TestStitch_SameChain_NotStitched: two same-chain transactions are not a
// bridge. Stitching them would collapse two real movements into one.
func TestStitch_SameChain_NotStitched(t *testing.T) {
	txs := []sync.DecodedTransaction{
		sendLeg(brBase, "0xsend", usdc(1000, 0), "USDC", 6, brBaseTime),
		receiveLeg(brBase, "0xrecv", usdc(1000, 0), "USDC", 6, brBaseTime.Add(time.Minute)),
	}

	plan := sync.Stitch(txs, brWallet, brBaseTime.Add(sync.BridgeMatchWindow+2*time.Hour))

	assert.NotEqual(t, sync.StitchAsSource, plan.Decision(0), "a bridge crosses chains by definition")
	assert.Equal(t, sync.StitchNone, plan.Decision(1))
}

// TestStitch_ReceiveBeforeSend_NotStitched: causality. A receive that predates
// its supposed source cannot have come from it.
func TestStitch_ReceiveBeforeSend_NotStitched(t *testing.T) {
	txs := []sync.DecodedTransaction{
		sendLeg(brArbitrum, "0xsend", usdc(1000, 0), "USDC", 6, brBaseTime.Add(time.Hour)),
		receiveLeg(brBase, "0xrecv", usdc(1000, 0), "USDC", 6, brBaseTime),
	}

	plan := sync.Stitch(txs, brWallet, brBaseTime.Add(4*time.Hour))

	assert.NotEqual(t, sync.StitchAsSource, plan.Decision(0), "the destination cannot precede the source")
}

// TestStitch_OutsideTimeWindow_NotStitched: two legs that would match on asset
// and amount but are separated by more than the window are not credibly one
// bridge. The send, long past its own window, has already been released as a
// transfer_out — so the late receive must not retroactively claim it.
func TestStitch_OutsideTimeWindow_NotStitched(t *testing.T) {
	lateArrival := brBaseTime.Add(sync.BridgeMatchWindow + time.Hour)
	txs := []sync.DecodedTransaction{
		sendLeg(brArbitrum, "0xsend", usdc(1000, 0), "USDC", 6, brBaseTime),
		receiveLeg(brBase, "0xrecv", usdc(1000, 0), "USDC", 6, lateArrival),
	}

	// Far enough past BOTH legs that neither is still held, so this asserts
	// their final resting state rather than an in-flight one.
	plan := sync.Stitch(txs, brWallet, lateArrival.Add(sync.BridgeMatchWindow+time.Hour))

	assert.Equal(t, sync.StitchNone, plan.Decision(0),
		"the send aged out to transfer_out long before this receive appeared")
	assert.Equal(t, sync.StitchNone, plan.Decision(1),
		"and the receive is its own standalone transfer_in")
}

// -----------------------------------------------------------------------------
// AC3 — cross-cycle straggler: held, then aged out to transfer_out
// -----------------------------------------------------------------------------

// TestStitch_StragglerSend_HeldInsideWindow is the hold-don't-reverse rule. The
// receive has not been collected yet, so the send must record NOTHING — no
// ledger transaction and above all no disposal, which an arriving receive would
// force us to reverse.
func TestStitch_StragglerSend_HeldInsideWindow(t *testing.T) {
	txs := []sync.DecodedTransaction{
		sendLeg(brBase, "0xba77f4a7", usdc(279, 158283), "USDC", 6, brBaseTime),
	}

	plan := sync.Stitch(txs, brWallet, brBaseTime.Add(time.Hour))

	assert.Equal(t, sync.StitchHold, plan.Decision(0),
		"inside the window the send is held unprocessed: realizing a disposal now risks having to undo it")
}

// TestStitch_StragglerSend_AgesOutToTransferOut: once the window closes the
// receive can no longer arrive, so the disposal is safe to realize and the leg
// is finalized as a plain transfer_out. Held forever would strand the asset.
func TestStitch_StragglerSend_AgesOutToTransferOut(t *testing.T) {
	txs := []sync.DecodedTransaction{
		sendLeg(brBase, "0xba77f4a7", usdc(279, 158283), "USDC", 6, brBaseTime),
	}

	plan := sync.Stitch(txs, brWallet, brBaseTime.Add(sync.BridgeMatchWindow+time.Minute))

	assert.Equal(t, sync.StitchNone, plan.Decision(0),
		"past the window the send is released to ordinary processing → transfer_out; "+
			"holding forever would strand the asset outside the ledger")
}

// TestStitch_StragglerHeldThenMatchesNextCycle simulates the real cross-cycle
// sequence: cycle 1 collects only the send (held), cycle 2 collects the receive
// too and the pair stitches. Because the decision is a pure function of the raws
// present, no state has to survive between the cycles.
func TestStitch_StragglerHeldThenMatchesNextCycle(t *testing.T) {
	send := sendLeg(brBase, "0xba77f4a7", usdc(279, 158283), "USDC", 6, brBaseTime)
	recv := receiveLeg(brArbitrum, "0x5e6bd538", usdc(279, 158283), "USDC", 6, brBaseTime.Add(23*time.Minute))

	// Cycle 1: only the send has been collected.
	cycle1 := sync.Stitch([]sync.DecodedTransaction{send}, brWallet, brBaseTime.Add(5*time.Minute))
	require.Equal(t, sync.StitchHold, cycle1.Decision(0), "no receive yet: hold, record nothing")

	// Cycle 2: the receive has arrived. The still-pending send now matches.
	cycle2 := sync.Stitch([]sync.DecodedTransaction{send, recv}, brWallet, brBaseTime.Add(30*time.Minute))
	assert.Equal(t, sync.StitchAsSource, cycle2.Decision(0),
		"the held send stitches once its counterpart arrives — never having realized a disposal")
	assert.Equal(t, brArbitrum, cycle2.DestinationChain(0))
	assert.Equal(t, sync.StitchSuppress, cycle2.Decision(1))
}

// TestStitch_HeldSendIsNotRecorded proves the hold has teeth at the pipeline
// level: a held raw must leave the ledger completely untouched and stay PENDING
// so a later cycle can reconsider it.
func TestStitch_HeldSendIsNotRecorded(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, brWallet)

	send := sendLeg(brBase, "0xba77f4a7", usdc(279, 158283), "USDC", 6, time.Now().UTC().Add(-time.Minute))

	env := newStitchPipelineEnv(t, userID, w, []sync.DecodedTransaction{send})
	require.NoError(t, env.processor.ProcessAll(ctx, w))

	assert.Empty(t, env.ledgerSvc.recordedTransactions,
		"a held bridge send must record NOTHING — no ledger transaction and therefore no disposal")
	assert.Empty(t, env.rawRepo.skipped, "a held raw must not be skipped; it has to stay pending")
	assert.Empty(t, env.rawRepo.processed, "a held raw must not be marked processed")
	assert.Empty(t, env.rawRepo.errored, "holding is a normal outcome, not an error")
}

// -----------------------------------------------------------------------------
// AC4 — round-trip (bridge-as-swap) → local swap, never stitched
// -----------------------------------------------------------------------------

// TestStitch_RoundTripBridge_NeverStitched uses the real bridge-as-swap found in
// the calibration data: 279.158283 USDC out, 0.00362749 cbBTC back in the SAME
// transaction. That is a disposal, and stitching it would erase real PnL.
func TestStitch_RoundTripBridge_NeverStitched(t *testing.T) {
	roundTrip := withInbound(
		sendLeg(brBase, "0x9d6ebe10", usdc(279, 158283), "USDC", 6, brBaseTime),
		"cbBTC", 8, big.NewInt(362749),
	)
	// A receive that would otherwise match perfectly on asset, amount and timing.
	recv := receiveLeg(brArbitrum, "0x5e6bd538", usdc(279, 158283), "USDC", 6, brBaseTime.Add(time.Minute))

	plan := sync.Stitch([]sync.DecodedTransaction{roundTrip, recv}, brWallet, brBaseTime.Add(sync.BridgeMatchWindow+2*time.Hour))

	assert.Equal(t, sync.StitchNone, plan.Decision(0),
		"a round-trip is a swap: it must be classified locally, never stitched and never held")
	assert.Equal(t, sync.StitchNone, plan.Decision(1),
		"and it must not consume the unrelated receive either")
}

// TestStitch_RoundTripBridge_ClassifiesAsSwap confirms the consequence of NOT
// stitching: the local classifier sees both directions and books a swap.
func TestStitch_RoundTripBridge_ClassifiesAsSwap(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, brWallet)

	roundTrip := withInbound(
		sendLeg(brBase, "0x9d6ebe10", usdc(279, 158283), "USDC", 6, time.Now().UTC().Add(-time.Minute)),
		"cbBTC", 8, big.NewInt(362749),
	)

	env := newStitchPipelineEnv(t, userID, w, []sync.DecodedTransaction{roundTrip})
	env.ledgerSvc.On("RecordTransaction", mock.Anything, ledger.TxTypeSwap, "noves",
		mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil).Once()

	require.NoError(t, env.processor.ProcessAll(ctx, w))

	require.Len(t, env.ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, ledger.TxTypeSwap, env.ledgerSvc.recordedTransactions[0].TxType,
		"asset out and a different asset back in one transaction is a swap — a real disposal")
}

// TestStitch_PureSendWithDustRefund_StillStitches is the rule the real data
// forced. Most genuine pure-sends carry a same-transaction refund of the very
// asset being sent (observed down to 1e-6 of it). A naive "received[] is empty"
// test would refuse to stitch the majority of real bridges.
func TestStitch_PureSendWithDustRefund_StillStitches(t *testing.T) {
	// Real shape: 0.008915068809592452 ETH out, 8.809592452e-9 ETH refunded back.
	sent := big.NewInt(8_915_068_809_592_452)
	refund := big.NewInt(8_809_592_452)
	net := new(big.Int).Sub(sent, refund)

	send := withInbound(
		sendLeg(brArbitrum, "0x10fafc6d", sent, "ETH", 18, brBaseTime),
		"ETH", 18, refund,
	)
	recv := receiveLeg(brBase, "0xrecv", net, "ETH", 18, brBaseTime.Add(time.Minute))

	plan := sync.Stitch([]sync.DecodedTransaction{send, recv}, brWallet, brBaseTime.Add(2*time.Hour))

	assert.Equal(t, sync.StitchAsSource, plan.Decision(0),
		"a same-asset dust refund is part of the send, not a swap — it must not block stitching")
	assert.Equal(t, sync.StitchSuppress, plan.Decision(1))
}

// TestStitch_RefundIsNettedOffTheSentAmount: the refund reduces what actually
// left, and the destination delivers that net amount. Comparing the gross would
// make a real pair look short by the refund.
func TestStitch_RefundIsNettedOffTheSentAmount(t *testing.T) {
	// A refund large enough that gross-vs-net decides the match: 5% refunded, so
	// the receive equals the net exactly but is 5% below the gross — outside the
	// 1% tolerance if netting were skipped.
	sent := usdc(1000, 0)
	refund := usdc(50, 0)
	net := usdc(950, 0)

	send := withInbound(sendLeg(brArbitrum, "0xsend", sent, "USDC", 6, brBaseTime), "USDC", 6, refund)
	recv := receiveLeg(brBase, "0xrecv", net, "USDC", 6, brBaseTime.Add(time.Minute))

	plan := sync.Stitch([]sync.DecodedTransaction{send, recv}, brWallet, brBaseTime.Add(2*time.Hour))

	assert.Equal(t, sync.StitchAsSource, plan.Decision(0),
		"the refund never left the wallet, so the match must compare the NET amount")
}

// -----------------------------------------------------------------------------
// AC5 — the stitch decision is deterministic across replay/wipe
// -----------------------------------------------------------------------------

// TestStitch_IsDeterministicAcrossReplay: a wipe re-pends the raws and re-runs
// the pipeline. If stitching re-derived a different answer, the replayed ledger
// would disagree with the original about whether a disposal ever happened.
// Input order is varied deliberately — nothing may depend on it.
func TestStitch_IsDeterministicAcrossReplay(t *testing.T) {
	send := sendLeg(brArbitrum, "0xa8333fc5", usdc(24, 446762), "USDC", 6, brBaseTime)
	recv := receiveLeg(brBase, "0x61749a8f", usdc(24, 441577), "USDC", 6, brBaseTime.Add(2*time.Second))
	other := sendLeg(brBase, "0xdbd8db37", usdc(2800, 0), "USDC", 6, brBaseTime.Add(time.Hour))
	now := brBaseTime.Add(3 * time.Hour)

	forward := sync.Stitch([]sync.DecodedTransaction{send, recv, other}, brWallet, now)

	// Same set, different order — as a replay from the DB may well deliver it.
	reversed := sync.Stitch([]sync.DecodedTransaction{other, recv, send}, brWallet, now)

	assert.Equal(t, forward.Decision(0), reversed.Decision(2), "the send's fate must not depend on input order")
	assert.Equal(t, forward.Decision(1), reversed.Decision(1))
	assert.Equal(t, forward.Decision(2), reversed.Decision(0))
	assert.Equal(t, forward.DestinationChain(0), reversed.DestinationChain(2))

	// And repeating the identical call is stable.
	again := sync.Stitch([]sync.DecodedTransaction{send, recv, other}, brWallet, now)
	assert.Equal(t, forward.Decisions, again.Decisions, "the stitcher holds no state between calls")
	assert.Equal(t, forward.DestChain, again.DestChain)
}

// TestStitch_TiedTimestamps_ResolveDeterministically: two receives sharing a
// timestamp must still resolve the same way every run, or a replay could hand
// the send to the other one and produce a different ledger.
func TestStitch_TiedTimestamps_ResolveDeterministically(t *testing.T) {
	at := brBaseTime.Add(time.Minute)
	txs := []sync.DecodedTransaction{
		sendLeg(brArbitrum, "0xsend", usdc(500, 0), "USDC", 6, brBaseTime),
		receiveLeg(brBase, "0xrecvA", usdc(499, 900000), "USDC", 6, at),
		receiveLeg(brBase, "0xrecvB", usdc(499, 900000), "USDC", 6, at),
	}

	first := sync.Stitch(txs, brWallet, brBaseTime.Add(2*time.Hour))
	for i := 0; i < 5; i++ {
		repeat := sync.Stitch(txs, brWallet, brBaseTime.Add(2*time.Hour))
		require.Equal(t, first.Decisions, repeat.Decisions,
			"tied timestamps must break the same way every time, or replay diverges")
	}
}

// -----------------------------------------------------------------------------
// Non-bridge traffic is untouched
// -----------------------------------------------------------------------------

// TestStitch_OrdinaryTransfersUnaffected: the stitcher keys on the provider's
// own bridge classification. An ordinary send and receive of the same asset and
// amount across two chains — the user selling on one chain and buying on
// another — must NOT be silently merged into a phantom internal transfer.
func TestStitch_OrdinaryTransfersUnaffected(t *testing.T) {
	send := sendLeg(brArbitrum, "0xsend", usdc(1000, 0), "USDC", 6, brBaseTime)
	send.ProviderType = "sendToken"
	send.Acts = []string{"sendToken"}

	recv := receiveLeg(brBase, "0xrecv", usdc(1000, 0), "USDC", 6, brBaseTime.Add(time.Minute))
	recv.ProviderType = "receiveToken"
	recv.Acts = []string{"receiveToken"}

	plan := sync.Stitch([]sync.DecodedTransaction{send, recv}, brWallet, brBaseTime.Add(2*time.Hour))

	assert.Equal(t, sync.StitchNone, plan.Decision(0), "only provider-identified bridge legs are stitch candidates")
	assert.Equal(t, sync.StitchNone, plan.Decision(1))
}

// TestStitch_FailedBridgeLeg_NotStitched: a failed transaction moved nothing.
func TestStitch_FailedBridgeLeg_NotStitched(t *testing.T) {
	send := sendLeg(brArbitrum, "0xsend", usdc(1000, 0), "USDC", 6, brBaseTime)
	send.Status = "failed"
	recv := receiveLeg(brBase, "0xrecv", usdc(1000, 0), "USDC", 6, brBaseTime.Add(time.Minute))

	plan := sync.Stitch([]sync.DecodedTransaction{send, recv}, brWallet, brBaseTime.Add(sync.BridgeMatchWindow+2*time.Hour))

	assert.Equal(t, sync.StitchNone, plan.Decision(0), "a failed send moved nothing and must not be held or stitched")
	assert.Equal(t, sync.StitchNone, plan.Decision(1))
}

// TestStitch_ReceiveToAnotherAddress_NotATrigger: the receive leg is only a
// trigger when it lands on the user's OWN wallet. That self-signal is the entire
// basis for receive-triggered matching.
func TestStitch_ReceiveToAnotherAddress_NotATrigger(t *testing.T) {
	send := sendLeg(brArbitrum, "0xsend", usdc(1000, 0), "USDC", 6, brBaseTime)
	recv := receiveLeg(brBase, "0xrecv", usdc(1000, 0), "USDC", 6, brBaseTime.Add(time.Minute))
	recv.Transfers[0].Recipient = "0x1111111111111111111111111111111111111111"

	plan := sync.Stitch([]sync.DecodedTransaction{send, recv}, brWallet, brBaseTime.Add(2*time.Hour))

	assert.NotEqual(t, sync.StitchAsSource, plan.Decision(0),
		"a bridge receive to somebody else's address proves nothing about our funds")
}

// -----------------------------------------------------------------------------
// AC6 — demoable end to end through the pipeline
// -----------------------------------------------------------------------------

// TestStitch_EndToEndPipeline_OneInternalTransferForTheBridge runs the real
// Processor over a wallet whose pending raws are the two legs of one real
// bridge, and asserts the outcome that matters: exactly ONE ledger transaction,
// of type internal_transfer, spanning the two chains — not a transfer_out plus a
// transfer_in.
func TestStitch_EndToEndPipeline_OneInternalTransferForTheBridge(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, brWallet)

	at := time.Now().UTC().Add(-2 * time.Hour)
	send := sendLeg(brBase, "0xba77f4a7", usdc(279, 158283), "USDC", 6, at)
	recv := receiveLeg(brArbitrum, "0x5e6bd538", usdc(279, 158283), "USDC", 6, at.Add(23*time.Minute))

	env := newStitchPipelineEnv(t, userID, w, []sync.DecodedTransaction{send, recv})
	env.ledgerSvc.On("RecordTransaction", mock.Anything, ledger.TxTypeInternalTransfer, "noves",
		mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil).Once()

	require.NoError(t, env.processor.ProcessAll(ctx, w))

	require.Len(t, env.ledgerSvc.recordedTransactions, 1,
		"one bridge is one economic event: exactly one ledger transaction, never a disposal plus an acquisition")
	rec := env.ledgerSvc.recordedTransactions[0]
	assert.Equal(t, ledger.TxTypeInternalTransfer, rec.TxType)
	assert.Equal(t, brBase, rec.RawData["source_chain_id"])
	assert.Equal(t, brArbitrum, rec.RawData["dest_chain_id"])

	assert.Len(t, env.rawRepo.skipped, 1, "the receive leg is absorbed, not recorded separately")
	assert.Len(t, env.rawRepo.processed, 1, "the send leg carries the stitched transaction")
}

// TestStitch_EndToEndPipeline_AgedOutSendBecomesTransferOut is the other half of
// the demo: a straggler nobody ever matched is finalized as an ordinary
// transfer_out once the window closes, so the asset is never stranded.
func TestStitch_EndToEndPipeline_AgedOutSendBecomesTransferOut(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, brWallet)

	old := time.Now().UTC().Add(-sync.BridgeMatchWindow - time.Hour)
	send := sendLeg(brBase, "0xdbd8db37", usdc(2800, 0), "USDC", 6, old)

	env := newStitchPipelineEnv(t, userID, w, []sync.DecodedTransaction{send})
	env.ledgerSvc.On("RecordTransaction", mock.Anything, ledger.TxTypeTransferOut, "noves",
		mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil).Once()

	require.NoError(t, env.processor.ProcessAll(ctx, w))

	require.Len(t, env.ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, ledger.TxTypeTransferOut, env.ledgerSvc.recordedTransactions[0].TxType,
		"past the window the receive can no longer arrive, so the disposal is finally safe to realize")
}

// =============================================================================
// Pipeline test environment
// =============================================================================

// stitchRawRepo is a RawTransactionRepository that records the terminal state of
// each raw, so a test can assert what the processor did with it — which is how
// "held" (still pending) is distinguished from "skipped" and "processed".
type stitchRawRepo struct {
	pending   []*sync.RawTransaction
	processed map[uuid.UUID]uuid.UUID
	skipped   map[uuid.UUID]string
	errored   map[uuid.UUID]string
}

func newStitchRawRepo(raws []*sync.RawTransaction) *stitchRawRepo {
	return &stitchRawRepo{
		pending:   raws,
		processed: make(map[uuid.UUID]uuid.UUID),
		skipped:   make(map[uuid.UUID]string),
		errored:   make(map[uuid.UUID]string),
	}
}

func (r *stitchRawRepo) UpsertRawTransaction(context.Context, *sync.RawTransaction) error { return nil }

func (r *stitchRawRepo) GetPendingByWallet(context.Context, uuid.UUID) ([]*sync.RawTransaction, error) {
	return r.pending, nil
}

func (r *stitchRawRepo) GetAllByWallet(context.Context, uuid.UUID) ([]*sync.RawTransaction, error) {
	return r.pending, nil
}

func (r *stitchRawRepo) MarkProcessed(_ context.Context, rawID, ledgerTxID uuid.UUID) error {
	r.processed[rawID] = ledgerTxID
	return nil
}

func (r *stitchRawRepo) MarkSkipped(_ context.Context, rawID uuid.UUID, reason string) error {
	r.skipped[rawID] = reason
	return nil
}

func (r *stitchRawRepo) MarkError(_ context.Context, rawID uuid.UUID, msg string) error {
	r.errored[rawID] = msg
	return nil
}

func (r *stitchRawRepo) ResetProcessingStatus(context.Context, uuid.UUID) error { return nil }

var _ sync.RawTransactionRepository = (*stitchRawRepo)(nil)

type stitchEnv struct {
	processor *sync.Processor
	ledgerSvc *MockLedgerService
	rawRepo   *stitchRawRepo
}

// newStitchPipelineEnv wires a real Processor and TxBuilder over fake storage,
// with the given decoded transactions already collected as pending raws — the
// exact state the collect phase leaves behind.
func newStitchPipelineEnv(t *testing.T, userID uuid.UUID, w *wallet.Wallet, txs []sync.DecodedTransaction) *stitchEnv {
	t.Helper()

	raws := make([]*sync.RawTransaction, len(txs))
	for i, tx := range txs {
		payload, err := json.Marshal(tx)
		require.NoError(t, err)
		raws[i] = &sync.RawTransaction{
			ID:               uuid.New(),
			WalletID:         w.ID,
			ExternalID:       tx.ID,
			TxHash:           tx.TxHash,
			ChainID:          tx.ChainID,
			OperationType:    string(tx.OperationType),
			MinedAt:          tx.MinedAt,
			Status:           tx.Status,
			RawJSON:          payload,
			ProcessingStatus: sync.ProcessingStatusPending,
		}
	}

	walletRepo := new(MockWalletRepository)
	walletRepo.On("SetSyncPhase", mock.Anything, w.ID, mock.Anything).Return(nil).Maybe()
	walletRepo.On("SetSyncCompletedAt", mock.Anything, w.ID, mock.Anything).Return(nil).Maybe()
	walletRepo.On("GetWalletsByAddressAndUserID", mock.Anything, mock.Anything, mock.Anything).
		Return([]*wallet.Wallet{}, nil).Maybe()

	ledgerSvc := new(MockLedgerService)
	log := logger.New("test", os.Stdout)
	rawRepo := newStitchRawRepo(raws)

	builder := sync.NewTxBuilder(walletRepo, ledgerSvc, nil, nil, log, nil, nil)

	return &stitchEnv{
		processor: sync.NewProcessor(rawRepo, walletRepo, builder, log),
		ledgerSvc: ledgerSvc,
		rawRepo:   rawRepo,
	}
}

// TestStitch_AgedOutPureSendWithRefund_IsTransferOutNotSwap closes the gap the
// round-trip classifier rule opens. A genuine pure-send that carries a
// same-asset dust refund does move value both ways, but it is not a trade:
// nothing was exchanged, the wallet simply got a sliver of its own asset back.
// Booking it as a swap would invent a disposal of an asset against itself.
func TestStitch_AgedOutPureSendWithRefund_IsTransferOutNotSwap(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, brWallet)

	old := time.Now().UTC().Add(-sync.BridgeMatchWindow - time.Hour)
	send := withInbound(
		sendLeg(brArbitrum, "0x10fafc6d", big.NewInt(8_915_068_809_592_452), "ETH", 18, old),
		"ETH", 18, big.NewInt(8_809_592_452),
	)

	env := newStitchPipelineEnv(t, userID, w, []sync.DecodedTransaction{send})
	env.ledgerSvc.On("RecordTransaction", mock.Anything, ledger.TxTypeTransferOut, "noves",
		mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil).Once()

	require.NoError(t, env.processor.ProcessAll(ctx, w))

	require.Len(t, env.ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, ledger.TxTypeTransferOut, env.ledgerSvc.recordedTransactions[0].TxType,
		"getting a dust refund of the asset you sent is not a trade — booking it as a swap "+
			"would fabricate a disposal of the asset against itself")
}

// -----------------------------------------------------------------------------
// Regression: the ledger must record the SAME amount the stitcher matched on
// -----------------------------------------------------------------------------

// TestStitch_LedgerAmountIsTheNetMatchedAmount guards the seam between the two
// halves of stitching. The matcher decides a pair belongs together by comparing
// the NET amount that left the wallet — gross minus any same-transaction refund.
// If the writer then records a different number, the destination lot opens at a
// quantity that never arrived and the source is credited a quantity that never
// left. Double-entry still balances (the same wrong figure is used for both
// legs), so nothing downstream catches it: it surfaces only as reconciliation
// drift and a silently wrong cost basis.
func TestStitch_LedgerAmountIsTheNetMatchedAmount(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, brWallet)

	at := time.Now().UTC().Add(-2 * time.Hour)
	// 1000 USDC out, 50 refunded in the same tx → 950 actually left, and 950 is
	// what the destination chain delivers.
	send := withInbound(
		sendLeg(brBase, "0xsend", usdc(1000, 0), "USDC", 6, at),
		"USDC", 6, usdc(50, 0),
	)
	recv := receiveLeg(brArbitrum, "0xrecv", usdc(950, 0), "USDC", 6, at.Add(time.Minute))

	env := newStitchPipelineEnv(t, userID, w, []sync.DecodedTransaction{send, recv})
	env.ledgerSvc.On("RecordTransaction", mock.Anything, ledger.TxTypeInternalTransfer, "noves",
		mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil).Once()

	require.NoError(t, env.processor.ProcessAll(ctx, w))

	require.Len(t, env.ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, usdc(950, 0).String(), env.ledgerSvc.recordedTransactions[0].RawData["amount"],
		"the ledger must record the NET amount the matcher used; the refund never left the wallet, "+
			"so recording the gross opens a destination lot for value that never arrived")
}

// TestStitch_LedgerAmountSumsSplitOutflows: a send leg that moves the asset out
// in several transfers must be recorded as their SUM. Recording only the first
// would understate the movement and strand the remainder.
func TestStitch_LedgerAmountSumsSplitOutflows(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, brWallet)

	at := time.Now().UTC().Add(-2 * time.Hour)
	send := sendLeg(brBase, "0xsend", usdc(300, 0), "USDC", 6, at)
	send.Transfers = append(send.Transfers, sync.DecodedTransfer{
		AssetSymbol: "USDC", Decimals: 6, Amount: usdc(700, 0),
		Direction: sync.DirectionOut, Sender: brWallet, Recipient: brBridgeCtr,
	})
	recv := receiveLeg(brArbitrum, "0xrecv", usdc(1000, 0), "USDC", 6, at.Add(time.Minute))

	env := newStitchPipelineEnv(t, userID, w, []sync.DecodedTransaction{send, recv})
	env.ledgerSvc.On("RecordTransaction", mock.Anything, ledger.TxTypeInternalTransfer, "noves",
		mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil).Once()

	require.NoError(t, env.processor.ProcessAll(ctx, w))

	require.Len(t, env.ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, usdc(1000, 0).String(), env.ledgerSvc.recordedTransactions[0].RawData["amount"],
		"a split outflow must be recorded as its sum — the matcher matched on the total")
}

// -----------------------------------------------------------------------------
// Regression: the hold must be symmetric — a receive can arrive FIRST
// -----------------------------------------------------------------------------

// TestStitch_ReceiveArrivingFirst_IsHeldNotBooked is the mirror of the straggler
// case, and chains are collected independently (#28/#29) so it is ordinary, not
// exotic: the destination chain can easily be collected before the source.
//
// If the receive is booked immediately as transfer_in it is marked processed and
// stops being pending, so the send arriving next cycle can never match it — and
// ages out to transfer_out. The result is transfer_in + transfer_out: precisely
// the fabricated disposal plus reset cost basis this ticket exists to prevent.
func TestStitch_ReceiveArrivingFirst_IsHeldNotBooked(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, brWallet)

	recv := receiveLeg(brArbitrum, "0x5e6bd538", usdc(279, 158283), "USDC", 6,
		time.Now().UTC().Add(-time.Minute))

	env := newStitchPipelineEnv(t, userID, w, []sync.DecodedTransaction{recv})
	require.NoError(t, env.processor.ProcessAll(ctx, w))

	assert.Empty(t, env.ledgerSvc.recordedTransactions,
		"a fresh bridge receive whose source leg has not been collected yet must be HELD, "+
			"not booked as transfer_in — booking it consumes the raw and leaves the later "+
			"send with nothing to match, fabricating a disposal")
	assert.Empty(t, env.rawRepo.processed, "a held receive must stay pending for the next cycle")
	assert.Empty(t, env.rawRepo.skipped, "a held receive must stay pending for the next cycle")
}

// TestStitch_ReceiveHeldThenSendArrives_Stitches completes the cross-cycle story
// in the receive-first direction: cycle 1 holds the lone receive, cycle 2 sees
// the send too and stitches the pair — with no disposal ever realized.
func TestStitch_ReceiveHeldThenSendArrives_Stitches(t *testing.T) {
	send := sendLeg(brBase, "0xba77f4a7", usdc(279, 158283), "USDC", 6, brBaseTime)
	recv := receiveLeg(brArbitrum, "0x5e6bd538", usdc(279, 158283), "USDC", 6, brBaseTime.Add(time.Minute))

	// Cycle 1: only the receive has been collected (the destination chain ran first).
	cycle1 := sync.Stitch([]sync.DecodedTransaction{recv}, brWallet, brBaseTime.Add(5*time.Minute))
	require.Equal(t, sync.StitchHold, cycle1.Decision(0),
		"no send collected yet: hold, so the arriving send can still claim it")

	// Cycle 2: the source chain is collected and the pair stitches.
	cycle2 := sync.Stitch([]sync.DecodedTransaction{send, recv}, brWallet, brBaseTime.Add(30*time.Minute))
	assert.Equal(t, sync.StitchAsSource, cycle2.Decision(0))
	assert.Equal(t, brArbitrum, cycle2.DestinationChain(0))
	assert.Equal(t, sync.StitchSuppress, cycle2.Decision(1))
}

// TestStitch_AgedOutReceive_BecomesTransferIn: the receive-side hold must also be
// bounded. Once the window closes the source leg can no longer arrive, so the
// inflow is finalized as an ordinary transfer_in rather than held forever —
// otherwise a bridge from a chain the user never enabled strands the asset
// outside the ledger permanently.
func TestStitch_AgedOutReceive_BecomesTransferIn(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, brWallet)

	old := time.Now().UTC().Add(-sync.BridgeMatchWindow - time.Hour)
	recv := receiveLeg(brBase, "0x283c65a2", usdc(251, 749084), "USDC", 6, old)

	env := newStitchPipelineEnv(t, userID, w, []sync.DecodedTransaction{recv})
	env.ledgerSvc.On("RecordTransaction", mock.Anything, ledger.TxTypeTransferIn, "noves",
		mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil).Once()

	require.NoError(t, env.processor.ProcessAll(ctx, w))

	require.Len(t, env.ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, ledger.TxTypeTransferIn, env.ledgerSvc.recordedTransactions[0].TxType,
		"past the window the source can no longer arrive, so the inflow must be recorded "+
			"rather than held forever")
}
