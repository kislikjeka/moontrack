package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/google/uuid"

	adjustmentDomain "github.com/kislikjeka/moontrack/internal/modules/asset_adjustment/domain"
	"github.com/kislikjeka/moontrack/internal/core/ledger/domain"
	"github.com/kislikjeka/moontrack/internal/core/ledger/handler"
	"github.com/kislikjeka/moontrack/internal/core/ledger/service"
)

// AssetAdjustmentHandler handles asset balance adjustments
type AssetAdjustmentHandler struct {
	handler.BaseHandler
	ledgerService *service.LedgerService
}

// NewAssetAdjustmentHandler creates a new asset adjustment handler
func NewAssetAdjustmentHandler(ledgerService *service.LedgerService) *AssetAdjustmentHandler {
	return &AssetAdjustmentHandler{
		BaseHandler:   handler.NewBaseHandler("asset_adjustment"),
		ledgerService: ledgerService,
	}
}

// Handle processes an asset adjustment transaction
func (h *AssetAdjustmentHandler) Handle(ctx context.Context, data map[string]interface{}) ([]*domain.Entry, error) {
	// Unmarshal data into AssetAdjustmentTransaction
	txData, err := h.unmarshalData(data)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal data: %w", err)
	}

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
func (h *AssetAdjustmentHandler) generateEntries(ctx context.Context, tx *adjustmentDomain.AssetAdjustmentTransaction) ([]*domain.Entry, error) {
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
	difference := new(big.Int).Sub(tx.NewBalance, currentBalance)

	// If difference is 0, no adjustment needed
	if difference.Sign() == 0 {
		return nil, fmt.Errorf("no adjustment needed: balance already matches target")
	}

	// Get or fetch USD rate
	usdRate := tx.USDRate
	if usdRate == nil {
		// TODO: Fetch from price service when implemented in Phase 5
		// For now, use 0 as placeholder
		usdRate = big.NewInt(0)
	}

	entries := make([]*domain.Entry, 0, 2)

	if difference.Sign() > 0 {
		// Increase: DEBIT wallet, CREDIT adjustment_income
		// Entry 1: DEBIT wallet account (increase balance)
		entries = append(entries, &domain.Entry{
			ID:          uuid.New(),
			AccountID:   walletAccountID,
			DebitCredit: domain.Debit,
			EntryType:   domain.EntryTypeAssetIncrease,
			Amount:      new(big.Int).Set(difference),
			AssetID:     tx.AssetID,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    calculateUSDValue(difference, usdRate),
			OccurredAt:  tx.OccurredAt,
		})

		// Entry 2: CREDIT adjustment income account
		// TODO: Need to resolve adjustment account ID properly
		adjustmentAccountID := uuid.New() // Placeholder
		entries = append(entries, &domain.Entry{
			ID:          uuid.New(),
			AccountID:   adjustmentAccountID,
			DebitCredit: domain.Credit,
			EntryType:   domain.EntryTypeIncome,
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
		entries = append(entries, &domain.Entry{
			ID:          uuid.New(),
			AccountID:   walletAccountID,
			DebitCredit: domain.Credit,
			EntryType:   domain.EntryTypeAssetDecrease,
			Amount:      new(big.Int).Set(absDifference),
			AssetID:     tx.AssetID,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    calculateUSDValue(absDifference, usdRate),
			OccurredAt:  tx.OccurredAt,
		})

		// Entry 2: DEBIT adjustment expense account
		// TODO: Need to resolve adjustment account ID properly
		adjustmentAccountID := uuid.New() // Placeholder
		entries = append(entries, &domain.Entry{
			ID:          uuid.New(),
			AccountID:   adjustmentAccountID,
			DebitCredit: domain.Debit,
			EntryType:   domain.EntryTypeExpense,
			Amount:      new(big.Int).Set(absDifference),
			AssetID:     tx.AssetID,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    calculateUSDValue(absDifference, usdRate),
			OccurredAt:  tx.OccurredAt,
		})
	}

	return entries, nil
}

// unmarshalData unmarshals map data into AssetAdjustmentTransaction
func (h *AssetAdjustmentHandler) unmarshalData(data map[string]interface{}) (*adjustmentDomain.AssetAdjustmentTransaction, error) {
	// Convert to JSON and back for easier unmarshaling
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data: %w", err)
	}

	var tx adjustmentDomain.AssetAdjustmentTransaction
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
