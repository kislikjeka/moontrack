package sync_test

import (
	"context"
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
	"github.com/kislikjeka/moontrack/internal/platform/price"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// =============================================================================
// TxBuilder Test Helpers
// =============================================================================

func newTxBuilder(walletRepo sync.WalletRepository, ledgerSvc sync.LedgerService) *sync.TxBuilder {
	log := logger.New("test", os.Stdout)
	return sync.NewTxBuilder(walletRepo, ledgerSvc, nil, nil, log, nil, nil)
}

func newDecodedTransaction(opType sync.OperationType, transfers []sync.DecodedTransfer) sync.DecodedTransaction {
	return sync.DecodedTransaction{
		ID:            "ext-tx-" + uuid.New().String()[:8],
		TxHash:        "0x" + uuid.New().String()[:32],
		ChainID:       "ethereum",
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

func TestTxBuilder_TransferIn(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"
	externalAddr := "0x9999999999999999999999999999999999999999"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	walletRepo.On("GetWalletsByAddressAndUserID", ctx, externalAddr, userID).Return([]*wallet.Wallet{}, nil)

	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferIn, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := newTxBuilder(walletRepo, ledgerSvc)
	w := newTestWallet(userID, walletAddr)

	tx := newDecodedTransaction(sync.OpReceive, []sync.DecodedTransfer{
		newIncomingTransfer(externalAddr),
	})

	_, err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, ledger.TxTypeTransferIn, ledgerSvc.recordedTransactions[0].TxType)
	assert.Equal(t, "zerion", ledgerSvc.recordedTransactions[0].Source)
	assert.Equal(t, tx.ID, *ledgerSvc.recordedTransactions[0].ExternalID)

	rawData := ledgerSvc.recordedTransactions[0].RawData
	assert.Equal(t, w.ID.String(), rawData["wallet_id"])
	assert.Equal(t, tx.TxHash, rawData["tx_hash"])
	assert.Equal(t, "ethereum", rawData["chain_id"])

	assert.Equal(t, "ETH", rawData["asset_id"])
	assert.Equal(t, "1000000000000000000", rawData["amount"])
	assert.Equal(t, 18, rawData["decimals"])
	assert.Equal(t, externalAddr, rawData["from_address"])
	assert.Equal(t, tx.ID, rawData["unique_id"])
}

func TestTxBuilder_TransferOut(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"
	externalAddr := "0x9999999999999999999999999999999999999999"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	walletRepo.On("GetWalletsByAddressAndUserID", ctx, externalAddr, userID).Return([]*wallet.Wallet{}, nil)

	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferOut, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := newTxBuilder(walletRepo, ledgerSvc)
	w := newTestWallet(userID, walletAddr)

	tx := newDecodedTransaction(sync.OpSend, []sync.DecodedTransfer{
		newOutgoingTransfer(externalAddr),
	})

	_, err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, ledger.TxTypeTransferOut, ledgerSvc.recordedTransactions[0].TxType)
	assert.Equal(t, "zerion", ledgerSvc.recordedTransactions[0].Source)
}

func TestTxBuilder_Swap(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeSwap, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := newTxBuilder(walletRepo, ledgerSvc)
	w := newTestWallet(userID, walletAddr)

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

	_, err := processor.ProcessTransaction(ctx, w, tx)
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

func TestTxBuilder_InternalTransfer(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	sourceAddr := "0x1111111111111111111111111111111111111111"
	destAddr := "0x2222222222222222222222222222222222222222"
	destWalletID := uuid.New()

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	walletRepo.On("GetWalletsByAddressAndUserID", ctx, destAddr, userID).Return([]*wallet.Wallet{
		{ID: destWalletID, UserID: userID, Address: destAddr},
	}, nil)

	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeInternalTransfer, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := newTxBuilder(walletRepo, ledgerSvc)
	sourceWallet := newTestWallet(userID, sourceAddr)

	tx := newDecodedTransaction(sync.OpSend, []sync.DecodedTransfer{
		newOutgoingTransfer(destAddr),
	})

	_, err := processor.ProcessTransaction(ctx, sourceWallet, tx)
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, ledger.TxTypeInternalTransfer, ledgerSvc.recordedTransactions[0].TxType)

	rawData := ledgerSvc.recordedTransactions[0].RawData
	assert.Equal(t, sourceWallet.ID.String(), rawData["source_wallet_id"])
	assert.Equal(t, destWalletID.String(), rawData["dest_wallet_id"])
}

func TestTxBuilder_InternalTransfer_IncomingSkipped(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	sourceAddr := "0x1111111111111111111111111111111111111111"
	destAddr := "0x2222222222222222222222222222222222222222"
	sourceWalletID := uuid.New()

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	walletRepo.On("GetWalletsByAddressAndUserID", ctx, sourceAddr, userID).Return([]*wallet.Wallet{
		{ID: sourceWalletID, UserID: userID, Address: sourceAddr},
	}, nil)

	processor := newTxBuilder(walletRepo, ledgerSvc)
	destWallet := newTestWallet(userID, destAddr)

	transfer := newIncomingTransfer(sourceAddr)
	transfer.Recipient = destAddr

	tx := newDecodedTransaction(sync.OpReceive, []sync.DecodedTransfer{transfer})

	_, err := processor.ProcessTransaction(ctx, destWallet, tx)
	require.NoError(t, err)

	assert.Empty(t, ledgerSvc.recordedTransactions)
}

func TestTxBuilder_ApproveSkipped(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	processor := newTxBuilder(walletRepo, ledgerSvc)
	w := newTestWallet(userID, walletAddr)

	tx := newDecodedTransaction(sync.OpApprove, nil)

	_, err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	assert.Empty(t, ledgerSvc.recordedTransactions)
}

func TestTxBuilder_FailedTxSkipped(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	processor := newTxBuilder(walletRepo, ledgerSvc)
	w := newTestWallet(userID, walletAddr)

	tx := newDecodedTransaction(sync.OpReceive, []sync.DecodedTransfer{
		newIncomingTransfer("0x9999999999999999999999999999999999999999"),
	})
	tx.Status = "failed"

	_, err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	assert.Empty(t, ledgerSvc.recordedTransactions)
}

func TestTxBuilder_DuplicateHandling(t *testing.T) {
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

	processor := newTxBuilder(walletRepo, ledgerSvc)
	w := newTestWallet(userID, walletAddr)

	tx := newDecodedTransaction(sync.OpReceive, []sync.DecodedTransfer{
		newIncomingTransfer(externalAddr),
	})

	_, err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err, "duplicate error should be silently handled")
}

func TestTxBuilder_USDPrices(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"
	externalAddr := "0x9999999999999999999999999999999999999999"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	walletRepo.On("GetWalletsByAddressAndUserID", ctx, externalAddr, userID).Return([]*wallet.Wallet{}, nil)
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferIn, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := newTxBuilder(walletRepo, ledgerSvc)
	w := newTestWallet(userID, walletAddr)

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

	_, err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	rawData := ledgerSvc.recordedTransactions[0].RawData
	assert.Equal(t, "ETH", rawData["asset_id"])
	assert.Equal(t, ethPrice.String(), rawData["usd_rate"])
}

func TestTxBuilder_GasFee(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"
	externalAddr := "0x9999999999999999999999999999999999999999"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	walletRepo.On("GetWalletsByAddressAndUserID", ctx, externalAddr, userID).Return([]*wallet.Wallet{}, nil)
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferOut, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := newTxBuilder(walletRepo, ledgerSvc)
	w := newTestWallet(userID, walletAddr)

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

	_, err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	rawData := ledgerSvc.recordedTransactions[0].RawData

	assert.Equal(t, "ETH", rawData["fee_asset"])
	assert.Equal(t, "21000000000000", rawData["fee_amount"])
	assert.Equal(t, 18, rawData["fee_decimals"])
	assert.Equal(t, feeUSDPrice.String(), rawData["fee_usd_price"])

	// TransferOut should also map fee fields to gas fields
	assert.Equal(t, "21000000000000", rawData["gas_amount"])
	assert.Equal(t, feeUSDPrice.String(), rawData["gas_usd_rate"])
}

func TestTxBuilder_DeFiDeposit(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	// Aave V3 deposits are now classified as lending_supply
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeLendingSupply, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := newTxBuilder(walletRepo, ledgerSvc)
	w := newTestWallet(userID, walletAddr)

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

	_, err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, ledger.TxTypeLendingSupply, ledgerSvc.recordedTransactions[0].TxType)

	rawData := ledgerSvc.recordedTransactions[0].RawData
	assert.Equal(t, "Aave V3", rawData["protocol"])
	assert.Equal(t, "ETH", rawData["asset"])
}

func TestTxBuilder_DeFiWithdraw(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	// Aave V3 withdrawals are now classified as lending_withdraw
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeLendingWithdraw, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := newTxBuilder(walletRepo, ledgerSvc)
	w := newTestWallet(userID, walletAddr)

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

	_, err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, ledger.TxTypeLendingWithdraw, ledgerSvc.recordedTransactions[0].TxType)

	rawData := ledgerSvc.recordedTransactions[0].RawData
	assert.Equal(t, "Aave V3", rawData["protocol"])
	assert.Equal(t, "ETH", rawData["asset"])
}

func TestTxBuilder_DeFiClaim(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	// Aave V3 claims are now classified as lending_claim
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeLendingClaim, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	processor := newTxBuilder(walletRepo, ledgerSvc)
	w := newTestWallet(userID, walletAddr)

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

	_, err := processor.ProcessTransaction(ctx, w, tx)
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, ledger.TxTypeLendingClaim, ledgerSvc.recordedTransactions[0].TxType)

	rawData := ledgerSvc.recordedTransactions[0].RawData
	assert.Equal(t, "Aave V3", rawData["protocol"])
	assert.Equal(t, "AAVE", rawData["asset"])
}

// TestTxBuilder_SanitizeLogField_StripsUTF8LineSeparators verifies that
// the sanitizer used by the tx builder for log fields (price.SanitizeLogField)
// strips Unicode line separators that would otherwise allow log-line forging.
// Regression guard: the tx builder previously used a byte-walking sanitizer
// that only stripped ASCII control chars, leaving U+2028/U+2029/U+0085
// exploitable in structured-log parsers.
func TestTxBuilder_SanitizeLogField_StripsUTF8LineSeparators(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"U+2028 LINE SEPARATOR", "bad\u2028line"},
		{"U+2029 PARAGRAPH SEPARATOR", "bad\u2029line"},
		{"U+0085 NEL", "bad\u0085line"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := price.SanitizeLogField(tc.in)
			for _, r := range got {
				if r == 0x2028 || r == 0x2029 || r == 0x85 {
					t.Fatalf("%s: forbidden rune U+%04X found in sanitized output %q", tc.name, r, got)
				}
			}
		})
	}
}
