package transactions

import (
	"context"
	"math/big"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/ledger"
)

// The arriving leg of a bridge is a SECOND asset with a spelling of its own
// (#84), recorded in raw_data under dest_asset_id / dest_asset_symbol. The
// harvest walked every other spelling and not that one, so the one entry type
// where two assets are normal rather than exceptional rendered its arriving leg
// with no ticker at all: a blank AssetSymbol, an AccountLabel fallen back to the
// raw account code, and a DisplayAmount that could not find decimals to format
// with (#86).

func TestSymbolsFromRawData_HarvestsTheArrivingLeg(t *testing.T) {
	srcAsset := uuid.New()
	dstAsset := uuid.New()

	raw := map[string]interface{}{
		"asset_id":          srcAsset.String(),
		"asset_symbol":      "USDC",
		"dest_asset_id":     dstAsset.String(),
		"dest_asset_symbol": "USDC",
	}

	symbols := symbolsFromRawData(raw)

	assert.Equal(t, "USDC", symbols[srcAsset], "the departing leg was always harvested")
	assert.Equal(t, "USDC", symbols[dstAsset],
		"the arriving leg's id and ticker sit in raw_data under their own spelling; not reading them "+
			"leaves that entry with no ticker to render (#86)")
}

func TestSymbolsFromRawData_ArrivingLegKeepsItsOwnTicker(t *testing.T) {
	srcAsset := uuid.New()
	dstAsset := uuid.New()

	// The two sides of a bridge normally share a ticker, so a fixture where they
	// agree cannot tell "read the destination spelling" from "reused the source
	// symbol for every id". These deliberately differ.
	raw := map[string]interface{}{
		"asset_id":          srcAsset.String(),
		"asset_symbol":      "USDC",
		"dest_asset_id":     dstAsset.String(),
		"dest_asset_symbol": "USDbC",
	}

	symbols := symbolsFromRawData(raw)

	assert.Equal(t, "USDC", symbols[srcAsset])
	assert.Equal(t, "USDbC", symbols[dstAsset],
		"each id is labelled with the ticker recorded beside IT")
}

func TestSymbolsFromRawData_NoDestinationLegIsUnchanged(t *testing.T) {
	srcAsset := uuid.New()

	symbols := symbolsFromRawData(map[string]interface{}{
		"asset_id":     srcAsset.String(),
		"asset_symbol": "ETH",
	})

	require.Len(t, symbols, 1, "an ordinary transaction names one asset and gains no phantom second")
	assert.Equal(t, "ETH", symbols[srcAsset])
}

// TestBridge_ArrivingLegRendersWithSymbolAndLabel is the user-visible half: the
// same defect seen through the API response the transactions list renders from.
func TestBridge_ArrivingLegRendersWithSymbolAndLabel(t *testing.T) {
	srcAsset := uuid.New()
	dstAsset := uuid.New()
	srcWallet := uuid.New()
	dstWallet := uuid.New()

	raw := map[string]interface{}{
		"asset_id":          srcAsset.String(),
		"asset_symbol":      "USDC",
		"dest_asset_id":     dstAsset.String(),
		"dest_asset_symbol": "USDC",
	}

	entries := []*ledger.Entry{
		{
			ID:          uuid.New(),
			AssetID:     dstAsset,
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeAssetIncrease,
			Amount:      big.NewInt(24_446_762),
			Metadata: map[string]interface{}{
				"account_code": "wallet." + dstWallet.String() + ".base." + dstAsset.String(),
			},
		},
		{
			ID:          uuid.New(),
			AssetID:     srcAsset,
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeAssetDecrease,
			Amount:      big.NewInt(24_446_762),
			Metadata: map[string]interface{}{
				"account_code": "wallet." + srcWallet.String() + ".arbitrum." + srcAsset.String(),
			},
		},
	}

	svc := &TransactionService{}
	got := svc.toEntryResponses(context.Background(), entries, "My Wallet", symbolsFromRawData(raw))
	require.Len(t, got, 2)

	arriving := got[0]
	assert.Equal(t, "USDC", arriving.AssetSymbol,
		"the arriving leg must carry a ticker; blank is what the missing harvest produced")
	assert.Equal(t, "My Wallet - USDC", arriving.AccountLabel,
		"with a ticker the label is human-readable; without one it falls back to the raw account "+
			"code, which is what the user actually saw on every bridge")
	assert.NotContains(t, arriving.AccountLabel, dstAsset.String(),
		"a bare UUID must not reach a field the user reads")
	assert.Contains(t, arriving.DisplayAmount, "USDC",
		"DisplayAmount looks up decimals BY SYMBOL, so a blank symbol degrades it too")

	departing := got[1]
	assert.Equal(t, "USDC", departing.AssetSymbol)
	assert.Equal(t, "My Wallet - USDC", departing.AccountLabel)
}
