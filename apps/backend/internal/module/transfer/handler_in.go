package transfer

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

// GenerateEntries generates ledger entries for a transfer in transaction
// Ledger entries generated (2 entries):
// 1. DEBIT wallet.{wallet_id}.{asset_id} (asset_increase) - increases wallet balance
// 2. CREDIT income.{chain_id}.{asset_id} (income) - records income from blockchain
func (h *TransferInHandler) GenerateEntries(ctx context.Context, txn *TransferInTransaction) ([]*ledger.Entry, error) {
	// Get USD rate
	usdRate := txn.GetUSDRate()
	if usdRate == nil || usdRate.Sign() == 0 {
		usdRate = big.NewInt(0) // Price will be determined later if not provided
	}

	// Calculate USD value: (amount * usd_rate) / 10^(decimals + 8)
	usdValue := new(big.Int).Mul(txn.GetAmount(), usdRate)
	if usdRate.Sign() > 0 {
		divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(txn.Decimals+8)), nil)
		usdValue.Div(usdValue, divisor)
	}

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
			"wallet_id":        txn.WalletID.String(),
			"account_code":     fmt.Sprintf("wallet.%s.%s", txn.WalletID.String(), txn.AssetID),
			"tx_hash":          txn.TxHash,
			"block_number":     txn.BlockNumber,
			"chain_id":         txn.ChainID,
			"from_address":     txn.FromAddress,
			"contract_address": txn.ContractAddress,
			"unique_id":        txn.UniqueID,
		},
	}

	// Entry 2: CREDIT income account (blockchain income)
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
			"account_code":     fmt.Sprintf("income.%d.%s", txn.ChainID, txn.AssetID),
			"tx_hash":          txn.TxHash,
			"block_number":     txn.BlockNumber,
			"chain_id":         txn.ChainID,
			"from_address":     txn.FromAddress,
			"contract_address": txn.ContractAddress,
			"unique_id":        txn.UniqueID,
		},
	}

	h.logger.Debug("transfer entries generated", "entry_count", len(entries), "asset_id", txn.AssetID)

	return entries, nil
}
