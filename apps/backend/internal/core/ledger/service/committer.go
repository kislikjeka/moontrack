package service

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/core/ledger/domain"
	"github.com/kislikjeka/moontrack/internal/core/ledger/repository"
)

// TransactionCommitter commits transactions to the ledger and updates balances
type TransactionCommitter struct {
	repo repository.LedgerRepository
}

// NewTransactionCommitter creates a new transaction committer
func NewTransactionCommitter(repo repository.LedgerRepository) *TransactionCommitter {
	return &TransactionCommitter{
		repo: repo,
	}
}

// Commit commits a transaction to the ledger
// This performs the following steps:
// 1. Save the transaction and its entries to the database
// 2. Update account balances based on the entries
//
// This should be called within a database transaction to ensure atomicity
func (c *TransactionCommitter) Commit(ctx context.Context, tx *domain.Transaction) error {
	// Step 1: Create the transaction and entries in the database
	if err := c.repo.CreateTransaction(ctx, tx); err != nil {
		return fmt.Errorf("failed to create transaction: %w", err)
	}

	// Step 2: Update account balances
	if err := c.updateBalances(ctx, tx); err != nil {
		return fmt.Errorf("failed to update balances: %w", err)
	}

	return nil
}

// updateBalances updates account balances based on transaction entries
func (c *TransactionCommitter) updateBalances(ctx context.Context, tx *domain.Transaction) error {
	// Group entries by account and asset
	balanceChanges := make(map[string]*balanceChange)

	for _, entry := range tx.Entries {
		key := fmt.Sprintf("%s:%s", entry.AccountID.String(), entry.AssetID)

		if _, exists := balanceChanges[key]; !exists {
			balanceChanges[key] = &balanceChange{
				accountID: entry.AccountID,
				assetID:   entry.AssetID,
				change:    big.NewInt(0),
			}
		}

		// Add signed amount (debits positive, credits negative)
		balanceChanges[key].change.Add(balanceChanges[key].change, entry.SignedAmount())
	}

	// Apply balance changes
	for _, bc := range balanceChanges {
		if err := c.applyBalanceChange(ctx, bc); err != nil {
			return fmt.Errorf("failed to apply balance change for %s:%s: %w", bc.accountID, bc.assetID, err)
		}
	}

	return nil
}

// applyBalanceChange applies a balance change to an account
func (c *TransactionCommitter) applyBalanceChange(ctx context.Context, bc *balanceChange) error {
	// Get current balance
	currentBalance, err := c.repo.GetAccountBalance(ctx, bc.accountID, bc.assetID)
	if err != nil {
		return fmt.Errorf("failed to get current balance: %w", err)
	}

	// Calculate new balance
	newBalance := new(big.Int).Add(currentBalance.Balance, bc.change)

	// Sanity check: balance should not be negative
	if newBalance.Sign() < 0 {
		return fmt.Errorf("balance would be negative: current=%s, change=%s", currentBalance.Balance.String(), bc.change.String())
	}

	// Update balance
	updatedBalance := &domain.AccountBalance{
		AccountID:   bc.accountID,
		AssetID:     bc.assetID,
		Balance:     newBalance,
		USDValue:    currentBalance.USDValue, // Keep USD value for now (will be updated by price refresh job)
		LastUpdated: time.Now(),
	}

	if err := c.repo.UpsertAccountBalance(ctx, updatedBalance); err != nil {
		return fmt.Errorf("failed to upsert balance: %w", err)
	}

	return nil
}

// balanceChange represents a pending balance change
type balanceChange struct {
	accountID uuid.UUID
	assetID   string
	change    *big.Int
}
