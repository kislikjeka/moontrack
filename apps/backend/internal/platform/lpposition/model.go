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

// IsFullyWithdrawn returns true if the position has been fully withdrawn.
// Handles impermanent loss where one token may be slightly over-withdrawn
// (negative remainder) while the other is slightly under-withdrawn (positive).
func (p *LPPosition) IsFullyWithdrawn() bool {
	remaining0 := p.RemainingToken0()
	remaining1 := p.RemainingToken1()

	// Simple case: both tokens fully withdrawn.
	if remaining0.Sign() <= 0 && remaining1.Sign() <= 0 {
		return true
	}

	// Impermanent loss case: net remaining <= 0 means more withdrawn than deposited overall.
	net := new(big.Int).Add(remaining0, remaining1)
	return net.Sign() <= 0
}
