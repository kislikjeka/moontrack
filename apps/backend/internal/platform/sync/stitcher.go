package sync

import (
	"math/big"
	"sort"
	"strings"
	"time"
)

// =============================================================================
// Bridge stitching (issue #33, design: docs/adr/0002-cross-chain-bridge-stitching.md)
//
// A bridge of the user's own funds between two chains is one economic event, but
// the provider decodes it as two independent single-chain transactions and links
// them in NEITHER direction. Left unstitched, the source leg books a disposal
// (realizing phantom PnL and consuming a lot by FIFO) and the destination leg
// opens a fresh lot at market — destroying exactly the cost basis this product
// exists to compute.
//
// Stitching pairs the two legs back into one cross-chain internal_transfer, so
// the TaxLotHook's carry-over path moves the lot across the bridge with its
// basis intact and no PnL realized.
//
// The matching heuristic is deliberately timid. A false positive destroys two
// real movements and fabricates a third; a false negative costs one recoverable
// phantom disposal. So on ANY ambiguity this refuses to stitch.
// =============================================================================

// Provider classification strings that identify a bridge leg. These are the
// provider's own type, carried verbatim through the port as ProviderType, and
// they are the only reliable bridge signal available: OperationType collapses
// sendToBridge onto OpSend and receiveFromBridge onto OpReceive, which makes a
// bridge leg indistinguishable from an ordinary transfer once mapped.
//
// They stay UNEXPORTED deliberately. The adapter's own type table (noves/
// adapter.go) holds the same two literals as map keys, and the duplication is
// preferable to the alternative: exporting them would publish a vendor's string
// vocabulary as part of the sync layer's API, which is exactly the coupling the
// de-vendoring rule forbids ("the sync platform layer must not name a vendor",
// CONTEXT.md). The coupling that must exist is confined to this one const block,
// and a drift would be caught by the bridge tests, which assert on the literal
// provider strings rather than on these constants.
const (
	providerTypeSendToBridge      = "sendToBridge"
	providerTypeReceiveFromBridge = "receiveFromBridge"
)

// BridgeFeeToleranceBP is the maximum shortfall, in basis points, between what a
// bridge send leg released and what the matching receive leg delivered. A bridge
// only ever WITHHOLDS value (its fee) — it never adds — so the receive is
// compared against the send with a one-sided tolerance.
//
// Calibrated against the real Base/Arbitrum history of wallet 0x9afc…811B
// (issue #33): across every genuinely matched pair the observed shortfall ranged
// from 0 to 2.121e-4 (0.0212%). 100 bp is ~47x the worst real case, and the
// match outcome is identical anywhere from 10 bp to 500 bp with zero ambiguous
// pairs — so the precise value is not load-bearing. It is set wide enough to
// absorb an unusually expensive bridge and still far too tight to let two
// unrelated movements of the same asset collide.
const BridgeFeeToleranceBP = 100

// BridgeMatchWindow is how long after a send leg a receive leg may arrive and
// still be considered the same bridge, and equally how long an unmatched
// pure-send is HELD before it is given up on and finalized as a plain
// transfer_out.
//
// Calibrated against the same real history: observed send→receive deltas were 2s
// and 1407s (23m). Zero ambiguity was observed at every window from 1h to 30d,
// which says the window is not what discriminates a match — the amount is. 24h
// therefore buys a wide safety margin for a slow bridge at no measured cost in
// false positives, while still bounding how long a bridged asset can sit absent
// from the portfolio before it is accounted for as an ordinary send.
const BridgeMatchWindow = 24 * time.Hour

// bridgeLeg is one side of a candidate bridge, projected out of a collected raw
// transaction into just the fields matching needs.
type bridgeLeg struct {
	// rawID indexes back into the caller's raw slice.
	rawIdx int
	// chain the leg was observed on.
	chain string
	// asset identity. Symbol alone is not enough — two chains' "USDC" are
	// different contracts, and matching on symbol is the point (a bridge moves
	// the same economic asset onto a different contract), but the symbol must
	// still be non-empty to be matchable at all.
	assetSymbol string
	// assetContract is the contract the asset has ON THIS LEG'S CHAIN. It plays
	// no part in matching — the two sides of a bridge have different contracts
	// by construction, which is why matching goes by symbol — but it is the
	// other half of the arriving asset's identity, and the receive leg is the
	// only place it exists. Carrying it is what lets the stitched source
	// transaction resolve the asset it receives instead of reusing the one it
	// sent (#70). Empty means the chain's native coin.
	assetContract string
	// amount moved, in base units, net of any same-transaction refund.
	amount *big.Int
	// decimals of the asset, needed to compare amounts across chains where the
	// same asset may be represented with different precision.
	decimals int
	minedAt  time.Time
}

// StitchDecision is what the stitcher concluded about a single collected raw
// transaction. It is a plan, not an effect: applying it is the caller's job,
// which is what keeps the stitcher a pure function.
type StitchDecision int

const (
	// StitchNone — process this raw normally. Everything that is not a bridge
	// leg lands here, as does a bridge leg the matcher declined to touch.
	StitchNone StitchDecision = iota
	// StitchAsSource — this pure-send leg matched a receive leg. Process it as a
	// cross-chain internal_transfer whose destination is DestChainID.
	StitchAsSource
	// StitchSuppress — this receive leg was absorbed into its matching send leg.
	// It records nothing of its own; the stitched source transaction owns the
	// whole movement.
	StitchSuppress
	// StitchHold — this pure-send leg has no matching receive yet and is still
	// inside the match window. Hold it unprocessed: no ledger transaction, no
	// disposal. It is retried next cycle, and once it ages past the window it is
	// released as a plain transfer_out.
	//
	// This is the hold-don't-reverse rule. Booking the disposal now and undoing
	// it when the receive arrives would briefly show phantom PnL and require
	// reversing a realized disposal — the one thing the tax-lot correctness rule
	// forbids.
	StitchHold
)

// StitchPlan maps a raw transaction's index within the input slice to what
// should happen to it. Indices absent from the map are StitchNone.
type StitchPlan struct {
	// Decisions holds every non-default decision, keyed by input index.
	Decisions map[int]StitchDecision
	// DestChain gives the destination chain for each StitchAsSource index — the
	// chain its matching receive leg was observed on.
	DestChain map[int]string
	// MatchedAmount gives, for each StitchAsSource index, the NET quantity in
	// base units that the matcher actually paired on: everything of that asset
	// that left the wallet, minus anything of it refunded in the same
	// transaction.
	//
	// It is carried on the plan rather than re-derived by the writer because the
	// two must agree. The matcher decides a pair belongs together by comparing
	// this number against the receive; if the writer independently arrives at a
	// different one, the destination lot opens at a quantity that never arrived
	// and the source is credited a quantity that never left. Double-entry would
	// still balance — the same wrong figure is used for both legs — so nothing
	// downstream catches it, and it surfaces only as reconciliation drift and a
	// silently wrong cost basis.
	MatchedAmount map[int]*big.Int
	// DestAsset gives, for each StitchAsSource index, the arriving asset as the
	// matching receive leg reported it on the destination chain.
	//
	// It rides on the plan for the same reason MatchedAmount does: the receive
	// leg is about to be suppressed, and this is the only moment its contract is
	// still in hand. The source leg cannot re-derive it — a bridged token has a
	// different contract on every chain — so a writer left to guess would name
	// the arriving asset with the departing asset's UUID, which is #70.
	DestAsset map[int]BridgeDestAsset
}

// BridgeDestAsset is the arriving side of a stitched bridge, as the receive leg
// reported it. Contract is empty for a chain's native coin.
type BridgeDestAsset struct {
	Contract string
	Symbol   string
	Decimals int
}

// Decision reports what to do with the raw at index i.
func (p StitchPlan) Decision(i int) StitchDecision {
	if p.Decisions == nil {
		return StitchNone
	}
	return p.Decisions[i]
}

// DestinationChain returns the destination chain for a stitched source leg, or
// "" when the raw is not a stitched source.
func (p StitchPlan) DestinationChain(i int) string {
	if p.DestChain == nil {
		return ""
	}
	return p.DestChain[i]
}

// NetAmount returns the net quantity the matcher paired on for a stitched source
// leg, or nil when the raw is not a stitched source.
func (p StitchPlan) NetAmount(i int) *big.Int {
	if p.MatchedAmount == nil {
		return nil
	}
	return p.MatchedAmount[i]
}

// DestinationAsset returns the arriving asset for a stitched source leg, and
// whether the raw is a stitched source at all.
func (p StitchPlan) DestinationAsset(i int) (BridgeDestAsset, bool) {
	if p.DestAsset == nil {
		return BridgeDestAsset{}, false
	}
	a, ok := p.DestAsset[i]
	return a, ok
}

// Stitch derives the bridge-stitching plan for one wallet's collected
// transactions. `now` bounds the hold: a pure-send older than BridgeMatchWindow
// is released rather than held forever.
//
// It is a PURE FUNCTION of its inputs — no repository reads, no clock of its
// own, no state carried between calls. That is a correctness requirement, not
// tidiness: the same collected raws must always yield the same stitch decision,
// or a wipe/replay would re-derive a different ledger than the original sync and
// the two would disagree about whether a disposal ever happened.
//
// The caller supplies decoded transactions in any order; matching does not
// depend on the order they arrive in.
func Stitch(txs []DecodedTransaction, walletAddress string, now time.Time) StitchPlan {
	plan := StitchPlan{
		Decisions:     make(map[int]StitchDecision),
		DestChain:     make(map[int]string),
		MatchedAmount: make(map[int]*big.Int),
		DestAsset:     make(map[int]BridgeDestAsset),
	}

	addr := strings.ToLower(walletAddress)

	var sends, receives []bridgeLeg
	for i := range txs {
		tx := &txs[i]
		switch {
		case isPureSendLeg(tx, addr):
			if leg, ok := newBridgeLeg(i, tx, DirectionOut, addr); ok {
				sends = append(sends, leg)
			}
		case isReceiveLeg(tx, addr):
			if leg, ok := newBridgeLeg(i, tx, DirectionIn, addr); ok {
				receives = append(receives, leg)
			}
		}
	}

	// claimed guards the 1:1 invariant across the whole pass: a send leg that one
	// receive already absorbed can never be handed to a second receive, and a
	// receive that matched can never be re-matched. Without this a single send
	// could be stitched into two internal transfers, conjuring value.
	claimedSends := make(map[int]bool)

	// Receives drive the match — only they carry the self-signal. Deterministic
	// order matters: two receives competing for one send must resolve the same
	// way on every replay, so the oldest receive is offered the send first.
	sortLegsByTime(receives)

	matchedReceives := make(map[int]bool)

	for _, r := range receives {
		matched, ok := matchSend(r, sends, claimedSends)
		if !ok {
			continue // 0 or >=2 candidates: leave both sides standalone
		}
		claimedSends[matched.rawIdx] = true
		matchedReceives[r.rawIdx] = true
		plan.Decisions[matched.rawIdx] = StitchAsSource
		plan.DestChain[matched.rawIdx] = r.chain
		plan.MatchedAmount[matched.rawIdx] = matched.amount
		plan.DestAsset[matched.rawIdx] = BridgeDestAsset{
			Contract: r.assetContract,
			Symbol:   r.assetSymbol,
			Decimals: r.decimals,
		}
		plan.Decisions[r.rawIdx] = StitchSuppress
	}

	// Any bridge leg still unclaimed is a straggler: its counterpart has not been
	// collected yet. Hold it while that counterpart could still plausibly show
	// up; release it once it cannot.
	//
	// BOTH sides are held, not just the send. The chains of a wallet are
	// collected independently (issues #28/#29), so the destination chain is
	// routinely collected before the source — a receive arriving first is
	// ordinary, not exotic. Booking that receive immediately as a transfer_in
	// would mark its raw processed and drop it out of the pending set, so the
	// send arriving next cycle would find nothing to match and age out to
	// transfer_out. The wallet ends up with a transfer_in plus a transfer_out:
	// exactly the fabricated disposal and reset cost basis this whole ticket
	// exists to prevent.
	hold := func(idx int, minedAt time.Time) {
		if now.Sub(minedAt) < BridgeMatchWindow {
			plan.Decisions[idx] = StitchHold
		}
		// Past the window: no decision is recorded, so the leg falls through to
		// StitchNone and is processed normally — transfer_out for a send,
		// transfer_in for a receive. A disposal is realized here and only here,
		// at the point it can no longer be contradicted by an arriving
		// counterpart. Releasing rather than holding forever is what stops a
		// bridge from a chain the user never enabled stranding the asset outside
		// the ledger permanently.
	}

	for _, s := range sends {
		if !claimedSends[s.rawIdx] {
			hold(s.rawIdx, s.minedAt)
		}
	}
	for _, r := range receives {
		if !matchedReceives[r.rawIdx] {
			hold(r.rawIdx, r.minedAt)
		}
	}

	return plan
}

// matchSend finds the unique send leg that r could have come from. It returns
// ok=false on ANY ambiguity — zero candidates or more than one — because a
// wrong pairing destroys two real transactions and invents a third, while a
// missed pairing merely leaves a recoverable phantom disposal.
func matchSend(r bridgeLeg, sends []bridgeLeg, claimed map[int]bool) (bridgeLeg, bool) {
	var found bridgeLeg
	count := 0

	for _, s := range sends {
		if claimed[s.rawIdx] {
			continue
		}
		if !legsCompatible(s, r) {
			continue
		}
		count++
		if count > 1 {
			return bridgeLeg{}, false // ambiguous: refuse
		}
		found = s
	}

	return found, count == 1
}

// legsCompatible applies the conservative 1:1 predicate to one (send, receive)
// pair. Every clause is a veto; all must hold.
func legsCompatible(s, r bridgeLeg) bool {
	// A bridge crosses chains. Same-chain is some other movement entirely, and
	// stitching it would collapse two real same-chain transactions into one.
	if s.chain == r.chain {
		return false
	}
	// Same asset. A bridge that hands back a different asset is a swap, and
	// swapping IS a disposal — exactly the event stitching must not erase.
	if !strings.EqualFold(s.assetSymbol, r.assetSymbol) {
		return false
	}
	// The destination must follow the source, within the window.
	delta := r.minedAt.Sub(s.minedAt)
	if delta < 0 || delta > BridgeMatchWindow {
		return false
	}
	return amountWithinFeeTolerance(s, r)
}

// amountWithinFeeTolerance reports whether the received amount is consistent
// with the sent amount after a plausible bridge fee: received must be at most
// sent (a bridge only withholds) and at least sent minus the tolerance.
//
// The comparison is done in a shared scale so assets represented with different
// decimals on the two chains still compare correctly — the same USDC is 6
// decimals on one chain and can be 18 on another, and comparing base units
// directly would be off by a factor of 10^12.
func amountWithinFeeTolerance(s, r bridgeLeg) bool {
	if s.amount == nil || r.amount == nil {
		return false
	}
	if s.amount.Sign() <= 0 || r.amount.Sign() <= 0 {
		return false
	}

	sent, received := alignScales(s, r)

	// received <= sent — a bridge never delivers more than it took.
	if received.Cmp(sent) > 0 {
		return false
	}
	// received >= sent * (10000 - tolerance) / 10000
	minAccepted := new(big.Int).Mul(sent, big.NewInt(10000-BridgeFeeToleranceBP))
	scaled := new(big.Int).Mul(received, big.NewInt(10000))
	return scaled.Cmp(minAccepted) >= 0
}

// alignScales rescales two base-unit amounts onto a common number of decimals so
// they can be compared as quantities rather than as raw integers.
func alignScales(s, r bridgeLeg) (sent, received *big.Int) {
	sent = new(big.Int).Set(s.amount)
	received = new(big.Int).Set(r.amount)

	switch {
	case s.decimals < r.decimals:
		sent.Mul(sent, pow10(r.decimals-s.decimals))
	case r.decimals < s.decimals:
		received.Mul(received, pow10(s.decimals-r.decimals))
	}
	return sent, received
}

func pow10(n int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}

// isReceiveLeg reports whether tx is a bridge receive into the user's own
// wallet — the trigger side. The provider type is the signal; the recipient
// check is what makes it a SELF-bridge rather than someone bridging to us.
func isReceiveLeg(tx *DecodedTransaction, walletAddr string) bool {
	if tx.ProviderType != providerTypeReceiveFromBridge {
		return false
	}
	if tx.Status == "failed" {
		return false
	}
	for _, t := range tx.Transfers {
		if t.Direction == DirectionIn && strings.ToLower(t.Recipient) == walletAddr {
			return true
		}
	}
	return false
}

// isPureSendLeg reports whether tx is a bridge send that is a genuine
// cross-chain outbound rather than a bridge-as-swap.
//
// It is defined as the exact complement of isCrossAssetRoundTrip, and that
// shared definition is load-bearing: the classifier uses the same predicate to
// decide whether to book a bridge leg as a swap. If the two ever disagreed, a
// leg could be booked as a swap while the stitcher still treated it as a pure
// send — recording the trade AND stitching the same value across a bridge.
func isPureSendLeg(tx *DecodedTransaction, walletAddr string) bool {
	if tx.ProviderType != providerTypeSendToBridge {
		return false
	}
	if tx.Status == "failed" {
		return false
	}
	if _, ok := primaryOutAsset(tx, walletAddr); !ok {
		return false
	}
	return !isCrossAssetRoundTrip(tx.Transfers)
}

// isCrossAssetRoundTrip reports whether the transfers show value leaving in one
// asset and a DIFFERENT asset arriving in the same transaction — the shape that
// makes a bridge leg a bridge-as-swap rather than a cross-chain movement.
//
// sendToBridge is overloaded, and the naive test — "received[] is empty" — is
// wrong on real data. Calibration against the real Base/Arbitrum history found
// that most genuine pure-sends DO carry a same-transaction inbound leg: either a
// dust refund of the very asset being sent (down to 1e-6 of it), or a small
// native-coin gas drop on the source chain. Rejecting those would refuse to
// stitch the majority of real bridges.
//
// What actually distinguishes a bridge-as-swap is receiving back a DIFFERENT
// asset, because that is a disposal: value left in one asset and returned in
// another. So the test is asset identity, not emptiness. A different-asset
// inbound disqualifies the leg regardless of size — the provider supplies no
// prices, so there is no way to tell a negligible gas drop from a small genuine
// swap, and under ADR-0002's asymmetry the safe reading is "not a pure send".
func isCrossAssetRoundTrip(transfers []DecodedTransfer) bool {
	out := make(map[string]bool)
	var in []string
	for _, t := range transfers {
		if t.AssetSymbol == "" {
			continue
		}
		key := strings.ToLower(t.AssetSymbol)
		switch t.Direction {
		case DirectionOut:
			out[key] = true
		case DirectionIn:
			in = append(in, key)
		}
	}
	if len(out) == 0 || len(in) == 0 {
		return false
	}
	for _, asset := range in {
		if !out[asset] {
			return true // an asset arrived that did not leave: a trade happened
		}
	}
	return false
}

// primaryOutAsset returns the symbol of the single asset leaving the wallet.
// A send leg moving more than one distinct asset out is not a 1:1 bridge and is
// rejected: stitching it would have to pick one asset and silently drop the rest.
func primaryOutAsset(tx *DecodedTransaction, walletAddr string) (string, bool) {
	symbol := ""
	for _, t := range tx.Transfers {
		if t.Direction != DirectionOut {
			continue
		}
		if t.Sender != "" && strings.ToLower(t.Sender) != walletAddr {
			continue
		}
		if t.AssetSymbol == "" {
			continue
		}
		if symbol == "" {
			symbol = t.AssetSymbol
			continue
		}
		if !strings.EqualFold(symbol, t.AssetSymbol) {
			return "", false // multi-asset outflow: not a simple bridge
		}
	}
	return symbol, symbol != ""
}

// newBridgeLeg projects a decoded transaction into a matchable leg, netting the
// primary direction against any same-transaction return of the same asset.
//
// Netting matters on the send side: a bridge that takes 0.0089 ETH and
// immediately refunds 8.8e-9 ETH in the same transaction actually released the
// difference, and that difference is what the destination chain will deliver.
// Comparing the gross amount instead would make the receive look short by the
// refund and could push a real pair outside the fee tolerance.
func newBridgeLeg(idx int, tx *DecodedTransaction, primary TransferDirection, walletAddr string) (bridgeLeg, bool) {
	var (
		symbol   string
		contract string
		decimals int
		gross    = big.NewInt(0)
		offset   = big.NewInt(0)
		found    bool
	)

	for _, t := range tx.Transfers {
		if t.Amount == nil || t.AssetSymbol == "" {
			continue
		}
		if t.Direction == primary {
			if !partyMatches(t, primary, walletAddr) {
				continue
			}
			if symbol == "" {
				symbol = t.AssetSymbol
				contract = t.ContractAddress
				decimals = t.Decimals
			} else if !strings.EqualFold(symbol, t.AssetSymbol) {
				return bridgeLeg{}, false // multi-asset: not matchable 1:1
			}
			gross.Add(gross, t.Amount)
			found = true
		}
	}
	if !found || gross.Sign() <= 0 {
		return bridgeLeg{}, false
	}

	// Net out any same-asset movement in the opposite direction (the refund).
	for _, t := range tx.Transfers {
		if t.Amount == nil || t.Direction == primary {
			continue
		}
		if !strings.EqualFold(t.AssetSymbol, symbol) {
			continue
		}
		if !partyMatches(t, oppositeDirection(primary), walletAddr) {
			continue
		}
		offset.Add(offset, t.Amount)
	}

	net := new(big.Int).Sub(gross, offset)
	if net.Sign() <= 0 {
		return bridgeLeg{}, false
	}

	return bridgeLeg{
		rawIdx:        idx,
		chain:         tx.ChainID,
		assetSymbol:   symbol,
		assetContract: contract,
		amount:        net,
		decimals:      decimals,
		minedAt:       tx.MinedAt,
	}, true
}

// partyMatches checks that the wallet is the party it should be for a transfer
// of the given direction: the sender of an outflow, the recipient of an inflow.
// An empty address on the provider's side is accepted rather than assumed wrong.
func partyMatches(t DecodedTransfer, dir TransferDirection, walletAddr string) bool {
	party := t.Sender
	if dir == DirectionIn {
		party = t.Recipient
	}
	if party == "" {
		return true
	}
	return strings.ToLower(party) == walletAddr
}

func oppositeDirection(d TransferDirection) TransferDirection {
	if d == DirectionOut {
		return DirectionIn
	}
	return DirectionOut
}

// sortLegsByTime orders legs oldest-first, breaking ties on the raw index so the
// ordering is total and therefore reproducible. Determinism here is what makes
// the whole stitch decision replay-safe when two legs share a timestamp — a
// wallet's transactions landing in one block is ordinary, not exotic.
func sortLegsByTime(legs []bridgeLeg) {
	sort.SliceStable(legs, func(i, j int) bool {
		if !legs[i].minedAt.Equal(legs[j].minedAt) {
			return legs[i].minedAt.Before(legs[j].minedAt)
		}
		return legs[i].rawIdx < legs[j].rawIdx
	})
}
