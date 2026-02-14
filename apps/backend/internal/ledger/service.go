package ledger

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
)

// Service orchestrates the ledger operations
// This is the main service for recording transactions and managing the ledger
type Service struct {
	repo            Repository
	handlerRegistry *Registry
	accountResolver *accountResolver
	validator       *transactionValidator
	committer       *transactionCommitter
}

// NewService creates a new ledger service
func NewService(repo Repository, handlerRegistry *Registry) *Service {
	return &Service{
		repo:            repo,
		handlerRegistry: handlerRegistry,
		accountResolver: newAccountResolver(repo),
		validator:       newTransactionValidator(repo),
		committer:       newTransactionCommitter(repo),
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
func (s *Service) RecordTransaction(
	ctx context.Context,
	transactionType TransactionType,
	source string,
	externalID *string,
	occurredAt time.Time,
	rawData map[string]interface{},
) (*Transaction, error) {
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
	tx := &Transaction{
		ID:         uuid.New(),
		Type:       transactionType,
		Source:     source,
		ExternalID: externalID,
		Status:     TransactionStatusCompleted,
		Version:    1,
		OccurredAt: occurredAt,
		RecordedAt: time.Now(),
		RawData:    rawData,
		Metadata:   make(map[string]interface{}),
		Entries:    entries,
	}

	// Set transaction ID on all entries
	for _, entry := range tx.Entries {
		entry.TransactionID = tx.ID
	}

	// Step 5: Resolve accounts for entries
	if err := s.accountResolver.resolveAccounts(ctx, tx); err != nil {
		errorMsg := fmt.Sprintf("failed to resolve accounts: %v", err)
		tx.Status = TransactionStatusFailed
		tx.ErrorMessage = &errorMsg
		return tx, err
	}

	// Step 6: Validate the transaction
	if err := s.validator.validate(ctx, tx); err != nil {
		errorMsg := fmt.Sprintf("validation failed: %v", err)
		tx.Status = TransactionStatusFailed
		tx.ErrorMessage = &errorMsg
		return tx, err
	}

	// Step 7: Commit the transaction
	if err := s.committer.commit(ctx, tx); err != nil {
		errorMsg := fmt.Sprintf("failed to commit: %v", err)
		tx.Status = TransactionStatusFailed
		tx.ErrorMessage = &errorMsg
		return tx, err
	}

	return tx, nil
}

// GetTransaction retrieves a transaction by ID
func (s *Service) GetTransaction(ctx context.Context, id uuid.UUID) (*Transaction, error) {
	return s.repo.GetTransaction(ctx, id)
}

// ListTransactions lists transactions with filters
func (s *Service) ListTransactions(ctx context.Context, filters TransactionFilters) ([]*Transaction, error) {
	return s.repo.ListTransactions(ctx, filters)
}

// GetAccountBalance retrieves the current balance for an account/asset
func (s *Service) GetAccountBalance(ctx context.Context, accountID uuid.UUID, assetID string) (*AccountBalance, error) {
	return s.repo.GetAccountBalance(ctx, accountID, assetID)
}

// GetAccountBalances retrieves all balances for an account
func (s *Service) GetAccountBalances(ctx context.Context, accountID uuid.UUID) ([]*AccountBalance, error) {
	return s.repo.GetAccountBalances(ctx, accountID)
}

// GetBalance retrieves the balance for a specific wallet and asset
// This is used by handlers that need to check balance before processing
func (s *Service) GetBalance(ctx context.Context, walletID uuid.UUID, assetID string) (*big.Int, error) {
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
func (s *Service) ReconcileBalance(ctx context.Context, accountID uuid.UUID, assetID string) error {
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
func (s *Service) createFailedTransaction(
	transactionType TransactionType,
	source string,
	externalID *string,
	occurredAt time.Time,
	rawData map[string]interface{},
	errorMessage string,
) *Transaction {
	return &Transaction{
		ID:           uuid.New(),
		Type:         transactionType,
		Source:       source,
		ExternalID:   externalID,
		Status:       TransactionStatusFailed,
		Version:      1,
		OccurredAt:   occurredAt,
		RecordedAt:   time.Now(),
		RawData:      rawData,
		Metadata:     make(map[string]interface{}),
		ErrorMessage: &errorMessage,
		Entries:      nil,
	}
}

// accountResolver resolves account references in ledger entries
type accountResolver struct {
	repo Repository
}

func newAccountResolver(repo Repository) *accountResolver {
	return &accountResolver{repo: repo}
}

func (r *accountResolver) resolveAccounts(ctx context.Context, tx *Transaction) error {
	for _, entry := range tx.Entries {
		accountCode, err := r.generateAccountCode(entry)
		if err != nil {
			return fmt.Errorf("failed to generate account code: %w", err)
		}

		accountType, walletID, chainID, err := r.parseAccountCode(accountCode, entry)
		if err != nil {
			return fmt.Errorf("failed to parse account code: %w", err)
		}

		candidate := &Account{
			ID:        uuid.New(),
			Code:      accountCode,
			Type:      accountType,
			AssetID:   entry.AssetID,
			WalletID:  walletID,
			ChainID:   chainID,
			CreatedAt: time.Now(),
			Metadata:  make(map[string]interface{}),
		}

		if entry.Metadata != nil {
			if walletIDStr, ok := entry.Metadata["wallet_id"].(string); ok {
				candidate.Metadata["wallet_id"] = walletIDStr
			}
			if chainIDStr, ok := entry.Metadata["chain_id"].(string); ok {
				candidate.Metadata["chain_id"] = chainIDStr
			}
		}

		account, err := r.repo.GetOrCreateAccount(ctx, candidate)
		if err != nil {
			return fmt.Errorf("failed to get or create account: %w", err)
		}

		entry.AccountID = account.ID
	}

	return nil
}

func (r *accountResolver) generateAccountCode(entry *Entry) (string, error) {
	if entry.Metadata == nil {
		return "", fmt.Errorf("entry metadata is nil")
	}

	code, ok := entry.Metadata["account_code"].(string)
	if !ok || code == "" {
		return "", fmt.Errorf("account_code not found in entry metadata")
	}

	return code, nil
}

func (r *accountResolver) parseAccountCode(code string, entry *Entry) (AccountType, *uuid.UUID, *string, error) {
	var accountType AccountType

	if entry.Metadata != nil {
		if accountTypeStr, ok := entry.Metadata["account_type"].(string); ok {
			accountType = AccountType(accountTypeStr)
		}
	}

	if accountType == "" {
		switch {
		case len(code) > 7 && code[:7] == "wallet.":
			accountType = AccountTypeCryptoWallet
		case len(code) > 7 && code[:7] == "income.":
			accountType = AccountTypeIncome
		case len(code) > 8 && code[:8] == "expense.":
			accountType = AccountTypeExpense
		case len(code) > 4 && code[:4] == "gas.":
			accountType = AccountTypeGasFee
		case len(code) > 9 && code[:9] == "clearing.":
			accountType = AccountTypeClearing
		default:
			return "", nil, nil, fmt.Errorf("cannot determine account type from code: %s", code)
		}
	}

	var walletID *uuid.UUID
	var chainID *string

	if entry.Metadata != nil {
		if walletIDStr, ok := entry.Metadata["wallet_id"].(string); ok && walletIDStr != "" {
			wID, err := uuid.Parse(walletIDStr)
			if err != nil {
				return "", nil, nil, fmt.Errorf("invalid wallet_id in metadata: %w", err)
			}
			walletID = &wID
		}

		if chainIDStr, ok := entry.Metadata["chain_id"].(string); ok && chainIDStr != "" {
			chainID = &chainIDStr
		}
	}

	return accountType, walletID, chainID, nil
}

// transactionValidator validates transactions before committing them
type transactionValidator struct {
	repo Repository
}

func newTransactionValidator(repo Repository) *transactionValidator {
	return &transactionValidator{repo: repo}
}

func (v *transactionValidator) validate(ctx context.Context, tx *Transaction) error {
	if err := tx.Validate(); err != nil {
		return fmt.Errorf("transaction validation failed: %w", err)
	}

	for i, entry := range tx.Entries {
		if err := entry.Validate(); err != nil {
			return fmt.Errorf("entry %d validation failed: %w", i, err)
		}
	}

	if err := v.validateBalance(tx); err != nil {
		return fmt.Errorf("balance validation failed: %w", err)
	}

	if err := v.validateAccountBalances(ctx, tx); err != nil {
		return fmt.Errorf("account balance validation failed: %w", err)
	}

	return nil
}

func (v *transactionValidator) validateBalance(tx *Transaction) error {
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

func (v *transactionValidator) validateAccountBalances(ctx context.Context, tx *Transaction) error {
	type balanceInfo struct {
		change  *big.Int
		assetID string
	}
	balanceChanges := make(map[uuid.UUID]*balanceInfo)

	for _, entry := range tx.Entries {
		if entry.EntryType != EntryTypeAssetIncrease && entry.EntryType != EntryTypeAssetDecrease {
			continue
		}

		if _, exists := balanceChanges[entry.AccountID]; !exists {
			balanceChanges[entry.AccountID] = &balanceInfo{
				change:  big.NewInt(0),
				assetID: entry.AssetID,
			}
		}

		balanceChanges[entry.AccountID].change.Add(balanceChanges[entry.AccountID].change, entry.SignedAmount())
	}

	for accountID, info := range balanceChanges {
		currentBalance, err := v.repo.GetAccountBalance(ctx, accountID, info.assetID)
		if err != nil {
			return fmt.Errorf("failed to get account balance: %w", err)
		}

		newBalance := new(big.Int).Add(currentBalance.Balance, info.change)

		if newBalance.Sign() < 0 {
			return fmt.Errorf(
				"account %s would have negative balance for %s: current=%s, change=%s, new=%s",
				accountID.String(),
				info.assetID,
				currentBalance.Balance.String(),
				info.change.String(),
				newBalance.String(),
			)
		}
	}

	return nil
}

// transactionCommitter commits transactions to the ledger
type transactionCommitter struct {
	repo Repository
}

func newTransactionCommitter(repo Repository) *transactionCommitter {
	return &transactionCommitter{repo: repo}
}

func (c *transactionCommitter) commit(ctx context.Context, tx *Transaction) error {
	// Begin database transaction for atomicity
	txCtx, err := c.repo.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Ensure rollback on error
	committed := false
	defer func() {
		if !committed {
			// Rollback on any error - ignore rollback errors as the commit failed anyway
			_ = c.repo.RollbackTx(txCtx)
		}
	}()

	// Create the transaction and entries within the DB transaction
	if err := c.repo.CreateTransaction(txCtx, tx); err != nil {
		return fmt.Errorf("failed to create transaction: %w", err)
	}

	// Update balances within the same DB transaction
	if err := c.updateBalances(txCtx, tx); err != nil {
		return fmt.Errorf("failed to update balances: %w", err)
	}

	// Commit the DB transaction
	if err := c.repo.CommitTx(txCtx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	committed = true
	return nil
}

type balanceChange struct {
	accountID uuid.UUID
	assetID   string
	change    *big.Int
}

func (c *transactionCommitter) updateBalances(ctx context.Context, tx *Transaction) error {
	balanceChanges := make(map[string]*balanceChange)

	for _, entry := range tx.Entries {
		if entry.EntryType != EntryTypeAssetIncrease && entry.EntryType != EntryTypeAssetDecrease {
			continue
		}

		key := fmt.Sprintf("%s:%s", entry.AccountID.String(), entry.AssetID)

		if _, exists := balanceChanges[key]; !exists {
			balanceChanges[key] = &balanceChange{
				accountID: entry.AccountID,
				assetID:   entry.AssetID,
				change:    big.NewInt(0),
			}
		}

		balanceChanges[key].change.Add(balanceChanges[key].change, entry.SignedAmount())
	}

	for _, bc := range balanceChanges {
		if err := c.applyBalanceChange(ctx, bc); err != nil {
			return fmt.Errorf("failed to apply balance change for %s:%s: %w", bc.accountID, bc.assetID, err)
		}
	}

	return nil
}

func (c *transactionCommitter) applyBalanceChange(ctx context.Context, bc *balanceChange) error {
	// Use FOR UPDATE to acquire row-level lock and prevent race conditions
	// This is only effective within a DB transaction (which we're in from commit())
	currentBalance, err := c.repo.GetAccountBalanceForUpdate(ctx, bc.accountID, bc.assetID)
	if err != nil {
		return fmt.Errorf("failed to get current balance: %w", err)
	}

	newBalance := new(big.Int).Add(currentBalance.Balance, bc.change)

	if newBalance.Sign() < 0 {
		return fmt.Errorf("balance would be negative: current=%s, change=%s", currentBalance.Balance.String(), bc.change.String())
	}

	updatedBalance := &AccountBalance{
		AccountID:   bc.accountID,
		AssetID:     bc.assetID,
		Balance:     newBalance,
		USDValue:    currentBalance.USDValue,
		LastUpdated: time.Now(),
	}

	if err := c.repo.UpsertAccountBalance(ctx, updatedBalance); err != nil {
		return fmt.Errorf("failed to upsert balance: %w", err)
	}

	return nil
}
