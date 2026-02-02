package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/modules/wallet/domain"
)

// WalletRepository defines the interface for wallet data access
type WalletRepository interface {
	// Create creates a new wallet
	Create(ctx context.Context, wallet *domain.Wallet) error

	// GetByID retrieves a wallet by ID
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Wallet, error)

	// GetByUserID retrieves all wallets for a user
	GetByUserID(ctx context.Context, userID uuid.UUID) ([]*domain.Wallet, error)

	// Update updates an existing wallet
	Update(ctx context.Context, wallet *domain.Wallet) error

	// Delete deletes a wallet by ID
	Delete(ctx context.Context, id uuid.UUID) error

	// ExistsByUserAndName checks if a wallet with the given name exists for the user
	ExistsByUserAndName(ctx context.Context, userID uuid.UUID, name string) (bool, error)
}
