# #29 — Oldest-first high-water cursor + completeness

Parent: #23 (Noves migration). Scope: collect history **oldest→newest** with an
inclusive high-water-mark cursor, so an interrupted deep sync resumes forward and
can never silently skip the oldest (lowest-cost-basis) history.

## Live probe (plan prerequisite) — DONE

Probed the live Noves API (5 requests, budget 12, key never printed).
Address `0x9afc…811B`, chain `base`.

| Question | Result |
|---|---|
| `sort=asc` supported | **Yes** — native ascending confirmed |
| pagination-direction-follows-sort | **Yes** — the asc `nextPageUrl` carries `startBlock=<next>&endBlock=&sort=asc` plus `ignoreTransactions=<boundary hash>` to dedupe the boundary block |
| cursor stability / `startTimestamp` inclusive | **Inclusive (`>=`)** — the anchor tx comes back as `items[0]` |
| `startTimestamp` unit | **SECONDS** — millisecond values are rejected with HTTP 400 `"Start Timestamp must be less than the current time"` |

**Verdict:** use the **native ascending** path. The range-anchor + reorder fallback
is not needed.

**Bug the probe found:** the client sends `since.UnixMilli()`, so *every* collect with
a non-zero `since` fails with HTTP 400. Broken since #25; masked in production by
#28's per-chain error isolation (the chain is marked errored and the loop continues).

## Design decisions

- **Cursor = contiguous high-water mark, not max.** Advance only across the
  unbroken ascending prefix of successfully-persisted transactions. The moment one
  transaction fails to serialize or upsert, the cursor stops there — everything
  after it is re-fetched next cycle rather than skipped forever.
- **Inclusive boundary (`>=`).** The cursor tx itself comes back on the next run;
  the idempotent upsert absorbs the duplicate. Never store `cursor+1ns` to
  "avoid" the dupe — that is exactly how a same-timestamp sibling gets skipped.
- **Per-page persistence.** The client streams pages to a callback; the collector
  persists and advances the cursor after each page. An interrupted deep sync
  (ctx cancel, rate-limit exhaustion, 5xx) keeps everything already collected and
  resumes forward from the last good page.
- Ascending order is the collector's **invariant**, not an assumption: it sorts
  each page by `MinedAt` before folding, so a provider that ignores `sort` cannot
  corrupt the high-water mark.

## Plan

- [x] Live probe + record results
- [x] Fix `startTimestamp` unit ms → seconds (test-first)
- [x] Stream pages via callback so collection persists incrementally
- [x] Contiguous high-water fold (stop at first un-persisted tx)
- [x] Inclusive-boundary handling documented + tested
- [x] Unit tests for each behavior; full suite at the end

## Review

**What changed**

- `noves/client.go` — `startTimestamp` now in **seconds** (was `UnixMilli()`, an
  HTTP 400 on every non-zero `since`). New `StreamTransactions` pages via the
  `onPage` callback; `GetTransactions` is now a thin accumulator over it.
- `noves/adapter.go` — mirrors the same split, converting each page to domain
  types before handing it on.
- `sync/port.go` — `TransactionDataProvider` gains `StreamTransactions`, and its
  doc now states the ordering + inclusive-boundary contract the collector relies on.
- `sync/collector.go` — per-chain collection extracted into `collectChain`
  (streams, persists and advances the cursor per page) plus `storeAscending`
  (defensive sort + contiguous-prefix fold, reports page contiguity).
- `sync/high_water_cursor_test.go` — 6 new tests covering the cursor contract.

**Verification**

- `go build ./...`, `go vet ./...`, `gofmt` all clean.
- Full backend suite green (`go test ./... -short`), 21 packages.
- **Live end-to-end run through the real client**: no HTTP 400, results
  ascending, and `first tx == anchor` — the inclusive boundary confirmed against
  the production API, not just mocks.

**Two-axis code review — findings applied**

1. *Spec, correctness:* contiguity was enforced only WITHIN a page — a gap on
   page 1 was forgotten, so a later clean page pushed the cursor past the
   unstored transaction. This was the exact silent-skip the ticket exists to
   prevent. Fixed by carrying a `contiguous` flag across the whole stream;
   regression test `TestCollect_CursorContiguityHoldsAcrossPages` fails on the
   old code and passes on the new.
2. *Spec:* a failed `SetChainCollectCursor` write was logged and ignored, letting
   a later page advance past an un-persisted mark — the same skip by another
   route. The cursor now freezes for the rest of the cycle.
3. *Spec:* the non-streaming error path returned `0, 0, err`, discarding counts.
4. *Standards (Speculative Generality):* `StreamingTransactionDataProvider` was
   an optional interface with a runtime type assertion, but there is exactly one
   production provider — the fallback branch was dead. Folded streaming into
   `TransactionDataProvider` and deleted the branch and the second `var _` check.

**Notes**

- `golangci-lint` reports `typecheck` noise on the sync test mocks (it doesn't
  resolve the embedded `mock.Mock`). Confirmed identical on clean `main` —
  pre-existing, unrelated to this work.
- The ms→seconds fix is technically outside #29's literal text, but the ticket's
  mandated probe is what surfaced it, and the cursor cannot be exercised at all
  while every windowed request 400s.
