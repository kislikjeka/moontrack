---
name: ledger-development
description: This skill should be used when the user asks to "create a new module", "add transaction handler", "implement ledger feature", "write ledger tests", "add concurrent tests", "test financial precision", "check authorization", "fix double-spending", "add security tests", "test atomicity", "work on ledger", "create new transaction type", or works on any code in internal/ledger/, internal/module/, or transaction-related code. Provides comprehensive guidance on ledger development, testing patterns, and security requirements for MoonTrack's double-entry accounting system.
---

# Ledger Development Skill

This skill provides comprehensive guidance for developing and testing MoonTrack's double-entry accounting ledger system.

## Overview

MoonTrack uses a **double-entry accounting system** where every financial transaction creates balanced ledger entries. The core principle is:

```
SUM(debits) = SUM(credits)  // NON-NEGOTIABLE invariant
```

### Key Components

| Component | Location | Purpose |
|-----------|----------|---------|
| Ledger Core | `internal/ledger/` | Transaction, Entry, Account models; Service |
| Handler Registry | `internal/ledger/handler.go` | Transaction type handlers |
| Modules | `internal/module/` | Transaction types (manual, adjustment) |
| Repository | `internal/infra/postgres/ledger_repo.go` | Database operations |

### Architecture Flow

```
HTTP Request → Handler → LedgerService.RecordTransaction()
                              ↓
                    Registry.Get(txType)
                              ↓
                    Handler.ValidateData()
                              ↓
                    Handler.Handle() → []*Entry
                              ↓
                    Validator.Validate(entries)
                              ↓
                    Committer.Commit(tx, entries)
                              ↓
                    AccountResolver → Balance Update
```

## Testing Requirements

When modifying ledger or module code, you MUST include these test categories:

### 1. Integration Tests (REQUIRED)

All ledger tests must use real PostgreSQL via TestContainers:

```go
//go:build integration

package ledger_test

func TestMain(m *testing.M) {
    // Setup testDB with TestContainers
    testDB = testutil.SetupTestDB()
    code := m.Run()
    testDB.Close()
    os.Exit(code)
}
```

### 2. Double-Entry Balance Tests (REQUIRED)

Every handler test MUST verify entry balance:

```go
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
```

### 3. Authorization Tests (REQUIRED for handlers)

Test that users can only operate on their own wallets:

```go
func TestHandler_CrossUserWallet_ReturnsUnauthorized(t *testing.T) {
    userA := uuid.New() // Wallet owner
    userB := uuid.New() // Attacker

    // Create wallet owned by userA
    wallet := &wallet.Wallet{ID: walletID, UserID: userA}

    // userB tries to operate on userA's wallet
    ctx := context.WithValue(context.Background(), middleware.UserIDKey, userB)

    err := handler.ValidateData(ctx, data)
    assert.ErrorIs(t, err, manual.ErrUnauthorized)
}
```

### 4. Security Tests (REQUIRED)

Test validation of malicious inputs:

- **Negative amounts** - Must be rejected
- **Future dates** - Must be rejected
- **Invalid UUIDs** - Must be handled gracefully
- **Empty asset IDs** - Must be rejected
- **Unbalanced entries** - Must be rejected

### 5. Concurrent Tests (REQUIRED for balance-affecting operations)

Test row-level locking prevents double-spending:

```go
func TestConcurrentWithdrawals_NoDoubleSpend(t *testing.T) {
    // Deposit 100
    // Run 10 concurrent withdrawals of 50 each
    // Only 2 should succeed (100/50 = 2)

    var wg sync.WaitGroup
    var successCount int32

    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            _, err := svc.RecordTransaction(ctx, ...)
            if err == nil {
                atomic.AddInt32(&successCount, 1)
            }
        }()
    }
    wg.Wait()

    assert.LessOrEqual(t, int(successCount), 2)
}
```

### 6. Reconciliation Tests (REQUIRED)

Verify balance consistency after operations:

```go
// After transactions
err = svc.ReconcileBalance(ctx, account.ID, "BTC")
assert.NoError(t, err, "Reconciliation should pass")
```

## Security Checklist

Before submitting code that touches ledger:

- [ ] Uses `GetAccountBalanceForUpdate()` for concurrent balance checks (row-level locking)
- [ ] Checks wallet ownership via `middleware.GetUserIDFromContext(ctx)`
- [ ] Rejects negative amounts (`Amount.Sign() < 0`)
- [ ] Rejects future dates (`OccurredAt.After(time.Now())`)
- [ ] Rejects unbalanced entries (`debitSum != creditSum`)
- [ ] Validates all required fields (wallet_id, asset_id, amount)
- [ ] Uses parameterized queries (no SQL injection)
- [ ] Entries are IMMUTABLE - never update, only create

## Quick Reference

### Transaction Types

| Type | Constant | Description |
|------|----------|-------------|
| Manual Income | `TxTypeManualIncome` | User deposits |
| Manual Outcome | `TxTypeManualOutcome` | User withdrawals |
| Asset Adjustment | `TxTypeAssetAdjustment` | Balance corrections |

### Entry Types

| Entry Type | Direction | Use Case |
|------------|-----------|----------|
| `EntryTypeAssetIncrease` | Debit | Asset account increases |
| `EntryTypeAssetDecrease` | Credit | Asset account decreases |
| `EntryTypeIncome` | Credit | Income recorded |
| `EntryTypeExpense` | Debit | Expense recorded |
| `EntryTypeGasFee` | Debit | Gas fees |

### Account Code Format

```
wallet.{wallet_id}.{asset_id}  - Crypto wallet accounts
income.{asset_id}               - Income accounts
expense.{asset_id}              - Expense accounts
gas_fee.{chain_id}              - Gas fee accounts
```

### Financial Precision

- **Database**: `NUMERIC(78,0)` - stores raw units (wei, satoshi)
- **Go**: `*big.Int` - never use float64 for amounts
- **USD Rate**: Scaled by 10^8 (e.g., $50,000 = 5000000000000)

## Running Tests

```bash
# All ledger integration tests
just backend-test-integration

# Specific test file
go test -v -tags=integration ./internal/ledger/... -run TestLedgerService_Concurrent

# With race detector
go test -v -tags=integration -race ./internal/ledger/...
```

## Creating a New Transaction Module

1. Create module directory: `internal/module/{name}/`
2. Define models in `model.go`
3. Implement handler in `handler.go`:
   - `Type() TransactionType`
   - `ValidateData(ctx, data) error`
   - `Handle(ctx, data) ([]*Entry, error)`
4. Register in `cmd/api/main.go`
5. Write tests following patterns in this skill

## References

- [Testing Patterns](./references/testing-patterns.md) - Detailed test examples
- [Security Checklist](./references/security-checklist.md) - Full security requirements
- [Precision Guide](./references/precision-guide.md) - big.Int and NUMERIC handling
- [Handler Test Template](./examples/handler_test_template.go)
- [Concurrent Test Template](./examples/concurrent_test_template.go)
