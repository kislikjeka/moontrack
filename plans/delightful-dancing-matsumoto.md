# Phase 2: Zerion Client & Adapter — Implementation Plan

## Context
Phase 2 of the MoonTrack implementation builds the Zerion HTTP client, API types, and domain adapter to fetch decoded blockchain transactions from Zerion's REST API and convert them into domain types. This enables future sync pipeline integration (Phase 3) without modifying the existing Alchemy-based sync flow. Phase 1 (foundation) is already complete, including `ZerionAPIKey` in config.

## Files Overview

| File | Action | Description |
|------|--------|-------------|
| `apps/backend/internal/platform/sync/port.go` | MODIFY | Add OperationType, DecodedTransaction, DecodedTransfer, DecodedFee, TransactionDataProvider |
| `apps/backend/internal/infra/gateway/zerion/types.go` | CREATE | Zerion API response types + chain ID maps |
| `apps/backend/internal/infra/gateway/zerion/client.go` | CREATE | HTTP client with Basic auth, pagination, rate limit retry |
| `apps/backend/internal/infra/gateway/zerion/adapter.go` | CREATE | Converts Zerion types → domain types, implements sync.TransactionDataProvider |
| `apps/backend/internal/infra/gateway/zerion/types_test.go` | CREATE | Chain mapping tests |
| `apps/backend/internal/infra/gateway/zerion/client_test.go` | CREATE | httptest-based client tests |
| `apps/backend/internal/infra/gateway/zerion/adapter_test.go` | CREATE | Conversion + nil-safety tests |
| `apps/backend/internal/platform/sync/port_test.go` | CREATE | Compile-check for new domain types |

## Execution Steps

### Step 1 (Parallel — 2 subagents)

**Subagent A: Domain types in `sync/port.go`**
- Add `OperationType` string type + 10 constants (OpTrade, OpDeposit, OpWithdraw, OpClaim, OpReceive, OpSend, OpExecute, OpApprove, OpMint, OpBurn) after line 28
- Add `DecodedTransaction` struct (ID, TxHash, ChainID, OperationType, Protocol, Transfers, Fee, MinedAt, Status)
- Add `DecodedTransfer` struct (AssetSymbol, ContractAddress, Decimals, Amount *big.Int, Direction, Sender, Recipient, USDPrice *big.Int)
- Add `DecodedFee` struct (AssetSymbol, Amount *big.Int, Decimals, USDPrice *big.Int)
- Add `TransactionDataProvider` interface with `GetTransactions(ctx, address string, chainID int64, since time.Time) ([]DecodedTransaction, error)`
- No new imports needed (context, math/big, time already imported)
- Also create `port_test.go` with compile-check tests

**Subagent B: Zerion API types in `zerion/types.go`**
- Create `apps/backend/internal/infra/gateway/zerion/types.go`
- All Zerion response types: TransactionResponse, Links, TransactionData, TransactionAttributes, Fee, ZTransfer, FungibleInfo, IconInfo, Implementation, Quantity, Approval, ApplicationMeta
- JSON tags matching Zerion API exactly
- Chain ID maps: `ZerionChainToID` (7 chains), `IDToZerionChain` (reverse), `ErrUnsupportedChain`
- Also create `types_test.go` with mapping consistency tests

**Verify**: `go build ./internal/platform/sync/... ./internal/infra/gateway/zerion/...`

### Step 2: Zerion HTTP Client (`zerion/client.go`)

Create following the Alchemy client pattern (`alchemy/client.go`):
- `Client` struct: `apiKey`, `httpClient` (30s timeout), `baseURL` (default: `https://api.zerion.io/v1`)
- `NewClient(apiKey string) *Client` constructor
- `doRequest(ctx, method, url string, params url.Values) ([]byte, error)`:
  - Auth: `Authorization: Basic {base64(apiKey + ":")}`
  - Accept: `application/json`
  - Exponential backoff for 429: 1s → 2s → 4s, max 3 retries, context-aware sleep
  - Returns `RateLimitError` after exhaustion
  - Descriptive errors for non-200/429
- `GetTransactions(ctx, address, chainID string, since time.Time) ([]TransactionData, error)`:
  - URL: `{baseURL}/wallets/{address}/transactions/`
  - Params: `filter[chain_ids]`, `filter[min_mined_at]` (RFC3339), `filter[asset_types]=fungible`, `filter[trash]=only_non_trash`
  - Pagination: follow absolute `Links.Next` URL until empty
- `RateLimitError` + `IsRateLimitError()` matching Alchemy pattern
- Also create `client_test.go` with httptest-based tests (auth header, pagination, rate limit retry/exhaustion, query params, error responses)

**Verify**: `go build ./internal/infra/gateway/zerion/...`

### Step 3: Zerion Adapter (`zerion/adapter.go`)

Create following the Alchemy adapter pattern (`alchemy/adapter.go`):
- `SyncAdapter` struct wrapping `*Client`
- `var _ sync.TransactionDataProvider = (*SyncAdapter)(nil)` compile-time check
- `NewSyncAdapter(client *Client) *SyncAdapter`
- `GetTransactions(ctx, address string, chainID int64, since time.Time) ([]sync.DecodedTransaction, error)`:
  - Convert chainID → Zerion chain string via `IDToZerionChain`, return `ErrUnsupportedChain` if missing
  - Call client, convert each result, skip failures
- Private helpers:
  - `convertTransaction(td, chainID, zerionChain)` — map fields, nil-safe ApplicationMetadata
  - `convertTransfer(zt, zerionChain)` — parse `Quantity.Int` to `*big.Int` (NEVER Float), USD price: `math.Round(*price * 1e8)` → big.Int, defensive empty string handling, lowercase addresses
  - `convertFee(fee, zerionChain)` — nil returns nil, same amount/price logic
- Also create `adapter_test.go` with conversion tests (precision, nil handling, chain mapping, directions, multiple transfers)

**Verify**: `go build ./internal/infra/gateway/zerion/...` + `go test ./internal/infra/gateway/zerion/... ./internal/platform/sync/...`

### Step 4: Final Verification

1. `go build ./...` — entire project compiles
2. `go test ./...` — all existing + new tests pass
3. Verify NO changes to `service.go` or `processor.go`
4. Update checkboxes in `plans/implementation-phases.md` for Phase 2

## Key Patterns to Follow

- **Alchemy client** (`alchemy/client.go`): constructor, doRequest helper, RateLimitError type
- **Alchemy adapter** (`alchemy/adapter.go`): compile-time interface check, convertTransfer helpers, nil-safety
- **Test patterns** (`alchemy/client_test.go`): external test package `_test`, table-driven tests, testify assert/require, httptest.NewServer
- **Import path**: `github.com/kislikjeka/moontrack/internal/...`

## Risk Mitigations

1. **USD float64→int64 overflow (>$92B)**: Use `math.Round(*price * 1e8)` + comment documenting the limit
2. **Empty Quantity.Int**: Defensive `SetString` check, fallback to `big.NewInt(0)`
3. **Nil nested structs**: Guard every access to FungibleInfo, ApplicationMetadata, Fee, Implementations map
4. **Pagination URL**: Zerion's Links.Next is absolute — use directly, don't reconstruct

## Verification

```bash
cd apps/backend
go build ./...                    # Zero compilation errors
go test ./...                     # All tests pass
go test -v ./internal/infra/gateway/zerion/...  # New Zerion tests
go test -v ./internal/platform/sync/...          # Domain type tests
```
