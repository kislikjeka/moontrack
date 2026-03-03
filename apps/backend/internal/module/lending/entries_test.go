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
		WalletID:        uuid.New(),
		TxHash:          "0xabc123",
		ChainID:         "ethereum",
		OccurredAt:      time.Now().UTC(),
		Protocol:        "Aave V3",
		Asset:           "ETH",
		Amount:          money.NewBigInt(big.NewInt(1_000_000_000_000_000_000)), // 1 ETH
		Decimals:        18,
		USDPrice:        money.NewBigInt(big.NewInt(200_000_000_000)), // $2000 scaled 10^8
		ContractAddress: "0xcontract",
	}
}

func TestGenerateSupplyEntries(t *testing.T) {
	txn := baseTxn()
	entries := generateSupplyEntries(txn)

	require.Len(t, entries, 2)
	assertEntriesBalanced(t, entries)

	// First entry: DEBIT collateral_increase
	assert.Equal(t, ledger.Debit, entries[0].DebitCredit)
	assert.Equal(t, ledger.EntryTypeCollateralIncrease, entries[0].EntryType)
	assert.Equal(t, "ETH", entries[0].AssetID)
	assert.Contains(t, entries[0].Metadata["account_code"], "collateral.")
	assert.Equal(t, "COLLATERAL", entries[0].Metadata["account_type"])

	// Second entry: CREDIT asset_decrease
	assert.Equal(t, ledger.Credit, entries[1].DebitCredit)
	assert.Equal(t, ledger.EntryTypeAssetDecrease, entries[1].EntryType)
	assert.Contains(t, entries[1].Metadata["account_code"], "wallet.")
}

func TestGenerateWithdrawEntries(t *testing.T) {
	txn := baseTxn()
	entries := generateWithdrawEntries(txn)

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

func TestGenerateBorrowEntries(t *testing.T) {
	txn := baseTxn()
	txn.Asset = "USDC"
	txn.Decimals = 6
	entries := generateBorrowEntries(txn)

	require.Len(t, entries, 2)
	assertEntriesBalanced(t, entries)

	// First entry: DEBIT asset_increase (wallet gets borrowed asset)
	assert.Equal(t, ledger.Debit, entries[0].DebitCredit)
	assert.Equal(t, ledger.EntryTypeAssetIncrease, entries[0].EntryType)
	assert.Contains(t, entries[0].Metadata["account_code"], "wallet.")
	assert.Equal(t, "USDC", entries[0].AssetID)

	// Second entry: CREDIT liability_increase
	assert.Equal(t, ledger.Credit, entries[1].DebitCredit)
	assert.Equal(t, ledger.EntryTypeLiabilityIncrease, entries[1].EntryType)
	assert.Contains(t, entries[1].Metadata["account_code"], "liability.")
	assert.Equal(t, "LIABILITY", entries[1].Metadata["account_type"])
}

func TestGenerateRepayEntries(t *testing.T) {
	txn := baseTxn()
	txn.Asset = "USDC"
	txn.Decimals = 6
	entries := generateRepayEntries(txn)

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

func TestGenerateClaimEntries(t *testing.T) {
	txn := baseTxn()
	txn.Asset = "AAVE"
	entries := generateClaimEntries(txn)

	require.Len(t, entries, 2)
	assertEntriesBalanced(t, entries)

	// First entry: DEBIT asset_increase (wallet gets reward)
	assert.Equal(t, ledger.Debit, entries[0].DebitCredit)
	assert.Equal(t, ledger.EntryTypeAssetIncrease, entries[0].EntryType)
	assert.Contains(t, entries[0].Metadata["account_code"], "wallet.")
	assert.Equal(t, "AAVE", entries[0].AssetID)

	// Second entry: CREDIT income
	assert.Equal(t, ledger.Credit, entries[1].DebitCredit)
	assert.Equal(t, ledger.EntryTypeIncome, entries[1].EntryType)
	assert.Contains(t, entries[1].Metadata["account_code"].(string), "income.lending.")
}

func TestGenerateGasFeeEntries(t *testing.T) {
	txn := baseTxn()
	txn.FeeAsset = "ETH"
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
	txn := baseTxn()
	entries := generateGasFeeEntries(txn)
	assert.Nil(t, entries)
}
