package sync

import (
	"time"

	"github.com/google/uuid"
)

// SyncAsset represents token metadata discovered from the sync provider
type SyncAsset struct {
	ID              uuid.UUID `db:"id"`
	Symbol          string    `db:"symbol"`
	Name            string    `db:"name"`
	ChainID         string    `db:"chain_id"`
	ContractAddress string    `db:"contract_address"`
	Decimals        int       `db:"decimals"`
	IconURL         string    `db:"icon_url"`
	CreatedAt       time.Time `db:"created_at"`
	UpdatedAt       time.Time `db:"updated_at"`
}
