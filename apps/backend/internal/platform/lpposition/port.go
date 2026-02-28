package lpposition

import (
	"context"

	"github.com/google/uuid"
)

type Repository interface {
	Create(ctx context.Context, pos *LPPosition) error
	Update(ctx context.Context, pos *LPPosition) error
	GetByID(ctx context.Context, id uuid.UUID) (*LPPosition, error)
	GetByNFTTokenID(ctx context.Context, walletID uuid.UUID, chainID, nftTokenID string) (*LPPosition, error)
	FindOpenByTokenPair(ctx context.Context, walletID uuid.UUID, chainID, protocol, token0, token1 string) ([]*LPPosition, error)
	ListByUser(ctx context.Context, userID uuid.UUID, status *Status, walletID *uuid.UUID, chainID *string) ([]*LPPosition, error)
}
