package wallet

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// Service provides business logic for wallet operations
type Service struct {
	repo   Repository
	logger *logger.Logger
}

// NewService creates a new wallet service
func NewService(repo Repository, log *logger.Logger) *Service {
	return &Service{
		repo:   repo,
		logger: log.WithField("component", "wallet"),
	}
}

// Create creates a new wallet for a user
func (s *Service) Create(ctx context.Context, wallet *Wallet) (*Wallet, error) {
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
		return nil, ErrDuplicateWalletName
	}

	// Generate UUID for new wallet
	wallet.ID = uuid.New()

	// Create wallet
	if err := s.repo.Create(ctx, wallet); err != nil {
		return nil, fmt.Errorf("failed to create wallet: %w", err)
	}

	s.logger.Info("wallet created", "wallet_id", wallet.ID, "user_id", wallet.UserID)

	return wallet, nil
}

// GetByID retrieves a wallet by ID and validates user ownership
func (s *Service) GetByID(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*Wallet, error) {
	wallet, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Verify wallet belongs to requesting user
	if wallet.UserID != userID {
		s.logger.Warn("unauthorized wallet access", "wallet_id", id, "user_id", userID)
		return nil, ErrUnauthorizedAccess
	}

	return wallet, nil
}

// List retrieves all wallets for a user
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]*Wallet, error) {
	wallets, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list wallets: %w", err)
	}

	return wallets, nil
}

// Update updates an existing wallet
func (s *Service) Update(ctx context.Context, wallet *Wallet, userID uuid.UUID) (*Wallet, error) {
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
		return nil, ErrUnauthorizedAccess
	}

	// Check if new name conflicts with existing wallet
	if wallet.Name != existing.Name {
		exists, err := s.repo.ExistsByUserAndName(ctx, userID, wallet.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to check wallet name: %w", err)
		}

		if exists {
			return nil, ErrDuplicateWalletName
		}
	}

	// Preserve user ID from existing wallet
	wallet.UserID = existing.UserID

	// Update wallet
	if err := s.repo.Update(ctx, wallet); err != nil {
		return nil, fmt.Errorf("failed to update wallet: %w", err)
	}

	s.logger.Info("wallet updated", "wallet_id", wallet.ID, "user_id", userID)

	return wallet, nil
}

// Delete deletes a wallet
func (s *Service) Delete(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	// Get existing wallet to verify ownership
	wallet, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if wallet.UserID != userID {
		s.logger.Warn("unauthorized wallet access", "wallet_id", id, "user_id", userID)
		return ErrUnauthorizedAccess
	}

	// Delete wallet
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("failed to delete wallet: %w", err)
	}

	s.logger.Info("wallet deleted", "wallet_id", id, "user_id", userID)

	return nil
}
