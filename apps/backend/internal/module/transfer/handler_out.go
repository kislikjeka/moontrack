package transfer

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/ledger/accountcode"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// TransferOutHandler handles outgoing blockchain transfers
// Generates ledger entries for assets sent to external addresses
type TransferOutHandler struct {
	ledger.BaseHandler
	walletRepo WalletRepository
	logger     *logger.Logger
}

// NewTransferOutHandler creates a new transfer out handler
func NewTransferOutHandler(walletRepo WalletRepository, log *logger.Logger) *TransferOutHandler {
	return &TransferOutHandler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeTransferOut),
		walletRepo:  walletRepo,
		logger:      log.WithField("component", "transfer"),
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

	h.logger.Debug("handling transfer", "tx_type", "transfer_out", "wallet_id", txn.WalletID)

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

// GenerateEntries generates ledger entries for a transfer out transaction.
// For each asset movement (from Transfers when populated, else the legacy
// flat fields) it emits a balanced pair:
//  1. DEBIT  expense.{chain_id}.{asset_id}            (expense)
//  2. CREDIT wallet.{wallet_id}.{chain}.{asset_id}    (asset_decrease)
//
// If gas is present the usual native-token gas pair is appended once.
func (h *TransferOutHandler) GenerateEntries(ctx context.Context, txn *TransferOutTransaction) ([]*ledger.Entry, error) {
	items := h.collectItems(txn)

	entries := make([]*ledger.Entry, 0, 2*len(items)+2)
	for i := range items {
		entries = append(entries, h.entriesForItem(txn, &items[i])...)
	}

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
			divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(18)), nil)
			gasUSDValue.Div(gasUSDValue, divisor)
		}

		// The chain's native asset must be resolved before gas can be booked.
		// The old code defaulted to "ETH" here, which charged every non-Ethereum
		// chain's gas to Ethereum's ETH account (#59) — a wrong balance that
		// still reconciled. There is no safe default, so this is an error.
		nativeAssetID := txn.NativeAssetID
		if nativeAssetID == uuid.Nil {
			return nil, ErrMissingNativeAsset
		}
		nativeAssetSeg := nativeAssetID.String()

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
				"account_code": accountcode.GasCode(txn.ChainID, nativeAssetSeg),
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
				"account_code": accountcode.WalletCode(txn.WalletID, txn.ChainID, nativeAssetSeg),
				"tx_hash":      txn.TxHash,
				"block_number": txn.BlockNumber,
				"chain_id":     txn.ChainID,
				"entry_type":   "gas_payment",
			},
		})
	}

	h.logger.Debug("transfer entries generated", "entry_count", len(entries), "item_count", len(items))

	return entries, nil
}

// collectItems returns the list of asset movements. When Transfers is
// populated it wins; otherwise the legacy flat fields are converted into a
// one-element slice.
func (h *TransferOutHandler) collectItems(txn *TransferOutTransaction) []TransferItem {
	if len(txn.Transfers) > 0 {
		return txn.Transfers
	}
	return []TransferItem{{
		AssetID:         txn.AssetID,
		Decimals:        txn.Decimals,
		Amount:          txn.Amount,
		USDRate:         txn.USDRate,
		ContractAddress: txn.ContractAddress,
		ToAddress:       txn.ToAddress,
		Direction:       "out",
	}}
}

// entriesForItem emits one balanced debit/credit pair for a single outgoing
// asset movement.
func (h *TransferOutHandler) entriesForItem(txn *TransferOutTransaction, item *TransferItem) []*ledger.Entry {
	usdRate := item.GetUSDRate()
	if usdRate == nil {
		usdRate = big.NewInt(0)
	}

	// USD value = (amount * usd_rate) / 10^decimals
	usdValue := new(big.Int).Mul(item.GetAmount(), usdRate)
	if usdRate.Sign() > 0 {
		divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(item.Decimals)), nil)
		usdValue.Div(usdValue, divisor)
	}

	return []*ledger.Entry{
		// DEBIT expense account (records expense)
		{
			ID:          uuid.New(),
			AccountID:   uuid.Nil,
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeExpense,
			Amount:      new(big.Int).Set(item.GetAmount()),
			AssetID:     item.AssetID,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    new(big.Int).Set(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"account_code":     accountcode.ExpenseCode(txn.ChainID, item.AssetID.String()),
				"tx_hash":          txn.TxHash,
				"block_number":     txn.BlockNumber,
				"chain_id":         txn.ChainID,
				"to_address":       item.ToAddress,
				"contract_address": item.ContractAddress,
				"unique_id":        txn.UniqueID,
			},
		},
		// CREDIT wallet account (decreases balance)
		{
			ID:          uuid.New(),
			AccountID:   uuid.Nil,
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeAssetDecrease,
			Amount:      new(big.Int).Set(item.GetAmount()),
			AssetID:     item.AssetID,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    new(big.Int).Set(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"wallet_id":        txn.WalletID.String(),
				"account_code":     accountcode.WalletCode(txn.WalletID, txn.ChainID, item.AssetID.String()),
				"tx_hash":          txn.TxHash,
				"block_number":     txn.BlockNumber,
				"chain_id":         txn.ChainID,
				"to_address":       item.ToAddress,
				"contract_address": item.ContractAddress,
				"unique_id":        txn.UniqueID,
			},
		},
	}
}
