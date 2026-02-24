package genesis

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/pkg/logger"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// GenesisBalanceTransaction represents the data for an auto-created genesis balance.
type GenesisBalanceTransaction struct {
	WalletID   uuid.UUID     `json:"wallet_id"`
	AssetID    string        `json:"asset_id"`
	ChainID    string        `json:"chain_id"`
	Amount     *money.BigInt `json:"amount"`
	Decimals   int           `json:"decimals"`
	USDRate    *money.BigInt `json:"usd_rate"`
	OccurredAt time.Time     `json:"occurred_at"`
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
	if txn.AssetID == "" {
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

	usdRate := big.NewInt(0)
	if txn.USDRate != nil && !txn.USDRate.IsNil() {
		usdRate = txn.USDRate.ToBigInt()
	}

	usdValue := new(big.Int).Mul(amount, usdRate)
	if usdRate.Sign() > 0 && txn.Decimals > 0 {
		divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(txn.Decimals)), nil)
		usdValue.Div(usdValue, divisor)
	}

	now := time.Now().UTC()
	walletCode := fmt.Sprintf("wallet.%s.%s.%s", txn.WalletID.String(), txn.ChainID, txn.AssetID)
	incomeCode := fmt.Sprintf("income.genesis.%s.%s", txn.ChainID, txn.AssetID)

	entries := []*ledger.Entry{
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeAssetIncrease,
			Amount:      new(big.Int).Set(amount),
			AssetID:     txn.AssetID,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    new(big.Int).Set(usdValue),
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
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    new(big.Int).Set(usdValue),
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
		"asset", txn.AssetID,
		"chain", txn.ChainID,
		"amount", amount.String())

	return entries, nil
}
