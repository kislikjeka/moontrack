# Ledger Testing Patterns

This document provides detailed testing patterns for MoonTrack's ledger system.

## Test Infrastructure Setup

### TestMain with TestContainers

All integration tests must use real PostgreSQL via TestContainers:

```go
//go:build integration

package ledger_test

import (
    "context"
    "os"
    "testing"

    "github.com/kislikjeka/moontrack/testutil"
)

var testDB *testutil.TestDB

func TestMain(m *testing.M) {
    ctx := context.Background()

    // Setup test database with TestContainers
    var err error
    testDB, err = testutil.NewTestDB(ctx)
    if err != nil {
        panic(err)
    }

    // Run tests
    code := m.Run()

    // Cleanup
    testDB.Close(ctx)
    os.Exit(code)
}
```

### Test Helpers

Create reusable helpers for common operations:

```go
// createTestUser creates a test user and returns their ID
func createTestUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uuid.UUID {
    t.Helper()
    userID := uuid.New()
    _, err := pool.Exec(ctx, `
        INSERT INTO users (id, email, password_hash, created_at)
        VALUES ($1, $2, $3, NOW())
    `, userID, "test@example.com", "hashedpassword")
    require.NoError(t, err)
    return userID
}

// createTestWallet creates a test wallet for a user
func createTestWallet(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) uuid.UUID {
    t.Helper()
    walletID := uuid.New()
    _, err := pool.Exec(ctx, `
        INSERT INTO wallets (id, user_id, name, created_at)
        VALUES ($1, $2, $3, NOW())
    `, walletID, userID, "Test Wallet")
    require.NoError(t, err)
    return walletID
}

// resetTestDB resets the test database to a clean state
func resetTestDB(t *testing.T, ctx context.Context) {
    t.Helper()
    require.NoError(t, testDB.Reset(ctx))
}
```

## Integration Test Patterns

### Basic Transaction Recording

```go
func TestLedgerService_RecordTransaction_BasicIncome(t *testing.T) {
    ctx := context.Background()
    require.NoError(t, testDB.Reset(ctx))

    // Setup
    repo := postgres.NewLedgerRepository(testDB.Pool)
    registry := ledger.NewRegistry()
    handler := manual.NewManualIncomeHandler(priceService, walletRepo)
    require.NoError(t, registry.Register(handler))
    svc := ledger.NewService(repo, registry)

    userID := createTestUser(t, ctx, testDB.Pool)
    walletID := createTestWallet(t, ctx, testDB.Pool, userID)

    // Execute
    tx, err := svc.RecordTransaction(
        ctx,
        ledger.TxTypeManualIncome,
        "manual",
        nil,
        time.Now().Add(-time.Hour),
        map[string]interface{}{
            "wallet_id": walletID.String(),
            "asset_id":  "BTC",
            "amount":    "100000000", // 1 BTC in satoshis
        },
    )

    // Verify
    require.NoError(t, err)
    assert.NotNil(t, tx)
    assert.Equal(t, ledger.TransactionStatusCompleted, tx.Status)
    assert.Len(t, tx.Entries, 2)

    // Verify balance
    accountCode := "wallet." + walletID.String() + ".BTC"
    account, err := repo.GetAccountByCode(ctx, accountCode)
    require.NoError(t, err)

    balance, err := svc.GetAccountBalance(ctx, account.ID, "BTC")
    require.NoError(t, err)
    assert.Equal(t, "100000000", balance.Balance.String())
}
```

### Double-Entry Balance Verification

Every test that creates entries MUST verify balance:

```go
func TestHandler_GenerateEntries_MustBalance(t *testing.T) {
    ctx := context.Background()
    handler := NewMyHandler(deps...)

    entries, err := handler.Handle(ctx, map[string]interface{}{
        "wallet_id": walletID.String(),
        "asset_id":  "ETH",
        "amount":    "1000000000000000000", // 1 ETH in wei
        "usd_rate":  "200000000000",        // $2000 * 10^8
    })

    require.NoError(t, err)

    // CRITICAL: Verify double-entry balance
    debitSum := new(big.Int)
    creditSum := new(big.Int)

    for _, entry := range entries {
        require.NotNil(t, entry.Amount, "Entry amount must not be nil")
        require.GreaterOrEqual(t, entry.Amount.Sign(), 0, "Entry amount must be non-negative")

        if entry.DebitCredit == ledger.Debit {
            debitSum.Add(debitSum, entry.Amount)
        } else {
            creditSum.Add(creditSum, entry.Amount)
        }
    }

    assert.Equal(t, 0, debitSum.Cmp(creditSum),
        "Ledger entries must balance: SUM(debits)=%s must equal SUM(credits)=%s",
        debitSum.String(), creditSum.String())
}
```

## Security Test Patterns

### Input Validation Tests

```go
func TestLedgerService_Security_InputValidation(t *testing.T) {
    tests := []struct {
        name        string
        setupEntry  func() *ledger.Entry
        expectError string
    }{
        {
            name: "negative amount rejected",
            setupEntry: func() *ledger.Entry {
                return &ledger.Entry{
                    Amount:  big.NewInt(-100),
                    AssetID: "BTC",
                    // ...
                }
            },
            expectError: "negative",
        },
        {
            name: "nil amount rejected",
            setupEntry: func() *ledger.Entry {
                return &ledger.Entry{
                    Amount:  nil,
                    AssetID: "BTC",
                }
            },
            expectError: "negative", // nil treated as invalid
        },
        {
            name: "empty asset ID rejected",
            setupEntry: func() *ledger.Entry {
                return &ledger.Entry{
                    Amount:  big.NewInt(100),
                    AssetID: "",
                }
            },
            expectError: "asset",
        },
        {
            name: "negative USD rate rejected",
            setupEntry: func() *ledger.Entry {
                return &ledger.Entry{
                    Amount:  big.NewInt(100),
                    AssetID: "BTC",
                    USDRate: big.NewInt(-5000000000000),
                }
            },
            expectError: "negative",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            entry := tt.setupEntry()
            err := entry.Validate()
            require.Error(t, err)
            assert.Contains(t, err.Error(), tt.expectError)
        })
    }
}
```

### Future Date Rejection

```go
func TestLedgerService_RecordTransaction_FutureDateRejected(t *testing.T) {
    ctx := context.Background()
    require.NoError(t, testDB.Reset(ctx))

    repo := postgres.NewLedgerRepository(testDB.Pool)
    registry := ledger.NewRegistry()
    // ... setup handler

    svc := ledger.NewService(repo, registry)

    futureTime := time.Now().Add(24 * time.Hour)

    tx, err := svc.RecordTransaction(
        ctx,
        ledger.TxTypeManualIncome,
        "manual",
        nil,
        futureTime, // FUTURE DATE
        map[string]interface{}{
            "wallet_id": walletID.String(),
            "asset_id":  "BTC",
            "amount":    "100",
        },
    )

    assert.Error(t, err)
    assert.Contains(t, err.Error(), "future")
    if tx != nil {
        assert.Equal(t, ledger.TransactionStatusFailed, tx.Status)
    }
}
```

### Unbalanced Entries Detection

```go
func TestLedgerService_RecordTransaction_UnbalancedEntriesRejected(t *testing.T) {
    ctx := context.Background()

    // Create handler that returns unbalanced entries
    handler := &testUnbalancedHandler{
        BaseHandler: ledger.NewBaseHandler(ledger.TxTypeManualIncome),
    }

    registry := ledger.NewRegistry()
    require.NoError(t, registry.Register(handler))

    svc := ledger.NewService(repo, registry)

    tx, err := svc.RecordTransaction(ctx, ledger.TxTypeManualIncome, ...)

    assert.Error(t, err)
    assert.Contains(t, err.Error(), "balance")
}

type testUnbalancedHandler struct {
    ledger.BaseHandler
}

func (h *testUnbalancedHandler) Handle(ctx context.Context, data map[string]interface{}) ([]*ledger.Entry, error) {
    return []*ledger.Entry{
        {Amount: big.NewInt(100), DebitCredit: ledger.Debit},
        {Amount: big.NewInt(50), DebitCredit: ledger.Credit}, // NOT EQUAL!
    }, nil
}
```

## Authorization Test Patterns

### Wallet Ownership Verification

```go
func TestManualIncomeHandler_ValidateData_ChecksOwnership(t *testing.T) {
    walletID := uuid.New()
    ownerID := uuid.New()
    attackerID := uuid.New()

    mockWalletRepo := &mockWalletRepository{
        wallet: &wallet.Wallet{
            ID:     walletID,
            UserID: ownerID, // Owned by ownerID
            Name:   "Owner's Wallet",
        },
    }

    handler := manual.NewManualIncomeHandler(priceService, mockWalletRepo)

    tests := []struct {
        name      string
        userID    uuid.UUID
        expectErr bool
    }{
        {
            name:      "owner can access",
            userID:    ownerID,
            expectErr: false,
        },
        {
            name:      "attacker cannot access",
            userID:    attackerID,
            expectErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            ctx := context.WithValue(context.Background(), middleware.UserIDKey, tt.userID)

            err := handler.ValidateData(ctx, map[string]interface{}{
                "wallet_id": walletID.String(),
                "asset_id":  "BTC",
                "amount":    "100",
            })

            if tt.expectErr {
                assert.ErrorIs(t, err, manual.ErrUnauthorized)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}
```

### System Operations Without User Context

```go
func TestHandler_NoUserInContext_AllowsSystemOperation(t *testing.T) {
    // System/internal operations may not have user context
    ctx := context.Background() // NO user ID

    handler := manual.NewManualIncomeHandler(priceService, walletRepo)

    err := handler.ValidateData(ctx, map[string]interface{}{
        "wallet_id": walletID.String(),
        "asset_id":  "BTC",
        "amount":    "100",
    })

    // Should succeed for system operations
    assert.NoError(t, err)
}
```

## Concurrent Test Patterns

### Double-Spend Prevention

```go
func TestLedgerService_ConcurrentWithdrawals_NoDoubleSpend(t *testing.T) {
    ctx := context.Background()
    require.NoError(t, testDB.Reset(ctx))

    repo := postgres.NewLedgerRepository(testDB.Pool)

    // Setup: Deposit initial balance
    incomeSvc := setupIncomeService(repo)
    _, err := incomeSvc.RecordTransaction(ctx, ledger.TxTypeManualIncome, ...)
    require.NoError(t, err)

    // Setup outcome service
    outcomeSvc := setupOutcomeService(repo)

    // Initial balance: 100, withdrawal: 50 each
    // Expected: max 2 successful withdrawals
    numGoroutines := 10
    withdrawAmount := "50"

    var wg sync.WaitGroup
    var successCount int32
    var failCount int32

    for i := 0; i < numGoroutines; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()

            _, err := outcomeSvc.RecordTransaction(
                ctx,
                ledger.TxTypeManualOutcome,
                "manual",
                nil,
                time.Now().Add(-time.Hour),
                map[string]interface{}{
                    "wallet_id": walletID.String(),
                    "asset_id":  "BTC",
                    "amount":    withdrawAmount,
                },
            )

            if err != nil {
                atomic.AddInt32(&failCount, 1)
            } else {
                atomic.AddInt32(&successCount, 1)
            }
        }()
    }

    wg.Wait()

    t.Logf("Results: %d succeeded, %d failed", successCount, failCount)

    // At most 2 should succeed (100 / 50 = 2)
    assert.LessOrEqual(t, int(successCount), 2)

    // Verify final balance is non-negative
    balance, err := svc.GetAccountBalance(ctx, account.ID, "BTC")
    require.NoError(t, err)
    assert.GreaterOrEqual(t, balance.Balance.Sign(), 0)
}
```

### Concurrent Deposits

```go
func TestLedgerService_ConcurrentDeposits_CorrectTotal(t *testing.T) {
    ctx := context.Background()
    require.NoError(t, testDB.Reset(ctx))

    // First create account with initial deposit
    _, err := svc.RecordTransaction(ctx, ledger.TxTypeManualIncome, ...)
    require.NoError(t, err)

    // Run concurrent deposits
    numGoroutines := 10
    depositAmount := "10"

    var wg sync.WaitGroup
    var successCount int32

    for i := 0; i < numGoroutines; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()

            _, err := svc.RecordTransaction(
                ctx,
                ledger.TxTypeManualIncome,
                "manual",
                nil,
                time.Now().Add(-time.Hour),
                map[string]interface{}{
                    "wallet_id": walletID.String(),
                    "asset_id":  "BTC",
                    "amount":    depositAmount,
                },
            )

            if err == nil {
                atomic.AddInt32(&successCount, 1)
            }
        }()
    }

    wg.Wait()

    // All should succeed
    assert.Equal(t, int32(numGoroutines), successCount)

    // Verify total: initial + (10 * 10) = 100
    balance, err := svc.GetAccountBalance(ctx, account.ID, "BTC")
    require.NoError(t, err)

    expectedBalance := big.NewInt(int64(numGoroutines) * 10)
    assert.Equal(t, 0, balance.Balance.Cmp(expectedBalance))
}
```

## Atomicity Test Patterns

### Rollback on Failure

```go
func TestLedgerService_ValidationError_NoEntriesInDB(t *testing.T) {
    ctx := context.Background()
    require.NoError(t, testDB.Reset(ctx))

    // Count entries before
    var countBefore int
    err := testDB.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM entries").Scan(&countBefore)
    require.NoError(t, err)

    // Record transaction that will fail
    handler := &testFailingHandler{...}
    svc := ledger.NewService(repo, registry)

    _, err = svc.RecordTransaction(ctx, ...)

    assert.Error(t, err)

    // Count entries after - should be unchanged
    var countAfter int
    err = testDB.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM entries").Scan(&countAfter)
    require.NoError(t, err)

    assert.Equal(t, countBefore, countAfter,
        "No entries should be created when transaction fails")
}
```

### Reconciliation Test

```go
func TestLedgerService_ReconcileBalance_DetectsInconsistency(t *testing.T) {
    ctx := context.Background()
    require.NoError(t, testDB.Reset(ctx))

    // Record valid transaction
    _, err := svc.RecordTransaction(ctx, ...)
    require.NoError(t, err)

    // Manually corrupt balance (simulate partial failure)
    _, err = testDB.Pool.Exec(ctx, `
        UPDATE account_balances SET balance = '999'
        WHERE account_id = $1 AND asset_id = $2
    `, account.ID, "BTC")
    require.NoError(t, err)

    // Reconciliation should detect mismatch
    err = svc.ReconcileBalance(ctx, account.ID, "BTC")
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "mismatch")
}
```

## Precision Test Patterns

### Large Number Handling

```go
func TestLedgerService_Precision_LargeAmounts(t *testing.T) {
    tests := []struct {
        name   string
        amount string
    }{
        {"max uint256", "115792089237316195423570985008687907853269984665640564039457584007913129639935"},
        {"10^78 - 1", "999999999999999999999999999999999999999999999999999999999999999999999999999999"},
        {"1 billion ETH in wei", "1000000000000000000000000000"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            amount, ok := new(big.Int).SetString(tt.amount, 10)
            require.True(t, ok)

            _, err := svc.RecordTransaction(
                ctx,
                ledger.TxTypeManualIncome,
                "manual",
                nil,
                time.Now().Add(-time.Hour),
                map[string]interface{}{
                    "wallet_id": walletID.String(),
                    "asset_id":  "ETH",
                    "amount":    tt.amount,
                },
            )
            require.NoError(t, err)

            // Verify precision preserved
            balance, err := svc.GetAccountBalance(ctx, account.ID, "ETH")
            require.NoError(t, err)
            assert.Equal(t, 0, balance.Balance.Cmp(amount),
                "Precision lost: expected %s, got %s", amount.String(), balance.Balance.String())
        })
    }
}
```

## Mock Patterns

### Minimal Mock for Unit Tests

```go
type mockWalletRepository struct {
    wallet *wallet.Wallet
    err    error
}

func (m *mockWalletRepository) GetByID(ctx context.Context, walletID uuid.UUID) (*wallet.Wallet, error) {
    if m.err != nil {
        return nil, m.err
    }
    return m.wallet, nil
}

type mockPriceService struct {
    price *big.Int
    err   error
}

func (m *mockPriceService) GetCurrentPriceByCoinGeckoID(ctx context.Context, id string) (*big.Int, error) {
    if m.err != nil {
        return nil, m.err
    }
    return m.price, nil
}

func (m *mockPriceService) GetHistoricalPriceByCoinGeckoID(ctx context.Context, id string, date time.Time) (*big.Int, error) {
    if m.err != nil {
        return nil, m.err
    }
    return m.price, nil
}
```

### Full Mock for Complex Scenarios

```go
type MockLedgerRepository struct {
    mock.Mock
}

func (m *MockLedgerRepository) GetAccountBalance(ctx context.Context, accountID uuid.UUID, assetID string) (*ledger.AccountBalance, error) {
    args := m.Called(ctx, accountID, assetID)
    if args.Get(0) == nil {
        return nil, args.Error(1)
    }
    return args.Get(0).(*ledger.AccountBalance), args.Error(1)
}

// Usage:
mockRepo := new(MockLedgerRepository)
mockRepo.On("GetAccountBalance", ctx, mock.AnythingOfType("uuid.UUID"), "BTC").
    Return(&ledger.AccountBalance{Balance: big.NewInt(1000)}, nil)
```

## Test Organization

```
internal/ledger/
├── service_test.go                # Unit tests
├── service_integration_test.go    # Integration tests with real DB
├── service_security_test.go       # Security-focused tests
├── service_concurrent_test.go     # Concurrency tests
├── service_atomicity_test.go      # Atomicity and rollback tests
└── testmain_test.go               # TestMain with TestContainers

internal/module/manual/
├── handler_income_test.go         # Income handler unit tests
├── handler_outcome_test.go        # Outcome handler unit tests
├── handler_auth_test.go           # Authorization tests
├── handler_validation_test.go     # Input validation tests
└── handler_integration_test.go    # Integration tests
```
