package defi

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

// WalletRepository defines the interface for wallet operations
type WalletRepository interface {
	GetByID(ctx context.Context, walletID uuid.UUID) (*wallet.Wallet, error)
}

// DeFiDepositHandler handles DeFi deposit/mint transactions.
// Generates swap-like balanced entries: OUT asset + IN receipt token through clearing accounts.
// Handles mint edge case (IN-only, no OUT transfers).
type DeFiDepositHandler struct {
	ledger.BaseHandler
	walletRepo WalletRepository
	logger     *logger.Logger
}

// NewDeFiDepositHandler creates a new DeFi deposit handler
func NewDeFiDepositHandler(walletRepo WalletRepository, log *logger.Logger) *DeFiDepositHandler {
	return &DeFiDepositHandler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeDefiDeposit),
		walletRepo:  walletRepo,
		logger:      log.WithField("component", "defi_deposit"),
	}
}

// Handle processes a DeFi deposit transaction and generates ledger entries
func (h *DeFiDepositHandler) Handle(ctx context.Context, data map[string]interface{}) ([]*ledger.Entry, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal transaction data: %w", err)
	}

	var txn DeFiTransaction
	if err := json.Unmarshal(jsonData, &txn); err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction data: %w", err)
	}

	h.logger.Debug("handling defi deposit",
		"wallet_id", txn.WalletID,
		"transfers", len(txn.Transfers),
		"protocol", txn.Protocol,
		"operation_type", txn.OperationType,
	)

	if err := h.ValidateData(ctx, data); err != nil {
		return nil, err
	}

	return h.GenerateEntries(ctx, &txn)
}

// ValidateData validates the DeFi deposit transaction data
func (h *DeFiDepositHandler) ValidateData(ctx context.Context, data map[string]interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal transaction data: %w", err)
	}

	var txn DeFiTransaction
	if err := json.Unmarshal(jsonData, &txn); err != nil {
		return fmt.Errorf("failed to unmarshal transaction data: %w", err)
	}

	if err := txn.Validate(); err != nil {
		return err
	}

	w, err := h.walletRepo.GetByID(ctx, txn.WalletID)
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

// GenerateEntries generates balanced ledger entries for a DeFi deposit.
// Uses shared swap-like entry generation plus gas fee entries.
func (h *DeFiDepositHandler) GenerateEntries(ctx context.Context, txn *DeFiTransaction) ([]*ledger.Entry, error) {
	entries := generateSwapLikeEntries(txn)

	if gasEntries := generateGasFeeEntries(txn); gasEntries != nil {
		entries = append(entries, gasEntries...)
	}

	h.logger.Debug("defi deposit entries generated", "entry_count", len(entries))

	return entries, nil
}
