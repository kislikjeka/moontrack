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

func newTestCollector(
	provider pkgsync.TransactionDataProvider,
	rawTxRepo pkgsync.RawTransactionRepository,
	walletRepo pkgsync.WalletRepository,
	assetRepo pkgsync.SyncAssetRepository,
) *pkgsync.Collector {
	log := logger.New("test", os.Stdout)
	config := pkgsync.DefaultConfig()
	return pkgsync.NewCollector(provider, rawTxRepo, walletRepo, assetRepo, config, log)
}

func TestCollectAll_ExtractAssets_UpsertsUniqueAssets(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	w := &wallet.Wallet{
		ID:      walletID,
		Address: walletAddr,
	}

	provider := new(MockTransactionDataProvider)
	rawTxRepo := new(MockRawTransactionRepository)
	walletRepo := new(MockWalletRepository)
	assetRepo := new(MockSyncAssetRepository)

	walletRepo.On("SetSyncPhase", ctx, walletID, mock.Anything).Return(nil)

	// Two transactions with the same asset (ETH on ethereum) — should deduplicate
	tx1 := pkgsync.DecodedTransaction{
		ID: "tx-1", TxHash: "0xaaa", ChainID: "ethereum",
		OperationType: pkgsync.OpReceive,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "ETH", AssetName: "Ethereum",
			Decimals: 18, Amount: big.NewInt(1e18),
			Direction: pkgsync.DirectionIn,
		}},
		MinedAt: time.Now(), Status: "confirmed",
	}
	tx2 := pkgsync.DecodedTransaction{
		ID: "tx-2", TxHash: "0xbbb", ChainID: "ethereum",
		OperationType: pkgsync.OpReceive,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "ETH", AssetName: "Ethereum",
			Decimals: 18, Amount: big.NewInt(2e18),
			Direction: pkgsync.DirectionIn,
		}},
		Fee: &pkgsync.DecodedFee{
			AssetSymbol: "ETH", AssetName: "Ethereum",
			Decimals: 18, Amount: big.NewInt(1e15),
		},
		MinedAt: time.Now(), Status: "confirmed",
	}
	// Third transaction with different asset (USDC) on same chain
	tx3 := pkgsync.DecodedTransaction{
		ID: "tx-3", TxHash: "0xccc", ChainID: "ethereum",
		OperationType: pkgsync.OpReceive,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "USDC", AssetName: "USD Coin",
			ContractAddress: "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
			Decimals:        6, Amount: big.NewInt(1_000_000),
			Direction: pkgsync.DirectionIn,
		}},
		MinedAt: time.Now(), Status: "confirmed",
	}

	expectChainTxs(provider, ctx, walletAddr, mock.Anything, []pkgsync.DecodedTransaction{tx1, tx2, tx3})

	// Expect exactly 2 Upsert calls: one for ETH, one for USDC (deduplicated)
	assetRepo.On("Upsert", ctx, mock.MatchedBy(func(a *pkgsync.SyncAsset) bool {
		return a.Symbol == "ETH" && a.ChainID == "ethereum"
	})).Return(nil).Once()
	assetRepo.On("Upsert", ctx, mock.MatchedBy(func(a *pkgsync.SyncAsset) bool {
		return a.Symbol == "USDC" && a.ChainID == "ethereum"
	})).Return(nil).Once()

	rawTxRepo.On("UpsertRawTransaction", ctx, mock.Anything).Return(nil)
	walletRepo.On("SetCollectCursor", ctx, walletID, mock.Anything).Return(nil)

	collector := newTestCollector(provider, rawTxRepo, walletRepo, assetRepo)
	count, err := collector.CollectAll(ctx, w)

	require.NoError(t, err)
	assert.Equal(t, 3, count)
	assetRepo.AssertNumberOfCalls(t, "Upsert", 2)
}

func TestCollectAll_ExtractAssets_IncludesFeeAssets(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	walletAddr := "0x2222222222222222222222222222222222222222"

	w := &wallet.Wallet{
		ID:      walletID,
		Address: walletAddr,
	}

	provider := new(MockTransactionDataProvider)
	rawTxRepo := new(MockRawTransactionRepository)
	walletRepo := new(MockWalletRepository)
	assetRepo := new(MockSyncAssetRepository)

	walletRepo.On("SetSyncPhase", ctx, walletID, mock.Anything).Return(nil)

	// Transaction with USDC transfer and ETH fee — both should be upserted
	tx := pkgsync.DecodedTransaction{
		ID: "tx-1", TxHash: "0xaaa", ChainID: "ethereum",
		OperationType: pkgsync.OpSend,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "USDC", Decimals: 6, Amount: big.NewInt(1_000_000),
			Direction: pkgsync.DirectionOut,
		}},
		Fee: &pkgsync.DecodedFee{
			AssetSymbol: "ETH", Decimals: 18, Amount: big.NewInt(1e15),
		},
		MinedAt: time.Now(), Status: "confirmed",
	}

	expectChainTxs(provider, ctx, walletAddr, mock.Anything, []pkgsync.DecodedTransaction{tx})

	assetRepo.On("Upsert", ctx, mock.Anything).Return(nil)
	rawTxRepo.On("UpsertRawTransaction", ctx, mock.Anything).Return(nil)
	walletRepo.On("SetCollectCursor", ctx, walletID, mock.Anything).Return(nil)

	collector := newTestCollector(provider, rawTxRepo, walletRepo, assetRepo)
	_, err := collector.CollectAll(ctx, w)
	require.NoError(t, err)

	// Should have 2 Upsert calls: USDC (transfer) + ETH (fee)
	assetRepo.AssertNumberOfCalls(t, "Upsert", 2)
}

func TestCollectAll_ExtractAssets_NilRepo_NoOp(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	walletAddr := "0x3333333333333333333333333333333333333333"

	w := &wallet.Wallet{
		ID:      walletID,
		Address: walletAddr,
	}

	provider := new(MockTransactionDataProvider)
	rawTxRepo := new(MockRawTransactionRepository)
	walletRepo := new(MockWalletRepository)

	walletRepo.On("SetSyncPhase", ctx, walletID, mock.Anything).Return(nil)

	tx := pkgsync.DecodedTransaction{
		ID: "tx-1", TxHash: "0xaaa", ChainID: "ethereum",
		OperationType: pkgsync.OpReceive,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "ETH", Decimals: 18, Amount: big.NewInt(1e18),
			Direction: pkgsync.DirectionIn,
		}},
		MinedAt: time.Now(), Status: "confirmed",
	}

	expectChainTxs(provider, ctx, walletAddr, mock.Anything, []pkgsync.DecodedTransaction{tx})
	rawTxRepo.On("UpsertRawTransaction", ctx, mock.Anything).Return(nil)
	walletRepo.On("SetCollectCursor", ctx, walletID, mock.Anything).Return(nil)

	// nil assetRepo — should not panic
	collector := newTestCollector(provider, rawTxRepo, walletRepo, nil)
	count, err := collector.CollectAll(ctx, w)

	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestCollectAll_ExtractAssets_ZeroDecimalsIncluded(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	walletAddr := "0x4444444444444444444444444444444444444444"

	w := &wallet.Wallet{
		ID:      walletID,
		Address: walletAddr,
	}

	provider := new(MockTransactionDataProvider)
	rawTxRepo := new(MockRawTransactionRepository)
	walletRepo := new(MockWalletRepository)
	assetRepo := new(MockSyncAssetRepository)

	walletRepo.On("SetSyncPhase", ctx, walletID, mock.Anything).Return(nil)

	// Asset with 0 decimals (e.g., CryptoKitties or governance tokens)
	tx := pkgsync.DecodedTransaction{
		ID: "tx-1", TxHash: "0xaaa", ChainID: "ethereum",
		OperationType: pkgsync.OpReceive,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "CK", Decimals: 0, Amount: big.NewInt(1),
			Direction: pkgsync.DirectionIn,
		}},
		MinedAt: time.Now(), Status: "confirmed",
	}

	expectChainTxs(provider, ctx, walletAddr, mock.Anything, []pkgsync.DecodedTransaction{tx})

	assetRepo.On("Upsert", ctx, mock.MatchedBy(func(a *pkgsync.SyncAsset) bool {
		return a.Symbol == "CK" && a.Decimals == 0
	})).Return(nil).Once()
	rawTxRepo.On("UpsertRawTransaction", ctx, mock.Anything).Return(nil)
	walletRepo.On("SetCollectCursor", ctx, walletID, mock.Anything).Return(nil)

	collector := newTestCollector(provider, rawTxRepo, walletRepo, assetRepo)
	_, err := collector.CollectAll(ctx, w)
	require.NoError(t, err)

	// Zero-decimal asset should still be upserted
	assetRepo.AssertNumberOfCalls(t, "Upsert", 1)
}
