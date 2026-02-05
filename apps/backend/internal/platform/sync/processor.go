package sync

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// LedgerService defines the interface for ledger operations needed by sync
type LedgerService interface {
	RecordTransaction(ctx context.Context, transactionType ledger.TransactionType, source string, externalID *string, occurredAt time.Time, rawData map[string]interface{}) (*ledger.Transaction, error)
}

// Processor handles transfer classification and ledger recording
type Processor struct {
	walletRepo   WalletRepository
	ledgerSvc    LedgerService
	assetSvc     AssetService
	logger       *slog.Logger
	addressCache map[string][]uuid.UUID // address -> wallet IDs cache
}

// NewProcessor creates a new sync processor
func NewProcessor(walletRepo WalletRepository, ledgerSvc LedgerService, assetSvc AssetService, logger *slog.Logger) *Processor {
	return &Processor{
		walletRepo:   walletRepo,
		ledgerSvc:    ledgerSvc,
		assetSvc:     assetSvc,
		logger:       logger,
		addressCache: make(map[string][]uuid.UUID),
	}
}

// ProcessTransfer processes a single transfer and records it to the ledger
func (p *Processor) ProcessTransfer(ctx context.Context, w *wallet.Wallet, transfer Transfer) error {
	// Classify the transfer
	classification := p.classifyTransfer(ctx, w, transfer)

	// Create transaction data based on classification
	switch classification {
	case TransferClassIncoming:
		return p.recordIncomingTransfer(ctx, w, transfer)
	case TransferClassOutgoing:
		return p.recordOutgoingTransfer(ctx, w, transfer)
	case TransferClassInternal:
		return p.recordInternalTransfer(ctx, w, transfer)
	default:
		p.logger.Warn("unknown transfer classification",
			"wallet_id", w.ID,
			"tx_hash", transfer.TxHash,
			"classification", classification)
		return nil
	}
}

// TransferClassification represents how a transfer is classified
type TransferClassification string

const (
	TransferClassIncoming TransferClassification = "incoming"
	TransferClassOutgoing TransferClassification = "outgoing"
	TransferClassInternal TransferClassification = "internal"
	TransferClassUnknown  TransferClassification = "unknown"
)

// classifyTransfer determines if a transfer is incoming, outgoing, or internal
func (p *Processor) classifyTransfer(ctx context.Context, w *wallet.Wallet, transfer Transfer) TransferClassification {
	walletAddr := strings.ToLower(w.Address)
	fromAddr := strings.ToLower(transfer.From)
	toAddr := strings.ToLower(transfer.To)

	// Check if the counterparty is one of user's wallets
	var counterpartyAddr string
	if transfer.Direction == DirectionIn {
		counterpartyAddr = fromAddr
	} else {
		counterpartyAddr = toAddr
	}

	// Check if counterparty is user's wallet
	isCounterpartyOurs := p.isUserWallet(ctx, counterpartyAddr)

	if transfer.Direction == DirectionIn {
		if isCounterpartyOurs {
			return TransferClassInternal
		}
		return TransferClassIncoming
	}

	if transfer.Direction == DirectionOut {
		if isCounterpartyOurs {
			return TransferClassInternal
		}
		return TransferClassOutgoing
	}

	// Fallback classification based on address matching
	if toAddr == walletAddr {
		return TransferClassIncoming
	}
	if fromAddr == walletAddr {
		return TransferClassOutgoing
	}

	return TransferClassUnknown
}

// isUserWallet checks if an address belongs to any of the user's wallets
func (p *Processor) isUserWallet(ctx context.Context, address string) bool {
	address = strings.ToLower(address)

	// Check cache first
	if _, ok := p.addressCache[address]; ok {
		return true
	}

	// Query database
	wallets, err := p.walletRepo.GetWalletsByAddress(ctx, address)
	if err != nil {
		p.logger.Error("failed to check wallet ownership", "address", address, "error", err)
		return false
	}

	if len(wallets) > 0 {
		// Cache the result
		ids := make([]uuid.UUID, len(wallets))
		for i, w := range wallets {
			ids[i] = w.ID
		}
		p.addressCache[address] = ids
		return true
	}

	return false
}

// getWalletByAddress returns the wallet ID for an address (if it exists)
func (p *Processor) getWalletByAddress(ctx context.Context, address string) *uuid.UUID {
	address = strings.ToLower(address)

	wallets, err := p.walletRepo.GetWalletsByAddress(ctx, address)
	if err != nil || len(wallets) == 0 {
		return nil
	}

	// Return the first matching wallet
	return &wallets[0].ID
}

// recordIncomingTransfer records a transfer_in transaction
func (p *Processor) recordIncomingTransfer(ctx context.Context, w *wallet.Wallet, transfer Transfer) error {
	// Get USD rate for the asset
	usdRate := p.getTransferUSDRate(ctx, transfer.AssetSymbol, transfer.ChainID)

	data := map[string]interface{}{
		"wallet_id":        w.ID.String(),
		"asset_id":         transfer.AssetSymbol,
		"decimals":         transfer.Decimals,
		"amount":           money.NewBigInt(transfer.Amount).String(),
		"usd_rate":         usdRate,
		"chain_id":         transfer.ChainID,
		"tx_hash":          transfer.TxHash,
		"block_number":     transfer.BlockNumber,
		"from_address":     transfer.From,
		"contract_address": transfer.ContractAddress,
		"occurred_at":      transfer.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
		"unique_id":        transfer.UniqueID,
	}

	// Record the transaction
	_, err := p.ledgerSvc.RecordTransaction(ctx, ledger.TxTypeTransferIn, "blockchain", &transfer.UniqueID, transfer.Timestamp, data)
	if err != nil {
		// Check if it's a duplicate (idempotency)
		if isDuplicateError(err) {
			p.logger.Debug("transfer already recorded (idempotent)", "unique_id", transfer.UniqueID)
			return nil
		}
		return fmt.Errorf("failed to record incoming transfer: %w", err)
	}

	return nil
}

// recordOutgoingTransfer records a transfer_out transaction
func (p *Processor) recordOutgoingTransfer(ctx context.Context, w *wallet.Wallet, transfer Transfer) error {
	// Get USD rate for the asset
	usdRate := p.getTransferUSDRate(ctx, transfer.AssetSymbol, transfer.ChainID)

	data := map[string]interface{}{
		"wallet_id":        w.ID.String(),
		"asset_id":         transfer.AssetSymbol,
		"decimals":         transfer.Decimals,
		"amount":           money.NewBigInt(transfer.Amount).String(),
		"usd_rate":         usdRate,
		"chain_id":         transfer.ChainID,
		"tx_hash":          transfer.TxHash,
		"block_number":     transfer.BlockNumber,
		"to_address":       transfer.To,
		"contract_address": transfer.ContractAddress,
		"occurred_at":      transfer.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
		"unique_id":        transfer.UniqueID,
	}

	// Record the transaction
	_, err := p.ledgerSvc.RecordTransaction(ctx, ledger.TxTypeTransferOut, "blockchain", &transfer.UniqueID, transfer.Timestamp, data)
	if err != nil {
		// Check if it's a duplicate (idempotency)
		if isDuplicateError(err) {
			p.logger.Debug("transfer already recorded (idempotent)", "unique_id", transfer.UniqueID)
			return nil
		}
		return fmt.Errorf("failed to record outgoing transfer: %w", err)
	}

	return nil
}

// recordInternalTransfer records an internal_transfer transaction
func (p *Processor) recordInternalTransfer(ctx context.Context, w *wallet.Wallet, transfer Transfer) error {
	// Determine source and destination wallets
	var sourceWalletID, destWalletID uuid.UUID

	if transfer.Direction == DirectionIn {
		// This wallet is receiving
		destWalletID = w.ID
		srcWallet := p.getWalletByAddress(ctx, transfer.From)
		if srcWallet == nil {
			// Counterparty not found, treat as regular incoming
			return p.recordIncomingTransfer(ctx, w, transfer)
		}
		sourceWalletID = *srcWallet
	} else {
		// This wallet is sending
		sourceWalletID = w.ID
		dstWallet := p.getWalletByAddress(ctx, transfer.To)
		if dstWallet == nil {
			// Counterparty not found, treat as regular outgoing
			return p.recordOutgoingTransfer(ctx, w, transfer)
		}
		destWalletID = *dstWallet
	}

	// Only record the transfer once (from the source wallet's perspective)
	// to avoid duplicate entries
	if transfer.Direction == DirectionIn {
		// Skip - the outgoing side will record this
		p.logger.Debug("skipping internal transfer (will be recorded from source)",
			"dest_wallet", destWalletID, "tx_hash", transfer.TxHash)
		return nil
	}

	// Get USD rate for the asset
	usdRate := p.getTransferUSDRate(ctx, transfer.AssetSymbol, transfer.ChainID)

	data := map[string]interface{}{
		"source_wallet_id": sourceWalletID.String(),
		"dest_wallet_id":   destWalletID.String(),
		"asset_id":         transfer.AssetSymbol,
		"decimals":         transfer.Decimals,
		"amount":           money.NewBigInt(transfer.Amount).String(),
		"usd_rate":         usdRate,
		"chain_id":         transfer.ChainID,
		"tx_hash":          transfer.TxHash,
		"block_number":     transfer.BlockNumber,
		"contract_address": transfer.ContractAddress,
		"occurred_at":      transfer.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
		"unique_id":        transfer.UniqueID,
	}

	// Record the transaction
	_, err := p.ledgerSvc.RecordTransaction(ctx, ledger.TxTypeInternalTransfer, "blockchain", &transfer.UniqueID, transfer.Timestamp, data)
	if err != nil {
		// Check if it's a duplicate (idempotency)
		if isDuplicateError(err) {
			p.logger.Debug("transfer already recorded (idempotent)", "unique_id", transfer.UniqueID)
			return nil
		}
		return fmt.Errorf("failed to record internal transfer: %w", err)
	}

	return nil
}

// isDuplicateError checks if the error is due to duplicate external_id
func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "duplicate") ||
		strings.Contains(errStr, "unique constraint") ||
		strings.Contains(errStr, "already exists")
}

// ClearCache clears the address cache
func (p *Processor) ClearCache() {
	p.addressCache = make(map[string][]uuid.UUID)
}

// getTransferUSDRate returns the USD rate for a transfer asset
// Returns "0" if price unavailable (graceful degradation)
func (p *Processor) getTransferUSDRate(ctx context.Context, symbol string, chainID int64) string {
	if p.assetSvc == nil {
		return "0"
	}

	price, err := p.assetSvc.GetPriceBySymbol(ctx, symbol, chainID)
	if err != nil || price == nil {
		p.logger.Debug("price unavailable for asset", "symbol", symbol, "chain_id", chainID)
		return "0"
	}

	return price.String()
}
