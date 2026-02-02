package handler_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	ledgerdomain "github.com/kislikjeka/moontrack/internal/core/ledger/domain"
	"github.com/kislikjeka/moontrack/internal/modules/manual_transaction/domain"
	"github.com/kislikjeka/moontrack/internal/modules/manual_transaction/handler"
	walletdomain "github.com/kislikjeka/moontrack/internal/modules/wallet/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPriceService implements PriceService for testing
type mockPriceService struct {
	currentPrice    *big.Int
	historicalPrice *big.Int
	returnError     bool
}

func (m *mockPriceService) GetCurrentPrice(ctx context.Context, assetID string) (*big.Int, error) {
	if m.returnError {
		return nil, domain.ErrPriceNotAvailable
	}
	if m.currentPrice != nil {
		return m.currentPrice, nil
	}
	return big.NewInt(4567890000000), nil // Default: $45,678.90 * 10^8
}

func (m *mockPriceService) GetHistoricalPrice(ctx context.Context, assetID string, date time.Time) (*big.Int, error) {
	if m.returnError {
		return nil, domain.ErrPriceNotAvailable
	}
	if m.historicalPrice != nil {
		return m.historicalPrice, nil
	}
	return big.NewInt(4500000000000), nil // Default: $45,000 * 10^8
}

// mockWalletRepository implements WalletRepository for testing
type mockWalletRepository struct {
	wallets map[uuid.UUID]*walletdomain.Wallet
}

func newMockWalletRepository() *mockWalletRepository {
	return &mockWalletRepository{
		wallets: make(map[uuid.UUID]*walletdomain.Wallet),
	}
}

func (m *mockWalletRepository) GetByID(ctx context.Context, walletID uuid.UUID) (*walletdomain.Wallet, error) {
	if wallet, found := m.wallets[walletID]; found {
		return wallet, nil
	}
	return nil, nil
}

func (m *mockWalletRepository) addWallet(wallet *walletdomain.Wallet) {
	m.wallets[wallet.ID] = wallet
}

func TestManualIncomeHandler_LedgerEntriesBalance(t *testing.T) {
	ctx := context.Background()
	priceService := &mockPriceService{}
	walletRepo := newMockWalletRepository()

	// Create test wallet
	walletID := uuid.New()
	userID := uuid.New()
	walletRepo.addWallet(&walletdomain.Wallet{
		ID:      walletID,
		UserID:  userID,
		Name:    "Test Wallet",
		ChainID: "ethereum",
	})

	h := handler.NewManualIncomeHandler(priceService, walletRepo)

	t.Run("Entries_Balance_Correctly", func(t *testing.T) {
		txn := &domain.ManualIncomeTransaction{
			WalletID:   walletID,
			AssetID:    "ethereum",
			Amount:     big.NewInt(1000000000000000000), // 1 ETH in wei
			OccurredAt: time.Now(),
			Notes:      "Test income transaction",
		}

		entries, err := h.GenerateEntries(ctx, txn)
		require.NoError(t, err)
		assert.Len(t, entries, 2, "Should generate exactly 2 entries")

		// Verify entries balance: SUM(debit) = SUM(credit)
		debitSum := big.NewInt(0)
		creditSum := big.NewInt(0)

		for _, entry := range entries {
			if entry.DebitCredit == ledgerdomain.Debit {
				debitSum.Add(debitSum, entry.Amount)
			} else {
				creditSum.Add(creditSum, entry.Amount)
			}
		}

		assert.Equal(t, 0, debitSum.Cmp(creditSum), "Ledger entries must balance: debit sum = credit sum")
	})

	t.Run("Entry_Types_Correct", func(t *testing.T) {
		txn := &domain.ManualIncomeTransaction{
			WalletID:   walletID,
			AssetID:    "bitcoin",
			Amount:     big.NewInt(100000000), // 1 BTC in satoshi
			OccurredAt: time.Now(),
		}

		entries, err := h.GenerateEntries(ctx, txn)
		require.NoError(t, err)

		// Entry 0: DEBIT wallet (asset increases)
		assert.Equal(t, ledgerdomain.Debit, entries[0].DebitCredit)
		assert.Equal(t, ledgerdomain.EntryTypeAssetIncrease, entries[0].EntryType)

		// Entry 1: CREDIT income (income source)
		assert.Equal(t, ledgerdomain.Credit, entries[1].DebitCredit)
		assert.Equal(t, ledgerdomain.EntryTypeIncome, entries[1].EntryType)
	})

	t.Run("Manual_Price_Override_Stored", func(t *testing.T) {
		manualPrice := big.NewInt(5000000000000) // $50,000 * 10^8
		txn := &domain.ManualIncomeTransaction{
			WalletID:    walletID,
			AssetID:     "bitcoin",
			Amount:      big.NewInt(100000000),
			USDRate:     manualPrice,
			PriceSource: "manual",
			OccurredAt:  time.Now(),
		}

		entries, err := h.GenerateEntries(ctx, txn)
		require.NoError(t, err)

		// Verify all entries use the manual price
		for _, entry := range entries {
			assert.Equal(t, manualPrice.String(), entry.USDRate.String(), "Should use manual price override")
			assert.Equal(t, "manual", entry.Metadata["price_source"], "Should record price source as manual")
		}
	})

	t.Run("API_Price_Fetched_When_No_Manual_Price", func(t *testing.T) {
		apiPrice := big.NewInt(4600000000000) // $46,000 * 10^8
		priceService.currentPrice = apiPrice

		txn := &domain.ManualIncomeTransaction{
			WalletID:   walletID,
			AssetID:    "ethereum",
			Amount:     big.NewInt(1000000000000000000),
			OccurredAt: time.Now(),
		}

		entries, err := h.GenerateEntries(ctx, txn)
		require.NoError(t, err)

		// Verify all entries use the API price
		for _, entry := range entries {
			assert.Equal(t, apiPrice.String(), entry.USDRate.String(), "Should use API-fetched price")
			assert.Equal(t, "coingecko", entry.Metadata["price_source"], "Should record price source as coingecko")
		}
	})

	t.Run("USD_Value_Calculated_Correctly", func(t *testing.T) {
		amount := big.NewInt(2000000000000000000) // 2 ETH in wei
		usdRate := big.NewInt(300000000000)       // $3,000 * 10^8

		txn := &domain.ManualIncomeTransaction{
			WalletID:   walletID,
			AssetID:    "ethereum",
			Amount:     amount,
			USDRate:    usdRate,
			OccurredAt: time.Now(),
		}

		entries, err := h.GenerateEntries(ctx, txn)
		require.NoError(t, err)

		// Calculate expected USD value: (amount * usd_rate) / 10^8
		// Note: amount is in wei (18 decimals), result will be large
		calculatedValue := new(big.Int).Mul(amount, usdRate)
		calculatedValue.Div(calculatedValue, big.NewInt(100000000))

		for _, entry := range entries {
			assert.Equal(t, calculatedValue.String(), entry.USDValue.String(), "All entries should have correct USD value")
		}
	})
}
