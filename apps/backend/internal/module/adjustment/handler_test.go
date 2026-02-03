package adjustment_test

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/module/adjustment"
)

var (
	ErrTestAccountNotFound = errors.New("account not found")
)

// MockLedgerRepository is a mock implementation of LedgerRepository
type MockLedgerRepository struct {
	mock.Mock
}

func (m *MockLedgerRepository) CreateAccount(ctx context.Context, account *ledger.Account) error {
	args := m.Called(ctx, account)
	return args.Error(0)
}

func (m *MockLedgerRepository) GetAccount(ctx context.Context, id uuid.UUID) (*ledger.Account, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ledger.Account), args.Error(1)
}

func (m *MockLedgerRepository) FindAccountsByWallet(ctx context.Context, walletID uuid.UUID) ([]*ledger.Account, error) {
	args := m.Called(ctx, walletID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*ledger.Account), args.Error(1)
}

func (m *MockLedgerRepository) GetAccountByCode(ctx context.Context, code string) (*ledger.Account, error) {
	args := m.Called(ctx, code)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ledger.Account), args.Error(1)
}

func (m *MockLedgerRepository) GetAccountBalance(ctx context.Context, accountID uuid.UUID, assetID string) (*ledger.AccountBalance, error) {
	args := m.Called(ctx, accountID, assetID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ledger.AccountBalance), args.Error(1)
}

func (m *MockLedgerRepository) GetAccountBalanceForUpdate(ctx context.Context, accountID uuid.UUID, assetID string) (*ledger.AccountBalance, error) {
	args := m.Called(ctx, accountID, assetID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ledger.AccountBalance), args.Error(1)
}

func (m *MockLedgerRepository) CreateTransaction(ctx context.Context, tx *ledger.Transaction) error {
	args := m.Called(ctx, tx)
	return args.Error(0)
}

func (m *MockLedgerRepository) GetTransaction(ctx context.Context, id uuid.UUID) (*ledger.Transaction, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ledger.Transaction), args.Error(1)
}

func (m *MockLedgerRepository) FindTransactionsBySource(ctx context.Context, source string, externalID string) (*ledger.Transaction, error) {
	args := m.Called(ctx, source, externalID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ledger.Transaction), args.Error(1)
}

func (m *MockLedgerRepository) ListTransactions(ctx context.Context, filters ledger.TransactionFilters) ([]*ledger.Transaction, error) {
	args := m.Called(ctx, filters)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*ledger.Transaction), args.Error(1)
}

func (m *MockLedgerRepository) GetEntriesByTransaction(ctx context.Context, txID uuid.UUID) ([]*ledger.Entry, error) {
	args := m.Called(ctx, txID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*ledger.Entry), args.Error(1)
}

func (m *MockLedgerRepository) GetEntriesByAccount(ctx context.Context, accountID uuid.UUID) ([]*ledger.Entry, error) {
	args := m.Called(ctx, accountID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*ledger.Entry), args.Error(1)
}

func (m *MockLedgerRepository) UpsertAccountBalance(ctx context.Context, balance *ledger.AccountBalance) error {
	args := m.Called(ctx, balance)
	return args.Error(0)
}

func (m *MockLedgerRepository) GetAccountBalances(ctx context.Context, accountID uuid.UUID) ([]*ledger.AccountBalance, error) {
	args := m.Called(ctx, accountID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*ledger.AccountBalance), args.Error(1)
}

func (m *MockLedgerRepository) BeginTx(ctx context.Context) (context.Context, error) {
	args := m.Called(ctx)
	return args.Get(0).(context.Context), args.Error(1)
}

func (m *MockLedgerRepository) CommitTx(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockLedgerRepository) RollbackTx(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockLedgerRepository) CalculateBalanceFromEntries(ctx context.Context, accountID uuid.UUID, assetID string) (*big.Int, error) {
	args := m.Called(ctx, accountID, assetID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*big.Int), args.Error(1)
}

// Verify interface compliance
var _ ledger.Repository = (*MockLedgerRepository)(nil)

// TestAssetAdjustmentHandler_GenerateEntries_Balance verifies ledger entries balance
func TestAssetAdjustmentHandler_GenerateEntries_Balance(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	assetID := "ETH"

	// Helper to create big.Int from string
	mustParseBigInt := func(s string) *big.Int {
		n, ok := new(big.Int).SetString(s, 10)
		if !ok {
			t.Fatalf("failed to parse big.Int from %s", s)
		}
		return n
	}

	tests := []struct {
		name            string
		txData          map[string]interface{}
		currentBalance  *big.Int
		balanceExists   bool
		expectedEntries int
		wantErr         bool
		expectedErrMsg  string
	}{
		{
			name: "increase balance - ledger entries balance",
			txData: map[string]interface{}{
				"wallet_id":   walletID.String(),
				"asset_id":    assetID,
				"new_balance": mustParseBigInt("5000000000000000000"), // 5 ETH in wei
				"usd_rate":    mustParseBigInt("200000000000"),        // $2000 * 10^8
				"occurred_at": time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			},
			currentBalance:  mustParseBigInt("3000000000000000000"), // 3 ETH
			balanceExists:   true,
			expectedEntries: 2,
			wantErr:         false,
		},
		{
			name: "decrease balance - ledger entries balance",
			txData: map[string]interface{}{
				"wallet_id":   walletID.String(),
				"asset_id":    assetID,
				"new_balance": mustParseBigInt("1000000000000000000"), // 1 ETH in wei
				"usd_rate":    mustParseBigInt("200000000000"),        // $2000 * 10^8
				"occurred_at": time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			},
			currentBalance:  mustParseBigInt("3000000000000000000"), // 3 ETH
			balanceExists:   true,
			expectedEntries: 2,
			wantErr:         false,
		},
		{
			name: "initial balance from zero - ledger entries balance",
			txData: map[string]interface{}{
				"wallet_id":   walletID.String(),
				"asset_id":    assetID,
				"new_balance": mustParseBigInt("10000000000000000000"), // 10 ETH in wei
				"usd_rate":    mustParseBigInt("200000000000"),         // $2000 * 10^8
				"occurred_at": time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			},
			currentBalance:  big.NewInt(0),
			balanceExists:   false,
			expectedEntries: 2,
			wantErr:         false,
		},
		{
			name: "no adjustment needed - same balance",
			txData: map[string]interface{}{
				"wallet_id":   walletID.String(),
				"asset_id":    assetID,
				"new_balance": mustParseBigInt("3000000000000000000"), // 3 ETH in wei
				"usd_rate":    mustParseBigInt("200000000000"),        // $2000 * 10^8
				"occurred_at": time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			},
			currentBalance:  mustParseBigInt("3000000000000000000"), // 3 ETH
			balanceExists:   true,
			expectedEntries: 0,
			wantErr:         true,
			expectedErrMsg:  "no adjustment needed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock for GetAccountBalance
			mockRepo := new(MockLedgerRepository)
			if tt.balanceExists {
				mockRepo.On("GetAccountBalance", ctx, mock.AnythingOfType("uuid.UUID"), assetID).Return(&ledger.AccountBalance{
					AccountID: walletID,
					AssetID:   assetID,
					Balance:   tt.currentBalance,
				}, nil)
			} else {
				mockRepo.On("GetAccountBalance", ctx, mock.AnythingOfType("uuid.UUID"), assetID).Return(nil, ErrTestAccountNotFound)
			}

			// Create ledger service with mock repository
			ledgerSvc := ledger.NewService(mockRepo, nil)
			h := adjustment.NewAssetAdjustmentHandler(ledgerSvc)
			entries, err := h.Handle(ctx, tt.txData)

			if tt.wantErr {
				require.Error(t, err)
				if tt.expectedErrMsg != "" {
					assert.Contains(t, err.Error(), tt.expectedErrMsg)
				}
				return
			}

			require.NoError(t, err)
			assert.Len(t, entries, tt.expectedEntries)

			// CRITICAL: Verify double-entry accounting invariant
			// SUM(debit amounts) MUST equal SUM(credit amounts)
			debitSum := big.NewInt(0)
			creditSum := big.NewInt(0)

			for _, entry := range entries {
				if entry.DebitCredit == ledger.Debit {
					debitSum.Add(debitSum, entry.Amount)
				} else if entry.DebitCredit == ledger.Credit {
					creditSum.Add(creditSum, entry.Amount)
				}
			}

			// Balance invariant: debitSum == creditSum
			assert.Equal(t, 0, debitSum.Cmp(creditSum),
				"Ledger entries must balance: SUM(debits)=%s must equal SUM(credits)=%s",
				debitSum.String(), creditSum.String())

			// Verify all entries have the same asset ID
			for _, entry := range entries {
				assert.Equal(t, assetID, entry.AssetID, "All entries must have the same asset ID")
			}

			// Verify all amounts are non-negative
			for _, entry := range entries {
				assert.GreaterOrEqual(t, entry.Amount.Sign(), 0, "Entry amounts must be non-negative")
			}

			mockRepo.AssertExpectations(t)
		})
	}
}

// TestAssetAdjustmentHandler_ValidateData tests data validation
func TestAssetAdjustmentHandler_ValidateData(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()

	// Helper to create big.Int from string
	mustParseBigInt := func(s string) *big.Int {
		n, ok := new(big.Int).SetString(s, 10)
		if !ok {
			t.Fatalf("failed to parse big.Int from %s", s)
		}
		return n
	}

	tests := []struct {
		name           string
		txData         map[string]interface{}
		wantErr        bool
		expectedErrMsg string
	}{
		{
			name: "valid adjustment data",
			txData: map[string]interface{}{
				"wallet_id":   walletID.String(),
				"asset_id":    "BTC",
				"new_balance": mustParseBigInt("100000000"), // 1 BTC in satoshis
				"occurred_at": time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			},
			wantErr: false,
		},
		{
			name: "missing wallet ID",
			txData: map[string]interface{}{
				"asset_id":    "BTC",
				"new_balance": mustParseBigInt("100000000"),
				"occurred_at": time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			},
			wantErr:        true,
			expectedErrMsg: "invalid wallet ID",
		},
		{
			name: "missing asset ID",
			txData: map[string]interface{}{
				"wallet_id":   walletID.String(),
				"new_balance": mustParseBigInt("100000000"),
				"occurred_at": time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			},
			wantErr:        true,
			expectedErrMsg: "asset ID is required",
		},
		{
			name: "negative balance",
			txData: map[string]interface{}{
				"wallet_id":   walletID.String(),
				"asset_id":    "BTC",
				"new_balance": mustParseBigInt("-100000000"), // Negative
				"occurred_at": time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			},
			wantErr:        true,
			expectedErrMsg: "balance cannot be negative",
		},
		{
			name: "future date",
			txData: map[string]interface{}{
				"wallet_id":   walletID.String(),
				"asset_id":    "BTC",
				"new_balance": mustParseBigInt("100000000"),
				"occurred_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339), // Future
			},
			wantErr:        true,
			expectedErrMsg: "occurred_at cannot be in the future",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := new(MockLedgerRepository)
			ledgerSvc := ledger.NewService(mockRepo, nil)
			h := adjustment.NewAssetAdjustmentHandler(ledgerSvc)

			err := h.ValidateData(ctx, tt.txData)

			if tt.wantErr {
				require.Error(t, err)
				if tt.expectedErrMsg != "" {
					assert.Contains(t, err.Error(), tt.expectedErrMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestAssetAdjustmentHandler_USDValueCalculation tests USD value calculations
func TestAssetAdjustmentHandler_USDValueCalculation(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()

	// Helper to create big.Int from string
	mustParseBigInt := func(s string) *big.Int {
		n, ok := new(big.Int).SetString(s, 10)
		if !ok {
			t.Fatalf("failed to parse big.Int from %s", s)
		}
		return n
	}

	tests := []struct {
		name        string
		newBalance  *big.Int
		currentBal  *big.Int
		usdRate     *big.Int
		expectedUSD *big.Int
	}{
		{
			name:        "calculate USD value for 1 ETH at $2000",
			newBalance:  mustParseBigInt("1000000000000000000"), // 1 ETH in wei
			currentBal:  big.NewInt(0),
			usdRate:     mustParseBigInt("200000000000"),           // $2000 * 10^8
			expectedUSD: mustParseBigInt("2000000000000000000000"), // (10^18 wei * 2000 * 10^8) / 10^8 = 2000 * 10^18
		},
		{
			name:        "calculate USD value for 0.5 BTC at $40000",
			newBalance:  mustParseBigInt("50000000"), // 0.5 BTC in satoshis (50,000,000 satoshis)
			currentBal:  big.NewInt(0),
			usdRate:     mustParseBigInt("4000000000000"), // $40000 * 10^8
			expectedUSD: mustParseBigInt("2000000000000"), // (50000000 * 4000000000000) / 10^8 = 2000000000000
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := new(MockLedgerRepository)
			mockRepo.On("GetAccountBalance", ctx, mock.AnythingOfType("uuid.UUID"), mock.Anything).Return(&ledger.AccountBalance{
				AccountID: walletID,
				AssetID:   "ETH",
				Balance:   tt.currentBal,
			}, nil)

			ledgerSvc := ledger.NewService(mockRepo, nil)
			h := adjustment.NewAssetAdjustmentHandler(ledgerSvc)

			txData := map[string]interface{}{
				"wallet_id":   walletID.String(),
				"asset_id":    "ETH",
				"new_balance": tt.newBalance,
				"usd_rate":    tt.usdRate,
				"occurred_at": time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			}

			entries, err := h.Handle(ctx, txData)
			require.NoError(t, err)
			require.NotEmpty(t, entries)

			// Check that at least one entry has the expected USD value
			// Print actual values for debugging
			t.Logf("Expected USD value: %s", tt.expectedUSD.String())
			for i, entry := range entries {
				t.Logf("Entry %d USD value: %s", i, entry.USDValue.String())
			}

			foundExpectedValue := false
			for _, entry := range entries {
				if entry.USDValue.Cmp(tt.expectedUSD) == 0 {
					foundExpectedValue = true
					break
				}
			}
			assert.True(t, foundExpectedValue, "Expected USD value %s not found in entries", tt.expectedUSD.String())
		})
	}
}
