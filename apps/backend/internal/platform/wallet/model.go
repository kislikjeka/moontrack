package wallet

import (
	"sort"
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
	Address       string     `json:"address" db:"address"`           // Required EVM address (0x...)
	SyncStatus    SyncStatus `json:"sync_status" db:"sync_status"`   // Sync state
	LastSyncAt    *time.Time `json:"last_sync_at" db:"last_sync_at"`
	SyncError     *string    `json:"sync_error,omitempty" db:"sync_error"`
	SyncStartedAt   *time.Time `json:"sync_started_at,omitempty" db:"sync_started_at"`
	SyncPhase       string     `json:"sync_phase" db:"sync_phase"`
	CollectCursorAt *time.Time `json:"collect_cursor_at,omitempty" db:"collect_cursor_at"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`
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

	return nil
}

// NeedsSyncing returns true if the wallet should be synced
func (w *Wallet) NeedsSyncing() bool {
	return w.SyncStatus == SyncStatusPending || w.SyncStatus == SyncStatusError
}

// Supported EVM chains keyed by Zerion chain name
var supportedEVMChains = map[string]string{
	"ethereum":            "Ethereum",
	"polygon":             "Polygon",
	"arbitrum":            "Arbitrum One",
	"optimism":            "Optimism",
	"base":                "Base",
	"avalanche":           "Avalanche",
	"binance-smart-chain": "BNB Smart Chain",
}

// IsValidChain checks if the chain is supported
func IsValidChain(chain string) bool {
	_, ok := supportedEVMChains[chain]
	return ok
}

// GetChainName returns the human-readable name for a chain
func GetChainName(chain string) string {
	if name, ok := supportedEVMChains[chain]; ok {
		return name
	}
	return "Unknown Chain"
}

// GetSupportedChains returns all supported chain keys
func GetSupportedChains() []string {
	chains := make([]string, 0, len(supportedEVMChains))
	for chain := range supportedEVMChains {
		chains = append(chains, chain)
	}
	sort.Strings(chains)
	return chains
}
