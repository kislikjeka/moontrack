package liquidity

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// WalletRepository defines the interface for wallet operations.
type WalletRepository interface {
	GetByID(ctx context.Context, walletID uuid.UUID) (*wallet.Wallet, error)
}

// LPDepositHandler handles LP deposit transactions.
type LPDepositHandler struct {
	ledger.BaseHandler
	walletRepo WalletRepository
	logger     *logger.Logger
}

func NewLPDepositHandler(walletRepo WalletRepository, log *logger.Logger) *LPDepositHandler {
	return &LPDepositHandler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeLPDeposit),
		walletRepo:  walletRepo,
		logger:      log.WithField("component", "lp_deposit"),
	}
}

func (h *LPDepositHandler) Handle(ctx context.Context, data map[string]any) ([]*ledger.Entry, error) {
	var txn LPTransaction
	if err := unmarshalData(data, &txn); err != nil {
		return nil, err
	}

	if err := h.ValidateData(ctx, data); err != nil {
		return nil, err
	}

	entries := generateSwapLikeEntries(&txn)
	entries = append(entries, generateGasFeeEntries(&txn)...)

	h.logger.Debug("LP deposit entries generated", "entry_count", len(entries))
	return entries, nil
}

func (h *LPDepositHandler) ValidateData(ctx context.Context, data map[string]any) error {
	var txn LPTransaction
	if err := unmarshalData(data, &txn); err != nil {
		return err
	}

	if err := txn.Validate(); err != nil {
		return err
	}

	return validateWalletOwnership(ctx, h.walletRepo, txn.WalletID)
}

// unmarshalData converts map[string]any to a typed struct via JSON roundtrip.
func unmarshalData(data map[string]any, target any) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal transaction data: %w", err)
	}
	if err := json.Unmarshal(jsonData, target); err != nil {
		return fmt.Errorf("failed to unmarshal transaction data: %w", err)
	}
	return nil
}

// validateWalletOwnership checks that the wallet exists and belongs to the authenticated user.
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
