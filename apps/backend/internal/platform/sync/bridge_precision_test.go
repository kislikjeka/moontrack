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
// Issue #86 — precision travels with identity, per leg.
//
// #84 gave the two legs of a bridge two asset identities, which is what they
// are. Precision did not follow: the model carried ONE Decimals for both, so
// the arriving leg was booked with the destination asset's UUID and a quantity
// counted in the SOURCE asset's units.
//
// The scales are equal on today's live bridge (Base USDC and Arbitrum USDC are
// both 6), which is the only reason nothing has been mis-booked yet. The
// registry already holds a Base USDC at 18 decimals beside the 6-decimal one,
// so the difference is one bridge away from being real — and an error of 10^Δ
// leaves both halves internally consistent, so no amount comparison finds it.
// =============================================================================

// bpSeedRegistry pre-creates a registry row at a chosen precision, standing in
// for an asset the registry already knows. The provider's reported decimals are
// only a hint used to CREATE a row; where one exists the registry is the
// authority, and these tests turn on that distinction.
func bpSeedRegistry(reg *fakeAssetRegistry, chain, contract, symbol string, decimals int) uuid.UUID {
	key := sync.NewAssetKey(chain, contract)
	a := &sync.RegistryAsset{
		ID:       uuid.New(),
		Key:      key,
		Symbol:   symbol,
		Name:     symbol,
		Decimals: decimals,
	}
	reg.byKey[key] = a
	return a.ID
}

// rescale restates a base-unit amount from one precision to another. It is the
// arithmetic under test, written out independently here so a fixture is not
// validated by the code it is meant to check.
func rescale(amount *big.Int, from, to int) *big.Int {
	switch {
	case to > from:
		return new(big.Int).Mul(amount, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(to-from)), nil))
	case to < from:
		return new(big.Int).Div(amount, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(from-to)), nil))
	default:
		return new(big.Int).Set(amount)
	}
}

// bpProbeRecorder wraps a knownness registry and remembers every identity the
// filter asked about. The question these tests need answered is not only "was
// the bridge booked" but "was the destination identity ever OFFERED to the
// filter" — a guard that convicts for the wrong reason would pass the first
// check while leaving the hole open.
type bpProbeRecorder struct {
	inner sync.KnownnessRegistry
	keys  []sync.AssetKey
}

func (r *bpProbeRecorder) Get(ctx context.Context, key sync.AssetKey) (*sync.KnownnessRecord, error) {
	r.keys = append(r.keys, key)
	return r.inner.Get(ctx, key)
}

func (r *bpProbeRecorder) Enqueue(ctx context.Context, key sync.AssetKey, symbol string) error {
	return r.inner.Enqueue(ctx, key, symbol)
}

func (r *bpProbeRecorder) probed() []sync.AssetKey { return r.keys }

var _ sync.KnownnessRegistry = (*bpProbeRecorder)(nil)

// newBIEnvFiltered is newBIEnv with the known-asset filter wired, which is the
// only difference that matters here: without a filter the guard is inert by
// design.
func newBIEnvFiltered(
	t *testing.T,
	userID uuid.UUID,
	w *wallet.Wallet,
	txs []sync.DecodedTransaction,
	knownness sync.KnownnessRegistry,
) *biEnv {
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

	builder := sync.NewTxBuilder(walletRepo, ledgerSvc, nil, nil, log, nil, registry,
		sync.NewKnownAssetFilter(knownness))

	return &biEnv{
		processor: sync.NewProcessor(newStitchRawRepo(raws), walletRepo, builder, log),
		ledgerSvc: ledgerSvc,
		registry:  registry,
	}
}

// bpBridge runs a full send+receive pair through the production path and returns
// the raw_data the ledger was handed, with the registry seeded beforehand.
func bpBridge(
	t *testing.T,
	srcChain, srcContract string, srcDecimals int,
	dstChain, dstContract string, dstDecimals int,
	amount int64,
) (map[string]interface{}, uuid.UUID, uuid.UUID, *wallet.Wallet) {
	t.Helper()
	return bpBridgeBig(t, srcChain, srcContract, srcDecimals, dstChain, dstContract, dstDecimals,
		big.NewInt(amount))
}

// bpBridgeBig is bpBridge for quantities that do not fit an int64 — which any
// realistic amount at 18 decimals does not.
func bpBridgeBig(
	t *testing.T,
	srcChain, srcContract string, srcDecimals int,
	dstChain, dstContract string, dstDecimals int,
	amount *big.Int,
) (map[string]interface{}, uuid.UUID, uuid.UUID, *wallet.Wallet) {
	t.Helper()
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, biWallet)

	send := biLeg(srcChain, "0xsend", srcContract, 0, sync.DirectionOut, biAt)
	send.Transfers[0].Decimals = srcDecimals
	send.Transfers[0].Amount = new(big.Int).Set(amount)

	// The receive leg reports the SAME QUANTITY at its own scale — which is what
	// a real bridge emits, and what the matcher compares after aligning the two
	// scales. Handing it the source's integer would be the very confusion under
	// test, and the pair would not even stitch.
	recv := biLeg(dstChain, "0xrecv", dstContract, 0, sync.DirectionIn, biAt.Add(2*time.Second))
	recv.Transfers[0].Decimals = dstDecimals
	recv.Transfers[0].Amount = rescale(amount, srcDecimals, dstDecimals)

	env := newBIEnv(t, userID, w, []sync.DecodedTransaction{send, recv})

	srcID := bpSeedRegistry(env.registry, srcChain, srcContract, "USDC", srcDecimals)
	dstID := bpSeedRegistry(env.registry, dstChain, dstContract, "USDC", dstDecimals)
	require.NotEqual(t, srcID, dstID)

	env.ledgerSvc.On("RecordTransaction", mock.Anything, ledger.TxTypeInternalTransfer, "noves",
		mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil).Once()

	require.NoError(t, env.processor.ProcessAll(ctx, w))
	require.Len(t, env.ledgerSvc.recordedTransactions, 1)

	return env.ledgerSvc.recordedTransactions[0].RawData, srcID, dstID, w
}

// bpEntries runs raw_data through the real handler.
func bpEntries(t *testing.T, data map[string]interface{}, w *wallet.Wallet) []*ledger.Entry {
	t.Helper()
	walletRepo := new(MockTransferWalletRepo)
	walletRepo.On("GetByID", mock.Anything, w.ID).Return(w, nil).Maybe()
	handler := transfer.NewInternalTransferHandler(walletRepo, logger.NewDefault("test"))

	entries, err := handler.Handle(context.Background(), data)
	require.NoError(t, err)
	return entries
}

func bpLeg(t *testing.T, entries []*ledger.Entry, entryType ledger.EntryType, assetID uuid.UUID) *ledger.Entry {
	t.Helper()
	for _, e := range entries {
		if e.EntryType == entryType && e.AssetID == assetID {
			return e
		}
	}
	t.Fatalf("no %s entry for asset %s", entryType, assetID)
	return nil
}

// -----------------------------------------------------------------------------
// The destination precision reaches the handler at all
// -----------------------------------------------------------------------------

// TestBridge_DestDecimalsReachTheHandler: tx.DestDecimals was computed at the
// resolve and dropped by the data assembler — the same shape as #70, one field
// further along. Everything downstream depends on it arriving.
func TestBridge_DestDecimalsReachTheHandler(t *testing.T) {
	data, _, _, _ := bpBridge(t,
		biArbitrum, biUSDCOnArb, 6,
		biBase, biUSDCOnBase, 18,
		24_446_762)

	require.Contains(t, data, "dest_decimals",
		"the resolve already computed the arriving asset's precision; dropping it here leaves the "+
			"handler to book the arrival at the DEPARTING asset's scale (#86)")
	assert.EqualValues(t, 18, data["dest_decimals"])
	assert.EqualValues(t, 6, data["decimals"], "the departing leg keeps its own scale")
}

// -----------------------------------------------------------------------------
// The arriving leg is booked in its own units — both directions
// -----------------------------------------------------------------------------

// TestBridge_6to18_ArrivingLegIsBookedInItsOwnUnits is the ticket's worked
// example: 24.446762 USDC leaving a 6-decimal chain must arrive as 24.446762
// USDC on an 18-decimal one, not as 2.4e-11 of it.
func TestBridge_6to18_ArrivingLegIsBookedInItsOwnUnits(t *testing.T) {
	const sent = int64(24_446_762) // 24.446762 USDC at 6 decimals

	data, srcID, dstID, w := bpBridge(t,
		biArbitrum, biUSDCOnArb, 6,
		biBase, biUSDCOnBase, 18,
		sent)

	entries := bpEntries(t, data, w)

	credit := bpLeg(t, entries, ledger.EntryTypeAssetDecrease, srcID)
	debit := bpLeg(t, entries, ledger.EntryTypeAssetIncrease, dstID)

	assert.Equal(t, big.NewInt(sent), credit.Amount,
		"the departing leg is stated in the departing asset's units, unchanged")

	// 24.446762 restated at 18 decimals.
	want := new(big.Int).Mul(big.NewInt(sent), new(big.Int).Exp(big.NewInt(10), big.NewInt(12), nil))
	assert.Equal(t, want, debit.Amount,
		"the arriving leg is the same QUANTITY at the arriving asset's scale. Carrying the integer "+
			"across instead credits %s base units of an 18-decimal asset — 2.4e-11 USDC (#86)", credit.Amount)

	// Stated as the human quantity, which is the whole point.
	assert.Equal(t,
		new(big.Rat).SetFrac(credit.Amount, new(big.Int).Exp(big.NewInt(10), big.NewInt(6), nil)),
		new(big.Rat).SetFrac(debit.Amount, new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)),
		"both legs must denote the same number of whole USDC")
}

// TestBridge_18to6_ArrivingLegIsBookedInItsOwnUnits is the same bridge run the
// other way, where the restatement narrows and therefore truncates.
func TestBridge_18to6_ArrivingLegIsBookedInItsOwnUnits(t *testing.T) {
	// 24.446762 USDC at 18 decimals. Exceeds int64, so the bridge helper takes
	// the quantity as a big.Int on this path.
	sent := rescale(big.NewInt(24_446_762), 6, 18)

	data, srcID, dstID, w := bpBridgeBig(t,
		biBase, biUSDCOnBase, 18,
		biArbitrum, biUSDCOnArb, 6,
		sent)

	entries := bpEntries(t, data, w)

	credit := bpLeg(t, entries, ledger.EntryTypeAssetDecrease, srcID)
	debit := bpLeg(t, entries, ledger.EntryTypeAssetIncrease, dstID)

	assert.Equal(t, sent, credit.Amount)
	assert.Equal(t, big.NewInt(24_446_762), debit.Amount,
		"narrowing precision restates the quantity and truncates what the destination chain cannot "+
			"represent; it does not carry the 18-decimal integer onto a 6-decimal asset")
}

// TestBridge_EqualDecimals_KeepsTheTwoEntryShape guards the case that is
// everything in production today. Where the scales agree there is nothing to
// restate, and the transfer must look exactly as it did before this fix — two
// entries, no transit account.
func TestBridge_EqualDecimals_KeepsTheTwoEntryShape(t *testing.T) {
	const sent = int64(24_446_762)

	data, srcID, dstID, w := bpBridge(t,
		biArbitrum, biUSDCOnArb, 6,
		biBase, biUSDCOnBase, 6,
		sent)

	entries := bpEntries(t, data, w)
	require.Len(t, entries, 2, "equal scales need no clearing pair")

	assert.Equal(t, big.NewInt(sent), bpLeg(t, entries, ledger.EntryTypeAssetDecrease, srcID).Amount)
	assert.Equal(t, big.NewInt(sent), bpLeg(t, entries, ledger.EntryTypeAssetIncrease, dstID).Amount)

	for _, e := range entries {
		assert.NotEqual(t, ledger.EntryTypeClearing, e.EntryType)
	}
}

// -----------------------------------------------------------------------------
// The registry is the authority on precision, not the provider's hint
// -----------------------------------------------------------------------------

// TestBridge_PrecisionComesFromTheRegistryRow: the arriving leg is booked AS the
// destination registry row, so it must be counted in THAT row's units. The
// provider's hint only creates a row that does not exist; where one does and
// they disagree, following the hint denominates the quantity at one scale and
// its identity at another.
//
// This is the live-data shape: Base USDC exists in the registry at 6 decimals
// and at 18 under two different contracts.
func TestBridge_PrecisionComesFromTheRegistryRow(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, biWallet)

	const sent = int64(24_446_762)

	send := biLeg(biArbitrum, "0xsend", biUSDCOnArb, sent, sync.DirectionOut, biAt)
	send.Transfers[0].Decimals = 6
	// The receive leg REPORTS 6, but the registry row for this contract says 18.
	recv := biLeg(biBase, "0xrecv", biUSDCOnBase, sent, sync.DirectionIn, biAt.Add(2*time.Second))
	recv.Transfers[0].Decimals = 6

	env := newBIEnv(t, userID, w, []sync.DecodedTransaction{send, recv})
	bpSeedRegistry(env.registry, biArbitrum, biUSDCOnArb, "USDC", 6)
	bpSeedRegistry(env.registry, biBase, biUSDCOnBase, "USDC", 18)

	env.ledgerSvc.On("RecordTransaction", mock.Anything, ledger.TxTypeInternalTransfer, "noves",
		mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil).Once()

	require.NoError(t, env.processor.ProcessAll(ctx, w))
	require.Len(t, env.ledgerSvc.recordedTransactions, 1)
	data := env.ledgerSvc.recordedTransactions[0].RawData

	assert.EqualValues(t, 18, data["dest_decimals"],
		"the row the arriving leg is booked as says 18; the provider saying 6 does not change what "+
			"that asset's base unit is")
}

// -----------------------------------------------------------------------------
// The arriving leg passes the known-asset filter
// -----------------------------------------------------------------------------

// TestBridge_DestinationLegIsFiltered: filterKnownLegs walks Transfers, and the
// arriving leg has no element there — the stitched source raw carries the
// outflow alone. So (DestChainID, DestContractAddress) was resolved into the
// registry and booked to the ledger without ever being offered to the filter: a
// hole straight through it, on the destination chain.
func TestBridge_DestinationLegIsFiltered(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, biWallet)

	send := biLeg(biArbitrum, "0xsend", biUSDCOnArb, biBridgedValue, sync.DirectionOut, biAt)
	recv := biLeg(biBase, "0xrecv", biUSDCOnBase, biArrivedValue, sync.DirectionIn, biAt.Add(2*time.Second))

	knownness := newFakeKnownnessRegistry()
	// The departing asset is known; the ARRIVING one is convicted spam.
	knownness.setOverride(sync.NewAssetKey(biArbitrum, biUSDCOnArb), true)
	knownness.setOverride(sync.NewAssetKey(biBase, biUSDCOnBase), false)

	recorder := &bpProbeRecorder{inner: knownness}
	env := newBIEnvFiltered(t, userID, w, []sync.DecodedTransaction{send, recv}, recorder)

	require.NoError(t, env.processor.ProcessAll(ctx, w))

	assert.Empty(t, env.ledgerSvc.recordedTransactions,
		"a bridge whose ARRIVING asset is not known must not be booked. The arriving leg is the "+
			"transfer; there is no partial form where it is dropped and the rest still happened (#86)")

	var probed bool
	for _, k := range recorder.probed() {
		if k == sync.NewAssetKey(biBase, biUSDCOnBase) {
			probed = true
		}
	}
	assert.True(t, probed,
		"the destination identity must be offered to the filter at all; it never was, because "+
			"nothing walks a leg that has no element in Transfers")
}

// TestBridge_DestinationLegKnown_StillBooks is the other half: the guard must
// convict only what the filter convicts.
func TestBridge_DestinationLegKnown_StillBooks(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, biWallet)

	send := biLeg(biArbitrum, "0xsend", biUSDCOnArb, biBridgedValue, sync.DirectionOut, biAt)
	recv := biLeg(biBase, "0xrecv", biUSDCOnBase, biArrivedValue, sync.DirectionIn, biAt.Add(2*time.Second))

	knownness := newFakeKnownnessRegistry()
	knownness.setOverride(sync.NewAssetKey(biArbitrum, biUSDCOnArb), true)
	knownness.setOverride(sync.NewAssetKey(biBase, biUSDCOnBase), true)

	env := newBIEnvFiltered(t, userID, w, []sync.DecodedTransaction{send, recv}, knownness)

	env.ledgerSvc.On("RecordTransaction", mock.Anything, ledger.TxTypeInternalTransfer, "noves",
		mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil).Once()

	require.NoError(t, env.processor.ProcessAll(ctx, w))
	assert.Len(t, env.ledgerSvc.recordedTransactions, 1)
}

// -----------------------------------------------------------------------------
// The arriving leg's contract names the destination chain
// -----------------------------------------------------------------------------

// TestBridge_DestContractIsTheDestinationChains: a bridged token has a different
// contract on every chain, so stamping the source chain's address on the
// arriving leg asserts an address that holds nothing there.
func TestBridge_DestContractIsTheDestinationChains(t *testing.T) {
	data, _, dstID, w := bpBridge(t,
		biArbitrum, biUSDCOnArb, 6,
		biBase, biUSDCOnBase, 6,
		biBridgedValue)

	require.Equal(t, biUSDCOnBase, data["dest_contract_address"])

	entries := bpEntries(t, data, w)
	debit := bpLeg(t, entries, ledger.EntryTypeAssetIncrease, dstID)

	assert.Equal(t, biUSDCOnBase, debit.Metadata["contract_address"],
		"the arriving leg names the contract that actually holds this asset on the destination chain")
	assert.NotEqual(t, biUSDCOnArb, debit.Metadata["contract_address"],
		"the source chain's contract is not this asset's address on the destination chain")
}

// TestBridge_NativeCoin_ArrivingLegNamesNoContract: the native coin has no
// contract anywhere. An absent field says "none here"; a borrowed one lies.
func TestBridge_NativeCoin_ArrivingLegNamesNoContract(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, biWallet)

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

	entries := bpEntries(t, data, w)
	baseETH := env.registry.idFor(t, biBase, sync.NativeContract)
	debit := bpLeg(t, entries, ledger.EntryTypeAssetIncrease, baseETH)

	assert.NotContains(t, debit.Metadata, "contract_address",
		"a native coin has no contract; the field is omitted rather than filled with the source "+
			"chain's or with an empty string pretending to be an address")
}
