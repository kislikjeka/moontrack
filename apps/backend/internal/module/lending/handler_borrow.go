package lending

import (
	"context"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// LendingBorrowHandler handles lending borrow transactions.
type LendingBorrowHandler struct {
	ledger.BaseHandler
	walletRepo WalletRepository
	logger     *logger.Logger
}

func NewLendingBorrowHandler(walletRepo WalletRepository, log *logger.Logger) *LendingBorrowHandler {
	return &LendingBorrowHandler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeLendingBorrow),
		walletRepo:  walletRepo,
		logger:      log.WithField("component", "lending_borrow"),
	}
}

func (h *LendingBorrowHandler) Handle(ctx context.Context, data map[string]interface{}) ([]*ledger.Entry, error) {
	var txn LendingTransaction
	if err := unmarshalData(data, &txn); err != nil {
		return nil, err
	}

	if err := h.ValidateData(ctx, data); err != nil {
		return nil, err
	}

	entries := make([]*ledger.Entry, 0, 2*len(txn.Transfers))
	for i := range txn.Transfers {
		entries = append(entries, generateBorrowItemEntries(&txn, &txn.Transfers[i])...)
	}
	if gasEntries := generateGasFeeEntries(&txn); gasEntries != nil {
		entries = append(entries, gasEntries...)
	}

	h.logger.Debug("lending borrow entries generated", "entry_count", len(entries))
	return entries, nil
}

func (h *LendingBorrowHandler) ValidateData(ctx context.Context, data map[string]interface{}) error {
	var txn LendingTransaction
	if err := unmarshalData(data, &txn); err != nil {
		return err
	}

	if err := txn.Validate(); err != nil {
		return err
	}

	return validateWalletOwnership(ctx, h.walletRepo, txn.WalletID)
}
