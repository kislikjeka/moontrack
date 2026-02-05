package postgres

import (
	"context"
	"errors"
	"fmt"
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
		INSERT INTO wallets (id, user_id, name, chain_id, address, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	now := time.Now()
	w.CreatedAt = now
	w.UpdatedAt = now

	if w.ID == uuid.Nil {
		w.ID = uuid.New()
	}

	_, err := r.pool.Exec(ctx, query,
		w.ID,
		w.UserID,
		w.Name,
		w.ChainID,
		w.Address,
		w.CreatedAt,
		w.UpdatedAt,
	)

	if err != nil {
		// Check for unique constraint violation
		if errors.Is(err, pgx.ErrNoRows) {
			return wallet.ErrDuplicateWalletName
		}
		return fmt.Errorf("failed to create wallet: %w", err)
	}

	return nil
}

// GetByID retrieves a wallet by ID
func (r *WalletRepository) GetByID(ctx context.Context, id uuid.UUID) (*wallet.Wallet, error) {
	query := `
		SELECT id, user_id, name, chain_id, address, created_at, updated_at
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
		SELECT id, user_id, name, chain_id, address, created_at, updated_at
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
