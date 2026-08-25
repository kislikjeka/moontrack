package ledger

// MetaLegPair is the entry-metadata key that names the two legs of one asset
// movement: the decrease that gives the asset up and the increase that takes it
// on. Both legs of a pair carry the same value; nothing else in the transaction
// carries it at all.
//
// # Why the pair is stamped instead of inferred
//
// [NewTaxLotHook] must know which acquisition inherits which disposal's cost
// basis. It used to answer that by matching on the asset registry UUID, which
// held only because the two legs of a bridge carried the SAME UUID — and they
// carried the same UUID only because #70 built the destination account out of
// the destination chain and the SOURCE asset's id. Fixing that defect gives the
// legs their two rightful identities and, with them, no shared key to match on:
// the carry-over would break silently, the destination lot would open at market
// price, and the balance would still tie out.
//
// Neither of the two properties one might infer the pair from survives contact
// with real data:
//
//   - "the gas leg is the native asset, the moved leg is not" — false the
//     moment the native coin itself is bridged, which is the ordinary case.
//   - "the legs differ by chain" — false for a same-chain internal transfer
//     between two wallets, where carry-over works today and must keep working.
//
// Gas is booked as an asset DECREASE on a wallet account, so it stands in the
// candidate set beside the leg being moved: every bridge transaction offers the
// hook two decreases, not one. Under lending supply/withdraw the same shape
// already occurs in production data — a supply paid for in the very coin being
// supplied — where correctness rests today on FIFO happening to consume the
// same lot twice, which is an accident of ordering rather than an invariant.
//
// # Why metadata and not slice order
//
// The hook is a POST-BALANCE hook: it reads entries that have already been
// written, so a convention about the order of a slice inside one handler cannot
// reach it. The pair has to be data. Being data, it also replays for free and
// is legible in the stored JSONB when a live transaction has to be explained.
//
// Swap is deliberately unmarked. Its two sides are different economic assets
// and realizing gain there is the intended accounting, so there is no pair to
// name.
const MetaLegPair = "leg_pair"

// legPair reads the pair marker off an entry, reporting whether one is present.
//
// An entry without a marker — gas, income, expense, a swap side, or anything a
// handler predating the marker wrote — is not part of any pair and never
// carries a basis across.
func legPair(e *Entry) (string, bool) {
	if e == nil || e.Metadata == nil {
		return "", false
	}
	v, ok := e.Metadata[MetaLegPair].(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}
