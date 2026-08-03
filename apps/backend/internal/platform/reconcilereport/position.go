// Package reconcilereport turns "the balance adds up" from a feeling into a
// verdict (issue #61, decision #41 as amended by #49).
//
// It reads the database and the provider's RAW JSON, sorts every position into
// an explained category, and reports RED for anything left over. "Adds up"
// means the red category is EMPTY — not a number, not a percentage.
//
// # Why this lives outside sync
//
// The checker must not be the checked. Inside the sync path, P would arrive
// through the same contract normalization the ledger side uses, so one bug on
// both sides would produce a reconciliation that agrees with itself. The only
// production code this package shares is the HTTP client (key, retries, rate
// limits); the domain normalization here is its own, deliberately duplicated so
// it can disagree with the pipeline's.
//
// The one exception is F, the net flow, which comes from sync.NetFlows (#60).
// That is not an independent observation of the chain but a statement about what
// THIS pipeline decided to book, so the only F worth comparing against is the
// one the pipeline actually used. A second implementation could only ever
// disagree with the per-chain flag about the same asset — the pair the ticket
// forbids.
package reconcilereport

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// Position is the P side of the triangle: one balance the provider reports for
// the wallet, normalized into MoonTrack's identity by THIS package.
//
// It carries no rejection attribution and never will. The receipt rule (#57)
// is deliberately NOT applied here: a receipt token is obliged to show up in P
// and to be absent from L, and filtering it out of P would make both sides
// agree by construction — exactly the defect that moved this check out of sync
// (#41, #49).
type Position struct {
	Key      sync.AssetKey
	Symbol   string
	Decimals int
	// Quantity is in base units. Amounts are compared as QUANTITIES, never in
	// USD, so the verdict does not depend on the price pipeline.
	Quantity *big.Int
}

// RawBalance is the provider's balance shape, decoded straight from the JSON
// array the balances endpoint returns.
//
// It is redeclared here rather than reused from the provider gateway on
// purpose. The gateway's decode is one import away from the gateway's
// normalization, and the whole point of this package is that the two
// normalizations are separate implementations that can disagree. What IS shared
// is the HTTP client that produced the bytes.
type RawBalance struct {
	Balance string `json:"balance"`
	Token   *struct {
		Symbol   string `json:"symbol"`
		Name     string `json:"name"`
		Decimals int    `json:"decimals"`
		Address  string `json:"address"`
	} `json:"token"`
}

// NormalizePositions converts a chain's raw provider balances into P rows.
//
// Zero and negative balances are dropped: both sides then say "no position",
// and printing them would bury the report under every token the wallet has ever
// fully spent.
//
// A balance whose token block is missing, or whose amount will not convert, is
// NOT silently skipped — it is returned as an error, because a position the
// report cannot read is a position the report cannot vouch for, and silence
// here would read as "nothing to see".
func NormalizePositions(chain string, raws []RawBalance) ([]Position, error) {
	out := make([]Position, 0, len(raws))
	for i, rb := range raws {
		if rb.Token == nil {
			return nil, fmt.Errorf("position %d on %s has no token block", i, chain)
		}
		qty, err := money.ToBaseUnits(rb.Balance, rb.Token.Decimals)
		if err != nil {
			return nil, fmt.Errorf("position %d on %s (%s): cannot convert balance %q at %d decimals: %w",
				i, chain, rb.Token.Symbol, rb.Balance, rb.Token.Decimals, err)
		}
		if qty == nil || qty.Sign() <= 0 {
			continue
		}
		contract, err := normalizeProviderContract(chain, rb.Token.Address, rb.Token.Symbol)
		if err != nil {
			return nil, fmt.Errorf("position %d on %s: %w", i, chain, err)
		}
		out = append(out, Position{
			Key:      sync.NewAssetKey(chain, contract),
			Symbol:   rb.Token.Symbol,
			Decimals: rb.Token.Decimals,
			Quantity: qty,
		})
	}
	return out, nil
}

// nativeSymbols is the coin each chain's native position must be called.
//
// It is duplicated here rather than imported from the sync pipeline ON PURPOSE,
// and this is the one place in the report where duplication is the feature: the
// whole reason P is normalized separately is that a single mistake shared by
// both sides would produce a reconciliation that agrees with itself. A shared
// helper would make an error in the native mapping invisible to exactly the
// check meant to catch it.
//
// It is kept honest by TestNativeSentinel_MatchesTheProductionRule, which fails
// if this table and the pipeline's ever disagree — the drift a deliberate
// duplicate has to be protected against.
var nativeSymbols = map[string]string{
	"ethereum":            "ETH",
	"base":                "ETH",
	"arbitrum":            "ETH",
	"optimism":            "ETH",
	"polygon":             "POL",
	"avalanche":           "AVAX",
	"binance-smart-chain": "BNB",
}

// normalizeProviderContract maps the provider's spelling of a contract onto
// MoonTrack's.
//
// The native coin is the case that matters. The provider sends the SYMBOL where
// an address belongs — `"address": "ETH"` — while MoonTrack settled on the
// literal `native` (#56). Without this mapping every native position would key
// as the token `eth` and appear as a red row on every chain, while the real
// native balance in the ledger would look like a position the provider does not
// report. Mapping the sentinel is part of the report's job, named in the ticket.
//
// The sentinel is recognised only when the CHAIN and the SYMBOL agree with it,
// the same three-way test the ledger path applies. A blanket "anything without
// 0x is native" would be wider than the fact it stands for: two malformed
// addresses on one chain would both collapse onto (chain, native) and be summed
// into one inflated native position, which is a corrupted P presented as a clean
// one. An address that is neither a token nor this chain's coin is an error,
// consistent with how every other unreadable field here is handled — the report
// refuses to vouch for a position it cannot read.
func normalizeProviderContract(chain, address, symbol string) (string, error) {
	a := strings.TrimSpace(address)
	if strings.HasPrefix(strings.ToLower(a), "0x") {
		return strings.ToLower(a), nil
	}

	want, known := nativeSymbols[strings.ToLower(strings.TrimSpace(chain))]
	if !known {
		return "", fmt.Errorf("token %q carries the non-address %q on unknown chain %q: "+
			"the native sentinel cannot be confirmed", symbol, address, chain)
	}
	if !strings.EqualFold(strings.TrimSpace(symbol), want) &&
		!strings.EqualFold(a, want) {
		return "", fmt.Errorf("token %q carries the non-address %q on %s, whose native coin is %s: "+
			"this is not the native sentinel and has no contract to key on",
			symbol, address, chain, want)
	}
	return sync.NativeContract, nil
}
