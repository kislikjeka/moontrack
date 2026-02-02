package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/core/ledger/domain"
	"github.com/kislikjeka/moontrack/internal/core/ledger/repository"
)

// AccountResolver resolves account references in ledger entries
// It creates accounts if they don't exist yet
type AccountResolver struct {
	repo repository.LedgerRepository
}

// NewAccountResolver creates a new account resolver
func NewAccountResolver(repo repository.LedgerRepository) *AccountResolver {
	return &AccountResolver{
		repo: repo,
	}
}

// ResolveAccounts resolves all account references in the transaction's entries
// For each entry, it:
// 1. Checks if an account exists for the given code
// 2. Creates the account if it doesn't exist
// 3. Sets the account ID in the entry
func (r *AccountResolver) ResolveAccounts(ctx context.Context, tx *domain.Transaction) error {
	for _, entry := range tx.Entries {
		// Generate account code based on entry metadata
		accountCode, err := r.generateAccountCode(entry)
		if err != nil {
			return fmt.Errorf("failed to generate account code: %w", err)
		}

		// Try to get existing account
		account, err := r.repo.GetAccountByCode(ctx, accountCode)
		if err != nil {
			// Account doesn't exist, create it
			account, err = r.createAccount(ctx, entry, accountCode)
			if err != nil {
				return fmt.Errorf("failed to create account: %w", err)
			}
		}

		// Set the account ID in the entry
		entry.AccountID = account.ID
	}

	return nil
}

// generateAccountCode generates an account code from an entry
// Code format examples:
// - "wallet.{wallet_id}.{asset_id}" for wallet accounts
// - "income.{asset_id}" for income accounts
// - "expense.{asset_id}" for expense accounts
func (r *AccountResolver) generateAccountCode(entry *domain.Entry) (string, error) {
	// The account code should be in the entry metadata
	// This is set by the transaction handler
	if entry.Metadata == nil {
		return "", fmt.Errorf("entry metadata is nil")
	}

	code, ok := entry.Metadata["account_code"].(string)
	if !ok || code == "" {
		return "", fmt.Errorf("account_code not found in entry metadata")
	}

	return code, nil
}

// createAccount creates a new account based on entry information
func (r *AccountResolver) createAccount(ctx context.Context, entry *domain.Entry, code string) (*domain.Account, error) {
	// Determine account type and extract metadata
	accountType, walletID, chainID, err := r.parseAccountCode(code, entry)
	if err != nil {
		return nil, fmt.Errorf("failed to parse account code: %w", err)
	}

	account := &domain.Account{
		ID:        uuid.New(),
		Code:      code,
		Type:      accountType,
		AssetID:   entry.AssetID,
		WalletID:  walletID,
		ChainID:   chainID,
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	// Store additional metadata if provided
	if entry.Metadata != nil {
		if walletIDStr, ok := entry.Metadata["wallet_id"].(string); ok {
			account.Metadata["wallet_id"] = walletIDStr
		}
		if chainIDStr, ok := entry.Metadata["chain_id"].(string); ok {
			account.Metadata["chain_id"] = chainIDStr
		}
	}

	if err := r.repo.CreateAccount(ctx, account); err != nil {
		return nil, fmt.Errorf("failed to create account in repository: %w", err)
	}

	return account, nil
}

// parseAccountCode parses the account code to determine type and attributes
func (r *AccountResolver) parseAccountCode(code string, entry *domain.Entry) (domain.AccountType, *uuid.UUID, *string, error) {
	// Extract account type from code prefix
	var accountType domain.AccountType

	// The account type should be in the entry metadata
	if entry.Metadata != nil {
		if accountTypeStr, ok := entry.Metadata["account_type"].(string); ok {
			accountType = domain.AccountType(accountTypeStr)
		}
	}

	// If not in metadata, infer from code
	if accountType == "" {
		switch {
		case len(code) > 7 && code[:7] == "wallet.":
			accountType = domain.AccountTypeCryptoWallet
		case len(code) > 7 && code[:7] == "income.":
			accountType = domain.AccountTypeIncome
		case len(code) > 8 && code[:8] == "expense.":
			accountType = domain.AccountTypeExpense
		case len(code) > 4 && code[:4] == "gas.":
			accountType = domain.AccountTypeGasFee
		default:
			return "", nil, nil, fmt.Errorf("cannot determine account type from code: %s", code)
		}
	}

	// Extract wallet ID and chain ID from metadata
	var walletID *uuid.UUID
	var chainID *string

	if entry.Metadata != nil {
		// Parse wallet ID
		if walletIDStr, ok := entry.Metadata["wallet_id"].(string); ok && walletIDStr != "" {
			wID, err := uuid.Parse(walletIDStr)
			if err != nil {
				return "", nil, nil, fmt.Errorf("invalid wallet_id in metadata: %w", err)
			}
			walletID = &wID
		}

		// Parse chain ID
		if chainIDStr, ok := entry.Metadata["chain_id"].(string); ok && chainIDStr != "" {
			chainID = &chainIDStr
		}
	}

	return accountType, walletID, chainID, nil
}
