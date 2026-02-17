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
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// InternalTransferHandler handles transfers between user's own wallets
// Generates ledger entries for moving assets between wallets without income/expense
type InternalTransferHandler struct {
	ledger.BaseHandler
	walletRepo WalletRepository
	logger     *logger.Logger
}

// NewInternalTransferHandler creates a new internal transfer handler
func NewInternalTransferHandler(walletRepo WalletRepository, log *logger.Logger) *InternalTransferHandler {
	return &InternalTransferHandler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeInternalTransfer),
		walletRepo:  walletRepo,
		logger:      log.WithField("component", "transfer"),
	}
}

// Handle processes an internal transfer transaction and generates ledger entries
func (h *InternalTransferHandler) Handle(ctx context.Context, data map[string]interface{}) ([]*ledger.Entry, error) {
	// Unmarshal data into InternalTransferTransaction
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal transaction data: %w", err)
	}

	var txn InternalTransferTransaction
	if err := json.Unmarshal(jsonData, &txn); err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction data: %w", err)
	}

	h.logger.Debug("handling transfer", "tx_type", "internal_transfer", "wallet_id", txn.SourceWalletID)

	// Validate data
	if err := h.ValidateData(ctx, data); err != nil {
		return nil, err
	}

	// Generate ledger entries
	return h.GenerateEntries(ctx, &txn)
}

// ValidateData validates the transaction data
func (h *InternalTransferHandler) ValidateData(ctx context.Context, data map[string]interface{}) error {
	// Unmarshal into struct for validation
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal transaction data: %w", err)
	}

	var txn InternalTransferTransaction
	if err := json.Unmarshal(jsonData, &txn); err != nil {
		return fmt.Errorf("failed to unmarshal transaction data: %w", err)
	}

	// Validate transaction
	if err := txn.Validate(); err != nil {
		return err
	}

	// Verify source wallet exists
	srcWallet, err := h.walletRepo.GetByID(ctx, txn.SourceWalletID)
	if err != nil {
		return fmt.Errorf("failed to get source wallet: %w", err)
	}
	if srcWallet == nil {
		return ErrWalletNotFound
	}

	// Verify destination wallet exists
	dstWallet, err := h.walletRepo.GetByID(ctx, txn.DestWalletID)
	if err != nil {
		return fmt.Errorf("failed to get destination wallet: %w", err)
	}
	if dstWallet == nil {
		return ErrWalletNotFound
	}

	// Verify both wallets belong to the same user
	if srcWallet.UserID != dstWallet.UserID {
		return ErrUnauthorized
	}

	// Verify wallet ownership - user can only record transactions on their own wallets
	if userID, ok := middleware.GetUserIDFromContext(ctx); ok && userID != uuid.Nil {
		if srcWallet.UserID != userID || dstWallet.UserID != userID {
			return ErrUnauthorized
		}
	}

	return nil
}

// GenerateEntries generates ledger entries for an internal transfer transaction
// Ledger entries generated (2-4 entries):
// 1. DEBIT wallet.{dest_wallet_id}.{asset_id} (asset_increase) - increases destination balance
// 2. CREDIT wallet.{src_wallet_id}.{asset_id} (asset_decrease) - decreases source balance
// If gas fee is present:
// 3. DEBIT gas.{chain_id}.{native_asset} (gas_fee) - records gas expense
// 4. CREDIT wallet.{src_wallet_id}.{native_asset} (asset_decrease) - decreases source native balance
func (h *InternalTransferHandler) GenerateEntries(ctx context.Context, txn *InternalTransferTransaction) ([]*ledger.Entry, error) {
	// Get USD rate for transferred asset
	usdRate := txn.GetUSDRate()
	if usdRate == nil {
		usdRate = big.NewInt(0)
	}

	// Calculate USD value for transfer: (amount * usd_rate) / 10^decimals
	usdValue := new(big.Int).Mul(txn.GetAmount(), usdRate)
	if usdRate.Sign() > 0 {
		divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(txn.Decimals)), nil)
		usdValue.Div(usdValue, divisor)
	}

	entries := make([]*ledger.Entry, 0, 4)

	// Entry 1: DEBIT destination wallet account (increases balance)
	entries = append(entries, &ledger.Entry{
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
			"wallet_id":        txn.DestWalletID.String(),
			"account_code":     fmt.Sprintf("wallet.%s.%s", txn.DestWalletID.String(), txn.AssetID),
			"tx_hash":          txn.TxHash,
			"block_number":     txn.BlockNumber,
			"chain_id":         txn.ChainID,
			"transfer_type":    "internal_receive",
			"source_wallet_id": txn.SourceWalletID.String(),
			"contract_address": txn.ContractAddress,
			"unique_id":        txn.UniqueID,
		},
	})

	// Entry 2: CREDIT source wallet account (decreases balance)
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
			"wallet_id":      txn.SourceWalletID.String(),
			"account_code":   fmt.Sprintf("wallet.%s.%s", txn.SourceWalletID.String(), txn.AssetID),
			"tx_hash":        txn.TxHash,
			"block_number":   txn.BlockNumber,
			"chain_id":       txn.ChainID,
			"transfer_type":  "internal_send",
			"dest_wallet_id": txn.DestWalletID.String(),
			"contract_address": txn.ContractAddress,
			"unique_id":      txn.UniqueID,
		},
	})

	// Add gas fee entries if gas is present
	gasAmount := txn.GetGasAmount()
	if gasAmount != nil && gasAmount.Sign() > 0 {
		gasUSDRate := txn.GetGasUSDRate()
		if gasUSDRate == nil {
			gasUSDRate = big.NewInt(0)
		}

		// Get gas decimals (default to 18 for native tokens)
		gasDecimals := txn.GasDecimals
		if gasDecimals == 0 {
			gasDecimals = 18
		}

		// Calculate gas USD value
		gasUSDValue := new(big.Int).Mul(gasAmount, gasUSDRate)
		if gasUSDRate.Sign() > 0 {
			divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(gasDecimals)), nil)
			gasUSDValue.Div(gasUSDValue, divisor)
		}

		// Get native asset ID
		nativeAssetID := txn.NativeAssetID
		if nativeAssetID == "" {
			nativeAssetID = "ETH" // Default fallback
		}

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

		// Entry 4: CREDIT source wallet native asset account (decreases native balance)
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
				"wallet_id":    txn.SourceWalletID.String(),
				"account_code": fmt.Sprintf("wallet.%s.%s", txn.SourceWalletID.String(), nativeAssetID),
				"tx_hash":      txn.TxHash,
				"block_number": txn.BlockNumber,
				"chain_id":     txn.ChainID,
				"entry_type":   "gas_payment",
			},
		})
	}

	h.logger.Debug("transfer entries generated", "entry_count", len(entries), "asset_id", txn.AssetID)

	return entries, nil
}
