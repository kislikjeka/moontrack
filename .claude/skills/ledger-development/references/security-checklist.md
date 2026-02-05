# Ledger Security Checklist

This checklist must be verified before submitting any code that modifies ledger, transactions, or module code.

## Pre-Submission Security Checklist

### 1. Row-Level Locking for Concurrent Access

- [ ] Uses `SELECT ... FOR UPDATE` when checking balances before withdrawal
- [ ] Repository method `GetAccountBalanceForUpdate()` used in concurrent scenarios
- [ ] Database transaction wraps balance check AND update

```go
// CORRECT: Lock the row before checking balance
func (r *Repository) GetAccountBalanceForUpdate(ctx context.Context, accountID uuid.UUID, assetID string) (*AccountBalance, error) {
    query := `
        SELECT account_id, asset_id, balance, usd_value, last_updated
        FROM account_balances
        WHERE account_id = $1 AND asset_id = $2
        FOR UPDATE  -- CRITICAL: Locks the row
    `
    // ...
}

// WRONG: Race condition possible
func (r *Repository) GetAccountBalance(ctx context.Context, accountID uuid.UUID, assetID string) (*AccountBalance, error) {
    query := `
        SELECT account_id, asset_id, balance, usd_value, last_updated
        FROM account_balances
        WHERE account_id = $1 AND asset_id = $2
        -- NO FOR UPDATE = Race condition!
    `
    // ...
}
```

### 2. Authorization Checks

- [ ] Handler's `ValidateData()` checks wallet ownership
- [ ] Uses `middleware.GetUserIDFromContext(ctx)` to get current user
- [ ] Compares `wallet.UserID` with context user ID
- [ ] Returns `ErrUnauthorized` for cross-user access
- [ ] System operations (no user in context) are explicitly allowed if needed

```go
func (h *Handler) ValidateData(ctx context.Context, data map[string]interface{}) error {
    walletID := parseWalletID(data)

    // Get wallet from repository
    wallet, err := h.walletRepo.GetByID(ctx, walletID)
    if err != nil {
        return err
    }

    // Check authorization
    userID, ok := middleware.GetUserIDFromContext(ctx)
    if ok && userID != wallet.UserID {
        return ErrUnauthorized // CRITICAL: Prevent cross-user access
    }

    return nil
}
```

### 3. Input Validation

- [ ] **Negative amounts rejected**
  ```go
  if amount.Sign() < 0 {
      return ErrNegativeAmount
  }
  ```

- [ ] **Future dates rejected**
  ```go
  if occurredAt.After(time.Now()) {
      return ErrOccurredAtInFuture
  }
  ```

- [ ] **Empty asset IDs rejected**
  ```go
  if assetID == "" {
      return ErrInvalidAssetID
  }
  ```

- [ ] **Invalid UUIDs handled**
  ```go
  walletID, err := uuid.Parse(walletIDStr)
  if err != nil {
      return fmt.Errorf("invalid wallet ID: %w", err)
  }
  ```

- [ ] **Nil amounts handled**
  ```go
  if amount == nil {
      return ErrNegativeAmount // Treat nil as invalid
  }
  ```

- [ ] **Negative USD rates rejected**
  ```go
  if usdRate == nil || usdRate.Sign() < 0 {
      return ErrNegativeUSDRate
  }
  ```

### 4. Double-Entry Balance Invariant

- [ ] All handlers return balanced entries
- [ ] Transaction.VerifyBalance() called before commit
- [ ] Test explicitly verifies SUM(debits) = SUM(credits)

```go
// In handler test:
debitSum := new(big.Int)
creditSum := new(big.Int)

for _, entry := range entries {
    if entry.DebitCredit == ledger.Debit {
        debitSum.Add(debitSum, entry.Amount)
    } else {
        creditSum.Add(creditSum, entry.Amount)
    }
}

assert.Equal(t, 0, debitSum.Cmp(creditSum),
    "Entries must balance: debits=%s credits=%s",
    debitSum.String(), creditSum.String())
```

### 5. Negative Balance Prevention

- [ ] Outcome/withdrawal handlers check sufficient balance
- [ ] Balance check uses row-level locking (see #1)
- [ ] Returns clear error when balance insufficient

```go
func (h *OutcomeHandler) ValidateData(ctx context.Context, data map[string]interface{}) error {
    // ... parse data ...

    // Check balance with row lock
    balance, err := h.balanceGetter.GetBalanceForUpdate(ctx, walletID, assetID)
    if err != nil {
        return err
    }

    if balance.Cmp(amount) < 0 {
        return ErrInsufficientBalance
    }

    return nil
}
```

### 6. Entry Immutability

- [ ] No UpdateEntry method exists
- [ ] Entries are INSERT-only
- [ ] Corrections use new adjustment transactions

```go
// CORRECT: Create adjustment transaction
func (s *Service) AdjustBalance(ctx context.Context, ...) error {
    // Create new transaction with correction entries
    return s.RecordTransaction(ctx, TxTypeAssetAdjustment, ...)
}

// WRONG: Never modify entries
func (r *Repository) UpdateEntry(...) error {
    // THIS SHOULD NOT EXIST!
}
```

### 7. SQL Injection Prevention

- [ ] All queries use parameterized statements ($1, $2, etc.)
- [ ] No string concatenation in SQL
- [ ] Account codes validated before use

```go
// CORRECT: Parameterized query
query := `
    SELECT * FROM accounts WHERE code = $1
`
row := pool.QueryRow(ctx, query, accountCode)

// WRONG: SQL injection vulnerability
query := fmt.Sprintf(`
    SELECT * FROM accounts WHERE code = '%s'
`, accountCode)  // NEVER DO THIS!
```

### 8. Transaction Atomicity

- [ ] All operations wrapped in database transaction
- [ ] Proper rollback on any error
- [ ] No partial state left on failure

```go
func (s *Service) RecordTransaction(ctx context.Context, ...) (*Transaction, error) {
    // Start transaction
    txCtx, err := s.repo.BeginTx(ctx)
    if err != nil {
        return nil, err
    }

    defer func() {
        if err != nil {
            s.repo.RollbackTx(txCtx) // Always rollback on error
        }
    }()

    // ... perform operations ...

    if err = s.repo.CommitTx(txCtx); err != nil {
        return nil, err
    }

    return tx, nil
}
```

## Test Requirements Checklist

When modifying ledger code, ensure these tests exist:

### Unit Tests
- [ ] Handler.ValidateData() rejects invalid inputs
- [ ] Handler.Handle() generates balanced entries
- [ ] Entry.Validate() rejects invalid entries

### Integration Tests
- [ ] Full transaction recording with real DB
- [ ] Balance updates correctly
- [ ] Reconciliation passes

### Security Tests
- [ ] Negative amount rejection
- [ ] Future date rejection
- [ ] Cross-user access rejection
- [ ] Unbalanced entries rejection
- [ ] Invalid UUID handling

### Concurrent Tests
- [ ] Double-spend prevention
- [ ] Race condition handling
- [ ] Correct balance after concurrent operations

### Atomicity Tests
- [ ] Rollback on validation failure
- [ ] No partial entries on failure
- [ ] Reconciliation detects inconsistency

## Common Security Vulnerabilities

### 1. Race Condition in Balance Check

**Vulnerable:**
```go
balance := getBalance(walletID)       // Read balance
if balance >= amount {
    deduct(walletID, amount)          // Update balance
}
// Time gap between read and write allows race condition
```

**Fixed:**
```go
txCtx := beginTransaction()
balance := getBalanceForUpdate(txCtx, walletID)  // Lock row
if balance >= amount {
    deduct(txCtx, walletID, amount)
}
commit(txCtx)
```

### 2. Missing Ownership Check

**Vulnerable:**
```go
func (h *Handler) Handle(ctx context.Context, data map[string]interface{}) error {
    walletID := data["wallet_id"]
    // Directly operates on any wallet ID without checking ownership
    return h.deposit(ctx, walletID, amount)
}
```

**Fixed:**
```go
func (h *Handler) ValidateData(ctx context.Context, data map[string]interface{}) error {
    walletID := data["wallet_id"]
    wallet := h.walletRepo.GetByID(ctx, walletID)

    userID := middleware.GetUserIDFromContext(ctx)
    if userID != wallet.UserID {
        return ErrUnauthorized
    }
    return nil
}
```

### 3. Float64 Precision Loss

**Vulnerable:**
```go
amount := 0.1 + 0.2  // = 0.30000000000000004 in float64
```

**Fixed:**
```go
amount := new(big.Int)
amount.SetString("100000000000000000", 10)  // 0.1 ETH in wei
```

## Security Review Checklist Summary

| Category | Check | Required |
|----------|-------|----------|
| Concurrency | Row-level locking (FOR UPDATE) | Yes |
| Authorization | Wallet ownership verification | Yes |
| Validation | Negative amount rejection | Yes |
| Validation | Future date rejection | Yes |
| Validation | Empty/nil field handling | Yes |
| Double-Entry | Balance verification (debit=credit) | Yes |
| Balance | Negative balance prevention | Yes |
| Immutability | Entries never updated | Yes |
| SQL | Parameterized queries only | Yes |
| Atomicity | Full transaction wrapping | Yes |
