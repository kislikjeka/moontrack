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
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
)

// ManualOutcomeHandler handles manual outcome transactions
// Generates ledger entries for outgoing assets (withdrawals, sales, payments)
type ManualOutcomeHandler struct {
	ledger.BaseHandler
	registryService RegistryService
	walletRepo      WalletRepository
	balanceGetter   BalanceGetter
}

// BalanceGetter defines the interface for getting account balances
type BalanceGetter interface {
	GetBalance(ctx context.Context, walletID uuid.UUID, assetID string) (*big.Int, error)
}

// NewManualOutcomeHandler creates a new manual outcome transaction handler
func NewManualOutcomeHandler(registryService RegistryService, walletRepo WalletRepository, balanceGetter BalanceGetter) *ManualOutcomeHandler {
	return &ManualOutcomeHandler{
		BaseHandler:     ledger.NewBaseHandler(ledger.TxTypeManualOutcome),
		registryService: registryService,
		walletRepo:      walletRepo,
		balanceGetter:   balanceGetter,
	}
}

// Handle processes a manual outcome transaction and generates ledger entries
func (h *ManualOutcomeHandler) Handle(ctx context.Context, data map[string]interface{}) ([]*ledger.Entry, error) {
	// Unmarshal data into ManualOutcomeTransaction
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal transaction data: %w", err)
	}

	var txn ManualOutcomeTransaction
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
func (h *ManualOutcomeHandler) ValidateData(ctx context.Context, data map[string]interface{}) error {
	// Unmarshal into struct for validation
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal transaction data: %w", err)
	}

	var txn ManualOutcomeTransaction
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

	// Verify sufficient balance
	currentBalance, err := h.balanceGetter.GetBalance(ctx, txn.WalletID, txn.AssetID)
	if err != nil {
		return fmt.Errorf("failed to get wallet balance: %w", err)
	}

	if currentBalance.Cmp(txn.GetAmount()) < 0 {
		return ErrInsufficientBalance
	}

	return nil
}

// GenerateEntries generates ledger entries for a manual outcome transaction
// Ledger entries generated (2 entries):
// 1. CREDIT wallet.{wallet_id}.{asset_id} (asset_decrease) - decreases wallet balance
// 2. DEBIT expense.{asset_id} (expense) - records expense
func (h *ManualOutcomeHandler) GenerateEntries(ctx context.Context, txn *ManualOutcomeTransaction) ([]*ledger.Entry, error) {
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

	// Entry 1: CREDIT wallet account (asset decreases)
	entries[0] = &ledger.Entry{
		ID:          uuid.New(),
		AccountID:   uuid.Nil, // Will be resolved by AccountResolver
		DebitCredit: ledger.Credit,
		EntryType:   ledger.EntryTypeAssetDecrease,
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

	// Entry 2: DEBIT expense account (expense)
	entries[1] = &ledger.Entry{
		ID:          uuid.New(),
		AccountID:   uuid.Nil, // Will be resolved by AccountResolver
		DebitCredit: ledger.Debit,
		EntryType:   ledger.EntryTypeExpense,
		Amount:      new(big.Int).Set(txn.GetAmount()),
		AssetID:     txn.AssetID,
		USDRate:     new(big.Int).Set(usdRate),
		USDValue:    new(big.Int).Set(usdValue),
		OccurredAt:  txn.OccurredAt,
		CreatedAt:   time.Now().UTC(),
		Metadata: map[string]interface{}{
			"account_code": fmt.Sprintf("expense.%s", txn.AssetID),
			"price_source": priceSource,
			"notes":        txn.Notes,
		},
	}

	return entries, nil
}
