package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kislikjeka/moontrack/internal/modules/wallet/domain"
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
func (r *WalletRepository) Create(ctx context.Context, wallet *domain.Wallet) error {
	query := `
		INSERT INTO wallets (id, user_id, name, chain_id, address, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	now := time.Now()
	wallet.CreatedAt = now
	wallet.UpdatedAt = now

	if wallet.ID == uuid.Nil {
		wallet.ID = uuid.New()
	}

	_, err := r.pool.Exec(ctx, query,
		wallet.ID,
		wallet.UserID,
		wallet.Name,
		wallet.ChainID,
		wallet.Address,
		wallet.CreatedAt,
		wallet.UpdatedAt,
	)

	if err != nil {
		// Check for unique constraint violation
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrDuplicateWalletName
		}
		return fmt.Errorf("failed to create wallet: %w", err)
	}

	return nil
}

// GetByID retrieves a wallet by ID
func (r *WalletRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Wallet, error) {
	query := `
		SELECT id, user_id, name, chain_id, address, created_at, updated_at
		FROM wallets
		WHERE id = $1
	`

	wallet := &domain.Wallet{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&wallet.ID,
		&wallet.UserID,
		&wallet.Name,
		&wallet.ChainID,
		&wallet.Address,
		&wallet.CreatedAt,
		&wallet.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("failed to get wallet: %w", err)
	}

	return wallet, nil
}

// GetByUserID retrieves all wallets for a user
func (r *WalletRepository) GetByUserID(ctx context.Context, userID uuid.UUID) ([]*domain.Wallet, error) {
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

	var wallets []*domain.Wallet
	for rows.Next() {
		wallet := &domain.Wallet{}
		err := rows.Scan(
			&wallet.ID,
			&wallet.UserID,
			&wallet.Name,
			&wallet.ChainID,
			&wallet.Address,
			&wallet.CreatedAt,
			&wallet.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan wallet: %w", err)
		}
		wallets = append(wallets, wallet)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating wallets: %w", err)
	}

	return wallets, nil
}

// Update updates an existing wallet
func (r *WalletRepository) Update(ctx context.Context, wallet *domain.Wallet) error {
	query := `
		UPDATE wallets
		SET name = $1, chain_id = $2, address = $3, updated_at = $4
		WHERE id = $5
	`

	wallet.UpdatedAt = time.Now()

	result, err := r.pool.Exec(ctx, query,
		wallet.Name,
		wallet.ChainID,
		wallet.Address,
		wallet.UpdatedAt,
		wallet.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update wallet: %w", err)
	}

	if result.RowsAffected() == 0 {
		return domain.ErrWalletNotFound
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
		return domain.ErrWalletNotFound
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
