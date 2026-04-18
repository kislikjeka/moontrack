package lending

import (
	"context"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// LendingSupplyHandler handles lending supply transactions.
type LendingSupplyHandler struct {
	ledger.BaseHandler
	walletRepo WalletRepository
	logger     *logger.Logger
}

func NewLendingSupplyHandler(walletRepo WalletRepository, log *logger.Logger) *LendingSupplyHandler {
	return &LendingSupplyHandler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeLendingSupply),
		walletRepo:  walletRepo,
		logger:      log.WithField("component", "lending_supply"),
	}
}

func (h *LendingSupplyHandler) Handle(ctx context.Context, data map[string]interface{}) ([]*ledger.Entry, error) {
	var txn LendingTransaction
	if err := unmarshalData(data, &txn); err != nil {
		return nil, err
	}

	if err := h.ValidateData(ctx, data); err != nil {
		return nil, err
	}

	entries := make([]*ledger.Entry, 0)
	if len(txn.Transfers) > 0 {
		for i := range txn.Transfers {
			entries = append(entries, generateSupplyItemEntries(&txn, &txn.Transfers[i])...)
		}
	} else {
		entries = append(entries, generateSupplyEntries(&txn)...)
	}
	if gasEntries := generateGasFeeEntries(&txn); gasEntries != nil {
		entries = append(entries, gasEntries...)
	}

	h.logger.Debug("lending supply entries generated", "entry_count", len(entries))
	return entries, nil
}

func (h *LendingSupplyHandler) ValidateData(ctx context.Context, data map[string]interface{}) error {
	var txn LendingTransaction
	if err := unmarshalData(data, &txn); err != nil {
		return err
	}

	if err := txn.Validate(); err != nil {
		return err
	}

	return validateWalletOwnership(ctx, h.walletRepo, txn.WalletID)
}
