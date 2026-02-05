package transfer_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/module/transfer"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// MockWalletRepository is a mock implementation of WalletRepository
type MockWalletRepository struct {
	mock.Mock
}

func (m *MockWalletRepository) GetByID(ctx context.Context, walletID uuid.UUID) (*wallet.Wallet, error) {
	args := m.Called(ctx, walletID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*wallet.Wallet), args.Error(1)
}

// =============================================================================
// TransferInHandler Tests
// =============================================================================

// TestTransferInHandler_GenerateEntries_Balance verifies double-entry balance
func TestTransferInHandler_GenerateEntries_Balance(t *testing.T) {
	testCases := []struct {
		name     string
		amount   int64
		usdRate  int64
		decimals int
	}{
		{
			name:     "1 ETH transfer - entries balance",
			amount:   1000000000000000000, // 1 ETH in wei
			usdRate:  200000000000,        // $2000 scaled by 10^8
			decimals: 18,
		},
		{
			name:     "100 USDC transfer - entries balance",
			amount:   100000000, // 100 USDC (6 decimals)
			usdRate:  100000000, // $1 scaled by 10^8
			decimals: 6,
		},
		{
			name:     "0.001 ETH (small amount) - entries balance",
			amount:   1000000000000000, // 0.001 ETH in wei
			usdRate:  200000000000,     // $2000 scaled by 10^8
			decimals: 18,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			walletID := uuid.New()
			userID := uuid.New()

			walletRepo := new(MockWalletRepository)
			walletRepo.On("GetByID", ctx, walletID).Return(&wallet.Wallet{
				ID:      walletID,
				UserID:  userID,
				ChainID: 1,
				Address: "0x1234567890123456789012345678901234567890",
			}, nil)

			handler := transfer.NewTransferInHandler(walletRepo)

			data := map[string]interface{}{
				"wallet_id":        walletID.String(),
				"asset_id":         "ETH",
				"decimals":         tc.decimals,
				"amount":           money.NewBigIntFromInt64(tc.amount).String(),
				"usd_rate":         money.NewBigIntFromInt64(tc.usdRate).String(),
				"chain_id":         int64(1),
				"tx_hash":          "0xabc123",
				"block_number":     int64(12345678),
				"from_address":     "0xsender",
				"contract_address": "",
				"occurred_at":      time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
				"unique_id":        "unique123",
			}

			entries, err := handler.Handle(ctx, data)
			require.NoError(t, err)
			require.Len(t, entries, 2, "TransferIn should generate 2 entries")

			// CRITICAL: Verify double-entry accounting invariant
			debitSum := big.NewInt(0)
			creditSum := big.NewInt(0)

			for _, entry := range entries {
				if entry.DebitCredit == ledger.Debit {
					debitSum.Add(debitSum, entry.Amount)
				} else {
					creditSum.Add(creditSum, entry.Amount)
				}
			}

			assert.Equal(t, 0, debitSum.Cmp(creditSum),
				"Ledger entries must balance: debits=%s credits=%s",
				debitSum.String(), creditSum.String())

			// Verify entry types
			assert.Equal(t, ledger.Debit, entries[0].DebitCredit)
			assert.Equal(t, ledger.EntryTypeAssetIncrease, entries[0].EntryType)
			assert.Equal(t, ledger.Credit, entries[1].DebitCredit)
			assert.Equal(t, ledger.EntryTypeIncome, entries[1].EntryType)
		})
	}
}

// TestTransferInHandler_ValidateData validates input validation
func TestTransferInHandler_ValidateData(t *testing.T) {
	testCases := []struct {
		name        string
		modifyData  func(map[string]interface{})
		expectedErr error
	}{
		{
			name: "valid transfer in data",
			modifyData: func(data map[string]interface{}) {
				// No modifications - valid data
			},
			expectedErr: nil,
		},
		{
			name: "missing wallet ID",
			modifyData: func(data map[string]interface{}) {
				data["wallet_id"] = uuid.Nil.String()
			},
			expectedErr: transfer.ErrInvalidWalletID,
		},
		{
			name: "missing asset ID",
			modifyData: func(data map[string]interface{}) {
				data["asset_id"] = ""
			},
			expectedErr: transfer.ErrInvalidAssetID,
		},
		{
			name: "negative amount",
			modifyData: func(data map[string]interface{}) {
				data["amount"] = "-1000000000000000000"
			},
			expectedErr: transfer.ErrInvalidAmount,
		},
		{
			name: "zero amount",
			modifyData: func(data map[string]interface{}) {
				data["amount"] = "0"
			},
			expectedErr: transfer.ErrInvalidAmount,
		},
		{
			name: "future date",
			modifyData: func(data map[string]interface{}) {
				data["occurred_at"] = time.Now().Add(24 * time.Hour).Format(time.RFC3339)
			},
			expectedErr: transfer.ErrOccurredAtInFuture,
		},
		{
			name: "missing tx_hash",
			modifyData: func(data map[string]interface{}) {
				data["tx_hash"] = ""
			},
			expectedErr: transfer.ErrInvalidTxHash,
		},
		{
			name: "invalid chain ID",
			modifyData: func(data map[string]interface{}) {
				data["chain_id"] = int64(0)
			},
			expectedErr: transfer.ErrInvalidChainID,
		},
		{
			name: "negative block number",
			modifyData: func(data map[string]interface{}) {
				data["block_number"] = int64(-1)
			},
			expectedErr: transfer.ErrInvalidBlockNumber,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			walletID := uuid.New()
			userID := uuid.New()

			walletRepo := new(MockWalletRepository)
			walletRepo.On("GetByID", ctx, walletID).Return(&wallet.Wallet{
				ID:      walletID,
				UserID:  userID,
				ChainID: 1,
				Address: "0x1234567890123456789012345678901234567890",
			}, nil)

			handler := transfer.NewTransferInHandler(walletRepo)

			data := map[string]interface{}{
				"wallet_id":        walletID.String(),
				"asset_id":         "ETH",
				"decimals":         18,
				"amount":           money.NewBigIntFromInt64(1000000000000000000).String(),
				"usd_rate":         money.NewBigIntFromInt64(200000000000).String(),
				"chain_id":         int64(1),
				"tx_hash":          "0xabc123",
				"block_number":     int64(12345678),
				"from_address":     "0xsender",
				"contract_address": "",
				"occurred_at":      time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
				"unique_id":        "unique123",
			}
			tc.modifyData(data)

			err := handler.ValidateData(ctx, data)
			if tc.expectedErr != nil {
				assert.ErrorIs(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestTransferInHandler_CrossUserWallet_ReturnsUnauthorized tests authorization
func TestTransferInHandler_CrossUserWallet_ReturnsUnauthorized(t *testing.T) {
	walletOwner := uuid.New()
	attacker := uuid.New()
	walletID := uuid.New()

	// Create context with attacker's user ID
	ctx := context.WithValue(context.Background(), middleware.UserIDKey, attacker)

	walletRepo := new(MockWalletRepository)
	// Wallet belongs to walletOwner
	walletRepo.On("GetByID", ctx, walletID).Return(&wallet.Wallet{
		ID:      walletID,
		UserID:  walletOwner,
		ChainID: 1,
		Address: "0x1234567890123456789012345678901234567890",
	}, nil)

	handler := transfer.NewTransferInHandler(walletRepo)

	data := map[string]interface{}{
		"wallet_id":        walletID.String(),
		"asset_id":         "ETH",
		"decimals":         18,
		"amount":           money.NewBigIntFromInt64(1000000000000000000).String(),
		"chain_id":         int64(1),
		"tx_hash":          "0xabc123",
		"block_number":     int64(12345678),
		"from_address":     "0xsender",
		"contract_address": "",
		"occurred_at":      time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"unique_id":        "unique123",
	}

	err := handler.ValidateData(ctx, data)
	assert.ErrorIs(t, err, transfer.ErrUnauthorized)
}

// TestTransferInHandler_WalletNotFound tests missing wallet error
func TestTransferInHandler_WalletNotFound(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, walletID).Return(nil, nil)

	handler := transfer.NewTransferInHandler(walletRepo)

	data := map[string]interface{}{
		"wallet_id":        walletID.String(),
		"asset_id":         "ETH",
		"decimals":         18,
		"amount":           money.NewBigIntFromInt64(1000000000000000000).String(),
		"chain_id":         int64(1),
		"tx_hash":          "0xabc123",
		"block_number":     int64(12345678),
		"from_address":     "0xsender",
		"contract_address": "",
		"occurred_at":      time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"unique_id":        "unique123",
	}

	err := handler.ValidateData(ctx, data)
	assert.ErrorIs(t, err, transfer.ErrWalletNotFound)
}

// =============================================================================
// TransferOutHandler Tests
// =============================================================================

// TestTransferOutHandler_GenerateEntries_Balance verifies double-entry balance
func TestTransferOutHandler_GenerateEntries_Balance(t *testing.T) {
	testCases := []struct {
		name     string
		amount   int64
		usdRate  int64
		decimals int
	}{
		{
			name:     "1 ETH transfer out - entries balance",
			amount:   1000000000000000000, // 1 ETH in wei
			usdRate:  200000000000,        // $2000 scaled by 10^8
			decimals: 18,
		},
		{
			name:     "100 USDC transfer out - entries balance",
			amount:   100000000, // 100 USDC (6 decimals)
			usdRate:  100000000, // $1 scaled by 10^8
			decimals: 6,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			walletID := uuid.New()
			userID := uuid.New()

			walletRepo := new(MockWalletRepository)
			walletRepo.On("GetByID", ctx, walletID).Return(&wallet.Wallet{
				ID:      walletID,
				UserID:  userID,
				ChainID: 1,
				Address: "0x1234567890123456789012345678901234567890",
			}, nil)

			handler := transfer.NewTransferOutHandler(walletRepo)

			data := map[string]interface{}{
				"wallet_id":        walletID.String(),
				"asset_id":         "ETH",
				"decimals":         tc.decimals,
				"amount":           money.NewBigIntFromInt64(tc.amount).String(),
				"usd_rate":         money.NewBigIntFromInt64(tc.usdRate).String(),
				"chain_id":         int64(1),
				"tx_hash":          "0xabc123",
				"block_number":     int64(12345678),
				"to_address":       "0xreceiver",
				"contract_address": "",
				"occurred_at":      time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
				"unique_id":        "unique123",
			}

			entries, err := handler.Handle(ctx, data)
			require.NoError(t, err)
			require.Len(t, entries, 2, "TransferOut without gas should generate 2 entries")

			// CRITICAL: Verify double-entry accounting invariant
			debitSum := big.NewInt(0)
			creditSum := big.NewInt(0)

			for _, entry := range entries {
				if entry.DebitCredit == ledger.Debit {
					debitSum.Add(debitSum, entry.Amount)
				} else {
					creditSum.Add(creditSum, entry.Amount)
				}
			}

			assert.Equal(t, 0, debitSum.Cmp(creditSum),
				"Ledger entries must balance: debits=%s credits=%s",
				debitSum.String(), creditSum.String())

			// Verify entry types
			assert.Equal(t, ledger.Debit, entries[0].DebitCredit)
			assert.Equal(t, ledger.EntryTypeExpense, entries[0].EntryType)
			assert.Equal(t, ledger.Credit, entries[1].DebitCredit)
			assert.Equal(t, ledger.EntryTypeAssetDecrease, entries[1].EntryType)
		})
	}
}

// TestTransferOutHandler_WithGas_GenerateEntries_Balance verifies balance with gas
func TestTransferOutHandler_WithGas_GenerateEntries_Balance(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, walletID).Return(&wallet.Wallet{
		ID:      walletID,
		UserID:  userID,
		ChainID: 1,
		Address: "0x1234567890123456789012345678901234567890",
	}, nil)

	handler := transfer.NewTransferOutHandler(walletRepo)

	data := map[string]interface{}{
		"wallet_id":        walletID.String(),
		"asset_id":         "ETH",
		"decimals":         18,
		"amount":           money.NewBigIntFromInt64(1000000000000000000).String(), // 1 ETH
		"usd_rate":         money.NewBigIntFromInt64(200000000000).String(),        // $2000
		"gas_amount":       money.NewBigIntFromInt64(21000000000000000).String(),   // 0.021 ETH gas
		"gas_usd_rate":     money.NewBigIntFromInt64(200000000000).String(),        // $2000
		"chain_id":         int64(1),
		"tx_hash":          "0xabc123",
		"block_number":     int64(12345678),
		"to_address":       "0xreceiver",
		"contract_address": "",
		"occurred_at":      time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"unique_id":        "unique123",
	}

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 4, "TransferOut with gas should generate 4 entries")

	// Verify double-entry per asset category
	// Transfer entries (asset 1): debit expense + credit wallet
	transferDebit := entries[0].Amount
	transferCredit := entries[1].Amount
	assert.Equal(t, 0, transferDebit.Cmp(transferCredit),
		"Transfer entries must balance: debit=%s credit=%s",
		transferDebit.String(), transferCredit.String())

	// Gas entries (native asset): debit gas + credit wallet
	gasDebit := entries[2].Amount
	gasCredit := entries[3].Amount
	assert.Equal(t, 0, gasDebit.Cmp(gasCredit),
		"Gas entries must balance: debit=%s credit=%s",
		gasDebit.String(), gasCredit.String())

	// Verify entry types for gas
	assert.Equal(t, ledger.EntryTypeGasFee, entries[2].EntryType)
	assert.Equal(t, ledger.EntryTypeAssetDecrease, entries[3].EntryType)
}

// TestTransferOutHandler_ValidateData validates input validation
func TestTransferOutHandler_ValidateData(t *testing.T) {
	testCases := []struct {
		name        string
		modifyData  func(map[string]interface{})
		expectedErr error
	}{
		{
			name:        "valid transfer out data",
			modifyData:  func(data map[string]interface{}) {},
			expectedErr: nil,
		},
		{
			name: "missing wallet ID",
			modifyData: func(data map[string]interface{}) {
				data["wallet_id"] = uuid.Nil.String()
			},
			expectedErr: transfer.ErrInvalidWalletID,
		},
		{
			name: "negative amount",
			modifyData: func(data map[string]interface{}) {
				data["amount"] = "-1000000000000000000"
			},
			expectedErr: transfer.ErrInvalidAmount,
		},
		{
			name: "future date",
			modifyData: func(data map[string]interface{}) {
				data["occurred_at"] = time.Now().Add(24 * time.Hour).Format(time.RFC3339)
			},
			expectedErr: transfer.ErrOccurredAtInFuture,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			walletID := uuid.New()
			userID := uuid.New()

			walletRepo := new(MockWalletRepository)
			walletRepo.On("GetByID", ctx, walletID).Return(&wallet.Wallet{
				ID:      walletID,
				UserID:  userID,
				ChainID: 1,
				Address: "0x1234567890123456789012345678901234567890",
			}, nil)

			handler := transfer.NewTransferOutHandler(walletRepo)

			data := map[string]interface{}{
				"wallet_id":        walletID.String(),
				"asset_id":         "ETH",
				"decimals":         18,
				"amount":           money.NewBigIntFromInt64(1000000000000000000).String(),
				"chain_id":         int64(1),
				"tx_hash":          "0xabc123",
				"block_number":     int64(12345678),
				"to_address":       "0xreceiver",
				"contract_address": "",
				"occurred_at":      time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
				"unique_id":        "unique123",
			}
			tc.modifyData(data)

			err := handler.ValidateData(ctx, data)
			if tc.expectedErr != nil {
				assert.ErrorIs(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// InternalTransferHandler Tests
// =============================================================================

// TestInternalTransferHandler_GenerateEntries_Balance verifies double-entry balance
func TestInternalTransferHandler_GenerateEntries_Balance(t *testing.T) {
	ctx := context.Background()
	sourceWalletID := uuid.New()
	destWalletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, sourceWalletID).Return(&wallet.Wallet{
		ID:      sourceWalletID,
		UserID:  userID,
		ChainID: 1,
		Address: "0x1111111111111111111111111111111111111111",
	}, nil)
	walletRepo.On("GetByID", ctx, destWalletID).Return(&wallet.Wallet{
		ID:      destWalletID,
		UserID:  userID,
		ChainID: 1,
		Address: "0x2222222222222222222222222222222222222222",
	}, nil)

	handler := transfer.NewInternalTransferHandler(walletRepo)

	data := map[string]interface{}{
		"source_wallet_id": sourceWalletID.String(),
		"dest_wallet_id":   destWalletID.String(),
		"asset_id":         "ETH",
		"decimals":         18,
		"amount":           money.NewBigIntFromInt64(1000000000000000000).String(), // 1 ETH
		"usd_rate":         money.NewBigIntFromInt64(200000000000).String(),        // $2000
		"chain_id":         int64(1),
		"tx_hash":          "0xabc123",
		"block_number":     int64(12345678),
		"contract_address": "",
		"occurred_at":      time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"unique_id":        "unique123",
	}

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 2, "InternalTransfer without gas should generate 2 entries")

	// CRITICAL: Verify double-entry accounting invariant
	debitSum := big.NewInt(0)
	creditSum := big.NewInt(0)

	for _, entry := range entries {
		if entry.DebitCredit == ledger.Debit {
			debitSum.Add(debitSum, entry.Amount)
		} else {
			creditSum.Add(creditSum, entry.Amount)
		}
	}

	assert.Equal(t, 0, debitSum.Cmp(creditSum),
		"Ledger entries must balance: debits=%s credits=%s",
		debitSum.String(), creditSum.String())

	// Verify entry types
	assert.Equal(t, ledger.Debit, entries[0].DebitCredit)
	assert.Equal(t, ledger.EntryTypeAssetIncrease, entries[0].EntryType) // Dest wallet receives
	assert.Equal(t, ledger.Credit, entries[1].DebitCredit)
	assert.Equal(t, ledger.EntryTypeAssetDecrease, entries[1].EntryType) // Source wallet sends
}

// TestInternalTransferHandler_ValidateData validates input validation
func TestInternalTransferHandler_ValidateData(t *testing.T) {
	testCases := []struct {
		name        string
		modifyData  func(map[string]interface{})
		expectedErr error
	}{
		{
			name:        "valid internal transfer data",
			modifyData:  func(data map[string]interface{}) {},
			expectedErr: nil,
		},
		{
			name: "missing source wallet ID",
			modifyData: func(data map[string]interface{}) {
				data["source_wallet_id"] = uuid.Nil.String()
			},
			expectedErr: transfer.ErrMissingSourceWallet,
		},
		{
			name: "missing dest wallet ID",
			modifyData: func(data map[string]interface{}) {
				data["dest_wallet_id"] = uuid.Nil.String()
			},
			expectedErr: transfer.ErrMissingDestWallet,
		},
		{
			name: "same source and dest wallet",
			modifyData: func(data map[string]interface{}) {
				// Set both to the same ID
				sameID := uuid.New().String()
				data["source_wallet_id"] = sameID
				data["dest_wallet_id"] = sameID
			},
			expectedErr: transfer.ErrSameWalletTransfer,
		},
		{
			name: "negative amount",
			modifyData: func(data map[string]interface{}) {
				data["amount"] = "-1000000000000000000"
			},
			expectedErr: transfer.ErrInvalidAmount,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			sourceWalletID := uuid.New()
			destWalletID := uuid.New()
			userID := uuid.New()

			walletRepo := new(MockWalletRepository)
			walletRepo.On("GetByID", ctx, mock.AnythingOfType("uuid.UUID")).Return(&wallet.Wallet{
				ID:      sourceWalletID,
				UserID:  userID,
				ChainID: 1,
				Address: "0x1111111111111111111111111111111111111111",
			}, nil)

			handler := transfer.NewInternalTransferHandler(walletRepo)

			data := map[string]interface{}{
				"source_wallet_id": sourceWalletID.String(),
				"dest_wallet_id":   destWalletID.String(),
				"asset_id":         "ETH",
				"decimals":         18,
				"amount":           money.NewBigIntFromInt64(1000000000000000000).String(),
				"chain_id":         int64(1),
				"tx_hash":          "0xabc123",
				"block_number":     int64(12345678),
				"contract_address": "",
				"occurred_at":      time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
				"unique_id":        "unique123",
			}
			tc.modifyData(data)

			err := handler.ValidateData(ctx, data)
			if tc.expectedErr != nil {
				assert.ErrorIs(t, err, tc.expectedErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestInternalTransferHandler_CrossUserWallet_ReturnsUnauthorized tests authorization
func TestInternalTransferHandler_CrossUserWallet_ReturnsUnauthorized(t *testing.T) {
	sourceOwner := uuid.New()
	attacker := uuid.New()
	sourceWalletID := uuid.New()
	destWalletID := uuid.New()

	// Create context with attacker's user ID
	ctx := context.WithValue(context.Background(), middleware.UserIDKey, attacker)

	walletRepo := new(MockWalletRepository)
	// Source wallet belongs to sourceOwner
	walletRepo.On("GetByID", ctx, sourceWalletID).Return(&wallet.Wallet{
		ID:      sourceWalletID,
		UserID:  sourceOwner,
		ChainID: 1,
		Address: "0x1111111111111111111111111111111111111111",
	}, nil)
	walletRepo.On("GetByID", ctx, destWalletID).Return(&wallet.Wallet{
		ID:      destWalletID,
		UserID:  attacker,
		ChainID: 1,
		Address: "0x2222222222222222222222222222222222222222",
	}, nil)

	handler := transfer.NewInternalTransferHandler(walletRepo)

	data := map[string]interface{}{
		"source_wallet_id": sourceWalletID.String(),
		"dest_wallet_id":   destWalletID.String(),
		"asset_id":         "ETH",
		"decimals":         18,
		"amount":           money.NewBigIntFromInt64(1000000000000000000).String(),
		"chain_id":         int64(1),
		"tx_hash":          "0xabc123",
		"block_number":     int64(12345678),
		"contract_address": "",
		"occurred_at":      time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"unique_id":        "unique123",
	}

	err := handler.ValidateData(ctx, data)
	assert.ErrorIs(t, err, transfer.ErrUnauthorized)
}

// =============================================================================
// Handler Type Tests
// =============================================================================

func TestTransferInHandler_Type(t *testing.T) {
	handler := transfer.NewTransferInHandler(nil)
	assert.Equal(t, ledger.TxTypeTransferIn, handler.Type())
}

func TestTransferOutHandler_Type(t *testing.T) {
	handler := transfer.NewTransferOutHandler(nil)
	assert.Equal(t, ledger.TxTypeTransferOut, handler.Type())
}

func TestInternalTransferHandler_Type(t *testing.T) {
	handler := transfer.NewInternalTransferHandler(nil)
	assert.Equal(t, ledger.TxTypeInternalTransfer, handler.Type())
}
