package sync

import (
	"context"
	"math/big"
	"sort"

	"github.com/kislikjeka/moontrack/pkg/logger"
)

// AssetNetFlow is the F side of the reconciliation report's P/F/L triangle
// (issue #41) for ONE asset identity: what the collected transaction history
// says the wallet's balance in that asset should be, and — when the answer is
// "nothing, on purpose" — which rule says so.
//
// It is the exported form of what the Reconciler computes internally, published
// so the report (#61) does not build a second, independently-written F. Two
// implementations of the same quantity would eventually disagree, and the pair
// they form is the one thing the ticket forbids: a flag that fires on what the
// report calls explained.
type AssetNetFlow struct {
	// Key is the asset identity: (chain, contract). Never the ticker — two
	// contracts sharing a symbol are two assets, which is exactly the confusion
	// that made the symbol-keyed flow wrong.
	Key AssetKey

	// AssetSymbol and Decimals are display metadata, taken from the first leg
	// seen. Neither is an identifier.
	AssetSymbol string
	Decimals    int

	// NetFlow is inflow minus outflow in base units, counting only legs that
	// were eligible for the ledger. Zero for an asset whose every leg was
	// rejected.
	NetFlow *big.Int

	// RejectedBy names every rule that kept a leg of this asset out of the
	// ledger, empty when none did.
	RejectedBy []RejectionReason

	// Booked reports whether ANY leg of this asset reached the ledger. It is
	// what makes the exemption exact: a rejection excuses a missing balance only
	// when there is no balance to miss.
	Booked bool

	// RejectedAmount is the total base-unit magnitude of the rejected legs,
	// summed over the history, so the report can print the SIZE of what was kept
	// out and not merely its name. #41 requires the filtered assets listed
	// поимённо with quantities, because a name alone cannot tell spam from a
	// broken resolve.
	RejectedAmount *big.Int
}

// Explained reports whether some rule fully accounts for this asset carrying no
// ledger balance.
//
// Both halves are required. A rejection alone is not enough: an asset with legs
// on both sides of the rule has a real ledger balance, so a position that
// disagrees with it is a genuine discrepancy and must stay red. Reading only
// RejectedBy would let one rejected leg excuse an asset forever — the same
// silent papering-over that switching genesis off (#49) was meant to end, and
// the reason the reconciler applies exactly this predicate.
func (f AssetNetFlow) Explained() bool { return len(f.RejectedBy) > 0 && !f.Booked }

// NetFlows computes the F side for a wallet's collected raw transactions: the
// net flow per asset identity, plus the rejection attribution for the assets a
// rule kept out of the ledger.
//
// This is the seam the reconciliation report is built on. The report reads the
// database itself and takes P straight from the provider's raw JSON — it must
// not share those with production, or it would agree with the code it is
// checking by construction (#41). F is the deliberate exception: F is not an
// independent observation of the chain but a statement about what THIS pipeline
// decided to book, so the only F worth comparing against is the one the pipeline
// actually used. Recomputing it separately would not make the check more
// independent, only capable of disagreeing with the flag about the same asset.
//
// Assets with no flow and no rejection simply do not appear.
//
// log receives the same diagnostics the sync path emits — notably a registry
// read that failed, which makes the filter fail open and can therefore change
// the answer. It is a parameter rather than a swallowed default so a caller
// cannot end up trusting a flow that was computed with the filter unavailable.
func NetFlows(
	ctx context.Context,
	raws []*RawTransaction,
	filter *KnownAssetFilter,
	log *logger.Logger,
) ([]AssetNetFlow, error) {
	res, err := calculateNetFlows(raws,
		newRejectionResolver(ctx, filter, log.WithField("component", "net_flow")))
	if err != nil {
		return nil, err
	}

	out := make([]AssetNetFlow, 0, len(res.flows)+len(res.rejected))

	for key, flow := range res.flows {
		reasons, _ := res.explains(key)
		row := AssetNetFlow{
			Key:         key,
			AssetSymbol: flow.AssetSymbol,
			Decimals:    flow.Decimals,
			NetFlow:     flow.NetFlow(),
			RejectedBy:  reasons,
			// A flow entry exists only because a leg was booked, so this asset
			// has a real ledger balance and a rejection does not excuse it.
			Booked:         true,
			RejectedAmount: big.NewInt(0),
		}
		if tally, ok := res.rejected[key]; ok {
			row.RejectedAmount = new(big.Int).Set(tally.amount)
		}
		out = append(out, row)
	}

	// An asset whose EVERY leg was rejected has no flow entry at all, and it is
	// the most important row in the report: it is precisely the aToken or the
	// spam token that shows a position with no ledger balance. Omitting it would
	// leave the report unable to explain the very rows #49 said must be
	// explained.
	//
	// Its name, decimals and size come from the rejected legs themselves, since
	// there is no flow entry to borrow them from — #41 asks for these assets
	// поимённо with quantities, and an anonymous row cannot separate spam from a
	// broken resolve.
	for key, tally := range res.rejected {
		if _, booked := res.flows[key]; booked {
			continue
		}
		reasons, _ := res.explains(key)
		out = append(out, AssetNetFlow{
			Key:            key,
			AssetSymbol:    tally.symbol,
			Decimals:       tally.decimals,
			NetFlow:        big.NewInt(0),
			RejectedBy:     reasons,
			Booked:         false,
			RejectedAmount: new(big.Int).Set(tally.amount),
		})
	}

	// Deterministic order: the report diffs two runs against each other, and map
	// iteration order would make that diff meaningless.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Key.Chain != out[j].Key.Chain {
			return out[i].Key.Chain < out[j].Key.Chain
		}
		return out[i].Key.Contract < out[j].Key.Contract
	})

	return out, nil
}
