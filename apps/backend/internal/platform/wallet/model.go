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

// WalletChainSync is the per-(wallet, chain) sync-state row. The set of these
// rows for a wallet IS the wallet's chain set: sync and reconciliation iterate
// exactly these chains. Each chain carries its own sync bookkeeping so chains can
// (eventually, per issue #28) advance independently. Stored in wallet_chain_sync.
type WalletChainSync struct {
	WalletID        uuid.UUID  `json:"wallet_id" db:"wallet_id"`
	Chain           string     `json:"chain" db:"chain"` // canonical domain slug (e.g. "ethereum")
	SyncStatus      SyncStatus `json:"sync_status" db:"sync_status"`
	SyncError       *string    `json:"sync_error,omitempty" db:"sync_error"`
	SyncPhase       string     `json:"sync_phase" db:"sync_phase"`
	CollectCursorAt *time.Time `json:"collect_cursor_at,omitempty" db:"collect_cursor_at"`
	LastSyncAt      *time.Time `json:"last_sync_at,omitempty" db:"last_sync_at"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`
}

// RollupStatus derives a wallet-level sync status from its per-chain rows. The
// rollup is a strict severity fold: error if ANY chain errored; else syncing if
// ANY chain is syncing; else synced if ANY chain is synced; else pending. An
// empty set (a wallet with no chain rows) rolls up to pending — there is nothing
// to have synced yet.
func RollupStatus(rows []WalletChainSync) SyncStatus {
	if len(rows) == 0 {
		return SyncStatusPending
	}
	anySyncing, anySynced := false, false
	for _, r := range rows {
		switch r.SyncStatus {
		case SyncStatusError:
			return SyncStatusError
		case SyncStatusSyncing:
			anySyncing = true
		case SyncStatusSynced:
			anySynced = true
		}
	}
	switch {
	case anySyncing:
		return SyncStatusSyncing
	case anySynced:
		return SyncStatusSynced
	default:
		return SyncStatusPending
	}
}

// RollupError returns the wallet-level sync_error derived from the chain rows: the
// first errored chain's message (rows are iterated in the order given), or nil if
// no chain is in error. Companion to RollupStatus so the rollup semantics live in
// one place in the domain rather than being re-derived by the persistence layer.
func RollupError(rows []WalletChainSync) *string {
	for _, r := range rows {
		if r.SyncStatus == SyncStatusError && r.SyncError != nil {
			return r.SyncError
		}
	}
	return nil
}

// Wallet represents an EVM blockchain wallet for tracking crypto assets
type Wallet struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	UserID          uuid.UUID  `json:"user_id" db:"user_id"`
	Name            string     `json:"name" db:"name"`
	Address         string     `json:"address" db:"address"`         // Required EVM address (0x...)
	SyncStatus      SyncStatus `json:"sync_status" db:"sync_status"` // Sync state
	LastSyncAt      *time.Time `json:"last_sync_at" db:"last_sync_at"`
	SyncError       *string    `json:"sync_error,omitempty" db:"sync_error"`
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

// supportedEVMChains is the Enabled chain set: the chains actually polled during
// sync, keyed by the canonical domain chain slug (provider-neutral; the Noves
// adapter maps these to its own short slugs via chains.go). Per issue #23 the
// initial Enabled set is Ethereum, Base and Arbitrum. The adapter remains
// Compatible with more chains (see noves/chains.go), but only these are synced.
//
// As of issue #27 a wallet's *actual* synced chains live in wallet_chain_sync
// (the wallet chain set); this map is the DEFAULT stamped on a new wallet and the
// upper bound of what IsValidChain accepts.
var supportedEVMChains = map[string]string{
	"ethereum": "Ethereum",
	"base":     "Base",
	"arbitrum": "Arbitrum One",
}

// EnabledChains returns the default Enabled chain set: the chains a newly created
// wallet is seeded with in wallet_chain_sync. Identical membership to
// GetSupportedChains today; the two are kept distinct because "the chains a new
// wallet defaults to" (Enabled) and "the chains the system can handle at all"
// (Compatible/supported) are separate concepts per CONTEXT.md, expected to
// diverge as more Compatible-but-not-Enabled chains are added.
func EnabledChains() []string {
	return GetSupportedChains()
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
