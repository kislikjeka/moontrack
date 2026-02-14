package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kislikjeka/moontrack/internal/platform/wallet"
)

// WalletRepository implements the wallet repository using PostgreSQL
type WalletRepository struct {
	pool *pgxpool.Pool
}

// NewWalletRepository creates a new PostgreSQL wallet repository
func NewWalletRepository(pool *pgxpool.Pool) *WalletRepository {
	return &WalletRepository{pool: pool}
}

// Create creates a new wallet
func (r *WalletRepository) Create(ctx context.Context, w *wallet.Wallet) error {
	query := `
		INSERT INTO wallets (id, user_id, name, chain_id, address, sync_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	now := time.Now()
	w.CreatedAt = now
	w.UpdatedAt = now

	if w.ID == uuid.Nil {
		w.ID = uuid.New()
	}

	// Default sync status to pending
	if w.SyncStatus == "" {
		w.SyncStatus = wallet.SyncStatusPending
	}

	_, err := r.pool.Exec(ctx, query,
		w.ID,
		w.UserID,
		w.Name,
		w.ChainID,
		w.Address,
		w.SyncStatus,
		w.CreatedAt,
		w.UpdatedAt,
	)

	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "idx_wallets_user_chain_address") {
			return wallet.ErrDuplicateAddress
		}
		if strings.Contains(errStr, "wallets_user_id_name_key") {
			return wallet.ErrDuplicateWalletName
		}
		return fmt.Errorf("failed to create wallet: %w", err)
	}

	return nil
}

// GetByID retrieves a wallet by ID
func (r *WalletRepository) GetByID(ctx context.Context, id uuid.UUID) (*wallet.Wallet, error) {
	query := `
		SELECT id, user_id, name, chain_id, address, sync_status, last_sync_block, last_sync_at, sync_error, sync_started_at, created_at, updated_at
		FROM wallets
		WHERE id = $1
	`

	w := &wallet.Wallet{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&w.ID,
		&w.UserID,
		&w.Name,
		&w.ChainID,
		&w.Address,
		&w.SyncStatus,
		&w.LastSyncBlock,
		&w.LastSyncAt,
		&w.SyncError,
		&w.SyncStartedAt,
		&w.CreatedAt,
		&w.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, wallet.ErrWalletNotFound
		}
		return nil, fmt.Errorf("failed to get wallet: %w", err)
	}

	return w, nil
}

// GetByUserID retrieves all wallets for a user
func (r *WalletRepository) GetByUserID(ctx context.Context, userID uuid.UUID) ([]*wallet.Wallet, error) {
	query := `
		SELECT id, user_id, name, chain_id, address, sync_status, last_sync_block, last_sync_at, sync_error, sync_started_at, created_at, updated_at
		FROM wallets
		WHERE user_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query wallets: %w", err)
	}
	defer rows.Close()

	var wallets []*wallet.Wallet
	for rows.Next() {
		w := &wallet.Wallet{}
		err := rows.Scan(
			&w.ID,
			&w.UserID,
			&w.Name,
			&w.ChainID,
			&w.Address,
			&w.SyncStatus,
			&w.LastSyncBlock,
			&w.LastSyncAt,
			&w.SyncError,
			&w.SyncStartedAt,
			&w.CreatedAt,
			&w.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan wallet: %w", err)
		}
		wallets = append(wallets, w)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating wallets: %w", err)
	}

	return wallets, nil
}

// Update updates an existing wallet
func (r *WalletRepository) Update(ctx context.Context, w *wallet.Wallet) error {
	query := `
		UPDATE wallets
		SET name = $1, chain_id = $2, address = $3, updated_at = $4
		WHERE id = $5
	`

	w.UpdatedAt = time.Now()

	result, err := r.pool.Exec(ctx, query,
		w.Name,
		w.ChainID,
		w.Address,
		w.UpdatedAt,
		w.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update wallet: %w", err)
	}

	if result.RowsAffected() == 0 {
		return wallet.ErrWalletNotFound
	}

	return nil
}

// Delete deletes a wallet by ID
func (r *WalletRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM wallets WHERE id = $1`

	result, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete wallet: %w", err)
	}

	if result.RowsAffected() == 0 {
		return wallet.ErrWalletNotFound
	}

	return nil
}

// ExistsByUserAndName checks if a wallet with the given name exists for the user
func (r *WalletRepository) ExistsByUserAndName(ctx context.Context, userID uuid.UUID, name string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM wallets WHERE user_id = $1 AND name = $2)`

	var exists bool
	err := r.pool.QueryRow(ctx, query, userID, name).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check wallet existence: %w", err)
	}

	return exists, nil
}

// ExistsByUserChainAndAddress checks if a wallet with the given chain/address exists for the user
func (r *WalletRepository) ExistsByUserChainAndAddress(ctx context.Context, userID uuid.UUID, chainID int64, address string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM wallets WHERE user_id = $1 AND chain_id = $2 AND lower(address) = lower($3))`

	var exists bool
	err := r.pool.QueryRow(ctx, query, userID, chainID, address).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check wallet existence: %w", err)
	}

	return exists, nil
}

// GetWalletsForSync retrieves wallets that need syncing (pending, error, synced, or stale syncing)
func (r *WalletRepository) GetWalletsForSync(ctx context.Context) ([]*wallet.Wallet, error) {
	query := `
		SELECT id, user_id, name, chain_id, address, sync_status, last_sync_block, last_sync_at, sync_error, sync_started_at, created_at, updated_at
		FROM wallets
		WHERE sync_status IN ('pending', 'error', 'synced')
		   OR (sync_status = 'syncing' AND sync_started_at < NOW() - INTERVAL '15 minutes')
		ORDER BY
			CASE sync_status
				WHEN 'pending' THEN 1
				WHEN 'error' THEN 2
				ELSE 3
			END,
			last_sync_at NULLS FIRST
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query wallets for sync: %w", err)
	}
	defer rows.Close()

	var wallets []*wallet.Wallet
	for rows.Next() {
		w := &wallet.Wallet{}
		err := rows.Scan(
			&w.ID,
			&w.UserID,
			&w.Name,
			&w.ChainID,
			&w.Address,
			&w.SyncStatus,
			&w.LastSyncBlock,
			&w.LastSyncAt,
			&w.SyncError,
			&w.SyncStartedAt,
			&w.CreatedAt,
			&w.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan wallet: %w", err)
		}
		wallets = append(wallets, w)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating wallets: %w", err)
	}

	return wallets, nil
}

// GetWalletsByAddressAndUserID retrieves wallets with a given address for a specific user
func (r *WalletRepository) GetWalletsByAddressAndUserID(ctx context.Context, address string, userID uuid.UUID) ([]*wallet.Wallet, error) {
	query := `
		SELECT id, user_id, name, chain_id, address, sync_status, last_sync_block, last_sync_at, sync_error, sync_started_at, created_at, updated_at
		FROM wallets
		WHERE lower(address) = lower($1) AND user_id = $2
	`

	rows, err := r.pool.Query(ctx, query, address, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query wallets by address and user: %w", err)
	}
	defer rows.Close()

	var wallets []*wallet.Wallet
	for rows.Next() {
		w := &wallet.Wallet{}
		err := rows.Scan(
			&w.ID,
			&w.UserID,
			&w.Name,
			&w.ChainID,
			&w.Address,
			&w.SyncStatus,
			&w.LastSyncBlock,
			&w.LastSyncAt,
			&w.SyncError,
			&w.SyncStartedAt,
			&w.CreatedAt,
			&w.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan wallet: %w", err)
		}
		wallets = append(wallets, w)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating wallets: %w", err)
	}

	return wallets, nil
}

// ClaimWalletForSync atomically claims a wallet for syncing using UPDATE...RETURNING
// Returns true if the wallet was claimed, false if it was already being synced
func (r *WalletRepository) ClaimWalletForSync(ctx context.Context, walletID uuid.UUID) (bool, error) {
	query := `
		UPDATE wallets
		SET sync_status = $1, sync_error = NULL, sync_started_at = $2, updated_at = $2
		WHERE id = $3
		  AND sync_status != 'syncing'
		RETURNING id
	`

	now := time.Now()
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, query, wallet.SyncStatusSyncing, now, walletID).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Already syncing — not claimed
			return false, nil
		}
		return false, fmt.Errorf("failed to claim wallet for sync: %w", err)
	}

	return true, nil
}

// UpdateSyncState updates the sync status and related fields for a wallet
func (r *WalletRepository) UpdateSyncState(ctx context.Context, walletID uuid.UUID, status wallet.SyncStatus, lastBlock *int64, syncError *string) error {
	query := `
		UPDATE wallets
		SET sync_status = $1, last_sync_block = $2, last_sync_at = $3, sync_error = $4, updated_at = $5
		WHERE id = $6
	`

	now := time.Now()
	var lastSyncAt *time.Time
	if status == wallet.SyncStatusSynced {
		lastSyncAt = &now
	}

	result, err := r.pool.Exec(ctx, query, status, lastBlock, lastSyncAt, syncError, now, walletID)
	if err != nil {
		return fmt.Errorf("failed to update sync state: %w", err)
	}

	if result.RowsAffected() == 0 {
		return wallet.ErrWalletNotFound
	}

	return nil
}

// SetSyncInProgress marks a wallet as currently syncing
func (r *WalletRepository) SetSyncInProgress(ctx context.Context, walletID uuid.UUID) error {
	query := `
		UPDATE wallets
		SET sync_status = $1, sync_error = NULL, updated_at = $2
		WHERE id = $3
	`

	result, err := r.pool.Exec(ctx, query, wallet.SyncStatusSyncing, time.Now(), walletID)
	if err != nil {
		return fmt.Errorf("failed to set sync in progress: %w", err)
	}

	if result.RowsAffected() == 0 {
		return wallet.ErrWalletNotFound
	}

	return nil
}

// SetSyncCompleted marks a wallet sync as completed
func (r *WalletRepository) SetSyncCompleted(ctx context.Context, walletID uuid.UUID, lastBlock int64, syncAt time.Time) error {
	query := `
		UPDATE wallets
		SET sync_status = $1, last_sync_block = $2, last_sync_at = $3, sync_error = NULL, updated_at = $4
		WHERE id = $5
	`

	result, err := r.pool.Exec(ctx, query, wallet.SyncStatusSynced, lastBlock, syncAt, time.Now(), walletID)
	if err != nil {
		return fmt.Errorf("failed to set sync completed: %w", err)
	}

	if result.RowsAffected() == 0 {
		return wallet.ErrWalletNotFound
	}

	return nil
}

// SetSyncCompletedAt marks a wallet sync as completed at a given time (without block number)
func (r *WalletRepository) SetSyncCompletedAt(ctx context.Context, walletID uuid.UUID, syncAt time.Time) error {
	query := `
		UPDATE wallets
		SET sync_status = $1, last_sync_at = $2, sync_error = NULL, updated_at = $3
		WHERE id = $4
	`

	result, err := r.pool.Exec(ctx, query, wallet.SyncStatusSynced, syncAt, time.Now(), walletID)
	if err != nil {
		return fmt.Errorf("failed to set sync completed: %w", err)
	}

	if result.RowsAffected() == 0 {
		return wallet.ErrWalletNotFound
	}

	return nil
}

// SetSyncError marks a wallet sync as failed with an error message
func (r *WalletRepository) SetSyncError(ctx context.Context, walletID uuid.UUID, errMsg string) error {
	query := `
		UPDATE wallets
		SET sync_status = $1, sync_error = $2, updated_at = $3
		WHERE id = $4
	`

	result, err := r.pool.Exec(ctx, query, wallet.SyncStatusError, errMsg, time.Now(), walletID)
	if err != nil {
		return fmt.Errorf("failed to set sync error: %w", err)
	}

	if result.RowsAffected() == 0 {
		return wallet.ErrWalletNotFound
	}

	return nil
}
