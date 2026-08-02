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

// --- Entry generation ---
//
// The entry builders below take a LendingTransferItem and the parent
// LendingTransaction for shared fields (wallet_id, chain_id, protocol,
// tx_hash), and emit one balanced pair per item:
//
//	| Op        | Pair                                  |
//	| --------- | ------------------------------------- |
//	| Supply    | DEBIT collateral / CREDIT wallet       |
//	| Withdraw  | DEBIT wallet     / CREDIT collateral   |
//	| Borrow    | DEBIT wallet     / CREDIT liability    |
//	| Repay     | DEBIT liability  / CREDIT wallet       |
//	| Claim     | DEBIT wallet     / CREDIT income.lend. |
//
// Every leg is booked as the principal. Protocol receipts (aToken,
// variableDebt*) are not a separate position and must not reach the ledger
// at all; rejecting them is the job of the provider-side leg filter, not of
// these builders. The wallet vs collateral / liability account is resolved by
// the ledger's accountResolver based on the `account_code` metadata.

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
// The principal leaves the wallet and lands in collateral.
func generateSupplyItemEntries(txn *LendingTransaction, item *LendingTransferItem) []*ledger.Entry {
	walletID := txn.WalletID.String()

	return buildLendingPair(txn, item, entryRouting{
		debitAccount:  fmt.Sprintf("collateral.%s.%s.%s.%s", txn.Protocol, walletID, txn.ChainID, item.AssetID),
		debitType:     ledger.EntryTypeCollateralIncrease,
		creditAccount: fmt.Sprintf("wallet.%s.%s.%s", walletID, txn.ChainID, item.AssetID),
		creditType:    ledger.EntryTypeAssetDecrease,
	})
}

// generateWithdrawItemEntries emits entries for one transfer item of a withdraw op.
// The principal leaves collateral and lands back in the wallet.
func generateWithdrawItemEntries(txn *LendingTransaction, item *LendingTransferItem) []*ledger.Entry {
	walletID := txn.WalletID.String()

	return buildLendingPair(txn, item, entryRouting{
		debitAccount:  fmt.Sprintf("wallet.%s.%s.%s", walletID, txn.ChainID, item.AssetID),
		debitType:     ledger.EntryTypeAssetIncrease,
		creditAccount: fmt.Sprintf("collateral.%s.%s.%s.%s", txn.Protocol, walletID, txn.ChainID, item.AssetID),
		creditType:    ledger.EntryTypeCollateralDecrease,
	})
}

// generateBorrowItemEntries emits entries for one transfer item of a borrow op.
// The borrowed principal arrives in the wallet against a matching liability.
func generateBorrowItemEntries(txn *LendingTransaction, item *LendingTransferItem) []*ledger.Entry {
	walletID := txn.WalletID.String()

	return buildLendingPair(txn, item, entryRouting{
		debitAccount:  fmt.Sprintf("wallet.%s.%s.%s", walletID, txn.ChainID, item.AssetID),
		debitType:     ledger.EntryTypeAssetIncrease,
		creditAccount: fmt.Sprintf("liability.%s.%s.%s.%s", txn.Protocol, walletID, txn.ChainID, item.AssetID),
		creditType:    ledger.EntryTypeLiabilityIncrease,
	})
}

// generateRepayItemEntries emits entries for one transfer item of a repay op.
// The principal leaves the wallet and retires the matching liability.
func generateRepayItemEntries(txn *LendingTransaction, item *LendingTransferItem) []*ledger.Entry {
	walletID := txn.WalletID.String()

	return buildLendingPair(txn, item, entryRouting{
		debitAccount:  fmt.Sprintf("liability.%s.%s.%s.%s", txn.Protocol, walletID, txn.ChainID, item.AssetID),
		debitType:     ledger.EntryTypeLiabilityDecrease,
		creditAccount: fmt.Sprintf("wallet.%s.%s.%s", walletID, txn.ChainID, item.AssetID),
		creditType:    ledger.EntryTypeAssetDecrease,
	})
}

// generateClaimItemEntries emits entries for one transfer item of a claim op.
// Rewards arrive in the wallet against lending income.
func generateClaimItemEntries(txn *LendingTransaction, item *LendingTransferItem) []*ledger.Entry {
	walletID := txn.WalletID.String()

	return buildLendingPair(txn, item, entryRouting{
		debitAccount:  fmt.Sprintf("wallet.%s.%s.%s", walletID, txn.ChainID, item.AssetID),
		debitType:     ledger.EntryTypeAssetIncrease,
		creditAccount: fmt.Sprintf("income.lending.%s.%s", txn.ChainID, item.AssetID),
		creditType:    ledger.EntryTypeIncome,
	})
}

// buildLendingPair turns an entryRouting and a LendingTransferItem into a
// balanced debit/credit pair. Metadata carries account_code for the resolver;
// per-side account_type is derived from the code prefix so wallet / collateral
// / liability accounts each get the correct Account.Type and the wallet-scoped
// sides are tagged with wallet_id.
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
