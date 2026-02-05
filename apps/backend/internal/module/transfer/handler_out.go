package transfer

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
)

// TransferOutHandler handles outgoing blockchain transfers
// Generates ledger entries for assets sent to external addresses
type TransferOutHandler struct {
	ledger.BaseHandler
	walletRepo WalletRepository
}

// NewTransferOutHandler creates a new transfer out handler
func NewTransferOutHandler(walletRepo WalletRepository) *TransferOutHandler {
	return &TransferOutHandler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeTransferOut),
		walletRepo:  walletRepo,
	}
}

// Handle processes a transfer out transaction and generates ledger entries
func (h *TransferOutHandler) Handle(ctx context.Context, data map[string]interface{}) ([]*ledger.Entry, error) {
	// Unmarshal data into TransferOutTransaction
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal transaction data: %w", err)
	}

	var txn TransferOutTransaction
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
func (h *TransferOutHandler) ValidateData(ctx context.Context, data map[string]interface{}) error {
	// Unmarshal into struct for validation
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal transaction data: %w", err)
	}

	var txn TransferOutTransaction
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

	// Note: For blockchain-synced transactions, we don't check balance here
	// because the transaction already happened on-chain. The balance check
	// is more relevant for manual transactions that haven't been confirmed yet.

	return nil
}

// GenerateEntries generates ledger entries for a transfer out transaction
// Ledger entries generated (2-4 entries):
// 1. DEBIT expense.{chain_id}.{asset_id} (expense) - records expense
// 2. CREDIT wallet.{wallet_id}.{asset_id} (asset_decrease) - decreases wallet balance
// If gas fee is present (separate from transfer amount):
// 3. DEBIT gas.{chain_id}.{native_asset} (gas_fee) - records gas expense
// 4. CREDIT wallet.{wallet_id}.{native_asset} (asset_decrease) - decreases native token balance
func (h *TransferOutHandler) GenerateEntries(ctx context.Context, txn *TransferOutTransaction) ([]*ledger.Entry, error) {
	// Get USD rate for transferred asset
	usdRate := txn.GetUSDRate()
	if usdRate == nil {
		usdRate = big.NewInt(0)
	}

	// Calculate USD value for transfer
	usdValue := new(big.Int).Mul(txn.GetAmount(), usdRate)
	if usdRate.Sign() > 0 {
		divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(txn.Decimals+8)), nil)
		usdValue.Div(usdValue, divisor)
	}

	entries := make([]*ledger.Entry, 0, 4)

	// Entry 1: DEBIT expense account (records expense)
	entries = append(entries, &ledger.Entry{
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
			"account_code":     fmt.Sprintf("expense.%d.%s", txn.ChainID, txn.AssetID),
			"tx_hash":          txn.TxHash,
			"block_number":     txn.BlockNumber,
			"chain_id":         txn.ChainID,
			"to_address":       txn.ToAddress,
			"contract_address": txn.ContractAddress,
			"unique_id":        txn.UniqueID,
		},
	})

	// Entry 2: CREDIT wallet account (decreases balance)
	entries = append(entries, &ledger.Entry{
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
			"wallet_id":        txn.WalletID.String(),
			"account_code":     fmt.Sprintf("wallet.%s.%s", txn.WalletID.String(), txn.AssetID),
			"tx_hash":          txn.TxHash,
			"block_number":     txn.BlockNumber,
			"chain_id":         txn.ChainID,
			"to_address":       txn.ToAddress,
			"contract_address": txn.ContractAddress,
			"unique_id":        txn.UniqueID,
		},
	})

	// Add gas fee entries if gas is present
	gasAmount := txn.GetGasAmount()
	if gasAmount != nil && gasAmount.Sign() > 0 {
		gasUSDRate := txn.GetGasUSDRate()
		if gasUSDRate == nil {
			gasUSDRate = big.NewInt(0)
		}

		// Calculate gas USD value (native token, always 18 decimals)
		gasUSDValue := new(big.Int).Mul(gasAmount, gasUSDRate)
		if gasUSDRate.Sign() > 0 {
			divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(18+8)), nil)
			gasUSDValue.Div(gasUSDValue, divisor)
		}

		// Get native asset symbol (assume ETH for simplicity, should come from chain config)
		nativeAssetID := "ETH" // This should be derived from chain ID

		// Entry 3: DEBIT gas account (records gas expense)
		entries = append(entries, &ledger.Entry{
			ID:          uuid.New(),
			AccountID:   uuid.Nil,
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeGasFee,
			Amount:      new(big.Int).Set(gasAmount),
			AssetID:     nativeAssetID,
			USDRate:     new(big.Int).Set(gasUSDRate),
			USDValue:    new(big.Int).Set(gasUSDValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"account_code": fmt.Sprintf("gas.%d.%s", txn.ChainID, nativeAssetID),
				"tx_hash":      txn.TxHash,
				"block_number": txn.BlockNumber,
				"chain_id":     txn.ChainID,
			},
		})

		// Entry 4: CREDIT wallet native asset account (decreases native balance)
		entries = append(entries, &ledger.Entry{
			ID:          uuid.New(),
			AccountID:   uuid.Nil,
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeAssetDecrease,
			Amount:      new(big.Int).Set(gasAmount),
			AssetID:     nativeAssetID,
			USDRate:     new(big.Int).Set(gasUSDRate),
			USDValue:    new(big.Int).Set(gasUSDValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"wallet_id":    txn.WalletID.String(),
				"account_code": fmt.Sprintf("wallet.%s.%s", txn.WalletID.String(), nativeAssetID),
				"tx_hash":      txn.TxHash,
				"block_number": txn.BlockNumber,
				"chain_id":     txn.ChainID,
				"entry_type":   "gas_payment",
			},
		})
	}

	return entries, nil
}
