package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	ledgerdomain "github.com/kislikjeka/moontrack/internal/core/ledger/domain"
	"github.com/kislikjeka/moontrack/internal/core/ledger/handler"
	"github.com/kislikjeka/moontrack/internal/core/pricing/service"
	"github.com/kislikjeka/moontrack/internal/modules/manual_transaction/domain"
	walletdomain "github.com/kislikjeka/moontrack/internal/modules/wallet/domain"
)

const (
	TransactionTypeManualIncome = "manual_income"
)

// ManualIncomeHandler handles manual income transactions
// Generates ledger entries for incoming assets (deposits, purchases, rewards)
type ManualIncomeHandler struct {
	handler.BaseHandler
	priceService PriceService
	walletRepo   WalletRepository
}

// PriceService defines the interface for price fetching
type PriceService interface {
	GetCurrentPrice(ctx context.Context, assetID string) (*big.Int, error)
	GetHistoricalPrice(ctx context.Context, assetID string, date time.Time) (*big.Int, error)
}

// WalletRepository defines the interface for wallet operations
type WalletRepository interface {
	GetByID(ctx context.Context, walletID uuid.UUID) (*walletdomain.Wallet, error)
}

// NewManualIncomeHandler creates a new manual income transaction handler
func NewManualIncomeHandler(priceService PriceService, walletRepo WalletRepository) *ManualIncomeHandler {
	return &ManualIncomeHandler{
		BaseHandler:  handler.NewBaseHandler(TransactionTypeManualIncome),
		priceService: priceService,
		walletRepo:   walletRepo,
	}
}

// Handle processes a manual income transaction and generates ledger entries
func (h *ManualIncomeHandler) Handle(ctx context.Context, data map[string]interface{}) ([]*ledgerdomain.Entry, error) {
	// Unmarshal data into ManualIncomeTransaction
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal transaction data: %w", err)
	}

	var txn domain.ManualIncomeTransaction
	if err := json.Unmarshal(jsonData, &txn); err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction data: %w", err)
	}

	// Validate data
	if err := h.ValidateData(ctx, data); err != nil {
		return nil, err
	}

	// Generate ledger entries
	return h.GenerateEntries(ctx, &txn)
}

// ValidateData validates the transaction data
func (h *ManualIncomeHandler) ValidateData(ctx context.Context, data map[string]interface{}) error {
	// Unmarshal into struct for validation
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal transaction data: %w", err)
	}

	var txn domain.ManualIncomeTransaction
	if err := json.Unmarshal(jsonData, &txn); err != nil {
		return fmt.Errorf("failed to unmarshal transaction data: %w", err)
	}

	// Validate transaction
	if err := txn.Validate(); err != nil {
		return err
	}

	// Verify wallet exists
	wallet, err := h.walletRepo.GetByID(ctx, txn.WalletID)
	if err != nil {
		return fmt.Errorf("failed to get wallet: %w", err)
	}
	if wallet == nil {
		return domain.ErrWalletNotFound
	}

	return nil
}

// GenerateEntries generates ledger entries for a manual income transaction
// Ledger entries generated (2 entries):
// 1. DEBIT wallet.{wallet_id}.{asset_id} (asset_increase) - increases wallet balance
// 2. CREDIT income.{asset_id} (income) - records income source
func (h *ManualIncomeHandler) GenerateEntries(ctx context.Context, txn *domain.ManualIncomeTransaction) ([]*ledgerdomain.Entry, error) {
	// Get USD rate (from manual input or fetch from API)
	usdRate := txn.USDRate
	priceSource := txn.PriceSource

	if usdRate == nil || usdRate.Sign() == 0 {
		// Fetch from price service
		// If occurred_at is today, use current price, otherwise use historical price
		today := time.Now().Truncate(24 * time.Hour)
		occurredDate := txn.OccurredAt.Truncate(24 * time.Hour)

		var price *big.Int
		var err error

		if occurredDate.Equal(today) {
			price, err = h.priceService.GetCurrentPrice(ctx, txn.AssetID)
		} else {
			price, err = h.priceService.GetHistoricalPrice(ctx, txn.AssetID, txn.OccurredAt)
		}

		if err != nil {
			// If price service fails and no manual price, return error with details
			var staleWarning *service.StalePriceWarning
			if errors.As(err, &staleWarning) {
				// Use stale price but keep warning in metadata
				usdRate = staleWarning.Price
				priceSource = "coingecko_stale"
			} else {
				return nil, fmt.Errorf("failed to fetch price for %s: %w (consider providing manual USD rate)", txn.AssetID, err)
			}
		} else {
			usdRate = price
			priceSource = "coingecko"
		}
	} else {
		priceSource = "manual"
	}

	// Calculate USD value: (amount * usd_rate) / 10^8
	usdValue := new(big.Int).Mul(txn.Amount, usdRate)
	usdValue.Div(usdValue, big.NewInt(100000000)) // Divide by 10^8

	// Generate entries
	entries := make([]*ledgerdomain.Entry, 2)

	// Entry 1: DEBIT wallet account (asset increases)
	entries[0] = &ledgerdomain.Entry{
		ID:          uuid.New(),
		AccountID:   uuid.Nil, // Will be resolved by AccountResolver
		DebitCredit: ledgerdomain.Debit,
		EntryType:   ledgerdomain.EntryTypeAssetIncrease,
		Amount:      new(big.Int).Set(txn.Amount),
		AssetID:     txn.AssetID,
		USDRate:     new(big.Int).Set(usdRate),
		USDValue:    new(big.Int).Set(usdValue),
		OccurredAt:  txn.OccurredAt,
		CreatedAt:   time.Now().UTC(),
		Metadata: map[string]interface{}{
			"wallet_id":    txn.WalletID.String(),
			"account_code": fmt.Sprintf("wallet.%s.%s", txn.WalletID.String(), txn.AssetID),
			"price_source": priceSource,
			"notes":        txn.Notes,
		},
	}

	// Entry 2: CREDIT income account (income source)
	entries[1] = &ledgerdomain.Entry{
		ID:          uuid.New(),
		AccountID:   uuid.Nil, // Will be resolved by AccountResolver
		DebitCredit: ledgerdomain.Credit,
		EntryType:   ledgerdomain.EntryTypeIncome,
		Amount:      new(big.Int).Set(txn.Amount),
		AssetID:     txn.AssetID,
		USDRate:     new(big.Int).Set(usdRate),
		USDValue:    new(big.Int).Set(usdValue),
		OccurredAt:  txn.OccurredAt,
		CreatedAt:   time.Now().UTC(),
		Metadata: map[string]interface{}{
			"account_code": fmt.Sprintf("income.%s", txn.AssetID),
			"price_source": priceSource,
			"notes":        txn.Notes,
		},
	}

	return entries, nil
}
