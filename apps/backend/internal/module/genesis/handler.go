package genesis

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/ledger/accountcode"
	"github.com/kislikjeka/moontrack/pkg/logger"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// GenesisBalanceTransaction represents the data for an auto-created genesis balance.
//
// AssetID is a registry UUID, not a ticker (#59). It reaches both the ledger
// entry and the two account codes below, so it is pure identity; the ticker that
// used to sit here now travels as AssetSymbol for display only. uuid.UUID's
// UnmarshalJSON rejects a malformed string outright, and ValidateData rejects an
// absent one, so a genesis balance can never be booked against uuid.Nil.
type GenesisBalanceTransaction struct {
	WalletID    uuid.UUID     `json:"wallet_id"`
	AssetID     uuid.UUID     `json:"asset_id"`
	AssetSymbol string        `json:"asset_symbol"`
	ChainID     string        `json:"chain_id"`
	Amount      *money.BigInt `json:"amount"`
	Decimals    int           `json:"decimals"`
	USDRate     *money.BigInt `json:"usd_rate"`
	OccurredAt  time.Time     `json:"occurred_at"`
}

// Handler handles genesis_balance transactions.
// These are auto-created by the sync service to cover missing prior history.
type Handler struct {
	ledger.BaseHandler
	logger *logger.Logger
}

// NewHandler creates a new genesis balance handler.
func NewHandler(log *logger.Logger) *Handler {
	return &Handler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeGenesisBalance),
		logger:      log.WithField("component", "genesis"),
	}
}

// Handle processes a genesis balance transaction and generates ledger entries.
func (h *Handler) Handle(ctx context.Context, data map[string]interface{}) ([]*ledger.Entry, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal transaction data: %w", err)
	}

	var txn GenesisBalanceTransaction
	if err := json.Unmarshal(jsonData, &txn); err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction data: %w", err)
	}

	if err := h.ValidateData(ctx, data); err != nil {
		return nil, err
	}

	return h.generateEntries(&txn)
}

// ValidateData validates the genesis balance transaction data.
func (h *Handler) ValidateData(_ context.Context, data map[string]interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal transaction data: %w", err)
	}

	var txn GenesisBalanceTransaction
	if err := json.Unmarshal(jsonData, &txn); err != nil {
		return fmt.Errorf("failed to unmarshal transaction data: %w", err)
	}

	if txn.WalletID == uuid.Nil {
		return fmt.Errorf("wallet_id is required")
	}
	if txn.AssetID == uuid.Nil {
		return fmt.Errorf("asset_id is required")
	}
	if txn.ChainID == "" {
		return fmt.Errorf("chain_id is required")
	}
	if txn.Amount.IsNil() || txn.Amount.Sign() <= 0 {
		return fmt.Errorf("amount must be positive")
	}

	return nil
}

// generateEntries creates the double-entry ledger entries for a genesis balance.
//
// Entry pattern (2 entries):
//   - DEBIT  wallet.{wallet_id}.{chain_id}.{asset_id} → asset_increase
//   - CREDIT income.genesis.{chain_id}.{asset_id}     → income
func (h *Handler) generateEntries(txn *GenesisBalanceTransaction) ([]*ledger.Entry, error) {
	amount := txn.Amount.ToBigInt()

	// An absent rate is nil, not zero (#74). Multiplying by it panicked and took
	// the whole process down (#77); money.CalcUSDValue returns nil instead, so
	// an unknown rate yields an unknown value and the lot is created pending.
	usdRate := txn.USDRate.ToBigInt()
	usdValue := money.CalcUSDValue(amount, usdRate, txn.Decimals)

	now := time.Now().UTC()
	// The asset segment of an account code is the registry UUID's string form
	// (#59) — same identity as Entry.AssetID, so the account a code names and
	// the asset an entry carries can no longer drift apart.
	assetSeg := txn.AssetID.String()
	walletCode := accountcode.WalletCode(txn.WalletID, txn.ChainID, assetSeg)
	incomeCode := accountcode.IncomeGenesisCode(txn.ChainID, assetSeg)

	entries := []*ledger.Entry{
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeAssetIncrease,
			Amount:      new(big.Int).Set(amount),
			AssetID:     txn.AssetID,
			USDRate:     money.CopyRate(usdRate),
			USDValue:    money.CopyRate(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   now,
			Metadata: map[string]interface{}{
				"wallet_id":    txn.WalletID.String(),
				"account_code": walletCode,
				"chain_id":     txn.ChainID,
			},
		},
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeIncome,
			Amount:      new(big.Int).Set(amount),
			AssetID:     txn.AssetID,
			USDRate:     money.CopyRate(usdRate),
			USDValue:    money.CopyRate(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   now,
			Metadata: map[string]interface{}{
				"account_code": incomeCode,
				"chain_id":     txn.ChainID,
			},
		},
	}

	h.logger.Debug("genesis balance entries generated",
		"wallet_id", txn.WalletID.String(),
		"asset_id", assetSeg,
		"asset_symbol", txn.AssetSymbol,
		"chain", txn.ChainID,
		"amount", amount.String())

	return entries, nil
}
