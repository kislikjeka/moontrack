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
	"github.com/kislikjeka/moontrack/pkg/logger"
	"github.com/kislikjeka/moontrack/pkg/money"
	"github.com/kislikjeka/moontrack/pkg/testasset"
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
				Address: "0x1234567890123456789012345678901234567890",
			}, nil)

			handler := transfer.NewTransferInHandler(walletRepo, logger.NewDefault("test"))

			data := map[string]interface{}{
				"wallet_id":        walletID.String(),
				"asset_id":         testasset.ETH.String(),
				"decimals":         tc.decimals,
				"amount":           money.NewBigIntFromInt64(tc.amount).String(),
				"usd_rate":         money.NewBigIntFromInt64(tc.usdRate).String(),
				"chain_id":         "ethereum",
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
				// The nil UUID is the "no asset" spelling now (#59) — validation
				// must reject it rather than emitting an entry against it.
				data["asset_id"] = uuid.Nil.String()
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
				data["chain_id"] = ""
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
				Address: "0x1234567890123456789012345678901234567890",
			}, nil)

			handler := transfer.NewTransferInHandler(walletRepo, logger.NewDefault("test"))

			data := map[string]interface{}{
				"wallet_id":        walletID.String(),
				"asset_id":         testasset.ETH.String(),
				"decimals":         18,
				"amount":           money.NewBigIntFromInt64(1000000000000000000).String(),
				"usd_rate":         money.NewBigIntFromInt64(200000000000).String(),
				"chain_id":         "ethereum",
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

// TestTransferInHandler_MultiAsset verifies a single tx with multiple IN
// transfers produces one balanced debit/credit pair per asset. Regression
// test for the multi-asset bug where only the first transfer was processed
// and the rest were silently dropped (e.g. Aave borrow debt + USDC both
// arriving in one on-chain tx).
func TestTransferInHandler_MultiAsset(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, walletID).Return(&wallet.Wallet{
		ID:      walletID,
		UserID:  userID,
		Address: "0x1234567890123456789012345678901234567890",
	}, nil)

	handler := transfer.NewTransferInHandler(walletRepo, logger.NewDefault("test"))

	// Two simultaneous IN transfers: 1 ETH (18 decimals) and 400 USDC (6).
	data := map[string]interface{}{
		"wallet_id":    walletID.String(),
		"chain_id":     "ethereum",
		"tx_hash":      "0xabc123",
		"block_number": int64(12345678),
		"occurred_at":  time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"unique_id":    "unique123",
		"transfers": []map[string]interface{}{
			{
				"asset_id":         testasset.ETH.String(),
				"decimals":         18,
				"amount":           money.NewBigIntFromInt64(1000000000000000000).String(),
				"usd_rate":         money.NewBigIntFromInt64(200000000000).String(),
				"contract_address": "",
				"from_address":     "0xsender1",
				"direction":        "in",
			},
			{
				"asset_id":         testasset.USDC.String(),
				"decimals":         6,
				"amount":           money.NewBigIntFromInt64(400000000).String(),
				"usd_rate":         money.NewBigIntFromInt64(100000000).String(),
				"contract_address": "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
				"from_address":     "0xsender2",
				"direction":        "in",
			},
		},
	}

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 4, "expected 2 entries per asset x 2 assets = 4 entries")

	// Each asset must contribute a balanced debit/credit pair.
	perAsset := map[uuid.UUID]struct {
		debitSum, creditSum *big.Int
	}{
		testasset.ETH:  {big.NewInt(0), big.NewInt(0)},
		testasset.USDC: {big.NewInt(0), big.NewInt(0)},
	}
	for _, e := range entries {
		bucket, ok := perAsset[e.AssetID]
		if !ok {
			t.Fatalf("unexpected asset %s in entries", e.AssetID)
		}
		if e.DebitCredit == ledger.Debit {
			bucket.debitSum.Add(bucket.debitSum, e.Amount)
		} else {
			bucket.creditSum.Add(bucket.creditSum, e.Amount)
		}
	}
	for asset, b := range perAsset {
		assert.Equal(t, 0, b.debitSum.Cmp(b.creditSum),
			"%s entries must balance: debit=%s credit=%s",
			asset, b.debitSum.String(), b.creditSum.String())
		assert.Equal(t, 1, b.debitSum.Sign(), "%s should have non-zero debit", asset)
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
		Address: "0x1234567890123456789012345678901234567890",
	}, nil)

	handler := transfer.NewTransferInHandler(walletRepo, logger.NewDefault("test"))

	data := map[string]interface{}{
		"wallet_id":        walletID.String(),
		"asset_id":         testasset.ETH.String(),
		"decimals":         18,
		"amount":           money.NewBigIntFromInt64(1000000000000000000).String(),
		"chain_id":         "ethereum",
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

	handler := transfer.NewTransferInHandler(walletRepo, logger.NewDefault("test"))

	data := map[string]interface{}{
		"wallet_id":        walletID.String(),
		"asset_id":         testasset.ETH.String(),
		"decimals":         18,
		"amount":           money.NewBigIntFromInt64(1000000000000000000).String(),
		"chain_id":         "ethereum",
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
				Address: "0x1234567890123456789012345678901234567890",
			}, nil)

			handler := transfer.NewTransferOutHandler(walletRepo, logger.NewDefault("test"))

			data := map[string]interface{}{
				"wallet_id":        walletID.String(),
				"asset_id":         testasset.ETH.String(),
				"decimals":         tc.decimals,
				"amount":           money.NewBigIntFromInt64(tc.amount).String(),
				"usd_rate":         money.NewBigIntFromInt64(tc.usdRate).String(),
				"chain_id":         "ethereum",
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
		Address: "0x1234567890123456789012345678901234567890",
	}, nil)

	handler := transfer.NewTransferOutHandler(walletRepo, logger.NewDefault("test"))

	data := map[string]interface{}{
		"wallet_id":    walletID.String(),
		"asset_id":     testasset.ETH.String(),
		"decimals":     18,
		"amount":       money.NewBigIntFromInt64(1000000000000000000).String(), // 1 ETH
		"usd_rate":     money.NewBigIntFromInt64(200000000000).String(),        // $2000
		"gas_amount":   money.NewBigIntFromInt64(21000000000000000).String(),   // 0.021 ETH gas
		"gas_usd_rate": money.NewBigIntFromInt64(200000000000).String(),        // $2000
		// native_asset_id is required to book gas now (#59): it used to be
		// optional and defaulted to ETH, which happened to be right on Ethereum
		// and wrong on every other chain.
		"native_asset_id":  testasset.ETH.String(),
		"chain_id":         "ethereum",
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

	// The gas legs must carry the native asset that raw_data named, not a
	// default. See TestTransferOutHandler_GasWithoutNativeAsset_Errors for the
	// case where it is absent — the old "fall back to ETH" behaviour is gone
	// (#59), because on a non-Ethereum chain it charged gas to the wrong asset.
	assert.Equal(t, testasset.ETH, entries[2].AssetID, "gas debit carries the named native asset")
	assert.Equal(t, testasset.ETH, entries[3].AssetID, "gas credit carries the named native asset")
}

// TestTransferOutHandler_GasWithoutNativeAsset_Errors pins the replacement for
// the removed "fall back to ETH" default (#59). A gas fee with no native asset
// resolved is unbookable: any guess would decrement a balance in an asset the
// wallet never spent, and would still balance, so it could not be caught later.
func TestTransferOutHandler_GasWithoutNativeAsset_Errors(t *testing.T) {
	userID := uuid.New()
	walletID := uuid.New()
	ctx := context.WithValue(context.Background(), middleware.UserIDKey, userID)

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", mock.Anything, walletID).Return(&wallet.Wallet{
		ID:      walletID,
		UserID:  userID,
		Address: "0x1234567890123456789012345678901234567890",
	}, nil)

	handler := transfer.NewTransferOutHandler(walletRepo, logger.NewDefault("test"))

	data := map[string]interface{}{
		"wallet_id":        walletID.String(),
		"asset_id":         testasset.USDC.String(),
		"decimals":         6,
		"amount":           money.NewBigIntFromInt64(1000000).String(),
		"usd_rate":         money.NewBigIntFromInt64(100000000).String(),
		"gas_amount":       money.NewBigIntFromInt64(21000000000000000).String(),
		"gas_usd_rate":     money.NewBigIntFromInt64(200000000000).String(),
		"chain_id":         "polygon",
		"tx_hash":          "0xnonative",
		"block_number":     int64(12345678),
		"to_address":       "0xreceiver",
		"contract_address": "0xusdc",
		"occurred_at":      time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"unique_id":        "unique-no-native",
	}

	_, err := handler.Handle(ctx, data)
	assert.ErrorIs(t, err, transfer.ErrMissingNativeAsset)
}

// TestTransferOutHandler_WithNativeAsset_GenerateEntries verifies that a
// non-ETH native fee asset (e.g. BNB on BSC) is booked to the chain-native
// gas asset rather than a hardcoded "ETH" (MT-SYNC-11 regression).
func TestTransferOutHandler_WithNativeAsset_GenerateEntries(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, walletID).Return(&wallet.Wallet{
		ID:      walletID,
		UserID:  userID,
		Address: "0x1234567890123456789012345678901234567890",
	}, nil)

	handler := transfer.NewTransferOutHandler(walletRepo, logger.NewDefault("test"))

	data := map[string]interface{}{
		"wallet_id":        walletID.String(),
		"asset_id":         testasset.USDT.String(),
		"decimals":         18,
		"amount":           money.NewBigIntFromInt64(1000000000000000000).String(), // 1 USDT
		"usd_rate":         money.NewBigIntFromInt64(100000000).String(),           // $1
		"gas_amount":       money.NewBigIntFromInt64(5000000000000000).String(),    // 0.005 BNB gas
		"gas_usd_rate":     money.NewBigIntFromInt64(30000000000).String(),         // $300
		"native_asset_id":  testasset.BNB.String(),
		"chain_id":         "binance-smart-chain",
		"tx_hash":          "0xbnb123",
		"block_number":     int64(87654321),
		"to_address":       "0xreceiver",
		"contract_address": "0xtoken",
		"occurred_at":      time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"unique_id":        "uniqueBNB",
	}

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 4, "TransferOut with gas should generate 4 entries")

	// Gas entries must be booked to BNB, not ETH.
	gasDebit := entries[2]
	gasCredit := entries[3]
	assert.Equal(t, ledger.EntryTypeGasFee, gasDebit.EntryType)
	assert.Equal(t, ledger.EntryTypeAssetDecrease, gasCredit.EntryType)
	assert.Equal(t, testasset.BNB, gasDebit.AssetID, "gas debit asset must be BNB")
	assert.Equal(t, testasset.BNB, gasCredit.AssetID, "gas credit asset must be BNB")

	// Account codes must reference the chain-native BNB asset.
	assert.Equal(t, "gas.binance-smart-chain."+testasset.BNB.String(), gasDebit.Metadata["account_code"],
		"gas account code must use native fee asset")
	assert.Equal(t,
		"wallet."+walletID.String()+".binance-smart-chain."+testasset.BNB.String(),
		gasCredit.Metadata["account_code"],
		"wallet native account code must use native fee asset")
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
				Address: "0x1234567890123456789012345678901234567890",
			}, nil)

			handler := transfer.NewTransferOutHandler(walletRepo, logger.NewDefault("test"))

			data := map[string]interface{}{
				"wallet_id":        walletID.String(),
				"asset_id":         testasset.ETH.String(),
				"decimals":         18,
				"amount":           money.NewBigIntFromInt64(1000000000000000000).String(),
				"chain_id":         "ethereum",
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
		Address: "0x1111111111111111111111111111111111111111",
	}, nil)
	walletRepo.On("GetByID", ctx, destWalletID).Return(&wallet.Wallet{
		ID:      destWalletID,
		UserID:  userID,
		Address: "0x2222222222222222222222222222222222222222",
	}, nil)

	handler := transfer.NewInternalTransferHandler(walletRepo, logger.NewDefault("test"))

	data := map[string]interface{}{
		"source_wallet_id": sourceWalletID.String(),
		"dest_wallet_id":   destWalletID.String(),
		"asset_id":         testasset.ETH.String(),
		"decimals":         18,
		"amount":           money.NewBigIntFromInt64(1000000000000000000).String(), // 1 ETH
		"usd_rate":         money.NewBigIntFromInt64(200000000000).String(),        // $2000
		"chain_id":         "ethereum",
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
				Address: "0x1111111111111111111111111111111111111111",
			}, nil)

			handler := transfer.NewInternalTransferHandler(walletRepo, logger.NewDefault("test"))

			data := map[string]interface{}{
				"source_wallet_id": sourceWalletID.String(),
				"dest_wallet_id":   destWalletID.String(),
				"asset_id":         testasset.ETH.String(),
				"decimals":         18,
				"amount":           money.NewBigIntFromInt64(1000000000000000000).String(),
				"chain_id":         "ethereum",
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
		Address: "0x1111111111111111111111111111111111111111",
	}, nil)
	walletRepo.On("GetByID", ctx, destWalletID).Return(&wallet.Wallet{
		ID:      destWalletID,
		UserID:  attacker,
		Address: "0x2222222222222222222222222222222222222222",
	}, nil)

	handler := transfer.NewInternalTransferHandler(walletRepo, logger.NewDefault("test"))

	data := map[string]interface{}{
		"source_wallet_id": sourceWalletID.String(),
		"dest_wallet_id":   destWalletID.String(),
		"asset_id":         testasset.ETH.String(),
		"decimals":         18,
		"amount":           money.NewBigIntFromInt64(1000000000000000000).String(),
		"chain_id":         "ethereum",
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
	handler := transfer.NewTransferInHandler(nil, logger.NewDefault("test"))
	assert.Equal(t, ledger.TxTypeTransferIn, handler.Type())
}

func TestTransferOutHandler_Type(t *testing.T) {
	handler := transfer.NewTransferOutHandler(nil, logger.NewDefault("test"))
	assert.Equal(t, ledger.TxTypeTransferOut, handler.Type())
}

func TestInternalTransferHandler_Type(t *testing.T) {
	handler := transfer.NewInternalTransferHandler(nil, logger.NewDefault("test"))
	assert.Equal(t, ledger.TxTypeInternalTransfer, handler.Type())
}
