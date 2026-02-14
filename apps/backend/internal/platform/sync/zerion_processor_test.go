package sync_test

import (
	"context"
	"log/slog"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
)

// =============================================================================
// Zerion Test Helpers
// =============================================================================

func newZerionProcessor(walletRepo sync.WalletRepository, ledgerSvc sync.LedgerService) *sync.ZerionProcessor {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	return sync.NewZerionProcessor(walletRepo, ledgerSvc, logger)
}

func newDecodedTransaction(opType sync.OperationType, transfers []sync.DecodedTransfer) sync.DecodedTransaction {
	return sync.DecodedTransaction{
		ID:            "zerion-tx-" + uuid.New().String()[:8],
		TxHash:        "0x" + uuid.New().String()[:32],
		ChainID:       1,
		OperationType: opType,
		Transfers:     transfers,
		MinedAt:       time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC),
		Status:        "confirmed",
	}
}

func newIncomingTransfer(sender string) sync.DecodedTransfer {
	return sync.DecodedTransfer{
		AssetSymbol:     "ETH",
		ContractAddress: "",
		Decimals:        18,
		Amount:          big.NewInt(1000000000000000000),
		Direction:       sync.DirectionIn,
		Sender:          sender,
		Recipient:       "0x1111111111111111111111111111111111111111",
		USDPrice:        big.NewInt(250000000000), // $2500 scaled by 1e8
	}
}

func newOutgoingTransfer(recipient string) sync.DecodedTransfer {
	return sync.DecodedTransfer{
		AssetSymbol:     "ETH",
		ContractAddress: "",
		Decimals:        18,
		Amount:          big.NewInt(1000000000000000000),
		Direction:       sync.DirectionOut,
		Sender:          "0x1111111111111111111111111111111111111111",
		Recipient:       recipient,
		USDPrice:        big.NewInt(250000000000),
	}
}

// =============================================================================
// Tests
// =============================================================================

func TestZerionProcessor_TransferIn(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"
	externalAddr := "0x9999999999999999999999999999999999999999"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	walletRepo.On("GetWalletsByAddressAndUserID", ctx, externalAddr, userID).Return([]*wallet.Wallet{}, nil)

	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferIn, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := newZerionProcessor(walletRepo, ledgerSvc)
	w := newTestWallet(userID, walletAddr, 1)

	tx := newDecodedTransaction(sync.OpReceive, []sync.DecodedTransfer{
		newIncomingTransfer(externalAddr),
	})

	err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, ledger.TxTypeTransferIn, ledgerSvc.recordedTransactions[0].TxType)
	assert.Equal(t, "zerion", ledgerSvc.recordedTransactions[0].Source)
	assert.Equal(t, tx.ID, *ledgerSvc.recordedTransactions[0].ExternalID)

	rawData := ledgerSvc.recordedTransactions[0].RawData
	assert.Equal(t, w.ID.String(), rawData["wallet_id"])
	assert.Equal(t, tx.TxHash, rawData["tx_hash"])
	assert.Equal(t, int64(1), rawData["chain_id"])

	transfers := rawData["transfers"].([]map[string]interface{})
	require.Len(t, transfers, 1)
	assert.Equal(t, "ETH", transfers[0]["asset_symbol"])
	assert.Equal(t, "in", transfers[0]["direction"])
}

func TestZerionProcessor_TransferOut(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"
	externalAddr := "0x9999999999999999999999999999999999999999"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	walletRepo.On("GetWalletsByAddressAndUserID", ctx, externalAddr, userID).Return([]*wallet.Wallet{}, nil)

	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferOut, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := newZerionProcessor(walletRepo, ledgerSvc)
	w := newTestWallet(userID, walletAddr, 1)

	tx := newDecodedTransaction(sync.OpSend, []sync.DecodedTransfer{
		newOutgoingTransfer(externalAddr),
	})

	err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, ledger.TxTypeTransferOut, ledgerSvc.recordedTransactions[0].TxType)
	assert.Equal(t, "zerion", ledgerSvc.recordedTransactions[0].Source)
}

func TestZerionProcessor_Swap(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeSwap, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := newZerionProcessor(walletRepo, ledgerSvc)
	w := newTestWallet(userID, walletAddr, 1)

	tx := newDecodedTransaction(sync.OpTrade, []sync.DecodedTransfer{
		{
			AssetSymbol: "ETH",
			Decimals:    18,
			Amount:      big.NewInt(1000000000000000000),
			Direction:   sync.DirectionOut,
			Sender:      walletAddr,
			Recipient:   "0xrouter",
			USDPrice:    big.NewInt(250000000000),
		},
		{
			AssetSymbol:     "USDC",
			ContractAddress: "0xusdc",
			Decimals:        6,
			Amount:          big.NewInt(2500000000),
			Direction:       sync.DirectionIn,
			Sender:          "0xrouter",
			Recipient:       walletAddr,
			USDPrice:        big.NewInt(100000000),
		},
	})

	err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, ledger.TxTypeSwap, ledgerSvc.recordedTransactions[0].TxType)

	rawData := ledgerSvc.recordedTransactions[0].RawData
	transfersIn := rawData["transfers_in"].([]map[string]interface{})
	transfersOut := rawData["transfers_out"].([]map[string]interface{})

	require.Len(t, transfersIn, 1)
	require.Len(t, transfersOut, 1)
	assert.Equal(t, "USDC", transfersIn[0]["asset_symbol"])
	assert.Equal(t, "ETH", transfersOut[0]["asset_symbol"])
}

func TestZerionProcessor_InternalTransfer(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	sourceAddr := "0x1111111111111111111111111111111111111111"
	destAddr := "0x2222222222222222222222222222222222222222"
	destWalletID := uuid.New()

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	walletRepo.On("GetWalletsByAddressAndUserID", ctx, destAddr, userID).Return([]*wallet.Wallet{
		{ID: destWalletID, UserID: userID, Address: destAddr, ChainID: 1},
	}, nil)

	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeInternalTransfer, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := newZerionProcessor(walletRepo, ledgerSvc)
	sourceWallet := newTestWallet(userID, sourceAddr, 1)

	tx := newDecodedTransaction(sync.OpSend, []sync.DecodedTransfer{
		newOutgoingTransfer(destAddr),
	})

	err := processor.ProcessTransaction(ctx, sourceWallet, tx)
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, ledger.TxTypeInternalTransfer, ledgerSvc.recordedTransactions[0].TxType)

	rawData := ledgerSvc.recordedTransactions[0].RawData
	assert.Equal(t, sourceWallet.ID.String(), rawData["source_wallet_id"])
	assert.Equal(t, destWalletID.String(), rawData["dest_wallet_id"])
}

func TestZerionProcessor_InternalTransfer_IncomingSkipped(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	sourceAddr := "0x1111111111111111111111111111111111111111"
	destAddr := "0x2222222222222222222222222222222222222222"
	sourceWalletID := uuid.New()

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	walletRepo.On("GetWalletsByAddressAndUserID", ctx, sourceAddr, userID).Return([]*wallet.Wallet{
		{ID: sourceWalletID, UserID: userID, Address: sourceAddr, ChainID: 1},
	}, nil)

	processor := newZerionProcessor(walletRepo, ledgerSvc)
	destWallet := newTestWallet(userID, destAddr, 1)

	transfer := newIncomingTransfer(sourceAddr)
	transfer.Recipient = destAddr

	tx := newDecodedTransaction(sync.OpReceive, []sync.DecodedTransfer{transfer})

	err := processor.ProcessTransaction(ctx, destWallet, tx)
	require.NoError(t, err)

	assert.Empty(t, ledgerSvc.recordedTransactions)
}

func TestZerionProcessor_ApproveSkipped(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	processor := newZerionProcessor(walletRepo, ledgerSvc)
	w := newTestWallet(userID, walletAddr, 1)

	tx := newDecodedTransaction(sync.OpApprove, nil)

	err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	assert.Empty(t, ledgerSvc.recordedTransactions)
}

func TestZerionProcessor_FailedTxSkipped(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	processor := newZerionProcessor(walletRepo, ledgerSvc)
	w := newTestWallet(userID, walletAddr, 1)

	tx := newDecodedTransaction(sync.OpReceive, []sync.DecodedTransfer{
		newIncomingTransfer("0x9999999999999999999999999999999999999999"),
	})
	tx.Status = "failed"

	err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	assert.Empty(t, ledgerSvc.recordedTransactions)
}

func TestZerionProcessor_DuplicateHandling(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"
	externalAddr := "0x9999999999999999999999999999999999999999"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	walletRepo.On("GetWalletsByAddressAndUserID", ctx, externalAddr, userID).Return([]*wallet.Wallet{}, nil)

	duplicateError := &pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint"}
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferIn, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, duplicateError)

	processor := newZerionProcessor(walletRepo, ledgerSvc)
	w := newTestWallet(userID, walletAddr, 1)

	tx := newDecodedTransaction(sync.OpReceive, []sync.DecodedTransfer{
		newIncomingTransfer(externalAddr),
	})

	err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err, "duplicate error should be silently handled")
}

func TestZerionProcessor_USDPrices(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"
	externalAddr := "0x9999999999999999999999999999999999999999"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	walletRepo.On("GetWalletsByAddressAndUserID", ctx, externalAddr, userID).Return([]*wallet.Wallet{}, nil)
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferIn, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := newZerionProcessor(walletRepo, ledgerSvc)
	w := newTestWallet(userID, walletAddr, 1)

	ethPrice := big.NewInt(250000000000)
	transfer := sync.DecodedTransfer{
		AssetSymbol: "ETH",
		Decimals:    18,
		Amount:      big.NewInt(1000000000000000000),
		Direction:   sync.DirectionIn,
		Sender:      externalAddr,
		Recipient:   walletAddr,
		USDPrice:    ethPrice,
	}
	tx := newDecodedTransaction(sync.OpReceive, []sync.DecodedTransfer{transfer})

	err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	rawData := ledgerSvc.recordedTransactions[0].RawData
	transfers := rawData["transfers"].([]map[string]interface{})
	require.Len(t, transfers, 1)
	assert.Equal(t, ethPrice.String(), transfers[0]["usd_price"])
}

func TestZerionProcessor_GasFee(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"
	externalAddr := "0x9999999999999999999999999999999999999999"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	walletRepo.On("GetWalletsByAddressAndUserID", ctx, externalAddr, userID).Return([]*wallet.Wallet{}, nil)
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferOut, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := newZerionProcessor(walletRepo, ledgerSvc)
	w := newTestWallet(userID, walletAddr, 1)

	feeUSDPrice := big.NewInt(500000000) // $5
	tx := newDecodedTransaction(sync.OpSend, []sync.DecodedTransfer{
		newOutgoingTransfer(externalAddr),
	})
	tx.Fee = &sync.DecodedFee{
		AssetSymbol: "ETH",
		Amount:      big.NewInt(21000000000000), // 0.000021 ETH
		Decimals:    18,
		USDPrice:    feeUSDPrice,
	}

	err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	rawData := ledgerSvc.recordedTransactions[0].RawData

	assert.Equal(t, "ETH", rawData["fee_asset"])
	assert.Equal(t, "21000000000000", rawData["fee_amount"])
	assert.Equal(t, 18, rawData["fee_decimals"])
	assert.Equal(t, feeUSDPrice.String(), rawData["fee_usd_price"])
}

func TestZerionProcessor_DeFiDeposit(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeDefiDeposit, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := newZerionProcessor(walletRepo, ledgerSvc)
	w := newTestWallet(userID, walletAddr, 1)

	tx := newDecodedTransaction(sync.OpDeposit, []sync.DecodedTransfer{
		{
			AssetSymbol: "ETH",
			Decimals:    18,
			Amount:      big.NewInt(5000000000000000000),
			Direction:   sync.DirectionOut,
			Sender:      walletAddr,
			Recipient:   "0xaavepool",
			USDPrice:    big.NewInt(250000000000),
		},
	})
	tx.Protocol = "Aave V3"

	err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, ledger.TxTypeDefiDeposit, ledgerSvc.recordedTransactions[0].TxType)

	rawData := ledgerSvc.recordedTransactions[0].RawData
	assert.Equal(t, "Aave V3", rawData["protocol"])
}

func TestZerionProcessor_DeFiWithdraw(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeDefiWithdraw, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := newZerionProcessor(walletRepo, ledgerSvc)
	w := newTestWallet(userID, walletAddr, 1)

	tx := newDecodedTransaction(sync.OpWithdraw, []sync.DecodedTransfer{
		{
			AssetSymbol: "ETH",
			Decimals:    18,
			Amount:      big.NewInt(5000000000000000000),
			Direction:   sync.DirectionIn,
			Sender:      "0xaavepool",
			Recipient:   walletAddr,
			USDPrice:    big.NewInt(250000000000),
		},
	})
	tx.Protocol = "Aave V3"

	err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, ledger.TxTypeDefiWithdraw, ledgerSvc.recordedTransactions[0].TxType)

	rawData := ledgerSvc.recordedTransactions[0].RawData
	assert.Equal(t, "Aave V3", rawData["protocol"])
}

func TestZerionProcessor_DeFiClaim(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeDefiClaim, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := newZerionProcessor(walletRepo, ledgerSvc)
	w := newTestWallet(userID, walletAddr, 1)

	tx := newDecodedTransaction(sync.OpClaim, []sync.DecodedTransfer{
		{
			AssetSymbol:     "AAVE",
			ContractAddress: "0xaavetoken",
			Decimals:        18,
			Amount:          big.NewInt(100000000000000000),
			Direction:       sync.DirectionIn,
			Sender:          "0xrewards",
			Recipient:       walletAddr,
			USDPrice:        big.NewInt(8000000000),
		},
	})
	tx.Protocol = "Aave V3"

	err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, ledger.TxTypeDefiClaim, ledgerSvc.recordedTransactions[0].TxType)

	rawData := ledgerSvc.recordedTransactions[0].RawData
	assert.Equal(t, "Aave V3", rawData["protocol"])

	transfers := rawData["transfers"].([]map[string]interface{})
	require.Len(t, transfers, 1)
	assert.Equal(t, "AAVE", transfers[0]["asset_symbol"])
}
