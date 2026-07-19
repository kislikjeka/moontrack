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

// Create creates a new wallet and seeds its chain set (wallet_chain_sync rows)
// with the default Enabled chains. The wallet row and its chain-set rows are
// written in one transaction so a wallet never exists without a chain set.
func (r *WalletRepository) Create(ctx context.Context, w *wallet.Wallet) error {
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

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO wallets (id, user_id, name, address, sync_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`,
		w.ID,
		w.UserID,
		w.Name,
		w.Address,
		w.SyncStatus,
		w.CreatedAt,
		w.UpdatedAt,
	)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "wallets_user_id_fkey") {
			return wallet.ErrUserNotFound
		}
		if strings.Contains(errStr, "idx_wallets_user_address") {
			return wallet.ErrDuplicateAddress
		}
		if strings.Contains(errStr, "wallets_user_id_name_key") {
			return wallet.ErrDuplicateWalletName
		}
		return fmt.Errorf("failed to insert wallet: %w", err)
	}

	// Seed the chain set with the default Enabled chains.
	for _, chain := range wallet.EnabledChains() {
		if _, err := tx.Exec(ctx, `
			INSERT INTO wallet_chain_sync (wallet_id, chain, sync_status, sync_phase)
			VALUES ($1, $2, $3, 'idle')
		`, w.ID, chain, wallet.SyncStatusPending); err != nil {
			return fmt.Errorf("failed to seed chain %s: %w", chain, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit wallet create: %w", err)
	}

	return nil
}

// GetByID retrieves a wallet by ID
func (r *WalletRepository) GetByID(ctx context.Context, id uuid.UUID) (*wallet.Wallet, error) {
	query := `
		SELECT id, user_id, name, address, sync_status, last_sync_at, sync_error, sync_started_at, created_at, updated_at, sync_phase, collect_cursor_at
		FROM wallets
		WHERE id = $1
	`

	w := &wallet.Wallet{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&w.ID,
		&w.UserID,
		&w.Name,
		&w.Address,
		&w.SyncStatus,
		&w.LastSyncAt,
		&w.SyncError,
		&w.SyncStartedAt,
		&w.CreatedAt,
		&w.UpdatedAt,
		&w.SyncPhase,
		&w.CollectCursorAt,
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
		SELECT id, user_id, name, address, sync_status, last_sync_at, sync_error, sync_started_at, created_at, updated_at, sync_phase, collect_cursor_at
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
			&w.Address,
			&w.SyncStatus,
			&w.LastSyncAt,
			&w.SyncError,
			&w.SyncStartedAt,
			&w.CreatedAt,
			&w.UpdatedAt,
			&w.SyncPhase,
			&w.CollectCursorAt,
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
		SET name = $1, address = $2, updated_at = $3
		WHERE id = $4
	`

	w.UpdatedAt = time.Now()

	result, err := r.pool.Exec(ctx, query,
		w.Name,
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

// ExistsByUserAndAddress checks if a wallet with the given address exists for the user
func (r *WalletRepository) ExistsByUserAndAddress(ctx context.Context, userID uuid.UUID, address string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM wallets WHERE user_id = $1 AND lower(address) = lower($2))`

	var exists bool
	err := r.pool.QueryRow(ctx, query, userID, address).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check wallet existence: %w", err)
	}

	return exists, nil
}

// GetWalletsForSync retrieves wallets that need syncing (pending, error, synced, or stale syncing)
func (r *WalletRepository) GetWalletsForSync(ctx context.Context) ([]*wallet.Wallet, error) {
	query := `
		SELECT id, user_id, name, address, sync_status, last_sync_at, sync_error, sync_started_at, created_at, updated_at, sync_phase, collect_cursor_at
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
			&w.Address,
			&w.SyncStatus,
			&w.LastSyncAt,
			&w.SyncError,
			&w.SyncStartedAt,
			&w.CreatedAt,
			&w.UpdatedAt,
			&w.SyncPhase,
			&w.CollectCursorAt,
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
		SELECT id, user_id, name, address, sync_status, last_sync_at, sync_error, sync_started_at, created_at, updated_at, sync_phase, collect_cursor_at
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
			&w.Address,
			&w.SyncStatus,
			&w.LastSyncAt,
			&w.SyncError,
			&w.SyncStartedAt,
			&w.CreatedAt,
			&w.UpdatedAt,
			&w.SyncPhase,
			&w.CollectCursorAt,
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
// Returns true if the wallet was claimed, false if it was already being synced.
// On a successful claim it also flips every chain row to 'syncing', so the
// wallet-level status stays a true rollup over the chain set (issue #27).
func (r *WalletRepository) ClaimWalletForSync(ctx context.Context, walletID uuid.UUID) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	now := time.Now()
	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		UPDATE wallets
		SET sync_status = $1, sync_error = NULL, sync_started_at = $2, updated_at = $3
		WHERE id = $4
		  AND sync_status != 'syncing'
		RETURNING id
	`, wallet.SyncStatusSyncing, now, now, walletID).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Already syncing — not claimed
			return false, nil
		}
		return false, fmt.Errorf("failed to claim wallet for sync: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE wallet_chain_sync
		SET sync_status = $1, sync_error = NULL, updated_at = now()
		WHERE wallet_id = $2
	`, wallet.SyncStatusSyncing, walletID); err != nil {
		return false, fmt.Errorf("failed to mark chain rows syncing: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("failed to commit claim: %w", err)
	}

	return true, nil
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

// SetSyncCompletedAt marks the wallet-level row as synced at a given time. It
// touches only the wallets row; per-chain completion is owned by
// SetChainSyncCompleted and the wallet-level status is a rollup derived by
// RollupWalletSyncStatus (issue #28). Retained for callers that legitimately set
// a wallet-wide completion outside the per-chain pipeline.
func (r *WalletRepository) SetSyncCompletedAt(ctx context.Context, walletID uuid.UUID, syncAt time.Time) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE wallets
		SET sync_status = $1, last_sync_at = $2, sync_error = NULL, updated_at = $3
		WHERE id = $4
	`, wallet.SyncStatusSynced, syncAt, time.Now(), walletID)
	if err != nil {
		return fmt.Errorf("failed to set sync completed: %w", err)
	}
	if result.RowsAffected() == 0 {
		return wallet.ErrWalletNotFound
	}
	return nil
}

// SetSyncError marks the wallet-level row as failed. It touches only the wallets
// row; per-chain failure is owned by SetChainSyncError and the wallet-level status
// is a rollup derived by RollupWalletSyncStatus (issue #28). One chain's failure
// no longer blanket-errors every chain.
func (r *WalletRepository) SetSyncError(ctx context.Context, walletID uuid.UUID, errMsg string) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE wallets
		SET sync_status = $1, sync_error = $2, updated_at = $3
		WHERE id = $4
	`, wallet.SyncStatusError, errMsg, time.Now(), walletID)
	if err != nil {
		return fmt.Errorf("failed to set sync error: %w", err)
	}
	if result.RowsAffected() == 0 {
		return wallet.ErrWalletNotFound
	}
	return nil
}

// SetSyncPhase updates the wallet's sync phase
func (r *WalletRepository) SetSyncPhase(ctx context.Context, walletID uuid.UUID, phase string) error {
	query := `
		UPDATE wallets
		SET sync_phase = $1, updated_at = $2
		WHERE id = $3
	`
	result, err := r.pool.Exec(ctx, query, phase, time.Now(), walletID)
	if err != nil {
		return fmt.Errorf("failed to set sync phase: %w", err)
	}
	if result.RowsAffected() == 0 {
		return wallet.ErrWalletNotFound
	}
	return nil
}

// SetCollectCursor updates the wallet's collect cursor timestamp
func (r *WalletRepository) SetCollectCursor(ctx context.Context, walletID uuid.UUID, cursor time.Time) error {
	query := `
		UPDATE wallets
		SET collect_cursor_at = $1, updated_at = $2
		WHERE id = $3
	`
	result, err := r.pool.Exec(ctx, query, cursor, time.Now(), walletID)
	if err != nil {
		return fmt.Errorf("failed to set collect cursor: %w", err)
	}
	if result.RowsAffected() == 0 {
		return wallet.ErrWalletNotFound
	}
	return nil
}

// WipeWalletLedger calls the wipe_wallet_ledger function to reset ledger data for replay
func (r *WalletRepository) WipeWalletLedger(ctx context.Context, walletID uuid.UUID) error {
	query := `SELECT wipe_wallet_ledger($1)`
	_, err := r.pool.Exec(ctx, query, walletID)
	if err != nil {
		return fmt.Errorf("failed to wipe wallet ledger: %w", err)
	}
	return nil
}

// GetChainSyncRows returns the wallet's per-chain sync-state rows (the wallet
// chain set), ordered by chain for deterministic fan-out.
func (r *WalletRepository) GetChainSyncRows(ctx context.Context, walletID uuid.UUID) ([]wallet.WalletChainSync, error) {
	query := `
		SELECT wallet_id, chain, sync_status, sync_error, sync_phase, collect_cursor_at, last_sync_at, created_at, updated_at
		FROM wallet_chain_sync
		WHERE wallet_id = $1
		ORDER BY chain
	`
	rows, err := r.pool.Query(ctx, query, walletID)
	if err != nil {
		return nil, fmt.Errorf("failed to query chain sync rows: %w", err)
	}
	defer rows.Close()

	var result []wallet.WalletChainSync
	for rows.Next() {
		var c wallet.WalletChainSync
		if err := rows.Scan(
			&c.WalletID,
			&c.Chain,
			&c.SyncStatus,
			&c.SyncError,
			&c.SyncPhase,
			&c.CollectCursorAt,
			&c.LastSyncAt,
			&c.CreatedAt,
			&c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan chain sync row: %w", err)
		}
		result = append(result, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating chain sync rows: %w", err)
	}
	return result, nil
}

// SetChainCollectCursor updates a single (wallet, chain) row's collect cursor.
func (r *WalletRepository) SetChainCollectCursor(ctx context.Context, walletID uuid.UUID, chain string, cursor time.Time) error {
	query := `
		UPDATE wallet_chain_sync
		SET collect_cursor_at = $1, updated_at = now()
		WHERE wallet_id = $2 AND chain = $3
	`
	result, err := r.pool.Exec(ctx, query, cursor, walletID, chain)
	if err != nil {
		return fmt.Errorf("failed to set chain collect cursor: %w", err)
	}
	if result.RowsAffected() == 0 {
		return wallet.ErrWalletNotFound
	}
	return nil
}

// SetChainSyncError marks a single (wallet, chain) row as errored WITHOUT touching
// its collect cursor, so the failed chain resumes from where it left off next
// cycle. Sibling chains are unaffected (issue #28 failure isolation).
func (r *WalletRepository) SetChainSyncError(ctx context.Context, walletID uuid.UUID, chain, errMsg string) error {
	query := `
		UPDATE wallet_chain_sync
		SET sync_status = $1, sync_error = $2, updated_at = now()
		WHERE wallet_id = $3 AND chain = $4
	`
	result, err := r.pool.Exec(ctx, query, wallet.SyncStatusError, errMsg, walletID, chain)
	if err != nil {
		return fmt.Errorf("failed to set chain sync error: %w", err)
	}
	if result.RowsAffected() == 0 {
		return wallet.ErrWalletNotFound
	}
	return nil
}

// SetChainSyncCompleted marks a single (wallet, chain) row as synced at syncAt,
// clearing its error and returning its phase to idle (issue #28).
func (r *WalletRepository) SetChainSyncCompleted(ctx context.Context, walletID uuid.UUID, chain string, syncAt time.Time) error {
	query := `
		UPDATE wallet_chain_sync
		SET sync_status = $1, last_sync_at = $2, sync_error = NULL, sync_phase = 'idle', updated_at = now()
		WHERE wallet_id = $3 AND chain = $4
	`
	result, err := r.pool.Exec(ctx, query, wallet.SyncStatusSynced, syncAt, walletID, chain)
	if err != nil {
		return fmt.Errorf("failed to set chain sync completed: %w", err)
	}
	if result.RowsAffected() == 0 {
		return wallet.ErrWalletNotFound
	}
	return nil
}

// RollupWalletSyncStatus derives the wallet-level sync_status from its per-chain
// rows and writes it to the wallets row. The status is the wallet.RollupStatus
// fold (error if any chain errored, else syncing, else synced, else pending); the
// wallet-level sync_error is set to the first errored chain's message, or cleared
// when no chain is in error. This keeps wallets.sync_status a true rollup now that
// chains advance independently (issue #28).
func (r *WalletRepository) RollupWalletSyncStatus(ctx context.Context, walletID uuid.UUID) error {
	rows, err := r.GetChainSyncRows(ctx, walletID)
	if err != nil {
		return fmt.Errorf("failed to load chain rows for rollup: %w", err)
	}

	status := wallet.RollupStatus(rows)
	syncErr := wallet.RollupError(rows)

	result, err := r.pool.Exec(ctx, `
		UPDATE wallets
		SET sync_status = $1, sync_error = $2, updated_at = now()
		WHERE id = $3
	`, status, syncErr, walletID)
	if err != nil {
		return fmt.Errorf("failed to write rolled-up sync status: %w", err)
	}
	if result.RowsAffected() == 0 {
		return wallet.ErrWalletNotFound
	}
	return nil
}
