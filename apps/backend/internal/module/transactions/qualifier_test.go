package transactions

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/kislikjeka/moontrack/internal/platform/assetregistry"
)

// The transactions list labels a row with the ticker recorded in raw_data, but
// whether that ticker is AMBIGUOUS is a fact about the registry as it stands
// now — a second contract sharing the ticker can appear long after the
// transaction was booked. These pin the stamping that carries that fact onto a
// row (#42).

func TestApplyQualifier_FlagsAndQualifiesAnAmbiguousTicker(t *testing.T) {
	id := uuid.New()
	const contract = "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"

	item := TransactionListItem{AssetID: id.String(), AssetSymbol: "USDC"}
	applyQualifier(&item, map[uuid.UUID]assetregistry.Asset{
		id: {ID: id, Symbol: "USDC", Contract: contract, SymbolAmbiguous: true},
	})

	assert.True(t, item.SymbolAmbiguous,
		"a ticker naming more than one asset on its chain must be flagged, or the "+
			"list renders two different assets identically")
	assert.Equal(t, contract, item.AssetContract,
		"the contract is what the client qualifies the ticker with; the flag "+
			"without it says 'ambiguous' and offers nothing to disambiguate by")
	assert.Equal(t, "USDC", item.AssetSymbol, "the ticker itself is untouched")
}

func TestApplyQualifier_LeavesAnUnambiguousTickerBare(t *testing.T) {
	id := uuid.New()
	item := TransactionListItem{AssetID: id.String(), AssetSymbol: "ETH"}
	applyQualifier(&item, map[uuid.UUID]assetregistry.Asset{
		id: {ID: id, Symbol: "ETH", Contract: "native"},
	})

	assert.False(t, item.SymbolAmbiguous,
		"flagging every row would make the qualifier carry no information")
}

func TestApplyQualifier_AbsentFromRegistryLeavesTheRowUnchanged(t *testing.T) {
	// An asset the registry does not answer for degrades to the pre-#42 row
	// rather than failing: the amount and ticker are still true, only the
	// qualifier is missing.
	item := TransactionListItem{AssetID: uuid.New().String(), AssetSymbol: "USDC"}
	applyQualifier(&item, map[uuid.UUID]assetregistry.Asset{uuid.New(): {SymbolAmbiguous: true}})

	assert.False(t, item.SymbolAmbiguous)
	assert.Empty(t, item.AssetContract)
	assert.Equal(t, "USDC", item.AssetSymbol)
}

func TestApplyQualifier_MalformedAssetIDIsNotStamped(t *testing.T) {
	// Reached when raw_data holds something that is not a registry id. It must
	// not panic and must not borrow another asset's flag.
	item := TransactionListItem{AssetID: "not-a-uuid", AssetSymbol: "USDC"}
	applyQualifier(&item, map[uuid.UUID]assetregistry.Asset{uuid.New(): {SymbolAmbiguous: true}})

	assert.False(t, item.SymbolAmbiguous)
	assert.Empty(t, item.AssetContract)
}
