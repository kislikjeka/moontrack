package lending

import (
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/ledger/accountcode"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// generateGasFeeEntries generates gas fee entries if the transaction has a fee.
//
//	DEBIT  gas.{chain}.{feeAsset}          (gas_fee)
//	CREDIT wallet.{wID}.{chain}.{feeAsset} (asset_decrease)
//
// Neither entry carries a leg-pair marker, and that omission is load-bearing.
// The credit is an asset DECREASE on a wallet account, so it stands in the
// TaxLotHook's disposal set beside the principal being supplied — and in the
// same asset whenever the fee is paid in the coin being supplied, which the
// production data already contains. Marked, it could hand its consumed lot to
// the collateral acquisition; unmarked, it can only ever consume its own lot.
// Until now correctness there rested on FIFO happening to consume the same lot
// twice, which is an accident of ordering, not an invariant (#84).
func generateGasFeeEntries(txn *LendingTransaction) []*ledger.Entry {
	if txn.FeeAmount == nil || txn.FeeAmount.IsNil() || txn.FeeAmount.Sign() <= 0 {
		return nil
	}

	feeAmount := txn.FeeAmount.ToBigInt()
	feeUSDRate := txn.FeeUSDPrice.ToBigInt()
	feeDecimals := txn.FeeDecimals
	if feeDecimals == 0 {
		feeDecimals = 18
	}
	feeUSDValue := money.CalcUSDValue(feeAmount, feeUSDRate, feeDecimals)

	walletID := txn.WalletID.String()
	chain := txn.ChainID
	// The gas account and the wallet's native leg are keyed on the fee asset's
	// registry UUID (#59) so they meet the same account native transfers use.
	feeAsset := txn.FeeAsset
	feeIdentity := accountcode.OnChain(chain, feeAsset)

	return []*ledger.Entry{
		{
			ID:          uuid.New(),
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
				"chain_id":     chain,
			},
		},
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeAssetDecrease,
			Amount:      new(big.Int).Set(feeAmount),
			AssetID:     feeAsset,
			USDRate:     money.CopyRate(feeUSDRate),
			USDValue:    money.CopyRate(feeUSDValue),
			OccurredAt:  txn.OccurredAt,
			CreatedAt:   time.Now().UTC(),
			Metadata: map[string]interface{}{
				"wallet_id":    walletID,
				"account_code": accountcode.Wallet(txn.WalletID, feeIdentity),
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

// itemAsset is the identity of the asset one transfer item moves: the
// transaction's chain paired with the item's registry UUID (#59). Every
// account code in a lending pair addresses that one asset, so the pair is
// formed once here rather than re-assembled at each of the ten call sites
// below — which is exactly where the halves drifted apart in #70.
func itemAsset(txn *LendingTransaction, item *LendingTransferItem) accountcode.Asset {
	return accountcode.OnChain(txn.ChainID, item.AssetID)
}

// entryRouting captures the debit and credit side of a single balanced
// pair for a lending asset movement. Account types are inferred from each
// account code's prefix in buildLendingPair — no need to carry them here.
type entryRouting struct {
	debitAccount  string
	debitType     ledger.EntryType
	creditAccount string
	creditType    ledger.EntryType
	// legPair, when set, stamps both sides with the same leg-pair marker so
	// the TaxLotHook carries the cost basis from the disposal to the
	// acquisition. See [ledger.MetaLegPair].
	//
	// Set for supply and withdraw, which move the user's OWN principal between
	// their wallet and their collateral and must not realize a gain. Left empty
	// for borrow, repay and claim: no lot crosses there.
	legPair string
}

// legPairFor names the two legs of one lending movement.
//
// The ITEM'S POSITION is the key, not its asset. Two items in one transaction
// can carry the same asset — buildLendingData copies every leg into the item
// list, opposite-direction ones included, precisely because "a real operation
// can still move principal both ways (a supply that also returns dust, a repay
// that reclaims excess)", and the handlers do not branch on Direction. Keyed on
// the asset, those two items would share a marker, the hook would pool their
// disposals under one group, and an acquisition would link to whichever
// disposal landed first rather than to its own counterpart. That is the asset
// collision this marker exists to remove, reintroduced one level down — and it
// balances, so nothing downstream would object.
//
// The asset stays in the key as well, but only for legibility when reading
// stored JSONB: the index alone already separates the pairs.
func legPairFor(txn *LendingTransaction, idx int, item *LendingTransferItem) string {
	return "lending:" + txn.TxHash + ":" + strconv.Itoa(idx) + ":" + item.AssetID.String()
}

// generateSupplyItemEntries emits entries for one transfer item of a supply op.
// The principal leaves the wallet and lands in collateral.
func generateSupplyItemEntries(txn *LendingTransaction, idx int, item *LendingTransferItem) []*ledger.Entry {
	return buildLendingPair(txn, item, entryRouting{
		debitAccount:  accountcode.Collateral(txn.Protocol, txn.WalletID, itemAsset(txn, item)),
		debitType:     ledger.EntryTypeCollateralIncrease,
		creditAccount: accountcode.Wallet(txn.WalletID, itemAsset(txn, item)),
		creditType:    ledger.EntryTypeAssetDecrease,
		legPair:       legPairFor(txn, idx, item),
	})
}

// generateWithdrawItemEntries emits entries for one transfer item of a withdraw op.
// The principal leaves collateral and lands back in the wallet.
func generateWithdrawItemEntries(txn *LendingTransaction, idx int, item *LendingTransferItem) []*ledger.Entry {
	return buildLendingPair(txn, item, entryRouting{
		debitAccount:  accountcode.Wallet(txn.WalletID, itemAsset(txn, item)),
		debitType:     ledger.EntryTypeAssetIncrease,
		creditAccount: accountcode.Collateral(txn.Protocol, txn.WalletID, itemAsset(txn, item)),
		creditType:    ledger.EntryTypeCollateralDecrease,
		legPair:       legPairFor(txn, idx, item),
	})
}

// generateBorrowItemEntries emits entries for one transfer item of a borrow op.
// The borrowed principal arrives in the wallet against a matching liability.
func generateBorrowItemEntries(txn *LendingTransaction, idx int, item *LendingTransferItem) []*ledger.Entry {
	return buildLendingPair(txn, item, entryRouting{
		debitAccount:  accountcode.Wallet(txn.WalletID, itemAsset(txn, item)),
		debitType:     ledger.EntryTypeAssetIncrease,
		creditAccount: accountcode.Liability(txn.Protocol, txn.WalletID, itemAsset(txn, item)),
		creditType:    ledger.EntryTypeLiabilityIncrease,
	})
}

// generateRepayItemEntries emits entries for one transfer item of a repay op.
// The principal leaves the wallet and retires the matching liability.
func generateRepayItemEntries(txn *LendingTransaction, idx int, item *LendingTransferItem) []*ledger.Entry {
	return buildLendingPair(txn, item, entryRouting{
		debitAccount:  accountcode.Liability(txn.Protocol, txn.WalletID, itemAsset(txn, item)),
		debitType:     ledger.EntryTypeLiabilityDecrease,
		creditAccount: accountcode.Wallet(txn.WalletID, itemAsset(txn, item)),
		creditType:    ledger.EntryTypeAssetDecrease,
	})
}

// generateClaimItemEntries emits entries for one transfer item of a claim op.
// Rewards arrive in the wallet against lending income.
func generateClaimItemEntries(txn *LendingTransaction, idx int, item *LendingTransferItem) []*ledger.Entry {
	return buildLendingPair(txn, item, entryRouting{
		debitAccount:  accountcode.Wallet(txn.WalletID, itemAsset(txn, item)),
		debitType:     ledger.EntryTypeAssetIncrease,
		creditAccount: accountcode.IncomeLending(itemAsset(txn, item)),
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
		if r.legPair != "" {
			m[ledger.MetaLegPair] = r.legPair
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
		USDRate:     money.CopyRate(usdRate),
		USDValue:    money.CopyRate(usdValue),
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
		USDRate:     money.CopyRate(usdRate),
		USDValue:    money.CopyRate(usdValue),
		OccurredAt:  txn.OccurredAt,
		CreatedAt:   time.Now().UTC(),
		Metadata:    metaFor(r.creditAccount),
	}
	return []*ledger.Entry{debit, credit}
}
