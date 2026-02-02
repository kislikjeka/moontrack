package service_test

import (
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/core/ledger/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// T035: Test transaction balance invariant (SUM(debit) = SUM(credit))
func TestLedgerService_TransactionBalanceInvariant(t *testing.T) {
	tests := []struct {
		name        string
		entries     []*domain.Entry
		shouldError bool
		errorMsg    string
	}{
		{
			name: "balanced transaction - simple income",
			entries: []*domain.Entry{
				{
					ID:          uuid.New(),
					DebitCredit: domain.Debit,
					Amount:      big.NewInt(1000000000), // 1 BTC in satoshis
					AssetID:     "BTC",
					USDRate:     big.NewInt(4500000000000), // $45,000
					USDValue:    big.NewInt(45000000000),
					OccurredAt:  time.Now(),
					CreatedAt:   time.Now(),
					Metadata:    map[string]interface{}{"account_code": "wallet.test.BTC", "account_type": "CRYPTO_WALLET", "wallet_id": uuid.New().String()},
				},
				{
					ID:          uuid.New(),
					DebitCredit: domain.Credit,
					Amount:      big.NewInt(1000000000), // 1 BTC in satoshis
					AssetID:     "BTC",
					USDRate:     big.NewInt(4500000000000),
					USDValue:    big.NewInt(45000000000),
					OccurredAt:  time.Now(),
					CreatedAt:   time.Now(),
					Metadata:    map[string]interface{}{"account_code": "income.BTC", "account_type": "INCOME"},
				},
			},
			shouldError: false,
		},
		{
			name: "unbalanced transaction - should fail",
			entries: []*domain.Entry{
				{
					ID:          uuid.New(),
					DebitCredit: domain.Debit,
					Amount:      big.NewInt(1000000000), // 1 BTC
					AssetID:     "BTC",
					USDRate:     big.NewInt(4500000000000),
					USDValue:    big.NewInt(45000000000),
					OccurredAt:  time.Now(),
					CreatedAt:   time.Now(),
					Metadata:    map[string]interface{}{"account_code": "wallet.test.BTC", "account_type": "CRYPTO_WALLET", "wallet_id": uuid.New().String()},
				},
				{
					ID:          uuid.New(),
					DebitCredit: domain.Credit,
					Amount:      big.NewInt(900000000), // 0.9 BTC - UNBALANCED!
					AssetID:     "BTC",
					USDRate:     big.NewInt(4500000000000),
					USDValue:    big.NewInt(40500000000),
					OccurredAt:  time.Now(),
					CreatedAt:   time.Now(),
					Metadata:    map[string]interface{}{"account_code": "income.BTC", "account_type": "INCOME"},
				},
			},
			shouldError: true,
			errorMsg:    "do not balance",
		},
		{
			name: "balanced transaction - multiple entries",
			entries: []*domain.Entry{
				// Debit wallet with BTC
				{
					ID:          uuid.New(),
					DebitCredit: domain.Debit,
					Amount:      big.NewInt(500000000), // 0.5 BTC
					AssetID:     "BTC",
					USDRate:     big.NewInt(4500000000000),
					USDValue:    big.NewInt(22500000000),
					OccurredAt:  time.Now(),
					CreatedAt:   time.Now(),
					Metadata:    map[string]interface{}{"account_code": "wallet.test.BTC", "account_type": "CRYPTO_WALLET", "wallet_id": uuid.New().String()},
				},
				// Debit wallet with ETH
				{
					ID:          uuid.New(),
					DebitCredit: domain.Debit,
					Amount:      big.NewInt(2000000000000000000), // 2 ETH in wei
					AssetID:     "ETH",
					USDRate:     big.NewInt(250000000000),
					USDValue:    big.NewInt(500000000),
					OccurredAt:  time.Now(),
					CreatedAt:   time.Now(),
					Metadata:    map[string]interface{}{"account_code": "wallet.test.ETH", "account_type": "CRYPTO_WALLET", "wallet_id": uuid.New().String()},
				},
				// Credit BTC income
				{
					ID:          uuid.New(),
					DebitCredit: domain.Credit,
					Amount:      big.NewInt(500000000), // 0.5 BTC
					AssetID:     "BTC",
					USDRate:     big.NewInt(4500000000000),
					USDValue:    big.NewInt(22500000000),
					OccurredAt:  time.Now(),
					CreatedAt:   time.Now(),
					Metadata:    map[string]interface{}{"account_code": "income.BTC", "account_type": "INCOME"},
				},
				// Credit ETH income
				{
					ID:          uuid.New(),
					DebitCredit: domain.Credit,
					Amount:      big.NewInt(2000000000000000000), // 2 ETH in wei
					AssetID:     "ETH",
					USDRate:     big.NewInt(250000000000),
					USDValue:    big.NewInt(500000000),
					OccurredAt:  time.Now(),
					CreatedAt:   time.Now(),
					Metadata:    map[string]interface{}{"account_code": "income.ETH", "account_type": "INCOME"},
				},
			},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &domain.Transaction{
				ID:         uuid.New(),
				Type:       "test_transaction",
				Source:     "test",
				Status:     domain.TransactionStatusCompleted,
				Version:    1,
				OccurredAt: time.Now().Add(-1 * time.Hour),
				RecordedAt: time.Now(),
				RawData:    make(map[string]interface{}),
				Metadata:   make(map[string]interface{}),
				Entries:    tt.entries,
			}

			// Set transaction ID on all entries
			for _, entry := range tx.Entries {
				entry.TransactionID = tx.ID
			}

			// Test the balance verification
			err := tx.VerifyBalance()

			if tt.shouldError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)

				// Additional verification: manually calculate balance
				debitSum := big.NewInt(0)
				creditSum := big.NewInt(0)

				for _, entry := range tx.Entries {
					if entry.IsDebit() {
						debitSum.Add(debitSum, entry.Amount)
					} else {
						creditSum.Add(creditSum, entry.Amount)
					}
				}

				assert.Equal(t, 0, debitSum.Cmp(creditSum), "Debit and credit sums must be equal")
			}
		})
	}
}

// T036: Test account balance reconciliation (balance = SUM(entries))
func TestLedgerService_AccountBalanceReconciliation(t *testing.T) {
	t.Run("balance matches sum of entries", func(t *testing.T) {
		// Simulate a series of entries for an account
		accountID := uuid.New()
		assetID := "BTC"

		entries := []*domain.Entry{
			// Initial deposit: +1 BTC
			{
				DebitCredit: domain.Debit,
				AccountID:   accountID,
				AssetID:     assetID,
				Amount:      big.NewInt(100000000), // 1 BTC in satoshis
			},
			// Another deposit: +0.5 BTC
			{
				DebitCredit: domain.Debit,
				AccountID:   accountID,
				AssetID:     assetID,
				Amount:      big.NewInt(50000000), // 0.5 BTC
			},
			// Withdrawal: -0.3 BTC
			{
				DebitCredit: domain.Credit,
				AccountID:   accountID,
				AssetID:     assetID,
				Amount:      big.NewInt(30000000), // 0.3 BTC
			},
		}

		// Calculate expected balance
		expectedBalance := big.NewInt(0)
		for _, entry := range entries {
			expectedBalance.Add(expectedBalance, entry.SignedAmount())
		}

		// Expected: 1 + 0.5 - 0.3 = 1.2 BTC = 120000000 satoshis
		assert.Equal(t, "120000000", expectedBalance.String())

		// Create account balance
		balance := &domain.AccountBalance{
			AccountID:   accountID,
			AssetID:     assetID,
			Balance:     expectedBalance,
			USDValue:    big.NewInt(0),
			LastUpdated: time.Now(),
		}

		// Validate
		err := balance.Validate()
		require.NoError(t, err)
		assert.True(t, balance.IsPositive())
		assert.False(t, balance.IsZero())
	})

	t.Run("zero balance is valid", func(t *testing.T) {
		balance := &domain.AccountBalance{
			AccountID:   uuid.New(),
			AssetID:     "ETH",
			Balance:     big.NewInt(0),
			USDValue:    big.NewInt(0),
			LastUpdated: time.Now(),
		}

		err := balance.Validate()
		require.NoError(t, err)
		assert.True(t, balance.IsZero())
		assert.False(t, balance.IsPositive())
	})

	t.Run("negative balance should fail validation", func(t *testing.T) {
		balance := &domain.AccountBalance{
			AccountID:   uuid.New(),
			AssetID:     "BTC",
			Balance:     big.NewInt(-100000000), // Negative balance
			USDValue:    big.NewInt(0),
			LastUpdated: time.Now(),
		}

		err := balance.Validate()
		require.Error(t, err)
		assert.Equal(t, domain.ErrNegativeBalance, err)
	})
}
