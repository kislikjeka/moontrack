package lending

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
)

// WalletRepository defines the interface for wallet operations.
type WalletRepository interface {
	GetByID(ctx context.Context, walletID uuid.UUID) (*wallet.Wallet, error)
}

func unmarshalData(data map[string]interface{}, out *LendingTransaction) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal transaction data: %w", err)
	}
	if err := json.Unmarshal(jsonData, out); err != nil {
		return fmt.Errorf("failed to unmarshal transaction data: %w", err)
	}
	return nil
}

func validateWalletOwnership(ctx context.Context, walletRepo WalletRepository, walletID uuid.UUID) error {
	w, err := walletRepo.GetByID(ctx, walletID)
	if err != nil {
		return fmt.Errorf("failed to get wallet: %w", err)
	}
	if w == nil {
		return ErrWalletNotFound
	}

	if userID, ok := middleware.GetUserIDFromContext(ctx); ok && userID != uuid.Nil {
		if w.UserID != userID {
			return ErrUnauthorized
		}
	}
	return nil
}
