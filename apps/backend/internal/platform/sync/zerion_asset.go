package sync

import (
	"time"

	"github.com/google/uuid"
)

// ZerionAsset represents token metadata discovered from Zerion API
type ZerionAsset struct {
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
