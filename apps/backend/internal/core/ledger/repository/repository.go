package repository

import (
	"context"
	"math/big"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/core/ledger/domain"
)

// LedgerRepository defines the interface for ledger persistence operations
type LedgerRepository interface {
	// Account operations
	CreateAccount(ctx context.Context, account *domain.Account) error
	GetAccount(ctx context.Context, id uuid.UUID) (*domain.Account, error)
	GetAccountByCode(ctx context.Context, code string) (*domain.Account, error)
	FindAccountsByWallet(ctx context.Context, walletID uuid.UUID) ([]*domain.Account, error)

	// Transaction operations
	CreateTransaction(ctx context.Context, tx *domain.Transaction) error
	GetTransaction(ctx context.Context, id uuid.UUID) (*domain.Transaction, error)
	FindTransactionsBySource(ctx context.Context, source string, externalID string) (*domain.Transaction, error)
	ListTransactions(ctx context.Context, filters TransactionFilters) ([]*domain.Transaction, error)

	// Entry operations (read-only - entries are immutable)
	GetEntriesByTransaction(ctx context.Context, transactionID uuid.UUID) ([]*domain.Entry, error)
	GetEntriesByAccount(ctx context.Context, accountID uuid.UUID) ([]*domain.Entry, error)

	// Balance operations
	GetAccountBalance(ctx context.Context, accountID uuid.UUID, assetID string) (*domain.AccountBalance, error)
	UpsertAccountBalance(ctx context.Context, balance *domain.AccountBalance) error
	GetAccountBalances(ctx context.Context, accountID uuid.UUID) ([]*domain.AccountBalance, error)
	CalculateBalanceFromEntries(ctx context.Context, accountID uuid.UUID, assetID string) (*big.Int, error)

	// Transaction management
	BeginTx(ctx context.Context) (context.Context, error)
	CommitTx(ctx context.Context) error
	RollbackTx(ctx context.Context) error
}

// TransactionFilters defines filters for listing transactions
type TransactionFilters struct {
	UserID     *uuid.UUID
	Type       *string
	Status     *domain.TransactionStatus
	FromDate   *string
	ToDate     *string
	Limit      int
	Offset     int
}
