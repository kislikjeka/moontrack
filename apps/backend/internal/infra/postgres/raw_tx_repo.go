package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kislikjeka/moontrack/internal/platform/sync"
)

// Compile-time check that RawTransactionRepository implements sync.RawTransactionRepository.
var _ sync.RawTransactionRepository = (*RawTransactionRepository)(nil)

// RawTransactionRepository implements the raw transaction repository using PostgreSQL.
type RawTransactionRepository struct {
	pool *pgxpool.Pool
}

// NewRawTransactionRepository creates a new PostgreSQL raw transaction repository.
func NewRawTransactionRepository(pool *pgxpool.Pool) *RawTransactionRepository {
	return &RawTransactionRepository{pool: pool}
}

const rawTxColumns = `id, wallet_id, zerion_id, tx_hash, chain_id,
	operation_type, mined_at, status, raw_json,
	processing_status, processing_error, ledger_tx_id,
	is_synthetic, created_at, processed_at`

// UpsertRawTransaction inserts a raw transaction, ignoring duplicates.
func (r *RawTransactionRepository) UpsertRawTransaction(ctx context.Context, raw *sync.RawTransaction) error {
	query := `
		INSERT INTO raw_transactions (
			id, wallet_id, zerion_id, tx_hash, chain_id,
			operation_type, mined_at, status, raw_json,
			processing_status, processing_error, ledger_tx_id,
			is_synthetic, created_at, processed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (wallet_id, zerion_id) DO NOTHING
	`

	if raw.ID == uuid.Nil {
		raw.ID = uuid.New()
	}
	if raw.CreatedAt.IsZero() {
		raw.CreatedAt = time.Now()
	}

	_, err := r.pool.Exec(ctx, query,
		raw.ID, raw.WalletID, raw.ZerionID, raw.TxHash, raw.ChainID,
		raw.OperationType, raw.MinedAt, raw.Status, raw.RawJSON,
		raw.ProcessingStatus, raw.ProcessingError, raw.LedgerTxID,
		raw.IsSynthetic, raw.CreatedAt, raw.ProcessedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert raw transaction: %w", err)
	}

	return nil
}

// GetPendingByWallet returns pending raw transactions ordered by mined_at ASC.
func (r *RawTransactionRepository) GetPendingByWallet(ctx context.Context, walletID uuid.UUID) ([]*sync.RawTransaction, error) {
	query := `
		SELECT ` + rawTxColumns + `
		FROM raw_transactions
		WHERE wallet_id = $1 AND processing_status = 'pending'
		ORDER BY mined_at ASC
	`

	return r.queryRawTransactions(ctx, query, walletID)
}

// GetAllByWallet returns all raw transactions for a wallet ordered by mined_at ASC.
func (r *RawTransactionRepository) GetAllByWallet(ctx context.Context, walletID uuid.UUID) ([]*sync.RawTransaction, error) {
	query := `
		SELECT ` + rawTxColumns + `
		FROM raw_transactions
		WHERE wallet_id = $1
		ORDER BY mined_at ASC
	`

	return r.queryRawTransactions(ctx, query, walletID)
}

// MarkProcessed marks a raw transaction as successfully processed with the ledger tx ID.
func (r *RawTransactionRepository) MarkProcessed(ctx context.Context, rawID uuid.UUID, ledgerTxID uuid.UUID) error {
	query := `
		UPDATE raw_transactions
		SET processing_status = 'processed', ledger_tx_id = $1, processed_at = now()
		WHERE id = $2
	`

	_, err := r.pool.Exec(ctx, query, ledgerTxID, rawID)
	if err != nil {
		return fmt.Errorf("failed to mark raw transaction as processed: %w", err)
	}

	return nil
}

// MarkSkipped marks a raw transaction as skipped with a reason.
func (r *RawTransactionRepository) MarkSkipped(ctx context.Context, rawID uuid.UUID, reason string) error {
	query := `
		UPDATE raw_transactions
		SET processing_status = 'skipped', processing_error = $1, processed_at = now()
		WHERE id = $2
	`

	_, err := r.pool.Exec(ctx, query, reason, rawID)
	if err != nil {
		return fmt.Errorf("failed to mark raw transaction as skipped: %w", err)
	}

	return nil
}

// MarkError marks a raw transaction as having a processing error.
func (r *RawTransactionRepository) MarkError(ctx context.Context, rawID uuid.UUID, errMsg string) error {
	query := `
		UPDATE raw_transactions
		SET processing_status = 'error', processing_error = $1, processed_at = now()
		WHERE id = $2
	`

	_, err := r.pool.Exec(ctx, query, errMsg, rawID)
	if err != nil {
		return fmt.Errorf("failed to mark raw transaction as error: %w", err)
	}

	return nil
}

// ResetProcessingStatus resets all raw transactions for a wallet back to pending.
func (r *RawTransactionRepository) ResetProcessingStatus(ctx context.Context, walletID uuid.UUID) error {
	query := `
		UPDATE raw_transactions
		SET processing_status = 'pending', processing_error = NULL, ledger_tx_id = NULL, processed_at = NULL
		WHERE wallet_id = $1
	`

	_, err := r.pool.Exec(ctx, query, walletID)
	if err != nil {
		return fmt.Errorf("failed to reset processing status: %w", err)
	}

	return nil
}

// GetEarliestMinedAt returns the earliest mined_at timestamp for a wallet's raw transactions.
func (r *RawTransactionRepository) GetEarliestMinedAt(ctx context.Context, walletID uuid.UUID) (*time.Time, error) {
	query := `SELECT MIN(mined_at) FROM raw_transactions WHERE wallet_id = $1`

	var minedAt *time.Time
	err := r.pool.QueryRow(ctx, query, walletID).Scan(&minedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to get earliest mined_at: %w", err)
	}

	return minedAt, nil
}

// DeleteSyntheticByWallet deletes all synthetic raw transactions for a wallet.
func (r *RawTransactionRepository) DeleteSyntheticByWallet(ctx context.Context, walletID uuid.UUID) error {
	query := `DELETE FROM raw_transactions WHERE wallet_id = $1 AND is_synthetic = true`

	_, err := r.pool.Exec(ctx, query, walletID)
	if err != nil {
		return fmt.Errorf("failed to delete synthetic raw transactions: %w", err)
	}

	return nil
}

// queryRawTransactions is a shared helper for querying and scanning raw transactions.
func (r *RawTransactionRepository) queryRawTransactions(ctx context.Context, query string, args ...any) ([]*sync.RawTransaction, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query raw transactions: %w", err)
	}
	defer rows.Close()

	var results []*sync.RawTransaction
	for rows.Next() {
		rt := &sync.RawTransaction{}
		err := rows.Scan(
			&rt.ID, &rt.WalletID, &rt.ZerionID, &rt.TxHash, &rt.ChainID,
			&rt.OperationType, &rt.MinedAt, &rt.Status, &rt.RawJSON,
			&rt.ProcessingStatus, &rt.ProcessingError, &rt.LedgerTxID,
			&rt.IsSynthetic, &rt.CreatedAt, &rt.ProcessedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan raw transaction: %w", err)
		}
		results = append(results, rt)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating raw transactions: %w", err)
	}

	return results, nil
}
