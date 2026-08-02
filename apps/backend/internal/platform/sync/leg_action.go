package sync

// Leg actions the sync provider stamps on each individual token movement.
//
// The provider already draws the line this package needs — per leg, not per
// transaction. A lending supply arrives as two legs, `deposited` (the principal
// leaving the wallet) and `collateralSharesMinted` (the aToken coming back);
// an LP add arrives as `liquidityAdded` legs plus one `lpTokensMinted`. The
// distinction is in the data, so MoonTrack reads it rather than re-deriving it
// from ticker shapes (issue #57).
const (
	// Receipt actions: the protocol handing back a claim on what it just took.
	ActionCollateralSharesMinted = "collateralSharesMinted"
	ActionCollateralSharesBurned = "collateralSharesBurned"
	ActionDebtSharesMinted       = "debtSharesMinted"
	ActionDebtSharesBurned       = "debtSharesBurned"
	ActionLPTokensMinted         = "lpTokensMinted"
	ActionLPTokenMinted          = "lpTokenMinted"
	ActionLPTokensBurned         = "lpTokensBurned"
	ActionLPTokenBurned          = "lpTokenBurned"

	// Principal actions: value genuinely entering or leaving the wallet.
	ActionDeposited        = "deposited"
	ActionWithdrawn        = "withdrawn"
	ActionLiquidityAdded   = "liquidityAdded"
	ActionLiquidityRemoved = "liquidityRemoved"
	ActionRewardsReceived  = "rewardsReceived"
	ActionBorrowed         = "borrowed"
	ActionRepaid           = "repaid"
)

// receiptActions is the closed set of leg actions that identify a PROTOCOL
// RECEIPT — an aToken, a debt token, an LP receipt: a token the protocol mints
// to record a position it is already holding for you.
//
// A receipt is not a position. Recognizing one requires no knowledge of the
// token itself, which is the whole point: the receipt token is a real, quoted,
// perfectly ordinary token — the known-asset criterion (#58) says so correctly.
// What makes it not a position is the ROLE it plays in this transaction, and
// the role is what the action names. The two questions are independent and both
// are needed.
//
// Booking a receipt alongside its principal counts one supply twice: the
// measurement in #44 found 18 real Aave supplies recorded with the principal in
// `collateral.` AND the aToken in `collateral.` for the same event, each pair
// internally balanced, so double-entry could never catch it — the defect is in
// cardinality, not arithmetic.
//
// The set is deliberately closed and short. It is an EXTERNAL vocabulary that
// MoonTrack does not control, so a protocol emitting a receipt action absent
// from this list will let its receipt through. That is the accepted limitation
// (#57); the mitigation is that an unrecognized action is LOGGED rather than
// silently assumed, so the gap is visible instead of invisible.
var receiptActions = map[string]bool{
	ActionCollateralSharesMinted: true,
	ActionCollateralSharesBurned: true,
	ActionDebtSharesMinted:       true,
	ActionDebtSharesBurned:       true,
	ActionLPTokensMinted:         true,
	ActionLPTokenMinted:          true,
	ActionLPTokensBurned:         true,
	ActionLPTokenBurned:          true,
}

// principalActions is the closed set of leg actions known to be PRINCIPAL —
// value that genuinely moved, which must reach the ledger.
//
// It is enumerated rather than taken as the complement of the receipt set so
// that "unrecognized" can be a distinguishable third answer. Membership is also
// what identifies a protocol interaction at all: these actions appear inside a
// lending market or a liquidity pool and nowhere else, which is how the
// classifier tells one from an ordinary transfer without consulting a protocol
// name — necessary because the provider leaves `protocol.name` null on most
// real data.
var principalActions = map[string]bool{
	ActionDeposited:        true,
	ActionWithdrawn:        true,
	ActionLiquidityAdded:   true,
	ActionLiquidityRemoved: true,
	ActionRewardsReceived:  true,
	ActionBorrowed:         true,
	ActionRepaid:           true,
}

// plainActions are the actions of an ORDINARY movement — one that has nothing
// to do with a protocol position and therefore cannot be a receipt. They are
// enumerated for one reason only: so that "unrecognized" means genuinely
// unrecognized. Without them every `received` and every `bought` would be
// reported as an action we have never seen, and the report that exists to make
// a rare gap visible would be the noisiest line in the log.
var plainActions = map[string]bool{
	"received":           true,
	"sent":               true,
	"bought":             true,
	"paid":               true,
	"paidGas":            true,
	"feesPaid":           true,
	"bridged":            true,
	"refundedByContract": true,
	"approved":           true,
	"revoked":            true,
	"minted":             true,
	"burned":             true,
	"swapped":            true,
	"staked":             true,
	"unstaked":           true,
}

// IsReceiptLeg reports whether a leg is a protocol receipt and must therefore
// not reach the ledger.
func IsReceiptLeg(action string) bool {
	return receiptActions[action]
}

// IsUnknownLegAction reports whether the provider stamped an action that
// appears in none of MoonTrack's three closed sets — receipt, protocol
// principal, or plain movement.
//
// This is the case the receipt rule cannot decide. The leg is handled as
// principal (see the caller), but a receipt minted under an action nobody here
// has seen is indistinguishable from a new kind of genuine movement, so it is
// reported rather than assumed.
//
// An empty action is not unknown: plenty of legs carry no action at all, and
// treating "absent" as "unrecognized" would bury the signal under noise.
func IsUnknownLegAction(action string) bool {
	if action == "" {
		return false
	}
	return !receiptActions[action] && !principalActions[action] && !plainActions[action]
}

// lendingActions are the protocol actions specific to a lending market. Their
// presence on any leg is what identifies a lending transaction.
var lendingActions = map[string]bool{
	ActionCollateralSharesMinted: true,
	ActionCollateralSharesBurned: true,
	ActionDebtSharesMinted:       true,
	ActionDebtSharesBurned:       true,
	ActionDeposited:              true,
	ActionWithdrawn:              true,
	ActionBorrowed:               true,
	ActionRepaid:                 true,
}

// rewardActions are the protocol actions that mark a claimed reward. Kept as a
// set rather than a bare equality check so it reads like its two siblings and
// so a second spelling can be added in one place.
var rewardActions = map[string]bool{
	ActionRewardsReceived: true,
}

// liquidityActions are the protocol actions specific to a liquidity pool.
var liquidityActions = map[string]bool{
	ActionLPTokensMinted:   true,
	ActionLPTokenMinted:    true,
	ActionLPTokensBurned:   true,
	ActionLPTokenBurned:    true,
	ActionLiquidityAdded:   true,
	ActionLiquidityRemoved: true,
}
