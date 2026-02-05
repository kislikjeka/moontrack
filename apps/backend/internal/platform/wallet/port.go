package wallet

import (
	"context"

	"github.com/google/uuid"
)

// Repository defines the interface for wallet data access
type Repository interface {
	// Create creates a new wallet
	Create(ctx context.Context, wallet *Wallet) error

	// GetByID retrieves a wallet by ID
	GetByID(ctx context.Context, id uuid.UUID) (*Wallet, error)

	// GetByUserID retrieves all wallets for a user
	GetByUserID(ctx context.Context, userID uuid.UUID) ([]*Wallet, error)

	// Update updates an existing wallet
	Update(ctx context.Context, wallet *Wallet) error

	// Delete deletes a wallet by ID
	Delete(ctx context.Context, id uuid.UUID) error

	// ExistsByUserAndName checks if a wallet with the given name exists for the user
	ExistsByUserAndName(ctx context.Context, userID uuid.UUID, name string) (bool, error)
}
