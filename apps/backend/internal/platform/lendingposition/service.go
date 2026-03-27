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

// FindOrCreate looks up an active lending position by wallet+protocol+chain,
// or creates a new one.
func (s *Service) FindOrCreate(
	ctx context.Context,
	userID, walletID uuid.UUID,
	protocol, chainID string,
	openedAt time.Time,
) (*LendingPosition, error) {
	existing, err := s.repo.FindActiveByWalletProtocolChain(ctx, walletID, protocol, chainID)
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

		InterestEarnedUSD: big.NewInt(0),

		Assets: nil,

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
		"chain_id", chainID,
	)

	return pos, nil
}

// RecordSupply adds to supply totals and current supply balance for a specific asset.
func (s *Service) RecordSupply(
	ctx context.Context,
	positionID uuid.UUID,
	asset string, decimals int, contract string,
	amount, usdValue *big.Int,
) error {
	pos, err := s.getPosition(ctx, positionID)
	if err != nil {
		return err
	}

	a := pos.FindAsset("supply", asset)
	if a == nil {
		newAsset := LendingPositionAsset{
			ID:          uuid.New(),
			PositionID:  positionID,
			Side:        "supply",
			Asset:       asset,
			Amount:      new(big.Int).Set(amount),
			Decimals:    decimals,
			Contract:    contract,
			TotalIn:     new(big.Int).Set(amount),
			TotalOut:    big.NewInt(0),
			TotalInUSD:  new(big.Int).Set(usdValue),
			TotalOutUSD: big.NewInt(0),
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		}
		pos.Assets = append(pos.Assets, newAsset)
		a = &pos.Assets[len(pos.Assets)-1]
	} else {
		a.Amount.Add(a.Amount, amount)
		a.TotalIn.Add(a.TotalIn, amount)
		a.TotalInUSD.Add(a.TotalInUSD, usdValue)
		a.UpdatedAt = time.Now().UTC()
	}

	return s.repo.UpsertAsset(ctx, a)
}

// RecordWithdraw subtracts from supply balance for a specific asset. May close position.
func (s *Service) RecordWithdraw(ctx context.Context, positionID uuid.UUID, asset string, amount, usdValue *big.Int) error {
	pos, err := s.getPosition(ctx, positionID)
	if err != nil {
		return err
	}

	a := pos.FindAsset("supply", asset)
	if a == nil {
		return fmt.Errorf("supply asset %s not found on position %s", asset, positionID)
	}

	a.Amount.Sub(a.Amount, amount)
	a.TotalOut.Add(a.TotalOut, amount)
	a.TotalOutUSD.Add(a.TotalOutUSD, usdValue)
	a.UpdatedAt = time.Now().UTC()

	if err := s.repo.UpsertAsset(ctx, a); err != nil {
		return err
	}

	if pos.ShouldClose() {
		s.closePosition(pos)
		return s.repo.Update(ctx, pos)
	}

	return nil
}

// RecordBorrow adds to borrow totals and current borrow balance for a specific asset.
func (s *Service) RecordBorrow(
	ctx context.Context,
	positionID uuid.UUID,
	asset string, decimals int, contract string,
	amount, usdValue *big.Int,
) error {
	pos, err := s.getPosition(ctx, positionID)
	if err != nil {
		return err
	}

	a := pos.FindAsset("borrow", asset)
	if a == nil {
		newAsset := LendingPositionAsset{
			ID:          uuid.New(),
			PositionID:  positionID,
			Side:        "borrow",
			Asset:       asset,
			Amount:      new(big.Int).Set(amount),
			Decimals:    decimals,
			Contract:    contract,
			TotalIn:     new(big.Int).Set(amount),
			TotalOut:    big.NewInt(0),
			TotalInUSD:  new(big.Int).Set(usdValue),
			TotalOutUSD: big.NewInt(0),
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		}
		pos.Assets = append(pos.Assets, newAsset)
		a = &pos.Assets[len(pos.Assets)-1]
	} else {
		a.Amount.Add(a.Amount, amount)
		a.TotalIn.Add(a.TotalIn, amount)
		a.TotalInUSD.Add(a.TotalInUSD, usdValue)
		a.UpdatedAt = time.Now().UTC()
	}

	return s.repo.UpsertAsset(ctx, a)
}

// RecordRepay subtracts from borrow balance for a specific asset. May close position.
func (s *Service) RecordRepay(ctx context.Context, positionID uuid.UUID, asset string, amount, usdValue *big.Int) error {
	pos, err := s.getPosition(ctx, positionID)
	if err != nil {
		return err
	}

	a := pos.FindAsset("borrow", asset)
	if a == nil {
		return fmt.Errorf("borrow asset %s not found on position %s", asset, positionID)
	}

	a.Amount.Sub(a.Amount, amount)
	a.TotalOut.Add(a.TotalOut, amount)
	a.TotalOutUSD.Add(a.TotalOutUSD, usdValue)
	a.UpdatedAt = time.Now().UTC()

	if err := s.repo.UpsertAsset(ctx, a); err != nil {
		return err
	}

	if pos.ShouldClose() {
		s.closePosition(pos)
		return s.repo.Update(ctx, pos)
	}

	return nil
}

// RecordClaim adds to interest earned at the position level.
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
