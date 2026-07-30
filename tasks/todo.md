# #33 — Bridge stitching: receive-triggered conservative 1:1 + hold

Parent: #23 (Noves migration). Design: **ADR-0002**. Scope: stitch cross-chain
bridge legs into ONE cross-chain `internal_transfer` (delivered by #32), so a
self-bridge does not fabricate a disposal and reset cost basis.

## Calibration (plan prerequisite) — DONE

Real history of wallet `0x9afc…811B` on base/arbitrum/eth: 245 transactions,
6 live Noves requests (budget 12, key never printed, `nextPageUrl` followed
exactly as production does). **15 `sendToBridge` + 8 `receiveFromBridge`** legs.

**Pure-send vs round-trip split.** 9 of 15 send legs are stitch candidates. The
disqualifier is a same-tx inbound to the wallet in a **different asset** than the
one sent, after dropping `paidGas` / `feesPaid` / `refund` legs. `received[]`
being non-empty is emphatically NOT the test: 7 real pure-sends carry a
same-asset `refund` dust leg (down to 1e-6 of the sent amount) or a native-coin
gas-drop. A rule keyed on "received[] is empty" would refuse to stitch most real
bridges.

**Fee tolerance.** Observed real bridge fee: **0 … 2.121e-4 (0.0212%)**.
Chosen **1%** — 47× the worst observed, and the match outcome is *identical*
across 0.1% … 5% with zero ambiguity, so the exact value is not load-bearing.

**Time window.** Observed send→receive deltas: **2s** and **1407s (23m)**.
Chosen **24h**. Zero ambiguity at every window from 1h to 30d — the window is not
what discriminates; the amount is. 24h is comfortably conservative.

**Result at (1%, 24h): 2 unique matches, 0 ambiguous**, 6 receives and 7 sends
left standalone because their counterparts sit on chains outside the Enabled set.
That is the designed false-negative behaviour, not a defect.

## Design decisions

- **Receive-triggered, matched backward.** Only the receive leg carries a usable
  self-signal (`to.address` == the wallet). The send leg's recipient is the
  bridge contract or the null address, so it can never point forward.
- **Pure function of the collected raws.** `Stitch()` takes the pending raws and
  returns a plan; it holds no state. Replay/wipe re-derives the same decision.
- **Conservative 1:1.** Same wallet, same asset, cross-chain, `received <= sent`
  within the fee tolerance, receive after send within the window. 0 or >= 2
  candidates on EITHER side → no stitch. A send already claimed by an earlier
  receive is not reusable.
- **Hold-don't-reverse, symmetrically.** An unmatched bridge leg younger than the
  window is held pending — no ledger transaction, no disposal. Past the window it
  is released (`transfer_out` for a send, `transfer_in` for a receive). A
  disposal is realized only once it can never be undone. The hold covers BOTH
  sides: chains are collected independently (#28/#29), so a receive arriving
  before its send is ordinary, and booking it eagerly would consume the raw and
  leave the later send with nothing to match.
- **One amount, one source of truth.** The net matched quantity (gross minus any
  same-tx refund) travels from the matcher to the ledger writer on the plan
  rather than being re-derived. If the two disagreed the transaction would still
  balance, so nothing downstream would catch it.
- **Round-trip is never stitched** and never held: it is released immediately to
  the local classifier, which books it as a swap.

## Plan

- [x] Calibrate fee tolerance + pure-send split against real Base wallet history
- [x] `stitcher.go` — pure matcher over collected raws
- [x] Stitch phase wired between collect and process
- [x] Hold stragglers (both sides); age out past the window
- [x] Round-trip → local swap, never stitched
- [x] Port-seam tests for every acceptance criterion
- [x] `go build` / `go vet` / `go test ./... -short` green
- [x] `/code-review` both axes; fix real Spec findings

## Review

Two-axis code review found **two real correctness bugs**, both fixed with
regression tests that fail on the pre-fix code:

1. **Spec/correctness — the ledger recorded a different amount than the matcher
   matched on.** `newBridgeLeg` nets the same-tx refund off the sent amount and
   sums split outflows, but `buildInternalTransferData` took the *first* out
   transfer's gross amount. The destination lot would open at a quantity that
   never arrived while the source was credited a quantity that never left — and
   because the same wrong figure is used for both legs, double-entry still
   balances, so nothing downstream catches it. Fixed by carrying the matched net
   amount on the `StitchPlan` and writing that. Tests:
   `TestStitch_LedgerAmountIsTheNetMatchedAmount`,
   `TestStitch_LedgerAmountSumsSplitOutflows`.

2. **Spec/correctness — the hold was send-side only.** An unmatched receive was
   booked immediately as `transfer_in` and marked processed, dropping it from the
   pending set; the send arriving next cycle then found nothing to match and aged
   out to `transfer_out`. Result: `transfer_in` + `transfer_out` — the exact
   fabricated disposal this ticket exists to prevent, reachable by ordinary
   independent per-chain collection. Fixed by holding both sides symmetrically,
   with a bounded release so nothing is stranded. Tests:
   `TestStitch_ReceiveArrivingFirst_IsHeldNotBooked`,
   `TestStitch_ReceiveHeldThenSendArrives_Stitches`,
   `TestStitch_AgedOutReceive_BecomesTransferIn`.

Standards findings applied: replaced a hand-rolled O(n²) insertion sort with
`sort.SliceStable` (the package's existing convention in `collector.go` and
`processor.go`); unified the round-trip predicate so the classifier and the
stitcher share one definition instead of two that could drift apart. The bare
provider-type constants were kept unexported deliberately — exporting them would
publish a vendor string vocabulary as sync's public API, against the de-vendoring
rule — with the reasoning recorded at the const block.

**Verification.** `go build ./...`, `go vet ./...`, gofmt all clean; full backend
suite green (`go test ./... -short`, 21 packages). Beyond the unit tests, all
**245 real captured transactions** were replayed through the real Noves adapter
and the real stitcher: 2 stitched pairs, 0 ambiguous, 0 false positives, with the
genuine bridge-as-swap correctly left as a swap — the same result before and
after the fixes.
