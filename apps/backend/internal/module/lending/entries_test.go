package lending

import (
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/pkg/money"
	"github.com/kislikjeka/moontrack/pkg/testasset"
)

func assertEntriesBalanced(t *testing.T, entries []*ledger.Entry) {
	t.Helper()
	debitSum := new(big.Int)
	creditSum := new(big.Int)
	for _, e := range entries {
		if e.DebitCredit == ledger.Debit {
			debitSum.Add(debitSum, e.Amount)
		} else {
			creditSum.Add(creditSum, e.Amount)
		}
	}
	assert.Equal(t, 0, debitSum.Cmp(creditSum),
		"entries must balance: debits=%s credits=%s", debitSum.String(), creditSum.String())
}

func baseTxn() *LendingTransaction {
	return &LendingTransaction{
		WalletID:   uuid.New(),
		TxHash:     "0xabc123",
		ChainID:    "ethereum",
		OccurredAt: time.Now().UTC(),
		Protocol:   "Aave V3",
	}
}

// baseItem is 1 ETH at $2000 (price scaled 10^8).
func baseItem() *LendingTransferItem {
	return &LendingTransferItem{
		AssetID:         testasset.ETH,
		Decimals:        18,
		Amount:          money.NewBigInt(big.NewInt(1_000_000_000_000_000_000)),
		USDRate:         money.NewBigInt(big.NewInt(200_000_000_000)),
		ContractAddress: "0xcontract",
	}
}

func TestGenerateSupplyItemEntries(t *testing.T) {
	entries := generateSupplyItemEntries(baseTxn(), baseItem())

	require.Len(t, entries, 2)
	assertEntriesBalanced(t, entries)

	// First entry: DEBIT collateral_increase
	assert.Equal(t, ledger.Debit, entries[0].DebitCredit)
	assert.Equal(t, ledger.EntryTypeCollateralIncrease, entries[0].EntryType)
	assert.Equal(t, testasset.ETH, entries[0].AssetID)
	assert.Contains(t, entries[0].Metadata["account_code"], "collateral.")
	assert.Equal(t, "COLLATERAL", entries[0].Metadata["account_type"])

	// Second entry: CREDIT asset_decrease
	assert.Equal(t, ledger.Credit, entries[1].DebitCredit)
	assert.Equal(t, ledger.EntryTypeAssetDecrease, entries[1].EntryType)
	assert.Contains(t, entries[1].Metadata["account_code"], "wallet.")
}

func TestGenerateWithdrawItemEntries(t *testing.T) {
	entries := generateWithdrawItemEntries(baseTxn(), baseItem())

	require.Len(t, entries, 2)
	assertEntriesBalanced(t, entries)

	// First entry: DEBIT asset_increase
	assert.Equal(t, ledger.Debit, entries[0].DebitCredit)
	assert.Equal(t, ledger.EntryTypeAssetIncrease, entries[0].EntryType)
	assert.Contains(t, entries[0].Metadata["account_code"], "wallet.")

	// Second entry: CREDIT collateral_decrease
	assert.Equal(t, ledger.Credit, entries[1].DebitCredit)
	assert.Equal(t, ledger.EntryTypeCollateralDecrease, entries[1].EntryType)
	assert.Contains(t, entries[1].Metadata["account_code"], "collateral.")
	assert.Equal(t, "COLLATERAL", entries[1].Metadata["account_type"])
}

func TestGenerateBorrowItemEntries(t *testing.T) {
	item := baseItem()
	item.AssetID = testasset.USDC
	item.Decimals = 6

	entries := generateBorrowItemEntries(baseTxn(), item)

	require.Len(t, entries, 2)
	assertEntriesBalanced(t, entries)

	// First entry: DEBIT asset_increase (wallet gets borrowed asset)
	assert.Equal(t, ledger.Debit, entries[0].DebitCredit)
	assert.Equal(t, ledger.EntryTypeAssetIncrease, entries[0].EntryType)
	assert.Contains(t, entries[0].Metadata["account_code"], "wallet.")
	assert.Equal(t, testasset.USDC, entries[0].AssetID)

	// Second entry: CREDIT liability_increase
	assert.Equal(t, ledger.Credit, entries[1].DebitCredit)
	assert.Equal(t, ledger.EntryTypeLiabilityIncrease, entries[1].EntryType)
	assert.Contains(t, entries[1].Metadata["account_code"], "liability.")
	assert.Equal(t, "LIABILITY", entries[1].Metadata["account_type"])
}

func TestGenerateRepayItemEntries(t *testing.T) {
	item := baseItem()
	item.AssetID = testasset.USDC
	item.Decimals = 6

	entries := generateRepayItemEntries(baseTxn(), item)

	require.Len(t, entries, 2)
	assertEntriesBalanced(t, entries)

	// First entry: DEBIT liability_decrease
	assert.Equal(t, ledger.Debit, entries[0].DebitCredit)
	assert.Equal(t, ledger.EntryTypeLiabilityDecrease, entries[0].EntryType)
	assert.Contains(t, entries[0].Metadata["account_code"], "liability.")
	assert.Equal(t, "LIABILITY", entries[0].Metadata["account_type"])

	// Second entry: CREDIT asset_decrease
	assert.Equal(t, ledger.Credit, entries[1].DebitCredit)
	assert.Equal(t, ledger.EntryTypeAssetDecrease, entries[1].EntryType)
	assert.Contains(t, entries[1].Metadata["account_code"], "wallet.")
}

func TestGenerateClaimItemEntries(t *testing.T) {
	item := baseItem()
	item.AssetID = testasset.AAVE

	entries := generateClaimItemEntries(baseTxn(), item)

	require.Len(t, entries, 2)
	assertEntriesBalanced(t, entries)

	// First entry: DEBIT asset_increase (wallet gets reward)
	assert.Equal(t, ledger.Debit, entries[0].DebitCredit)
	assert.Equal(t, ledger.EntryTypeAssetIncrease, entries[0].EntryType)
	assert.Contains(t, entries[0].Metadata["account_code"], "wallet.")
	assert.Equal(t, testasset.AAVE, entries[0].AssetID)

	// Second entry: CREDIT income
	assert.Equal(t, ledger.Credit, entries[1].DebitCredit)
	assert.Equal(t, ledger.EntryTypeIncome, entries[1].EntryType)
	assert.Contains(t, entries[1].Metadata["account_code"].(string), "income.lending.")
}

// TestSupplyItemEntries_NoClearingNamespace pins the decision from #44: the
// lending clearing namespace existed only to balance the protocol receipt's
// leg, and the receipt no longer reaches the ledger at all. No lending entry
// may route through clearing, for any op.
func TestSupplyItemEntries_NoClearingNamespace(t *testing.T) {
	txn := baseTxn()
	item := baseItem()

	generators := map[string]func(*LendingTransaction, *LendingTransferItem) []*ledger.Entry{
		"supply":   generateSupplyItemEntries,
		"withdraw": generateWithdrawItemEntries,
		"borrow":   generateBorrowItemEntries,
		"repay":    generateRepayItemEntries,
		"claim":    generateClaimItemEntries,
	}

	for op, gen := range generators {
		t.Run(op, func(t *testing.T) {
			for _, e := range gen(txn, item) {
				code, _ := e.Metadata["account_code"].(string)
				assert.NotContains(t, code, "clearing.",
					"%s must not route through a clearing account, got %s", op, code)
				assert.NotEqual(t, ledger.EntryTypeClearing, e.EntryType,
					"%s must not emit a clearing entry", op)
			}
		})
	}
}

func TestGenerateGasFeeEntries(t *testing.T) {
	txn := baseTxn()
	txn.FeeAsset = testasset.ETH
	txn.FeeAmount = money.NewBigInt(big.NewInt(500_000_000_000_000)) // 0.0005 ETH
	txn.FeeDecimals = 18
	txn.FeeUSDPrice = money.NewBigInt(big.NewInt(200_000_000_000)) // $2000

	entries := generateGasFeeEntries(txn)

	require.Len(t, entries, 2)
	assertEntriesBalanced(t, entries)

	// DEBIT gas_fee
	assert.Equal(t, ledger.Debit, entries[0].DebitCredit)
	assert.Equal(t, ledger.EntryTypeGasFee, entries[0].EntryType)
	assert.Contains(t, entries[0].Metadata["account_code"], "gas.")

	// CREDIT asset_decrease
	assert.Equal(t, ledger.Credit, entries[1].DebitCredit)
	assert.Equal(t, ledger.EntryTypeAssetDecrease, entries[1].EntryType)
	assert.Contains(t, entries[1].Metadata["account_code"], "wallet.")
	assert.Equal(t, "gas_payment", entries[1].Metadata["entry_type"])
}

func TestGenerateGasFeeEntries_NoFee(t *testing.T) {
	entries := generateGasFeeEntries(baseTxn())
	assert.Nil(t, entries)
}
