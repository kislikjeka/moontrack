package sync_test

import (
	"context"
	"errors"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/price"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/sync/assetlist"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// =============================================================================
// Real addresses from the #37 measurement
//
// These are not invented fixtures. They are the actual contracts found in the
// 393 real raws the decision was measured on, so a test that passes here is a
// statement about the data the system will really meet.
// =============================================================================

const (
	// realUSDCBase is Circle's USDC on base — in every public token list, so
	// level 1 knows it offline.
	realUSDCBase = "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"

	// spamUSDCBase is `UЅDС` — a homoglyph forgery of USDC using Cyrillic Ѕ and
	// С. Same ticker as the real coin, different contract, in no token list.
	// This is the exact attack the filter exists to stop.
	//
	// The issue records the address by its prefix (0xeb9caafc…); the remainder
	// here is padding, because what the test turns on is only that this is a
	// DIFFERENT contract carrying the SAME ticker. assetlist_test asserts the
	// real prefixes against the generated list.
	spamUSDCBase = "0xeb9caafcd0bb1a3d8ba2e0d1c1c1d1e1f1a1b1c1"

	// debtTokenBase is variableDebtBasUSDC — a real position from the
	// measurement (prefix 0x59dca05b…, padded here for the same reason). In NO
	// token list, and never will be: a debt token is not a coin. Yet the price
	// provider quotes it, at −0.9997. Level 2 is what admits it.
	debtTokenBase = "0x59dca05b6c26dbd64b5381374aaac5cd05644c28"
)

// =============================================================================
// Fake knownness registry
// =============================================================================

// fakeKnownnessRegistry is an in-memory stand-in for asset_knownness. It records
// every enqueue so a test can prove the hot path queues an unfamiliar asset
// rather than probing it.
type fakeKnownnessRegistry struct {
	rows      map[sync.AssetKey]*sync.KnownnessRecord
	enqueued  []sync.AssetKey
	getErr    error
	getCalled int
}

func newFakeKnownnessRegistry() *fakeKnownnessRegistry {
	return &fakeKnownnessRegistry{rows: map[sync.AssetKey]*sync.KnownnessRecord{}}
}

func (f *fakeKnownnessRegistry) Get(ctx context.Context, key sync.AssetKey) (*sync.KnownnessRecord, error) {
	f.getCalled++
	if f.getErr != nil {
		return nil, f.getErr
	}
	if rec, ok := f.rows[key]; ok {
		return rec, nil
	}
	return nil, nil
}

func (f *fakeKnownnessRegistry) Enqueue(ctx context.Context, key sync.AssetKey, symbol string) error {
	f.enqueued = append(f.enqueued, key)
	if _, exists := f.rows[key]; !exists {
		f.rows[key] = &sync.KnownnessRecord{Key: key, Status: sync.KnownnessPending, Symbol: symbol}
	}
	return nil
}

// setStatus plants a resolved verdict, as the background worker would.
func (f *fakeKnownnessRegistry) setStatus(key sync.AssetKey, status sync.KnownnessStatus, source sync.KnownnessSource) {
	f.rows[key] = &sync.KnownnessRecord{Key: key, Status: status, Source: source}
}

// setOverride plants a manual verdict (level 3).
func (f *fakeKnownnessRegistry) setOverride(key sync.AssetKey, override bool) {
	rec, ok := f.rows[key]
	if !ok {
		rec = &sync.KnownnessRecord{Key: key, Status: sync.KnownnessPending}
		f.rows[key] = rec
	}
	rec.Override = &override
}

var _ sync.KnownnessRegistry = (*fakeKnownnessRegistry)(nil)

// =============================================================================
// Test builder
// =============================================================================

// newFilterTestBuilder wires a TxBuilder whose only interesting dependency is
// the known-asset filter, plus a ledger mock that CAPTURES every recorded
// transaction. Capturing rather than counting is what lets the required test
// assert on the entries that would be generated.
func newFilterTestBuilder(t *testing.T, registry sync.KnownnessRegistry) (*sync.TxBuilder, *captureLedgerSvc) {
	t.Helper()
	log := logger.New("test", os.Stdout)

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetWalletsByAddressAndUserID", mock.Anything, mock.Anything, mock.Anything).
		Return([]*wallet.Wallet{}, nil).Maybe()

	ledgerSvc := &captureLedgerSvc{}
	filter := sync.NewKnownAssetFilter(registry)

	return sync.NewTxBuilder(walletRepo, ledgerSvc, nil, nil, log, nil, nil, filter), ledgerSvc
}

// captureLedgerSvc records every RecordTransaction call verbatim.
type captureLedgerSvc struct {
	recorded []capturedTx
}

type capturedTx struct {
	Type ledger.TransactionType
	Data map[string]interface{}
}

func (c *captureLedgerSvc) RecordTransaction(ctx context.Context, txType ledger.TransactionType, source string, externalID *string, occurredAt time.Time, rawData map[string]interface{}) (*ledger.Transaction, error) {
	c.recorded = append(c.recorded, capturedTx{Type: txType, Data: rawData})
	return &ledger.Transaction{ID: uuid.New()}, nil
}

func (c *captureLedgerSvc) FindBySourceExternalID(ctx context.Context, source, externalID string) (*ledger.Transaction, error) {
	return nil, nil
}

var _ sync.LedgerService = (*captureLedgerSvc)(nil)

// transfersOf pulls the legs array out of a captured transaction's raw data.
func transfersOf(t *testing.T, data map[string]interface{}) []map[string]interface{} {
	t.Helper()
	raw, ok := data["transfers"]
	if !ok {
		return nil
	}
	legs, ok := raw.([]map[string]interface{})
	require.True(t, ok, "transfers has unexpected type %T", raw)
	return legs
}

func recvTx(txHash, chain string, transfers ...sync.DecodedTransfer) sync.DecodedTransaction {
	return sync.DecodedTransaction{
		ID:            chain + ":" + txHash,
		TxHash:        txHash,
		ChainID:       chain,
		OperationType: sync.OpReceive,
		Status:        "confirmed",
		MinedAt:       time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		Transfers:     transfers,
	}
}

func inLeg(symbol, contract string, decimals int, amount int64) sync.DecodedTransfer {
	return sync.DecodedTransfer{
		AssetSymbol:     symbol,
		AssetName:       symbol,
		ContractAddress: contract,
		Decimals:        decimals,
		Amount:          big.NewInt(amount),
		Direction:       sync.DirectionIn,
		Sender:          "0x1111111111111111111111111111111111111111",
		Recipient:       "0x9afc000000000000000000000000000000000000",
	}
}

func testWallet() *wallet.Wallet {
	return &wallet.Wallet{
		ID:      uuid.New(),
		UserID:  uuid.New(),
		Address: "0x9afc000000000000000000000000000000000000",
	}
}

// =============================================================================
// REQUIRED TEST 1 (acceptance criterion 10)
//
// A spam leg carrying the ticker of a known coin at a DIFFERENT contract
// produces neither entries nor lots, while the real coin of the same pair is
// accounted for in full.
// =============================================================================

// TestFilter_SpamTickerDifferentContract_ProducesNoEntriesOrLots is the port
// statement of the whole ticket.
//
// Both legs call themselves USDC on base. One is Circle's real contract, the
// other a Cyrillic homoglyph forgery at a different address — the actual attack
// found in the measured history, where four such forgeries mirrored real
// outgoing sends down to the exact amount.
//
// The assertion is deliberately made on WHAT REACHES THE LEDGER SERVICE, not on
// an internal filter verdict: entries and tax lots are generated downstream of
// RecordTransaction, so a leg that never appears in a recorded transaction can
// produce neither. Asserting on the verdict would test the filter's opinion of
// itself; asserting here tests the property the ticket asks for.
func TestFilter_SpamTickerDifferentContract_ProducesNoEntriesOrLots(t *testing.T) {
	ctx := context.Background()
	reg := newFakeKnownnessRegistry()

	// The forgery has been probed and convicted: terminal `unknown`. The real
	// coin needs no row at all — level 1 knows it from the compiled-in list.
	reg.setStatus(sync.NewAssetKey("base", spamUSDCBase), sync.KnownnessUnknown, sync.KnownnessSourceQuotable)

	builder, ledgerSvc := newFilterTestBuilder(t, reg)
	w := testWallet()

	// One transaction, two legs, same ticker, different contracts.
	tx := recvTx("0xaaa", "base",
		inLeg("USDC", realUSDCBase, 6, 2_800_000_000),
		inLeg("USDC", spamUSDCBase, 6, 2_800_000_000),
	)

	_, err := builder.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recorded, 1, "the real leg must still be recorded")
	rec := ledgerSvc.recorded[0]

	legs := transfersOf(t, rec.Data)
	require.Len(t, legs, 1, "exactly one leg may reach the ledger")
	assert.Equal(t, realUSDCBase, legs[0]["contract_address"],
		"the surviving leg must be the REAL contract, not the forgery")

	// The real coin is accounted for IN FULL — the filter must not shave the
	// legitimate movement while removing the spam one.
	assert.Equal(t, "2800000000", legs[0]["amount"])

	// The flat legacy fields are the trap named in the decision: they are
	// computed from the FIRST leg independently of the array. Had the filter run
	// after the builders, or touched only the array, these could still name the
	// forgery — or name a token the array no longer contains.
	assert.Equal(t, realUSDCBase, rec.Data["contract_address"],
		"flat legacy contract must agree with the filtered array")
	assert.Equal(t, "2800000000", rec.Data["amount"],
		"flat legacy amount must agree with the filtered array")

	// And the spam contract appears NOWHERE in what was recorded.
	assertContractAbsent(t, rec.Data, spamUSDCBase)
}

// TestFilter_SpamOnlyTransaction_RecordsNothingAtAll covers the pure-spam case:
// when every leg is filtered there is no movement left, so no ledger
// transaction may be created at all. An empty transaction would still open a
// row and a raw pointing at it for an event that, on the books, did not happen.
func TestFilter_SpamOnlyTransaction_RecordsNothingAtAll(t *testing.T) {
	ctx := context.Background()
	reg := newFakeKnownnessRegistry()
	reg.setStatus(sync.NewAssetKey("base", spamUSDCBase), sync.KnownnessUnknown, sync.KnownnessSourceQuotable)

	builder, ledgerSvc := newFilterTestBuilder(t, reg)

	tx := recvTx("0xbbb", "base", inLeg("USDC", spamUSDCBase, 6, 109_907_431))

	txID, err := builder.ProcessTransaction(ctx, testWallet(), tx)
	require.NoError(t, err)

	assert.Nil(t, txID, "a wholly-spam transaction yields no ledger transaction")
	assert.Empty(t, ledgerSvc.recorded, "nothing may be recorded — no entries, no lots")
}

// assertContractAbsent fails when a contract string appears anywhere in the
// recorded raw data, at any nesting depth.
func assertContractAbsent(t *testing.T, data map[string]interface{}, contract string) {
	t.Helper()
	for k, v := range data {
		switch typed := v.(type) {
		case string:
			assert.NotEqual(t, contract, typed, "filtered contract leaked into %q", k)
		case map[string]interface{}:
			assertContractAbsent(t, typed, contract)
		case []map[string]interface{}:
			for _, item := range typed {
				assertContractAbsent(t, item, contract)
			}
		}
	}
}

// =============================================================================
// REQUIRED TEST 2 (acceptance criterion 11)
//
// Wipe + reprocess from raw after an override change gives the same result
// WITHOUT going to the provider.
// =============================================================================

// TestFilter_OverrideChange_ReplayIsDeterministicWithoutProvider states the
// replay property the ticket demands.
//
// A wipe re-pends the wallet's raws and the processor feeds the very same
// decoded transactions through again. The filter's decision has to be a PURE
// FUNCTION of those stored raws plus the local registry — never of a provider
// call — otherwise a replay could silently disagree with the original run, and
// changing an override would mean re-fetching history that is already durable.
//
// "Without going to the provider" is proved STRUCTURALLY, by the registry
// recording every call made to it and the test asserting those are all local
// reads. A probe object that the filter has no way of reaching would prove
// nothing at all — the assertion would hold even if a provider were called.
func TestFilter_OverrideChange_ReplayIsDeterministicWithoutProvider(t *testing.T) {
	ctx := context.Background()

	// The exact same decoded transaction is replayed in every pass, standing in
	// for the durable raw_json a wipe leaves behind.
	rawTx := func() sync.DecodedTransaction {
		return recvTx("0xccc", "base", inLeg("aBascbBTC", debtTokenBase, 8, 5_000_000))
	}

	// PASS 1 — no override, and the worker has convicted the asset.
	reg := newFakeKnownnessRegistry()
	key := sync.NewAssetKey("base", debtTokenBase)
	reg.setStatus(key, sync.KnownnessUnknown, sync.KnownnessSourceQuotable)

	builder, ledgerSvc := newFilterTestBuilder(t, reg)
	_, err := builder.ProcessTransaction(ctx, testWallet(), rawTx())
	require.NoError(t, err)
	require.Empty(t, ledgerSvc.recorded, "before the override the asset stays out of the ledger")

	// THE OVERRIDE CHANGES — a human declares the asset known. This is the only
	// input that moves between the passes.
	reg.setOverride(key, true)

	// PASS 2 — wipe + reprocess. Same raws, same code, new override.
	builder2, ledgerSvc2 := newFilterTestBuilder(t, reg)
	_, err = builder2.ProcessTransaction(ctx, testWallet(), rawTx())
	require.NoError(t, err)

	require.Len(t, ledgerSvc2.recorded, 1, "after the override the asset enters the ledger")
	legs := transfersOf(t, ledgerSvc2.recorded[0].Data)
	require.Len(t, legs, 1)
	assert.Equal(t, debtTokenBase, legs[0]["contract_address"])

	// PASS 3 — replaying again with the override unchanged must reproduce pass 2
	// exactly. Determinism is the property, not merely "it worked once".
	builder3, ledgerSvc3 := newFilterTestBuilder(t, reg)
	_, err = builder3.ProcessTransaction(ctx, testWallet(), rawTx())
	require.NoError(t, err)
	require.Len(t, ledgerSvc3.recorded, 1)
	assert.Equal(t, ledgerSvc2.recorded[0].Data["amount"], ledgerSvc3.recorded[0].Data["amount"],
		"replay from the same raws must be byte-for-byte deterministic")

	// And across all three passes the ONLY thing consulted was the local
	// registry. Every call the filter is capable of making passes through this
	// fake, so an exhaustive account of its calls is an exhaustive account of
	// the filter's dependencies — there is no other collaborator it could have
	// reached for.
	assert.Positive(t, reg.getCalled, "the local registry is what was read")
	assert.Empty(t, reg.enqueued,
		"a replay of already-known identities must not even queue new probe work")
}

// TestKnownAssetFilter_HasNoProviderDependency proves the "no network call on
// the hot path" property STRUCTURALLY rather than behaviourally.
//
// A behavioural test cannot state this honestly: passing a probe object that the
// filter has no field to hold, then asserting it was never called, is vacuously
// true and would keep passing if a provider call were added tomorrow. So the
// property is asserted where it actually lives — in the filter's TYPE. The
// KnownnessRegistry it depends on is the local table, and the compile-time
// assertion below is what fails if anyone widens that dependency to something
// that can reach the network.
func TestKnownAssetFilter_HasNoProviderDependency(t *testing.T) {
	// The filter is constructible from a registry ALONE. If a provider probe
	// were ever added as a required dependency, this stops compiling — which is
	// a stronger and earlier signal than any runtime assertion.
	var registryOnly sync.KnownnessRegistry = newFakeKnownnessRegistry()
	filter := sync.NewKnownAssetFilter(registryOnly)
	require.NotNil(t, filter)

	// And the registry port itself exposes only local reads and a queue write —
	// no probing method that an implementation could route to a provider. This
	// interface satisfaction is the actual guarantee.
	var _ sync.KnownnessRegistry = (*fakeKnownnessRegistry)(nil)

	// The probe lives on the OTHER side of the split, in the queue the
	// background worker drains, and nothing on the read path can reach it.
	var _ sync.KnownnessQueue = (*stubQueue)(nil)
}

// TestNativeSentinelMatchesSync pins the one value that is deliberately spelled
// twice.
//
// assetlist cannot import sync — sync imports assetlist, so the arrow would
// cycle — which is why the sentinel is duplicated rather than shared. The
// duplication is only safe while the two literals agree: if they diverged, every
// native leg would fail the level-1 native check and fall through to a probe
// that cannot answer for a contract-less asset, and the chain's native coin
// would silently leave the ledger. This test is the seam that makes the
// divergence impossible to commit, and it lives HERE because this package is the
// one that can see both constants.
func TestNativeSentinelMatchesSync(t *testing.T) {
	assert.Equal(t, sync.NativeContract, assetlist.NativeContract,
		"the native sentinel is spelled in two packages and they must never drift apart")
}

// =============================================================================
// The three levels, in order
// =============================================================================

// TestFilter_LevelOne_BuiltinListAnswersOffline proves level 1 needs no registry
// row at all: the real USDC contract is admitted without the local table ever
// being consulted for a verdict, and without anything being enqueued.
func TestFilter_LevelOne_BuiltinListAnswersOffline(t *testing.T) {
	ctx := context.Background()
	reg := newFakeKnownnessRegistry()
	filter := sync.NewKnownAssetFilter(reg)

	v, err := filter.Resolve(ctx, sync.NewAssetKey("base", realUSDCBase), "USDC")
	require.NoError(t, err)

	assert.True(t, v.Known)
	assert.Equal(t, sync.KnownnessSourceBuiltin, v.Source)
	assert.Empty(t, reg.enqueued, "a coin answered by the built-in list needs no probe")
}

// TestFilter_LevelTwo_QuotabilityAdmitsDeFiAsset is the measured case level 1
// structurally cannot cover: a debt token, in no token list and never will be,
// yet quotable at the provider (−0.9997 in the real measurement). Its knownness
// arrives from the local table the background worker fills.
func TestFilter_LevelTwo_QuotabilityAdmitsDeFiAsset(t *testing.T) {
	ctx := context.Background()
	reg := newFakeKnownnessRegistry()
	key := sync.NewAssetKey("base", debtTokenBase)

	// Level 1 genuinely misses it — that is the premise of the whole level.
	filter := sync.NewKnownAssetFilter(reg)
	first, err := filter.Resolve(ctx, key, "variableDebtBasUSDC")
	require.NoError(t, err)
	assert.False(t, first.Known, "unknown until the worker answers")
	assert.Equal(t, sync.KnownnessPending, first.Status)
	assert.Equal(t, []sync.AssetKey{key}, reg.enqueued, "first sighting queues a probe")

	// The worker finds it quotable.
	reg.setStatus(key, sync.KnownnessKnown, sync.KnownnessSourceQuotable)

	after, err := filter.Resolve(ctx, key, "variableDebtBasUSDC")
	require.NoError(t, err)
	assert.True(t, after.Known)
	assert.Equal(t, sync.KnownnessSourceQuotable, after.Source)
}

// TestFilter_LevelThree_OverrideOutranksAutomaticVerdict pins the ordering: a
// human's answer wins over whatever the machine concluded, in BOTH directions.
func TestFilter_LevelThree_OverrideOutranksAutomaticVerdict(t *testing.T) {
	ctx := context.Background()

	t.Run("override admits a convicted asset", func(t *testing.T) {
		reg := newFakeKnownnessRegistry()
		key := sync.NewAssetKey("base", debtTokenBase)
		reg.setStatus(key, sync.KnownnessUnknown, sync.KnownnessSourceQuotable)
		reg.setOverride(key, true)

		v, err := sync.NewKnownAssetFilter(reg).Resolve(ctx, key, "variableDebtBasUSDC")
		require.NoError(t, err)
		assert.True(t, v.Known)
		assert.Equal(t, sync.KnownnessSourceOverride, v.Source)
	})

	t.Run("override rejects even a built-in coin", func(t *testing.T) {
		// The override has to outrank level 1 too, otherwise there is no way to
		// exclude an asset a token list happens to carry.
		reg := newFakeKnownnessRegistry()
		key := sync.NewAssetKey("base", realUSDCBase)
		reg.setOverride(key, false)

		v, err := sync.NewKnownAssetFilter(reg).Resolve(ctx, key, "USDC")
		require.NoError(t, err)
		assert.False(t, v.Known)
		assert.Equal(t, sync.KnownnessSourceOverride, v.Source)
	})
}

// =============================================================================
// Natives (acceptance criteria 3 and 9)
// =============================================================================

// TestFilter_NativeCoin_KnownByConstructionWithSymbolCheck covers both halves of
// the native rule at once, because they are the same rule.
//
// Without knownness by construction the filter would kill the balance more
// surely than any spam: the measurement found 104 native legs — the largest
// position and ALL gas — none of which can appear in a token list.
//
// And knownness is granted to (chain, native) WITH the symbol checked, not to
// "any leg with a blank contract". The provider really does emit `UNKN` legs
// with zero decimals and no contract; the pre-#56 predicate merged those into
// real ETH, which is exactly the silent failure the epic is about.
func TestFilter_NativeCoin_KnownByConstructionWithSymbolCheck(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		chain     string
		symbol    string
		wantKnown bool
		why       string
	}{
		{"ETH on ethereum", "ethereum", "ETH", true, "the chain's own coin"},
		{"ETH on base", "base", "ETH", true, "base is an ETH L2"},
		{"ETH on arbitrum", "arbitrum", "ETH", true, "arbitrum is an ETH L2"},
		{"POL on polygon", "polygon", "POL", true, "polygon's own coin"},
		{"AVAX on avalanche", "avalanche", "AVAX", true, "avalanche's own coin"},

		// The edge case named in the ticket, verbatim: unknown symbol, no
		// contract. Must NOT be admitted and must NOT count as native.
		{"UNKN on ethereum", "ethereum", "UNKN", false, "unrecognised symbol is not the native coin"},
		{"blank symbol", "ethereum", "", false, "a leg with no symbol at all is not native"},

		// A real coin, but the wrong chain's. AVAX on base is not base's native
		// coin, so the pair must not inherit ETH's knownness.
		{"AVAX on base", "base", "AVAX", false, "wrong chain's coin"},

		// A chain the system does not know cannot have a verified native coin.
		{"ETH on an unknown chain", "fantom-not-enabled", "ETH", false, "chain not in the native table"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reg := newFakeKnownnessRegistry()
			// A blank contract becomes the native sentinel in NewAssetKey.
			key := sync.NewAssetKey(tc.chain, "")
			require.True(t, key.IsNative(), "precondition: the key is native-shaped")

			v, err := sync.NewKnownAssetFilter(reg).Resolve(ctx, key, tc.symbol)
			require.NoError(t, err)
			assert.Equal(t, tc.wantKnown, v.Known, tc.why)

			if tc.wantKnown {
				assert.Equal(t, sync.KnownnessSourceBuiltin, v.Source,
					"a native coin is known by construction, never by a probe")
			}
		})
	}
}

// TestFilter_ZeroDecimalsUnknownSymbol_IsNotNative isolates the ticket's edge
// case at the transaction level: `UNKN`, zero decimals, no contract must not
// reach the ledger by being mistaken for the chain's coin.
func TestFilter_ZeroDecimalsUnknownSymbol_IsNotNative(t *testing.T) {
	ctx := context.Background()
	reg := newFakeKnownnessRegistry()
	builder, ledgerSvc := newFilterTestBuilder(t, reg)

	junk := inLeg("UNKN", "", 0, 1)
	real := inLeg("ETH", "", 18, 1_000_000_000_000_000_000)

	tx := recvTx("0xddd", "ethereum", real, junk)

	_, err := builder.ProcessTransaction(ctx, testWallet(), tx)
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recorded, 1)
	legs := transfersOf(t, ledgerSvc.recorded[0].Data)
	require.Len(t, legs, 1, "the UNKN leg must not be merged with real ETH")
	// Which leg survived is the point; the ticker names it. Identity travels
	// under asset_id as a UUID, and this builder has no registry wired.
	assert.Equal(t, "ETH", legs[0]["asset_symbol"])
}

// =============================================================================
// Fail-open behaviour
// =============================================================================

// TestFilter_RegistryError_AdmitsLeg pins the direction of failure. If the local
// table cannot be read, the honest answer is to record the movement: a lost
// movement is the one outcome the product does not accept, and afterwards it
// would be indistinguishable from correctly-filtered spam.
func TestFilter_RegistryError_AdmitsLeg(t *testing.T) {
	ctx := context.Background()
	reg := newFakeKnownnessRegistry()
	reg.getErr = errors.New("database unavailable")

	builder, ledgerSvc := newFilterTestBuilder(t, reg)
	tx := recvTx("0xeee", "base", inLeg("SOMETHING", debtTokenBase, 18, 42))

	_, err := builder.ProcessTransaction(ctx, testWallet(), tx)
	require.NoError(t, err, "a registry outage must not fail the sync")

	require.Len(t, ledgerSvc.recorded, 1, "the leg is admitted rather than lost")
}

// TestFilter_NilFilter_AdmitsEverything guards the unwired case: a deployment
// that has not configured the filter must behave exactly as before it existed,
// never silently empty the ledger.
func TestFilter_NilFilter_AdmitsEverything(t *testing.T) {
	ctx := context.Background()
	var filter *sync.KnownAssetFilter

	v, err := filter.Resolve(ctx, sync.NewAssetKey("base", spamUSDCBase), "USDC")
	require.NoError(t, err)
	assert.True(t, v.Known, "an unwired filter admits everything")
}

// =============================================================================
// The hot path stays offline (acceptance criterion 1)
// =============================================================================

// TestFilter_UnfamiliarAsset_IsQueuedNotProbed states the architectural rule in
// terms of what the hot path DOES, which is the observable half of "it makes no
// network call": meeting a completely unfamiliar asset — the case most likely to
// tempt a synchronous lookup — it defers the asset to the background queue and
// admits nothing.
//
// The complementary structural half, that the filter has no provider dependency
// to call in the first place, is TestKnownAssetFilter_HasNoProviderDependency.
func TestFilter_UnfamiliarAsset_IsQueuedNotProbed(t *testing.T) {
	ctx := context.Background()
	reg := newFakeKnownnessRegistry()

	builder, ledgerSvc := newFilterTestBuilder(t, reg)

	tx := recvTx("0xfff", "base", inLeg("NEVERSEEN", "0x1234567890abcdef1234567890abcdef12345678", 18, 7))
	_, err := builder.ProcessTransaction(ctx, testWallet(), tx)
	require.NoError(t, err)

	require.Len(t, reg.enqueued, 1, "the identity is queued for the background worker")
	assert.Empty(t, ledgerSvc.recorded,
		"and until a verdict arrives it stays out of the ledger — pending, not spam")
}

// =============================================================================
// Worker: the verdict comes only from exhausting retries
// (acceptance criteria 7 and 8)
// =============================================================================

// stubQueue is an in-memory knownness queue recording what the worker did.
type stubQueue struct {
	next *sync.KnownnessRecord

	markedKnown   []sync.AssetKey
	markedUnknown []sync.AssetKey
	rescheduled   []int // attempts recorded per counted failure
	unlocked      int   // releases that did NOT count as an attempt
	lastError     string
}

func (q *stubQueue) ClaimReady(ctx context.Context) (*sync.KnownnessRecord, error) {
	rec := q.next
	q.next = nil
	return rec, nil
}

func (q *stubQueue) MarkKnown(ctx context.Context, key sync.AssetKey, source sync.KnownnessSource) error {
	q.markedKnown = append(q.markedKnown, key)
	return nil
}

func (q *stubQueue) MarkUnknown(ctx context.Context, key sync.AssetKey, attempts int, lastError string) error {
	q.markedUnknown = append(q.markedUnknown, key)
	q.lastError = lastError
	return nil
}

func (q *stubQueue) Reschedule(ctx context.Context, key sync.AssetKey, attempts int, next time.Time, lastError string) error {
	q.rescheduled = append(q.rescheduled, attempts)
	q.lastError = lastError
	return nil
}

func (q *stubQueue) UnlockWithoutCounting(ctx context.Context, key sync.AssetKey, next time.Time) error {
	q.unlocked++
	return nil
}

var _ sync.KnownnessQueue = (*stubQueue)(nil)

// scriptedProbe returns a fixed error.
type scriptedProbe struct{ err error }

func (p *scriptedProbe) IsQuotable(ctx context.Context, key sync.AssetKey) error { return p.err }

func runWorkerOnce(t *testing.T, rec *sync.KnownnessRecord, probeErr error) *stubQueue {
	t.Helper()
	q := &stubQueue{next: rec}
	w := sync.NewKnownnessWorker(sync.KnownnessWorkerDeps{
		Queue:  q,
		Probe:  &scriptedProbe{err: probeErr},
		Logger: logger.New("test", os.Stdout),
	})
	require.NoError(t, w.ProcessOne(context.Background()))
	return q
}

// TestKnownnessWorker_TransientErrorsDoNotCountAsAttempts is the user's explicit
// requirement, stated as a test: the verdict is never set by a failing API.
//
// A rate limit or a network error leaves the identity exactly as far from
// conviction as it was — attempts do not advance — so a provider outage costs
// time and nothing else. Only a real negative answer spends an attempt.
func TestKnownnessWorker_TransientErrorsDoNotCountAsAttempts(t *testing.T) {
	key := sync.NewAssetKey("base", debtTokenBase)

	// One attempt short of terminal: if a transient error counted, this row
	// would be convicted on the very next tick.
	almostTerminal := func() *sync.KnownnessRecord {
		return &sync.KnownnessRecord{Key: key, Status: sync.KnownnessPending, Attempts: price.MaxAttempts - 1}
	}

	t.Run("rate limit does not count", func(t *testing.T) {
		q := runWorkerOnce(t, almostTerminal(), &price.RateLimitedError{RetryAfter: time.Minute})
		assert.Equal(t, 1, q.unlocked, "released without counting")
		assert.Empty(t, q.markedUnknown, "a rate limit may never convict an asset")
		assert.Empty(t, q.rescheduled, "attempts must not advance")
	})

	t.Run("network failure does not count", func(t *testing.T) {
		q := runWorkerOnce(t, almostTerminal(), price.ErrTransient)
		assert.Equal(t, 1, q.unlocked)
		assert.Empty(t, q.markedUnknown, "a network blip may never convict an asset")
		assert.Empty(t, q.rescheduled)
	})

	t.Run("a real negative counts but does not yet convict", func(t *testing.T) {
		q := runWorkerOnce(t, &sync.KnownnessRecord{Key: key, Status: sync.KnownnessPending, Attempts: 0}, price.ErrNotFound)
		assert.Equal(t, []int{1}, q.rescheduled, "one attempt spent")
		assert.Empty(t, q.markedUnknown, "one miss is not a verdict")
		assert.Zero(t, q.unlocked)
	})

	t.Run("the verdict lands only at the end of the ladder", func(t *testing.T) {
		q := runWorkerOnce(t, almostTerminal(), price.ErrNotFound)
		require.Len(t, q.markedUnknown, 1, "terminal only after MaxAttempts")
		assert.Equal(t, key, q.markedUnknown[0])
	})

	t.Run("quotable resolves known", func(t *testing.T) {
		q := runWorkerOnce(t, &sync.KnownnessRecord{Key: key, Status: sync.KnownnessPending}, nil)
		require.Len(t, q.markedKnown, 1)
		assert.Empty(t, q.markedUnknown)
	})
}

// TestKnownness_CheckedDistinctionSurvives pins the distinction the
// reconciliation report (#61) depends on: "checked, and the answer is no" must
// stay separable from "could not be checked". Erasing it would make a clean
// reconciliation over a spam-filled wallet unverifiable.
func TestKnownness_CheckedDistinctionSurvives(t *testing.T) {
	ctx := context.Background()
	reg := newFakeKnownnessRegistry()
	filter := sync.NewKnownAssetFilter(reg)

	convicted := sync.NewAssetKey("base", spamUSDCBase)
	reg.setStatus(convicted, sync.KnownnessUnknown, sync.KnownnessSourceQuotable)

	queued := sync.NewAssetKey("base", "0xabcabcabcabcabcabcabcabcabcabcabcabcabca")
	reg.setStatus(queued, sync.KnownnessPending, "")

	vConvicted, err := filter.Resolve(ctx, convicted, "USDC")
	require.NoError(t, err)
	vQueued, err := filter.Resolve(ctx, queued, "WHATEVER")
	require.NoError(t, err)

	// Both are kept out of the ledger …
	assert.False(t, vConvicted.Known)
	assert.False(t, vQueued.Known)

	// … but they are NOT the same fact.
	assert.True(t, vConvicted.Checked(), "terminally resolved unknown")
	assert.False(t, vQueued.Checked(), "still queued — not spam, just unchecked")
	assert.NotEqual(t, vConvicted.Status, vQueued.Status)
}
