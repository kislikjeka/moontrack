package defi

import (
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// generateSwapLikeEntries generates balanced ledger entries for DeFi deposit/withdraw.
// Both are swap-like: OUT assets leave wallet through clearing, IN assets enter wallet through clearing.
//
// For each OUT transfer:
//
//	CREDIT wallet.{wID}.{chain}.{asset}   (asset_decrease)
//	DEBIT  clearing.{chain}.{asset}       (clearing)
//
// For each IN transfer:
//
//	DEBIT  wallet.{wID}.{chain}.{asset}   (asset_increase)
//	CREDIT clearing.{chain}.{asset}       (clearing)
//
// Includes USD price fallback: if an IN transfer has usd_price=0 but OUT transfers have
// a price, compute the price from total OUT USD value / IN amount.
func generateSwapLikeEntries(txn *DeFiTransaction) []*ledger.Entry {
	transfersOut := txn.TransfersOut()
	transfersIn := txn.TransfersIn()

	entries := make([]*ledger.Entry, 0, 2*(len(transfersOut)+len(transfersIn)))

	walletIDStr := txn.WalletID.String()
	chainIDStr := txn.ChainID

	// Compute total OUT USD value for price fallback
	totalOutUSDValue := new(big.Int)
	for _, tr := range transfersOut {
		usdRate := big.NewInt(0)
		if tr.USDPrice != nil && !tr.USDPrice.IsNil() {
			usdRate = tr.USDPrice.ToBigInt()
		}
		usdValue := money.CalcUSDValue(tr.Amount.ToBigInt(), usdRate, tr.Decimals)
		totalOutUSDValue.Add(totalOutUSDValue, usdValue)
	}

	metadata := buildBaseMetadata(txn)

	// Outgoing transfers (asset leaving wallet)
	for _, tr := range transfersOut {
		amount := tr.Amount.ToBigInt()
		usdRate := big.NewInt(0)
		if tr.USDPrice != nil && !tr.USDPrice.IsNil() {
			usdRate = tr.USDPrice.ToBigInt()
		}
		usdValue := money.CalcUSDValue(amount, usdRate, tr.Decimals)

		entryMeta := mergeMetadata(metadata, map[string]interface{}{
			"wallet_id":        walletIDStr,
			"account_code":     fmt.Sprintf("wallet.%s.%s.%s", walletIDStr, chainIDStr, tr.AssetSymbol),
			"tx_hash":          txn.TxHash,
			"chain_id":         chainIDStr,
			"direction":        "out",
			"contract_address": tr.ContractAddress,
		})

		// CREDIT wallet (asset decrease)
		entries = append(entries, &ledger.Entry{
			ID:          uuid.New(),
			AccountID:   uuid.Nil,
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeAssetDecrease,
			Amount:      new(big.Int).Set(amount),
			AssetID:     tr.AssetSymbol,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    new(big.Int).Set(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata:    entryMeta,
		})

		clearingMeta := mergeMetadata(metadata, map[string]interface{}{
			"account_code": fmt.Sprintf("clearing.%s.%s", chainIDStr, tr.AssetSymbol),
			"account_type": "CLEARING",
			"chain_id":     chainIDStr,
			"tx_hash":      txn.TxHash,
			"direction":    "out",
		})

		// DEBIT clearing
		entries = append(entries, &ledger.Entry{
			ID:          uuid.New(),
			AccountID:   uuid.Nil,
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeClearing,
			Amount:      new(big.Int).Set(amount),
			AssetID:     tr.AssetSymbol,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    new(big.Int).Set(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata:    clearingMeta,
		})
	}

	// Incoming transfers (asset entering wallet)
	for _, tr := range transfersIn {
		amount := tr.Amount.ToBigInt()
		usdRate := big.NewInt(0)
		if tr.USDPrice != nil && !tr.USDPrice.IsNil() {
			usdRate = tr.USDPrice.ToBigInt()
		}

		// USD price fallback: if IN has no price but we have OUT value, compute it
		if usdRate.Sign() == 0 && totalOutUSDValue.Sign() > 0 && amount.Sign() > 0 {
			// usdRate = (totalOutUSDValue * 10^decimals) / amount
			scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(tr.Decimals)), nil)
			usdRate = new(big.Int).Mul(totalOutUSDValue, scale)
			usdRate.Div(usdRate, amount)
		}

		usdValue := money.CalcUSDValue(amount, usdRate, tr.Decimals)

		entryMeta := mergeMetadata(metadata, map[string]interface{}{
			"wallet_id":        walletIDStr,
			"account_code":     fmt.Sprintf("wallet.%s.%s.%s", walletIDStr, chainIDStr, tr.AssetSymbol),
			"tx_hash":          txn.TxHash,
			"chain_id":         chainIDStr,
			"direction":        "in",
			"contract_address": tr.ContractAddress,
		})

		// DEBIT wallet (asset increase)
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
			Metadata:    entryMeta,
		})

		clearingMeta := mergeMetadata(metadata, map[string]interface{}{
			"account_code": fmt.Sprintf("clearing.%s.%s", chainIDStr, tr.AssetSymbol),
			"account_type": "CLEARING",
			"chain_id":     chainIDStr,
			"tx_hash":      txn.TxHash,
			"direction":    "in",
		})

		// CREDIT clearing
		entries = append(entries, &ledger.Entry{
			ID:          uuid.New(),
			AccountID:   uuid.Nil,
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeClearing,
			Amount:      new(big.Int).Set(amount),
			AssetID:     tr.AssetSymbol,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    new(big.Int).Set(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata:    clearingMeta,
		})
	}

	return entries
}

// generateGasFeeEntries generates gas fee entries if the transaction has a fee.
//
//	DEBIT  gas.{chain}.{feeAsset}          (gas_fee)
//	CREDIT wallet.{wID}.{chain}.{feeAsset} (asset_decrease)
func generateGasFeeEntries(txn *DeFiTransaction) []*ledger.Entry {
	feeAmount := getFeeAmount(txn)
	if feeAmount == nil || feeAmount.Sign() <= 0 {
		return nil
	}

	feeUSDRate := big.NewInt(0)
	if txn.FeeUSDPrice != nil && !txn.FeeUSDPrice.IsNil() {
		feeUSDRate = txn.FeeUSDPrice.ToBigInt()
	}
	feeDecimals := txn.FeeDecimals
	if feeDecimals == 0 {
		feeDecimals = 18 // Default to 18 for native tokens
	}
	feeUSDValue := money.CalcUSDValue(feeAmount, feeUSDRate, feeDecimals)
	feeAsset := txn.FeeAsset

	walletIDStr := txn.WalletID.String()
	chainIDStr := txn.ChainID

	entries := make([]*ledger.Entry, 2)

	// DEBIT gas account (gas fee)
	entries[0] = &ledger.Entry{
		ID:          uuid.New(),
		AccountID:   uuid.Nil,
		DebitCredit: ledger.Debit,
		EntryType:   ledger.EntryTypeGasFee,
		Amount:      new(big.Int).Set(feeAmount),
		AssetID:     feeAsset,
		USDRate:     new(big.Int).Set(feeUSDRate),
		USDValue:    new(big.Int).Set(feeUSDValue),
		OccurredAt:  txn.OccurredAt,
		CreatedAt:   time.Now().UTC(),
		Metadata: map[string]interface{}{
			"account_code": fmt.Sprintf("gas.%s.%s", chainIDStr, feeAsset),
			"tx_hash":      txn.TxHash,
			"chain_id":     chainIDStr,
		},
	}

	// CREDIT wallet native asset (asset decrease for gas)
	entries[1] = &ledger.Entry{
		ID:          uuid.New(),
		AccountID:   uuid.Nil,
		DebitCredit: ledger.Credit,
		EntryType:   ledger.EntryTypeAssetDecrease,
		Amount:      new(big.Int).Set(feeAmount),
		AssetID:     feeAsset,
		USDRate:     new(big.Int).Set(feeUSDRate),
		USDValue:    new(big.Int).Set(feeUSDValue),
		OccurredAt:  txn.OccurredAt,
		CreatedAt:   time.Now().UTC(),
		Metadata: map[string]interface{}{
			"wallet_id":    walletIDStr,
			"account_code": fmt.Sprintf("wallet.%s.%s.%s", walletIDStr, chainIDStr, feeAsset),
			"tx_hash":      txn.TxHash,
			"chain_id":     chainIDStr,
			"entry_type":   "gas_payment",
		},
	}

	return entries
}

// getFeeAmount returns the fee amount as *big.Int, or nil if no fee
func getFeeAmount(txn *DeFiTransaction) *big.Int {
	if txn.FeeAmount == nil || txn.FeeAmount.IsNil() {
		return nil
	}
	return txn.FeeAmount.ToBigInt()
}

// buildBaseMetadata creates metadata common to all entries (operation_type, protocol)
func buildBaseMetadata(txn *DeFiTransaction) map[string]interface{} {
	meta := make(map[string]interface{})
	if txn.OperationType != "" {
		meta["operation_type"] = txn.OperationType
	}
	if txn.Protocol != "" {
		meta["protocol"] = txn.Protocol
	}
	return meta
}

// mergeMetadata creates a new map combining base metadata with entry-specific metadata
func mergeMetadata(base, specific map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(base)+len(specific))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range specific {
		result[k] = v
	}
	return result
}
