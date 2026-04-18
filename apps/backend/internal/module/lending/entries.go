package lending

import (
	"fmt"
	"math/big"
	"strings"
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

// --- Multi-asset aware entry generation ---
//
// The entry builders below take a LendingTransferItem and the parent
// LendingTransaction for shared fields (wallet_id, chain_id, protocol,
// tx_hash). They classify the item's asset via ClassifyLendingAsset and
// emit the balanced pair appropriate for the chosen lending op type:
//
//	| Op        | Liquid asset         | Receipt / debt token         |
//	| --------- | -------------------- | ----------------------------- |
//	| Supply    | DEBIT  collateral    | DEBIT  collateral            |
//	|           | CREDIT wallet        | CREDIT clearing.{protocol}   |
//	| Withdraw  | DEBIT  wallet        | DEBIT  clearing.{protocol}   |
//	|           | CREDIT collateral    | CREDIT collateral            |
//	| Borrow    | DEBIT  wallet        | DEBIT  clearing.{protocol}   |
//	|           | CREDIT liability     | CREDIT liability             |
//	| Repay     | DEBIT  liability     | DEBIT  liability             |
//	|           | CREDIT wallet        | CREDIT clearing.{protocol}   |
//	| Claim     | DEBIT  wallet        | DEBIT  wallet (rare)         |
//	|           | CREDIT income.lend.. | CREDIT income.lending.*      |
//
// Receipt/debt movements use a clearing account on the protocol side so
// the debit+credit pair balances without leaking into the user's liquid
// wallet balance. The wallet vs collateral/liability account is resolved
// by the ledger's accountResolver based on the `account_code` metadata.

// entryRouting captures the debit and credit side of a single balanced
// pair for a lending asset movement. Account types are inferred from each
// account code's prefix in buildLendingPair — no need to carry them here.
type entryRouting struct {
	debitAccount  string
	debitType     ledger.EntryType
	creditAccount string
	creditType    ledger.EntryType
}

// generateSupplyItemEntries emits entries for one transfer item of a supply op.
func generateSupplyItemEntries(txn *LendingTransaction, item *LendingTransferItem) []*ledger.Entry {
	walletID := txn.WalletID.String()
	chain := txn.ChainID
	proto := txn.Protocol

	role := ClassifyLendingAsset(item.AssetID, proto)
	switch role {
	case RoleCollateralReceipt:
		// Supply receipt (aToken) inbound — tracks the user's claim.
		return buildLendingPair(txn, item, entryRouting{
			debitAccount:  fmt.Sprintf("collateral.%s.%s.%s.%s", proto, walletID, chain, item.AssetID),
			debitType:     ledger.EntryTypeCollateralIncrease,
			creditAccount: fmt.Sprintf("clearing.lending.%s.%s", proto, chain),
			creditType:    ledger.EntryTypeClearing,
		})
	default:
		// Liquid asset outbound — the principal that leaves the wallet.
		return buildLendingPair(txn, item, entryRouting{
			debitAccount:  fmt.Sprintf("collateral.%s.%s.%s.%s", proto, walletID, chain, item.AssetID),
			debitType:     ledger.EntryTypeCollateralIncrease,
			creditAccount: fmt.Sprintf("wallet.%s.%s.%s", walletID, chain, item.AssetID),
			creditType:    ledger.EntryTypeAssetDecrease,
		})
	}
}

// generateWithdrawItemEntries emits entries for one transfer item of a withdraw op.
func generateWithdrawItemEntries(txn *LendingTransaction, item *LendingTransferItem) []*ledger.Entry {
	walletID := txn.WalletID.String()
	chain := txn.ChainID
	proto := txn.Protocol

	role := ClassifyLendingAsset(item.AssetID, proto)
	switch role {
	case RoleCollateralReceipt:
		// Supply receipt (aToken) outbound — burning the user's claim.
		return buildLendingPair(txn, item, entryRouting{
			debitAccount:  fmt.Sprintf("clearing.lending.%s.%s", proto, chain),
			debitType:     ledger.EntryTypeClearing,
			creditAccount: fmt.Sprintf("collateral.%s.%s.%s.%s", proto, walletID, chain, item.AssetID),
			creditType:    ledger.EntryTypeCollateralDecrease,
		})
	default:
		// Principal inbound to wallet.
		return buildLendingPair(txn, item, entryRouting{
			debitAccount:  fmt.Sprintf("wallet.%s.%s.%s", walletID, chain, item.AssetID),
			debitType:     ledger.EntryTypeAssetIncrease,
			creditAccount: fmt.Sprintf("collateral.%s.%s.%s.%s", proto, walletID, chain, item.AssetID),
			creditType:    ledger.EntryTypeCollateralDecrease,
		})
	}
}

// generateBorrowItemEntries emits entries for one transfer item of a borrow op.
// For borrow both the debt receipt token AND the liquid borrowed asset arrive
// as IN transfers; we route each to its correct account side.
func generateBorrowItemEntries(txn *LendingTransaction, item *LendingTransferItem) []*ledger.Entry {
	walletID := txn.WalletID.String()
	chain := txn.ChainID
	proto := txn.Protocol

	role := ClassifyLendingAsset(item.AssetID, proto)
	switch role {
	case RoleLiabilityReceipt:
		// Debt receipt inbound — tracks outstanding debt growing.
		return buildLendingPair(txn, item, entryRouting{
			debitAccount:  fmt.Sprintf("clearing.lending.%s.%s", proto, chain),
			debitType:     ledger.EntryTypeClearing,
			creditAccount: fmt.Sprintf("liability.%s.%s.%s.%s", proto, walletID, chain, item.AssetID),
			creditType:    ledger.EntryTypeLiabilityIncrease,
		})
	default:
		// Liquid borrowed asset inbound to wallet.
		return buildLendingPair(txn, item, entryRouting{
			debitAccount:  fmt.Sprintf("wallet.%s.%s.%s", walletID, chain, item.AssetID),
			debitType:     ledger.EntryTypeAssetIncrease,
			creditAccount: fmt.Sprintf("liability.%s.%s.%s.%s", proto, walletID, chain, item.AssetID),
			creditType:    ledger.EntryTypeLiabilityIncrease,
		})
	}
}

// generateRepayItemEntries emits entries for one transfer item of a repay op.
func generateRepayItemEntries(txn *LendingTransaction, item *LendingTransferItem) []*ledger.Entry {
	walletID := txn.WalletID.String()
	chain := txn.ChainID
	proto := txn.Protocol

	role := ClassifyLendingAsset(item.AssetID, proto)
	switch role {
	case RoleLiabilityReceipt:
		// Debt receipt burned — liability decreases.
		return buildLendingPair(txn, item, entryRouting{
			debitAccount:  fmt.Sprintf("liability.%s.%s.%s.%s", proto, walletID, chain, item.AssetID),
			debitType:     ledger.EntryTypeLiabilityDecrease,
			creditAccount: fmt.Sprintf("clearing.lending.%s.%s", proto, chain),
			creditType:    ledger.EntryTypeClearing,
		})
	default:
		// Liquid asset outbound from wallet (repayment).
		return buildLendingPair(txn, item, entryRouting{
			debitAccount:  fmt.Sprintf("liability.%s.%s.%s.%s", proto, walletID, chain, item.AssetID),
			debitType:     ledger.EntryTypeLiabilityDecrease,
			creditAccount: fmt.Sprintf("wallet.%s.%s.%s", walletID, chain, item.AssetID),
			creditType:    ledger.EntryTypeAssetDecrease,
		})
	}
}

// generateClaimItemEntries emits entries for one transfer item of a claim op.
// Rewards (liquid) flow to the wallet; the rare receipt adjustment goes through
// the clearing account.
func generateClaimItemEntries(txn *LendingTransaction, item *LendingTransferItem) []*ledger.Entry {
	walletID := txn.WalletID.String()
	chain := txn.ChainID
	proto := txn.Protocol

	role := ClassifyLendingAsset(item.AssetID, proto)
	switch role {
	case RoleCollateralReceipt, RoleLiabilityReceipt:
		return buildLendingPair(txn, item, entryRouting{
			debitAccount:  fmt.Sprintf("clearing.lending.%s.%s", proto, chain),
			debitType:     ledger.EntryTypeClearing,
			creditAccount: fmt.Sprintf("income.lending.%s.%s", chain, item.AssetID),
			creditType:    ledger.EntryTypeIncome,
		})
	default:
		return buildLendingPair(txn, item, entryRouting{
			debitAccount:  fmt.Sprintf("wallet.%s.%s.%s", walletID, chain, item.AssetID),
			debitType:     ledger.EntryTypeAssetIncrease,
			creditAccount: fmt.Sprintf("income.lending.%s.%s", chain, item.AssetID),
			creditType:    ledger.EntryTypeIncome,
		})
	}
}

// buildLendingPair turns an entryRouting and a LendingTransferItem into a
// balanced debit/credit pair. Metadata carries account_code for the resolver;
// per-side account_type is derived from the code prefix so wallet / collateral
// / liability / clearing accounts each get the correct Account.Type and the
// wallet-scoped sides are tagged with wallet_id.
func buildLendingPair(txn *LendingTransaction, item *LendingTransferItem, r entryRouting) []*ledger.Entry {
	amount := item.GetAmount()
	usdRate := item.GetUSDRate()
	usdValue := money.CalcUSDValue(amount, usdRate, item.Decimals)

	walletID := txn.WalletID.String()
	chain := txn.ChainID
	proto := txn.Protocol

	metaFor := func(accountCode string) map[string]interface{} {
		m := map[string]interface{}{
			"account_code":     accountCode,
			"tx_hash":          txn.TxHash,
			"chain_id":         chain,
			"protocol":         proto,
			"contract_address": item.ContractAddress,
		}
		switch {
		case strings.HasPrefix(accountCode, "wallet."):
			m["wallet_id"] = walletID
			// crypto_wallet is inferred from the prefix; no account_type needed.
		case strings.HasPrefix(accountCode, "collateral."):
			m["wallet_id"] = walletID
			m["account_type"] = "COLLATERAL"
		case strings.HasPrefix(accountCode, "liability."):
			m["wallet_id"] = walletID
			m["account_type"] = "LIABILITY"
		case strings.HasPrefix(accountCode, "clearing."):
			m["account_type"] = "CLEARING"
		case strings.HasPrefix(accountCode, "income."):
			m["account_type"] = "INCOME"
		case strings.HasPrefix(accountCode, "expense."):
			m["account_type"] = "EXPENSE"
		}
		return m
	}

	debit := &ledger.Entry{
		ID:          uuid.New(),
		DebitCredit: ledger.Debit,
		EntryType:   r.debitType,
		Amount:      new(big.Int).Set(amount),
		AssetID:     item.AssetID,
		USDRate:     new(big.Int).Set(usdRate),
		USDValue:    new(big.Int).Set(usdValue),
		OccurredAt:  txn.OccurredAt,
		CreatedAt:   time.Now().UTC(),
		Metadata:    metaFor(r.debitAccount),
	}
	credit := &ledger.Entry{
		ID:          uuid.New(),
		DebitCredit: ledger.Credit,
		EntryType:   r.creditType,
		Amount:      new(big.Int).Set(amount),
		AssetID:     item.AssetID,
		USDRate:     new(big.Int).Set(usdRate),
		USDValue:    new(big.Int).Set(usdValue),
		OccurredAt:  txn.OccurredAt,
		CreatedAt:   time.Now().UTC(),
		Metadata:    metaFor(r.creditAccount),
	}
	return []*ledger.Entry{debit, credit}
}
