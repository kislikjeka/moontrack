package sync_test

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/asset"
	"github.com/kislikjeka/moontrack/internal/platform/price"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// =============================================================================
// Fakes for backfill test
// =============================================================================

type fakeAssetUpserter struct {
	calls []assetUpsertCall
	asset *asset.Asset
	// err, when set, is returned from UpsertByOnChainIdentity with asset=nil.
	err error
}

type assetUpsertCall struct {
	chainID         string
	contractAddress string
	symbol          string
}

func (f *fakeAssetUpserter) UpsertByOnChainIdentity(ctx context.Context, chainID, contractAddress, symbol, name string, decimals int) (*asset.Asset, bool, error) {
	f.calls = append(f.calls, assetUpsertCall{chainID: chainID, contractAddress: contractAddress, symbol: symbol})
	if f.err != nil {
		return nil, false, f.err
	}
	return f.asset, true, nil
}

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

// compile-time interface checks
var _ sync.AssetUpserter = (*fakeAssetUpserter)(nil)
var _ sync.JobEnqueuer = (*fakeJobEnqueuer)(nil)

// =============================================================================
// Tests
// =============================================================================

// TestTxBuilder_MissingPrice_UpsertAndEnqueue verifies that when a decoded
// transfer has no USD price but has an on-chain contract address, the processor:
//  1. Calls AssetUpserter.UpsertByOnChainIdentity with the correct chain + contract.
//  2. Calls JobEnqueuer.Enqueue with the resulting asset UUID and the tx timestamp.
//  3. Omits "usd_price" from the built transfer map so downstream readers treat
//     the transfer as pending (USDRate = nil).
func TestTxBuilder_MissingPrice_UpsertAndEnqueue(t *testing.T) {
	ctx := context.Background()
	log := logger.New("test", os.Stdout)

	assetID := uuid.New()
	contractAddr := "0xabc1234567890123456789012345678901234567"
	txTime := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)

	upsert := &fakeAssetUpserter{
		asset: &asset.Asset{
			ID:     assetID,
			Symbol: "FOO",
			Name:   "Foo Token",
		},
	}
	enqueuer := &fakeJobEnqueuer{}

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeSwap, "zerion",
		mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := sync.NewTxBuilder(walletRepo, ledgerSvc, nil, nil, log, upsert, enqueuer)
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"
	w := newTestWallet(userID, walletAddr)

	// Build a swap with one OUT transfer (priced) and one IN transfer (no price, has contract).
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

	// 1. AssetUpserter must have been called with correct chain + contract
	require.Len(t, upsert.calls, 1, "UpsertByOnChainIdentity should be called once")
	assert.Equal(t, "ethereum", upsert.calls[0].chainID)
	assert.Equal(t, contractAddr, upsert.calls[0].contractAddress)
	assert.Equal(t, "FOO", upsert.calls[0].symbol)

	// 2. JobEnqueuer must have been called with the asset UUID and tx timestamp
	require.Len(t, enqueuer.calls, 1, "Enqueue should be called once")
	assert.Equal(t, assetID, enqueuer.calls[0].assetID)
	assert.Equal(t, txTime, enqueuer.calls[0].targetTime)

	// 3. The transfer map for FOO must omit usd_price entirely so downstream
	//    readers treat it as pending (USDRate = nil).
	require.Len(t, ledgerSvc.recordedTransactions, 1)
	rawData := ledgerSvc.recordedTransactions[0].RawData
	transfersIn := rawData["transfers_in"].([]map[string]interface{})
	require.Len(t, transfersIn, 1)
	fooTransfer := transfersIn[0]
	assert.Equal(t, "FOO", fooTransfer["asset_symbol"])
	_, hasPrice := fooTransfer["usd_price"]
	assert.False(t, hasPrice, "usd_price key should be absent when no price available")
}

// TestTxBuilder_MissingPrice_NativeToken verifies that when a transfer has
// no price AND no contract address (native coin), the processor does NOT call
// the asset upserter (can't upsert by on-chain identity without a contract address).
func TestTxBuilder_MissingPrice_NativeToken(t *testing.T) {
	ctx := context.Background()
	log := logger.New("test", os.Stdout)

	upsert := &fakeAssetUpserter{
		asset: &asset.Asset{ID: uuid.New(), Symbol: "ETH", Name: "Ethereum"},
	}
	enqueuer := &fakeJobEnqueuer{}

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeSwap, "zerion",
		mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := sync.NewTxBuilder(walletRepo, ledgerSvc, nil, nil, log, upsert, enqueuer)
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
				ContractAddress: "0xusdc",
				Decimals:        6,
				Amount:          big.NewInt(2000000000),
				Direction:       sync.DirectionOut,
				Sender:          walletAddr,
				Recipient:       "0xrouter",
				USDPrice:        big.NewInt(100000000),
			},
			{
				AssetSymbol:     "ETH",
				ContractAddress: "", // native — no contract address
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

	// No upsert/enqueue for native tokens without contract address
	assert.Len(t, upsert.calls, 0, "UpsertByOnChainIdentity should NOT be called for native coins")
	assert.Len(t, enqueuer.calls, 0, "Enqueue should NOT be called for native coins")
}

// TestTxBuilder_OutgoingOnly_MissingPrice_EnqueuesJob verifies that a
// standalone transfer_out of an unpriced token (no matching incoming transfer)
// still enqueues a price-backfill job. Prior to the fix, the enqueue lived
// only in buildSingleTransfer (used by swaps) — standalone transfer_out went
// through buildTransferOutData and dropped the job, so the pending disposal
// was stuck at proceeds_status='pending' indefinitely.
func TestTxBuilder_OutgoingOnly_MissingPrice_EnqueuesJob(t *testing.T) {
	ctx := context.Background()
	log := logger.New("test", os.Stdout)

	assetID := uuid.New()
	contractAddr := "0xbad1234567890123456789012345678901234567"
	txTime := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)

	upsert := &fakeAssetUpserter{
		asset: &asset.Asset{ID: assetID, Symbol: "BAD", Name: "Bad Token"},
	}
	enqueuer := &fakeJobEnqueuer{}

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	// detectInternalTransfer calls GetWalletsByAddressAndUserID for the
	// counterparty to see if it belongs to the same user. Return empty so
	// it's treated as an external transfer.
	walletRepo.On("GetWalletsByAddressAndUserID", ctx, mock.Anything, mock.Anything).
		Return([]*wallet.Wallet{}, nil)

	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferOut, "zerion",
		mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := sync.NewTxBuilder(walletRepo, ledgerSvc, nil, nil, log, upsert, enqueuer)
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

	// Upsert must fire so the asset lands in the assets table.
	require.Len(t, upsert.calls, 1, "UpsertByOnChainIdentity should be called for outgoing unpriced transfer")
	assert.Equal(t, "ethereum", upsert.calls[0].chainID)
	assert.Equal(t, contractAddr, upsert.calls[0].contractAddress)
	assert.Equal(t, "BAD", upsert.calls[0].symbol)

	// Enqueue must fire with the resolved asset UUID and the tx timestamp.
	require.Len(t, enqueuer.calls, 1, "Enqueue should be called for outgoing unpriced transfer")
	assert.Equal(t, assetID, enqueuer.calls[0].assetID)
	assert.Equal(t, txTime, enqueuer.calls[0].targetTime)
}

// TestTxBuilder_IncomingOnly_MissingPrice_EnqueuesJob is the symmetric
// case: transfer_in of an unpriced token also needs to enqueue a job. Prior
// to the fix, buildTransferInData silently dropped the price without
// registering the asset / enqueuing a job.
func TestTxBuilder_IncomingOnly_MissingPrice_EnqueuesJob(t *testing.T) {
	ctx := context.Background()
	log := logger.New("test", os.Stdout)

	assetID := uuid.New()
	contractAddr := "0xcafe1234567890123456789012345678901234ab"
	txTime := time.Date(2024, 7, 1, 9, 0, 0, 0, time.UTC)

	upsert := &fakeAssetUpserter{
		asset: &asset.Asset{ID: assetID, Symbol: "CAFE", Name: "Cafe Token"},
	}
	enqueuer := &fakeJobEnqueuer{}

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	walletRepo.On("GetWalletsByAddressAndUserID", ctx, mock.Anything, mock.Anything).
		Return([]*wallet.Wallet{}, nil)

	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferIn, "zerion",
		mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := sync.NewTxBuilder(walletRepo, ledgerSvc, nil, nil, log, upsert, enqueuer)
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

	require.Len(t, upsert.calls, 1, "UpsertByOnChainIdentity should be called for incoming unpriced transfer")
	require.Len(t, enqueuer.calls, 1, "Enqueue should be called for incoming unpriced transfer")
	assert.Equal(t, assetID, enqueuer.calls[0].assetID)
	assert.Equal(t, txTime, enqueuer.calls[0].targetTime)
}

// TestTxBuilder_InvalidContractAddress_NoEnqueue verifies that when
// AssetUpserter returns asset.ErrInvalidContractAddress (the provider emitted a
// malformed/bogus contract address that fails our shape check), the
// processor:
//  1. Does NOT enqueue a backfill job (there is no asset row for the worker
//     to resolve, so a pending lot would be stranded forever).
//  2. Emits no usd_price key (same reason — downstream treats this like a
//     native-coin fallback).
//  3. Still allows the transfer to flow through to the ledger (no panic,
//     no return error).
func TestTxBuilder_InvalidContractAddress_NoEnqueue(t *testing.T) {
	ctx := context.Background()
	log := logger.New("test", os.Stdout)

	// Upserter returns ErrInvalidContractAddress (wrapped to mirror the real
	// repository implementation which wraps with %w).
	upsert := &fakeAssetUpserter{
		err: fmt.Errorf("normalize: %w", asset.ErrInvalidContractAddress),
	}
	enqueuer := &fakeJobEnqueuer{}

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeSwap, "zerion",
		mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := sync.NewTxBuilder(walletRepo, ledgerSvc, nil, nil, log, upsert, enqueuer)
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"
	w := newTestWallet(userID, walletAddr)

	// Swap with a bogus contract address that the repo will reject.
	tx := sync.DecodedTransaction{
		ID:            "ext-tx-invalid-contract",
		TxHash:        "0xdeadbeef3",
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
				USDPrice:        big.NewInt(250000000000),
			},
			{
				AssetSymbol:     "XYZ",
				ContractAddress: "0xGHIJ000000000000000000000000000000000000", // invalid (non-hex)
				Decimals:        18,
				Amount:          big.NewInt(5000000000000000000),
				Direction:       sync.DirectionIn,
				Sender:          "0xrouter",
				Recipient:       walletAddr,
				USDPrice:        nil,
			},
		},
		MinedAt: time.Now(),
		Status:  "confirmed",
	}

	// Must not panic / error out of the processor.
	_, err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	// Upsert was attempted once...
	require.Len(t, upsert.calls, 1)
	// ...but no job was enqueued.
	assert.Len(t, enqueuer.calls, 0,
		"Enqueue must NOT be called when upsert returns ErrInvalidContractAddress")

	// The XYZ transfer must emit no usd_price key — the downstream reader
	// treats the absence as pending/unknown. Since no backfill job was
	// enqueued (invalid contract), the lot will stay unpriced until the
	// user supplies a manual price.
	require.Len(t, ledgerSvc.recordedTransactions, 1)
	rawData := ledgerSvc.recordedTransactions[0].RawData
	transfersIn := rawData["transfers_in"].([]map[string]interface{})
	require.Len(t, transfersIn, 1)
	xyz := transfersIn[0]
	assert.Equal(t, "XYZ", xyz["asset_symbol"])
	_, hasPrice := xyz["usd_price"]
	assert.False(t, hasPrice,
		"usd_price must be absent (no price, no asset)")
}
