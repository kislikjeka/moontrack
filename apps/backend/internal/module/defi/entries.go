package defi

import (
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/ledger/accountcode"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// transferAsset is the identity of the asset one transfer moves: the
// transaction's chain paired with the transfer's registry UUID (#59). The
// wallet leg and its clearing (or income) counterpart address the same asset,
// so the pair is formed once here instead of being re-assembled on each of the
// neighbouring lines — which is where the two halves drifted apart in #70.
func transferAsset(txn *DeFiTransaction, tr DeFiTransfer) accountcode.Asset {
	return accountcode.OnChain(txn.ChainID, tr.AssetID)
}

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
// Includes USD price fallback: if an IN transfer has no price but every OUT transfer
// has one, compute the price from total OUT USD value / IN amount.
func generateSwapLikeEntries(txn *DeFiTransaction) []*ledger.Entry {
	transfersOut := txn.TransfersOut()
	transfersIn := txn.TransfersIn()

	entries := make([]*ledger.Entry, 0, 2*(len(transfersOut)+len(transfersIn)))

	walletIDStr := txn.WalletID.String()
	chainIDStr := txn.ChainID

	// Compute total OUT USD value for price fallback.
	//
	// nil means "the total is not known" and is NOT the same as zero (#74, #77).
	// The total is only known when every OUT leg is priced: a partial sum
	// understates what left the wallet, and feeding it to the fallback below
	// would invent an IN rate that is confidently wrong. One unpriced OUT leg
	// therefore makes the whole total unknown, the fallback stays silent, and
	// the backfill worker resolves the IN price later.
	totalOutUSDValue := totalKnownOutUSDValue(transfersOut)

	metadata := buildBaseMetadata(txn)

	// Outgoing transfers (asset leaving wallet)
	for _, tr := range transfersOut {
		amount := tr.Amount.ToBigInt()
		usdRate := tr.USDPrice.ToBigInt()
		usdValue := money.CalcUSDValue(amount, usdRate, tr.Decimals)

		entryMeta := mergeMetadata(metadata, map[string]interface{}{
			"wallet_id":        walletIDStr,
			"account_code":     accountcode.Wallet(txn.WalletID, transferAsset(txn, tr)),
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
			AssetID:     tr.AssetID,
			USDRate:     money.CopyRate(usdRate),
			USDValue:    money.CopyRate(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata:    entryMeta,
		})

		clearingMeta := mergeMetadata(metadata, map[string]interface{}{
			"account_code": accountcode.Clearing(transferAsset(txn, tr)),
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
			AssetID:     tr.AssetID,
			USDRate:     money.CopyRate(usdRate),
			USDValue:    money.CopyRate(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata:    clearingMeta,
		})
	}

	// Incoming transfers (asset entering wallet)
	for _, tr := range transfersIn {
		amount := tr.Amount.ToBigInt()
		usdRate := tr.USDPrice.ToBigInt()

		// USD price fallback: if IN has no price but we have a known OUT value,
		// compute it. An unknown IN rate (nil) is a candidate for the fallback,
		// but only a known, positive OUT total may supply it — otherwise the
		// rate stays nil and travels to the lot as an honest "not known yet".
		if isUnknownOrZero(usdRate) && totalOutUSDValue != nil && totalOutUSDValue.Sign() > 0 && amount.Sign() > 0 {
			// usdRate = (totalOutUSDValue * 10^decimals) / amount
			scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(tr.Decimals)), nil)
			usdRate = new(big.Int).Mul(totalOutUSDValue, scale)
			usdRate.Div(usdRate, amount)
		}

		usdValue := money.CalcUSDValue(amount, usdRate, tr.Decimals)

		entryMeta := mergeMetadata(metadata, map[string]interface{}{
			"wallet_id":        walletIDStr,
			"account_code":     accountcode.Wallet(txn.WalletID, transferAsset(txn, tr)),
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
			AssetID:     tr.AssetID,
			USDRate:     money.CopyRate(usdRate),
			USDValue:    money.CopyRate(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata:    entryMeta,
		})

		clearingMeta := mergeMetadata(metadata, map[string]interface{}{
			"account_code": accountcode.Clearing(transferAsset(txn, tr)),
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
			AssetID:     tr.AssetID,
			USDRate:     money.CopyRate(usdRate),
			USDValue:    money.CopyRate(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata:    clearingMeta,
		})
	}

	return entries
}

// totalKnownOutUSDValue sums the USD value of the OUT legs, or returns nil when
// any leg's price is unknown.
//
// money.CalcUSDValue returns nil for an unknown rate (#74), and big.Int.Add
// panics on a nil operand — that panic killed the backend process mid-resync
// (#77). Short-circuiting to nil fixes the crash and states the right thing:
// a total that is missing one of its terms is unknown, not partial.
func totalKnownOutUSDValue(transfersOut []DeFiTransfer) *big.Int {
	total := new(big.Int)
	for _, tr := range transfersOut {
		usdValue := money.CalcUSDValue(tr.Amount.ToBigInt(), tr.USDPrice.ToBigInt(), tr.Decimals)
		if usdValue == nil {
			return nil
		}
		total.Add(total, usdValue)
	}
	return total
}

// isUnknownOrZero reports whether a rate carries no usable price — either
// unknown (nil) or an explicit zero. Both are eligible for the OUT-side
// fallback; neither may be dereferenced without this check.
func isUnknownOrZero(rate *big.Int) bool {
	return rate == nil || rate.Sign() == 0
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

	feeUSDRate := txn.FeeUSDPrice.ToBigInt()
	feeDecimals := txn.FeeDecimals
	if feeDecimals == 0 {
		feeDecimals = 18 // Default to 18 for native tokens
	}
	feeUSDValue := money.CalcUSDValue(feeAmount, feeUSDRate, feeDecimals)
	// Gas account and the wallet's native leg key on the fee asset's registry
	// UUID (#59); the fee ticker is display data and keys nothing.
	feeAsset := txn.FeeAsset
	feeIdentity := accountcode.OnChain(txn.ChainID, feeAsset)

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
		USDRate:     money.CopyRate(feeUSDRate),
		USDValue:    money.CopyRate(feeUSDValue),
		OccurredAt:  txn.OccurredAt,
		CreatedAt:   time.Now().UTC(),
		Metadata: map[string]interface{}{
			"account_code": accountcode.Gas(feeIdentity),
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
		USDRate:     money.CopyRate(feeUSDRate),
		USDValue:    money.CopyRate(feeUSDValue),
		OccurredAt:  txn.OccurredAt,
		CreatedAt:   time.Now().UTC(),
		Metadata: map[string]interface{}{
			"wallet_id":    walletIDStr,
			"account_code": accountcode.Wallet(txn.WalletID, feeIdentity),
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
