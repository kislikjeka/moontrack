package ledger

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEntryBalanceChange(t *testing.T) {
	amount := big.NewInt(1000)
	tests := []struct {
		entryType EntryType
		expected  *big.Int
	}{
		{EntryTypeAssetIncrease, big.NewInt(1000)},
		{EntryTypeAssetDecrease, big.NewInt(-1000)},
		{EntryTypeCollateralIncrease, big.NewInt(1000)},
		{EntryTypeCollateralDecrease, big.NewInt(-1000)},
		{EntryTypeLiabilityIncrease, big.NewInt(1000)},
		{EntryTypeLiabilityDecrease, big.NewInt(-1000)},
		{EntryTypeGasFee, nil},
		{EntryTypeClearing, nil},
		{EntryTypeIncome, nil},
		{EntryTypeExpense, nil},
	}
	for _, tt := range tests {
		t.Run(string(tt.entryType), func(t *testing.T) {
			entry := &Entry{Amount: new(big.Int).Set(amount), EntryType: tt.entryType}
			result := entryBalanceChange(entry)
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				assert.Equal(t, tt.expected.String(), result.String())
			}
		})
	}
}
