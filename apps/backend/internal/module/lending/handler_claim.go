package lending

import (
	"context"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// LendingClaimHandler handles lending claim (rewards/interest) transactions.
type LendingClaimHandler struct {
	ledger.BaseHandler
	walletRepo WalletRepository
	logger     *logger.Logger
}

func NewLendingClaimHandler(walletRepo WalletRepository, log *logger.Logger) *LendingClaimHandler {
	return &LendingClaimHandler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeLendingClaim),
		walletRepo:  walletRepo,
		logger:      log.WithField("component", "lending_claim"),
	}
}

func (h *LendingClaimHandler) Handle(ctx context.Context, data map[string]interface{}) ([]*ledger.Entry, error) {
	var txn LendingTransaction
	if err := unmarshalData(data, &txn); err != nil {
		return nil, err
	}

	if err := h.ValidateData(ctx, data); err != nil {
		return nil, err
	}

	entries := generateClaimEntries(&txn)
	if gasEntries := generateGasFeeEntries(&txn); gasEntries != nil {
		entries = append(entries, gasEntries...)
	}

	h.logger.Debug("lending claim entries generated", "entry_count", len(entries))
	return entries, nil
}

func (h *LendingClaimHandler) ValidateData(ctx context.Context, data map[string]interface{}) error {
	var txn LendingTransaction
	if err := unmarshalData(data, &txn); err != nil {
		return err
	}

	if err := txn.Validate(); err != nil {
		return err
	}

	return validateWalletOwnership(ctx, h.walletRepo, txn.WalletID)
}
