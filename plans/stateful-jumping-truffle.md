# Plan: DeFi Transaction Handlers (defi_deposit, defi_withdraw, defi_claim)

## Context

Sync service stops when it encounters DeFi transactions from Zerion because no handlers are registered for `defi_deposit`, `defi_withdraw`, `defi_claim`. The classifier and processor already correctly classify and build data for these types — only the handlers are missing.

**Blocking error:** `"no handler registered for transaction type: defi_deposit"` — stops sync entirely for wallets with DeFi activity (AAVE deposits, Flux Finance, GMX LP positions).

**Real data:** 50 transactions in Zerion response, 11 are DeFi (9 deposit, 1 withdraw, 1 mint→defi_deposit).

## Scope

3 new handlers in `apps/backend/internal/module/defi/`:
- `DeFiDepositHandler` — deposit/mint (OUT+IN swap-like, or IN-only for mint)
- `DeFiWithdrawHandler` — withdraw/burn (OUT+IN swap-like)
- `DeFiClaimHandler` — claim rewards (IN-only, income)

Each handler includes: unit tests, validation, integration tests, Zerion real-data validation.

## Architecture

### Package structure
```
apps/backend/internal/module/defi/
├── handler_deposit.go        # DeFiDepositHandler
├── handler_withdraw.go       # DeFiWithdrawHandler
├── handler_claim.go          # DeFiClaimHandler
├── entries.go                # Shared entry generation (deposit & withdraw reuse)
├── model.go                  # DeFiTransaction, DeFiTransfer structs
├── errors.go                 # Error definitions
├── handler_test.go           # Unit tests (all 3 handlers)
└── handler_integration_test.go # Integration tests (real DB)
```

### Shared model (`model.go`)

All 3 handlers share the same input shape from `buildDeFiDepositData`/`buildDeFiWithdrawData`/`buildDeFiClaimData`:

```go
type DeFiTransaction struct {
    WalletID    uuid.UUID          `json:"wallet_id"`
    TxHash      string             `json:"tx_hash"`
    ChainID     string             `json:"chain_id"`
    OccurredAt  time.Time          `json:"occurred_at"`
    Protocol    string             `json:"protocol,omitempty"`
    Transfers   []DeFiTransfer     `json:"transfers"`
    FeeAsset    string             `json:"fee_asset,omitempty"`
    FeeAmount   *money.BigInt      `json:"fee_amount,omitempty"`
    FeeDecimals int                `json:"fee_decimals,omitempty"`
    FeeUSDPrice *money.BigInt      `json:"fee_usd_price,omitempty"`
}

type DeFiTransfer struct {
    AssetSymbol     string        `json:"asset_symbol"`
    Amount          *money.BigInt `json:"amount"`
    Decimals        int           `json:"decimals"`
    USDPrice        *money.BigInt `json:"usd_price"`
    Direction       string        `json:"direction"`       // "in" or "out"
    ContractAddress string        `json:"contract_address"`
    Sender          string        `json:"sender"`
    Recipient       string        `json:"recipient"`
}
```

Follows pattern from `apps/backend/internal/module/swap/model.go`.

### Entry generation logic

#### DeFi Deposit & Withdraw (shared in `entries.go`)

Both are swap-like: OUT asset + IN asset through clearing accounts.

```
For each OUT transfer:
  CREDIT wallet.{wID}.{chain}.{asset}   (asset_decrease)
  DEBIT  clearing.{chain}.{asset}       (clearing)

For each IN transfer:
  DEBIT  wallet.{wID}.{chain}.{asset}   (asset_increase)
  CREDIT clearing.{chain}.{asset}       (clearing)

Gas fee (if present):
  DEBIT  gas.{chain}.{feeAsset}         (gas_fee)
  CREDIT wallet.{wID}.{chain}.{feeAsset} (asset_decrease)
```

**Mint edge case (IN-only, no OUT):** Same logic — just no OUT transfers. Only asset_increase + clearing entries for the IN transfer. This handles GMX mint where only a receipt token is received.

**USD price fallback:** If IN transfer has `usd_price = 0` but OUT transfer has a price, compute:
`usdPrice(IN) = (amount_out × usdPrice_out) / amount_in`

This ensures correct cost basis on tax lots for receipt tokens like aBascbBTC.

Reference: `apps/backend/internal/module/swap/handler.go:96-279` (GenerateEntries pattern).

#### DeFi Claim (in `handler_claim.go`)

IN-only — rewards received, nothing sent out. Credits income account.

```
For each IN transfer:
  DEBIT  wallet.{wID}.{chain}.{asset}   (asset_increase)
  CREDIT income.defi.{chain}.{asset}    (income)

Gas fee (if present):
  DEBIT  gas.{chain}.{feeAsset}         (gas_fee)
  CREDIT wallet.{wID}.{chain}.{feeAsset} (asset_decrease)
```

Reference: `apps/backend/internal/module/transfer/handler_in.go:105-172` (income pattern).

### Metadata enrichment

All entries get `operation_type` in metadata (from classifier: "deposit", "withdraw", "mint", "burn", "claim") plus `protocol` if available. This supports future LendingPosition tracking without changing handler logic.

```go
metadata["operation_type"] = txn.OperationType  // "deposit" | "mint" | "withdraw" | "burn" | "claim"
metadata["protocol"]       = txn.Protocol        // "AAVE" | "GMX" | ""
```

Requires adding `OperationType` field to `DeFiTransaction` model and passing it through from `buildDeFiDepositData()`.

### Validation rules

Per handler, following existing pattern from swap/transfer handlers:

- `wallet_id` required, must exist in DB, must belong to user (authorization check)
- `tx_hash` required
- `chain_id` required
- **Deposit/Withdraw**: at least 1 transfer required
- **Claim**: at least 1 IN transfer required
- Each transfer: `asset_symbol` non-empty, `amount` positive

### Registration in `cmd/api/main.go`

After swap handler registration (line 143):
```go
defiDepositHandler := defi.NewDeFiDepositHandler(walletRepo, log)
handlerRegistry.Register(defiDepositHandler)

defiWithdrawHandler := defi.NewDeFiWithdrawHandler(walletRepo, log)
handlerRegistry.Register(defiWithdrawHandler)

defiClaimHandler := defi.NewDeFiClaimHandler(walletRepo, log)
handlerRegistry.Register(defiClaimHandler)
```

### Processor metadata update

In `apps/backend/internal/platform/sync/zerion_processor.go`, update `buildDeFiDepositData`, `buildDeFiWithdrawData`, `buildDeFiClaimData` to include `operation_type` from the original Zerion operation:

```go
func (p *ZerionProcessor) buildDeFiDepositData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
    data := p.buildBaseData(w, tx)
    data["transfers"] = p.buildTransferArray(tx.Transfers)
    data["operation_type"] = string(tx.OperationType) // NEW: "deposit" or "mint"
    return data
}
```

Same for withdraw ("withdraw" or "burn") and claim ("claim").

## Files to modify

| File | Change |
|---|---|
| `apps/backend/internal/module/defi/` (NEW) | All handler files, model, errors, tests |
| `apps/backend/cmd/api/main.go:143` | Register 3 new handlers |
| `apps/backend/internal/platform/sync/zerion_processor.go:305-321` | Add `operation_type` to data builders |

## Execution Plan (parallelizable tasks)

### Phase 1: Foundation (sequential, 1 agent)

**Agent**: `general-purpose`

1. Create `apps/backend/internal/module/defi/` package
2. Create `model.go` — DeFiTransaction, DeFiTransfer structs with Validate()
3. Create `errors.go` — error definitions
4. Create `entries.go` — shared `generateSwapLikeEntries()` and `generateGasFeeEntries()` functions

### Phase 2: Handlers (parallel, 3 agents)

All 3 handlers are independent — no dependencies between them. Each implements `ledger.Handler` interface.

**Agent A** (`general-purpose`): `handler_deposit.go`
- `DeFiDepositHandler` with `Type() = TxTypeDefiDeposit`
- Handle(): unmarshal → validate → generate entries
- ValidateData(): wallet exists + ownership + transfers non-empty
- Uses `generateSwapLikeEntries()` from entries.go
- Handles mint edge case (IN-only transfers)
- USD price fallback for receipt tokens

**Agent B** (`general-purpose`): `handler_withdraw.go`
- `DeFiWithdrawHandler` with `Type() = TxTypeDefiWithdraw`
- Same structure as deposit, uses same `generateSwapLikeEntries()`
- USD price fallback for receipt tokens (OUT direction)

**Agent C** (`general-purpose`): `handler_claim.go`
- `DeFiClaimHandler` with `Type() = TxTypeDefiClaim`
- Different entry logic: IN transfers → income account (not clearing)
- Validates only IN transfers present

### Phase 3: Unit tests (parallel, 2 agents)

**Agent D** (`general-purpose`): Unit tests for deposit + withdraw
- `TestDeFiDepositHandler_Type()`
- `TestDeFiDepositHandler_SimpleDeposit_Balance()` — AAVE cbBTC→aBascbBTC, 4 entries, SUM(debit)==SUM(credit)
- `TestDeFiDepositHandler_WithGasFee_Balance()` — 6 entries
- `TestDeFiDepositHandler_MintOnly_Balance()` — GMX mint, IN-only, 2 entries
- `TestDeFiDepositHandler_USDPriceFallback()` — receipt token gets computed price
- `TestDeFiDepositHandler_Validate_MissingFields()` — table-driven subtests
- `TestDeFiDepositHandler_EntryMetadata()` — operation_type, protocol in metadata
- Same set for withdraw handler
- Authorization tests (cross-user wallet)

**Agent E** (`general-purpose`): Unit tests for claim
- `TestDeFiClaimHandler_Type()`
- `TestDeFiClaimHandler_SimpleClaim_Balance()` — 2 entries (wallet debit + income credit)
- `TestDeFiClaimHandler_WithGasFee_Balance()` — 4 entries
- `TestDeFiClaimHandler_MultipleRewards_Balance()` — multiple IN transfers
- `TestDeFiClaimHandler_Validate_NoInTransfers()` — error case
- `TestDeFiClaimHandler_EntryMetadata()` — income account code pattern
- Authorization tests

### Phase 4: Integration & wiring (sequential, 1 agent)

**Agent F** (`general-purpose`):

1. Register handlers in `cmd/api/main.go`
2. Add `operation_type` to zerion processor data builders
3. Update zerion processor tests for new metadata field
4. Create `handler_integration_test.go` with real DB tests:
   - `TestDeFiDeposit_E2E_AAVE()` — real AAVE data from Zerion response:
     - wallet_id, tx_hash=`0x30a455...`, chain_id=base
     - OUT: cbBTC amount=981547 decimals=8 price=6795302440668
     - IN: aBascbBTC amount=981581 decimals=8 price=0
     - **Verify**: 4 entries balanced, cbBTC balance decreased, aBascbBTC balance increased
     - **Verify**: tax lot created for aBascbBTC with computed cost basis
     - **Verify**: tax lot consumed for cbBTC via FIFO
   - `TestDeFiWithdraw_E2E_Flux()` — real Flux data:
     - OUT: fUSDC amount=1360462608 decimals=6 price=null
     - IN: USDC amount=1500000000 decimals=6 price=99920006
     - **Verify**: fUSDC balance decreased, USDC balance increased, lots correct
   - `TestDeFiDeposit_E2E_Mint_GMX()` — real GMX mint data:
     - IN only: GM token amount=151057598000000000000 decimals=18 price=0
     - **Verify**: 2 entries (wallet + clearing), balance increased, lot created with cost_basis=0
   - `TestDeFiDeposit_Idempotency()` — same external_id twice → no duplicate
   - `TestDeFiDeposit_NegativeBalance_Rejected()` — withdraw without prior deposit → error

### Phase 5: Full sync validation (sequential, 1 agent)

**Agent G** (`general-purpose`):

1. Run `go build ./...` — verify compilation
2. Run all unit tests: `go test ./internal/module/defi/...`
3. Run integration tests: `go test -tags integration ./internal/module/defi/...`
4. Run existing sync tests to verify no regressions: `go test ./internal/platform/sync/...`
5. Run full test suite: `go test ./...`

## Verification (manual, post-implementation)

1. `just dev` — start backend
2. Trigger wallet sync via UI or API
3. Check Loki logs:
   ```logql
   {service="backend", component="sync"} | json | tx_type=~"defi_.*"
   ```
   — should see "transaction recorded to ledger" instead of "handler not found"
4. Check no sync errors:
   ```logql
   {service="backend", component="sync", level="ERROR"}
   ```
5. Verify ledger entries created:
   ```logql
   {service="backend", component="ledger"} | json | tx_type="defi_deposit"
   ```
6. Check portfolio shows aBascbBTC and other receipt tokens in positions

## Security & Transactional Safety

- **Double-entry balance**: Every handler must produce entries where SUM(debit) == SUM(credit). Enforced by `Transaction.VerifyBalance()` in ledger service before commit.
- **Atomicity**: All entries, balance updates, and tax lots are committed in a single DB transaction. Failure at any point → full rollback.
- **Negative balance protection**: `transactionCommitter.applyBalanceChange()` rejects transactions that would make any account balance negative. Row-level locking (`SELECT FOR UPDATE`) prevents race conditions.
- **Authorization**: Each handler verifies wallet belongs to the authenticated user via `middleware.GetUserIDFromContext()`.
- **Idempotency**: Ledger service checks `external_id` uniqueness — duplicate Zerion transaction IDs are silently skipped.
- **Tax lot integrity**: TaxLotHook runs inside the same DB transaction — lot creation/consumption is atomic with entry persistence.
- **Input validation**: All fields validated before entry generation — prevents malformed data from reaching the ledger.
- **Immutability**: Entries are never updated or deleted after creation (DB constraint: `ON DELETE RESTRICT`).
