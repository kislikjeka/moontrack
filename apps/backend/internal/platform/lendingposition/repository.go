package lendingposition

import (
	"context"

	"github.com/google/uuid"
)

type Repository interface {
	Create(ctx context.Context, pos *LendingPosition) error
	Update(ctx context.Context, pos *LendingPosition) error
	GetByID(ctx context.Context, id uuid.UUID) (*LendingPosition, error)
	FindActiveByWalletAndAsset(ctx context.Context, walletID uuid.UUID, protocol, chainID, supplyAsset, borrowAsset string) (*LendingPosition, error)
	ListByUser(ctx context.Context, userID uuid.UUID, status *Status, walletID *uuid.UUID, chainID *string) ([]*LendingPosition, error)
}
