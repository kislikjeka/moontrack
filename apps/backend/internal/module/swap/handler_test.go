package swap_test

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
	"github.com/kislikjeka/moontrack/internal/module/swap"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
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
// Handler Type Test
// =============================================================================

func TestSwapHandler_Type(t *testing.T) {
	handler := swap.NewSwapHandler(nil, logger.NewDefault("test"))
	assert.Equal(t, ledger.TxTypeSwap, handler.Type())
}

// =============================================================================
// Balance Tests
// =============================================================================

func TestSwapHandler_SimpleSwap_Balance(t *testing.T) {
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

	handler := swap.NewSwapHandler(walletRepo, logger.NewDefault("test"))

	data := map[string]interface{}{
		"wallet_id": walletID.String(),
		"tx_hash":   "0xswap123",
		"chain_id":  int64(1),
		"occurred_at": time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"protocol":  "uniswap_v3",
		"transfers_out": []map[string]interface{}{
			{
				"asset_symbol":     "ETH",
				"amount":           money.NewBigIntFromInt64(1000000000000000000).String(), // 1 ETH
				"decimals":         18,
				"usd_price":        "200000000000", // $2000
				"contract_address": "",
				"sender":           "0x1234",
				"recipient":        "0xdex",
			},
		},
		"transfers_in": []map[string]interface{}{
			{
				"asset_symbol":     "USDC",
				"amount":           money.NewBigIntFromInt64(2000000000).String(), // 2000 USDC
				"decimals":         6,
				"usd_price":        "100000000", // $1
				"contract_address": "0xusdc",
				"sender":           "0xdex",
				"recipient":        "0x1234",
			},
		},
	}

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 4, "Simple swap should generate 4 entries (2 per transfer)")

	// Verify double-entry balance per transfer pair
	// Out pair: CREDIT wallet ETH + DEBIT clearing ETH
	assert.Equal(t, ledger.Credit, entries[0].DebitCredit)
	assert.Equal(t, ledger.EntryTypeAssetDecrease, entries[0].EntryType)
	assert.Equal(t, ledger.Debit, entries[1].DebitCredit)
	assert.Equal(t, ledger.EntryTypeClearing, entries[1].EntryType)
	assert.Equal(t, 0, entries[0].Amount.Cmp(entries[1].Amount), "Out pair must balance")

	// In pair: DEBIT wallet USDC + CREDIT clearing USDC
	assert.Equal(t, ledger.Debit, entries[2].DebitCredit)
	assert.Equal(t, ledger.EntryTypeAssetIncrease, entries[2].EntryType)
	assert.Equal(t, ledger.Credit, entries[3].DebitCredit)
	assert.Equal(t, ledger.EntryTypeClearing, entries[3].EntryType)
	assert.Equal(t, 0, entries[2].Amount.Cmp(entries[3].Amount), "In pair must balance")

	// Verify overall balance: SUM(debit) == SUM(credit)
	debitSum := big.NewInt(0)
	creditSum := big.NewInt(0)
	for _, e := range entries {
		if e.DebitCredit == ledger.Debit {
			debitSum.Add(debitSum, e.Amount)
		} else {
			creditSum.Add(creditSum, e.Amount)
		}
	}
	assert.Equal(t, 0, debitSum.Cmp(creditSum),
		"Ledger entries must balance: debits=%s credits=%s",
		debitSum.String(), creditSum.String())
}

func TestSwapHandler_WithGasFee_Balance(t *testing.T) {
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

	handler := swap.NewSwapHandler(walletRepo, logger.NewDefault("test"))

	data := map[string]interface{}{
		"wallet_id": walletID.String(),
		"tx_hash":   "0xswap456",
		"chain_id":  int64(1),
		"occurred_at": time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"fee_asset":     "ETH",
		"fee_amount":    money.NewBigIntFromInt64(21000000000000).String(), // 0.000021 ETH
		"fee_decimals":  18,
		"fee_usd_price": "200000000000", // $2000
		"transfers_out": []map[string]interface{}{
			{
				"asset_symbol":     "ETH",
				"amount":           money.NewBigIntFromInt64(500000000000000000).String(), // 0.5 ETH
				"decimals":         18,
				"usd_price":        "200000000000",
				"contract_address": "",
				"sender":           "0x1234",
				"recipient":        "0xdex",
			},
		},
		"transfers_in": []map[string]interface{}{
			{
				"asset_symbol":     "USDC",
				"amount":           money.NewBigIntFromInt64(1000000000).String(), // 1000 USDC
				"decimals":         6,
				"usd_price":        "100000000",
				"contract_address": "0xusdc",
				"sender":           "0xdex",
				"recipient":        "0x1234",
			},
		},
	}

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 6, "Swap with gas should generate 6 entries (2+2+2)")

	// Verify gas entries
	gasDebit := entries[4]
	gasCredit := entries[5]
	assert.Equal(t, ledger.EntryTypeGasFee, gasDebit.EntryType)
	assert.Equal(t, ledger.EntryTypeAssetDecrease, gasCredit.EntryType)
	assert.Equal(t, 0, gasDebit.Amount.Cmp(gasCredit.Amount), "Gas pair must balance")

	// Verify overall balance
	debitSum := big.NewInt(0)
	creditSum := big.NewInt(0)
	for _, e := range entries {
		if e.DebitCredit == ledger.Debit {
			debitSum.Add(debitSum, e.Amount)
		} else {
			creditSum.Add(creditSum, e.Amount)
		}
	}
	assert.Equal(t, 0, debitSum.Cmp(creditSum),
		"Ledger entries must balance: debits=%s credits=%s",
		debitSum.String(), creditSum.String())
}

func TestSwapHandler_MultiAsset(t *testing.T) {
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

	handler := swap.NewSwapHandler(walletRepo, logger.NewDefault("test"))

	// Multi-hop swap: ETH + WBTC out -> USDC + DAI in
	data := map[string]interface{}{
		"wallet_id": walletID.String(),
		"tx_hash":   "0xmultiswap",
		"chain_id":  int64(1),
		"occurred_at": time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"transfers_out": []map[string]interface{}{
			{
				"asset_symbol":     "ETH",
				"amount":           money.NewBigIntFromInt64(1000000000000000000).String(),
				"decimals":         18,
				"usd_price":        "200000000000",
				"contract_address": "",
				"sender":           "0x1234",
				"recipient":        "0xdex",
			},
			{
				"asset_symbol":     "WBTC",
				"amount":           money.NewBigIntFromInt64(10000000).String(), // 0.1 WBTC (8 decimals)
				"decimals":         8,
				"usd_price":        "5000000000000", // $50000
				"contract_address": "0xwbtc",
				"sender":           "0x1234",
				"recipient":        "0xdex",
			},
		},
		"transfers_in": []map[string]interface{}{
			{
				"asset_symbol":     "USDC",
				"amount":           money.NewBigIntFromInt64(5000000000).String(), // 5000 USDC
				"decimals":         6,
				"usd_price":        "100000000",
				"contract_address": "0xusdc",
				"sender":           "0xdex",
				"recipient":        "0x1234",
			},
			{
				"asset_symbol":     "DAI",
				"amount":           money.NewBigIntFromInt64(2000000000000000000).String(), // 2000 DAI
				"decimals":         18,
				"usd_price":        "100000000",
				"contract_address": "0xdai",
				"sender":           "0xdex",
				"recipient":        "0x1234",
			},
		},
	}

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 8, "Multi-asset swap should generate 8 entries (2 per transfer)")

	// Verify overall balance
	debitSum := big.NewInt(0)
	creditSum := big.NewInt(0)
	for _, e := range entries {
		if e.DebitCredit == ledger.Debit {
			debitSum.Add(debitSum, e.Amount)
		} else {
			creditSum.Add(creditSum, e.Amount)
		}
	}
	assert.Equal(t, 0, debitSum.Cmp(creditSum),
		"Ledger entries must balance: debits=%s credits=%s",
		debitSum.String(), creditSum.String())
}

// =============================================================================
// Validation Tests
// =============================================================================

func TestSwapHandler_Validate_MissingFields(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, mock.AnythingOfType("uuid.UUID")).Return(&wallet.Wallet{
		ID:      walletID,
		UserID:  userID,
		ChainID: 1,
		Address: "0x1234567890123456789012345678901234567890",
	}, nil)

	handler := swap.NewSwapHandler(walletRepo, logger.NewDefault("test"))

	testCases := []struct {
		name        string
		modifyData  func(map[string]interface{})
		expectedErr error
	}{
		{
			name: "missing wallet_id",
			modifyData: func(data map[string]interface{}) {
				data["wallet_id"] = uuid.Nil.String()
			},
			expectedErr: swap.ErrInvalidWalletID,
		},
		{
			name: "missing tx_hash",
			modifyData: func(data map[string]interface{}) {
				data["tx_hash"] = ""
			},
			expectedErr: swap.ErrInvalidTxHash,
		},
		{
			name: "invalid chain_id",
			modifyData: func(data map[string]interface{}) {
				data["chain_id"] = int64(0)
			},
			expectedErr: swap.ErrInvalidChainID,
		},
		{
			name: "negative amount in transfer",
			modifyData: func(data map[string]interface{}) {
				data["transfers_out"] = []map[string]interface{}{
					{
						"asset_symbol": "ETH",
						"amount":       "-1",
						"decimals":     18,
						"usd_price":    "0",
					},
				}
			},
			expectedErr: swap.ErrInvalidAmount,
		},
		{
			name: "missing asset symbol",
			modifyData: func(data map[string]interface{}) {
				data["transfers_in"] = []map[string]interface{}{
					{
						"asset_symbol": "",
						"amount":       "100",
						"decimals":     6,
						"usd_price":    "0",
					},
				}
			},
			expectedErr: swap.ErrInvalidAssetID,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := validSwapData(walletID)
			tc.modifyData(data)

			err := handler.ValidateData(ctx, data)
			assert.ErrorIs(t, err, tc.expectedErr)
		})
	}
}

func TestSwapHandler_Validate_NoTransfers(t *testing.T) {
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

	handler := swap.NewSwapHandler(walletRepo, logger.NewDefault("test"))

	testCases := []struct {
		name       string
		modifyData func(map[string]interface{})
	}{
		{
			name: "empty transfers_in",
			modifyData: func(data map[string]interface{}) {
				data["transfers_in"] = []map[string]interface{}{}
			},
		},
		{
			name: "empty transfers_out",
			modifyData: func(data map[string]interface{}) {
				data["transfers_out"] = []map[string]interface{}{}
			},
		},
		{
			name: "both empty",
			modifyData: func(data map[string]interface{}) {
				data["transfers_in"] = []map[string]interface{}{}
				data["transfers_out"] = []map[string]interface{}{}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := validSwapData(walletID)
			tc.modifyData(data)

			err := handler.ValidateData(ctx, data)
			assert.ErrorIs(t, err, swap.ErrNoTransfers)
		})
	}
}

// =============================================================================
// Metadata Tests
// =============================================================================

func TestSwapHandler_EntryMetadata(t *testing.T) {
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

	handler := swap.NewSwapHandler(walletRepo, logger.NewDefault("test"))
	data := validSwapData(walletID)

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 4)

	// Check wallet entry metadata
	walletEntry := entries[0] // CREDIT wallet.{walletID}.ETH
	assert.Equal(t, walletID.String(), walletEntry.Metadata["wallet_id"])
	assert.Contains(t, walletEntry.Metadata["account_code"], "wallet.")
	assert.Equal(t, "0xswap123", walletEntry.Metadata["tx_hash"])
	assert.Equal(t, "out", walletEntry.Metadata["swap_direction"])

	// Check clearing entry metadata
	clearingEntry := entries[1] // DEBIT clearing.1.ETH
	assert.Contains(t, clearingEntry.Metadata["account_code"], "clearing.")
	assert.Equal(t, "CLEARING", clearingEntry.Metadata["account_type"])
	assert.Equal(t, "0xswap123", clearingEntry.Metadata["tx_hash"])
}

// =============================================================================
// Helpers
// =============================================================================

func validSwapData(walletID uuid.UUID) map[string]interface{} {
	return map[string]interface{}{
		"wallet_id": walletID.String(),
		"tx_hash":   "0xswap123",
		"chain_id":  int64(1),
		"occurred_at": time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"transfers_out": []map[string]interface{}{
			{
				"asset_symbol":     "ETH",
				"amount":           money.NewBigIntFromInt64(1000000000000000000).String(),
				"decimals":         18,
				"usd_price":        "200000000000",
				"contract_address": "",
				"sender":           "0x1234",
				"recipient":        "0xdex",
			},
		},
		"transfers_in": []map[string]interface{}{
			{
				"asset_symbol":     "USDC",
				"amount":           money.NewBigIntFromInt64(2000000000).String(),
				"decimals":         6,
				"usd_price":        "100000000",
				"contract_address": "0xusdc",
				"sender":           "0xdex",
				"recipient":        "0x1234",
			},
		},
	}
}
