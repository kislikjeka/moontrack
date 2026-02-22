package defi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// DeFiWithdrawHandler handles DeFi withdraw/burn transactions.
// Generates swap-like balanced entries: OUT receipt token + IN underlying asset through clearing.
type DeFiWithdrawHandler struct {
	ledger.BaseHandler
	walletRepo WalletRepository
	logger     *logger.Logger
}

// NewDeFiWithdrawHandler creates a new DeFi withdraw handler
func NewDeFiWithdrawHandler(walletRepo WalletRepository, log *logger.Logger) *DeFiWithdrawHandler {
	return &DeFiWithdrawHandler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeDefiWithdraw),
		walletRepo:  walletRepo,
		logger:      log.WithField("component", "defi_withdraw"),
	}
}

// Handle processes a DeFi withdraw transaction and generates ledger entries
func (h *DeFiWithdrawHandler) Handle(ctx context.Context, data map[string]interface{}) ([]*ledger.Entry, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal transaction data: %w", err)
	}

	var txn DeFiTransaction
	if err := json.Unmarshal(jsonData, &txn); err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction data: %w", err)
	}

	h.logger.Debug("handling defi withdraw",
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

// ValidateData validates the DeFi withdraw transaction data
func (h *DeFiWithdrawHandler) ValidateData(ctx context.Context, data map[string]interface{}) error {
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

// GenerateEntries generates balanced ledger entries for a DeFi withdrawal.
// Uses shared swap-like entry generation plus gas fee entries.
func (h *DeFiWithdrawHandler) GenerateEntries(ctx context.Context, txn *DeFiTransaction) ([]*ledger.Entry, error) {
	entries := generateSwapLikeEntries(txn)

	if gasEntries := generateGasFeeEntries(txn); gasEntries != nil {
		entries = append(entries, gasEntries...)
	}

	h.logger.Debug("defi withdraw entries generated", "entry_count", len(entries))

	return entries, nil
}
