package sync

import (
	"context"

	"github.com/kislikjeka/moontrack/internal/platform/sync/assetlist"
)

// KnownnessStatus is the stored verdict for an on-chain identity (#58).
//
// Three values, not a boolean, because a boolean cannot express the state the
// system spends most of its time in: queued and undecided. Collapsing that into
// "not known" would make every freshly-seen asset indistinguishable from proven
// spam, and the reconciliation report (#61) has to tell those apart to
// distinguish spam from a migration bug.
type KnownnessStatus string

const (
	// KnownnessPending — queued, no verdict yet. The leg does NOT enter the
	// ledger, but it is NOT spam either: it is waiting. This is the default for
	// every identity the built-in list does not answer for.
	KnownnessPending KnownnessStatus = "pending"

	// KnownnessKnown — resolved known. The leg enters the ledger.
	KnownnessKnown KnownnessStatus = "known"

	// KnownnessUnknown — resolved unknown, reached ONLY by exhausting the retry
	// ladder. This is "checked, and the answer is no", never "the provider was
	// down when we asked".
	KnownnessUnknown KnownnessStatus = "unknown"
)

// KnownnessSource records WHICH level of the resolve produced a verdict, so a
// decision can be explained afterwards instead of being an unattributable
// boolean.
type KnownnessSource string

const (
	// KnownnessSourceBuiltin — level 1: the compiled-in major-coin list, or a
	// chain's native coin.
	KnownnessSourceBuiltin KnownnessSource = "builtin"

	// KnownnessSourceQuotable — level 2: the price provider quotes it by
	// (chain, contract).
	KnownnessSourceQuotable KnownnessSource = "quotable"

	// KnownnessSourceOverride — level 3: a human said so.
	KnownnessSourceOverride KnownnessSource = "override"
)

// KnownnessRecord is one row of the knownness registry.
type KnownnessRecord struct {
	Key      AssetKey
	Status   KnownnessStatus
	Source   KnownnessSource
	Override *bool // nil when no human has spoken
	Attempts int
	Symbol   string
}

// Verdict is what the filter tells the ledger path about one identity.
type Verdict struct {
	// Known is the only question the ledger path actually asks: may this leg be
	// recorded?
	Known bool

	// Status is the underlying stored state, carried so callers that report
	// rather than filter (reconciliation, #61) can tell a pending identity from
	// a convicted one.
	Status KnownnessStatus

	// Source names the level that decided, empty while pending.
	Source KnownnessSource
}

// Checked reports whether a verdict was actually reached, as opposed to still
// being queued. "Checked and unknown" and "not checked yet" are different facts
// and the reconciliation report depends on the difference.
func (v Verdict) Checked() bool {
	return v.Status == KnownnessKnown || v.Status == KnownnessUnknown
}

// KnownnessRegistry is the LOCAL store of knownness verdicts. Every method is a
// local database call — no implementation may reach the network, because the
// only caller on the read side is the sync hot path.
type KnownnessRegistry interface {
	// Get returns the stored record for an identity, or nil when the identity
	// has never been seen.
	Get(ctx context.Context, key AssetKey) (*KnownnessRecord, error)

	// Enqueue registers an identity for background probing if it is not already
	// present. It is idempotent and never overwrites an existing verdict.
	Enqueue(ctx context.Context, key AssetKey, symbol string) error
}

// KnownAssetFilter answers "may this asset enter the ledger" for an on-chain
// identity, by the three-level resolve of decision #37.
//
// The order is strict and each level exists because the one before it cannot
// answer the question alone:
//
//  1. THE BUILT-IN LIST — offline and instant, covering the bulk plus every
//     native coin. Generated from public token lists at build time.
//  2. QUOTABILITY at the price provider, read from the local registry. This is
//     what catches DeFi assets that no token list carries and never will: a debt
//     token, an LP share. Measured at 15/16 of the real positions, including a
//     debt token quoted at −0.9997. Read locally; the probe itself is a
//     background worker.
//  3. THE MANUAL OVERRIDE — the remainder, and the escape hatch for the one
//     accepted consequence of this design: a token in neither the list nor the
//     provider stays out of the balance until a human says otherwise.
//
// A token-list-style ADDRESS VERIFIER was measured and rejected as level 2: it
// cuts spam, but it also cuts 4 of 5 real DeFi positions, because a verifier
// answers "is this coin legitimate" while the balance needs "can this be
// valued". Any verifier is stricter than this system can afford.
type KnownAssetFilter struct {
	registry KnownnessRegistry
}

// NewKnownAssetFilter builds the filter over a local knownness registry.
//
// A nil registry yields a filter that admits everything, which is what keeps
// every existing test and any deployment that has not wired the registry
// working unchanged — the same nil-guard convention the asset registry uses.
func NewKnownAssetFilter(registry KnownnessRegistry) *KnownAssetFilter {
	return &KnownAssetFilter{registry: registry}
}

// Resolve returns the verdict for one on-chain identity.
//
// It performs NO network call. Level 2 is read out of the local table that the
// background worker fills; if the worker has not got there yet the answer is
// `pending`, which means "not in the ledger, not spam either". That is the
// design: a provider outage can slow a verdict down but can never convict a real
// token, and it can never stop sync.
func (f *KnownAssetFilter) Resolve(ctx context.Context, key AssetKey, symbol string) (Verdict, error) {
	if f == nil || f.registry == nil {
		// No registry wired: the filter is inert and admits everything. Failing
		// open here is deliberate — an unwired filter must not silently empty
		// the ledger.
		return Verdict{Known: true, Status: KnownnessKnown, Source: KnownnessSourceBuiltin}, nil
	}

	// The override is level 3 but it is checked FIRST, because being the last
	// resort in the resolution order means it outranks the automatic levels, not
	// that it is consulted last. It lives in the same row, so this costs nothing
	// extra.
	rec, err := f.registry.Get(ctx, key)
	if err != nil {
		return Verdict{}, err
	}
	if rec != nil && rec.Override != nil {
		return Verdict{
			Known:  *rec.Override,
			Status: statusFor(*rec.Override),
			Source: KnownnessSourceOverride,
		}, nil
	}

	// Level 1a: the native coin, known BY CONSTRUCTION. It is checked before the
	// list because a native coin has no contract and so can never appear in a
	// token list — without this, every native leg and all gas would fall to
	// level 2 and, since the price provider is not queried by contract for a
	// coin that has none, would eventually be convicted. 104 native legs in the
	// measured history: the largest position and the entire gas history.
	//
	// The SYMBOL IS CHECKED against the chain's expected native ticker. Granting
	// knownness to "any leg with the native sentinel" would readmit exactly the
	// junk leg this filter exists to stop: the provider emits legs with an
	// unrecognisable symbol, zero decimals and no contract, and the old
	// "native is anything not starting with 0x" predicate merged those into real
	// ETH.
	if key.IsNative() {
		if assetlist.IsNative(key.Chain, key.Contract, symbol) {
			return Verdict{Known: true, Status: KnownnessKnown, Source: KnownnessSourceBuiltin}, nil
		}
		// A native-shaped key whose symbol is not the chain's coin is NOT
		// native. It falls through to the registry like any other identity, and
		// since it has no contract to probe it will sit pending until a human
		// looks — visible in the queue, absent from the ledger, never silently
		// merged with the real coin.
		return f.fromRecord(ctx, key, symbol, rec)
	}

	// Level 1b: the compiled-in major-coin list.
	if _, ok := assetlist.Lookup(key.Chain, key.Contract); ok {
		return Verdict{Known: true, Status: KnownnessKnown, Source: KnownnessSourceBuiltin}, nil
	}

	// Level 2: quotability, read from the local table.
	return f.fromRecord(ctx, key, symbol, rec)
}

// fromRecord turns a stored registry row into a verdict, enqueueing the identity
// for probing the first time it is seen.
func (f *KnownAssetFilter) fromRecord(ctx context.Context, key AssetKey, symbol string, rec *KnownnessRecord) (Verdict, error) {
	if rec == nil {
		// First sighting: queue it for the background worker and report pending.
		// Enqueueing here rather than in the worker is what keeps the worker
		// from needing to know anything about transactions — it drains a queue,
		// and the queue is filled by whoever meets an unfamiliar asset.
		if err := f.registry.Enqueue(ctx, key, symbol); err != nil {
			return Verdict{}, err
		}
		return Verdict{Known: false, Status: KnownnessPending}, nil
	}

	return Verdict{
		Known:  rec.Status == KnownnessKnown,
		Status: rec.Status,
		Source: rec.Source,
	}, nil
}

func statusFor(known bool) KnownnessStatus {
	if known {
		return KnownnessKnown
	}
	return KnownnessUnknown
}
