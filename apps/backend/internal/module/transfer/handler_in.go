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
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// TransferInHandler handles incoming blockchain transfers
// Generates ledger entries for assets received from external addresses
type TransferInHandler struct {
	ledger.BaseHandler
	walletRepo WalletRepository
	logger     *logger.Logger
}

// WalletRepository defines the interface for wallet operations
type WalletRepository interface {
	GetByID(ctx context.Context, walletID uuid.UUID) (*wallet.Wallet, error)
}

// NewTransferInHandler creates a new transfer in handler
func NewTransferInHandler(walletRepo WalletRepository, log *logger.Logger) *TransferInHandler {
	return &TransferInHandler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeTransferIn),
		walletRepo:  walletRepo,
		logger:      log.WithField("component", "transfer"),
	}
}

// Handle processes a transfer in transaction and generates ledger entries
func (h *TransferInHandler) Handle(ctx context.Context, data map[string]interface{}) ([]*ledger.Entry, error) {
	// Unmarshal data into TransferInTransaction
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal transaction data: %w", err)
	}

	var txn TransferInTransaction
	if err := json.Unmarshal(jsonData, &txn); err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction data: %w", err)
	}

	h.logger.Debug("handling transfer", "tx_type", "transfer_in", "wallet_id", txn.WalletID)

	// Validate data
	if err := h.ValidateData(ctx, data); err != nil {
		return nil, err
	}

	// Generate ledger entries
	return h.GenerateEntries(ctx, &txn)
}

// ValidateData validates the transaction data
func (h *TransferInHandler) ValidateData(ctx context.Context, data map[string]interface{}) error {
	// Unmarshal into struct for validation
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal transaction data: %w", err)
	}

	var txn TransferInTransaction
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

// GenerateEntries generates ledger entries for a transfer in transaction.
// For each asset movement (from Transfers when populated, else the legacy
// flat fields) it emits a balanced pair:
//  1. DEBIT  wallet.{wallet_id}.{chain}.{asset_id} (asset_increase)
//  2. CREDIT income.{chain_id}.{asset_id}         (income)
func (h *TransferInHandler) GenerateEntries(ctx context.Context, txn *TransferInTransaction) ([]*ledger.Entry, error) {
	items := h.collectItems(txn)

	entries := make([]*ledger.Entry, 0, 2*len(items))
	for i := range items {
		entries = append(entries, h.entriesForItem(txn, &items[i])...)
	}

	h.logger.Debug("transfer entries generated", "entry_count", len(entries), "item_count", len(items))
	return entries, nil
}

// collectItems returns the list of asset movements to turn into ledger entries.
// When Transfers is populated it wins; otherwise the single-asset flat fields
// are converted into a one-element slice so the generator is uniform.
func (h *TransferInHandler) collectItems(txn *TransferInTransaction) []TransferItem {
	if len(txn.Transfers) > 0 {
		return txn.Transfers
	}
	return []TransferItem{{
		AssetID:         txn.AssetID,
		Decimals:        txn.Decimals,
		Amount:          txn.Amount,
		USDRate:         txn.USDRate,
		ContractAddress: txn.ContractAddress,
		FromAddress:     txn.FromAddress,
		Direction:       "in",
	}}
}

// entriesForItem emits one balanced debit/credit pair for a single asset.
func (h *TransferInHandler) entriesForItem(txn *TransferInTransaction, item *TransferItem) []*ledger.Entry {
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
		// DEBIT wallet account (asset increases)
		{
			ID:          uuid.New(),
			AccountID:   uuid.Nil,
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeAssetIncrease,
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
				"from_address":     item.FromAddress,
				"contract_address": item.ContractAddress,
				"unique_id":        txn.UniqueID,
			},
		},
		// CREDIT income account (blockchain income)
		{
			ID:          uuid.New(),
			AccountID:   uuid.Nil,
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeIncome,
			Amount:      new(big.Int).Set(item.GetAmount()),
			AssetID:     item.AssetID,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    new(big.Int).Set(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"account_code":     accountcode.IncomeCode(txn.ChainID, item.AssetID.String()),
				"tx_hash":          txn.TxHash,
				"block_number":     txn.BlockNumber,
				"chain_id":         txn.ChainID,
				"from_address":     item.FromAddress,
				"contract_address": item.ContractAddress,
				"unique_id":        txn.UniqueID,
			},
		},
	}
}
