# Add chain_id to tax lots for cross-chain FIFO clarity

## Context

User observed what looks like a FIFO violation: an older lot (#3, 02.02) is fully open while a newer lot (#2, 05.02) is partially consumed. **This is NOT a FIFO bug** — FIFO works correctly per `account_id` (per chain). The lots belong to different chains, so they have independent FIFO queues. The UI mixes lots from all chains into a flat list without chain indicator, creating the illusion of FIFO violation.

**Goal**: Expose `chain_id` through to the UI so users see which chain each lot belongs to.

## Approach: Add runtime ChainID field to TaxLot

No new types, no maps — just add a non-persisted `ChainID` field to `TaxLot` and populate it where lots are loaded.

### 1. `apps/backend/internal/ledger/taxlot_model.go` — Add `ChainID` field

Add `ChainID string` to `TaxLot` struct (line ~45, after `CreatedAt`). Not stored in DB — populated at runtime by service layer.

### 2. `apps/backend/internal/platform/taxlot/service.go` — Populate `ChainID` in `GetLotsByWallet`

In `GetLotsByWallet` (line 66-99), the function already iterates over `accounts` from `FindAccountsByWallet`. Build an `accountChainMap` and set `lot.ChainID` after collecting lots:

```go
// After line 76 (accounts loop start)
chainMap := make(map[uuid.UUID]string, len(accounts))
for _, acc := range accounts {
    if acc.ChainID != nil {
        chainMap[acc.ID] = *acc.ChainID
    }
    // ... existing lot fetching ...
}

// After sort (line 97), before return:
for _, lot := range allLots {
    lot.ChainID = chainMap[lot.AccountID]
}
```

### 3. `apps/backend/internal/transport/httpapi/handler/taxlot.go` — Add `chain_id` to response

- Add `ChainID string \`json:"chain_id"\`` to `TaxLotResponse` (after `AccountID`, line ~43)
- In `toTaxLotResponse` (line 320): set `ChainID: lot.ChainID`

### 4. `apps/frontend/src/types/taxlot.ts` — Add field

Add `chain_id?: string` to `TaxLot` interface (after `account_id`)

### 5. `apps/frontend/src/components/domain/LotDetailTable.tsx` — Show chain column

- Add "Chain" `<TableHead>` after "Acquired"
- Add `<TableCell>` displaying `lot.chain_id` (capitalize first letter)

## Verification

1. `cd apps/backend && go build ./...`
2. `just backend-test`
3. `cd apps/frontend && bun run test --run`
4. Open Holdings → expand multi-chain asset → confirm chain column shows per-lot chain
