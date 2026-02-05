package wallet

import (
	"time"

	"github.com/google/uuid"
)

// Wallet represents a collection of assets on a specific blockchain network
type Wallet struct {
	ID        uuid.UUID `json:"id" db:"id"`
	UserID    uuid.UUID `json:"user_id" db:"user_id"`
	Name      string    `json:"name" db:"name"`
	ChainID   string    `json:"chain_id" db:"chain_id"`
	Address   *string   `json:"address,omitempty" db:"address"` // Optional blockchain address
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
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

	if !isValidChainID(w.ChainID) {
		return ErrInvalidChainID
	}

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

	if w.ChainID != "" && !isValidChainID(w.ChainID) {
		return ErrInvalidChainID
	}

	return nil
}

// Supported blockchain networks
var supportedChains = map[string]bool{
	"ethereum":            true,
	"bitcoin":             true,
	"solana":              true,
	"polygon":             true,
	"binance-smart-chain": true,
	"arbitrum":            true,
	"optimism":            true,
	"avalanche":           true,
}

func isValidChainID(chainID string) bool {
	return supportedChains[chainID]
}
