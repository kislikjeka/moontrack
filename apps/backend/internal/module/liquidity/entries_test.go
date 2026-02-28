package liquidity

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

func newTestLPTxn(transfers []LPTransfer) *LPTransaction {
	return &LPTransaction{
		WalletID:   uuid.New(),
		TxHash:     "0xabc123",
		ChainID:    1,
		OccurredAt: time.Now().UTC(),
		Protocol:   "Uniswap V3",
		Transfers:  transfers,
	}
}

func sumDebits(entries []*ledger.Entry) *big.Int {
	total := big.NewInt(0)
	for _, e := range entries {
		if e.IsDebit() {
			total.Add(total, e.Amount)
		}
	}
	return total
}

func sumCredits(entries []*ledger.Entry) *big.Int {
	total := big.NewInt(0)
	for _, e := range entries {
		if e.IsCredit() {
			total.Add(total, e.Amount)
		}
	}
	return total
}

func TestDepositEntries_Balanced(t *testing.T) {
	txn := newTestLPTxn([]LPTransfer{
		{AssetSymbol: "ETH", Amount: money.NewBigIntFromInt64(1000000000000000000), Decimals: 18, Direction: "out", USDPrice: money.NewBigIntFromInt64(250000000000)},
		{AssetSymbol: "USDC", Amount: money.NewBigIntFromInt64(2500000000), Decimals: 6, Direction: "out", USDPrice: money.NewBigIntFromInt64(100000000)},
	})

	entries := generateSwapLikeEntries(txn)
	require.Len(t, entries, 4) // 2 assets * 2 entries each

	assert.Equal(t, 0, sumDebits(entries).Cmp(sumCredits(entries)), "entries must be balanced")
}

func TestDepositEntries_AssetDecrease(t *testing.T) {
	txn := newTestLPTxn([]LPTransfer{
		{AssetSymbol: "ETH", Amount: money.NewBigIntFromInt64(1e18), Decimals: 18, Direction: "out", USDPrice: money.NewBigIntFromInt64(250000000000)},
	})

	entries := generateSwapLikeEntries(txn)
	require.Len(t, entries, 2)

	// First entry: CREDIT asset_decrease (wallet)
	assert.Equal(t, ledger.Credit, entries[0].DebitCredit)
	assert.Equal(t, ledger.EntryTypeAssetDecrease, entries[0].EntryType)
	assert.Equal(t, "ETH", entries[0].AssetID)

	// Second entry: DEBIT clearing
	assert.Equal(t, ledger.Debit, entries[1].DebitCredit)
	assert.Equal(t, ledger.EntryTypeClearing, entries[1].EntryType)
}

func TestWithdrawEntries_AssetIncrease(t *testing.T) {
	txn := newTestLPTxn([]LPTransfer{
		{AssetSymbol: "ETH", Amount: money.NewBigIntFromInt64(1e18), Decimals: 18, Direction: "in", USDPrice: money.NewBigIntFromInt64(250000000000)},
		{AssetSymbol: "USDC", Amount: money.NewBigIntFromInt64(2500000000), Decimals: 6, Direction: "in", USDPrice: money.NewBigIntFromInt64(100000000)},
	})

	entries := generateSwapLikeEntries(txn)
	require.Len(t, entries, 4)

	assert.Equal(t, 0, sumDebits(entries).Cmp(sumCredits(entries)), "entries must be balanced")

	// First entry: DEBIT asset_increase (wallet)
	assert.Equal(t, ledger.Debit, entries[0].DebitCredit)
	assert.Equal(t, ledger.EntryTypeAssetIncrease, entries[0].EntryType)
}

func TestClaimEntries_IncomeBooked(t *testing.T) {
	txn := newTestLPTxn([]LPTransfer{
		{AssetSymbol: "ETH", Amount: money.NewBigIntFromInt64(50000000000000000), Decimals: 18, Direction: "in", USDPrice: money.NewBigIntFromInt64(250000000000)},
		{AssetSymbol: "USDC", Amount: money.NewBigIntFromInt64(125000000), Decimals: 6, Direction: "in", USDPrice: money.NewBigIntFromInt64(100000000)},
	})

	entries := generateLPClaimEntries(txn)
	require.Len(t, entries, 4) // 2 assets * 2 entries each

	assert.Equal(t, 0, sumDebits(entries).Cmp(sumCredits(entries)), "entries must be balanced")

	// Check income entries
	for _, e := range entries {
		if e.EntryType == ledger.EntryTypeIncome {
			code, _ := e.Metadata["account_code"].(string)
			assert.Contains(t, code, "income.lp.")
		}
	}
}

func TestClaimEntries_IgnoresOutTransfers(t *testing.T) {
	txn := newTestLPTxn([]LPTransfer{
		{AssetSymbol: "ETH", Amount: money.NewBigIntFromInt64(50000000000000000), Decimals: 18, Direction: "in", USDPrice: money.NewBigIntFromInt64(250000000000)},
		{AssetSymbol: "UNI", Amount: money.NewBigIntFromInt64(1000000000000000000), Decimals: 18, Direction: "out", USDPrice: money.NewBigIntFromInt64(500000000)},
	})

	entries := generateLPClaimEntries(txn)
	require.Len(t, entries, 2) // only 1 IN transfer * 2 entries
}

func TestGasFeeEntries(t *testing.T) {
	txn := newTestLPTxn([]LPTransfer{
		{AssetSymbol: "ETH", Amount: money.NewBigIntFromInt64(1e18), Decimals: 18, Direction: "out", USDPrice: money.NewBigIntFromInt64(250000000000)},
	})
	txn.FeeAsset = "ETH"
	txn.FeeAmount = money.NewBigIntFromInt64(21000000000000)
	txn.FeeDecimals = 18
	txn.FeeUSDPrice = money.NewBigIntFromInt64(250000000000)

	entries := generateGasFeeEntries(txn)
	require.Len(t, entries, 2)

	assert.Equal(t, 0, sumDebits(entries).Cmp(sumCredits(entries)), "gas fee entries must be balanced")

	// DEBIT gas_fee
	assert.Equal(t, ledger.Debit, entries[0].DebitCredit)
	assert.Equal(t, ledger.EntryTypeGasFee, entries[0].EntryType)

	// CREDIT asset_decrease
	assert.Equal(t, ledger.Credit, entries[1].DebitCredit)
	assert.Equal(t, ledger.EntryTypeAssetDecrease, entries[1].EntryType)
}

func TestGasFeeEntries_NoFee(t *testing.T) {
	txn := newTestLPTxn([]LPTransfer{
		{AssetSymbol: "ETH", Amount: money.NewBigIntFromInt64(1e18), Decimals: 18, Direction: "out", USDPrice: money.NewBigIntFromInt64(250000000000)},
	})

	entries := generateGasFeeEntries(txn)
	assert.Nil(t, entries)
}
