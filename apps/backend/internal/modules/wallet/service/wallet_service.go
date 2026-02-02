package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/modules/wallet/domain"
	"github.com/kislikjeka/moontrack/internal/modules/wallet/repository"
)

// WalletService provides business logic for wallet operations
type WalletService struct {
	repo repository.WalletRepository
}

// NewWalletService creates a new wallet service
func NewWalletService(repo repository.WalletRepository) *WalletService {
	return &WalletService{repo: repo}
}

// Create creates a new wallet for a user
func (s *WalletService) Create(ctx context.Context, wallet *domain.Wallet) (*domain.Wallet, error) {
	// Validate wallet data
	if err := wallet.ValidateCreate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Check if wallet with same name already exists for user
	exists, err := s.repo.ExistsByUserAndName(ctx, wallet.UserID, wallet.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to check wallet existence: %w", err)
	}

	if exists {
		return nil, domain.ErrDuplicateWalletName
	}

	// Generate UUID for new wallet
	wallet.ID = uuid.New()

	// Create wallet
	if err := s.repo.Create(ctx, wallet); err != nil {
		return nil, fmt.Errorf("failed to create wallet: %w", err)
	}

	return wallet, nil
}

// GetByID retrieves a wallet by ID and validates user ownership
func (s *WalletService) GetByID(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*domain.Wallet, error) {
	wallet, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Verify wallet belongs to requesting user
	if wallet.UserID != userID {
		return nil, domain.ErrUnauthorizedAccess
	}

	return wallet, nil
}

// List retrieves all wallets for a user
func (s *WalletService) List(ctx context.Context, userID uuid.UUID) ([]*domain.Wallet, error) {
	wallets, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list wallets: %w", err)
	}

	return wallets, nil
}

// Update updates an existing wallet
func (s *WalletService) Update(ctx context.Context, wallet *domain.Wallet, userID uuid.UUID) (*domain.Wallet, error) {
	// Validate update data
	if err := wallet.ValidateUpdate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Get existing wallet to verify ownership
	existing, err := s.repo.GetByID(ctx, wallet.ID)
	if err != nil {
		return nil, err
	}

	if existing.UserID != userID {
		return nil, domain.ErrUnauthorizedAccess
	}

	// Check if new name conflicts with existing wallet
	if wallet.Name != existing.Name {
		exists, err := s.repo.ExistsByUserAndName(ctx, userID, wallet.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to check wallet name: %w", err)
		}

		if exists {
			return nil, domain.ErrDuplicateWalletName
		}
	}

	// Preserve user ID from existing wallet
	wallet.UserID = existing.UserID

	// Update wallet
	if err := s.repo.Update(ctx, wallet); err != nil {
		return nil, fmt.Errorf("failed to update wallet: %w", err)
	}

	return wallet, nil
}

// Delete deletes a wallet
func (s *WalletService) Delete(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	// Get existing wallet to verify ownership
	wallet, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if wallet.UserID != userID {
		return domain.ErrUnauthorizedAccess
	}

	// Delete wallet
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("failed to delete wallet: %w", err)
	}

	return nil
}
