package sync

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// ZerionProcessor handles decoded transaction classification and ledger recording
// for transactions fetched via the Zerion API.
type ZerionProcessor struct {
	walletRepo   WalletRepository
	ledgerSvc    LedgerService
	classifier   *Classifier
	logger       *logger.Logger
	addressCache map[string][]uuid.UUID
}

// NewZerionProcessor creates a new ZerionProcessor.
func NewZerionProcessor(walletRepo WalletRepository, ledgerSvc LedgerService, logger *logger.Logger) *ZerionProcessor {
	return &ZerionProcessor{
		walletRepo:   walletRepo,
		ledgerSvc:    ledgerSvc,
		classifier:   NewClassifier(),
		logger:       logger,
		addressCache: make(map[string][]uuid.UUID),
	}
}

// ProcessTransaction classifies a decoded transaction and records it to the ledger.
func (p *ZerionProcessor) ProcessTransaction(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) error {
	if tx.Status == "failed" {
		p.logger.Debug("skipping failed transaction", "tx_hash", tx.TxHash)
		return nil
	}

	txType := p.classifier.Classify(tx)
	p.logger.Debug("transaction classified", "tx_hash", tx.TxHash, "op_type", tx.OperationType, "tx_type", string(txType))

	if txType == "" {
		p.logger.Debug("skipping unclassifiable transaction", "tx_hash", tx.TxHash, "op_type", tx.OperationType)
		return nil
	}

	txType, destWalletID := p.detectInternalTransfer(ctx, w, tx, txType)

	// Skip incoming side of internal transfers (recorded from outgoing side)
	if txType == ledger.TxTypeInternalTransfer && p.isIncomingSide(w, tx) {
		p.logger.Debug("skipping internal transfer (will be recorded from source)",
			"wallet_id", w.ID, "tx_hash", tx.TxHash)
		return nil
	}

	var data map[string]interface{}
	externalID := tx.ID

	switch txType {
	case ledger.TxTypeTransferIn:
		data = p.buildTransferInData(w, tx)
	case ledger.TxTypeTransferOut:
		data = p.buildTransferOutData(w, tx)
	case ledger.TxTypeSwap:
		data = p.buildSwapData(w, tx)
	case ledger.TxTypeInternalTransfer:
		data = p.buildInternalTransferData(w, tx, destWalletID)
	case ledger.TxTypeDefiDeposit:
		data = p.buildDeFiDepositData(w, tx)
	case ledger.TxTypeDefiWithdraw:
		data = p.buildDeFiWithdrawData(w, tx)
	case ledger.TxTypeDefiClaim:
		data = p.buildDeFiClaimData(w, tx)
	default:
		p.logger.Warn("unhandled transaction type", "type", txType, "tx_hash", tx.TxHash)
		return nil
	}

	_, err := p.ledgerSvc.RecordTransaction(ctx, txType, "zerion", &externalID, tx.MinedAt, data)
	if err != nil {
		if isDuplicateError(err) {
			p.logger.Debug("transaction already recorded (idempotent)", "external_id", externalID)
			return nil
		}
		return fmt.Errorf("failed to record transaction: %w", err)
	}

	p.logger.Debug("transaction recorded to ledger", "tx_hash", tx.TxHash, "tx_type", string(txType), "external_id", externalID)

	return nil
}

// detectInternalTransfer checks if a transfer_in/transfer_out is actually an internal
// transfer between user wallets.
func (p *ZerionProcessor) detectInternalTransfer(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction, txType ledger.TransactionType) (ledger.TransactionType, *uuid.UUID) {
	if txType != ledger.TxTypeTransferIn && txType != ledger.TxTypeTransferOut {
		return txType, nil
	}

	for _, t := range tx.Transfers {
		var counterpartyAddr string
		if t.Direction == DirectionIn {
			counterpartyAddr = t.Sender
		} else {
			counterpartyAddr = t.Recipient
		}

		if p.isUserWallet(ctx, counterpartyAddr, w.UserID) {
			destID := p.getWalletByAddress(ctx, counterpartyAddr, w.UserID)
			if destID != nil {
				p.logger.Debug("internal transfer detected", "tx_hash", tx.TxHash, "source_wallet", w.ID, "dest_wallet", *destID)
			}
			return ledger.TxTypeInternalTransfer, destID
		}
	}

	return txType, nil
}

// isIncomingSide checks if the wallet is on the receiving side of transfers.
func (p *ZerionProcessor) isIncomingSide(w *wallet.Wallet, tx DecodedTransaction) bool {
	walletAddr := strings.ToLower(w.Address)
	for _, t := range tx.Transfers {
		if t.Direction == DirectionIn && strings.ToLower(t.Recipient) == walletAddr {
			return true
		}
	}
	return false
}

// isUserWallet checks if an address belongs to any of the user's wallets.
func (p *ZerionProcessor) isUserWallet(ctx context.Context, address string, userID uuid.UUID) bool {
	address = strings.ToLower(address)
	if _, ok := p.addressCache[address]; ok {
		return true
	}
	wallets, err := p.walletRepo.GetWalletsByAddressAndUserID(ctx, address, userID)
	if err != nil {
		p.logger.Error("failed to check wallet ownership", "address", address, "error", err)
		return false
	}
	if len(wallets) > 0 {
		ids := make([]uuid.UUID, len(wallets))
		for i, w := range wallets {
			ids[i] = w.ID
		}
		p.addressCache[address] = ids
		return true
	}
	return false
}

// getWalletByAddress returns the wallet ID for an address belonging to a specific user.
func (p *ZerionProcessor) getWalletByAddress(ctx context.Context, address string, userID uuid.UUID) *uuid.UUID {
	address = strings.ToLower(address)
	wallets, err := p.walletRepo.GetWalletsByAddressAndUserID(ctx, address, userID)
	if err != nil || len(wallets) == 0 {
		return nil
	}
	return &wallets[0].ID
}

// ClearCache clears the address cache.
func (p *ZerionProcessor) ClearCache() {
	p.addressCache = make(map[string][]uuid.UUID)
}

// --- Raw data builders ---

func (p *ZerionProcessor) buildTransferInData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	data["transfers"] = p.buildTransferArray(tx.Transfers)
	return data
}

func (p *ZerionProcessor) buildTransferOutData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	data["transfers"] = p.buildTransferArray(tx.Transfers)
	return data
}

func (p *ZerionProcessor) buildSwapData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)

	var transfersIn, transfersOut []map[string]interface{}
	for _, t := range tx.Transfers {
		td := p.buildSingleTransfer(t)
		if t.Direction == DirectionIn {
			transfersIn = append(transfersIn, td)
		} else {
			transfersOut = append(transfersOut, td)
		}
	}
	data["transfers_in"] = transfersIn
	data["transfers_out"] = transfersOut
	return data
}

func (p *ZerionProcessor) buildInternalTransferData(w *wallet.Wallet, tx DecodedTransaction, destWalletID *uuid.UUID) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	data["source_wallet_id"] = w.ID.String()
	if destWalletID != nil {
		data["dest_wallet_id"] = destWalletID.String()
	}
	data["transfers"] = p.buildTransferArray(tx.Transfers)
	return data
}

func (p *ZerionProcessor) buildDeFiDepositData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	data["transfers"] = p.buildTransferArray(tx.Transfers)
	return data
}

func (p *ZerionProcessor) buildDeFiWithdrawData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	data["transfers"] = p.buildTransferArray(tx.Transfers)
	return data
}

func (p *ZerionProcessor) buildDeFiClaimData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	data["transfers"] = p.buildTransferArray(tx.Transfers)
	return data
}

func (p *ZerionProcessor) buildBaseData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := map[string]interface{}{
		"wallet_id":   w.ID.String(),
		"tx_hash":     tx.TxHash,
		"chain_id":    tx.ChainID,
		"occurred_at": tx.MinedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if tx.Protocol != "" {
		data["protocol"] = tx.Protocol
	}
	if tx.Fee != nil {
		data["fee_asset"] = tx.Fee.AssetSymbol
		data["fee_amount"] = money.NewBigInt(tx.Fee.Amount).String()
		data["fee_decimals"] = tx.Fee.Decimals
		if tx.Fee.USDPrice != nil {
			data["fee_usd_price"] = tx.Fee.USDPrice.String()
		} else {
			data["fee_usd_price"] = "0"
		}
	}
	return data
}

func (p *ZerionProcessor) buildTransferArray(transfers []DecodedTransfer) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(transfers))
	for _, t := range transfers {
		result = append(result, p.buildSingleTransfer(t))
	}
	return result
}

func (p *ZerionProcessor) buildSingleTransfer(t DecodedTransfer) map[string]interface{} {
	usdPrice := "0"
	if t.USDPrice != nil {
		usdPrice = t.USDPrice.String()
	}
	return map[string]interface{}{
		"asset_symbol":     t.AssetSymbol,
		"amount":           money.NewBigInt(t.Amount).String(),
		"decimals":         t.Decimals,
		"contract_address": t.ContractAddress,
		"direction":        string(t.Direction),
		"usd_price":        usdPrice,
		"sender":           t.Sender,
		"recipient":        t.Recipient,
	}
}
