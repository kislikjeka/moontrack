package lendingposition

import (
	"math/big"
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusActive Status = "active"
	StatusClosed Status = "closed"
)

type LendingPositionAsset struct {
	ID         uuid.UUID
	PositionID uuid.UUID
	Side       string // "supply" | "borrow"
	Asset      string
	Amount     *big.Int
	Decimals   int
	Contract   string
	TotalIn    *big.Int
	TotalOut   *big.Int
	TotalInUSD  *big.Int
	TotalOutUSD *big.Int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type LendingPosition struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	WalletID uuid.UUID
	ChainID  string
	Protocol string

	InterestEarnedUSD *big.Int

	Assets []LendingPositionAsset

	Status   Status
	OpenedAt time.Time
	ClosedAt *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// ShouldClose returns true when ALL assets have Amount <= 0.
func (p *LendingPosition) ShouldClose() bool {
	if len(p.Assets) == 0 {
		return true
	}
	for i := range p.Assets {
		if p.Assets[i].Amount != nil && p.Assets[i].Amount.Sign() > 0 {
			return false
		}
	}
	return true
}

// SupplyAssets returns all assets with side == "supply".
func (p *LendingPosition) SupplyAssets() []LendingPositionAsset {
	var result []LendingPositionAsset
	for _, a := range p.Assets {
		if a.Side == "supply" {
			result = append(result, a)
		}
	}
	return result
}

// BorrowAssets returns all assets with side == "borrow".
func (p *LendingPosition) BorrowAssets() []LendingPositionAsset {
	var result []LendingPositionAsset
	for _, a := range p.Assets {
		if a.Side == "borrow" {
			result = append(result, a)
		}
	}
	return result
}

// FindAsset finds a specific asset entry by side and asset name. Returns nil if not found.
func (p *LendingPosition) FindAsset(side, asset string) *LendingPositionAsset {
	for i := range p.Assets {
		if p.Assets[i].Side == side && p.Assets[i].Asset == asset {
			return &p.Assets[i]
		}
	}
	return nil
}
