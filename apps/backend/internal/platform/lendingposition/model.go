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

type LendingPosition struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	WalletID uuid.UUID
	ChainID  string
	Protocol string

	SupplyAsset    string
	SupplyAmount   *big.Int // current supply balance
	SupplyDecimals int
	SupplyContract string

	BorrowAsset    string
	BorrowAmount   *big.Int // current borrow balance
	BorrowDecimals int
	BorrowContract string

	TotalSupplied  *big.Int
	TotalWithdrawn *big.Int
	TotalBorrowed  *big.Int
	TotalRepaid    *big.Int

	TotalSuppliedUSD  *big.Int
	TotalWithdrawnUSD *big.Int
	TotalBorrowedUSD  *big.Int
	TotalRepaidUSD    *big.Int

	InterestEarnedUSD *big.Int

	Status   Status
	OpenedAt time.Time
	ClosedAt *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// ShouldClose returns true if both supply and borrow balances are zero or negative.
func (p *LendingPosition) ShouldClose() bool {
	return p.SupplyAmount.Sign() <= 0 && p.BorrowAmount.Sign() <= 0
}
