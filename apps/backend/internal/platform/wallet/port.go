package wallet

import (
	"context"
	"time"

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

	// ExistsByUserChainAndAddress checks if a wallet with the given chain/address exists for the user
	ExistsByUserChainAndAddress(ctx context.Context, userID uuid.UUID, chainID int64, address string) (bool, error)

	// GetWalletsForSync retrieves wallets that need syncing (pending or error status)
	GetWalletsForSync(ctx context.Context) ([]*Wallet, error)

	// GetWalletsByAddressAndUserID retrieves wallets with a given address for a specific user
	GetWalletsByAddressAndUserID(ctx context.Context, address string, userID uuid.UUID) ([]*Wallet, error)

	// ClaimWalletForSync atomically claims a wallet for syncing (returns false if already syncing)
	ClaimWalletForSync(ctx context.Context, walletID uuid.UUID) (bool, error)

	// UpdateSyncState updates the sync status and related fields for a wallet
	UpdateSyncState(ctx context.Context, walletID uuid.UUID, status SyncStatus, lastBlock *int64, syncError *string) error

	// SetSyncInProgress marks a wallet as currently syncing
	SetSyncInProgress(ctx context.Context, walletID uuid.UUID) error

	// SetSyncCompleted marks a wallet sync as completed
	SetSyncCompleted(ctx context.Context, walletID uuid.UUID, lastBlock int64, syncAt time.Time) error

	// SetSyncCompletedAt marks a wallet sync as completed at a given time (without block number)
	SetSyncCompletedAt(ctx context.Context, walletID uuid.UUID, syncAt time.Time) error

	// SetSyncError marks a wallet sync as failed with an error message
	SetSyncError(ctx context.Context, walletID uuid.UUID, errMsg string) error
}
