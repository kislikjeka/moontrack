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

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/price"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// =============================================================================
// Fakes for backfill test
// =============================================================================

type fakeJobEnqueuer struct {
	calls []jobEnqueueCall
}

type jobEnqueueCall struct {
	assetID    uuid.UUID
	targetTime time.Time
}

func (f *fakeJobEnqueuer) Enqueue(ctx context.Context, assetID uuid.UUID, targetTime time.Time) (*price.BackfillJob, error) {
	f.calls = append(f.calls, jobEnqueueCall{assetID: assetID, targetTime: targetTime})
	return &price.BackfillJob{ID: uuid.New(), AssetID: assetID, TargetTime: targetTime}, nil
}

// assetIDs returns the asset ids of every enqueue call, in call order.
func (f *fakeJobEnqueuer) assetIDs() []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(f.calls))
	for _, c := range f.calls {
		ids = append(ids, c.assetID)
	}
	return ids
}

// compile-time interface check
var _ sync.JobEnqueuer = (*fakeJobEnqueuer)(nil)

// newBackfillTestBuilder wires a TxBuilder with a registry and an enqueuer,
// which after #59 is the whole of the backfill path: the registry mints the
// identity and the enqueuer is keyed on it. There is no third participant —
// the legacy `assets`-table upserter that used to sit between them is gone
// along with its table.
func newBackfillTestBuilder(t *testing.T) (*sync.TxBuilder, *fakeAssetRegistry, *fakeJobEnqueuer, *MockLedgerService) {
	t.Helper()
	log := logger.New("test", os.Stdout)

	registry := newFakeAssetRegistry()
	enqueuer := &fakeJobEnqueuer{}

	walletRepo := new(MockWalletRepository)
	// Counterparties in these fixtures are outside addresses, so the
	// internal-transfer probe finds nothing.
	walletRepo.On("GetWalletsByAddressAndUserID", mock.Anything, mock.Anything, mock.Anything).
		Return([]*wallet.Wallet{}, nil).Maybe()

	ledgerSvc := new(MockLedgerService)
	ledgerSvc.On("RecordTransaction", mock.Anything, mock.Anything, "noves",
		mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	builder := sync.NewTxBuilder(walletRepo, ledgerSvc, nil, nil, log, enqueuer, registry, nil)
	return builder, registry, enqueuer, ledgerSvc
}

// =============================================================================
// Tests
// =============================================================================

// TestTxBuilder_MissingPrice_ResolvesAndEnqueues verifies that when a decoded
// transfer has no USD price, the builder:
//  1. Resolves the leg to a registry UUID keyed on (chain, contract).
//  2. Calls JobEnqueuer.Enqueue with THAT UUID and the tx timestamp.
//  3. Omits "usd_price" from the built transfer map so downstream readers treat
//     the transfer as pending (USDRate = nil).
//
// Step 1 used to be an upsert into the `assets` table keyed on the contract
// alone; the job now rides the same registry id the ledger entry carries, so a
// job cannot name an asset the ledger does not know (#59).
func TestTxBuilder_MissingPrice_ResolvesAndEnqueues(t *testing.T) {
	ctx := context.Background()

	contractAddr := "0xabc1234567890123456789012345678901234567"
	txTime := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)

	processor, registry, enqueuer, ledgerSvc := newBackfillTestBuilder(t)
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"
	w := newTestWallet(userID, walletAddr)

	// Build a swap with one OUT transfer (priced) and one IN transfer (no price).
	tx := sync.DecodedTransaction{
		ID:            "ext-tx-backfill-test",
		TxHash:        "0xdeadbeef",
		ChainID:       "ethereum",
		OperationType: sync.OpTrade,
		Transfers: []sync.DecodedTransfer{
			{
				AssetSymbol:     "ETH",
				ContractAddress: "",
				Decimals:        18,
				Amount:          big.NewInt(1000000000000000000),
				Direction:       sync.DirectionOut,
				Sender:          walletAddr,
				Recipient:       "0xrouter",
				USDPrice:        big.NewInt(250000000000), // ETH has a price
			},
			{
				AssetSymbol:     "FOO",
				ContractAddress: contractAddr,
				Decimals:        18,
				Amount:          big.NewInt(5000000000000000000),
				Direction:       sync.DirectionIn,
				Sender:          "0xrouter",
				Recipient:       walletAddr,
				USDPrice:        nil, // No provider price for this token
			},
		},
		MinedAt: txTime,
		Status:  "confirmed",
	}

	_, err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	// 1 + 2. The job must carry the registry id minted for (ethereum, FOO's
	// contract) — not a fresh id and not the priced ETH leg's.
	fooID := registry.idFor(t, "ethereum", contractAddr)
	require.Len(t, enqueuer.calls, 1, "Enqueue should be called once, for the unpriced leg")
	assert.Equal(t, fooID, enqueuer.calls[0].assetID)
	assert.Equal(t, txTime, enqueuer.calls[0].targetTime)

	// 3. The transfer map for FOO must omit usd_price entirely so downstream
	//    readers treat it as pending (USDRate = nil).
	require.Len(t, ledgerSvc.recordedTransactions, 1)
	rawData := ledgerSvc.recordedTransactions[0].RawData
	transfersIn := rawData["transfers_in"].([]map[string]interface{})
	require.Len(t, transfersIn, 1)
	fooTransfer := transfersIn[0]
	assert.Equal(t, "FOO", fooTransfer["asset_symbol"])
	assert.Equal(t, fooID.String(), fooTransfer["asset_id"],
		"the leg's identity must travel as the registry UUID")
	_, hasPrice := fooTransfer["usd_price"]
	assert.False(t, hasPrice, "usd_price key should be absent when no price available")
}

// TestTxBuilder_MissingPrice_NativeToken_EnqueuesJob pins the INVERSION made by
// issue #59 (decision #39): an unpriced NATIVE leg now gets a backfill job.
//
// This assertion used to read the other way — "Enqueue should NOT be called for
// native coins" — and that was not a policy, it was a limitation leaking into
// the test. The old path upserted into the `assets` table, whose partial unique
// index could not represent a coin with no contract, so it returned early and
// the native leg was skipped. The consequence was that gas, and the largest
// position in most wallets, could never be priced, and the resulting zero flowed
// into cost basis and PnL.
//
// The registry keys a native coin as (chain, 'native') like any other asset, so
// the skip has nothing left to protect and is gone.
func TestTxBuilder_MissingPrice_NativeToken_EnqueuesJob(t *testing.T) {
	ctx := context.Background()

	processor, registry, enqueuer, _ := newBackfillTestBuilder(t)
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"
	w := newTestWallet(userID, walletAddr)

	tx := sync.DecodedTransaction{
		ID:            "ext-tx-native-no-price",
		TxHash:        "0xdeadbeef2",
		ChainID:       "ethereum",
		OperationType: sync.OpTrade,
		Transfers: []sync.DecodedTransfer{
			{
				AssetSymbol:     "USDC",
				ContractAddress: "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
				Decimals:        6,
				Amount:          big.NewInt(2000000000),
				Direction:       sync.DirectionOut,
				Sender:          walletAddr,
				Recipient:       "0xrouter",
				USDPrice:        big.NewInt(100000000),
			},
			{
				AssetSymbol:     "ETH",
				ContractAddress: sync.NativeContract, // native — the sentinel, not an address
				Decimals:        18,
				Amount:          big.NewInt(1000000000000000000),
				Direction:       sync.DirectionIn,
				Sender:          "0xrouter",
				Recipient:       walletAddr,
				USDPrice:        nil, // no price
			},
		},
		MinedAt: time.Now(),
		Status:  "confirmed",
	}

	_, err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	nativeID := registry.idFor(t, "ethereum", sync.NativeContract)
	require.Len(t, enqueuer.calls, 1,
		"a native coin must get a backfill job like any other asset (#59)")
	assert.Equal(t, nativeID, enqueuer.calls[0].assetID)
}

// TestTxBuilder_OutgoingOnly_MissingPrice_EnqueuesJob verifies that a
// standalone transfer_out of an unpriced token (no matching incoming transfer)
// still enqueues a price-backfill job. Prior to the fix, the enqueue lived
// only in buildSingleTransfer (used by swaps) — standalone transfer_out went
// through buildTransferOutData and dropped the job, so the pending disposal
// was stuck at proceeds_status='pending' indefinitely.
func TestTxBuilder_OutgoingOnly_MissingPrice_EnqueuesJob(t *testing.T) {
	ctx := context.Background()

	contractAddr := "0xbad1234567890123456789012345678901234567"
	txTime := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)

	processor, registry, enqueuer, _ := newBackfillTestBuilder(t)
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"
	w := newTestWallet(userID, walletAddr)

	// Outgoing-only: one OUT transfer, no matching IN transfer.
	tx := sync.DecodedTransaction{
		ID:            "ext-tx-out-only-nopri",
		TxHash:        "0xdeadbeef3",
		ChainID:       "ethereum",
		OperationType: sync.OpSend, // classifies to TransferOut
		Transfers: []sync.DecodedTransfer{
			{
				AssetSymbol:     "BAD",
				ContractAddress: contractAddr,
				Decimals:        18,
				Amount:          big.NewInt(5000000000000000000),
				Direction:       sync.DirectionOut,
				Sender:          walletAddr,
				Recipient:       "0xcounterparty",
				USDPrice:        nil, // no price
			},
		},
		MinedAt: txTime,
		Status:  "confirmed",
	}

	_, err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	// The leg must have been resolved, and the job must carry that identity.
	badID := registry.idFor(t, "ethereum", contractAddr)
	require.Len(t, enqueuer.calls, 1, "Enqueue should be called for outgoing unpriced transfer")
	assert.Equal(t, badID, enqueuer.calls[0].assetID)
	assert.Equal(t, txTime, enqueuer.calls[0].targetTime)
}

// TestTxBuilder_IncomingOnly_MissingPrice_EnqueuesJob is the symmetric
// case: transfer_in of an unpriced token also needs to enqueue a job. Prior
// to the fix, buildTransferInData silently dropped the price without
// registering the asset / enqueuing a job.
func TestTxBuilder_IncomingOnly_MissingPrice_EnqueuesJob(t *testing.T) {
	ctx := context.Background()

	contractAddr := "0xcafe1234567890123456789012345678901234ab"
	txTime := time.Date(2024, 7, 1, 9, 0, 0, 0, time.UTC)

	processor, registry, enqueuer, _ := newBackfillTestBuilder(t)
	userID := uuid.New()
	walletAddr := "0x2222222222222222222222222222222222222222"
	w := newTestWallet(userID, walletAddr)

	tx := sync.DecodedTransaction{
		ID:            "ext-tx-in-only-nopri",
		TxHash:        "0xbeefcafe",
		ChainID:       "ethereum",
		OperationType: sync.OpReceive,
		Transfers: []sync.DecodedTransfer{
			{
				AssetSymbol:     "CAFE",
				ContractAddress: contractAddr,
				Decimals:        18,
				Amount:          big.NewInt(1000000000000000000),
				Direction:       sync.DirectionIn,
				Sender:          "0xsender",
				Recipient:       walletAddr,
				USDPrice:        nil,
			},
		},
		MinedAt: txTime,
		Status:  "confirmed",
	}

	_, err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	cafeID := registry.idFor(t, "ethereum", contractAddr)
	require.Len(t, enqueuer.calls, 1, "Enqueue should be called for incoming unpriced transfer")
	assert.Equal(t, cafeID, enqueuer.calls[0].assetID)
	assert.Equal(t, txTime, enqueuer.calls[0].targetTime)
}

// TestTxBuilder_SameAssetTwice_EnqueuesOnePerLeg documents that the enqueue is
// keyed on the resolved identity, so two unpriced legs of the SAME asset agree
// on the id they enqueue against.
//
// Enqueue is idempotent on (asset_id, target_time), so the duplicate call is
// harmless; what matters — and what a symbol key could not guarantee — is that
// both legs name the same asset.
func TestTxBuilder_SameAssetTwice_EnqueuesOnePerLeg(t *testing.T) {
	ctx := context.Background()

	contractAddr := "0xfeed1234567890123456789012345678901234ab"
	txTime := time.Date(2024, 8, 1, 9, 0, 0, 0, time.UTC)

	processor, registry, enqueuer, _ := newBackfillTestBuilder(t)
	userID := uuid.New()
	walletAddr := "0x3333333333333333333333333333333333333333"
	w := newTestWallet(userID, walletAddr)

	tx := sync.DecodedTransaction{
		ID:            "ext-tx-same-asset-twice",
		TxHash:        "0xfeedbeef",
		ChainID:       "ethereum",
		OperationType: sync.OpTrade,
		Transfers: []sync.DecodedTransfer{
			{
				AssetSymbol:     "FEED",
				ContractAddress: contractAddr,
				Decimals:        18,
				Amount:          big.NewInt(1000000000000000000),
				Direction:       sync.DirectionOut,
				Sender:          walletAddr,
				Recipient:       "0xrouter",
				USDPrice:        nil,
			},
			{
				AssetSymbol:     "FEED",
				ContractAddress: contractAddr,
				Decimals:        18,
				Amount:          big.NewInt(2000000000000000000),
				Direction:       sync.DirectionIn,
				Sender:          "0xrouter",
				Recipient:       walletAddr,
				USDPrice:        nil,
			},
		},
		MinedAt: txTime,
		Status:  "confirmed",
	}

	_, err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	feedID := registry.idFor(t, "ethereum", contractAddr)
	require.NotEmpty(t, enqueuer.calls, "unpriced legs must enqueue")
	for _, got := range enqueuer.assetIDs() {
		assert.Equal(t, feedID, got,
			"every leg of the same (chain, contract) must enqueue against one identity")
	}
}
