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
	"github.com/kislikjeka/moontrack/internal/platform/asset"
	"github.com/kislikjeka/moontrack/internal/platform/price"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// =============================================================================
// Fakes for backfill test
// =============================================================================

type fakeAssetUpserter struct {
	calls []assetUpsertCall
	asset *asset.Asset
}

type assetUpsertCall struct {
	chainID         string
	contractAddress string
	symbol          string
}

func (f *fakeAssetUpserter) UpsertByOnChainIdentity(ctx context.Context, chainID, contractAddress, symbol, name string, decimals int) (*asset.Asset, bool, error) {
	f.calls = append(f.calls, assetUpsertCall{chainID: chainID, contractAddress: contractAddress, symbol: symbol})
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

// TestZerionProcessor_MissingPrice_UpsertAndEnqueue verifies that when a decoded
// transfer has no USD price but has an on-chain contract address, the processor:
// 1. Calls AssetUpserter.UpsertByOnChainIdentity with the correct chain + contract.
// 2. Calls JobEnqueuer.Enqueue with the resulting asset UUID and the tx timestamp.
// 3. Omits "usd_price" from the built transfer map (sets usd_price_pending instead).
func TestZerionProcessor_MissingPrice_UpsertAndEnqueue(t *testing.T) {
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

	processor := sync.NewZerionProcessor(walletRepo, ledgerSvc, nil, nil, log, upsert, enqueuer)
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"
	w := newTestWallet(userID, walletAddr)

	// Build a swap with one OUT transfer (priced) and one IN transfer (no price, has contract).
	tx := sync.DecodedTransaction{
		ID:            "zerion-tx-backfill-test",
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
				USDPrice:        nil, // No Zerion price for this token
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

	// 3. The transfer map for FOO should have usd_price_pending=true and no usd_price key
	require.Len(t, ledgerSvc.recordedTransactions, 1)
	rawData := ledgerSvc.recordedTransactions[0].RawData
	transfersIn := rawData["transfers_in"].([]map[string]interface{})
	require.Len(t, transfersIn, 1)
	fooTransfer := transfersIn[0]
	assert.Equal(t, "FOO", fooTransfer["asset_symbol"])
	_, hasPrice := fooTransfer["usd_price"]
	assert.False(t, hasPrice, "usd_price key should be absent when no price available")
	assert.Equal(t, true, fooTransfer["usd_price_pending"])
}

// TestZerionProcessor_MissingPrice_NativeToken verifies that when a transfer has
// no price AND no contract address (native coin), the processor does NOT call
// the asset upserter (can't upsert by on-chain identity without a contract address).
func TestZerionProcessor_MissingPrice_NativeToken(t *testing.T) {
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

	processor := sync.NewZerionProcessor(walletRepo, ledgerSvc, nil, nil, log, upsert, enqueuer)
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"
	w := newTestWallet(userID, walletAddr)

	tx := sync.DecodedTransaction{
		ID:            "zerion-tx-native-no-price",
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
