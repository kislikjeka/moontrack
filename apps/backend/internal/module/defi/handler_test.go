package defi_test

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
	"github.com/kislikjeka/moontrack/internal/module/defi"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
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
// Helper: verify entries balance (SUM debit == SUM credit)
// =============================================================================

func assertEntriesBalanced(t *testing.T, entries []*ledger.Entry) {
	t.Helper()
	debitSum := new(big.Int)
	creditSum := new(big.Int)
	for _, e := range entries {
		if e.DebitCredit == ledger.Debit {
			debitSum.Add(debitSum, e.Amount)
		} else {
			creditSum.Add(creditSum, e.Amount)
		}
	}
	assert.Equal(t, 0, debitSum.Cmp(creditSum),
		"Ledger entries must balance: debits=%s credits=%s", debitSum.String(), creditSum.String())
}

func testWallet(walletID, userID uuid.UUID) *wallet.Wallet {
	return &wallet.Wallet{
		ID:      walletID,
		UserID:  userID,
		Address: "0x1234567890123456789012345678901234567890",
	}
}

// =============================================================================
// DeFi Deposit Handler Tests
// =============================================================================

func TestDeFiDepositHandler_Type(t *testing.T) {
	handler := defi.NewDeFiDepositHandler(nil, logger.NewDefault("test"))
	assert.Equal(t, ledger.TxTypeDefiDeposit, handler.Type())
}

func TestDeFiDepositHandler_SimpleDeposit_Balance(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, walletID).Return(testWallet(walletID, userID), nil)

	handler := defi.NewDeFiDepositHandler(walletRepo, logger.NewDefault("test"))

	// AAVE deposit: cbBTC out, aBascbBTC in
	data := map[string]interface{}{
		"wallet_id":      walletID.String(),
		"tx_hash":        "0x30a455deposit",
		"chain_id":       "base",
		"occurred_at":    time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"protocol":       "AAVE",
		"operation_type": "deposit",
		"transfers": []map[string]interface{}{
			{
				"asset_symbol":     "cbBTC",
				"amount":           "981547",
				"decimals":         8,
				"usd_price":        "6795302440668",
				"direction":        "out",
				"contract_address": "0xcbbtc",
				"sender":           "0x1234",
				"recipient":        "0xaave",
			},
			{
				"asset_symbol":     "aBascbBTC",
				"amount":           "981581",
				"decimals":         8,
				"usd_price":        "0",
				"direction":        "in",
				"contract_address": "0xabasbtc",
				"sender":           "0xaave",
				"recipient":        "0x1234",
			},
		},
	}

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 4, "Simple deposit should generate 4 entries (2 OUT + 2 IN)")

	// Verify OUT pair: CREDIT wallet cbBTC + DEBIT clearing cbBTC
	assert.Equal(t, ledger.Credit, entries[0].DebitCredit)
	assert.Equal(t, ledger.EntryTypeAssetDecrease, entries[0].EntryType)
	assert.Equal(t, "cbBTC", entries[0].AssetID)
	assert.Equal(t, ledger.Debit, entries[1].DebitCredit)
	assert.Equal(t, ledger.EntryTypeClearing, entries[1].EntryType)

	// Verify IN pair: DEBIT wallet aBascbBTC + CREDIT clearing aBascbBTC
	assert.Equal(t, ledger.Debit, entries[2].DebitCredit)
	assert.Equal(t, ledger.EntryTypeAssetIncrease, entries[2].EntryType)
	assert.Equal(t, "aBascbBTC", entries[2].AssetID)
	assert.Equal(t, ledger.Credit, entries[3].DebitCredit)
	assert.Equal(t, ledger.EntryTypeClearing, entries[3].EntryType)

	assertEntriesBalanced(t, entries)
}

func TestDeFiDepositHandler_WithGasFee_Balance(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, walletID).Return(testWallet(walletID, userID), nil)

	handler := defi.NewDeFiDepositHandler(walletRepo, logger.NewDefault("test"))

	data := map[string]interface{}{
		"wallet_id":      walletID.String(),
		"tx_hash":        "0xdepositwithgas",
		"chain_id":       "base",
		"occurred_at":    time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"protocol":       "AAVE",
		"operation_type": "deposit",
		"fee_asset":      "ETH",
		"fee_amount":     money.NewBigIntFromInt64(21000000000000).String(),
		"fee_decimals":   18,
		"fee_usd_price":  "200000000000",
		"transfers": []map[string]interface{}{
			{
				"asset_symbol":     "cbBTC",
				"amount":           "981547",
				"decimals":         8,
				"usd_price":        "6795302440668",
				"direction":        "out",
				"contract_address": "0xcbbtc",
				"sender":           "0x1234",
				"recipient":        "0xaave",
			},
			{
				"asset_symbol":     "aBascbBTC",
				"amount":           "981581",
				"decimals":         8,
				"usd_price":        "0",
				"direction":        "in",
				"contract_address": "0xabasbtc",
				"sender":           "0xaave",
				"recipient":        "0x1234",
			},
		},
	}

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 6, "Deposit with gas should generate 6 entries (2+2+2)")

	// Verify gas entries
	gasDebit := entries[4]
	gasCredit := entries[5]
	assert.Equal(t, ledger.EntryTypeGasFee, gasDebit.EntryType)
	assert.Equal(t, ledger.Debit, gasDebit.DebitCredit)
	assert.Equal(t, "ETH", gasDebit.AssetID)
	assert.Equal(t, ledger.EntryTypeAssetDecrease, gasCredit.EntryType)
	assert.Equal(t, ledger.Credit, gasCredit.DebitCredit)
	assert.Equal(t, 0, gasDebit.Amount.Cmp(gasCredit.Amount), "Gas pair must balance")

	assertEntriesBalanced(t, entries)
}

func TestDeFiDepositHandler_MintOnly_Balance(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, walletID).Return(testWallet(walletID, userID), nil)

	handler := defi.NewDeFiDepositHandler(walletRepo, logger.NewDefault("test"))

	// GMX mint: IN only, no OUT transfers
	data := map[string]interface{}{
		"wallet_id":      walletID.String(),
		"tx_hash":        "0xgmxmint",
		"chain_id":       "arbitrum",
		"occurred_at":    time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"protocol":       "GMX",
		"operation_type": "mint",
		"transfers": []map[string]interface{}{
			{
				"asset_symbol":     "GM",
				"amount":           "151057598000000000000",
				"decimals":         18,
				"usd_price":        "0",
				"direction":        "in",
				"contract_address": "0xgm",
				"sender":           "0x0000",
				"recipient":        "0x1234",
			},
		},
	}

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 2, "Mint-only should generate 2 entries (wallet + clearing)")

	// Verify IN pair
	assert.Equal(t, ledger.Debit, entries[0].DebitCredit)
	assert.Equal(t, ledger.EntryTypeAssetIncrease, entries[0].EntryType)
	assert.Equal(t, "GM", entries[0].AssetID)
	assert.Equal(t, ledger.Credit, entries[1].DebitCredit)
	assert.Equal(t, ledger.EntryTypeClearing, entries[1].EntryType)

	assertEntriesBalanced(t, entries)
}

func TestDeFiDepositHandler_USDPriceFallback(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, walletID).Return(testWallet(walletID, userID), nil)

	handler := defi.NewDeFiDepositHandler(walletRepo, logger.NewDefault("test"))

	// AAVE deposit with OUT having a price, IN having usd_price=0
	// cbBTC: amount=1000000 (0.01 BTC), decimals=8, price=$67000 = 6700000000000 (scaled 1e8)
	// aBascbBTC: amount=1000000, decimals=8, price=0 -> should get computed price
	data := map[string]interface{}{
		"wallet_id":      walletID.String(),
		"tx_hash":        "0xpricefallback",
		"chain_id":       "base",
		"occurred_at":    time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"operation_type": "deposit",
		"transfers": []map[string]interface{}{
			{
				"asset_symbol":     "cbBTC",
				"amount":           "1000000",
				"decimals":         8,
				"usd_price":        "6700000000000",
				"direction":        "out",
				"contract_address": "0xcbbtc",
				"sender":           "0x1234",
				"recipient":        "0xaave",
			},
			{
				"asset_symbol":     "aBascbBTC",
				"amount":           "1000000",
				"decimals":         8,
				"usd_price":        "0",
				"direction":        "in",
				"contract_address": "0xabasbtc",
				"sender":           "0xaave",
				"recipient":        "0x1234",
			},
		},
	}

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 4)

	// The IN wallet entry (index 2) should have exact computed USD rate & value
	// OUT: amount=1000000, decimals=8, usd_price=6700000000000
	//   totalOutUSDValue = CalcUSDValue(1000000, 6700000000000, 8) = (1000000 * 6700000000000) / 10^8 = 67000000000
	// IN: amount=1000000, decimals=8, usd_price=0
	//   fallback usdRate = (67000000000 * 10^8) / 1000000 = 6700000000000
	//   usdValue = CalcUSDValue(1000000, 6700000000000, 8) = 67000000000
	inWalletEntry := entries[2]
	assert.Equal(t, ledger.EntryTypeAssetIncrease, inWalletEntry.EntryType)
	assert.Equal(t, "6700000000000", inWalletEntry.USDRate.String(),
		"IN transfer should have computed USD rate matching OUT rate")
	assert.Equal(t, "67000000000", inWalletEntry.USDValue.String(),
		"IN transfer should have computed USD value matching OUT value")

	assertEntriesBalanced(t, entries)
}

func TestDeFiDepositHandler_EntryMetadata(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, walletID).Return(testWallet(walletID, userID), nil)

	handler := defi.NewDeFiDepositHandler(walletRepo, logger.NewDefault("test"))

	data := map[string]interface{}{
		"wallet_id":      walletID.String(),
		"tx_hash":        "0xmetadata",
		"chain_id":       "base",
		"occurred_at":    time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"protocol":       "AAVE",
		"operation_type": "deposit",
		"transfers": []map[string]interface{}{
			{
				"asset_symbol":     "cbBTC",
				"amount":           "100",
				"decimals":         8,
				"usd_price":        "100",
				"direction":        "out",
				"contract_address": "0xcbbtc",
				"sender":           "0x1234",
				"recipient":        "0xaave",
			},
			{
				"asset_symbol":     "aBascbBTC",
				"amount":           "100",
				"decimals":         8,
				"usd_price":        "100",
				"direction":        "in",
				"contract_address": "0xabasbtc",
				"sender":           "0xaave",
				"recipient":        "0x1234",
			},
		},
	}

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)

	// Check operation_type and protocol in metadata
	for _, e := range entries {
		assert.Equal(t, "deposit", e.Metadata["operation_type"])
		assert.Equal(t, "AAVE", e.Metadata["protocol"])
	}

	// Check wallet entry metadata
	walletEntry := entries[0]
	assert.Equal(t, walletID.String(), walletEntry.Metadata["wallet_id"])
	assert.Contains(t, walletEntry.Metadata["account_code"], "wallet.")
	assert.Equal(t, "0xmetadata", walletEntry.Metadata["tx_hash"])
	assert.Equal(t, "out", walletEntry.Metadata["direction"])

	// Check clearing entry metadata
	clearingEntry := entries[1]
	assert.Contains(t, clearingEntry.Metadata["account_code"], "clearing.")
	assert.Equal(t, "CLEARING", clearingEntry.Metadata["account_type"])

	assertEntriesBalanced(t, entries)
}

func TestDeFiDepositHandler_Validate_MissingFields(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, mock.AnythingOfType("uuid.UUID")).Return(testWallet(walletID, userID), nil)

	handler := defi.NewDeFiDepositHandler(walletRepo, logger.NewDefault("test"))

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
			expectedErr: defi.ErrInvalidWalletID,
		},
		{
			name: "missing tx_hash",
			modifyData: func(data map[string]interface{}) {
				data["tx_hash"] = ""
			},
			expectedErr: defi.ErrInvalidTxHash,
		},
		{
			name: "missing chain_id",
			modifyData: func(data map[string]interface{}) {
				data["chain_id"] = ""
			},
			expectedErr: defi.ErrInvalidChainID,
		},
		{
			name: "empty transfers",
			modifyData: func(data map[string]interface{}) {
				data["transfers"] = []map[string]interface{}{}
			},
			expectedErr: defi.ErrNoTransfers,
		},
		{
			name: "negative amount",
			modifyData: func(data map[string]interface{}) {
				data["transfers"] = []map[string]interface{}{
					{
						"asset_symbol": "ETH",
						"amount":       "-1",
						"decimals":     18,
						"usd_price":    "0",
						"direction":    "in",
					},
				}
			},
			expectedErr: defi.ErrInvalidAmount,
		},
		{
			name: "missing asset symbol",
			modifyData: func(data map[string]interface{}) {
				data["transfers"] = []map[string]interface{}{
					{
						"asset_symbol": "",
						"amount":       "100",
						"decimals":     8,
						"usd_price":    "0",
						"direction":    "in",
					},
				}
			},
			expectedErr: defi.ErrInvalidAssetID,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := validDepositData(walletID)
			tc.modifyData(data)

			err := handler.ValidateData(ctx, data)
			assert.ErrorIs(t, err, tc.expectedErr)
		})
	}
}

func TestDeFiDepositHandler_Validate_WalletNotFound(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, walletID).Return(nil, nil)

	handler := defi.NewDeFiDepositHandler(walletRepo, logger.NewDefault("test"))

	data := validDepositData(walletID)
	err := handler.ValidateData(ctx, data)
	assert.ErrorIs(t, err, defi.ErrWalletNotFound)
}

// =============================================================================
// DeFi Withdraw Handler Tests
// =============================================================================

func TestDeFiWithdrawHandler_Type(t *testing.T) {
	handler := defi.NewDeFiWithdrawHandler(nil, logger.NewDefault("test"))
	assert.Equal(t, ledger.TxTypeDefiWithdraw, handler.Type())
}

func TestDeFiWithdrawHandler_SimpleWithdraw_Balance(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, walletID).Return(testWallet(walletID, userID), nil)

	handler := defi.NewDeFiWithdrawHandler(walletRepo, logger.NewDefault("test"))

	// Flux Finance withdraw: fUSDC out, USDC in
	data := map[string]interface{}{
		"wallet_id":      walletID.String(),
		"tx_hash":        "0xfluxwithdraw",
		"chain_id":       "ethereum",
		"occurred_at":    time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"protocol":       "Flux Finance",
		"operation_type": "withdraw",
		"transfers": []map[string]interface{}{
			{
				"asset_symbol":     "fUSDC",
				"amount":           "1360462608",
				"decimals":         6,
				"usd_price":        "0",
				"direction":        "out",
				"contract_address": "0xfusdc",
				"sender":           "0x1234",
				"recipient":        "0xflux",
			},
			{
				"asset_symbol":     "USDC",
				"amount":           "1500000000",
				"decimals":         6,
				"usd_price":        "99920006",
				"direction":        "in",
				"contract_address": "0xusdc",
				"sender":           "0xflux",
				"recipient":        "0x1234",
			},
		},
	}

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 4, "Simple withdraw should generate 4 entries")

	// Verify OUT pair: CREDIT wallet fUSDC + DEBIT clearing fUSDC
	assert.Equal(t, ledger.Credit, entries[0].DebitCredit)
	assert.Equal(t, ledger.EntryTypeAssetDecrease, entries[0].EntryType)
	assert.Equal(t, "fUSDC", entries[0].AssetID)

	// Verify IN pair: DEBIT wallet USDC + CREDIT clearing USDC
	assert.Equal(t, ledger.Debit, entries[2].DebitCredit)
	assert.Equal(t, ledger.EntryTypeAssetIncrease, entries[2].EntryType)
	assert.Equal(t, "USDC", entries[2].AssetID)

	assertEntriesBalanced(t, entries)
}

func TestDeFiWithdrawHandler_WithGasFee_Balance(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, walletID).Return(testWallet(walletID, userID), nil)

	handler := defi.NewDeFiWithdrawHandler(walletRepo, logger.NewDefault("test"))

	data := map[string]interface{}{
		"wallet_id":      walletID.String(),
		"tx_hash":        "0xwithdrawgas",
		"chain_id":       "ethereum",
		"occurred_at":    time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"operation_type": "withdraw",
		"fee_asset":      "ETH",
		"fee_amount":     money.NewBigIntFromInt64(50000000000000).String(),
		"fee_decimals":   18,
		"fee_usd_price":  "200000000000",
		"transfers": []map[string]interface{}{
			{
				"asset_symbol": "fUSDC",
				"amount":       "1000000",
				"decimals":     6,
				"usd_price":    "0",
				"direction":    "out",
			},
			{
				"asset_symbol": "USDC",
				"amount":       "1000000",
				"decimals":     6,
				"usd_price":    "100000000",
				"direction":    "in",
			},
		},
	}

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 6, "Withdraw with gas should generate 6 entries")

	assertEntriesBalanced(t, entries)
}

func TestDeFiWithdrawHandler_Validate_MissingFields(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, mock.AnythingOfType("uuid.UUID")).Return(testWallet(walletID, userID), nil)

	handler := defi.NewDeFiWithdrawHandler(walletRepo, logger.NewDefault("test"))

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
			expectedErr: defi.ErrInvalidWalletID,
		},
		{
			name: "empty transfers",
			modifyData: func(data map[string]interface{}) {
				data["transfers"] = []map[string]interface{}{}
			},
			expectedErr: defi.ErrNoTransfers,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := validWithdrawData(walletID)
			tc.modifyData(data)

			err := handler.ValidateData(ctx, data)
			assert.ErrorIs(t, err, tc.expectedErr)
		})
	}
}

func TestDeFiWithdrawHandler_EntryMetadata(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, walletID).Return(testWallet(walletID, userID), nil)

	handler := defi.NewDeFiWithdrawHandler(walletRepo, logger.NewDefault("test"))

	data := map[string]interface{}{
		"wallet_id":      walletID.String(),
		"tx_hash":        "0xwdmetadata",
		"chain_id":       "ethereum",
		"occurred_at":    time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"protocol":       "Flux Finance",
		"operation_type": "burn",
		"transfers": []map[string]interface{}{
			{
				"asset_symbol": "fUSDC",
				"amount":       "100",
				"decimals":     6,
				"usd_price":    "0",
				"direction":    "out",
			},
			{
				"asset_symbol": "USDC",
				"amount":       "100",
				"decimals":     6,
				"usd_price":    "100000000",
				"direction":    "in",
			},
		},
	}

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)

	for _, e := range entries {
		assert.Equal(t, "burn", e.Metadata["operation_type"])
		assert.Equal(t, "Flux Finance", e.Metadata["protocol"])
	}

	assertEntriesBalanced(t, entries)
}

// =============================================================================
// DeFi Claim Handler Tests
// =============================================================================

func TestDeFiClaimHandler_Type(t *testing.T) {
	handler := defi.NewDeFiClaimHandler(nil, logger.NewDefault("test"))
	assert.Equal(t, ledger.TxTypeDefiClaim, handler.Type())
}

func TestDeFiClaimHandler_SimpleClaim_Balance(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, walletID).Return(testWallet(walletID, userID), nil)

	handler := defi.NewDeFiClaimHandler(walletRepo, logger.NewDefault("test"))

	data := map[string]interface{}{
		"wallet_id":      walletID.String(),
		"tx_hash":        "0xclaimrewards",
		"chain_id":       "ethereum",
		"occurred_at":    time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"protocol":       "AAVE",
		"operation_type": "claim",
		"transfers": []map[string]interface{}{
			{
				"asset_symbol":     "AAVE",
				"amount":           "500000000000000000",
				"decimals":         18,
				"usd_price":        "15000000000",
				"direction":        "in",
				"contract_address": "0xaave",
				"sender":           "0xaavepool",
				"recipient":        "0x1234",
			},
		},
	}

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 2, "Simple claim should generate 2 entries (wallet debit + income credit)")

	// Verify wallet entry (asset increase)
	assert.Equal(t, ledger.Debit, entries[0].DebitCredit)
	assert.Equal(t, ledger.EntryTypeAssetIncrease, entries[0].EntryType)
	assert.Equal(t, "AAVE", entries[0].AssetID)

	// Verify income entry
	assert.Equal(t, ledger.Credit, entries[1].DebitCredit)
	assert.Equal(t, ledger.EntryTypeIncome, entries[1].EntryType)
	assert.Equal(t, "AAVE", entries[1].AssetID)
	assert.Contains(t, entries[1].Metadata["account_code"], "income.defi.")

	assertEntriesBalanced(t, entries)
}

func TestDeFiClaimHandler_WithGasFee_Balance(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, walletID).Return(testWallet(walletID, userID), nil)

	handler := defi.NewDeFiClaimHandler(walletRepo, logger.NewDefault("test"))

	data := map[string]interface{}{
		"wallet_id":      walletID.String(),
		"tx_hash":        "0xclaimwithgas",
		"chain_id":       "ethereum",
		"occurred_at":    time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"operation_type": "claim",
		"fee_asset":      "ETH",
		"fee_amount":     money.NewBigIntFromInt64(30000000000000).String(),
		"fee_decimals":   18,
		"fee_usd_price":  "200000000000",
		"transfers": []map[string]interface{}{
			{
				"asset_symbol": "COMP",
				"amount":       "1000000000000000000",
				"decimals":     18,
				"usd_price":    "5000000000",
				"direction":    "in",
			},
		},
	}

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 4, "Claim with gas should generate 4 entries (2 income + 2 gas)")

	assertEntriesBalanced(t, entries)
}

func TestDeFiClaimHandler_MultipleRewards_Balance(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, walletID).Return(testWallet(walletID, userID), nil)

	handler := defi.NewDeFiClaimHandler(walletRepo, logger.NewDefault("test"))

	data := map[string]interface{}{
		"wallet_id":      walletID.String(),
		"tx_hash":        "0xmultireward",
		"chain_id":       "ethereum",
		"occurred_at":    time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"operation_type": "claim",
		"transfers": []map[string]interface{}{
			{
				"asset_symbol": "AAVE",
				"amount":       "500000000000000000",
				"decimals":     18,
				"usd_price":    "15000000000",
				"direction":    "in",
			},
			{
				"asset_symbol": "stkAAVE",
				"amount":       "200000000000000000",
				"decimals":     18,
				"usd_price":    "14000000000",
				"direction":    "in",
			},
		},
	}

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 4, "Multiple rewards should generate 4 entries (2 per reward)")

	// Verify both reward pairs
	assert.Equal(t, "AAVE", entries[0].AssetID)
	assert.Equal(t, ledger.EntryTypeAssetIncrease, entries[0].EntryType)
	assert.Equal(t, "AAVE", entries[1].AssetID)
	assert.Equal(t, ledger.EntryTypeIncome, entries[1].EntryType)

	assert.Equal(t, "stkAAVE", entries[2].AssetID)
	assert.Equal(t, ledger.EntryTypeAssetIncrease, entries[2].EntryType)
	assert.Equal(t, "stkAAVE", entries[3].AssetID)
	assert.Equal(t, ledger.EntryTypeIncome, entries[3].EntryType)

	assertEntriesBalanced(t, entries)
}

func TestDeFiClaimHandler_Validate_NoInTransfers(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, mock.AnythingOfType("uuid.UUID")).Return(testWallet(walletID, userID), nil)

	handler := defi.NewDeFiClaimHandler(walletRepo, logger.NewDefault("test"))

	// Claim with only OUT transfers (invalid)
	data := map[string]interface{}{
		"wallet_id":      walletID.String(),
		"tx_hash":        "0xinvalidclaim",
		"chain_id":       "ethereum",
		"occurred_at":    time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"operation_type": "claim",
		"transfers": []map[string]interface{}{
			{
				"asset_symbol": "ETH",
				"amount":       "100",
				"decimals":     18,
				"usd_price":    "100",
				"direction":    "out",
			},
		},
	}

	err := handler.ValidateData(ctx, data)
	assert.ErrorIs(t, err, defi.ErrNoInTransfers)
}

func TestDeFiClaimHandler_EntryMetadata(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, walletID).Return(testWallet(walletID, userID), nil)

	handler := defi.NewDeFiClaimHandler(walletRepo, logger.NewDefault("test"))

	data := map[string]interface{}{
		"wallet_id":      walletID.String(),
		"tx_hash":        "0xclaimmeta",
		"chain_id":       "arbitrum",
		"occurred_at":    time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"protocol":       "GMX",
		"operation_type": "claim",
		"transfers": []map[string]interface{}{
			{
				"asset_symbol":     "WETH",
				"amount":           "100000000000000",
				"decimals":         18,
				"usd_price":        "200000000000",
				"direction":        "in",
				"contract_address": "0xweth",
			},
		},
	}

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	// Wallet entry metadata
	walletEntry := entries[0]
	assert.Equal(t, "claim", walletEntry.Metadata["operation_type"])
	assert.Equal(t, "GMX", walletEntry.Metadata["protocol"])
	assert.Equal(t, walletID.String(), walletEntry.Metadata["wallet_id"])
	assert.Contains(t, walletEntry.Metadata["account_code"], "wallet.")

	// Income entry metadata
	incomeEntry := entries[1]
	assert.Equal(t, "claim", incomeEntry.Metadata["operation_type"])
	assert.Equal(t, "GMX", incomeEntry.Metadata["protocol"])
	expectedIncomeCode := "income.defi.arbitrum.WETH"
	assert.Equal(t, expectedIncomeCode, incomeEntry.Metadata["account_code"])

	assertEntriesBalanced(t, entries)
}

// =============================================================================
// Item C: USD price fallback with DIFFERENT amounts (IN ≠ OUT)
// =============================================================================

func TestDeFiDepositHandler_USDPriceFallback_DifferentAmounts(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, walletID).Return(testWallet(walletID, userID), nil)

	handler := defi.NewDeFiDepositHandler(walletRepo, logger.NewDefault("test"))

	// AAVE deposit with different IN and OUT amounts (realistic scenario)
	// OUT: cbBTC amount=981547, decimals=8, usd_price=6795302440668
	//   totalOutUSDValue = CalcUSDValue(981547, 6795302440668, 8)
	//     = (981547 * 6795302440668) / 10^8 = 66699087247 (integer truncation)
	// IN: aBascbBTC amount=981581, decimals=8, usd_price=0
	//   fallback usdRate = (66699087247 * 10^8) / 981581 = 6795067064969
	//   usdValue = CalcUSDValue(981581, 6795067064969, 8) = 66699087246
	data := map[string]interface{}{
		"wallet_id":      walletID.String(),
		"tx_hash":        "0xfallbackdiff",
		"chain_id":       "base",
		"occurred_at":    time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"operation_type": "deposit",
		"transfers": []map[string]interface{}{
			{
				"asset_symbol":     "cbBTC",
				"amount":           "981547",
				"decimals":         8,
				"usd_price":        "6795302440668",
				"direction":        "out",
				"contract_address": "0xcbbtc",
				"sender":           "0x1234",
				"recipient":        "0xaave",
			},
			{
				"asset_symbol":     "aBascbBTC",
				"amount":           "981581",
				"decimals":         8,
				"usd_price":        "0",
				"direction":        "in",
				"contract_address": "0xabasbtc",
				"sender":           "0xaave",
				"recipient":        "0x1234",
			},
		},
	}

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 4)

	// Verify OUT entry USD values
	outWalletEntry := entries[0]
	assert.Equal(t, "66699087247", outWalletEntry.USDValue.String(),
		"OUT entry USD value: (981547 * 6795302440668) / 10^8")

	// Verify IN entry computed fallback values
	inWalletEntry := entries[2]
	assert.Equal(t, ledger.EntryTypeAssetIncrease, inWalletEntry.EntryType)
	assert.Equal(t, "6795067064969", inWalletEntry.USDRate.String(),
		"IN transfer fallback rate: (66699087247 * 10^8) / 981581")
	assert.Equal(t, "66699087246", inWalletEntry.USDValue.String(),
		"IN transfer USD value: CalcUSDValue(981581, 6795067064969, 8)")

	// The $0.00001 difference between OUT and IN USD values is expected integer truncation
	assertEntriesBalanced(t, entries)
}

// =============================================================================
// Item D: Zero-amount validation
// =============================================================================

func TestDeFiDepositHandler_Validate_ZeroAmount(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, mock.AnythingOfType("uuid.UUID")).Return(testWallet(walletID, userID), nil)

	handler := defi.NewDeFiDepositHandler(walletRepo, logger.NewDefault("test"))

	data := validDepositData(walletID)
	data["transfers"] = []map[string]interface{}{
		{
			"asset_symbol": "ETH",
			"amount":       "0",
			"decimals":     18,
			"usd_price":    "0",
			"direction":    "in",
		},
	}

	err := handler.ValidateData(ctx, data)
	assert.ErrorIs(t, err, defi.ErrInvalidAmount)
}

// =============================================================================
// Item D (cont): Negative decimals validation
// =============================================================================

func TestDeFiDepositHandler_Validate_NegativeDecimals(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, mock.AnythingOfType("uuid.UUID")).Return(testWallet(walletID, userID), nil)

	handler := defi.NewDeFiDepositHandler(walletRepo, logger.NewDefault("test"))

	data := validDepositData(walletID)
	data["transfers"] = []map[string]interface{}{
		{
			"asset_symbol": "ETH",
			"amount":       "100",
			"decimals":     -1,
			"usd_price":    "0",
			"direction":    "in",
		},
	}

	err := handler.ValidateData(ctx, data)
	assert.ErrorIs(t, err, defi.ErrInvalidDecimals)
}

// =============================================================================
// Item E: Authorization tests for all 3 handlers
// =============================================================================

func TestDeFiHandlers_Unauthorized(t *testing.T) {
	ownerID := uuid.New()
	attackerID := uuid.New()
	walletID := uuid.New()

	testCases := []struct {
		name        string
		newHandler  func(defi.WalletRepository, *logger.Logger) interface{ ValidateData(context.Context, map[string]interface{}) error }
		dataFunc    func(uuid.UUID) map[string]interface{}
	}{
		{
			name: "deposit handler",
			newHandler: func(repo defi.WalletRepository, log *logger.Logger) interface{ ValidateData(context.Context, map[string]interface{}) error } {
				return defi.NewDeFiDepositHandler(repo, log)
			},
			dataFunc: validDepositData,
		},
		{
			name: "withdraw handler",
			newHandler: func(repo defi.WalletRepository, log *logger.Logger) interface{ ValidateData(context.Context, map[string]interface{}) error } {
				return defi.NewDeFiWithdrawHandler(repo, log)
			},
			dataFunc: validWithdrawData,
		},
		{
			name: "claim handler",
			newHandler: func(repo defi.WalletRepository, log *logger.Logger) interface{ ValidateData(context.Context, map[string]interface{}) error } {
				return defi.NewDeFiClaimHandler(repo, log)
			},
			dataFunc: validClaimData,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Wallet belongs to ownerID
			w := testWallet(walletID, ownerID)

			walletRepo := new(MockWalletRepository)
			walletRepo.On("GetByID", mock.Anything, walletID).Return(w, nil)

			handler := tc.newHandler(walletRepo, logger.NewDefault("test"))

			// Context has attackerID (different user)
			ctx := context.WithValue(context.Background(), middleware.UserIDKey, attackerID)

			data := tc.dataFunc(walletID)
			err := handler.ValidateData(ctx, data)
			assert.ErrorIs(t, err, defi.ErrUnauthorized)
		})
	}
}

// =============================================================================
// Item F: Multi-asset DeFi deposit (2 OUT + 2 IN)
// =============================================================================

func TestDeFiDepositHandler_MultiAsset_Balance(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, walletID).Return(testWallet(walletID, userID), nil)

	handler := defi.NewDeFiDepositHandler(walletRepo, logger.NewDefault("test"))

	// Multi-asset deposit: 2 OUT + 2 IN
	data := map[string]interface{}{
		"wallet_id":      walletID.String(),
		"tx_hash":        "0xmultideposit",
		"chain_id":       "arbitrum",
		"occurred_at":    time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"protocol":       "GMX",
		"operation_type": "deposit",
		"transfers": []map[string]interface{}{
			{
				"asset_symbol":     "ETH",
				"amount":           money.NewBigIntFromInt64(1000000000000000000).String(),
				"decimals":         18,
				"usd_price":        "200000000000",
				"direction":        "out",
				"contract_address": "",
				"sender":           "0x1234",
				"recipient":        "0xgmx",
			},
			{
				"asset_symbol":     "USDC",
				"amount":           money.NewBigIntFromInt64(2000000000).String(),
				"decimals":         6,
				"usd_price":        "100000000",
				"direction":        "out",
				"contract_address": "0xusdc",
				"sender":           "0x1234",
				"recipient":        "0xgmx",
			},
			{
				"asset_symbol":     "GM-ETH-USDC",
				"amount":           money.NewBigIntFromInt64(500000000000000000).String(),
				"decimals":         18,
				"usd_price":        "0",
				"direction":        "in",
				"contract_address": "0xgm-eth-usdc",
				"sender":           "0xgmx",
				"recipient":        "0x1234",
			},
			{
				"asset_symbol":     "esGMX",
				"amount":           money.NewBigIntFromInt64(100000000000000000).String(),
				"decimals":         18,
				"usd_price":        "0",
				"direction":        "in",
				"contract_address": "0xesgmx",
				"sender":           "0xgmx",
				"recipient":        "0x1234",
			},
		},
	}

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 8, "Multi-asset deposit should generate 8 entries (2 per transfer)")

	// Verify OUT entries
	assert.Equal(t, ledger.Credit, entries[0].DebitCredit)
	assert.Equal(t, "ETH", entries[0].AssetID)
	assert.Equal(t, ledger.Debit, entries[1].DebitCredit)
	assert.Equal(t, ledger.EntryTypeClearing, entries[1].EntryType)

	assert.Equal(t, ledger.Credit, entries[2].DebitCredit)
	assert.Equal(t, "USDC", entries[2].AssetID)
	assert.Equal(t, ledger.Debit, entries[3].DebitCredit)
	assert.Equal(t, ledger.EntryTypeClearing, entries[3].EntryType)

	// Verify IN entries
	assert.Equal(t, ledger.Debit, entries[4].DebitCredit)
	assert.Equal(t, "GM-ETH-USDC", entries[4].AssetID)
	assert.Equal(t, ledger.Credit, entries[5].DebitCredit)
	assert.Equal(t, ledger.EntryTypeClearing, entries[5].EntryType)

	assert.Equal(t, ledger.Debit, entries[6].DebitCredit)
	assert.Equal(t, "esGMX", entries[6].AssetID)
	assert.Equal(t, ledger.Credit, entries[7].DebitCredit)
	assert.Equal(t, ledger.EntryTypeClearing, entries[7].EntryType)

	assertEntriesBalanced(t, entries)
}

// =============================================================================
// Item G: Expanded withdraw validation tests
// =============================================================================

func TestDeFiWithdrawHandler_Validate_ExpandedFields(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, mock.AnythingOfType("uuid.UUID")).Return(testWallet(walletID, userID), nil)

	handler := defi.NewDeFiWithdrawHandler(walletRepo, logger.NewDefault("test"))

	testCases := []struct {
		name        string
		modifyData  func(map[string]interface{})
		expectedErr error
	}{
		{
			name: "missing tx_hash",
			modifyData: func(data map[string]interface{}) {
				data["tx_hash"] = ""
			},
			expectedErr: defi.ErrInvalidTxHash,
		},
		{
			name: "missing chain_id",
			modifyData: func(data map[string]interface{}) {
				data["chain_id"] = ""
			},
			expectedErr: defi.ErrInvalidChainID,
		},
		{
			name: "negative amount",
			modifyData: func(data map[string]interface{}) {
				data["transfers"] = []map[string]interface{}{
					{
						"asset_symbol": "USDC",
						"amount":       "-1",
						"decimals":     6,
						"usd_price":    "0",
						"direction":    "out",
					},
				}
			},
			expectedErr: defi.ErrInvalidAmount,
		},
		{
			name: "missing asset symbol",
			modifyData: func(data map[string]interface{}) {
				data["transfers"] = []map[string]interface{}{
					{
						"asset_symbol": "",
						"amount":       "100",
						"decimals":     6,
						"usd_price":    "0",
						"direction":    "out",
					},
				}
			},
			expectedErr: defi.ErrInvalidAssetID,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := validWithdrawData(walletID)
			tc.modifyData(data)

			err := handler.ValidateData(ctx, data)
			assert.ErrorIs(t, err, tc.expectedErr)
		})
	}
}

// =============================================================================
// Helpers
// =============================================================================

func validClaimData(walletID uuid.UUID) map[string]interface{} {
	return map[string]interface{}{
		"wallet_id":      walletID.String(),
		"tx_hash":        "0xclaim123",
		"chain_id":       "ethereum",
		"occurred_at":    time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"operation_type": "claim",
		"transfers": []map[string]interface{}{
			{
				"asset_symbol": "AAVE",
				"amount":       "500000000000000000",
				"decimals":     18,
				"usd_price":    "15000000000",
				"direction":    "in",
			},
		},
	}
}

func validDepositData(walletID uuid.UUID) map[string]interface{} {
	return map[string]interface{}{
		"wallet_id":      walletID.String(),
		"tx_hash":        "0xdeposit123",
		"chain_id":       "base",
		"occurred_at":    time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"operation_type": "deposit",
		"transfers": []map[string]interface{}{
			{
				"asset_symbol":     "cbBTC",
				"amount":           "100",
				"decimals":         8,
				"usd_price":        "6700000000000",
				"direction":        "out",
				"contract_address": "0xcbbtc",
				"sender":           "0x1234",
				"recipient":        "0xaave",
			},
			{
				"asset_symbol":     "aBascbBTC",
				"amount":           "100",
				"decimals":         8,
				"usd_price":        "0",
				"direction":        "in",
				"contract_address": "0xabasbtc",
				"sender":           "0xaave",
				"recipient":        "0x1234",
			},
		},
	}
}

func validWithdrawData(walletID uuid.UUID) map[string]interface{} {
	return map[string]interface{}{
		"wallet_id":      walletID.String(),
		"tx_hash":        "0xwithdraw123",
		"chain_id":       "ethereum",
		"occurred_at":    time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"operation_type": "withdraw",
		"transfers": []map[string]interface{}{
			{
				"asset_symbol": "fUSDC",
				"amount":       "1000000",
				"decimals":     6,
				"usd_price":    "0",
				"direction":    "out",
			},
			{
				"asset_symbol": "USDC",
				"amount":       "1000000",
				"decimals":     6,
				"usd_price":    "100000000",
				"direction":    "in",
			},
		},
	}
}
