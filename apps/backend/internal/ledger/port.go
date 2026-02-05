package ledger

import (
	"context"
	"math/big"

	"github.com/google/uuid"
)

// Repository defines the interface for ledger persistence operations
type Repository interface {
	// Account operations
	CreateAccount(ctx context.Context, account *Account) error
	GetAccount(ctx context.Context, id uuid.UUID) (*Account, error)
	GetAccountByCode(ctx context.Context, code string) (*Account, error)
	FindAccountsByWallet(ctx context.Context, walletID uuid.UUID) ([]*Account, error)

	// Transaction operations
	CreateTransaction(ctx context.Context, tx *Transaction) error
	GetTransaction(ctx context.Context, id uuid.UUID) (*Transaction, error)
	FindTransactionsBySource(ctx context.Context, source string, externalID string) (*Transaction, error)
	ListTransactions(ctx context.Context, filters TransactionFilters) ([]*Transaction, error)

	// Entry operations (read-only - entries are immutable)
	GetEntriesByTransaction(ctx context.Context, transactionID uuid.UUID) ([]*Entry, error)
	GetEntriesByAccount(ctx context.Context, accountID uuid.UUID) ([]*Entry, error)

	// Balance operations
	GetAccountBalance(ctx context.Context, accountID uuid.UUID, assetID string) (*AccountBalance, error)
	GetAccountBalanceForUpdate(ctx context.Context, accountID uuid.UUID, assetID string) (*AccountBalance, error)
	UpsertAccountBalance(ctx context.Context, balance *AccountBalance) error
	GetAccountBalances(ctx context.Context, accountID uuid.UUID) ([]*AccountBalance, error)
	CalculateBalanceFromEntries(ctx context.Context, accountID uuid.UUID, assetID string) (*big.Int, error)

	// Transaction management
	BeginTx(ctx context.Context) (context.Context, error)
	CommitTx(ctx context.Context) error
	RollbackTx(ctx context.Context) error
}

// TransactionFilters defines filters for listing transactions
type TransactionFilters struct {
	UserID   *uuid.UUID
	Type     *string
	Status   *TransactionStatus
	FromDate *string
	ToDate   *string
	Limit    int
	Offset   int
}
