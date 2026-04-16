package sync

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/asset"
	"github.com/kislikjeka/moontrack/internal/platform/lpposition"
	"github.com/kislikjeka/moontrack/internal/platform/price"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// AssetUpserter finds-or-creates an asset by on-chain identity.
type AssetUpserter interface {
	UpsertByOnChainIdentity(ctx context.Context, chainID, contractAddress, symbol, name string, decimals int) (*asset.Asset, bool, error)
}

// JobEnqueuer enqueues a price backfill job for a given asset and target time.
type JobEnqueuer interface {
	Enqueue(ctx context.Context, assetID uuid.UUID, targetTime time.Time) (*price.BackfillJob, error)
}

// ZerionProcessor handles decoded transaction classification and ledger recording
// for transactions fetched via the Zerion API.
type ZerionProcessor struct {
	walletRepo         WalletRepository
	ledgerSvc          LedgerService
	lpPositionSvc      LPPositionService
	lendingPositionSvc LendingPositionService
	assetUpsert        AssetUpserter
	jobEnqueuer        JobEnqueuer
	classifier         *Classifier
	logger             *logger.Logger
	addressCache       map[string][]uuid.UUID
}

// NewZerionProcessor creates a new ZerionProcessor.
func NewZerionProcessor(walletRepo WalletRepository, ledgerSvc LedgerService, lpPositionSvc LPPositionService, lendingPositionSvc LendingPositionService, log *logger.Logger, assetUpsert AssetUpserter, jobEnqueuer JobEnqueuer) *ZerionProcessor {
	return &ZerionProcessor{
		walletRepo:         walletRepo,
		ledgerSvc:          ledgerSvc,
		lpPositionSvc:      lpPositionSvc,
		lendingPositionSvc: lendingPositionSvc,
		assetUpsert:        assetUpsert,
		jobEnqueuer:        jobEnqueuer,
		classifier:         NewClassifier(),
		logger:             log,
		addressCache:       make(map[string][]uuid.UUID),
	}
}

// ProcessTransaction classifies a decoded transaction and records it to the ledger.
// Returns the ledger transaction ID on success, nil if skipped, or an error.
func (p *ZerionProcessor) ProcessTransaction(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) (*uuid.UUID, error) {
	if tx.Status == "failed" {
		p.logger.Debug("skipping failed transaction", "tx_hash", tx.TxHash)
		return nil, nil
	}

	txType := p.classifier.Classify(tx)
	p.logger.Debug("transaction classified", "tx_hash", tx.TxHash, "op_type", tx.OperationType, "tx_type", string(txType))

	if txType == "" {
		p.logger.Debug("skipping unclassifiable transaction", "tx_hash", tx.TxHash, "op_type", tx.OperationType)
		return nil, nil
	}

	txType, destWalletID := p.detectInternalTransfer(ctx, w, tx, txType)

	// Skip incoming side of internal transfers (recorded from outgoing side)
	if txType == ledger.TxTypeInternalTransfer && p.isIncomingSide(w, tx) {
		p.logger.Debug("skipping internal transfer (will be recorded from source)",
			"wallet_id", w.ID, "tx_hash", tx.TxHash)
		return nil, nil
	}

	var data map[string]interface{}
	externalID := tx.ID

	switch txType {
	case ledger.TxTypeTransferIn:
		data = p.buildTransferInData(w, tx)
	case ledger.TxTypeTransferOut:
		data = p.buildTransferOutData(w, tx)
	case ledger.TxTypeSwap:
		data = p.buildSwapData(ctx, w, tx)
	case ledger.TxTypeInternalTransfer:
		data = p.buildInternalTransferData(w, tx, destWalletID)
	case ledger.TxTypeDefiDeposit:
		data = p.buildDeFiDepositData(ctx, w, tx)
	case ledger.TxTypeDefiWithdraw:
		data = p.buildDeFiWithdrawData(ctx, w, tx)
	case ledger.TxTypeDefiClaim:
		data = p.buildDeFiClaimData(ctx, w, tx)
	case ledger.TxTypeLPDeposit:
		data = p.buildLPDepositData(ctx, w, tx)
	case ledger.TxTypeLPWithdraw:
		data = p.buildLPWithdrawData(ctx, w, tx)
	case ledger.TxTypeLPClaimFees:
		data = p.buildLPClaimFeesData(ctx, w, tx)
	case ledger.TxTypeLendingSupply:
		data = p.buildLendingSupplyData(w, tx)
	case ledger.TxTypeLendingWithdraw:
		data = p.buildLendingWithdrawData(w, tx)
	case ledger.TxTypeLendingBorrow:
		data = p.buildLendingBorrowData(w, tx)
	case ledger.TxTypeLendingRepay:
		data = p.buildLendingRepayData(w, tx)
	case ledger.TxTypeLendingClaim:
		data = p.buildLendingClaimData(w, tx)
	default:
		p.logger.Warn("unhandled transaction type", "type", txType, "tx_hash", tx.TxHash)
		return nil, nil
	}

	ledgerTx, err := p.ledgerSvc.RecordTransaction(ctx, txType, "zerion", &externalID, tx.MinedAt, data)
	if err != nil {
		if isDuplicateError(err) {
			p.logger.Debug("transaction already recorded (idempotent)", "external_id", externalID)
			return nil, nil
		}
		return nil, fmt.Errorf("failed to record transaction: %w", err)
	}

	p.logger.Debug("transaction recorded to ledger", "tx_hash", tx.TxHash, "tx_type", string(txType), "external_id", externalID)

	// Post-process LP transactions: update LP position aggregates
	if p.lpPositionSvc != nil {
		switch txType {
		case ledger.TxTypeLPDeposit:
			p.handleLPDeposit(ctx, w, tx)
		case ledger.TxTypeLPWithdraw:
			p.handleLPWithdraw(ctx, w, tx)
		case ledger.TxTypeLPClaimFees:
			p.handleLPClaimFees(ctx, w, tx)
		}
	}

	// Post-process lending transactions: update lending position aggregates
	if p.lendingPositionSvc != nil {
		switch txType {
		case ledger.TxTypeLendingSupply:
			p.handleLendingSupply(ctx, w, tx)
		case ledger.TxTypeLendingWithdraw:
			p.handleLendingWithdraw(ctx, w, tx)
		case ledger.TxTypeLendingBorrow:
			p.handleLendingBorrow(ctx, w, tx)
		case ledger.TxTypeLendingRepay:
			p.handleLendingRepay(ctx, w, tx)
		case ledger.TxTypeLendingClaim:
			p.handleLendingClaim(ctx, w, tx)
		}
	}

	return &ledgerTx.ID, nil
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

		// Bridge guard: if counterparty is the same address as the wallet,
		// it's a bridge (same address, different chain), not an internal transfer
		if strings.EqualFold(counterpartyAddr, w.Address) {
			continue
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

// --- LP position post-processing ---

func (p *ZerionProcessor) handleLPDeposit(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) {
	token0, token1 := p.extractTokenPair(tx.Transfers, DirectionOut)
	if token0.AssetSymbol == "" {
		p.logger.Warn("LP deposit: no outgoing transfers for token pair extraction", "tx_hash", tx.TxHash)
		return
	}

	chainID := tx.ChainID
	pos, err := p.lpPositionSvc.FindOrCreate(ctx, w.UserID, w.ID, chainID, tx.Protocol, tx.NFTTokenID, "",
		lpposition.TokenInfo{Symbol: token0.AssetSymbol, Contract: token0.ContractAddress, Decimals: token0.Decimals},
		lpposition.TokenInfo{Symbol: token1.AssetSymbol, Contract: token1.ContractAddress, Decimals: token1.Decimals},
		tx.MinedAt,
	)
	if err != nil {
		p.logger.Error("LP deposit: failed to find or create position", "tx_hash", tx.TxHash, "error", err)
		return
	}

	token0Amt, token1Amt, usdValue := p.calcLPAmounts(tx.Transfers, DirectionOut, pos)
	if err := p.lpPositionSvc.RecordDeposit(ctx, pos.ID, token0Amt, token1Amt, usdValue); err != nil {
		p.logger.Error("LP deposit: failed to record deposit", "tx_hash", tx.TxHash, "position_id", pos.ID, "error", err)
	}
}

func (p *ZerionProcessor) handleLPWithdraw(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) {
	token0, token1 := p.extractTokenPair(tx.Transfers, DirectionIn)
	if token0.AssetSymbol == "" {
		p.logger.Warn("LP withdraw: no incoming transfers for token pair extraction", "tx_hash", tx.TxHash)
		return
	}

	chainID := tx.ChainID

	// Try to find position by NFT token ID first, then by token pair
	var pos *lpposition.LPPosition
	var err error
	if tx.NFTTokenID != "" {
		pos, err = p.lpPositionSvc.FindOrCreate(ctx, w.UserID, w.ID, chainID, tx.Protocol, tx.NFTTokenID, "",
			lpposition.TokenInfo{Symbol: token0.AssetSymbol, Contract: token0.ContractAddress, Decimals: token0.Decimals},
			lpposition.TokenInfo{Symbol: token1.AssetSymbol, Contract: token1.ContractAddress, Decimals: token1.Decimals},
			tx.MinedAt,
		)
	} else {
		pos, err = p.lpPositionSvc.FindOpenByTokenPair(ctx, w.ID, chainID, tx.Protocol, token0.AssetSymbol, token1.AssetSymbol)
	}
	if err != nil {
		p.logger.Error("LP withdraw: failed to find position", "tx_hash", tx.TxHash, "error", err)
		return
	}
	if pos == nil {
		p.logger.Warn("LP withdraw: no open position found", "tx_hash", tx.TxHash, "token0", token0.AssetSymbol, "token1", token1.AssetSymbol)
		return
	}

	token0Amt, token1Amt, usdValue := p.calcLPAmounts(tx.Transfers, DirectionIn, pos)
	if err := p.lpPositionSvc.RecordWithdraw(ctx, pos.ID, token0Amt, token1Amt, usdValue); err != nil {
		p.logger.Error("LP withdraw: failed to record withdraw", "tx_hash", tx.TxHash, "position_id", pos.ID, "error", err)
	}
}

func (p *ZerionProcessor) handleLPClaimFees(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) {
	token0, token1 := p.extractTokenPair(tx.Transfers, DirectionIn)
	if token0.AssetSymbol == "" {
		p.logger.Warn("LP claim fees: no incoming transfers for token pair extraction", "tx_hash", tx.TxHash)
		return
	}

	chainID := tx.ChainID

	var pos *lpposition.LPPosition
	var err error
	if tx.NFTTokenID != "" {
		pos, err = p.lpPositionSvc.FindOrCreate(ctx, w.UserID, w.ID, chainID, tx.Protocol, tx.NFTTokenID, "",
			lpposition.TokenInfo{Symbol: token0.AssetSymbol, Contract: token0.ContractAddress, Decimals: token0.Decimals},
			lpposition.TokenInfo{Symbol: token1.AssetSymbol, Contract: token1.ContractAddress, Decimals: token1.Decimals},
			tx.MinedAt,
		)
	} else {
		pos, err = p.lpPositionSvc.FindOpenByTokenPair(ctx, w.ID, chainID, tx.Protocol, token0.AssetSymbol, token1.AssetSymbol)
	}
	if err != nil {
		p.logger.Error("LP claim fees: failed to find position", "tx_hash", tx.TxHash, "error", err)
		return
	}
	if pos == nil {
		p.logger.Warn("LP claim fees: no open position found", "tx_hash", tx.TxHash, "token0", token0.AssetSymbol, "token1", token1.AssetSymbol)
		return
	}

	token0Amt, token1Amt, usdValue := p.calcLPAmounts(tx.Transfers, DirectionIn, pos)
	if err := p.lpPositionSvc.RecordClaimFees(ctx, pos.ID, token0Amt, token1Amt, usdValue); err != nil {
		p.logger.Error("LP claim fees: failed to record claim", "tx_hash", tx.TxHash, "position_id", pos.ID, "error", err)
	}
}

// extractTokenPair extracts token0 and token1 from transfers matching the given direction.
// Returns up to two unique tokens. If only one token, token1 is returned with empty symbol.
func (p *ZerionProcessor) extractTokenPair(transfers []DecodedTransfer, dir TransferDirection) (DecodedTransfer, DecodedTransfer) {
	var token0, token1 DecodedTransfer
	seen := make(map[string]bool)
	for _, t := range transfers {
		if t.Direction != dir {
			continue
		}
		if !seen[t.AssetSymbol] {
			seen[t.AssetSymbol] = true
			if token0.AssetSymbol == "" {
				token0 = t
			} else {
				token1 = t
				break
			}
		}
	}
	return token0, token1
}

// calcLPAmounts calculates token0/token1 amounts and total USD value from transfers
// matching a given direction, mapped to the position's token pair.
func (p *ZerionProcessor) calcLPAmounts(transfers []DecodedTransfer, dir TransferDirection, pos *lpposition.LPPosition) (*big.Int, *big.Int, *big.Int) {
	token0Amt := big.NewInt(0)
	token1Amt := big.NewInt(0)
	usdValue := big.NewInt(0)

	for _, t := range transfers {
		if t.Direction != dir {
			continue
		}
		switch t.AssetSymbol {
		case pos.Token0Symbol:
			token0Amt.Add(token0Amt, t.Amount)
		case pos.Token1Symbol:
			token1Amt.Add(token1Amt, t.Amount)
		}
		if t.USDPrice != nil && t.Amount != nil {
			v := money.CalcUSDValue(t.Amount, t.USDPrice, t.Decimals)
			usdValue.Add(usdValue, v)
		}
	}

	return token0Amt, token1Amt, usdValue
}

// --- Raw data builders ---

func (p *ZerionProcessor) buildTransferInData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	// TransferInHandler expects flat fields, not a transfers array.
	// Find the primary "in" transfer.
	var t *DecodedTransfer
	for i := range tx.Transfers {
		if tx.Transfers[i].Direction == DirectionIn {
			t = &tx.Transfers[i]
			break
		}
	}
	if t == nil && len(tx.Transfers) > 0 {
		t = &tx.Transfers[0]
	}
	if t != nil {
		data["asset_id"] = t.AssetSymbol
		data["amount"] = money.NewBigInt(t.Amount).String()
		data["decimals"] = t.Decimals
		data["contract_address"] = t.ContractAddress
		data["from_address"] = t.Sender
		data["unique_id"] = tx.ID
		if t.USDPrice != nil {
			data["usd_rate"] = t.USDPrice.String()
		}
	}
	return data
}

func (p *ZerionProcessor) buildTransferOutData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	// TransferOutHandler expects flat fields, not a transfers array.
	// Find the primary "out" transfer.
	var t *DecodedTransfer
	for i := range tx.Transfers {
		if tx.Transfers[i].Direction == DirectionOut {
			t = &tx.Transfers[i]
			break
		}
	}
	if t == nil && len(tx.Transfers) > 0 {
		t = &tx.Transfers[0]
	}
	if t != nil {
		data["asset_id"] = t.AssetSymbol
		data["amount"] = money.NewBigInt(t.Amount).String()
		data["decimals"] = t.Decimals
		data["contract_address"] = t.ContractAddress
		data["to_address"] = t.Recipient
		data["unique_id"] = tx.ID
		if t.USDPrice != nil {
			data["usd_rate"] = t.USDPrice.String()
		}
	}
	// Map fee fields to gas fields expected by TransferOutHandler
	if feeAmt, ok := data["fee_amount"]; ok {
		data["gas_amount"] = feeAmt
	}
	if feeRate, ok := data["fee_usd_price"]; ok {
		data["gas_usd_rate"] = feeRate
	}
	return data
}

func (p *ZerionProcessor) buildSwapData(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)

	var transfersIn, transfersOut []map[string]interface{}
	for _, t := range tx.Transfers {
		td := p.buildSingleTransfer(ctx, t, tx.ChainID, tx.MinedAt)
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
	// InternalTransferHandler expects flat fields.
	// Extract the primary "out" transfer (from source wallet).
	var t *DecodedTransfer
	for i := range tx.Transfers {
		if tx.Transfers[i].Direction == DirectionOut {
			t = &tx.Transfers[i]
			break
		}
	}
	if t == nil && len(tx.Transfers) > 0 {
		t = &tx.Transfers[0]
	}
	if t != nil {
		data["asset_id"] = t.AssetSymbol
		data["amount"] = money.NewBigInt(t.Amount).String()
		data["decimals"] = t.Decimals
		data["contract_address"] = t.ContractAddress
		data["unique_id"] = tx.ID
		if t.USDPrice != nil {
			data["usd_rate"] = t.USDPrice.String()
		}
	}
	// Map fee fields to gas fields expected by InternalTransferHandler
	if feeAmt, ok := data["fee_amount"]; ok {
		data["gas_amount"] = feeAmt
	}
	if feeRate, ok := data["fee_usd_price"]; ok {
		data["gas_usd_rate"] = feeRate
	}
	if feeDec, ok := data["fee_decimals"]; ok {
		data["gas_decimals"] = feeDec
	}
	if feeAsset, ok := data["fee_asset"]; ok {
		data["native_asset_id"] = feeAsset
	}
	return data
}

func (p *ZerionProcessor) buildDeFiDepositData(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	data["transfers"] = p.buildTransferArray(ctx, tx.Transfers, tx.ChainID, tx.MinedAt)
	data["operation_type"] = string(tx.OperationType)
	return data
}

func (p *ZerionProcessor) buildDeFiWithdrawData(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	data["transfers"] = p.buildTransferArray(ctx, tx.Transfers, tx.ChainID, tx.MinedAt)
	data["operation_type"] = string(tx.OperationType)
	return data
}

func (p *ZerionProcessor) buildDeFiClaimData(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	data["transfers"] = p.buildTransferArray(ctx, tx.Transfers, tx.ChainID, tx.MinedAt)
	data["operation_type"] = string(tx.OperationType)
	return data
}

func (p *ZerionProcessor) buildLPDepositData(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	data["transfers"] = p.buildTransferArray(ctx, tx.Transfers, tx.ChainID, tx.MinedAt)
	data["operation_type"] = string(tx.OperationType)
	if tx.NFTTokenID != "" {
		data["nft_token_id"] = tx.NFTTokenID
	}
	return data
}

func (p *ZerionProcessor) buildLPWithdrawData(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	data["transfers"] = p.buildTransferArray(ctx, tx.Transfers, tx.ChainID, tx.MinedAt)
	data["operation_type"] = string(tx.OperationType)
	return data
}

func (p *ZerionProcessor) buildLPClaimFeesData(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	data["transfers"] = p.buildTransferArray(ctx, tx.Transfers, tx.ChainID, tx.MinedAt)
	data["operation_type"] = string(tx.OperationType)
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

func (p *ZerionProcessor) buildTransferArray(ctx context.Context, transfers []DecodedTransfer, chainID string, occurredAt time.Time) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(transfers))
	for _, t := range transfers {
		result = append(result, p.buildSingleTransfer(ctx, t, chainID, occurredAt))
	}
	return result
}

func (p *ZerionProcessor) buildSingleTransfer(ctx context.Context, t DecodedTransfer, chainID string, occurredAt time.Time) map[string]interface{} {
	m := map[string]interface{}{
		"asset_symbol":     t.AssetSymbol,
		"amount":           money.NewBigInt(t.Amount).String(),
		"decimals":         t.Decimals,
		"contract_address": t.ContractAddress,
		"direction":        string(t.Direction),
		"sender":           t.Sender,
		"recipient":        t.Recipient,
	}

	if t.USDPrice != nil {
		// Price provided by Zerion — use it directly.
		m["usd_price"] = t.USDPrice.String()
	} else if t.ContractAddress != "" && chainID != "" {
		// Zerion has no price for this token. Upsert the asset by on-chain identity
		// and enqueue a backfill job so the price is resolved later.
		if p.assetUpsert != nil {
			name := t.AssetName
			if name == "" {
				name = t.AssetSymbol
			}
			a, _, err := p.assetUpsert.UpsertByOnChainIdentity(ctx, chainID, t.ContractAddress, t.AssetSymbol, name, t.Decimals)
			if err == nil && a != nil && p.jobEnqueuer != nil {
				_, _ = p.jobEnqueuer.Enqueue(ctx, a.ID, occurredAt)
			}
		}
		// Omit usd_price key — downstream reads absence as nil USDRate (pending lot).
		m["usd_price_pending"] = true
	}
	// For native coins (ContractAddress == "") with no price: omit usd_price entirely.
	// Downstream handles missing key as nil USDRate.

	return m
}

// --- Lending data builders ---

func (p *ZerionProcessor) buildLendingSupplyData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	// Supply: the outgoing transfer is the asset being supplied
	if t := p.findTransfer(tx.Transfers, DirectionOut); t != nil {
		p.setLendingAssetFields(data, t)
	}
	return data
}

func (p *ZerionProcessor) buildLendingWithdrawData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	// Withdraw: the incoming transfer is the asset being withdrawn
	if t := p.findTransfer(tx.Transfers, DirectionIn); t != nil {
		p.setLendingAssetFields(data, t)
	}
	return data
}

func (p *ZerionProcessor) buildLendingBorrowData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	// Borrow: the incoming transfer is the borrowed asset
	if t := p.findTransfer(tx.Transfers, DirectionIn); t != nil {
		p.setLendingAssetFields(data, t)
	}
	return data
}

func (p *ZerionProcessor) buildLendingRepayData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	// Repay: the outgoing transfer is the asset being repaid
	if t := p.findTransfer(tx.Transfers, DirectionOut); t != nil {
		p.setLendingAssetFields(data, t)
	}
	return data
}

func (p *ZerionProcessor) buildLendingClaimData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	// Claim: the incoming transfer is the reward/interest
	if t := p.findTransfer(tx.Transfers, DirectionIn); t != nil {
		p.setLendingAssetFields(data, t)
	}
	return data
}

func (p *ZerionProcessor) findTransfer(transfers []DecodedTransfer, dir TransferDirection) *DecodedTransfer {
	for i := range transfers {
		if transfers[i].Direction == dir {
			return &transfers[i]
		}
	}
	if len(transfers) > 0 {
		return &transfers[0]
	}
	return nil
}

func (p *ZerionProcessor) setLendingAssetFields(data map[string]interface{}, t *DecodedTransfer) {
	data["asset"] = t.AssetSymbol
	data["amount"] = money.NewBigInt(t.Amount).String()
	data["decimals"] = t.Decimals
	data["contract_address"] = t.ContractAddress
	if t.USDPrice != nil {
		data["usd_price"] = t.USDPrice.String()
	}
}

// --- Lending position post-processing ---

func (p *ZerionProcessor) handleLendingSupply(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) {
	t := p.findTransfer(tx.Transfers, DirectionOut)
	if t == nil {
		p.logger.Warn("lending supply: no outgoing transfer", "tx_hash", tx.TxHash)
		return
	}

	pos, err := p.lendingPositionSvc.FindOrCreate(ctx, w.UserID, w.ID,
		p.lendingProtocol(tx), tx.ChainID, tx.MinedAt,
	)
	if err != nil {
		p.logger.Error("lending supply: failed to find or create position", "tx_hash", tx.TxHash, "error", err)
		return
	}

	usdValue := p.calcLendingUSD(t)
	if err := p.lendingPositionSvc.RecordSupply(ctx, pos.ID, t.AssetSymbol, t.Decimals, t.ContractAddress, t.Amount, usdValue); err != nil {
		p.logger.Error("lending supply: failed to record", "tx_hash", tx.TxHash, "position_id", pos.ID, "error", err)
	}
}

func (p *ZerionProcessor) handleLendingWithdraw(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) {
	t := p.findTransfer(tx.Transfers, DirectionIn)
	if t == nil {
		p.logger.Warn("lending withdraw: no incoming transfer", "tx_hash", tx.TxHash)
		return
	}

	pos, err := p.lendingPositionSvc.FindOrCreate(ctx, w.UserID, w.ID,
		p.lendingProtocol(tx), tx.ChainID, tx.MinedAt,
	)
	if err != nil {
		p.logger.Error("lending withdraw: failed to find position", "tx_hash", tx.TxHash, "error", err)
		return
	}

	usdValue := p.calcLendingUSD(t)
	if err := p.lendingPositionSvc.RecordWithdraw(ctx, pos.ID, t.AssetSymbol, t.Amount, usdValue); err != nil {
		p.logger.Error("lending withdraw: failed to record", "tx_hash", tx.TxHash, "position_id", pos.ID, "error", err)
	}
}

func (p *ZerionProcessor) handleLendingBorrow(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) {
	t := p.findTransfer(tx.Transfers, DirectionIn)
	if t == nil {
		p.logger.Warn("lending borrow: no incoming transfer", "tx_hash", tx.TxHash)
		return
	}

	pos, err := p.lendingPositionSvc.FindOrCreate(ctx, w.UserID, w.ID,
		p.lendingProtocol(tx), tx.ChainID, tx.MinedAt,
	)
	if err != nil {
		p.logger.Error("lending borrow: failed to find position", "tx_hash", tx.TxHash, "error", err)
		return
	}

	usdValue := p.calcLendingUSD(t)
	if err := p.lendingPositionSvc.RecordBorrow(ctx, pos.ID, t.AssetSymbol, t.Decimals, t.ContractAddress, t.Amount, usdValue); err != nil {
		p.logger.Error("lending borrow: failed to record", "tx_hash", tx.TxHash, "position_id", pos.ID, "error", err)
	}
}

func (p *ZerionProcessor) handleLendingRepay(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) {
	t := p.findTransfer(tx.Transfers, DirectionOut)
	if t == nil {
		p.logger.Warn("lending repay: no outgoing transfer", "tx_hash", tx.TxHash)
		return
	}

	pos, err := p.lendingPositionSvc.FindOrCreate(ctx, w.UserID, w.ID,
		p.lendingProtocol(tx), tx.ChainID, tx.MinedAt,
	)
	if err != nil {
		p.logger.Error("lending repay: failed to find position", "tx_hash", tx.TxHash, "error", err)
		return
	}

	usdValue := p.calcLendingUSD(t)
	if err := p.lendingPositionSvc.RecordRepay(ctx, pos.ID, t.AssetSymbol, t.Amount, usdValue); err != nil {
		p.logger.Error("lending repay: failed to record", "tx_hash", tx.TxHash, "position_id", pos.ID, "error", err)
	}
}

func (p *ZerionProcessor) handleLendingClaim(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) {
	t := p.findTransfer(tx.Transfers, DirectionIn)
	if t == nil {
		p.logger.Warn("lending claim: no incoming transfer", "tx_hash", tx.TxHash)
		return
	}

	pos, err := p.lendingPositionSvc.FindOrCreate(ctx, w.UserID, w.ID,
		p.lendingProtocol(tx), tx.ChainID, tx.MinedAt,
	)
	if err != nil {
		p.logger.Error("lending claim: failed to find position", "tx_hash", tx.TxHash, "error", err)
		return
	}

	usdValue := p.calcLendingUSD(t)
	if err := p.lendingPositionSvc.RecordClaim(ctx, pos.ID, usdValue); err != nil {
		p.logger.Error("lending claim: failed to record", "tx_hash", tx.TxHash, "position_id", pos.ID, "error", err)
	}
}

// lendingProtocol returns the protocol name for lending operations,
// defaulting to "AAVE" when Zerion doesn't tag the protocol.
func (p *ZerionProcessor) lendingProtocol(tx DecodedTransaction) string {
	if tx.Protocol != "" {
		return tx.Protocol
	}
	return "AAVE"
}

func (p *ZerionProcessor) calcLendingUSD(t *DecodedTransfer) *big.Int {
	if t.USDPrice != nil && t.Amount != nil {
		return new(big.Int).Mul(t.Amount, t.USDPrice)
	}
	return big.NewInt(0)
}
