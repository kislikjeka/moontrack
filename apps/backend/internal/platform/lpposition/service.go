package lpposition

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/pkg/logger"
)

type Service struct {
	repo   Repository
	logger *logger.Logger
}

func NewService(repo Repository, log *logger.Logger) *Service {
	return &Service{
		repo:   repo,
		logger: log.WithField("component", "lpposition"),
	}
}

type TokenInfo struct {
	Symbol   string
	Contract string
	Decimals int
}

// FindOrCreate looks up an LP position by NFT token ID, or creates a new one.
func (s *Service) FindOrCreate(ctx context.Context, userID, walletID uuid.UUID, chainID, protocol, nftTokenID, contractAddress string, token0, token1 TokenInfo, openedAt time.Time) (*LPPosition, error) {
	if nftTokenID != "" {
		pos, err := s.repo.GetByNFTTokenID(ctx, walletID, chainID, nftTokenID)
		if err != nil {
			return nil, fmt.Errorf("find by nft: %w", err)
		}
		if pos != nil {
			return pos, nil
		}
	}

	pos := &LPPosition{
		ID:              uuid.New(),
		UserID:          userID,
		WalletID:        walletID,
		ChainID:         chainID,
		Protocol:        protocol,
		NFTTokenID:      nftTokenID,
		ContractAddress: contractAddress,

		Token0Symbol:   token0.Symbol,
		Token1Symbol:   token1.Symbol,
		Token0Contract: token0.Contract,
		Token0Decimals: token0.Decimals,
		Token1Contract: token1.Contract,
		Token1Decimals: token1.Decimals,

		TotalDepositedUSD:    big.NewInt(0),
		TotalWithdrawnUSD:    big.NewInt(0),
		TotalClaimedFeesUSD:  big.NewInt(0),
		TotalDepositedToken0: big.NewInt(0),
		TotalDepositedToken1: big.NewInt(0),
		TotalWithdrawnToken0: big.NewInt(0),
		TotalWithdrawnToken1: big.NewInt(0),
		TotalClaimedToken0:   big.NewInt(0),
		TotalClaimedToken1:   big.NewInt(0),

		Status:   StatusOpen,
		OpenedAt: openedAt,

		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	if err := s.repo.Create(ctx, pos); err != nil {
		return nil, fmt.Errorf("create position: %w", err)
	}

	s.logger.Info("LP position created",
		"position_id", pos.ID,
		"nft_token_id", nftTokenID,
		"token0", token0.Symbol,
		"token1", token1.Symbol,
	)

	return pos, nil
}

// FindOpenByTokenPair finds an open LP position by token pair (heuristic for withdraw/claim).
// Returns the oldest open position if multiple found.
func (s *Service) FindOpenByTokenPair(ctx context.Context, walletID uuid.UUID, chainID, protocol, token0, token1 string) (*LPPosition, error) {
	positions, err := s.repo.FindOpenByTokenPair(ctx, walletID, chainID, protocol, token0, token1)
	if err != nil {
		return nil, fmt.Errorf("find by token pair: %w", err)
	}

	if len(positions) == 0 {
		return nil, nil
	}

	if len(positions) > 1 {
		s.logger.Warn("multiple open LP positions for same token pair, using oldest",
			"wallet_id", walletID,
			"chain_id", chainID,
			"token0", token0,
			"token1", token1,
			"count", len(positions),
		)
	}

	return positions[0], nil // oldest first (repo sorts by opened_at ASC)
}

// RecordDeposit updates aggregates after a deposit.
func (s *Service) RecordDeposit(ctx context.Context, positionID uuid.UUID, token0Amt, token1Amt, usdValue *big.Int) error {
	pos, err := s.repo.GetByID(ctx, positionID)
	if err != nil {
		return fmt.Errorf("get position: %w", err)
	}
	if pos == nil {
		return fmt.Errorf("position not found: %s", positionID)
	}

	pos.TotalDepositedUSD.Add(pos.TotalDepositedUSD, usdValue)
	pos.TotalDepositedToken0.Add(pos.TotalDepositedToken0, token0Amt)
	pos.TotalDepositedToken1.Add(pos.TotalDepositedToken1, token1Amt)
	pos.UpdatedAt = time.Now().UTC()

	return s.repo.Update(ctx, pos)
}

// RecordWithdraw updates aggregates after a withdrawal. Closes position if fully withdrawn.
func (s *Service) RecordWithdraw(ctx context.Context, positionID uuid.UUID, token0Amt, token1Amt, usdValue *big.Int) error {
	pos, err := s.repo.GetByID(ctx, positionID)
	if err != nil {
		return fmt.Errorf("get position: %w", err)
	}
	if pos == nil {
		return fmt.Errorf("position not found: %s", positionID)
	}

	pos.TotalWithdrawnUSD.Add(pos.TotalWithdrawnUSD, usdValue)
	pos.TotalWithdrawnToken0.Add(pos.TotalWithdrawnToken0, token0Amt)
	pos.TotalWithdrawnToken1.Add(pos.TotalWithdrawnToken1, token1Amt)
	pos.UpdatedAt = time.Now().UTC()

	if pos.IsFullyWithdrawn() {
		s.closePosition(pos)
	}

	return s.repo.Update(ctx, pos)
}

// RecordClaimFees updates aggregates after a fee claim.
func (s *Service) RecordClaimFees(ctx context.Context, positionID uuid.UUID, token0Amt, token1Amt, usdValue *big.Int) error {
	pos, err := s.repo.GetByID(ctx, positionID)
	if err != nil {
		return fmt.Errorf("get position: %w", err)
	}
	if pos == nil {
		return fmt.Errorf("position not found: %s", positionID)
	}

	pos.TotalClaimedFeesUSD.Add(pos.TotalClaimedFeesUSD, usdValue)
	pos.TotalClaimedToken0.Add(pos.TotalClaimedToken0, token0Amt)
	pos.TotalClaimedToken1.Add(pos.TotalClaimedToken1, token1Amt)
	pos.UpdatedAt = time.Now().UTC()

	if pos.IsFullyWithdrawn() && pos.Status != StatusClosed {
		s.closePosition(pos)
	}

	return s.repo.Update(ctx, pos)
}

func (s *Service) closePosition(pos *LPPosition) {
	now := time.Now().UTC()
	pos.Status = StatusClosed
	pos.ClosedAt = &now

	// realized PnL = withdrawn + claimed - deposited
	pnl := new(big.Int).Add(pos.TotalWithdrawnUSD, pos.TotalClaimedFeesUSD)
	pnl.Sub(pnl, pos.TotalDepositedUSD)
	pos.RealizedPnLUSD = pnl

	// APR in basis points = (pnl / deposited) / years * 10000
	if pos.TotalDepositedUSD.Sign() > 0 && pos.ClosedAt != nil {
		duration := pos.ClosedAt.Sub(pos.OpenedAt)
		if duration > 0 {
			// apr = pnl * 10000 * seconds_per_year / (deposited * duration_seconds)
			secondsPerYear := big.NewInt(365 * 24 * 3600)
			numerator := new(big.Int).Mul(pnl, big.NewInt(10000))
			numerator.Mul(numerator, secondsPerYear)
			denominator := new(big.Int).Mul(pos.TotalDepositedUSD, big.NewInt(int64(duration.Seconds())))
			if denominator.Sign() > 0 {
				apr := new(big.Int).Div(numerator, denominator)
				aprInt := int(apr.Int64())
				pos.APRBps = &aprInt
			}
		}
	}

	s.logger.Info("LP position closed",
		"position_id", pos.ID,
		"realized_pnl", pos.RealizedPnLUSD,
		"apr_bps", pos.APRBps,
	)
}

// GetByID returns a position by ID.
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (*LPPosition, error) {
	return s.repo.GetByID(ctx, id)
}

// ListByUser returns positions for a user, with optional filters.
func (s *Service) ListByUser(ctx context.Context, userID uuid.UUID, status *Status, walletID *uuid.UUID, chainID *string) ([]*LPPosition, error) {
	return s.repo.ListByUser(ctx, userID, status, walletID, chainID)
}
