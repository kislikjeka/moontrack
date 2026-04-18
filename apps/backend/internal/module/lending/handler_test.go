package lending_test

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
	"github.com/kislikjeka/moontrack/internal/module/lending"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
	"github.com/kislikjeka/moontrack/pkg/logger"
	"github.com/kislikjeka/moontrack/pkg/money"
)

func testWallet(walletID, userID uuid.UUID) *wallet.Wallet {
	return &wallet.Wallet{
		ID:     walletID,
		UserID: userID,
	}
}

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
		"entries must balance: debits=%s credits=%s", debitSum.String(), creditSum.String())
}

func buildTestData(walletID uuid.UUID) map[string]interface{} {
	return map[string]interface{}{
		"wallet_id":        walletID.String(),
		"tx_hash":          "0xtest123",
		"chain_id":         "ethereum",
		"occurred_at":      time.Now().UTC().Format(time.RFC3339),
		"protocol":         "Aave V3",
		"asset":            "ETH",
		"amount":           "1000000000000000000",
		"decimals":         float64(18),
		"usd_price":        "200000000000",
		"contract_address": "0xcontract",
	}
}

func setupHandler(t *testing.T) (uuid.UUID, uuid.UUID, *MockWalletRepository, *logger.Logger, context.Context) {
	t.Helper()
	userID := uuid.New()
	walletID := uuid.New()

	mockRepo := new(MockWalletRepository)
	mockRepo.On("GetByID", mock.Anything, walletID).Return(testWallet(walletID, userID), nil)

	log := logger.NewDefault("test")
	ctx := context.WithValue(context.Background(), middleware.UserIDKey, userID)

	return userID, walletID, mockRepo, log, ctx
}

// === Supply Handler ===

func TestLendingSupplyHandler_Handle(t *testing.T) {
	_, walletID, mockRepo, log, ctx := setupHandler(t)

	handler := lending.NewLendingSupplyHandler(mockRepo, log)
	assert.Equal(t, ledger.TxTypeLendingSupply, handler.Type())

	data := buildTestData(walletID)
	entries, err := handler.Handle(ctx, data)

	require.NoError(t, err)
	require.Len(t, entries, 2)
	assertEntriesBalanced(t, entries)
}

func TestLendingSupplyHandler_WithGasFee(t *testing.T) {
	_, walletID, mockRepo, log, ctx := setupHandler(t)

	handler := lending.NewLendingSupplyHandler(mockRepo, log)
	data := buildTestData(walletID)
	data["fee_asset"] = "ETH"
	data["fee_amount"] = "500000000000000"
	data["fee_decimals"] = float64(18)
	data["fee_usd_price"] = "200000000000"

	entries, err := handler.Handle(ctx, data)

	require.NoError(t, err)
	require.Len(t, entries, 4) // 2 supply + 2 gas
	assertEntriesBalanced(t, entries)
}

func TestLendingSupplyHandler_ValidateData_MissingAsset(t *testing.T) {
	_, walletID, mockRepo, log, ctx := setupHandler(t)

	handler := lending.NewLendingSupplyHandler(mockRepo, log)
	data := buildTestData(walletID)
	delete(data, "asset")

	err := handler.ValidateData(ctx, data)
	assert.Error(t, err)
}

func TestLendingSupplyHandler_ValidateData_Unauthorized(t *testing.T) {
	_, walletID, _, log, _ := setupHandler(t)

	attackerID := uuid.New()
	mockRepo := new(MockWalletRepository)
	mockRepo.On("GetByID", mock.Anything, walletID).Return(testWallet(walletID, uuid.New()), nil)

	handler := lending.NewLendingSupplyHandler(mockRepo, log)
	data := buildTestData(walletID)
	ctx := context.WithValue(context.Background(), middleware.UserIDKey, attackerID)

	err := handler.ValidateData(ctx, data)
	assert.Error(t, err)
}

// === Withdraw Handler ===

func TestLendingWithdrawHandler_Handle(t *testing.T) {
	_, walletID, mockRepo, log, ctx := setupHandler(t)

	handler := lending.NewLendingWithdrawHandler(mockRepo, log)
	assert.Equal(t, ledger.TxTypeLendingWithdraw, handler.Type())

	entries, err := handler.Handle(ctx, buildTestData(walletID))

	require.NoError(t, err)
	require.Len(t, entries, 2)
	assertEntriesBalanced(t, entries)
}

// === Borrow Handler ===

func TestLendingBorrowHandler_Handle(t *testing.T) {
	_, walletID, mockRepo, log, ctx := setupHandler(t)

	handler := lending.NewLendingBorrowHandler(mockRepo, log)
	assert.Equal(t, ledger.TxTypeLendingBorrow, handler.Type())

	data := buildTestData(walletID)
	data["asset"] = "USDC"
	data["decimals"] = float64(6)

	entries, err := handler.Handle(ctx, data)

	require.NoError(t, err)
	require.Len(t, entries, 2)
	assertEntriesBalanced(t, entries)
}

// TestLendingBorrowHandler_MultiAsset verifies that a borrow tx with BOTH
// the debt receipt token AND the real borrowed asset emits balanced pairs
// for each. Regression test for the multi-asset bug: historically the first
// transfer was kept and the rest were silently dropped, so either the debt
// or the liquid asset never landed in the ledger.
func TestLendingBorrowHandler_MultiAsset(t *testing.T) {
	_, walletID, mockRepo, log, ctx := setupHandler(t)

	handler := lending.NewLendingBorrowHandler(mockRepo, log)

	data := map[string]interface{}{
		"wallet_id":   walletID.String(),
		"tx_hash":     "0xmultiasset",
		"chain_id":    "base",
		"occurred_at": time.Now().UTC().Format(time.RFC3339),
		"protocol":    "Aave V3",
		"transfers": []map[string]interface{}{
			{
				// Liquid borrowed asset
				"asset_id":         "USDC",
				"decimals":         6,
				"amount":           "400000000",
				"usd_rate":         "100000000",
				"contract_address": "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
				"direction":        "in",
			},
			{
				// Debt receipt token
				"asset_id":         "variableDebtBasUSDC",
				"decimals":         6,
				"amount":           "400000000",
				"usd_rate":         "0",
				"contract_address": "0xdebtcontract",
				"direction":        "in",
			},
		},
	}

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 4, "expected 2 entries per asset × 2 assets")

	// Locate per-asset entries by AssetID.
	perAsset := map[string][]*ledger.Entry{
		"USDC":                {},
		"variableDebtBasUSDC": {},
	}
	for _, e := range entries {
		perAsset[e.AssetID] = append(perAsset[e.AssetID], e)
	}
	require.Len(t, perAsset["USDC"], 2, "USDC should have one debit+credit pair")
	require.Len(t, perAsset["variableDebtBasUSDC"], 2, "debt token should have one debit+credit pair")

	// Per-asset balance and account routing.
	for asset, es := range perAsset {
		var debitSum, creditSum = new(big.Int), new(big.Int)
		for _, e := range es {
			if e.DebitCredit == ledger.Debit {
				debitSum.Add(debitSum, e.Amount)
			} else {
				creditSum.Add(creditSum, e.Amount)
			}
		}
		assert.Equal(t, 0, debitSum.Cmp(creditSum),
			"%s entries must balance: debit=%s credit=%s",
			asset, debitSum.String(), creditSum.String())
	}

	// USDC (liquid) debit must be the user's wallet account; credit is liability.
	for _, e := range perAsset["USDC"] {
		code, _ := e.Metadata["account_code"].(string)
		if e.DebitCredit == ledger.Debit {
			assert.Contains(t, code, "wallet.", "USDC debit should land on wallet account, got %s", code)
			assert.Equal(t, ledger.EntryTypeAssetIncrease, e.EntryType)
		} else {
			assert.Contains(t, code, "liability.", "USDC credit should land on liability account, got %s", code)
			assert.Equal(t, ledger.EntryTypeLiabilityIncrease, e.EntryType)
		}
	}

	// Debt receipt: debit clears through protocol, credit on liability.
	for _, e := range perAsset["variableDebtBasUSDC"] {
		code, _ := e.Metadata["account_code"].(string)
		if e.DebitCredit == ledger.Debit {
			assert.Contains(t, code, "clearing.", "debt token debit should clear, got %s", code)
			assert.Equal(t, ledger.EntryTypeClearing, e.EntryType)
		} else {
			assert.Contains(t, code, "liability.", "debt token credit should be liability, got %s", code)
			assert.Equal(t, ledger.EntryTypeLiabilityIncrease, e.EntryType)
		}
	}
}

// === Repay Handler ===

func TestLendingRepayHandler_Handle(t *testing.T) {
	_, walletID, mockRepo, log, ctx := setupHandler(t)

	handler := lending.NewLendingRepayHandler(mockRepo, log)
	assert.Equal(t, ledger.TxTypeLendingRepay, handler.Type())

	data := buildTestData(walletID)
	data["asset"] = "USDC"
	data["decimals"] = float64(6)

	entries, err := handler.Handle(ctx, data)

	require.NoError(t, err)
	require.Len(t, entries, 2)
	assertEntriesBalanced(t, entries)
}

// === Claim Handler ===

func TestLendingClaimHandler_Handle(t *testing.T) {
	_, walletID, mockRepo, log, ctx := setupHandler(t)

	handler := lending.NewLendingClaimHandler(mockRepo, log)
	assert.Equal(t, ledger.TxTypeLendingClaim, handler.Type())

	data := buildTestData(walletID)
	data["asset"] = "AAVE"

	entries, err := handler.Handle(ctx, data)

	require.NoError(t, err)
	require.Len(t, entries, 2)
	assertEntriesBalanced(t, entries)
}

// === Model Validation ===

func TestLendingTransaction_Validate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(txn *lending.LendingTransaction)
		wantErr bool
	}{
		{"valid", func(txn *lending.LendingTransaction) {}, false},
		{"missing wallet_id", func(txn *lending.LendingTransaction) { txn.WalletID = uuid.Nil }, true},
		{"missing tx_hash", func(txn *lending.LendingTransaction) { txn.TxHash = "" }, true},
		{"missing chain_id", func(txn *lending.LendingTransaction) { txn.ChainID = "" }, true},
		{"missing asset", func(txn *lending.LendingTransaction) { txn.Asset = "" }, true},
		{"nil amount", func(txn *lending.LendingTransaction) { txn.Amount = nil }, true},
		{"zero amount", func(txn *lending.LendingTransaction) { txn.Amount = money.NewBigInt(big.NewInt(0)) }, true},
		{"zero decimals", func(txn *lending.LendingTransaction) { txn.Decimals = 0 }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			txn := &lending.LendingTransaction{
				WalletID: uuid.New(),
				TxHash:   "0xtest",
				ChainID:  "ethereum",
				Asset:    "ETH",
				Amount:   money.NewBigInt(big.NewInt(1000)),
				Decimals: 18,
			}
			tt.modify(txn)
			err := txn.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
