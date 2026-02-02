package integration

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/core/ledger/domain"
	"github.com/kislikjeka/moontrack/internal/core/ledger/handler"
	ledgerPostgres "github.com/kislikjeka/moontrack/internal/core/ledger/postgres"
	"github.com/kislikjeka/moontrack/internal/core/ledger/service"
	userPostgres "github.com/kislikjeka/moontrack/internal/core/user/repository/postgres"
	userService "github.com/kislikjeka/moontrack/internal/core/user/service"
	adjustmentDomain "github.com/kislikjeka/moontrack/internal/modules/asset_adjustment/domain"
	adjustmentHandler "github.com/kislikjeka/moontrack/internal/modules/asset_adjustment/handler"
	walletDomain "github.com/kislikjeka/moontrack/internal/modules/wallet/domain"
	walletPostgres "github.com/kislikjeka/moontrack/internal/modules/wallet/repository/postgres"
	walletService "github.com/kislikjeka/moontrack/internal/modules/wallet/service"
	"github.com/kislikjeka/moontrack/internal/shared/database"
)

// setupIntegrationTest creates all necessary services for integration testing
func setupIntegrationTest(t *testing.T) (
	context.Context,
	*userService.UserService,
	*walletService.WalletService,
	*service.LedgerService,
	*handler.HandlerRegistry,
) {
	t.Helper()

	ctx := context.Background()

	// Connect to test database
	dbCfg := database.Config{
		URL: "postgres://postgres:postgres@localhost:5432/moontrack_test?sslmode=disable",
	}
	db, err := database.NewPool(ctx, dbCfg)
	require.NoError(t, err, "failed to connect to test database")

	// Clean up database before test
	_, err = db.Exec(ctx, `
		TRUNCATE users, wallets, accounts, transactions, entries, account_balances CASCADE
	`)
	require.NoError(t, err, "failed to clean test database")

	// Initialize user services
	userRepo := userPostgres.NewUserRepository(db.Pool)
	userSvc := userService.NewUserService(userRepo)

	// Initialize wallet services
	walletRepo := walletPostgres.NewWalletRepository(db.Pool)
	walletSvc := walletService.NewWalletService(walletRepo)

	// Initialize ledger services
	ledgerRepo := ledgerPostgres.NewLedgerRepository(db.Pool)
	accountResolver := service.NewAccountResolver(ledgerRepo)
	validator := service.NewTransactionValidator()
	committer := service.NewTransactionCommitter(ledgerRepo)
	ledgerSvc := service.NewLedgerService(ledgerRepo, accountResolver, validator, committer)

	// Initialize handler registry
	registry := handler.NewHandlerRegistry()

	// Register asset adjustment handler
	assetAdjHandler := adjustmentHandler.NewAssetAdjustmentHandler(ledgerSvc)
	err = registry.RegisterHandler("asset_adjustment", assetAdjHandler)
	require.NoError(t, err, "failed to register asset adjustment handler")

	return ctx, userSvc, walletSvc, ledgerSvc, registry
}

// TestWalletCreation_AssetAdjustment_UpdatesBalance tests the complete flow (T081)
func TestWalletCreation_AssetAdjustment_UpdatesBalance(t *testing.T) {
	ctx, userSvc, walletSvc, ledgerSvc, registry := setupIntegrationTest(t)

	// Step 1: Create a user
	user, err := userSvc.Register(ctx, "test@example.com", "SecureP@ssw0rd123")
	require.NoError(t, err, "failed to create user")
	assert.NotEmpty(t, user.ID, "user ID should not be empty")

	// Step 2: Create a wallet
	wallet := &walletDomain.Wallet{
		ID:      uuid.New(),
		UserID:  user.ID,
		Name:    "My Bitcoin Wallet",
		ChainID: "bitcoin",
	}
	createdWallet, err := walletSvc.Create(ctx, wallet)
	require.NoError(t, err, "failed to create wallet")
	assert.Equal(t, wallet.Name, createdWallet.Name)
	assert.Equal(t, wallet.ChainID, createdWallet.ChainID)

	// Step 3: Create ledger account for the wallet
	// In a real scenario, this would be done automatically by the system
	// For testing, we create it manually
	accountCode := "wallet." + createdWallet.ID.String() + ".BTC"
	account := &domain.Account{
		ID:       uuid.New(),
		Code:     accountCode,
		Type:     domain.AccountTypeCryptoWallet,
		AssetID:  "BTC",
		WalletID: &createdWallet.ID,
		ChainID:  createdWallet.ChainID,
	}
	createdAccount, err := ledgerSvc.CreateAccount(ctx, account)
	require.NoError(t, err, "failed to create ledger account")
	assert.Equal(t, accountCode, createdAccount.Code)

	// Step 4: Create an asset adjustment transaction to set initial balance
	initialBalance := big.NewInt(100000000) // 1 BTC in satoshis (1 BTC = 100,000,000 satoshis)
	usdRate := big.NewInt(4500000000000)    // $45,000 per BTC, scaled by 10^8

	adjustmentTx := &adjustmentDomain.AssetAdjustmentTransaction{
		WalletID:   createdAccount.ID, // Use account ID as wallet ID for simplicity
		AssetID:    "BTC",
		NewBalance: initialBalance,
		USDRate:    usdRate,
		OccurredAt: time.Now(),
		Notes:      "Initial balance setup",
	}

	// Convert to map for handler
	txData := map[string]interface{}{
		"wallet_id":   adjustmentTx.WalletID.String(),
		"asset_id":    adjustmentTx.AssetID,
		"new_balance": initialBalance.String(),
		"usd_rate":    usdRate.String(),
		"occurred_at": adjustmentTx.OccurredAt,
		"notes":       adjustmentTx.Notes,
	}

	// Process transaction through ledger
	tx := &domain.Transaction{
		ID:         uuid.New(),
		Type:       "asset_adjustment",
		Source:     "manual",
		Status:     domain.StatusCompleted,
		OccurredAt: time.Now(),
		RawData:    txData,
	}

	committedTx, err := ledgerSvc.RecordTransaction(ctx, tx, registry)
	require.NoError(t, err, "failed to record asset adjustment transaction")
	assert.Equal(t, domain.StatusCompleted, committedTx.Status)
	assert.NotEmpty(t, committedTx.ID)

	// Step 5: Verify balance was updated correctly
	balance, err := ledgerSvc.GetAccountBalance(ctx, createdAccount.ID, "BTC")
	require.NoError(t, err, "failed to get account balance")
	assert.Equal(t, 0, initialBalance.Cmp(balance.Balance), "balance should equal initial balance: expected %s, got %s", initialBalance.String(), balance.Balance.String())

	// Step 6: Verify ledger entries balance (double-entry accounting)
	entries, err := ledgerSvc.GetEntriesByTransaction(ctx, committedTx.ID)
	require.NoError(t, err, "failed to get transaction entries")
	assert.Len(t, entries, 2, "asset adjustment should create 2 entries")

	// Calculate debit and credit sums
	debitSum := big.NewInt(0)
	creditSum := big.NewInt(0)
	for _, entry := range entries {
		if entry.DebitCredit == domain.Debit {
			debitSum.Add(debitSum, entry.Amount)
		} else {
			creditSum.Add(creditSum, entry.Amount)
		}
	}

	// Verify balance invariant: SUM(debit) = SUM(credit)
	assert.Equal(t, 0, debitSum.Cmp(creditSum), "ledger must balance: debit sum (%s) must equal credit sum (%s)", debitSum.String(), creditSum.String())

	// Step 7: Make another adjustment to increase balance
	newBalance := big.NewInt(150000000) // 1.5 BTC in satoshis
	adjustmentTx2 := &adjustmentDomain.AssetAdjustmentTransaction{
		WalletID:   createdAccount.ID,
		AssetID:    "BTC",
		NewBalance: newBalance,
		USDRate:    usdRate,
		OccurredAt: time.Now(),
		Notes:      "Balance increase",
	}

	txData2 := map[string]interface{}{
		"wallet_id":   adjustmentTx2.WalletID.String(),
		"asset_id":    adjustmentTx2.AssetID,
		"new_balance": newBalance.String(),
		"usd_rate":    usdRate.String(),
		"occurred_at": adjustmentTx2.OccurredAt,
		"notes":       adjustmentTx2.Notes,
	}

	tx2 := &domain.Transaction{
		ID:         uuid.New(),
		Type:       "asset_adjustment",
		Source:     "manual",
		Status:     domain.StatusCompleted,
		OccurredAt: time.Now(),
		RawData:    txData2,
	}

	committedTx2, err := ledgerSvc.RecordTransaction(ctx, tx2, registry)
	require.NoError(t, err, "failed to record second asset adjustment transaction")
	assert.Equal(t, domain.StatusCompleted, committedTx2.Status)

	// Step 8: Verify final balance
	finalBalance, err := ledgerSvc.GetAccountBalance(ctx, createdAccount.ID, "BTC")
	require.NoError(t, err, "failed to get final account balance")
	assert.Equal(t, 0, newBalance.Cmp(finalBalance.Balance), "balance should equal new balance: expected %s, got %s", newBalance.String(), finalBalance.Balance.String())

	// Step 9: Verify second transaction also balances
	entries2, err := ledgerSvc.GetEntriesByTransaction(ctx, committedTx2.ID)
	require.NoError(t, err, "failed to get second transaction entries")
	assert.Len(t, entries2, 2, "second asset adjustment should create 2 entries")

	debitSum2 := big.NewInt(0)
	creditSum2 := big.NewInt(0)
	for _, entry := range entries2 {
		if entry.DebitCredit == domain.Debit {
			debitSum2.Add(debitSum2, entry.Amount)
		} else {
			creditSum2.Add(creditSum2, entry.Amount)
		}
	}

	assert.Equal(t, 0, debitSum2.Cmp(creditSum2), "second ledger transaction must balance: debit sum (%s) must equal credit sum (%s)", debitSum2.String(), creditSum2.String())
}

// TestAssetAdjustment_DecreaseBalance tests decreasing wallet balance (T081)
func TestAssetAdjustment_DecreaseBalance(t *testing.T) {
	ctx, userSvc, walletSvc, ledgerSvc, registry := setupIntegrationTest(t)

	// Create user
	user, err := userSvc.Register(ctx, "test2@example.com", "SecureP@ssw0rd123")
	require.NoError(t, err)

	// Create wallet
	wallet := &walletDomain.Wallet{
		ID:      uuid.New(),
		UserID:  user.ID,
		Name:    "My Ethereum Wallet",
		ChainID: "ethereum",
	}
	createdWallet, err := walletSvc.Create(ctx, wallet)
	require.NoError(t, err)

	// Create ledger account
	accountCode := "wallet." + createdWallet.ID.String() + ".ETH"
	account := &domain.Account{
		ID:       uuid.New(),
		Code:     accountCode,
		Type:     domain.AccountTypeCryptoWallet,
		AssetID:  "ETH",
		WalletID: &createdWallet.ID,
		ChainID:  createdWallet.ChainID,
	}
	createdAccount, err := ledgerSvc.CreateAccount(ctx, account)
	require.NoError(t, err)

	// Set initial balance to 10 ETH
	initialBalance := new(big.Int)
	initialBalance.SetString("10000000000000000000", 10) // 10 ETH in wei
	usdRate := big.NewInt(300000000000)                  // $3,000 per ETH, scaled by 10^8

	txData1 := map[string]interface{}{
		"wallet_id":   createdAccount.ID.String(),
		"asset_id":    "ETH",
		"new_balance": initialBalance.String(),
		"usd_rate":    usdRate.String(),
		"occurred_at": time.Now(),
		"notes":       "Initial balance",
	}

	tx1 := &domain.Transaction{
		ID:         uuid.New(),
		Type:       "asset_adjustment",
		Source:     "manual",
		Status:     domain.StatusCompleted,
		OccurredAt: time.Now(),
		RawData:    txData1,
	}

	_, err = ledgerSvc.RecordTransaction(ctx, tx1, registry)
	require.NoError(t, err)

	// Verify initial balance
	balance1, err := ledgerSvc.GetAccountBalance(ctx, createdAccount.ID, "ETH")
	require.NoError(t, err)
	assert.Equal(t, 0, initialBalance.Cmp(balance1.Balance), "initial balance should be set")

	// Decrease balance to 5 ETH
	newBalance := new(big.Int)
	newBalance.SetString("5000000000000000000", 10) // 5 ETH in wei

	txData2 := map[string]interface{}{
		"wallet_id":   createdAccount.ID.String(),
		"asset_id":    "ETH",
		"new_balance": newBalance.String(),
		"usd_rate":    usdRate.String(),
		"occurred_at": time.Now(),
		"notes":       "Decrease balance",
	}

	tx2 := &domain.Transaction{
		ID:         uuid.New(),
		Type:       "asset_adjustment",
		Source:     "manual",
		Status:     domain.StatusCompleted,
		OccurredAt: time.Now(),
		RawData:    txData2,
	}

	committedTx2, err := ledgerSvc.RecordTransaction(ctx, tx2, registry)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusCompleted, committedTx2.Status)

	// Verify decreased balance
	balance2, err := ledgerSvc.GetAccountBalance(ctx, createdAccount.ID, "ETH")
	require.NoError(t, err)
	assert.Equal(t, 0, newBalance.Cmp(balance2.Balance), "balance should be decreased to new balance")

	// Verify entries balance
	entries, err := ledgerSvc.GetEntriesByTransaction(ctx, committedTx2.ID)
	require.NoError(t, err)
	assert.Len(t, entries, 2)

	debitSum := big.NewInt(0)
	creditSum := big.NewInt(0)
	for _, entry := range entries {
		if entry.DebitCredit == domain.Debit {
			debitSum.Add(debitSum, entry.Amount)
		} else {
			creditSum.Add(creditSum, entry.Amount)
		}
	}

	assert.Equal(t, 0, debitSum.Cmp(creditSum), "ledger must balance")
}
