# Goal: finish the asset-identity epic (#50)

**Repo:** `/Users/kislikjeka/projects/code/moontrack`, branch `main`, tracker — GitHub `kislikjeka/moontrack` (conventions in `docs/agents/issue-tracker.md`).
**Talk to the user in Russian.** This file is in English; the conversation is not.

## Destination

**Real-wallet transactions are counted correctly: balance and PnL agree with the positions Noves returns — for known tokens — and cost basis is computed from prices at transaction time.**

The goal is met when **either** holds:

1. The epic is closed — tickets #51–#62 all closed, final reviews done, findings fixed; **or**
2. Only **manual human validation** remains — every automatable piece is done, the reconcile report has been run against real data, and what is left needs a human eye (recognize a DeFi wrapper by name, decide the fate of a specific red row).

**What does NOT count as done:** green unit tests without a real-wallet run. This epic is about the numbers being right, not about the code compiling.

## Sources (follow the links, do not paraphrase)

- **Spec:** [#50](https://github.com/kislikjeka/moontrack/issues/50) — problem statement, 62 user stories, 13 decision blocks, test seams, open questions, accepted limitations.
- **Map:** [#34](https://github.com/kislikjeka/moontrack/issues/34) — Destination, the 5 user decisions taken while drafting (**do not revisit without an explicit request**), fog, out of scope.
- **Decision tickets (closed; the resolution is the comment on each):** [#35](https://github.com/kislikjeka/moontrack/issues/35) identity · [#36](https://github.com/kislikjeka/moontrack/issues/36) account-code constructor · [#37](https://github.com/kislikjeka/moontrack/issues/37) known-token filter · [#38](https://github.com/kislikjeka/moontrack/issues/38) protocol coverage · [#39](https://github.com/kislikjeka/moontrack/issues/39) prices · [#40](https://github.com/kislikjeka/moontrack/issues/40) re-sync · [#41](https://github.com/kislikjeka/moontrack/issues/41) reconcile report · [#42](https://github.com/kislikjeka/moontrack/issues/42) API contract · [#43](https://github.com/kislikjeka/moontrack/issues/43) linter · [#44](https://github.com/kislikjeka/moontrack/issues/44) lending · [#45](https://github.com/kislikjeka/moontrack/issues/45) price history · [#48](https://github.com/kislikjeka/moontrack/issues/48) protocol receipt · [#49](https://github.com/kislikjeka/moontrack/issues/49) genesis.
- **Domain:** `CONTEXT.md`, `docs/adr/`, `docs/agents/domain.md`.
- **Prior epic:** `tasks/goal-noves-epic-report.md` (epic #23).

**Important:** the body of decision [#41](https://github.com/kislikjeka/moontrack/issues/41) carries a stale wording (“`L > 0` + absent from P — red”). It is superseded by the **amendment from [#49](https://github.com/kislikjeka/moontrack/issues/49)**, left as a separate comment on #41. Read both.

## Work graph

```
#51 Style A ─→ #54 golden ─→ #55 constructor ─→ #56 registry ─┬─→ #57 receipt ─┐
                                                               └─→ #58 filter ─┤
#52 GetPriceAt ─────────────────────────────────────────────────────────────→ #59 UUID
#53 genesis ────────────────────────────────────────────────────────────────↗ (contract)
                                                                                │
                                         #60 per-leg rejection ─→ #61 report    │
                                         #62 API ───────────────────────────────┘
```

Blocking edges are **native GitHub issue dependencies** — they are the source of truth. Starting frontier: **#51, #52, #53** (no blockers).

## How to run the work

### The main session dispatches; it does not implement

The main session **writes no ticket code**. It:

1. Computes the frontier: open epic tickets whose `issue_dependencies_summary.blocked_by == 0`.
2. Picks **one** ticket. Order within the frontier is by number unless there is a reason otherwise.
3. Launches a **separate agent** on that ticket via the `/implement` skill.
4. Waits for completion, verifies the result, closes the ticket.
5. Recomputes the frontier. Repeats.

**Tickets are done strictly one at a time.** No parallelism between tickets, even when the frontier allows it (#52 and #53 are unblocked alongside #51 — still sequential). Reason: they touch overlapping code, and a green build between tickets is what progress is measured by.

**Context is cleared between tickets.** Each ticket is a fresh agent with its own context. The main session holds graph state only, not implementation detail.

### What the ticket agent does

The agent receives a ticket number and runs `/implement` on it. The ticket body has everything: what to build, acceptance criteria, links to the decisions. The agent must:

- Read the ticket body **and** the resolution of the decision ticket it references.
- Implement it, run the tests (**single-run, never watch mode**), confirm `go build ./...` is clean.
- Tick off the acceptance criteria in the ticket body.
- If the ticket touches `apps/frontend/src/` — run the `verify-ui` skill in a headless browser. Frontend work is not done without it.

The agent does **not close** the ticket. The main session closes it after verifying the result.

### Verification before closing a ticket

Before closing, the main session confirms:

- `go build ./...` is clean;
- `just backend-test` is green (single-run); integration tests with `TESTCONTAINERS_RYUK_DISABLED=true` (Colima);
- acceptance criteria are actually satisfied, not merely ticked;
- no regressions in neighbouring tests.

If something does not add up — **do not close**. Send it back via `SendMessage`, or launch a fresh agent with the specific finding.

## Real runs are required, not optional

**Explicitly permitted by the user: the database may be truncated, that is fine.** Zero users, no hand-entered data.

Run against the real wallet wherever it answers a question tests cannot:

- **After #59** (UUID migration + re-sync) — this *is* the ticket's acceptance: TRUNCATE, re-sync from Noves, reconcile **before the filter** on `(chain, contract) → quantity` with a by-name list of everything dropped.
- **After #61** (reconcile report) — run the report against real data and read the red category. **Red rows are expected behaviour**, not failure: the user accepted that "balance agrees with Noves" stops being reached automatically.
- **Along the way** — anywhere a measurement on real data is cheaper than an argument. Map #34 was built that way: nearly every decision was settled by measurement, not by reasoning.

**Noves API limits** (`docs/agents/noves-api-capture.md`): key from `.env`, **never print it**, **≤12 requests per session**, reuse `testdata` fixtures via `jq`. The reconcile report has a snapshot mode — capture the raw JSON once and run the report against it dozens of times. **Use it instead of burning quota.**

Infrastructure: `just up`, `just migrate-up`, `just dev` / `just dev-logs`. Backend logs via the `loki` MCP server (`observability-debugging` skill).

## Incidental findings

**A problem unrelated to the epic gets its own ticket immediately.** Do not fix it in passing; do not keep it in your head.

Then one rule decides what happens next: **take it now if it blocks or simplifies the current work**; otherwise leave it in the tracker and move on.

Already filed and outside the chain: [#43](https://github.com/kislikjeka/moontrack/issues/43) linter, [#46](https://github.com/kislikjeka/moontrack/issues/46) sync goroutine swallows an error after `202` was already returned, [#47](https://github.com/kislikjeka/moontrack/issues/47) redis pipeline error dropped into an empty branch. Pick them up if they get in the way.

The spec's open questions (Further Notes) are **not fog to ignore**: if implementation runs into one, that is a reason to file a ticket and decide, not to route around it silently. Two are likely to surface: cost basis 0 with status `resolved` (the mechanism is wider than genesis), and price-source priority versus proximity in time.

## Final phase — epic review

Once #51–#62 are all closed:

1. Launch final reviewers as **separate agents**. Not one — several, with different lenses. At minimum:
   - **Spec conformance** — was what #50 describes actually built, and did it drift along the way; pay particular attention to the accepted limitations (they must not have quietly become defects).
   - **Accounting correctness** — double entry, lots, FIFO, cost basis; the `ledger-development` lens.
   - **Identity and silent failures** — did new places appear where an error does not fail but instead creates a second account, merges two assets, or leaves a lot `resolved` at zero.
   - **Real data** — the reconcile report against the real wallet: every red row explained, every green category justified.
2. Reviewer findings get **fixed**, not written off. Substantial ones become their own tickets if they amount to standalone work.
3. Run `just check` (fmt + lint + test) clean.

**Useful:** `/code-review` already reviews a branch along two axes (standards + spec) in parallel agents — that is a ready tool for step 1, not a reason to write your own.

## Rules in force throughout

From `CLAUDE.md` (these override defaults):

- **`go build ./...` after any Go change.** Never end a session with a broken build.
- **Tests in single-run mode only.** If a command hangs, kill it rather than wait.
- **Never disable lint rules** — fix the underlying cause.
- **Frontend uses Bun**, not npm/node.
- **Commit all modified files**, not just docs. Commit/push only when the user asks.
- Before proposing architectural changes, read the ADRs and `CONTEXT.md` so proposals do not contradict what is already settled.

From the user's instructions for this work:

- **Do not revisit the map's decisions** without an explicit request. They were taken with arguments and measurements behind them; an agent "improving" them mid-implementation destroys work already done.
- **Do not discard fog.** If implementation uncovers a question — file a ticket, do not stay silent.
- **Red rows in the report are correct behaviour.** Do not tune the system toward a green report.

## Reporting

On completion, write up the results in the final PR body (not in `tasks/` — that duplicates it). It must contain:

- What each ticket delivered, one line each.
- **The real-run result**: does the balance agree, what sits in the red category, what is explained by what.
- Open questions left after the epic, and why they were left.
- What remains for manual validation, if the goal was met via option 2.
