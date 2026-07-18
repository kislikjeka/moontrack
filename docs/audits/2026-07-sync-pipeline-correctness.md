# Security & Correctness Assessment: Blockchain Sync Pipeline

**Project:** MoonTrack — crypto portfolio tracker with double-entry accounting
**Assessment type:** Internal correctness & data-integrity review (not a security/pentest engagement)
**Date:** 2026-07-18
**Commit / branch:** `main`
**Prepared for:** MoonTrack maintainer (`kislikjeka`)

---

## 1. Executive Summary

This assessment reviewed the blockchain synchronization pipeline that ingests decoded on-chain transactions (via the Zerion API), reconciles them against on-chain balances, and records them into the double-entry ledger. The pipeline is structurally sound — a clean three-phase design (`collect → reconcile → process`) with the data provider abstracted behind a port interface — but it contains **correctness defects that corrupt real portfolio balances and asset valuations.**

Sixteen findings are reported: **5 High, 7 Medium, 4 Low** by combined severity, plus **4 items reviewed and cleared** as not-bugs. Two findings (MT-SYNC-08 lending USD over-valuation, MT-SYNC-11 hardcoded gas asset) are **live defects affecting real data today**, independent of any planned migration.

A single root cause underlies the most severe cluster: **asset identity is keyed on `chain:symbol` rather than `chain:contract_address`.** Because token symbols are not unique (spam airdrops impersonate real tokens, wrapped/LP variants collide), net-flow accounting, position reconciliation, and genesis identity all conflate economically distinct assets. Re-keying to contract address resolves four findings at once.

### 1.1 Findings by severity

| Severity | Count | IDs |
|---|---|---|
| High | 5 | MT-SYNC-01, MT-SYNC-06, MT-SYNC-07, MT-SYNC-10, MT-SYNC-11 |
| Medium | 7 | MT-SYNC-02, MT-SYNC-03, MT-SYNC-04, MT-SYNC-08, MT-SYNC-09, MT-SYNC-12, MT-SYNC-13 |
| Low | 4 | MT-SYNC-05, MT-SYNC-14, MT-SYNC-15, MT-SYNC-16 |

### 1.2 Findings by type

| Type | Findings |
|---|---|
| Data Integrity / Identity | MT-SYNC-01, MT-SYNC-06, MT-SYNC-13 |
| Arithmetic / Precision | MT-SYNC-04, MT-SYNC-07, MT-SYNC-08, MT-SYNC-09 |
| Data Validation | MT-SYNC-02, MT-SYNC-05, MT-SYNC-10 |
| Accounting Correctness | MT-SYNC-03, MT-SYNC-11, MT-SYNC-12, MT-SYNC-14 |
| Reliability / Availability | MT-SYNC-15, MT-SYNC-16 |

---

## 2. Scope, Methodology & Limitations

### 2.1 Scope

| Area | Paths |
|---|---|
| Sync pipeline | `apps/backend/internal/platform/sync/` (collector, reconciler, processor, classifier, service) |
| Data provider | `apps/backend/internal/infra/gateway/zerion/` (client, adapter, types) |
| Ledger handlers | `apps/backend/internal/module/{transfer,genesis,lending,swap}/` |
| Money primitives | `apps/backend/pkg/money/` |
| Schema | `apps/backend/migrations/` (idempotency constraints) |

### 2.2 Methodology

Three independent reviewers audited the pipeline in parallel along orthogonal axes — reconciliation/balance logic, parser/precision, and classification/idempotency/concurrency — each instructed to produce adversarial, numbers-driven failure scenarios rather than style observations. Every reported finding was then **independently re-verified against source** by reading the cited code path end-to-end before inclusion; findings that did not survive verification were dropped or moved to §5 (Cleared). Industry reconciliation practice for transfer-derived balances (rebasing tokens, airdrops, interest accrual) was consulted to calibrate the reconciler findings.

### 2.3 Limitations

- This is a **correctness and data-integrity** review, not a security/adversarial-attacker engagement. "Likelihood" below reflects how often a defect triggers on **real production data**, not attacker effort.
- Line numbers reference `main` at the assessment date and will drift as the code changes.
- The forthcoming Zerion → Noves provider migration was **not** in scope, but findings that materially affect it are tagged **[Noves-risk]**.

### 2.4 Severity & likelihood definitions

Severity combines impact with likelihood (OpenZeppelin/Trail-of-Bits style). Impact is the damage when the defect triggers; likelihood is how often that happens on real data.

| Severity | Meaning |
|---|---|
| **High** | Corrupts real portfolio balances or asset values, or causes hard failures on common paths. |
| **Medium** | Wrong values or failures under specific but realistic conditions. |
| **Low** | Narrow, conditional, or defense-in-depth. |

| Likelihood | Meaning |
|---|---|
| **High** | Triggers on ordinary wallets / common chains without any special condition. |
| **Medium** | Requires a realistic but non-universal condition (a specific token, chain, or timing). |
| **Low** | Requires an unusual provider response or edge configuration. |

---

## 3. Findings Summary

| ID | Title | Type | Severity | Likelihood | Status |
|---|---|---|---|---|---|
| MT-SYNC-01 | Assets keyed by symbol, not contract → distinct tokens conflated | Data Integrity | High | High | Open |
| MT-SYNC-02 | `decimals == 0` fallback mis-scales value by up to 10¹⁸ | Arithmetic | High | Medium | Open |
| MT-SYNC-03 | Negative reconciliation delta silently swallowed | Accounting | Medium | Medium | Open |
| MT-SYNC-04 | Net-flow sums amounts across mismatched decimals | Arithmetic | Medium | Medium | Open |
| MT-SYNC-05 | `parseIntString` coerces unparseable amount to zero | Data Validation | High | Low | Open |
| MT-SYNC-06 | Native-asset gas netting breaks on symbol drift | Data Integrity | Medium | Medium | Open |
| MT-SYNC-07 | USD price passes through float64 (saturation & precision loss) | Precision | Medium | Medium | Open |
| MT-SYNC-08 | Lending USD value omits `/10^decimals` divisor | Arithmetic | High | High | Open |
| MT-SYNC-09 | Reconcile runs only on initial sync — drift never re-closed | Accounting | Medium | High | Open |
| MT-SYNC-10 | Global idempotency key drops second user's shared-address tx | Data Integrity | High | Medium | Open |
| MT-SYNC-11 | Gas asset hardcoded to "ETH" — non-ETH-gas chains fail | Accounting | High | High | Open |
| MT-SYNC-12 | Same-timestamp outflow ordered before funding inflow → hard fail | Accounting | Medium | Medium | Open |
| MT-SYNC-13 | Genesis idempotency key symbol-scoped & stable across re-syncs | Data Integrity | Medium | Medium | Open |
| MT-SYNC-14 | Classifier downgrades on exact-string / `Acts` dependence | Accounting | Medium | Medium | Open |
| MT-SYNC-15 | Errored raws never retried; 5-in-a-row abandons the wallet | Reliability | Medium | Medium | Open |
| MT-SYNC-16 | Transient 5xx/network errors abort the whole sync (no retry) | Reliability | Low | Medium | Open |

> Two further defense-in-depth items — dropped-tx laundered as genesis, `hasAaveAssets` false-positive, missing gas double-count guard — are folded into MT-SYNC-03/09, MT-SYNC-14, and MT-SYNC-11 recommendations respectively; see each finding's long-term note.

---

## 4. Detailed Findings

Each finding follows: **Type · Severity · Likelihood · Location → Description → Failure Scenario → Recommendation (short-term / long-term).**

---

### MT-SYNC-01 — Assets keyed by symbol, not contract, so distinct tokens are conflated

- **Type:** Data Integrity / Identity · **Severity:** High · **Likelihood:** High
- **Location:** `internal/platform/sync/reconciler.go:102,170`; `internal/module/genesis/handler.go:110`

**Description.** Net-flows and on-chain positions are keyed on `chainID + ":" + AssetSymbol`, and the synthesized genesis account code is `wallet.{id}.{chain}.{symbol}`. Token symbols are not unique: spam airdrops deliberately reuse the symbols of real tokens (`USDC`, `AAVE`, `USDT`), and wrapped/LP variants collide. All state that keys on symbol therefore merges economically distinct assets.

**Failure Scenario.** A wallet holds 1,000 real USDC (`0xa0b8…`, 6 dp) and received a 5,000 spam "USDC" (`0xdead…`, 6 dp) airdrop it still holds. `calculateNetFlows` merges both under `ethereum:USDC`; if the spam inflow was captured, netFlow = 6,000·10⁶. The real-USDC position (1,000·10⁶) yields delta = 1,000·10⁶ − 6,000·10⁶ < 0 → silently dropped (see MT-SYNC-03). A legitimate pre-history balance that genuinely needed a genesis is thereby suppressed, under-reporting the portfolio. The ledger cannot distinguish the two tokens downstream because the account code is also symbol-keyed.

**Recommendation.**
- *Short-term:* key flows, positions, and genesis identity on `(chainID, lower(contractAddress))`, with an explicit `native` sentinel for the empty-contract coin. Carry symbol as display metadata only.
- *Long-term:* introduce a single canonical `AssetIdentity` value type used everywhere identity is compared, so symbol can never again be used as a key. Resolves MT-SYNC-06 and MT-SYNC-13, and de-risks MT-SYNC-04.

---

### MT-SYNC-02 — `decimals == 0` fallback can mis-scale a token's value by 10¹⁸

- **Type:** Arithmetic / Precision · **Severity:** High · **Likelihood:** Medium
- **Location:** `internal/infra/gateway/zerion/adapter.go:141–143,186–188,258–260` → `internal/module/transfer/handler_in.go:146`

**Description.** Decimals resolution uses `if decimals == 0 { decimals = Quantity.Decimals }`. This cannot distinguish "implementation entry missing" from "implementation genuinely reports 0 decimals", and can leave `decimals = 0` for an 18-decimal token. Downstream USD value is computed as `amount * rate / 10^decimals`; with `decimals = 0` the divisor is `10⁰ = 1`.

**Failure Scenario.** 1 WETH = `1_000000000000000000` base units at rate `3500e8`. Correct USD value = `$3,500`. With `decimals = 0` the ledger records `3.5e21` scaled USD — a ~$3.5 sextillion phantom position — and the tax-lot cost basis inherits it, permanently corrupting realized-PnL math.

**Recommendation.**
- *Short-term:* resolve decimals with an explicit found signal (`impl, ok := …; if ok { … }`); treat an unresolved `0` as "unknown".
- *Long-term:* route all decimals resolution through the already-wired `money.DecimalResolver` cascade; never let an unknown scale default to a silently-wrong value — skip and flag the transfer instead.

---

### MT-SYNC-03 — Negative reconciliation delta is silently swallowed

- **Type:** Accounting Correctness · **Severity:** Medium · **Likelihood:** Medium
- **Location:** `internal/platform/sync/reconciler.go:114–123`

**Description.** When on-chain balance is *less* than the sum of transaction net-flows (negative delta), the reconciler logs a WARN and `continue`s. A negative delta is a red flag that the ledger will over-report the asset — caused by over-counted inflows, a decimals bug (MT-SYNC-02/04), or a dropped outflow. Because genesis can only add, never subtract, the over-report becomes permanent and invisible. Downstream, `DisposeFIFO` likewise only WARNs on `ErrInsufficientLots`, so cost-basis silently truncates with no hard failure.

**Failure Scenario.** Position USDC = 100·10⁶; buggy netFlow = 250·10⁶. Delta = −150·10⁶ → ignored. The ledger processes all transfers and reports 250 USDC held while the chain says 100. The only trace is a WARN log line.

**Recommendation.**
- *Short-term:* beyond a dust tolerance, mark the wallet sync degraded (`SetSyncError`) on a negative delta rather than swallowing it.
- *Long-term:* emit a first-class reconciliation-discrepancy record surfaced to the user; distinguish genesis-from-partial-history from genesis-from-missing-flow (the "dropped-tx laundered as genesis" concern) so data loss isn't disguised as a clean balance.

---

### MT-SYNC-04 — Net-flow sums amounts across mismatched decimals

- **Type:** Arithmetic / Precision · **Severity:** Medium · **Likelihood:** Medium
- **Location:** `internal/platform/sync/reconciler.go:184–188`; delta at `:112`

**Description.** `AssetFlow.Decimals` is set once from the first transfer seen and never re-checked against later transfers or against the position's decimals. Amounts are summed as raw base-unit integers, and the delta subtracts `pos.Quantity` (at `pos.Decimals`) from that sum — with no scale reconciliation.

**Failure Scenario.** The transactions feed resolves a token at 18 decimals but the positions feed resolves it at 6 (a realistic outcome of the MT-SYNC-02 fallback). netFlow = 1·10¹⁸, position = 1·10⁶ → delta = 1·10⁶ − 1·10¹⁸, an astronomically negative number → dropped (MT-SYNC-03), or the reverse direction synthesizes a ~10¹² phantom genesis.

**Recommendation.**
- *Short-term:* assert `flow.Decimals == pos.Decimals` before computing delta; on mismatch, treat as a hard reconciliation error and do not synthesize.
- *Long-term:* store amounts normalized to a canonical per-asset scale so cross-source arithmetic can never mix scales.

---

### MT-SYNC-05 — `parseIntString` coerces an unparseable amount to zero

- **Type:** Data Validation · **Severity:** High · **Likelihood:** Low
- **Location:** `internal/infra/gateway/zerion/adapter.go:208–217` (callers `:114,:171,:263`)

**Description.** On empty or non-decimal input, `parseIntString` returns `big.NewInt(0)` with no error. A money field that is malformed (scientific notation, hex, or a field renamed in an API version bump) becomes a zero-amount transfer.

**Failure Scenario.** A single-asset transfer with a malformed `Quantity.Int` parses to 0 → `Amount.Sign() <= 0` triggers `ErrInvalidAmount` (`transfer/model.go:33`) → the **entire transaction fails** → its balance mutation is never recorded → reconciliation later observes the gap and fabricates a phantom genesis with wrong cost basis. In LP multi-transfer aggregation the bad leg silently adds 0, understating the position with no error at all.

**Recommendation.**
- *Short-term:* change `parseIntString` to return `(*big.Int, error)` and propagate; a parse failure on a money field fails `convertTransaction` (logged, retried) rather than coercing to 0.
- *Long-term:* reserve empty→0 only for genuinely-optional quantities, made explicit at the call site.

---

### MT-SYNC-06 — Native-asset gas netting breaks on symbol drift between feeds

- **Type:** Data Integrity / Identity · **Severity:** Medium · **Likelihood:** Medium
- **Location:** `internal/platform/sync/reconciler.go:193` (fee) vs `:170` (transfers) vs `:102` (positions)

**Description.** The native coin has an empty contract address, so all three feeds key it by symbol string, each sourced independently (fee `FungibleInfo.Symbol`, transfer symbol, position symbol). Any inconsistency in how the native asset is named across endpoints splits its flow from its position.

**Failure Scenario.** Positions report `"Ether"` while transfers/fees report `"ETH"`. The ETH flow lands under `ethereum:ETH`; the position under `ethereum:Ether` with `exists == false` → netFlow treated as 0 → a bogus full-balance genesis synthesized on top of real transaction history, double-counting the entire native balance.

**Recommendation.**
- *Short-term / long-term:* fold into MT-SYNC-01 — derive a `(chain, native)` identity from the empty contract address, and assert the fee asset resolves to the same native identity as the transfers.

---

### MT-SYNC-07 — USD price passes through float64, causing silent saturation and precision loss

- **Type:** Arithmetic / Precision · **Severity:** Medium · **Likelihood:** Medium
- **Location:** `internal/infra/gateway/zerion/adapter.go:222–225` (`usdFloatToBigInt`)

**Description.** `big.NewInt(int64(math.Round(price * 1e8)))` converts a USD price through `float64` → `int64`. Above ~$9.2·10¹⁰ per unit the conversion saturates to `MaxInt64` (a garbage-but-plausible positive rate), and above ~$9·10⁷ it loses cents-level precision to float64 mantissa limits. This violates the project's stated "never float64 for money" principle at the money boundary.

**Failure Scenario.** An illiquid or mispriced token reports a unit price of `1e12`; the recorded rate saturates to `9223372036854775807` instead of the true scaled value — a wrong, positive, plausible rate that flows into position valuation.

**Recommendation.**
- *Short-term:* range-guard the conversion and reject-with-log instead of silently saturating.
- *Long-term:* parse price from Zerion's string numeric form via `big.Float`/decimal and scale by 1e8 using `big.Int`, keeping money off `float64` end-to-end.

---

### MT-SYNC-08 — Lending USD value omits the `/10^decimals` divisor

- **Type:** Arithmetic / Precision · **Severity:** High · **Likelihood:** High
- **Location:** `internal/platform/sync/zerion_processor.go:962–967` (`calcLendingUSD`), called from `:863,884,905,926,947`

**Description.** `calcLendingUSD` returns raw `new(big.Int).Mul(t.Amount, t.USDPrice)` with no decimals divisor, whereas the same file computes LP USD value correctly via `money.CalcUSDValue(t.Amount, t.USDPrice, t.Decimals)` at line 385. Every lending USD value (supply, withdraw, borrow, repay, claim) is therefore overstated by `10^decimals`.

**Failure Scenario.** A 1,000 USDC (6 dp) Aave supply records `1000·10⁶ × rate` without dividing by `10⁶` — a value inflated by 1,000,000×. A WETH (18 dp) position is inflated by 10¹⁸×. Affects Aave and non-Aave (Fluid) lending alike.

**Recommendation.**
- *Short-term:* `return money.CalcUSDValue(t.Amount, t.USDPrice, t.Decimals)` — a one-line change.
- *Long-term:* forbid ad-hoc `amount × price` multiplication in the codebase; all USD valuation must go through the single `money.CalcUSDValue` helper.

---

### MT-SYNC-09 — Reconciliation runs only on the initial sync, so balance drift is never re-closed

- **Type:** Accounting Correctness · **Severity:** Medium · **Likelihood:** High
- **Location:** `internal/platform/sync/service.go:222–269` (reconcile is inside the `if isInitial` branch only)

**Description.** Reconciliation is one-shot. Any balance change that arrives *without* a decoded transaction — rebasing tokens (stETH, AMPL), un-indexed airdrops, interest accrual, validator rewards — opens a gap that is never reconciled again after day 0. Transfer-derived balances are known to diverge from on-chain balances for exactly these asset classes.

**Failure Scenario.** Day 0: stETH balance 10.0 reconciles exactly. Over a month stETH rebases to 10.4 with zero transfer events. Every incremental sync collects no stETH transaction, reconciles nothing, and under-reports the position by 0.4 indefinitely.

**Recommendation.**
- *Short-term:* run reconciliation (or a lightweight balance-drift check) on incremental syncs, not just the initial one.
- *Long-term:* add a correction primitive that can express a *decrease* (the current genesis can only add), so downward rebases and un-indexed outflows can also be reconciled.

---

### MT-SYNC-10 — Global idempotency key drops the second user's copy of a shared-address transaction

- **Type:** Data Integrity · **Severity:** High · **Likelihood:** Medium
- **Location:** `apps/backend/migrations/000001_create_schema.up.sql:57`; `internal/platform/sync/zerion_processor.go:87,125`

**Description.** The ledger `transactions` table enforces `UNIQUE(source, external_id)` — global, with no `wallet_id` or `user_id` component (verified across all 24 migrations). Two users who each register a wallet at the same on-chain address both produce `source="zerion"` and the same Zerion transaction id.

**Failure Scenario.** User A syncs first and records the tx. User B's identical INSERT returns Postgres `23505`, which the sync layer interprets as "already recorded → idempotent skip". User B's ledger silently never receives the transaction; their balances are wrong with no error surfaced. The genesis path (`source="sync_genesis"`) has the same defect.

**Recommendation.**
- *Short-term:* scope the constraint to the owner: `UNIQUE(wallet_id, source, external_id)` (schema migration plus the insert/idempotency path).
- *Long-term:* treat a `23505` as idempotent only when it matches the *same wallet's* prior write, not any global row.

---

### MT-SYNC-11 — Gas asset hardcoded to "ETH"; every non-ETH-gas-chain transfer-out fails

- **Type:** Accounting Correctness · **Severity:** High · **Likelihood:** High
- **Location:** `internal/module/transfer/handler_out.go:129` (`nativeAssetID := "ETH"`)

**Description.** Transfer-out gas is booked to `wallet.{id}.{chain}.ETH` and `gas.{chain}.ETH` regardless of chain (the author's own comment reads `// should come from chain ID`). The swap and lending handlers already use `txn.FeeAsset` correctly, so a template for the fix exists in-repo.

**Failure Scenario.** On BNB Chain / Polygon / Avalanche the wallet holds no ETH, so the gas entry drives a negative ETH balance → `NegativeBalanceError` (asset-decrease is balance-checked at `ledger/service.go:486`) → **every transfer-out carrying gas fails** on those chains, which then compounds with MT-SYNC-15 (no retry) to abandon the wallet.

**Recommendation.**
- *Short-term:* use the fee asset already carried in `data["gas_*"]` (mapped from `fee_asset` at `zerion_processor.go:490`) instead of the literal `"ETH"`; apply the same to `internal_transfer`.
- *Long-term:* add a gas double-count guard (the "missing guard" concern) — assert/dedup when the fee asset+amount also appears as an out-transfer, required before the Noves adapter (which emits a `paidGas` transfer) lands. **[Noves-risk]**

---

### MT-SYNC-12 — Same-timestamp outflow can be ordered before its funding inflow, causing a hard failure

- **Type:** Accounting Correctness · **Severity:** Medium · **Likelihood:** Medium
- **Location:** `internal/platform/sync/processor.go:63–68`; `service.go:281–294`; `ledger/service.go:486`

**Description.** Transactions are sorted by `mined_at` then `operationPriority`. Within an identical `mined_at`, correctness depends entirely on `operationPriority`, and wallet `AssetDecrease`, `CollateralDecrease`, and `LiabilityDecrease` are hard-checked for negative balance.

**Failure Scenario.** A repay classified as `OpExecute` (priority 2) or a withdraw (`OpWithdraw`, priority 1) can be processed before the supply/borrow that created its collateral/liability, in the same block → the balance goes negative → `NegativeBalanceError` → the raw is marked `error` (see MT-SYNC-15) and, after 5 consecutive such errors, the whole wallet aborts.

**Recommendation.**
- *Short-term:* tie-break the sort so that, per asset, inflows sort strictly before outflows.
- *Long-term:* act on the fields `NegativeBalanceError` already carries (per `errors.go`, intended "for callers to create genesis balances") to auto-synthesize a funding genesis rather than hard-failing.

---

### MT-SYNC-13 — Genesis idempotency key is symbol-scoped and stable across re-syncs

- **Type:** Data Integrity · **Severity:** Medium · **Likelihood:** Medium
- **Location:** `internal/platform/sync/reconciler.go:240`; `processor.go:199`

**Description.** The genesis external id is `genesis:{wallet}:{chain}:{symbol}` — no amount, no contract, no run discriminator. Reconcile deletes and recreates synthetic *raw* rows, but the *ledger* genesis transaction from a prior run persists under the same external id.

**Failure Scenario.** First sync creates a genesis for 100 USDC. A later re-sync computes the corrected delta as 40 and creates a new genesis raw; on processing it collides with the stale ledger genesis on `UNIQUE(source, external_id)`, is marked "duplicate → skipped", and the wrong 100-unit genesis stands. Two symbol-sharing tokens (MT-SYNC-01) likewise collide, so only the first ever receives a genesis.

**Recommendation.**
- *Short-term:* include the contract address (or a content hash of chain+contract+amount) in the genesis external id.
- *Long-term:* on replay, supersede the prior genesis *ledger* transaction, not only the raw row, so corrected deltas replace stale ones.

---

### MT-SYNC-14 — Classifier silently downgrades on exact-string and `Acts` dependence

- **Type:** Accounting Correctness · **Severity:** Medium · **Likelihood:** Medium · **[Noves-risk]**
- **Location:** `internal/platform/sync/classifier.go:64,75,84,123,130–146,157`

**Description.** LP classification requires `protocol == "Uniswap V3"` exactly; claim detection requires the `Acts` array to contain `"claim"`; lending `OpReceive` with an empty `Acts` array defaults to `Borrow`. Separately, `hasAaveAssets` matches any symbol where `symbol[0]=='a'` and `symbol[1]` is uppercase, running on all transfers even when protocol is empty.

**Failure Scenario.** (a) An untagged LP fee claim (`OpReceive`, empty `Acts`) falls through to `transfer_in` → booked as income with a spurious tax lot, inflating realized PnL. (b) An aToken interest receipt with empty `Acts` defaults to `LendingBorrow` → booked as a liability instead of income. (c) A memecoin or scam token like `aXYZ` is routed into `classifyLending`, fabricating an Aave position (protocol defaulted to `"AAVE"` at `zerion_processor.go:955`). Under Noves — which has no `Acts` concept and emits different protocol strings — all DeFi typing collapses to generic swap/transfer unless the adapter synthesizes `Acts` and normalizes protocol names.

**Recommendation.**
- *Short-term:* broaden protocol matching (case-insensitive contains); gate the aToken symbol heuristic on a non-empty protocol or a known-Aave counterpart transfer.
- *Long-term:* classify from operation-type + asset-role rather than the `Acts` array, and stop defaulting lending `OpReceive` to `Borrow`. This is a prerequisite for the Noves migration.

---

### MT-SYNC-15 — Errored raw transactions are never retried; five in a row abandons the wallet

- **Type:** Reliability / Availability · **Severity:** Medium · **Likelihood:** Medium
- **Location:** `internal/platform/sync/processor.go:110–143`

**Description.** Each transaction is its own DB transaction, with no batch atomicity. A non-duplicate failure marks the raw `error` (not `pending`); the next run's `GetPendingByWallet` never returns `error` rows, so the transaction is never retried. Collector re-collection uses `ON CONFLICT DO NOTHING`, so the errored raw is never refreshed either. After 5 consecutive errors the processor breaks, leaving the remainder pending.

**Failure Scenario.** A transient MT-SYNC-12 ordering failure that would succeed once its predecessor lands is instead stuck permanently in `error`; everything after the 5-error abort stays pending until a future run that will hit the same wall.

**Recommendation.**
- *Short-term:* include `error` rows in the requeue set on subsequent runs.
- *Long-term:* don't hard-error on recoverable conditions like `NegativeBalanceError` — defer them ("needs predecessor / genesis") and resume cleanly.

---

### MT-SYNC-16 — Transient 5xx / network errors abort the whole sync with no retry

- **Type:** Reliability / Availability · **Severity:** Low · **Likelihood:** Medium
- **Location:** `internal/infra/gateway/zerion/client.go:96–99,116–135,105`

**Description.** The HTTP client retries only on `429`. A `502` or a dropped connection on page *n* of a paginated fetch discards all accumulated pages and fails the fetch. This is fail-closed (the cursor advances only after a fully successful fetch — verified, so no corruption), but a transient upstream blip fails the entire wallet sync. The 16 MiB `LimitReader` truncation shares the fail-closed shape but can wedge a sync permanently on a legitimately-large first page.

**Failure Scenario.** Page 1 of 5 succeeds; page 2 returns a transient `502`. `GetTransactions` returns an error, all fetched pages are discarded, and the wallet sync fails until the next poll cycle.

**Recommendation.**
- *Short-term:* retry on `status >= 500` and on network errors using the existing exponential backoff.
- *Long-term:* detect 16 MiB truncation explicitly (read one byte past the limit → distinct "response too large" error) and/or request a smaller page size.

---

## 5. Reviewed and Cleared (Not Findings)

These were investigated adversarially and confirmed correct:

| Item | Why it is not a bug |
|---|---|
| **Genesis timestamp ordering** | `earliest − 1s` sorts first; FIFO orders lots by `acquired_at ASC`, so the genesis lot is always consumed first. No transient negative within a run. |
| **Swap balancing via clearing account** | Per-asset clearing holds for multi-asset (2+2), 3+ transfers, and mixed assets; clearing accounts are exempt from the negative-balance check so swaps never fail on clearing. One-sided "swaps" are rejected by validation, not mis-booked. |
| **Internal-transfer dedup** | Same-address-different-chain (bridge) is guarded; the incoming side of a genuine internal transfer is skipped to avoid a double count. |
| **Array-vs-flat double count** | Handlers read the `transfers` array XOR the flat legacy fields via `collectItems` — no overlap. Cursor is fail-closed: `last_sync_at` advances only after a fully successful fetch. |

---

## 6. Remediation Plan

Ordered by value-to-effort. Each item maps to a GitHub issue. **Ready** = root cause and fix are known and localized (safe to implement directly). **Needs design** = the fix requires a design decision and should be grilled / recorded as an ADR first.

| # | Findings | Rationale | Track |
|---|---|---|---|
| 1 | MT-SYNC-08 | One-line divisor fix; corrects every lending position value today. | Ready |
| 2 | MT-SYNC-11 | Actively fails all non-ETH-gas chains; in-repo template exists. | Ready |
| 3 | MT-SYNC-01, MT-SYNC-06, MT-SYNC-13 | One structural re-key (symbol → contract) collapses the worst cluster. | Needs design |
| 4 | MT-SYNC-02, MT-SYNC-05 | Stop the 10¹⁸ mis-scale and the zero-coercion that fails whole txs. | Mixed (02 ready; 05 needs design — signature change) |
| 5 | MT-SYNC-10 | Scope the uniqueness constraint; unblocks multi-user same-address wallets. | Needs design (migration) |
| 6 | MT-SYNC-12, MT-SYNC-15 | Ordering + retry compound; fix together so deferrals resume cleanly. | Needs design |
| 7 | MT-SYNC-03, MT-SYNC-04, MT-SYNC-07, MT-SYNC-09 | Turn silent drift into first-class discrepancies; add incremental reconcile. | Mixed (09 needs design; rest ready) |
| 8 | MT-SYNC-14, MT-SYNC-16 | Pre-Noves classifier hardening + retry robustness. | Needs design |

---

## Appendix A — Finding ID scheme

IDs follow `MT-SYNC-NN` (**T**racking **O**f **B**ugs — **M**oon**T**rack), assigned sequentially and stable for cross-reference from GitHub issues and commit messages. The prefix is a local convention, not affiliation with any external auditor.
