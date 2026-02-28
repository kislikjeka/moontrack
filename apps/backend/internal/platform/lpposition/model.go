package lpposition

import (
	"math/big"
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusOpen   Status = "open"
	StatusClosed Status = "closed"
)

type LPPosition struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	WalletID        uuid.UUID
	ChainID         string
	Protocol        string
	NFTTokenID      string // Uniswap V3 position NFT ID, empty if unknown
	ContractAddress string

	Token0Symbol   string
	Token1Symbol   string
	Token0Contract string
	Token1Contract string
	Token0Decimals int
	Token1Decimals int

	TotalDepositedUSD   *big.Int
	TotalWithdrawnUSD   *big.Int
	TotalClaimedFeesUSD *big.Int

	TotalDepositedToken0 *big.Int
	TotalDepositedToken1 *big.Int
	TotalWithdrawnToken0 *big.Int
	TotalWithdrawnToken1 *big.Int
	TotalClaimedToken0   *big.Int
	TotalClaimedToken1   *big.Int

	Status   Status
	OpenedAt time.Time
	ClosedAt *time.Time

	RealizedPnLUSD *big.Int
	APRBps         *int // basis points, nil if not calculated

	CreatedAt time.Time
	UpdatedAt time.Time
}

// RemainingToken0 returns deposited - withdrawn for token0.
// May be negative due to impermanent loss.
func (p *LPPosition) RemainingToken0() *big.Int {
	return new(big.Int).Sub(p.TotalDepositedToken0, p.TotalWithdrawnToken0)
}

// RemainingToken1 returns deposited - withdrawn for token1.
func (p *LPPosition) RemainingToken1() *big.Int {
	return new(big.Int).Sub(p.TotalDepositedToken1, p.TotalWithdrawnToken1)
}

// IsFullyWithdrawn returns true if both token remainders are <= 0.
func (p *LPPosition) IsFullyWithdrawn() bool {
	return p.RemainingToken0().Sign() <= 0 && p.RemainingToken1().Sign() <= 0
}
