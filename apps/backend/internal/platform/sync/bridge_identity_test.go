package sync_test

import (
	"context"
	"encoding/json"
	"math/big"
	"os"
	"strings"
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
// Issue #84 / #70 — the arriving asset keeps its own identity.
//
// USDC on Base and USDC on Arbitrum are the same ticker and two different
// contracts, which since #59 makes them two different assets. A bridge moves
// value between them.
//
// The chain was already carried across the stitch; the CONTRACT was not, and it
// exists only on the receive leg — which the stitcher suppresses. So the source
// leg had nothing to identify the arriving asset by, and the handler named it
// with the departing asset's UUID: an account whose chain segment said "base"
// and whose asset was Arbitrum's USDC. On the live database both bridge
// transactions were built that way, and the two halves of the split summed to a
// plausible number, so only a cardinality check found it.
// =============================================================================

const (
	biWallet       = "0x9afcd847c633b820a2f291794d28d374b555811b"
	biBridgeCtr    = "0x89c6340b1a1f4b25d36cd8b063d49045caf3f818"
	biBase         = "base"
	biArbitrum     = "arbitrum"
	biUSDCOnBase   = "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"
	biUSDCOnArb    = "0xaf88d065e77c8cc2239327c5edb3a432268e5831"
	biBridgedValue = 24_446_762 // 24.446762 USDC, 6 decimals
	biArrivedValue = 24_441_577 // the bridge withheld 0.0212%
)

var biAt = time.Date(2026, 2, 2, 10, 0, 0, 0, time.UTC)

// biLeg builds one side of a bridge, with a REAL per-chain contract — which is
// the half the defect dropped.
func biLeg(chain, hash, contract string, amount int64, dir sync.TransferDirection, at time.Time) sync.DecodedTransaction {
	providerType, opType := "sendToBridge", sync.OpSend
	sender, recipient := biWallet, biBridgeCtr
	if dir == sync.DirectionIn {
		providerType, opType = "receiveFromBridge", sync.OpReceive
		sender, recipient = biBridgeCtr, biWallet
	}
	return sync.DecodedTransaction{
		ID:            chain + ":" + hash,
		TxHash:        hash,
		ChainID:       chain,
		OperationType: opType,
		ProviderType:  providerType,
		Acts:          []string{providerType, "bridged"},
		Transfers: []sync.DecodedTransfer{{
			AssetSymbol:     "USDC",
			Decimals:        6,
			Amount:          big.NewInt(amount),
			ContractAddress: contract,
			Direction:       dir,
			Sender:          sender,
			Recipient:       recipient,
		}},
		MinedAt: at,
		Status:  "confirmed",
	}
}

// -----------------------------------------------------------------------------
// The stitch plan must carry the arriving asset, not only the arriving chain
// -----------------------------------------------------------------------------

// TestStitch_PlanCarriesTheArrivingAssetsContract: the receive leg is about to be
// suppressed, so this is the last moment its contract exists. Dropping it here is
// what left the writer with nothing but the departing asset's UUID.
func TestStitch_PlanCarriesTheArrivingAssetsContract(t *testing.T) {
	send := biLeg(biArbitrum, "0xsend", biUSDCOnArb, biBridgedValue, sync.DirectionOut, biAt)
	recv := biLeg(biBase, "0xrecv", biUSDCOnBase, biArrivedValue, sync.DirectionIn, biAt.Add(2*time.Second))

	plan := sync.Stitch([]sync.DecodedTransaction{send, recv}, biWallet, biAt.Add(time.Hour))

	require.Equal(t, sync.StitchAsSource, plan.Decision(0))
	require.Equal(t, sync.StitchSuppress, plan.Decision(1))

	arriving, ok := plan.DestinationAsset(0)
	require.True(t, ok, "a stitched source must name the asset it receives")
	assert.Equal(t, biUSDCOnBase, arriving.Contract,
		"the arriving contract comes from the RECEIVE leg — the send leg cannot know it, and after "+
			"suppression nothing else remembers it")
	assert.NotEqual(t, biUSDCOnArb, arriving.Contract,
		"reusing the departing contract is the #70 shape: one asset addressed by two accounts")
	assert.Equal(t, biBase, plan.DestinationChain(0))
}

// -----------------------------------------------------------------------------
// End to end: raw pair → stitch → ledger data → handler entries
// -----------------------------------------------------------------------------

type biEnv struct {
	processor *sync.Processor
	ledgerSvc *MockLedgerService
	registry  *fakeAssetRegistry
}

func newBIEnv(t *testing.T, userID uuid.UUID, w *wallet.Wallet, txs []sync.DecodedTransaction) *biEnv {
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
	registry := newFakeAssetRegistry()

	builder := sync.NewTxBuilder(walletRepo, ledgerSvc, nil, nil, log, nil, registry, nil)

	return &biEnv{
		processor: sync.NewProcessor(newStitchRawRepo(raws), walletRepo, builder, log),
		ledgerSvc: ledgerSvc,
		registry:  registry,
	}
}

// TestBridge_CodeChainMatchesAssetChainOnBothSides is #70's acceptance criterion
// asserted through the production path: raw send + raw receive in, ledger entries
// out, and on each side the chain in the account code is the chain of the asset
// in that code.
func TestBridge_CodeChainMatchesAssetChainOnBothSides(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, biWallet)

	send := biLeg(biArbitrum, "0xsend", biUSDCOnArb, biBridgedValue, sync.DirectionOut, biAt)
	recv := biLeg(biBase, "0xrecv", biUSDCOnBase, biArrivedValue, sync.DirectionIn, biAt.Add(2*time.Second))

	env := newBIEnv(t, userID, w, []sync.DecodedTransaction{send, recv})
	env.ledgerSvc.On("RecordTransaction", mock.Anything, ledger.TxTypeInternalTransfer, "noves",
		mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil).Once()

	require.NoError(t, env.processor.ProcessAll(ctx, w))

	require.Len(t, env.ledgerSvc.recordedTransactions, 1,
		"one bridge is ONE ledger transaction: the receive leg is absorbed, not recorded")
	data := env.ledgerSvc.recordedTransactions[0].RawData

	// The registry minted an id per (chain, contract). Both must reach the data.
	arbUSDC := env.registry.idFor(t, biArbitrum, biUSDCOnArb)
	baseUSDC := env.registry.idFor(t, biBase, biUSDCOnBase)
	require.NotEqual(t, arbUSDC, baseUSDC,
		"two contracts on two chains are two registry rows; a fixture where they collide proves nothing")

	assert.Equal(t, arbUSDC.String(), data["asset_id"], "the departing asset is Arbitrum's USDC")
	assert.Equal(t, baseUSDC.String(), data["dest_asset_id"],
		"the arriving asset must reach the handler. The resolver already computed this id and the "+
			"data assembler used to throw it away, leaving the handler to name the arrival with the "+
			"departing asset's UUID (#70)")

	// Through the real handler to the entries.
	walletRepo := new(MockTransferWalletRepo)
	walletRepo.On("GetByID", mock.Anything, w.ID).Return(w, nil).Maybe()
	handler := transfer.NewInternalTransferHandler(walletRepo, logger.NewDefault("test"))

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)

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

	assert.Equal(t, baseUSDC, debit.AssetID, "the arriving leg books Base's USDC")
	assert.Equal(t, arbUSDC, credit.AssetID, "the departing leg books Arbitrum's USDC")

	// The invariant, stated the way the reconciliation report checks it on the
	// live database: for every entry, the chain segment of the account code and
	// the chain of the asset in that code are the same chain.
	for _, e := range []*ledger.Entry{debit, credit} {
		code := e.Metadata["account_code"].(string)
		chain := e.Metadata["chain_id"].(string)
		assert.True(t, strings.HasSuffix(code, chain+"."+e.AssetID.String()),
			"code %q must end in {chain}.{asset} for its own chain %q; #70 produced a base-segment "+
				"code holding the arbitrum asset and vice versa", code, chain)
	}

	// And the two legs are still one movement, which is what carries the basis
	// now that the assets differ.
	assert.Equal(t, debit.Metadata[ledger.MetaLegPair], credit.Metadata[ledger.MetaLegPair])
	assert.NotEmpty(t, debit.Metadata[ledger.MetaLegPair])
}

// TestBridge_NativeCoin_ArrivingLegIsTheDestinationChainsCoin: bridging the
// native coin is the case that defeats "the gas leg is the native one, so the
// other leg is the transfer". Here EVERY leg is the native coin — and the two
// sides are still two assets, because ETH on Base and ETH on Arbitrum are two
// registry rows.
func TestBridge_NativeCoin_ArrivingLegIsTheDestinationChainsCoin(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, biWallet)

	// No contract on either side: the native coin.
	send := biLeg(biArbitrum, "0xsendeth", "", 1_000_000_000_000_000_000, sync.DirectionOut, biAt)
	send.Transfers[0].AssetSymbol = "ETH"
	send.Transfers[0].Decimals = 18
	recv := biLeg(biBase, "0xrecveth", "", 999_000_000_000_000_000, sync.DirectionIn, biAt.Add(2*time.Second))
	recv.Transfers[0].AssetSymbol = "ETH"
	recv.Transfers[0].Decimals = 18

	env := newBIEnv(t, userID, w, []sync.DecodedTransaction{send, recv})
	env.ledgerSvc.On("RecordTransaction", mock.Anything, ledger.TxTypeInternalTransfer, "noves",
		mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil).Once()

	require.NoError(t, env.processor.ProcessAll(ctx, w))
	require.Len(t, env.ledgerSvc.recordedTransactions, 1)
	data := env.ledgerSvc.recordedTransactions[0].RawData

	arbETH := env.registry.idFor(t, biArbitrum, sync.NativeContract)
	baseETH := env.registry.idFor(t, biBase, sync.NativeContract)
	require.NotEqual(t, arbETH, baseETH,
		"a chain's native coin is still that chain's asset: two chains, two registry rows")

	assert.Equal(t, arbETH.String(), data["asset_id"])
	assert.Equal(t, baseETH.String(), data["dest_asset_id"],
		"the arriving native coin belongs to the destination chain")
}

// TestBridge_SameChainInternalTransfer_NamesNoDestinationAsset: an ordinary
// same-chain internal transfer genuinely moves ONE asset. It must carry no
// destination override at all — the shape every raw written before #84 has.
func TestBridge_SameChainInternalTransfer_NamesNoDestinationAsset(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	srcWallet := newTestWallet(userID, biWallet)
	dstWallet := newTestWallet(userID, biBridgeCtr)

	env := newIdemEnv(t, userID, map[string]*wallet.Wallet{
		biWallet:    srcWallet,
		biBridgeCtr: dstWallet,
	})
	env.ledgerSvc.On("RecordTransaction", mock.Anything, ledger.TxTypeInternalTransfer, "noves",
		mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil).Once()

	// A plain send between two of the user's own wallets on one chain.
	tx := biLeg(biBase, "0xlocal", biUSDCOnBase, biBridgedValue, sync.DirectionOut, biAt)
	tx.ProviderType = ""
	tx.Acts = nil

	_, err := env.builder.ProcessTransaction(ctx, srcWallet, tx)
	require.NoError(t, err)

	require.Len(t, env.ledgerSvc.recordedTransactions, 1)
	data := env.ledgerSvc.recordedTransactions[0].RawData
	assert.NotContains(t, data, "dest_asset_id",
		"one chain, one contract, one asset — a destination override here would invent a second identity")
	assert.NotContains(t, data, "dest_chain_id")
}
