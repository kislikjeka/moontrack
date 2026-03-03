package lending

import (
	"context"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// LendingRepayHandler handles lending repay transactions.
type LendingRepayHandler struct {
	ledger.BaseHandler
	walletRepo WalletRepository
	logger     *logger.Logger
}

func NewLendingRepayHandler(walletRepo WalletRepository, log *logger.Logger) *LendingRepayHandler {
	return &LendingRepayHandler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeLendingRepay),
		walletRepo:  walletRepo,
		logger:      log.WithField("component", "lending_repay"),
	}
}

func (h *LendingRepayHandler) Handle(ctx context.Context, data map[string]interface{}) ([]*ledger.Entry, error) {
	var txn LendingTransaction
	if err := unmarshalData(data, &txn); err != nil {
		return nil, err
	}

	if err := h.ValidateData(ctx, data); err != nil {
		return nil, err
	}

	entries := generateRepayEntries(&txn)
	if gasEntries := generateGasFeeEntries(&txn); gasEntries != nil {
		entries = append(entries, gasEntries...)
	}

	h.logger.Debug("lending repay entries generated", "entry_count", len(entries))
	return entries, nil
}

func (h *LendingRepayHandler) ValidateData(ctx context.Context, data map[string]interface{}) error {
	var txn LendingTransaction
	if err := unmarshalData(data, &txn); err != nil {
		return err
	}

	if err := txn.Validate(); err != nil {
		return err
	}

	return validateWalletOwnership(ctx, h.walletRepo, txn.WalletID)
}
