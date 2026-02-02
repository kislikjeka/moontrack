package service

import (
	"context"
	"fmt"
	"math/big"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/core/ledger/domain"
	"github.com/kislikjeka/moontrack/internal/core/ledger/repository"
)

// TransactionValidator validates transactions before committing them
// This ensures all constitution-mandated invariants are maintained
type TransactionValidator struct {
	repo repository.LedgerRepository
}

// NewTransactionValidator creates a new transaction validator
func NewTransactionValidator(repo repository.LedgerRepository) *TransactionValidator {
	return &TransactionValidator{
		repo: repo,
	}
}

// Validate validates a transaction before it is committed
// This is a critical step that ensures ledger integrity
//
// Validations performed:
// 1. Transaction balance invariant: SUM(debit) = SUM(credit)
// 2. All entries are valid
// 3. Account balances won't go negative
// 4. Temporal constraints (occurred_at <= recorded_at)
func (v *TransactionValidator) Validate(ctx context.Context, tx *domain.Transaction) error {
	// Validate the transaction itself
	if err := tx.Validate(); err != nil {
		return fmt.Errorf("transaction validation failed: %w", err)
	}

	// Validate all entries
	for i, entry := range tx.Entries {
		if err := entry.Validate(); err != nil {
			return fmt.Errorf("entry %d validation failed: %w", i, err)
		}
	}

	// Constitution Principle V: Verify transaction balances
	if err := v.validateBalance(tx); err != nil {
		return fmt.Errorf("balance validation failed: %w", err)
	}

	// Validate that account balances won't go negative
	if err := v.validateAccountBalances(ctx, tx); err != nil {
		return fmt.Errorf("account balance validation failed: %w", err)
	}

	return nil
}

// validateBalance verifies the transaction balance invariant
// Per constitution Principle V: SUM(debit) = SUM(credit)
func (v *TransactionValidator) validateBalance(tx *domain.Transaction) error {
	if len(tx.Entries) == 0 {
		return fmt.Errorf("transaction has no entries")
	}

	debitSum := big.NewInt(0)
	creditSum := big.NewInt(0)

	for _, entry := range tx.Entries {
		if entry.IsDebit() {
			debitSum.Add(debitSum, entry.Amount)
		} else {
			creditSum.Add(creditSum, entry.Amount)
		}
	}

	if debitSum.Cmp(creditSum) != 0 {
		return fmt.Errorf(
			"transaction not balanced: debit=%s, credit=%s",
			debitSum.String(),
			creditSum.String(),
		)
	}

	return nil
}

// validateAccountBalances ensures no account will have a negative balance
// This is critical for crypto wallet accounts (can't have negative crypto)
func (v *TransactionValidator) validateAccountBalances(ctx context.Context, tx *domain.Transaction) error {
	// Group entries by account and asset
	balanceChanges := make(map[string]*big.Int)

	for _, entry := range tx.Entries {
		key := fmt.Sprintf("%s:%s", entry.AccountID.String(), entry.AssetID)

		if _, exists := balanceChanges[key]; !exists {
			balanceChanges[key] = big.NewInt(0)
		}

		// Add signed amount (debits positive, credits negative)
		balanceChanges[key].Add(balanceChanges[key], entry.SignedAmount())
	}

	// Check each account balance change
	for key, change := range balanceChanges {
		// Parse account ID and asset ID from key
		var accountID, assetID string
		fmt.Sscanf(key, "%36s:%s", &accountID, &assetID)

		// Get current balance
		accountUUID, err := parseUUID(accountID)
		if err != nil {
			return fmt.Errorf("invalid account ID: %w", err)
		}

		currentBalance, err := v.repo.GetAccountBalance(ctx, accountUUID, assetID)
		if err != nil {
			return fmt.Errorf("failed to get account balance: %w", err)
		}

		// Calculate new balance
		newBalance := new(big.Int).Add(currentBalance.Balance, change)

		// Check if balance would go negative
		if newBalance.Sign() < 0 {
			return fmt.Errorf(
				"account %s would have negative balance for %s: current=%s, change=%s, new=%s",
				accountID,
				assetID,
				currentBalance.Balance.String(),
				change.String(),
				newBalance.String(),
			)
		}
	}

	return nil
}

// parseUUID is a helper to parse UUID from string
func parseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}
