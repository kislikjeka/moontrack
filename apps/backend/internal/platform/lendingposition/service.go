package lendingposition

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
		logger: log.WithField("component", "lendingposition"),
	}
}

// FindOrCreate looks up an active lending position by wallet+protocol+chain+asset,
// or creates a new one.
func (s *Service) FindOrCreate(
	ctx context.Context,
	userID, walletID uuid.UUID,
	protocol, chainID, supplyAsset string,
	supplyDecimals int, supplyContract string,
	openedAt time.Time,
) (*LendingPosition, error) {
	existing, err := s.repo.FindActiveByWalletAndAsset(ctx, walletID, protocol, chainID, supplyAsset, "")
	if err != nil {
		return nil, fmt.Errorf("find active position: %w", err)
	}
	if existing != nil {
		return existing, nil
	}

	pos := &LendingPosition{
		ID:       uuid.New(),
		UserID:   userID,
		WalletID: walletID,
		ChainID:  chainID,
		Protocol: protocol,

		SupplyAsset:    supplyAsset,
		SupplyAmount:   big.NewInt(0),
		SupplyDecimals: supplyDecimals,
		SupplyContract: supplyContract,

		BorrowAmount: big.NewInt(0),

		TotalSupplied:  big.NewInt(0),
		TotalWithdrawn: big.NewInt(0),
		TotalBorrowed:  big.NewInt(0),
		TotalRepaid:    big.NewInt(0),

		TotalSuppliedUSD:  big.NewInt(0),
		TotalWithdrawnUSD: big.NewInt(0),
		TotalBorrowedUSD:  big.NewInt(0),
		TotalRepaidUSD:    big.NewInt(0),

		InterestEarnedUSD: big.NewInt(0),

		Status:   StatusActive,
		OpenedAt: openedAt,

		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	if err := s.repo.Create(ctx, pos); err != nil {
		return nil, fmt.Errorf("create position: %w", err)
	}

	s.logger.Info("lending position created",
		"position_id", pos.ID,
		"protocol", protocol,
		"supply_asset", supplyAsset,
	)

	return pos, nil
}

// RecordSupply adds to supply totals and current supply balance.
func (s *Service) RecordSupply(ctx context.Context, positionID uuid.UUID, amount, usdValue *big.Int) error {
	pos, err := s.getPosition(ctx, positionID)
	if err != nil {
		return err
	}

	pos.SupplyAmount.Add(pos.SupplyAmount, amount)
	pos.TotalSupplied.Add(pos.TotalSupplied, amount)
	pos.TotalSuppliedUSD.Add(pos.TotalSuppliedUSD, usdValue)
	pos.UpdatedAt = time.Now().UTC()

	return s.repo.Update(ctx, pos)
}

// RecordWithdraw subtracts from supply balance. May close position.
func (s *Service) RecordWithdraw(ctx context.Context, positionID uuid.UUID, amount, usdValue *big.Int) error {
	pos, err := s.getPosition(ctx, positionID)
	if err != nil {
		return err
	}

	pos.SupplyAmount.Sub(pos.SupplyAmount, amount)
	pos.TotalWithdrawn.Add(pos.TotalWithdrawn, amount)
	pos.TotalWithdrawnUSD.Add(pos.TotalWithdrawnUSD, usdValue)
	pos.UpdatedAt = time.Now().UTC()

	if pos.ShouldClose() {
		s.closePosition(pos)
	}

	return s.repo.Update(ctx, pos)
}

// RecordBorrow sets borrow asset info and adds to borrow totals.
func (s *Service) RecordBorrow(
	ctx context.Context,
	positionID uuid.UUID,
	borrowAsset string, borrowDecimals int, borrowContract string,
	amount, usdValue *big.Int,
) error {
	pos, err := s.getPosition(ctx, positionID)
	if err != nil {
		return err
	}

	pos.BorrowAsset = borrowAsset
	pos.BorrowDecimals = borrowDecimals
	pos.BorrowContract = borrowContract
	pos.BorrowAmount.Add(pos.BorrowAmount, amount)
	pos.TotalBorrowed.Add(pos.TotalBorrowed, amount)
	pos.TotalBorrowedUSD.Add(pos.TotalBorrowedUSD, usdValue)
	pos.UpdatedAt = time.Now().UTC()

	return s.repo.Update(ctx, pos)
}

// RecordRepay subtracts from borrow balance. May close position.
func (s *Service) RecordRepay(ctx context.Context, positionID uuid.UUID, amount, usdValue *big.Int) error {
	pos, err := s.getPosition(ctx, positionID)
	if err != nil {
		return err
	}

	pos.BorrowAmount.Sub(pos.BorrowAmount, amount)
	pos.TotalRepaid.Add(pos.TotalRepaid, amount)
	pos.TotalRepaidUSD.Add(pos.TotalRepaidUSD, usdValue)
	pos.UpdatedAt = time.Now().UTC()

	if pos.ShouldClose() {
		s.closePosition(pos)
	}

	return s.repo.Update(ctx, pos)
}

// RecordClaim adds to interest earned.
func (s *Service) RecordClaim(ctx context.Context, positionID uuid.UUID, usdValue *big.Int) error {
	pos, err := s.getPosition(ctx, positionID)
	if err != nil {
		return err
	}

	pos.InterestEarnedUSD.Add(pos.InterestEarnedUSD, usdValue)
	pos.UpdatedAt = time.Now().UTC()

	return s.repo.Update(ctx, pos)
}

// GetByID returns a position by ID.
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (*LendingPosition, error) {
	return s.repo.GetByID(ctx, id)
}

// ListByUser returns positions for a user, with optional filters.
func (s *Service) ListByUser(ctx context.Context, userID uuid.UUID, status *Status, walletID *uuid.UUID, chainID *string) ([]*LendingPosition, error) {
	return s.repo.ListByUser(ctx, userID, status, walletID, chainID)
}

func (s *Service) getPosition(ctx context.Context, id uuid.UUID) (*LendingPosition, error) {
	pos, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get position: %w", err)
	}
	if pos == nil {
		return nil, fmt.Errorf("position not found: %s", id)
	}
	return pos, nil
}

func (s *Service) closePosition(pos *LendingPosition) {
	now := time.Now().UTC()
	pos.Status = StatusClosed
	pos.ClosedAt = &now

	s.logger.Info("lending position closed",
		"position_id", pos.ID,
		"interest_earned_usd", pos.InterestEarnedUSD,
	)
}
