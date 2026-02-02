package service

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/core/ledger/domain"
	"github.com/kislikjeka/moontrack/internal/core/ledger/handler"
	"github.com/kislikjeka/moontrack/internal/core/ledger/repository"
)

// LedgerService orchestrates the ledger operations
// This is the main service for recording transactions and managing the ledger
type LedgerService struct {
	repo             repository.LedgerRepository
	handlerRegistry  *handler.Registry
	accountResolver  *AccountResolver
	validator        *TransactionValidator
	committer        *TransactionCommitter
}

// NewLedgerService creates a new ledger service
func NewLedgerService(
	repo repository.LedgerRepository,
	handlerRegistry *handler.Registry,
) *LedgerService {
	accountResolver := NewAccountResolver(repo)
	validator := NewTransactionValidator(repo)
	committer := NewTransactionCommitter(repo)

	return &LedgerService{
		repo:            repo,
		handlerRegistry: handlerRegistry,
		accountResolver: accountResolver,
		validator:       validator,
		committer:       committer,
	}
}

// RecordTransaction records a new transaction in the ledger
// This is the main entry point for creating transactions
//
// Steps:
// 1. Validate transaction type has a registered handler
// 2. Generate ledger entries using the handler
// 3. Resolve account IDs for entries (create accounts if needed)
// 4. Validate the transaction (balance check, business rules)
// 5. Commit the transaction and update balances
func (s *LedgerService) RecordTransaction(
	ctx context.Context,
	transactionType string,
	source string,
	externalID *string,
	occurredAt time.Time,
	rawData map[string]interface{},
) (*domain.Transaction, error) {
	// Step 1: Get the handler for this transaction type
	h, err := s.handlerRegistry.Get(transactionType)
	if err != nil {
		return nil, fmt.Errorf("transaction type not supported: %w", err)
	}

	// Step 2: Validate the transaction data
	if err := h.ValidateData(ctx, rawData); err != nil {
		return s.createFailedTransaction(
			transactionType,
			source,
			externalID,
			occurredAt,
			rawData,
			fmt.Sprintf("validation failed: %v", err),
		), err
	}

	// Step 3: Generate ledger entries
	entries, err := h.Handle(ctx, rawData)
	if err != nil {
		return s.createFailedTransaction(
			transactionType,
			source,
			externalID,
			occurredAt,
			rawData,
			fmt.Sprintf("failed to generate entries: %v", err),
		), err
	}

	// Step 4: Create transaction object
	tx := &domain.Transaction{
		ID:         uuid.New(),
		Type:       transactionType,
		Source:     source,
		ExternalID: externalID,
		Status:     domain.TransactionStatusCompleted,
		Version:    1,
		OccurredAt: occurredAt,
		RecordedAt: time.Now(),
		RawData:    rawData,
		Metadata:   make(map[string]interface{}),
		Entries:    entries,
	}

	// Step 5: Resolve accounts for entries
	if err := s.accountResolver.ResolveAccounts(ctx, tx); err != nil {
		errorMsg := fmt.Sprintf("failed to resolve accounts: %v", err)
		tx.Status = domain.TransactionStatusFailed
		tx.ErrorMessage = &errorMsg
		return tx, err
	}

	// Step 6: Validate the transaction
	if err := s.validator.Validate(ctx, tx); err != nil {
		errorMsg := fmt.Sprintf("validation failed: %v", err)
		tx.Status = domain.TransactionStatusFailed
		tx.ErrorMessage = &errorMsg
		return tx, err
	}

	// Step 7: Commit the transaction
	if err := s.committer.Commit(ctx, tx); err != nil {
		errorMsg := fmt.Sprintf("failed to commit: %v", err)
		tx.Status = domain.TransactionStatusFailed
		tx.ErrorMessage = &errorMsg
		return tx, err
	}

	return tx, nil
}

// GetTransaction retrieves a transaction by ID
func (s *LedgerService) GetTransaction(ctx context.Context, id uuid.UUID) (*domain.Transaction, error) {
	return s.repo.GetTransaction(ctx, id)
}

// ListTransactions lists transactions with filters
func (s *LedgerService) ListTransactions(ctx context.Context, filters repository.TransactionFilters) ([]*domain.Transaction, error) {
	return s.repo.ListTransactions(ctx, filters)
}

// GetAccountBalance retrieves the current balance for an account/asset
func (s *LedgerService) GetAccountBalance(ctx context.Context, accountID uuid.UUID, assetID string) (*domain.AccountBalance, error) {
	return s.repo.GetAccountBalance(ctx, accountID, assetID)
}

// GetAccountBalances retrieves all balances for an account
func (s *LedgerService) GetAccountBalances(ctx context.Context, accountID uuid.UUID) ([]*domain.AccountBalance, error) {
	return s.repo.GetAccountBalances(ctx, accountID)
}

// GetBalance retrieves the balance for a specific wallet and asset
// This is used by handlers that need to check balance before processing
func (s *LedgerService) GetBalance(ctx context.Context, walletID uuid.UUID, assetID string) (*big.Int, error) {
	// Build account code for wallet asset
	accountCode := fmt.Sprintf("wallet.%s.%s", walletID.String(), assetID)

	// Find the account by code
	account, err := s.repo.GetAccountByCode(ctx, accountCode)
	if err != nil {
		// Account doesn't exist means zero balance
		return big.NewInt(0), nil
	}

	// Get the balance for this account and asset
	balance, err := s.repo.GetAccountBalance(ctx, account.ID, assetID)
	if err != nil {
		// No balance found means zero
		return big.NewInt(0), nil
	}

	return balance.Balance, nil
}

// ReconcileBalance verifies that the account balance matches the ledger entries
// This is a constitution-required check per Principle V
func (s *LedgerService) ReconcileBalance(ctx context.Context, accountID uuid.UUID, assetID string) error {
	// Get current balance from account_balances table
	currentBalance, err := s.repo.GetAccountBalance(ctx, accountID, assetID)
	if err != nil {
		return fmt.Errorf("failed to get current balance: %w", err)
	}

	// Calculate balance from entries
	calculatedBalance, err := s.repo.CalculateBalanceFromEntries(ctx, accountID, assetID)
	if err != nil {
		return fmt.Errorf("failed to calculate balance from entries: %w", err)
	}

	// Compare
	if currentBalance.Balance.Cmp(calculatedBalance) != 0 {
		return fmt.Errorf(
			"balance mismatch: current=%s, calculated=%s",
			currentBalance.Balance.String(),
			calculatedBalance.String(),
		)
	}

	return nil
}

// createFailedTransaction creates a failed transaction record
func (s *LedgerService) createFailedTransaction(
	transactionType string,
	source string,
	externalID *string,
	occurredAt time.Time,
	rawData map[string]interface{},
	errorMessage string,
) *domain.Transaction {
	return &domain.Transaction{
		ID:           uuid.New(),
		Type:         transactionType,
		Source:       source,
		ExternalID:   externalID,
		Status:       domain.TransactionStatusFailed,
		Version:      1,
		OccurredAt:   occurredAt,
		RecordedAt:   time.Now(),
		RawData:      rawData,
		Metadata:     make(map[string]interface{}),
		ErrorMessage: &errorMessage,
		Entries:      nil,
	}
}
