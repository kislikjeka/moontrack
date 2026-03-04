//go:build integration

package lending_test

import (
	"context"
	"io"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/infra/postgres"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/module/lending"
	"github.com/kislikjeka/moontrack/internal/module/transfer"
	"github.com/kislikjeka/moontrack/pkg/logger"
	"github.com/kislikjeka/moontrack/pkg/money"
	"github.com/kislikjeka/moontrack/testutil/testdb"
)

var testDB *testdb.TestDB

func TestMain(m *testing.M) {
	ctx := context.Background()

	var err error
	testDB, err = testdb.NewTestDB(ctx)
	if err != nil {
		panic("failed to create test database: " + err.Error())
	}

	code := m.Run()

	testDB.Close(ctx)
	if code != 0 {
		panic("tests failed")
	}
}

func setupLendingTest(t *testing.T) (*ledger.Service, *postgres.LedgerRepository, context.Context) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := postgres.NewLedgerRepository(testDB.Pool)
	walletRepo := postgres.NewWalletRepository(testDB.Pool)
	registry := ledger.NewRegistry()

	log := logger.New("test", io.Discard)

	// Register transfer_in for seeding initial wallet balance
	registry.Register(transfer.NewTransferInHandler(walletRepo, log))

	// Register lending handlers
	registry.Register(lending.NewLendingSupplyHandler(walletRepo, log))
	registry.Register(lending.NewLendingWithdrawHandler(walletRepo, log))
	registry.Register(lending.NewLendingBorrowHandler(walletRepo, log))
	registry.Register(lending.NewLendingRepayHandler(walletRepo, log))
	registry.Register(lending.NewLendingClaimHandler(walletRepo, log))

	svc := ledger.NewService(repo, registry, log)
	return svc, repo, ctx
}

func createTestUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uuid.UUID {
	userID := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
	`, userID, "test-"+userID.String()[:8]+"@example.com", "hash")
	require.NoError(t, err)
	return userID
}

func createTestWallet(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, address string) uuid.UUID {
	walletID := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO wallets (id, user_id, name, address, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
	`, walletID, userID, "Test Wallet "+walletID.String()[:8], address)
	require.NoError(t, err)
	return walletID
}

func stringPtr(s string) *string {
	return &s
}

// =============================================================================
// Full Lending Cycle: Supply → Borrow → Repay → Withdraw → Claim
// =============================================================================

func TestIntegration_FullLendingCycle(t *testing.T) {
	svc, repo, ctx := setupLendingTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID, "0xAABBCCDDEE1234567890AABBCCDDEEFF12345678")

	chain := "ethereum"
	protocol := "Aave V3"

	// Seed wallet with 5 ETH via transfer_in
	_, err := svc.RecordTransaction(ctx, ledger.TxTypeTransferIn, "blockchain", stringPtr("seed-eth"),
		time.Now().Add(-5*time.Hour), map[string]interface{}{
			"wallet_id":        walletID.String(),
			"asset_id":         "ETH",
			"decimals":         18,
			"amount":           money.NewBigIntFromInt64(5000000000000000000).String(), // 5 ETH
			"usd_rate":         money.NewBigIntFromInt64(200000000000).String(),        // $2000
			"chain_id":         chain,
			"tx_hash":          "0xseed_eth",
			"block_number":     int64(100000),
			"from_address":     "0xfaucet",
			"contract_address": "",
			"occurred_at":      time.Now().Add(-5 * time.Hour).Format(time.RFC3339),
			"unique_id":        "seed-eth",
		})
	require.NoError(t, err)

	// Seed wallet with 2000 USDC for repay
	_, err = svc.RecordTransaction(ctx, ledger.TxTypeTransferIn, "blockchain", stringPtr("seed-usdc"),
		time.Now().Add(-5*time.Hour), map[string]interface{}{
			"wallet_id":        walletID.String(),
			"asset_id":         "USDC",
			"decimals":         6,
			"amount":           money.NewBigIntFromInt64(2000000000).String(), // 2000 USDC
			"usd_rate":         money.NewBigIntFromInt64(100000000).String(),  // $1
			"chain_id":         chain,
			"tx_hash":          "0xseed_usdc",
			"block_number":     int64(100001),
			"from_address":     "0xfaucet",
			"contract_address": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
			"occurred_at":      time.Now().Add(-5 * time.Hour).Format(time.RFC3339),
			"unique_id":        "seed-usdc",
		})
	require.NoError(t, err)

	// =========================================================================
	// Step 1: Supply 2 ETH to AAVE
	// =========================================================================
	supplyTx, err := svc.RecordTransaction(ctx, ledger.TxTypeLendingSupply, "blockchain", stringPtr("supply-eth"),
		time.Now().Add(-4*time.Hour), map[string]interface{}{
			"wallet_id":        walletID.String(),
			"asset":            "ETH",
			"amount":           money.NewBigIntFromInt64(2000000000000000000).String(), // 2 ETH
			"decimals":         18,
			"usd_price":        money.NewBigIntFromInt64(200000000000).String(), // $2000
			"chain_id":         chain,
			"protocol":         protocol,
			"tx_hash":          "0xsupply_eth",
			"contract_address": "0xaave_pool",
			"occurred_at":      time.Now().Add(-4 * time.Hour).Format(time.RFC3339),
		})
	require.NoError(t, err)
	require.NotNil(t, supplyTx)

	// Verify balanced entries
	require.Len(t, supplyTx.Entries, 2)
	require.NoError(t, supplyTx.VerifyBalance())

	// Verify entry types: collateral_increase + asset_decrease
	assert.Equal(t, ledger.EntryTypeCollateralIncrease, supplyTx.Entries[0].EntryType)
	assert.Equal(t, ledger.EntryTypeAssetDecrease, supplyTx.Entries[1].EntryType)

	// Verify wallet ETH balance: 5 - 2 = 3 ETH
	walletETHAccount, err := repo.GetAccountByCode(ctx, "wallet."+walletID.String()+"."+chain+".ETH")
	require.NoError(t, err)
	walletETHBalance, err := svc.GetAccountBalance(ctx, walletETHAccount.ID, "ETH")
	require.NoError(t, err)
	assert.Equal(t, 0, walletETHBalance.Balance.Cmp(big.NewInt(3000000000000000000)), "wallet ETH should be 3 after supply")

	// Verify collateral balance: 2 ETH
	collateralCode := "collateral." + protocol + "." + walletID.String() + "." + chain + ".ETH"
	collateralAccount, err := repo.GetAccountByCode(ctx, collateralCode)
	require.NoError(t, err)
	collateralBalance, err := svc.GetAccountBalance(ctx, collateralAccount.ID, "ETH")
	require.NoError(t, err)
	assert.Equal(t, 0, collateralBalance.Balance.Cmp(big.NewInt(2000000000000000000)), "collateral should be 2 ETH")

	// =========================================================================
	// Step 2: Borrow 1000 USDC against ETH collateral
	// =========================================================================
	borrowTx, err := svc.RecordTransaction(ctx, ledger.TxTypeLendingBorrow, "blockchain", stringPtr("borrow-usdc"),
		time.Now().Add(-3*time.Hour), map[string]interface{}{
			"wallet_id":        walletID.String(),
			"asset":            "USDC",
			"amount":           money.NewBigIntFromInt64(1000000000).String(), // 1000 USDC
			"decimals":         6,
			"usd_price":        money.NewBigIntFromInt64(100000000).String(), // $1
			"chain_id":         chain,
			"protocol":         protocol,
			"tx_hash":          "0xborrow_usdc",
			"contract_address": "0xaave_pool",
			"occurred_at":      time.Now().Add(-3 * time.Hour).Format(time.RFC3339),
		})
	require.NoError(t, err)
	require.NotNil(t, borrowTx)

	// Verify balanced entries
	require.Len(t, borrowTx.Entries, 2)
	require.NoError(t, borrowTx.VerifyBalance())

	// Verify entry types: asset_increase + liability_increase
	assert.Equal(t, ledger.EntryTypeAssetIncrease, borrowTx.Entries[0].EntryType)
	assert.Equal(t, ledger.EntryTypeLiabilityIncrease, borrowTx.Entries[1].EntryType)

	// Verify wallet USDC balance: 2000 + 1000 = 3000 USDC
	walletUSDCAccount, err := repo.GetAccountByCode(ctx, "wallet."+walletID.String()+"."+chain+".USDC")
	require.NoError(t, err)
	walletUSDCBalance, err := svc.GetAccountBalance(ctx, walletUSDCAccount.ID, "USDC")
	require.NoError(t, err)
	assert.Equal(t, 0, walletUSDCBalance.Balance.Cmp(big.NewInt(3000000000)), "wallet USDC should be 3000 after borrow")

	// Verify liability balance: 1000 USDC
	liabilityCode := "liability." + protocol + "." + walletID.String() + "." + chain + ".USDC"
	liabilityAccount, err := repo.GetAccountByCode(ctx, liabilityCode)
	require.NoError(t, err)
	liabilityBalance, err := svc.GetAccountBalance(ctx, liabilityAccount.ID, "USDC")
	require.NoError(t, err)
	assert.Equal(t, 0, liabilityBalance.Balance.Cmp(big.NewInt(1000000000)), "liability should be 1000 USDC")

	// =========================================================================
	// Step 3: Repay 500 USDC
	// =========================================================================
	repayTx, err := svc.RecordTransaction(ctx, ledger.TxTypeLendingRepay, "blockchain", stringPtr("repay-usdc"),
		time.Now().Add(-2*time.Hour), map[string]interface{}{
			"wallet_id":        walletID.String(),
			"asset":            "USDC",
			"amount":           money.NewBigIntFromInt64(500000000).String(), // 500 USDC
			"decimals":         6,
			"usd_price":        money.NewBigIntFromInt64(100000000).String(), // $1
			"chain_id":         chain,
			"protocol":         protocol,
			"tx_hash":          "0xrepay_usdc",
			"contract_address": "0xaave_pool",
			"occurred_at":      time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
		})
	require.NoError(t, err)
	require.NotNil(t, repayTx)

	// Verify balanced entries
	require.Len(t, repayTx.Entries, 2)
	require.NoError(t, repayTx.VerifyBalance())

	// Verify entry types: liability_decrease + asset_decrease
	assert.Equal(t, ledger.EntryTypeLiabilityDecrease, repayTx.Entries[0].EntryType)
	assert.Equal(t, ledger.EntryTypeAssetDecrease, repayTx.Entries[1].EntryType)

	// Verify wallet USDC balance: 3000 - 500 = 2500 USDC
	walletUSDCBalance, err = svc.GetAccountBalance(ctx, walletUSDCAccount.ID, "USDC")
	require.NoError(t, err)
	assert.Equal(t, 0, walletUSDCBalance.Balance.Cmp(big.NewInt(2500000000)), "wallet USDC should be 2500 after repay")

	// Verify liability balance: 1000 - 500 = 500 USDC
	liabilityBalance, err = svc.GetAccountBalance(ctx, liabilityAccount.ID, "USDC")
	require.NoError(t, err)
	assert.Equal(t, 0, liabilityBalance.Balance.Cmp(big.NewInt(500000000)), "liability should be 500 USDC after repay")

	// =========================================================================
	// Step 4: Withdraw 1 ETH from AAVE
	// =========================================================================
	withdrawTx, err := svc.RecordTransaction(ctx, ledger.TxTypeLendingWithdraw, "blockchain", stringPtr("withdraw-eth"),
		time.Now().Add(-1*time.Hour), map[string]interface{}{
			"wallet_id":        walletID.String(),
			"asset":            "ETH",
			"amount":           money.NewBigIntFromInt64(1000000000000000000).String(), // 1 ETH
			"decimals":         18,
			"usd_price":        money.NewBigIntFromInt64(200000000000).String(), // $2000
			"chain_id":         chain,
			"protocol":         protocol,
			"tx_hash":          "0xwithdraw_eth",
			"contract_address": "0xaave_pool",
			"occurred_at":      time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		})
	require.NoError(t, err)
	require.NotNil(t, withdrawTx)

	// Verify balanced entries
	require.Len(t, withdrawTx.Entries, 2)
	require.NoError(t, withdrawTx.VerifyBalance())

	// Verify entry types: asset_increase + collateral_decrease
	assert.Equal(t, ledger.EntryTypeAssetIncrease, withdrawTx.Entries[0].EntryType)
	assert.Equal(t, ledger.EntryTypeCollateralDecrease, withdrawTx.Entries[1].EntryType)

	// Verify wallet ETH balance: 3 + 1 = 4 ETH
	walletETHBalance, err = svc.GetAccountBalance(ctx, walletETHAccount.ID, "ETH")
	require.NoError(t, err)
	assert.Equal(t, 0, walletETHBalance.Balance.Cmp(big.NewInt(4000000000000000000)), "wallet ETH should be 4 after withdraw")

	// Verify collateral balance: 2 - 1 = 1 ETH
	collateralBalance, err = svc.GetAccountBalance(ctx, collateralAccount.ID, "ETH")
	require.NoError(t, err)
	assert.Equal(t, 0, collateralBalance.Balance.Cmp(big.NewInt(1000000000000000000)), "collateral should be 1 ETH after withdraw")

	// =========================================================================
	// Step 5: Claim rewards (AAVE token)
	// =========================================================================
	claimTx, err := svc.RecordTransaction(ctx, ledger.TxTypeLendingClaim, "blockchain", stringPtr("claim-aave"),
		time.Now().Add(-30*time.Minute), map[string]interface{}{
			"wallet_id":        walletID.String(),
			"asset":            "AAVE",
			"amount":           money.NewBigIntFromInt64(500000000000000000).String(), // 0.5 AAVE
			"decimals":         18,
			"usd_price":        money.NewBigIntFromInt64(10000000000).String(), // $100
			"chain_id":         chain,
			"protocol":         protocol,
			"tx_hash":          "0xclaim_aave",
			"contract_address": "0xaave_incentives",
			"occurred_at":      time.Now().Add(-30 * time.Minute).Format(time.RFC3339),
		})
	require.NoError(t, err)
	require.NotNil(t, claimTx)

	// Verify balanced entries
	require.Len(t, claimTx.Entries, 2)
	require.NoError(t, claimTx.VerifyBalance())

	// Verify entry types: asset_increase + income
	assert.Equal(t, ledger.EntryTypeAssetIncrease, claimTx.Entries[0].EntryType)
	assert.Equal(t, ledger.EntryTypeIncome, claimTx.Entries[1].EntryType)

	// Verify wallet AAVE balance: 0.5 AAVE
	walletAAVEAccount, err := repo.GetAccountByCode(ctx, "wallet."+walletID.String()+"."+chain+".AAVE")
	require.NoError(t, err)
	walletAAVEBalance, err := svc.GetAccountBalance(ctx, walletAAVEAccount.ID, "AAVE")
	require.NoError(t, err)
	assert.Equal(t, 0, walletAAVEBalance.Balance.Cmp(big.NewInt(500000000000000000)), "wallet AAVE should be 0.5 after claim")

	// Verify income account credited
	incomeAccount, err := repo.GetAccountByCode(ctx, "income.lending."+chain+".AAVE")
	require.NoError(t, err)
	require.NotNil(t, incomeAccount, "income account should exist")

	// =========================================================================
	// Final state summary:
	// - Wallet ETH: 4 (started 5, supplied 2, withdrew 1)
	// - Wallet USDC: 2500 (started 2000, borrowed 1000, repaid 500)
	// - Wallet AAVE: 0.5 (claimed)
	// - Collateral ETH: 1 (supplied 2, withdrew 1)
	// - Liability USDC: 500 (borrowed 1000, repaid 500)
	// - Income AAVE: credited 0.5
	// =========================================================================

	// Reconcile all accounts to verify consistency
	err = svc.ReconcileBalance(ctx, walletETHAccount.ID, "ETH")
	assert.NoError(t, err, "ETH wallet reconciliation should pass")

	err = svc.ReconcileBalance(ctx, walletUSDCAccount.ID, "USDC")
	assert.NoError(t, err, "USDC wallet reconciliation should pass")

	err = svc.ReconcileBalance(ctx, collateralAccount.ID, "ETH")
	assert.NoError(t, err, "collateral reconciliation should pass")

	err = svc.ReconcileBalance(ctx, liabilityAccount.ID, "USDC")
	assert.NoError(t, err, "liability reconciliation should pass")
}

// TestIntegration_SupplyEntries verifies supply creates exactly 2 balanced entries
func TestIntegration_SupplyEntries(t *testing.T) {
	svc, repo, ctx := setupLendingTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID, "0xAABBCCDDEE1234567890AABBCCDDEEFF12345678")

	// Seed wallet with ETH
	_, err := svc.RecordTransaction(ctx, ledger.TxTypeTransferIn, "blockchain", stringPtr("seed-supply-test"),
		time.Now().Add(-2*time.Hour), map[string]interface{}{
			"wallet_id":        walletID.String(),
			"asset_id":         "ETH",
			"decimals":         18,
			"amount":           money.NewBigIntFromInt64(10000000000000000000).String(),
			"usd_rate":         money.NewBigIntFromInt64(200000000000).String(),
			"chain_id":         "ethereum",
			"tx_hash":          "0xseed",
			"block_number":     int64(100000),
			"from_address":     "0xfaucet",
			"contract_address": "",
			"occurred_at":      time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
			"unique_id":        "seed-supply-test",
		})
	require.NoError(t, err)

	// Supply 1 ETH
	tx, err := svc.RecordTransaction(ctx, ledger.TxTypeLendingSupply, "blockchain", stringPtr("supply-test"),
		time.Now().Add(-1*time.Hour), map[string]interface{}{
			"wallet_id":        walletID.String(),
			"asset":            "ETH",
			"amount":           money.NewBigIntFromInt64(1000000000000000000).String(),
			"decimals":         18,
			"usd_price":        money.NewBigIntFromInt64(200000000000).String(),
			"chain_id":         "ethereum",
			"protocol":         "Aave V3",
			"tx_hash":          "0xsupply_test",
			"contract_address": "0xaave",
			"occurred_at":      time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		})
	require.NoError(t, err)

	assert.Len(t, tx.Entries, 2)
	assert.NoError(t, tx.VerifyBalance())

	// Wallet decreased
	walletAccount, err := repo.GetAccountByCode(ctx, "wallet."+walletID.String()+".ethereum.ETH")
	require.NoError(t, err)
	bal, err := svc.GetAccountBalance(ctx, walletAccount.ID, "ETH")
	require.NoError(t, err)
	assert.Equal(t, 0, bal.Balance.Cmp(big.NewInt(9000000000000000000)), "wallet should have 9 ETH")

	// Collateral increased
	collAccount, err := repo.GetAccountByCode(ctx, "collateral.Aave V3."+walletID.String()+".ethereum.ETH")
	require.NoError(t, err)
	collBal, err := svc.GetAccountBalance(ctx, collAccount.ID, "ETH")
	require.NoError(t, err)
	assert.Equal(t, 0, collBal.Balance.Cmp(big.NewInt(1000000000000000000)), "collateral should be 1 ETH")
}
