package liquidity

import (
	"context"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// LPWithdrawHandler handles LP withdraw transactions.
type LPWithdrawHandler struct {
	ledger.BaseHandler
	walletRepo WalletRepository
	logger     *logger.Logger
}

func NewLPWithdrawHandler(walletRepo WalletRepository, log *logger.Logger) *LPWithdrawHandler {
	return &LPWithdrawHandler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeLPWithdraw),
		walletRepo:  walletRepo,
		logger:      log.WithField("component", "lp_withdraw"),
	}
}

func (h *LPWithdrawHandler) Handle(ctx context.Context, data map[string]any) ([]*ledger.Entry, error) {
	var txn LPTransaction
	if err := unmarshalData(data, &txn); err != nil {
		return nil, err
	}

	if err := h.ValidateData(ctx, data); err != nil {
		return nil, err
	}

	entries := generateSwapLikeEntries(&txn)
	entries = append(entries, generateGasFeeEntries(&txn)...)

	h.logger.Debug("LP withdraw entries generated", "entry_count", len(entries))
	return entries, nil
}

func (h *LPWithdrawHandler) ValidateData(ctx context.Context, data map[string]any) error {
	var txn LPTransaction
	if err := unmarshalData(data, &txn); err != nil {
		return err
	}

	if err := txn.Validate(); err != nil {
		return err
	}

	return validateWalletOwnership(ctx, h.walletRepo, txn.WalletID)
}
