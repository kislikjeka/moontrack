package sync

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// RawTransactionRepository defines data access for raw transactions
type RawTransactionRepository interface {
	// UpsertRawTransaction inserts a raw transaction, ignoring duplicates (ON CONFLICT DO NOTHING)
	UpsertRawTransaction(ctx context.Context, raw *RawTransaction) error

	// GetPendingByWallet returns pending raw transactions ordered by mined_at ASC
	GetPendingByWallet(ctx context.Context, walletID uuid.UUID) ([]*RawTransaction, error)

	// GetAllByWallet returns all raw transactions for a wallet ordered by mined_at ASC
	GetAllByWallet(ctx context.Context, walletID uuid.UUID) ([]*RawTransaction, error)

	// MarkProcessed marks a raw transaction as successfully processed with the ledger tx ID
	MarkProcessed(ctx context.Context, rawID uuid.UUID, ledgerTxID uuid.UUID) error

	// MarkSkipped marks a raw transaction as skipped with a reason
	MarkSkipped(ctx context.Context, rawID uuid.UUID, reason string) error

	// MarkError marks a raw transaction as having a processing error
	MarkError(ctx context.Context, rawID uuid.UUID, errMsg string) error

	// ResetProcessingStatus resets all raw transactions for a wallet back to pending
	ResetProcessingStatus(ctx context.Context, walletID uuid.UUID) error

	// GetEarliestMinedAt returns the earliest mined_at timestamp for a wallet's raw transactions
	GetEarliestMinedAt(ctx context.Context, walletID uuid.UUID) (*time.Time, error)

	// DeleteSyntheticByWallet deletes all synthetic raw transactions for a wallet
	DeleteSyntheticByWallet(ctx context.Context, walletID uuid.UUID) error
}
