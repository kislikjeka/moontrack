package adapter

import (
	"context"

	"github.com/google/uuid"
	portfolioService "github.com/kislikjeka/moontrack/internal/modules/portfolio/service"
	walletDomain "github.com/kislikjeka/moontrack/internal/modules/wallet/domain"
)

// WalletRepositoryInterface is the interface for the wallet repository
type WalletRepositoryInterface interface {
	GetByUserID(ctx context.Context, userID uuid.UUID) ([]*walletDomain.Wallet, error)
}

// WalletRepositoryAdapter adapts the wallet repository to the portfolio service interface
type WalletRepositoryAdapter struct {
	repo WalletRepositoryInterface
}

// NewWalletRepositoryAdapter creates a new wallet repository adapter
func NewWalletRepositoryAdapter(repo WalletRepositoryInterface) *WalletRepositoryAdapter {
	return &WalletRepositoryAdapter{repo: repo}
}

// GetByUserID returns wallets for a user, converting to the portfolio service's Wallet type
func (a *WalletRepositoryAdapter) GetByUserID(ctx context.Context, userID uuid.UUID) ([]*portfolioService.Wallet, error) {
	wallets, err := a.repo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	result := make([]*portfolioService.Wallet, len(wallets))
	for i, w := range wallets {
		result[i] = &portfolioService.Wallet{
			ID:      w.ID,
			UserID:  w.UserID,
			Name:    w.Name,
			ChainID: w.ChainID,
		}
	}

	return result, nil
}
