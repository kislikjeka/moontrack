package swap

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
)

// WalletRepository defines the interface for wallet operations
type WalletRepository interface {
	GetByID(ctx context.Context, walletID uuid.UUID) (*wallet.Wallet, error)
}

// SwapHandler handles token swap (DEX) transactions.
// Generates balanced ledger entries for assets entering/leaving a wallet through a swap,
// plus optional gas fee entries.
type SwapHandler struct {
	ledger.BaseHandler
	walletRepo WalletRepository
}

// NewSwapHandler creates a new swap handler
func NewSwapHandler(walletRepo WalletRepository) *SwapHandler {
	return &SwapHandler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeSwap),
		walletRepo:  walletRepo,
	}
}

// Handle processes a swap transaction and generates ledger entries
func (h *SwapHandler) Handle(ctx context.Context, data map[string]interface{}) ([]*ledger.Entry, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal transaction data: %w", err)
	}

	var txn SwapTransaction
	if err := json.Unmarshal(jsonData, &txn); err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction data: %w", err)
	}

	if err := h.ValidateData(ctx, data); err != nil {
		return nil, err
	}

	return h.GenerateEntries(ctx, &txn)
}

// ValidateData validates the swap transaction data
func (h *SwapHandler) ValidateData(ctx context.Context, data map[string]interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal transaction data: %w", err)
	}

	var txn SwapTransaction
	if err := json.Unmarshal(jsonData, &txn); err != nil {
		return fmt.Errorf("failed to unmarshal transaction data: %w", err)
	}

	if err := txn.Validate(); err != nil {
		return err
	}

	w, err := h.walletRepo.GetByID(ctx, txn.WalletID)
	if err != nil {
		return fmt.Errorf("failed to get wallet: %w", err)
	}
	if w == nil {
		return ErrWalletNotFound
	}

	if userID, ok := middleware.GetUserIDFromContext(ctx); ok && userID != uuid.Nil {
		if w.UserID != userID {
			return ErrUnauthorized
		}
	}

	return nil
}

// GenerateEntries generates balanced ledger entries for a swap.
//
// For each outgoing transfer (asset leaving wallet):
//   - CREDIT wallet.{walletID}.{asset} (asset_decrease)
//   - DEBIT  clearing.{chainID}.{asset} (clearing)
//
// For each incoming transfer (asset entering wallet):
//   - DEBIT  wallet.{walletID}.{asset} (asset_increase)
//   - CREDIT clearing.{chainID}.{asset} (clearing)
//
// Gas fee (if present):
//   - DEBIT  gas.{chainID}.{feeAsset} (gas_fee)
//   - CREDIT wallet.{walletID}.{feeAsset} (asset_decrease)
func (h *SwapHandler) GenerateEntries(ctx context.Context, txn *SwapTransaction) ([]*ledger.Entry, error) {
	entries := make([]*ledger.Entry, 0, 2*(len(txn.TransfersOut)+len(txn.TransfersIn))+2)

	walletIDStr := txn.WalletID.String()
	chainIDStr := fmt.Sprintf("%d", txn.ChainID)

	// Outgoing transfers (asset leaving wallet)
	for _, tr := range txn.TransfersOut {
		amount := tr.Amount.ToBigInt()
		usdRate := big.NewInt(0)
		if tr.USDPrice != nil && !tr.USDPrice.IsNil() {
			usdRate = tr.USDPrice.ToBigInt()
		}
		usdValue := calcUSDValue(amount, usdRate, tr.Decimals)

		// CREDIT wallet (asset decrease)
		entries = append(entries, &ledger.Entry{
			ID:          uuid.New(),
			AccountID:   uuid.Nil,
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeAssetDecrease,
			Amount:      new(big.Int).Set(amount),
			AssetID:     tr.AssetSymbol,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    new(big.Int).Set(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"wallet_id":        walletIDStr,
				"account_code":     fmt.Sprintf("wallet.%s.%s", walletIDStr, tr.AssetSymbol),
				"tx_hash":          txn.TxHash,
				"chain_id":         chainIDStr,
				"swap_direction":   "out",
				"contract_address": tr.ContractAddress,
			},
		})

		// DEBIT clearing (clearing)
		entries = append(entries, &ledger.Entry{
			ID:          uuid.New(),
			AccountID:   uuid.Nil,
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeClearing,
			Amount:      new(big.Int).Set(amount),
			AssetID:     tr.AssetSymbol,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    new(big.Int).Set(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"account_code": fmt.Sprintf("clearing.%d.%s", txn.ChainID, tr.AssetSymbol),
				"account_type": "CLEARING",
				"chain_id":     chainIDStr,
				"tx_hash":      txn.TxHash,
				"swap_direction": "out",
			},
		})
	}

	// Incoming transfers (asset entering wallet)
	for _, tr := range txn.TransfersIn {
		amount := tr.Amount.ToBigInt()
		usdRate := big.NewInt(0)
		if tr.USDPrice != nil && !tr.USDPrice.IsNil() {
			usdRate = tr.USDPrice.ToBigInt()
		}
		usdValue := calcUSDValue(amount, usdRate, tr.Decimals)

		// DEBIT wallet (asset increase)
		entries = append(entries, &ledger.Entry{
			ID:          uuid.New(),
			AccountID:   uuid.Nil,
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeAssetIncrease,
			Amount:      new(big.Int).Set(amount),
			AssetID:     tr.AssetSymbol,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    new(big.Int).Set(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"wallet_id":        walletIDStr,
				"account_code":     fmt.Sprintf("wallet.%s.%s", walletIDStr, tr.AssetSymbol),
				"tx_hash":          txn.TxHash,
				"chain_id":         chainIDStr,
				"swap_direction":   "in",
				"contract_address": tr.ContractAddress,
			},
		})

		// CREDIT clearing (clearing)
		entries = append(entries, &ledger.Entry{
			ID:          uuid.New(),
			AccountID:   uuid.Nil,
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeClearing,
			Amount:      new(big.Int).Set(amount),
			AssetID:     tr.AssetSymbol,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    new(big.Int).Set(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"account_code": fmt.Sprintf("clearing.%d.%s", txn.ChainID, tr.AssetSymbol),
				"account_type": "CLEARING",
				"chain_id":     chainIDStr,
				"tx_hash":      txn.TxHash,
				"swap_direction": "in",
			},
		})
	}

	// Gas fee entries (if present)
	feeAmount := txn.getFeeAmount()
	if feeAmount != nil && feeAmount.Sign() > 0 {
		feeUSDRate := big.NewInt(0)
		if txn.FeeUSDPrice != nil && !txn.FeeUSDPrice.IsNil() {
			feeUSDRate = txn.FeeUSDPrice.ToBigInt()
		}
		feeDecimals := txn.FeeDecimals
		if feeDecimals == 0 {
			feeDecimals = 18 // Default to 18 for native tokens
		}
		feeUSDValue := calcUSDValue(feeAmount, feeUSDRate, feeDecimals)
		feeAsset := txn.FeeAsset

		// DEBIT gas account (gas fee)
		entries = append(entries, &ledger.Entry{
			ID:          uuid.New(),
			AccountID:   uuid.Nil,
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeGasFee,
			Amount:      new(big.Int).Set(feeAmount),
			AssetID:     feeAsset,
			USDRate:     new(big.Int).Set(feeUSDRate),
			USDValue:    new(big.Int).Set(feeUSDValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"account_code": fmt.Sprintf("gas.%d.%s", txn.ChainID, feeAsset),
				"tx_hash":      txn.TxHash,
				"chain_id":     chainIDStr,
			},
		})

		// CREDIT wallet native asset (asset decrease for gas)
		entries = append(entries, &ledger.Entry{
			ID:          uuid.New(),
			AccountID:   uuid.Nil,
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeAssetDecrease,
			Amount:      new(big.Int).Set(feeAmount),
			AssetID:     feeAsset,
			USDRate:     new(big.Int).Set(feeUSDRate),
			USDValue:    new(big.Int).Set(feeUSDValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"wallet_id":    walletIDStr,
				"account_code": fmt.Sprintf("wallet.%s.%s", walletIDStr, feeAsset),
				"tx_hash":      txn.TxHash,
				"chain_id":     chainIDStr,
				"entry_type":   "gas_payment",
			},
		})
	}

	return entries, nil
}

// getFeeAmount returns the fee amount as *big.Int, or nil if no fee
func (txn *SwapTransaction) getFeeAmount() *big.Int {
	if txn.FeeAmount == nil || txn.FeeAmount.IsNil() {
		return nil
	}
	return txn.FeeAmount.ToBigInt()
}

// calcUSDValue computes (amount * usdRate) / 10^(decimals+8)
func calcUSDValue(amount, usdRate *big.Int, decimals int) *big.Int {
	if usdRate.Sign() == 0 {
		return big.NewInt(0)
	}
	value := new(big.Int).Mul(amount, usdRate)
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals+8)), nil)
	value.Div(value, divisor)
	return value
}
