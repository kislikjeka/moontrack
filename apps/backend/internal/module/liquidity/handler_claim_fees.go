package liquidity

import (
	"context"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// LPClaimFeesHandler handles LP fee claim transactions.
type LPClaimFeesHandler struct {
	ledger.BaseHandler
	walletRepo WalletRepository
	logger     *logger.Logger
}

func NewLPClaimFeesHandler(walletRepo WalletRepository, log *logger.Logger) *LPClaimFeesHandler {
	return &LPClaimFeesHandler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeLPClaimFees),
		walletRepo:  walletRepo,
		logger:      log.WithField("component", "lp_claim_fees"),
	}
}

func (h *LPClaimFeesHandler) Handle(ctx context.Context, data map[string]any) ([]*ledger.Entry, error) {
	var txn LPTransaction
	if err := unmarshalData(data, &txn); err != nil {
		return nil, err
	}

	if err := h.ValidateData(ctx, data); err != nil {
		return nil, err
	}

	entries := generateLPClaimEntries(&txn)
	entries = append(entries, generateGasFeeEntries(&txn)...)

	h.logger.Debug("LP claim fees entries generated", "entry_count", len(entries))
	return entries, nil
}

func (h *LPClaimFeesHandler) ValidateData(ctx context.Context, data map[string]any) error {
	var txn LPTransaction
	if err := unmarshalData(data, &txn); err != nil {
		return err
	}

	if err := txn.ValidateClaim(); err != nil {
		return err
	}

	return validateWalletOwnership(ctx, h.walletRepo, txn.WalletID)
}
