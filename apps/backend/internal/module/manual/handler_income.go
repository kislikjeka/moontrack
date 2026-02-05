package manual

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/asset"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
)

// ManualIncomeHandler handles manual income transactions
// Generates ledger entries for incoming assets (deposits, purchases, rewards)
type ManualIncomeHandler struct {
	ledger.BaseHandler
	registryService RegistryService
	walletRepo      WalletRepository
}

// RegistryService defines the interface for asset registry and price operations
type RegistryService interface {
	GetCurrentPriceByCoinGeckoID(ctx context.Context, coinGeckoID string) (*big.Int, error)
	GetHistoricalPriceByCoinGeckoID(ctx context.Context, coinGeckoID string, date time.Time) (*big.Int, error)
}

// WalletRepository defines the interface for wallet operations
type WalletRepository interface {
	GetByID(ctx context.Context, walletID uuid.UUID) (*wallet.Wallet, error)
}

// NewManualIncomeHandler creates a new manual income transaction handler
func NewManualIncomeHandler(registryService RegistryService, walletRepo WalletRepository) *ManualIncomeHandler {
	return &ManualIncomeHandler{
		BaseHandler:     ledger.NewBaseHandler(ledger.TxTypeManualIncome),
		registryService: registryService,
		walletRepo:      walletRepo,
	}
}

// Handle processes a manual income transaction and generates ledger entries
func (h *ManualIncomeHandler) Handle(ctx context.Context, data map[string]interface{}) ([]*ledger.Entry, error) {
	// Unmarshal data into ManualIncomeTransaction
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal transaction data: %w", err)
	}

	var txn ManualIncomeTransaction
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

	var txn ManualIncomeTransaction
	if err := json.Unmarshal(jsonData, &txn); err != nil {
		return fmt.Errorf("failed to unmarshal transaction data: %w", err)
	}

	// Validate transaction
	if err := txn.Validate(); err != nil {
		return err
	}

	// Verify wallet exists
	w, err := h.walletRepo.GetByID(ctx, txn.WalletID)
	if err != nil {
		return fmt.Errorf("failed to get wallet: %w", err)
	}
	if w == nil {
		return ErrWalletNotFound
	}

	// Verify wallet ownership - user can only record transactions on their own wallets
	if userID, ok := middleware.GetUserIDFromContext(ctx); ok && userID != uuid.Nil {
		if w.UserID != userID {
			return ErrUnauthorized
		}
	}

	return nil
}

// GenerateEntries generates ledger entries for a manual income transaction
// Ledger entries generated (2 entries):
// 1. DEBIT wallet.{wallet_id}.{asset_id} (asset_increase) - increases wallet balance
// 2. CREDIT income.{asset_id} (income) - records income source
func (h *ManualIncomeHandler) GenerateEntries(ctx context.Context, txn *ManualIncomeTransaction) ([]*ledger.Entry, error) {
	// Get USD rate (from manual input or fetch from API)
	usdRate := txn.GetUSDRate()
	priceSource := txn.PriceSource

	if usdRate == nil || usdRate.Sign() == 0 {
		// Fetch from registry service
		// Use PriceAssetID (CoinGecko ID) if available, otherwise fall back to AssetID
		priceAssetID := txn.PriceAssetID
		if priceAssetID == "" {
			priceAssetID = txn.AssetID
		}

		// If occurred_at is today, use current price, otherwise use historical price
		today := time.Now().Truncate(24 * time.Hour)
		occurredDate := txn.OccurredAt.Truncate(24 * time.Hour)

		var price *big.Int
		var err error

		if occurredDate.Equal(today) {
			price, err = h.registryService.GetCurrentPriceByCoinGeckoID(ctx, priceAssetID)
		} else {
			price, err = h.registryService.GetHistoricalPriceByCoinGeckoID(ctx, priceAssetID, txn.OccurredAt)
		}

		if err != nil {
			// If asset service fails and no manual price, check for stale price warning
			var staleWarning *asset.StalePriceWarning
			if errors.As(err, &staleWarning) {
				// Use stale price but keep warning in metadata
				usdRate = staleWarning.Price
				priceSource = "coingecko_stale"
			} else {
				return nil, fmt.Errorf("failed to fetch price for %s: %w (consider providing manual USD rate)", priceAssetID, err)
			}
		} else {
			usdRate = price
			priceSource = "coingecko"
		}
	} else {
		priceSource = "manual"
	}

	// Calculate USD value: (amount * usd_rate) / 10^decimals / 10^8
	// amount is in base units (satoshi, wei, etc.)
	// usd_rate is USD price per 1 whole unit, scaled by 10^8
	// Result should be in cents (or micro-dollars for precision)
	usdValue := new(big.Int).Mul(txn.GetAmount(), usdRate)
	// Divide by 10^(decimals + 8) to account for both base unit scaling and USD rate scaling
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(txn.Decimals+8)), nil)
	usdValue.Div(usdValue, divisor)

	// Generate entries
	entries := make([]*ledger.Entry, 2)

	// Entry 1: DEBIT wallet account (asset increases)
	entries[0] = &ledger.Entry{
		ID:          uuid.New(),
		AccountID:   uuid.Nil, // Will be resolved by AccountResolver
		DebitCredit: ledger.Debit,
		EntryType:   ledger.EntryTypeAssetIncrease,
		Amount:      new(big.Int).Set(txn.GetAmount()),
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
	entries[1] = &ledger.Entry{
		ID:          uuid.New(),
		AccountID:   uuid.Nil, // Will be resolved by AccountResolver
		DebitCredit: ledger.Credit,
		EntryType:   ledger.EntryTypeIncome,
		Amount:      new(big.Int).Set(txn.GetAmount()),
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
