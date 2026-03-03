package lending

import (
	"context"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// LendingWithdrawHandler handles lending withdraw transactions.
type LendingWithdrawHandler struct {
	ledger.BaseHandler
	walletRepo WalletRepository
	logger     *logger.Logger
}

func NewLendingWithdrawHandler(walletRepo WalletRepository, log *logger.Logger) *LendingWithdrawHandler {
	return &LendingWithdrawHandler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeLendingWithdraw),
		walletRepo:  walletRepo,
		logger:      log.WithField("component", "lending_withdraw"),
	}
}

func (h *LendingWithdrawHandler) Handle(ctx context.Context, data map[string]interface{}) ([]*ledger.Entry, error) {
	var txn LendingTransaction
	if err := unmarshalData(data, &txn); err != nil {
		return nil, err
	}

	if err := h.ValidateData(ctx, data); err != nil {
		return nil, err
	}

	entries := generateWithdrawEntries(&txn)
	if gasEntries := generateGasFeeEntries(&txn); gasEntries != nil {
		entries = append(entries, gasEntries...)
	}

	h.logger.Debug("lending withdraw entries generated", "entry_count", len(entries))
	return entries, nil
}

func (h *LendingWithdrawHandler) ValidateData(ctx context.Context, data map[string]interface{}) error {
	var txn LendingTransaction
	if err := unmarshalData(data, &txn); err != nil {
		return err
	}

	if err := txn.Validate(); err != nil {
		return err
	}

	return validateWalletOwnership(ctx, h.walletRepo, txn.WalletID)
}
