package lending

import (
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// generateSupplyEntries generates entries for lending supply: wallet → collateral.
//
//	DEBIT  collateral.{protocol}.{wID}.{chain}.{asset}  (collateral_increase)
//	CREDIT wallet.{wID}.{chain}.{asset}                 (asset_decrease)
func generateSupplyEntries(txn *LendingTransaction) []*ledger.Entry {
	amount := txn.Amount.ToBigInt()
	usdRate, usdValue := calcUSD(txn)

	walletID := txn.WalletID.String()
	chain := txn.ChainID

	return []*ledger.Entry{
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeCollateralIncrease,
			Amount:      new(big.Int).Set(amount),
			AssetID:     txn.Asset,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    new(big.Int).Set(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"wallet_id":        walletID,
				"account_code":     fmt.Sprintf("collateral.%s.%s.%s.%s", txn.Protocol, walletID, chain, txn.Asset),
				"account_type":     "COLLATERAL",
				"tx_hash":          txn.TxHash,
				"chain_id":         chain,
				"protocol":         txn.Protocol,
				"contract_address": txn.ContractAddress,
			},
		},
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeAssetDecrease,
			Amount:      new(big.Int).Set(amount),
			AssetID:     txn.Asset,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    new(big.Int).Set(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"wallet_id":    walletID,
				"account_code": fmt.Sprintf("wallet.%s.%s.%s", walletID, chain, txn.Asset),
				"tx_hash":      txn.TxHash,
				"chain_id":     chain,
				"protocol":     txn.Protocol,
			},
		},
	}
}

// generateWithdrawEntries generates entries for lending withdraw: collateral → wallet.
//
//	DEBIT  wallet.{wID}.{chain}.{asset}                 (asset_increase)
//	CREDIT collateral.{protocol}.{wID}.{chain}.{asset}  (collateral_decrease)
func generateWithdrawEntries(txn *LendingTransaction) []*ledger.Entry {
	amount := txn.Amount.ToBigInt()
	usdRate, usdValue := calcUSD(txn)

	walletID := txn.WalletID.String()
	chain := txn.ChainID

	return []*ledger.Entry{
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeAssetIncrease,
			Amount:      new(big.Int).Set(amount),
			AssetID:     txn.Asset,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    new(big.Int).Set(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"wallet_id":    walletID,
				"account_code": fmt.Sprintf("wallet.%s.%s.%s", walletID, chain, txn.Asset),
				"tx_hash":      txn.TxHash,
				"chain_id":     chain,
				"protocol":     txn.Protocol,
			},
		},
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeCollateralDecrease,
			Amount:      new(big.Int).Set(amount),
			AssetID:     txn.Asset,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    new(big.Int).Set(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"wallet_id":        walletID,
				"account_code":     fmt.Sprintf("collateral.%s.%s.%s.%s", txn.Protocol, walletID, chain, txn.Asset),
				"account_type":     "COLLATERAL",
				"tx_hash":          txn.TxHash,
				"chain_id":         chain,
				"protocol":         txn.Protocol,
				"contract_address": txn.ContractAddress,
			},
		},
	}
}

// generateBorrowEntries generates entries for lending borrow: liability → wallet.
//
//	DEBIT  wallet.{wID}.{chain}.{asset}                (asset_increase)
//	CREDIT liability.{protocol}.{wID}.{chain}.{asset}  (liability_increase)
func generateBorrowEntries(txn *LendingTransaction) []*ledger.Entry {
	amount := txn.Amount.ToBigInt()
	usdRate, usdValue := calcUSD(txn)

	walletID := txn.WalletID.String()
	chain := txn.ChainID

	return []*ledger.Entry{
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeAssetIncrease,
			Amount:      new(big.Int).Set(amount),
			AssetID:     txn.Asset,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    new(big.Int).Set(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"wallet_id":    walletID,
				"account_code": fmt.Sprintf("wallet.%s.%s.%s", walletID, chain, txn.Asset),
				"tx_hash":      txn.TxHash,
				"chain_id":     chain,
				"protocol":     txn.Protocol,
			},
		},
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeLiabilityIncrease,
			Amount:      new(big.Int).Set(amount),
			AssetID:     txn.Asset,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    new(big.Int).Set(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"wallet_id":        walletID,
				"account_code":     fmt.Sprintf("liability.%s.%s.%s.%s", txn.Protocol, walletID, chain, txn.Asset),
				"account_type":     "LIABILITY",
				"tx_hash":          txn.TxHash,
				"chain_id":         chain,
				"protocol":         txn.Protocol,
				"contract_address": txn.ContractAddress,
			},
		},
	}
}

// generateRepayEntries generates entries for lending repay: wallet → liability.
//
//	DEBIT  liability.{protocol}.{wID}.{chain}.{asset}  (liability_decrease)
//	CREDIT wallet.{wID}.{chain}.{asset}                (asset_decrease)
func generateRepayEntries(txn *LendingTransaction) []*ledger.Entry {
	amount := txn.Amount.ToBigInt()
	usdRate, usdValue := calcUSD(txn)

	walletID := txn.WalletID.String()
	chain := txn.ChainID

	return []*ledger.Entry{
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeLiabilityDecrease,
			Amount:      new(big.Int).Set(amount),
			AssetID:     txn.Asset,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    new(big.Int).Set(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"wallet_id":        walletID,
				"account_code":     fmt.Sprintf("liability.%s.%s.%s.%s", txn.Protocol, walletID, chain, txn.Asset),
				"account_type":     "LIABILITY",
				"tx_hash":          txn.TxHash,
				"chain_id":         chain,
				"protocol":         txn.Protocol,
				"contract_address": txn.ContractAddress,
			},
		},
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeAssetDecrease,
			Amount:      new(big.Int).Set(amount),
			AssetID:     txn.Asset,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    new(big.Int).Set(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"wallet_id":    walletID,
				"account_code": fmt.Sprintf("wallet.%s.%s.%s", walletID, chain, txn.Asset),
				"tx_hash":      txn.TxHash,
				"chain_id":     chain,
				"protocol":     txn.Protocol,
			},
		},
	}
}

// generateClaimEntries generates entries for lending claim (rewards/interest): income → wallet.
//
//	DEBIT  wallet.{wID}.{chain}.{asset}           (asset_increase)
//	CREDIT income.lending.{chain}.{asset}          (income)
func generateClaimEntries(txn *LendingTransaction) []*ledger.Entry {
	amount := txn.Amount.ToBigInt()
	usdRate, usdValue := calcUSD(txn)

	walletID := txn.WalletID.String()
	chain := txn.ChainID

	return []*ledger.Entry{
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeAssetIncrease,
			Amount:      new(big.Int).Set(amount),
			AssetID:     txn.Asset,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    new(big.Int).Set(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"wallet_id":    walletID,
				"account_code": fmt.Sprintf("wallet.%s.%s.%s", walletID, chain, txn.Asset),
				"tx_hash":      txn.TxHash,
				"chain_id":     chain,
				"protocol":     txn.Protocol,
			},
		},
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeIncome,
			Amount:      new(big.Int).Set(amount),
			AssetID:     txn.Asset,
			USDRate:     new(big.Int).Set(usdRate),
			USDValue:    new(big.Int).Set(usdValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"account_code": fmt.Sprintf("income.lending.%s.%s", chain, txn.Asset),
				"tx_hash":      txn.TxHash,
				"chain_id":     chain,
				"protocol":     txn.Protocol,
			},
		},
	}
}

// generateGasFeeEntries generates gas fee entries if the transaction has a fee.
//
//	DEBIT  gas.{chain}.{feeAsset}          (gas_fee)
//	CREDIT wallet.{wID}.{chain}.{feeAsset} (asset_decrease)
func generateGasFeeEntries(txn *LendingTransaction) []*ledger.Entry {
	if txn.FeeAmount == nil || txn.FeeAmount.IsNil() || txn.FeeAmount.Sign() <= 0 {
		return nil
	}

	feeAmount := txn.FeeAmount.ToBigInt()
	feeUSDRate := big.NewInt(0)
	if txn.FeeUSDPrice != nil && !txn.FeeUSDPrice.IsNil() {
		feeUSDRate = txn.FeeUSDPrice.ToBigInt()
	}
	feeDecimals := txn.FeeDecimals
	if feeDecimals == 0 {
		feeDecimals = 18
	}
	feeUSDValue := money.CalcUSDValue(feeAmount, feeUSDRate, feeDecimals)

	walletID := txn.WalletID.String()
	chain := txn.ChainID
	feeAsset := txn.FeeAsset

	return []*ledger.Entry{
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeGasFee,
			Amount:      new(big.Int).Set(feeAmount),
			AssetID:     feeAsset,
			USDRate:     new(big.Int).Set(feeUSDRate),
			USDValue:    new(big.Int).Set(feeUSDValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"account_code": fmt.Sprintf("gas.%s.%s", chain, feeAsset),
				"tx_hash":      txn.TxHash,
				"chain_id":     chain,
			},
		},
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeAssetDecrease,
			Amount:      new(big.Int).Set(feeAmount),
			AssetID:     feeAsset,
			USDRate:     new(big.Int).Set(feeUSDRate),
			USDValue:    new(big.Int).Set(feeUSDValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"wallet_id":    walletID,
				"account_code": fmt.Sprintf("wallet.%s.%s.%s", walletID, chain, feeAsset),
				"tx_hash":      txn.TxHash,
				"chain_id":     chain,
				"entry_type":   "gas_payment",
			},
		},
	}
}

// calcUSD extracts the USD rate and computes USD value for the transaction.
func calcUSD(txn *LendingTransaction) (*big.Int, *big.Int) {
	usdRate := big.NewInt(0)
	if txn.USDPrice != nil && !txn.USDPrice.IsNil() {
		usdRate = txn.USDPrice.ToBigInt()
	}
	usdValue := money.CalcUSDValue(txn.Amount.ToBigInt(), usdRate, txn.Decimals)
	return usdRate, usdValue
}
