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
	"github.com/kislikjeka/moontrack/pkg/money"
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

// GenerateEntries generates ledger entries for an internal transfer transaction.
// Ledger entries generated (2-4 entries):
// 1. DEBIT wallet.{dest_wallet_id}.{dest_chain}.{asset_id} (asset_increase) - increases destination balance
// 2. CREDIT wallet.{src_wallet_id}.{source_chain}.{asset_id} (asset_decrease) - decreases source balance
// If gas fee is present:
// 3. DEBIT gas.{source_chain}.{native_asset} (gas_fee) - records gas expense
// 4. CREDIT wallet.{src_wallet_id}.{source_chain}.{native_asset} (asset_decrease) - decreases source native balance
//
// source_chain and dest_chain are equal for an ordinary internal transfer and
// differ for a bridge of the user's own funds across chains (ADR-0002).
func (h *InternalTransferHandler) GenerateEntries(ctx context.Context, txn *InternalTransferTransaction) ([]*ledger.Entry, error) {
	// USD rate for the transferred asset. nil means the price is not known yet
	// and stays nil into the entries (#74).
	usdRate := txn.GetUSDRate()
	usdValue := money.CalcUSDValue(txn.GetAmount(), usdRate, txn.Decimals)

	entries := make([]*ledger.Entry, 0, 4)

	// Each leg is booked against its OWN chain. For a same-chain transfer both
	// resolve to ChainID and nothing changes; for a bridge (ADR-0002) they
	// differ, and since the account code embeds the chain — and the ledger's
	// account resolver reads chain_id per entry — the two legs land on two
	// different accounts. That is what lets one transaction hold a source-chain
	// disposal and a destination-chain acquisition, so the TaxLotHook carries
	// the lot across the bridge instead of realizing phantom PnL.
	sourceChain := txn.SourceChain()
	destChain := txn.DestChain()

	// Each leg's identity is its own chain paired with its own registry UUID.
	// Building the destination code out of the destination chain and the SOURCE
	// asset's id is exactly #70: a code that is well-formed, addresses an
	// account that should not exist, and splits one asset across two of them.
	// accountcode.OnChain is what makes the mismatched pair unbuildable — the
	// two halves travel together from here on.
	sourceAsset := accountcode.OnChain(sourceChain, txn.AssetID)
	destAsset := accountcode.OnChain(destChain, txn.DestAsset())

	// The two principal legs are stamped as one pair so the TaxLotHook can
	// carry the cost basis from the disposal to the acquisition. It used to
	// find them by matching asset UUIDs, which stops working the moment the
	// legs carry their two rightful identities — and never distinguished the
	// gas leg, an asset decrease on the same wallet account in the same asset
	// whenever the native coin is what moved. See ledger.MetaLegPair.
	legPair := "transfer:" + txn.TxHash + ":" + txn.AssetID.String()

	// Entry 1: DEBIT destination wallet account (increases balance)
	entries = append(entries, &ledger.Entry{
		ID:          uuid.New(),
		AccountID:   uuid.Nil, // Will be resolved by AccountResolver
		DebitCredit: ledger.Debit,
		EntryType:   ledger.EntryTypeAssetIncrease,
		Amount:      new(big.Int).Set(txn.GetAmount()),
		AssetID:     txn.DestAsset(),
		USDRate:     money.CopyRate(usdRate),
		USDValue:    money.CopyRate(usdValue),
		OccurredAt:  txn.OccurredAt,
		CreatedAt:   time.Now().UTC(),
		Metadata: map[string]interface{}{
			"wallet_id":        txn.DestWalletID.String(),
			"account_code":     accountcode.Wallet(txn.DestWalletID, destAsset),
			"tx_hash":          txn.TxHash,
			"block_number":     txn.BlockNumber,
			"chain_id":         destChain,
			"transfer_type":    "internal_receive",
			"source_wallet_id": txn.SourceWalletID.String(),
			"contract_address": txn.ContractAddress,
			"unique_id":        txn.UniqueID,
			ledger.MetaLegPair: legPair,
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
		USDRate:     money.CopyRate(usdRate),
		USDValue:    money.CopyRate(usdValue),
		OccurredAt:  txn.OccurredAt,
		CreatedAt:   time.Now().UTC(),
		Metadata: map[string]interface{}{
			"wallet_id":        txn.SourceWalletID.String(),
			"account_code":     accountcode.Wallet(txn.SourceWalletID, sourceAsset),
			"tx_hash":          txn.TxHash,
			"block_number":     txn.BlockNumber,
			"chain_id":         sourceChain,
			"transfer_type":    "internal_send",
			"dest_wallet_id":   txn.DestWalletID.String(),
			"contract_address": txn.ContractAddress,
			"unique_id":        txn.UniqueID,
			ledger.MetaLegPair: legPair,
		},
	})

	// Add gas fee entries if gas is present
	gasAmount := txn.GetGasAmount()
	if gasAmount != nil && gasAmount.Sign() > 0 {
		gasUSDRate := txn.GetGasUSDRate()

		// Get gas decimals (default to 18 for native tokens)
		gasDecimals := txn.GasDecimals
		if gasDecimals == 0 {
			gasDecimals = 18
		}

		gasUSDValue := money.CalcUSDValue(gasAmount, gasUSDRate, gasDecimals)

		// No default here — see TransferOutHandler for why "ETH" was wrong on
		// every chain that is not Ethereum (#59).
		nativeAssetID := txn.NativeAssetID
		if nativeAssetID == uuid.Nil {
			return nil, ErrMissingNativeAsset
		}
		// Gas is burned on the source chain in that chain's native coin.
		nativeAsset := accountcode.OnChain(sourceChain, nativeAssetID)

		// Entries 3 and 4 both book to the SOURCE chain: gas is burned there,
		// in that chain's native token, and the destination chain never sees
		// this fee.
		//
		// Neither entry carries a leg-pair marker. Entry 4 is an asset decrease
		// on a wallet account, so it stands in the TaxLotHook's disposal set
		// beside the leg being moved — and when the native coin is what is being
		// bridged, in the very same asset. Leaving it unmarked is what stops it
		// from being offered as the source of the destination lot's basis.

		// Entry 3: DEBIT gas account (records gas expense)
		entries = append(entries, &ledger.Entry{
			ID:          uuid.New(),
			AccountID:   uuid.Nil,
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeGasFee,
			Amount:      new(big.Int).Set(gasAmount),
			AssetID:     nativeAssetID,
			USDRate:     money.CopyRate(gasUSDRate),
			USDValue:    money.CopyRate(gasUSDValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"account_code": accountcode.Gas(nativeAsset),
				"tx_hash":      txn.TxHash,
				"block_number": txn.BlockNumber,
				"chain_id":     sourceChain,
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
			USDRate:     money.CopyRate(gasUSDRate),
			USDValue:    money.CopyRate(gasUSDValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"wallet_id":    txn.SourceWalletID.String(),
				"account_code": accountcode.Wallet(txn.SourceWalletID, nativeAsset),
				"tx_hash":      txn.TxHash,
				"block_number": txn.BlockNumber,
				"chain_id":     sourceChain,
				"entry_type":   "gas_payment",
			},
		})
	}

	h.logger.Debug("transfer entries generated",
		"entry_count", len(entries),
		"asset_id", txn.AssetID,
		"source_chain", sourceChain,
		"dest_chain", destChain,
		"cross_chain", txn.IsCrossChain())

	return entries, nil
}
