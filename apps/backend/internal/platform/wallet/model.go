package wallet

import (
	"time"

	"github.com/google/uuid"
)

// SyncStatus represents the synchronization status of a wallet
type SyncStatus string

const (
	SyncStatusPending SyncStatus = "pending" // Not yet synced
	SyncStatusSyncing SyncStatus = "syncing" // Currently syncing
	SyncStatusSynced  SyncStatus = "synced"  // Successfully synced
	SyncStatusError   SyncStatus = "error"   // Sync failed
)

// IsValid checks if the sync status is valid
func (s SyncStatus) IsValid() bool {
	switch s {
	case SyncStatusPending, SyncStatusSyncing, SyncStatusSynced, SyncStatusError:
		return true
	}
	return false
}

// Wallet represents an EVM blockchain wallet for tracking crypto assets
type Wallet struct {
	ID            uuid.UUID  `json:"id" db:"id"`
	UserID        uuid.UUID  `json:"user_id" db:"user_id"`
	Name          string     `json:"name" db:"name"`
	ChainID       int64      `json:"chain_id" db:"chain_id"`         // EVM chain ID (1=ETH, 137=Polygon, etc.)
	Address       string     `json:"address" db:"address"`           // Required EVM address (0x...)
	SyncStatus    SyncStatus `json:"sync_status" db:"sync_status"`   // Sync state
	LastSyncBlock *int64     `json:"last_sync_block" db:"last_sync_block"`
	LastSyncAt    *time.Time `json:"last_sync_at" db:"last_sync_at"`
	SyncError     *string    `json:"sync_error,omitempty" db:"sync_error"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at" db:"updated_at"`
}

// ValidateCreate validates wallet fields for creation
func (w *Wallet) ValidateCreate() error {
	if w.UserID == uuid.Nil {
		return ErrInvalidUserID
	}

	if w.Name == "" {
		return ErrMissingWalletName
	}

	if len(w.Name) > 100 {
		return ErrWalletNameTooLong
	}

	if !IsValidEVMChainID(w.ChainID) {
		return ErrInvalidChainID
	}

	// Validate EVM address (required)
	checksumAddr, err := ValidateEVMAddress(w.Address)
	if err != nil {
		return err
	}
	w.Address = checksumAddr

	return nil
}

// ValidateUpdate validates wallet fields for updates
func (w *Wallet) ValidateUpdate() error {
	if w.ID == uuid.Nil {
		return ErrInvalidWalletID
	}

	if w.Name == "" {
		return ErrMissingWalletName
	}

	if len(w.Name) > 100 {
		return ErrWalletNameTooLong
	}

	if w.ChainID != 0 && !IsValidEVMChainID(w.ChainID) {
		return ErrInvalidChainID
	}

	return nil
}

// NeedsSyncing returns true if the wallet should be synced
func (w *Wallet) NeedsSyncing() bool {
	return w.SyncStatus == SyncStatusPending || w.SyncStatus == SyncStatusError
}

// Supported EVM chain IDs
var supportedEVMChains = map[int64]string{
	1:     "Ethereum Mainnet",
	137:   "Polygon",
	42161: "Arbitrum One",
	10:    "Optimism",
	8453:  "Base",
	43114: "Avalanche C-Chain",
	56:    "BNB Smart Chain",
}

// IsValidEVMChainID checks if the chain ID is supported
func IsValidEVMChainID(chainID int64) bool {
	_, ok := supportedEVMChains[chainID]
	return ok
}

// GetChainName returns the human-readable name for a chain ID
func GetChainName(chainID int64) string {
	if name, ok := supportedEVMChains[chainID]; ok {
		return name
	}
	return "Unknown Chain"
}

// GetSupportedChains returns all supported chain IDs
func GetSupportedChains() []int64 {
	chains := make([]int64, 0, len(supportedEVMChains))
	for chainID := range supportedEVMChains {
		chains = append(chains, chainID)
	}
	return chains
}
