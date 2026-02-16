package adjustment

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// AssetAdjustmentHandler handles asset balance adjustments
type AssetAdjustmentHandler struct {
	ledger.BaseHandler
	ledgerService *ledger.Service
	logger        *logger.Logger
}

// NewAssetAdjustmentHandler creates a new asset adjustment handler
func NewAssetAdjustmentHandler(ledgerService *ledger.Service, log *logger.Logger) *AssetAdjustmentHandler {
	return &AssetAdjustmentHandler{
		BaseHandler:   ledger.NewBaseHandler(ledger.TxTypeAssetAdjustment),
		ledgerService: ledgerService,
		logger:        log.WithField("component", "adjustment"),
	}
}

// Handle processes an asset adjustment transaction
func (h *AssetAdjustmentHandler) Handle(ctx context.Context, data map[string]interface{}) ([]*ledger.Entry, error) {
	// Unmarshal data into AssetAdjustmentTransaction
	txData, err := h.unmarshalData(data)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal data: %w", err)
	}

	h.logger.Debug("handling adjustment", "wallet_id", txData.WalletID, "asset_id", txData.AssetID)

	// Validate transaction data
	if err := txData.Validate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Generate ledger entries
	entries, err := h.generateEntries(ctx, txData)
	if err != nil {
		return nil, fmt.Errorf("failed to generate entries: %w", err)
	}

	return entries, nil
}

// ValidateData validates the transaction data
func (h *AssetAdjustmentHandler) ValidateData(ctx context.Context, data map[string]interface{}) error {
	txData, err := h.unmarshalData(data)
	if err != nil {
		return fmt.Errorf("failed to unmarshal data: %w", err)
	}

	return txData.Validate()
}

// generateEntries generates ledger entries for asset adjustment
func (h *AssetAdjustmentHandler) generateEntries(ctx context.Context, tx *AssetAdjustmentTransaction) ([]*ledger.Entry, error) {
	// Get wallet account ID (will be resolved by ledger service)
	// For now, we use the wallet ID directly as the account ID
	// In a production system, we'd need to properly resolve the account
	walletAccountID := tx.WalletID

	// Get current balance for this account
	currentBalanceObj, err := h.ledgerService.GetAccountBalance(ctx, walletAccountID, tx.AssetID)
	var currentBalance *big.Int
	if err != nil {
		// If account doesn't exist yet, current balance is 0
		currentBalance = big.NewInt(0)
	} else {
		currentBalance = currentBalanceObj.Balance
	}

	// Calculate difference
	difference := new(big.Int).Sub(tx.GetNewBalance(), currentBalance)

	// If difference is 0, no adjustment needed
	if difference.Sign() == 0 {
		return nil, fmt.Errorf("no adjustment needed: balance already matches target")
	}

	// Get or fetch USD rate
	usdRate := tx.GetUSDRate()
	if usdRate == nil {
		// TODO: Fetch from price service when implemented in Phase 5
		// For now, use 0 as placeholder
		usdRate = big.NewInt(0)
	}

	entries := make([]*ledger.Entry, 0, 2)

	if difference.Sign() > 0 {
		// Increase: DEBIT wallet, CREDIT adjustment_income
		// Entry 1: DEBIT wallet account (increase balance)
		entries = append(entries, &ledger.Entry{
			ID:          uuid.New(),
			AccountID:   walletAccountID,
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeAssetIncrease,
			Amount:      new(big.Int).Set(difference),
			AssetID:     tx.AssetID,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    calculateUSDValue(difference, usdRate),
			OccurredAt:  tx.OccurredAt,
		})

		// Entry 2: CREDIT adjustment income account
		// TODO: Need to resolve adjustment account ID properly
		adjustmentAccountID := uuid.New() // Placeholder
		entries = append(entries, &ledger.Entry{
			ID:          uuid.New(),
			AccountID:   adjustmentAccountID,
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeIncome,
			Amount:      new(big.Int).Set(difference),
			AssetID:     tx.AssetID,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    calculateUSDValue(difference, usdRate),
			OccurredAt:  tx.OccurredAt,
		})
	} else {
		// Decrease: CREDIT wallet, DEBIT adjustment_expense
		absDifference := new(big.Int).Abs(difference)

		// Entry 1: CREDIT wallet account (decrease balance)
		entries = append(entries, &ledger.Entry{
			ID:          uuid.New(),
			AccountID:   walletAccountID,
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeAssetDecrease,
			Amount:      new(big.Int).Set(absDifference),
			AssetID:     tx.AssetID,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    calculateUSDValue(absDifference, usdRate),
			OccurredAt:  tx.OccurredAt,
		})

		// Entry 2: DEBIT adjustment expense account
		// TODO: Need to resolve adjustment account ID properly
		adjustmentAccountID := uuid.New() // Placeholder
		entries = append(entries, &ledger.Entry{
			ID:          uuid.New(),
			AccountID:   adjustmentAccountID,
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeExpense,
			Amount:      new(big.Int).Set(absDifference),
			AssetID:     tx.AssetID,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    calculateUSDValue(absDifference, usdRate),
			OccurredAt:  tx.OccurredAt,
		})
	}

	direction := "increase"
	if difference.Sign() < 0 {
		direction = "decrease"
	}
	h.logger.Info("adjustment entries generated", "direction", direction, "difference", difference.String(), "entry_count", len(entries))

	return entries, nil
}

// unmarshalData unmarshals map data into AssetAdjustmentTransaction
func (h *AssetAdjustmentHandler) unmarshalData(data map[string]interface{}) (*AssetAdjustmentTransaction, error) {
	// Convert to JSON and back for easier unmarshaling
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data: %w", err)
	}

	var tx AssetAdjustmentTransaction
	if err := json.Unmarshal(jsonData, &tx); err != nil {
		return nil, fmt.Errorf("failed to unmarshal data: %w", err)
	}

	return &tx, nil
}

// calculateUSDValue calculates USD value: (amount * usd_rate) / 10^8
func calculateUSDValue(amount, usdRate *big.Int) *big.Int {
	if usdRate == nil || usdRate.Sign() == 0 {
		return big.NewInt(0)
	}

	// usd_value = (amount * usd_rate) / 10^8
	value := new(big.Int).Mul(amount, usdRate)
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(8), nil)
	return value.Div(value, divisor)
}
