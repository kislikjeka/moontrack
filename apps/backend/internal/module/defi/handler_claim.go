package defi

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
	"github.com/kislikjeka/moontrack/pkg/money"
)

// DeFiClaimHandler handles DeFi reward claim transactions.
// Generates income entries: IN transfers are rewards, credited to income account.
type DeFiClaimHandler struct {
	ledger.BaseHandler
	walletRepo WalletRepository
	logger     *logger.Logger
}

// NewDeFiClaimHandler creates a new DeFi claim handler
func NewDeFiClaimHandler(walletRepo WalletRepository, log *logger.Logger) *DeFiClaimHandler {
	return &DeFiClaimHandler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeDefiClaim),
		walletRepo:  walletRepo,
		logger:      log.WithField("component", "defi_claim"),
	}
}

// Handle processes a DeFi claim transaction and generates ledger entries
func (h *DeFiClaimHandler) Handle(ctx context.Context, data map[string]interface{}) ([]*ledger.Entry, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal transaction data: %w", err)
	}

	var txn DeFiTransaction
	if err := json.Unmarshal(jsonData, &txn); err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction data: %w", err)
	}

	h.logger.Debug("handling defi claim",
		"wallet_id", txn.WalletID,
		"transfers", len(txn.Transfers),
		"protocol", txn.Protocol,
		"operation_type", txn.OperationType,
	)

	if err := h.ValidateData(ctx, data); err != nil {
		return nil, err
	}

	return h.GenerateEntries(ctx, &txn)
}

// ValidateData validates the DeFi claim transaction data
func (h *DeFiClaimHandler) ValidateData(ctx context.Context, data map[string]interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal transaction data: %w", err)
	}

	var txn DeFiTransaction
	if err := json.Unmarshal(jsonData, &txn); err != nil {
		return fmt.Errorf("failed to unmarshal transaction data: %w", err)
	}

	// Claims require at least one IN transfer
	if err := txn.ValidateClaim(); err != nil {
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

// GenerateEntries generates balanced ledger entries for a DeFi claim.
//
// For each IN transfer (reward received):
//
//	DEBIT  wallet.{wID}.{chain}.{asset}   (asset_increase)
//	CREDIT income.defi.{chain}.{asset}    (income)
//
// Gas fee (if present):
//
//	DEBIT  gas.{chain}.{feeAsset}          (gas_fee)
//	CREDIT wallet.{wID}.{chain}.{feeAsset} (asset_decrease)
func (h *DeFiClaimHandler) GenerateEntries(ctx context.Context, txn *DeFiTransaction) ([]*ledger.Entry, error) {
	transfersIn := txn.TransfersIn()
	entries := make([]*ledger.Entry, 0, 2*len(transfersIn)+2)

	walletIDStr := txn.WalletID.String()
	chainIDStr := txn.ChainID

	baseMeta := buildBaseMetadata(txn)

	for _, tr := range transfersIn {
		amount := tr.Amount.ToBigInt()
		usdRate := big.NewInt(0)
		if tr.USDPrice != nil && !tr.USDPrice.IsNil() {
			usdRate = tr.USDPrice.ToBigInt()
		}
		usdValue := money.CalcUSDValue(amount, usdRate, tr.Decimals)

		walletMeta := mergeMetadata(baseMeta, map[string]interface{}{
			"wallet_id":        walletIDStr,
			"account_code":     fmt.Sprintf("wallet.%s.%s.%s", walletIDStr, chainIDStr, tr.AssetSymbol),
			"tx_hash":          txn.TxHash,
			"chain_id":         chainIDStr,
			"direction":        "in",
			"contract_address": tr.ContractAddress,
		})

		// DEBIT wallet (asset increase — reward received)
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
			Metadata:    walletMeta,
		})

		incomeMeta := mergeMetadata(baseMeta, map[string]interface{}{
			"account_code":     fmt.Sprintf("income.defi.%s.%s", chainIDStr, tr.AssetSymbol),
			"tx_hash":          txn.TxHash,
			"chain_id":         chainIDStr,
			"contract_address": tr.ContractAddress,
		})

		// CREDIT income account (DeFi reward income)
		entries = append(entries, &ledger.Entry{
			ID:          uuid.New(),
			AccountID:   uuid.Nil,
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeIncome,
			Amount:      new(big.Int).Set(amount),
			AssetID:     tr.AssetSymbol,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    new(big.Int).Set(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata:    incomeMeta,
		})
	}

	if gasEntries := generateGasFeeEntries(txn); gasEntries != nil {
		entries = append(entries, gasEntries...)
	}

	h.logger.Debug("defi claim entries generated", "entry_count", len(entries))

	return entries, nil
}
