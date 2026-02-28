# LP Position Tracking (Uniswap V3) — Design

**Date**: 2026-02-28
**Scope**: Backend only (API + accounting). Frontend in next iteration.
**Protocol**: Uniswap V3 only. Model is generic (protocol field) for future extension.

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Tick range / fee tier | Deferred (MVP) | Zerion API doesn't provide; needs on-chain RPC or subgraph |
| Position linking | NFT token_id + heuristic | token_id from deposit; token pair match for withdraw/claim |
| Accounting model | Existing DeFi entry types | asset_increase/decrease + clearing + income; TaxLotHook unchanged |
| PnL calculation | LP Position entity level | Aggregated USD sums; unrealized PnL approximate for open positions |
| Tx ↔ Position link | lp_position_id in JSONB metadata | No schema change to transactions table |

## Transaction Types

Three new types in `ledger/model.go`:

- `lp_deposit` — add liquidity to pool
- `lp_withdraw` — remove liquidity from pool
- `lp_claim_fees` — collect accumulated fees

## Handlers (module/liquidity/)

### LPDepositHandler
- Token OUT via clearing (asset_decrease on wallet accounts)
- Tax lots consumed for both tokens (FIFO)
- Gas fee entries

### LPWithdrawHandler
- Token IN via clearing (asset_increase on wallet accounts)
- New tax lots created with FMV cost basis
- Gas fee entries

### LPClaimFeesHandler
- Token IN as income (`income.lp.{chain}.{asset}`)
- New tax lots created
- Gas fee entries

### LPTransaction Model
Extends DeFi pattern with:
- `NFTTokenID string` — Uniswap V3 position NFT ID
- `LPPositionID uuid.UUID` — link to LP position entity

## LP Position Entity

### Table: `lp_positions`

```sql
CREATE TABLE lp_positions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id),
    wallet_id       UUID NOT NULL REFERENCES wallets(id),
    chain_id        VARCHAR(50) NOT NULL,
    protocol        VARCHAR(100) NOT NULL,
    nft_token_id    VARCHAR(100),
    contract_address VARCHAR(255),

    token0_symbol   VARCHAR(50) NOT NULL,
    token1_symbol   VARCHAR(50) NOT NULL,
    token0_contract VARCHAR(255),
    token1_contract VARCHAR(255),
    token0_decimals SMALLINT NOT NULL,
    token1_decimals SMALLINT NOT NULL,

    total_deposited_usd    NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_withdrawn_usd    NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_claimed_fees_usd NUMERIC(78,0) NOT NULL DEFAULT 0,

    total_deposited_token0  NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_deposited_token1  NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_withdrawn_token0  NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_withdrawn_token1  NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_claimed_token0    NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_claimed_token1    NUMERIC(78,0) NOT NULL DEFAULT 0,

    status          VARCHAR(20) NOT NULL DEFAULT 'open',
    opened_at       TIMESTAMPTZ NOT NULL,
    closed_at       TIMESTAMPTZ,

    realized_pnl_usd NUMERIC(78,0),
    apr_bps          INTEGER,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_lp_positions_user ON lp_positions(user_id);
CREATE INDEX idx_lp_positions_wallet ON lp_positions(wallet_id);
CREATE UNIQUE INDEX idx_lp_positions_nft ON lp_positions(wallet_id, chain_id, nft_token_id)
    WHERE nft_token_id IS NOT NULL;
```

### Platform Service (platform/lpposition/)

Key methods:
- `FindOrCreate(wallet, chain, protocol, nftTokenID, tokens)` — on deposit
- `FindOpenByTokenPair(wallet, chain, protocol, token0, token1)` — heuristic for withdraw/claim
- `RecordDeposit(positionID, amounts, usdValue)` — update aggregates
- `RecordWithdraw(positionID, amounts, usdValue)` — update aggregates, may close
- `RecordClaimFees(positionID, amounts, usdValue)` — update aggregates
- `ClosePosition(positionID)` — finalize PnL, APR, status=closed
- `GetPositions(userID, filters)` — list with filtering
- `GetPositionPnL(positionID, currentPrices)` — unrealized PnL for open positions

### PnL Calculation

- **Closed**: `realized_pnl = total_withdrawn_usd + total_claimed_fees_usd - total_deposited_usd`
- **Open** (approximate): `unrealized_pnl = (remaining_tokens_current_value + total_withdrawn_usd + total_claimed_fees_usd) - total_deposited_usd`
  - `remaining_token0 = total_deposited_token0 - total_withdrawn_token0`
  - `remaining_token1 = total_deposited_token1 - total_withdrawn_token1`
  - Approximate because impermanent loss changes actual pool proportions
- **APR**: `pnl / total_deposited_usd / duration_years * 10000` (basis points)

### Position Close Detection

```
remaining_token0 = total_deposited_token0 - total_withdrawn_token0
remaining_token1 = total_deposited_token1 - total_withdrawn_token1
if remaining_token0 <= 0 AND remaining_token1 <= 0 → close position
```

## Sync Pipeline Changes

### Zerion Adapter
- Extract `nft_token_id` from NFT transfers (currently skipped when `fungible_info == nil`)
- Parse `acts` array and pass to `DecodedTransaction`
- New fields: `NFTTokenID string`, `Acts []Act`

### Classifier
- `operation_type="deposit" + protocol="Uniswap V3"` → `lp_deposit`
- `operation_type="withdraw" + protocol="Uniswap V3"` → `lp_withdraw`
- `operation_type="receive" + acts contains "claim" + protocol="Uniswap V3"` → `lp_claim_fees`
- All other protocols: unchanged behavior

### ZerionProcessor
- `buildLPDepositData()` — like DeFi deposit + `nft_token_id`
- `buildLPWithdrawData()` — like DeFi withdraw
- `buildLPClaimFeesData()` — like DeFi claim

### Processor Post-Processing
After `ledgerSvc.RecordTransaction()` for LP types:
1. FindOrCreate LP position (by nft_token_id or heuristic)
2. RecordDeposit/Withdraw/ClaimFees
3. Store lp_position_id in transaction metadata
4. On withdraw: check if position fully closed → ClosePosition()

### Heuristic for Linking withdraw/claim to Position
1. Find open positions by (wallet_id, chain_id, protocol="Uniswap V3")
2. Filter by matching token pair (token0 + token1)
3. 1 match → auto-link
4. 0 matches → create orphan position (incomplete import case)
5. >1 matches → link to oldest open (FIFO), log warning

## API Endpoints

All under `/api/v1`, JWT protected:

```
GET  /lp/positions                    — list user's LP positions
GET  /lp/positions/{id}               — position details + PnL/APR
GET  /lp/positions/{id}/transactions  — transactions linked to position
```

Query filters for list: `?status=open|closed`, `?wallet_id=...`, `?chain_id=...`

## File Structure

### New Files
```
internal/module/liquidity/
    handler_deposit.go
    handler_withdraw.go
    handler_claim_fees.go
    entries.go
    model.go

internal/platform/lpposition/
    service.go
    port.go
    model.go

internal/infra/postgres/
    lp_position_repo.go

internal/transport/httpapi/handler/
    lp_position.go

migrations/
    000022_lp_positions.up.sql
    000022_lp_positions.down.sql
```

### Modified Files
```
internal/ledger/model.go                     — +3 transaction type constants
internal/infra/gateway/zerion/adapter.go     — NFT token_id + Acts extraction
internal/platform/sync/model.go             — DecodedTransaction fields
internal/platform/sync/classifier.go        — LP classification logic
internal/platform/sync/zerion_processor.go  — LP data builders
internal/platform/sync/processor.go         — LP position post-processing
internal/module/transactions/service.go     — reader for LP types
cmd/api/main.go                             — DI wiring
```

### Layer Dependencies
```
transport/handler/lp_position → platform/lpposition, platform/asset
module/liquidity/handlers     → ledger, platform/wallet
platform/lpposition/service   → platform/lpposition/port
platform/sync/processor       → platform/lpposition
infra/postgres/lp_position_repo → platform/lpposition/port (implements)
```

No layer violations: `transport → module → platform → ledger ← infra`

## Testing

### Unit Tests
- Handler entries: balanced, correct types, gas fees
- LP Position service: CRUD, aggregation math, PnL/APR
- Multi deposit/withdraw scenario: deposit $100 → withdraw $30 → deposit $50 → withdraw all
- Position close detection with impermanent loss
- Classifier: Uni V3 vs other protocols, receive+claim act

### Integration Tests
- Full LP lifecycle through sync pipeline
- deposit → claim fees → partial withdraw → full withdraw → position closed
- Correct tax lot creation/consumption throughout

## Known Limitations (MVP)

1. **No tick range / fee tier** — Zerion doesn't provide; needs on-chain data source
2. **Unrealized PnL approximate** — uses deposited-withdrawn token diff, not actual pool balances
3. **Uniswap V3 only** — other protocols continue as generic defi_deposit/withdraw/claim
4. **No frontend** — API-only in this iteration
5. **Heuristic linking** — may mis-link with multiple same-pair positions (warning logged)

## Future Iterations

1. On-chain RPC for tick range, fee tier, current pool balances
2. Accurate unrealized PnL via on-chain position query
3. Frontend: LP positions page, transaction history, PnL dashboard
4. Universal LP system for other protocols (Aerodrome, Curve, etc.)
