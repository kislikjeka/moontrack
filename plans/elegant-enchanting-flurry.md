# Plan — Issue #25: Noves adapter (raw JSON → DecodedTransaction, adapter seam)

## Context

MoonTrack is migrating blockchain sync from Zerion to the **Noves Translate API** (parent
issue #23, ADR-0002, CONTEXT.md). Issue #25 delivers **only the adapter seam**: a new
`internal/infra/gateway/noves/` package (`client.go`, `adapter.go`, `types.go` + tests) that
converts Noves raw JSON into the existing `sync.DecodedTransaction` contract. It is **not**
wired into DI — this ticket is the tested conversion boundary only. Bridge *stitching*,
per-chain cursor, collector fan-out, and DI wiring are separate downstream tickets.

The value: Noves classifies each tx into a rich taxonomy (lending with real asset + receipt
token, LP, swaps, bridge legs) instead of Zerion's op-type collapse, and the adapter must map
that faithfully so the existing `classifier.go` / ledger handlers stay untouched.

### Empirical grounding (real Noves data captured this session)

I captured real Noves v2 JSON for the pilot Base wallet (12-request budget, scratchpad
`noves_capture.go`, key+address read from `.env`, never printed). Confirmed against real data:

- **v2 shape**: top-level `{txTypeVersion, chain, accountAddress, classificationData, rawTransactionData}`.
  `classificationData = {type, source{type}, description, protocol{name}, sent[], received[]}`.
  Each transfer = `{action, from{name,address}, to{name,address}, amount, token{symbol,name,decimals,address}}`,
  with an `nft{name,id,symbol,address}` sub-object on NFT entries (`id` can exceed int64 → `json.Number`).
- **Amounts are decimal strings in HUMAN units** (e.g. `"120.559701"`, `"0.00199564"`) — must be
  converted ×10^decimals to base units.
- **Native token** uses a symbol-as-address sentinel (`token.address == "ETH"`, `decimals:18`) — not hex.
- `protocol.name` is **null** on every real tx → derive protocol from `to.name`/`from.name`/`nft.name`.
- `paidGas` transfers present in sent[] (also in `rawTransactionData.transactionFee`) → must filter.
- **Lending** (`depositCollateral`): real asset (`cbBTC` sent) + receipt token (`aBascbBTC` received) →
  existing aToken heuristic fires.
- **Bridge** (`receiveFromBridge`): `received[]` `action=bridged`, `to.name="This wallet"`, `to.address`=own wallet.
  **Bridge round-trip** (`sendToBridge`): sends a GMX LP token to null address AND `received[]` ETH back to
  "This wallet" in a *different* asset — the exact bridge-as-swap case from ADR-0002.
- `rawTransactionData.transactionFee.amount` is a decimal string; extra fields (`l1Gas`, `gasUsed`, …) ignored.
- `sort=asc` works natively; pagination is cursor-by-`nextPageUrl` with `hasNextPage`.

Real captures are saved and will become `testdata/*.json` fixtures. The user additionally
supplied **real** Uniswap V3 `addLiquidity`/`removeLiquidity` JSON, so the LP-with-`nft` case is
real too. Confirmed extra facts from it: **`nft.id` is a JSON string** (`"5325584"`) — `json.Number`
decodes both string and number forms; the LP-NFT entry has `nft` and **no `token`** (token-vs-nft
split); `source.type` can be `"inference"` (not just `"human"`); protocol must be derived from
`to.name`/`nft.name` = `"Uniswap V3 Positions NFT-V1"` → extract `"Uniswap V3"`; benign actions like
`refundedByContract`/`lpTokensMinted`/`liquidityAdded` are NOT filtered (only `paidGas` is).
Only `unclassified` (both-direction) and a `bridge_send_pure` leg remain hand-authored (the latter
trivially derived from the real round-trip leg minus its `received[]`).

## Deliverables (mirror `internal/infra/gateway/zerion/`)

### `types.go` — Noves v2 wire structs
- `TransactionsResponse{ Items []Transaction; PageSize int; HasNextPage bool; NextPageURL string }`
- `Transaction{ TxTypeVersion int; Chain, AccountAddress string; ClassificationData; RawTransactionData }`
- `ClassificationData{ Type string; Source struct{Type string}; Description string; Protocol struct{Name *string}; Sent, Received []Transfer }`
- `Transfer{ Action string; From, To Party; Amount string; Token *Token; NFT *NFT }`
- `Party{ Name, Address *string }` (both nullable)
- `Token{ Symbol, Name, Address string; Decimals int }`
- `NFT{ Name, Symbol, Address string; ID json.Number }` (id can exceed int64)
- `RawTransactionData{ TransactionHash, FromAddress, ToAddress string; BlockNumber int64; Timestamp int64; TransactionFee Fee }`
- `Fee{ Amount json.Number; Token *Token }`
- `RateLimitError` + `IsRateLimitError` (mirror Zerion) if the client needs it.

### `client.go` — HTTP client (mirror Zerion `client.go`)
- Base URL `https://translate.noves.fi`, `SetBaseURL` for tests.
- Auth: header `apiKey: <key>` (NOT Basic), `accept: application/json`.
- `doRequest`: same retry/backoff shape as Zerion (429 + 5xx + network transient, 4xx immediate),
  `maxResponseBytes` bound, context-aware backoff. **Reuse the exact retry structure** — it's already
  hardened (MT-SYNC-16) and tested.
- `GetTransactions(ctx, chain, address, since)`: `GET /evm/{chain}/txs/{addr}?pageSize=…&sort=asc`,
  follow `nextPageUrl` until `!hasNextPage`. `since` → `startTimestamp` (ms) when non-zero. Oldest-first
  (`sort=asc`) per CONTEXT.md cursor model. (Note: the collector owns the real cursor; the client just
  paginates a page window — keep it simple, mirror Zerion's loop.)
- **Scope (confirmed)**: Noves adapter implements **only `sync.TransactionDataProvider`**. No
  `GetPositions` / balances endpoint in #25 — that is a separate reconciler ticket. Compile-time
  assertion is `var _ sync.TransactionDataProvider = (*noves.SyncAdapter)(nil)` only.

### `adapter.go` — `SyncAdapter` implementing `sync.TransactionDataProvider`
Core conversion `convertTransaction(Transaction) (sync.DecodedTransaction, error)`:
- **Chain slug mapping** `novesToDomainChain` / `domainToNovesChain`: domain uses Zerion-style slugs
  (`ethereum`, `binance-smart-chain`, `base`, `arbitrum`, …); Noves uses short slugs (`eth`, `bsc`, …).
  The port receives a **domain** chain slug → map to Noves slug for the endpoint; emit `ChainID` back in
  **domain** slug (canonical). Only `base/arbitrum/polygon/optimism/avalanche` map 1:1; `ethereum↔eth`,
  `binance-smart-chain↔bsc`.
- **Operation type**: pass `classificationData.type` straight through as `sync.OperationType(type)` —
  BUT the classifier switches on `sync.OpTrade/OpDeposit/...` (Zerion vocabulary), not Noves' 70-type
  enum. **Map Noves types → the existing `OperationType` vocabulary** (`swap→trade`,
  `depositCollateral/addLiquidity→deposit`, `removeLiquidity/withdrawCollateral→withdraw`,
  `claimRewards→claim`, `receiveToken*/receiveFromBridge→receive`, `sendToken/sendToBridge→send`,
  `approveToken→approve`, `unclassified→execute`, else→execute). This keeps `classifier.go` untouched
  (its contract is the `OperationType` enum, not raw provider strings).
- **Transfers**: iterate `sent[]` (→ `DirectionOut`) and `received[]` (→ `DirectionIn`).
  - **Filter `action=="paidGas"`** (gas double-count; it's in the fee).
  - **Token vs NFT split**: entries with a non-nil `nft` and nil/zero `token` are NFT transfers →
    do NOT emit as a fungible `DecodedTransfer` (mirrors Zerion skipping NFT transfers); instead capture
    `nft.id` into `DecodedTransaction.NFTTokenID`. Entries with a `token` are fungible transfers.
  - **Amount conversion**: `money.ToBaseUnits(amount, decimals)` (existing, exact string-based) →
    base units. **Exact-or-flag**: if the decimal has more fractional digits than `decimals`,
    `ToBaseUnits` truncates — detect that (count fractional digits > decimals) and set
    `NeedsReview=true` + `ReviewReason` + WARN log, never silently floor. Add these two fields to the
    contract (see below).
  - **Native token**: `token.address` non-hex sentinel (== symbol) → emit `ContractAddress=""`
    (contract says empty for native). Detect: address has no `0x` prefix / equals symbol.
  - `Decimals<=0` receipt tokens (e.g. dec=0/empty symbol) must not break — emit as-is; the amount
    conversion with decimals=0 is identity.
- **Fee**: `rawTransactionData.transactionFee` → `DecodedFee` (amount decimal→base units, native token).
- **Protocol derivation**: `deriveProtocol(tx)` — `protocol.name` is null, so scan `to.name`/`from.name`/
  `nft.name` hints for known protocol markers feeding `isUniswapV3` (`"Uniswap V3"`) and `isAAVE`
  (`"Aave …"`). Return the best hint string so `classifier.isUniswapV3`/`isAAVE` still match. The aToken
  *symbol* heuristic (`hasAaveAssets`) already fires on `aBascbBTC` without protocol, so derivation is a
  best-effort augmentation, not the sole path.
- **Acts**: collect distinct `action` values (minus `paidGas`) into `Acts` (the classifier reads
  `hasClaimAct` → needs `"claim"`; Noves uses `claimRewards`/`bridged`/`bought` — map claim-ish actions
  so `LPClaimFees`/`LendingClaim` still classify). Concretely: include the Noves `classificationData.type`
  and per-transfer actions; add `"claim"` to Acts when type is `claimRewards`.
- **External ID**: `ID = chain:txHash` lowercased (CONTEXT.md `external_id`). `TxHash = transactionHash`.
- **MinedAt**: `time.Unix(rawTransactionData.timestamp, 0).UTC()`.
- **Status**: `type=="failed"` → `"failed"`, else `"confirmed"`.

### Contract change (additive, backward-compatible) — `internal/platform/sync/port.go`
Add to `DecodedTransaction`:
```go
NeedsReview  bool   // adapter could not convert exactly (precision loss); route to review
ReviewReason string // human-readable reason, empty when NeedsReview is false
```
Zero-valued for every existing producer (Zerion adapter, test fixtures) → no behavior change.
Consumed later by the processor/port-seam ticket. This is the minimal-blast-radius way to satisfy
acceptance criterion "flag the transaction rather than silently floor".

## Tests (adapter seam — assert on `DecodedTransaction` output from raw JSON)

`testdata/` fixtures (real unless noted):
- `swap.json` (real) — trade classification, paidGas filtered, native fee, USDC/cbBTC.
- `lending_supply.json` (real depositCollateral) — real asset + receipt aToken, deposit op.
- `transfer_in.json` (real receiveToken) — receive → in-only.
- `lp_remove_univ2.json` (real docs Polygon removeLiquidity) — LP token + two received.
- `bridge_receive.json` (real receiveFromBridge) — received bridged to "This wallet".
- `bridge_send_roundtrip.json` (real sendToBridge) — LP-out + different-asset-in (bridge-as-swap).
- `bridge_send_pure.json` (synthetic — round-trip minus its received[]) — pure-send leg.
- `lp_add_nft.json` (**real**, user-provided Uniswap V3 addLiquidity) — token-vs-nft split, NFTTokenID=5325584,
  protocol derivation from nft/to name, `source.type=inference`, `refundedByContract` dust received.
- `lp_remove.json` (**real**, user-provided Uniswap V3 removeLiquidity) — one-sided received, paidGas-only sent.
- `unclassified_both.json` (synthetic) — both-direction unclassified → execute/swap fallback + flag.
- `precision_loss.json` (synthetic — amount with more digits than decimals) — exact-or-flag.
- `failed.json` (real) — status=failed.

`client_test.go` (mirror Zerion): auth header (`apiKey`), accept header, pagination via
`nextPageUrl`, `sort=asc` + `startTimestamp` params, 429 retry/exhaustion, 5xx retry, network retry,
context cancel, oversized-response bound, 4xx immediate.

`adapter_test.go`: interface compliance; full conversion per fixture asserting exact
`DecodedTransaction` (op type, transfers, directions, amounts in base units, ContractAddress lowercased/
empty-for-native, NFTTokenID, protocol, Acts, fee, ExternalID=chain:txHash, MinedAt, status);
paidGas filtered; token-vs-nft split; protocol derivation; native-sentinel → empty contract;
decimals<=0 receipt token doesn't panic; chain-slug mapping (ethereum→eth roundtrip).

`adapter_internal_test.go`: `novesToDomainChain`/`domainToNovesChain` tables; `ToBaseUnits`
exact-or-flag detection helper; native-token detection.

`lending_usd_test.go` regression is out of scope (that's the platform `sync` bug fix in a different
ticket) — NOT touched here.

## Files

- **New**: `internal/infra/gateway/noves/{types.go, client.go, adapter.go, client_test.go, adapter_test.go, adapter_internal_test.go}`
- **New**: `internal/infra/gateway/noves/testdata/*.json`
- **Edit**: `internal/platform/sync/port.go` — add `NeedsReview` + `ReviewReason` to `DecodedTransaction`.
- **New (docs, per user)**: `docs/agents/noves-api-capture.md` — rules for working with the Noves API key
  and capture-budget limits (key from `.env`, never printed; ≤12 requests/session; testdata reuse via
  `jq`, no re-querying). Referenced from CLAUDE.md. (User asked to document this in docs, not memory.)

## Decisions (confirmed with user)
1. Noves adapter implements **only `TransactionDataProvider`**; `GetPositions`/balances is a later ticket.
2. Capture rules go in **`docs/agents/noves-api-capture.md`** + a one-line pointer in `CLAUDE.md`
   (Agent skills section).

## Verification
- `cd apps/backend && go build ./...` — no compile errors (CLAUDE.md hard rule).
- `go test ./internal/infra/gateway/noves/... -v -short` — adapter-seam tests pass.
- `go test ./internal/platform/sync/... -short` — contract change breaks nothing (Zerion adapter + sync
  still compile/pass with the new zero-valued fields).
- `just backend-test` (or `go test ./... -short`) — full suite green once at the end.
- `/code-review` on the branch, then commit to a feature branch (currently on `main`).
