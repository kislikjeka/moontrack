# Plan: Update ADR-001 — Replace Reversal with Collect-Match-Commit

## Context

ADR-001 описывал reversal-based upgrade: Alchemy записывает raw transfers → Zerion сторнирует → пишет DeFi операции. Это конфликтует с lot-based cost basis ADR, потому что:
- TaxLot создаётся при записи в ledger
- Лот мог быть частично списан (FIFO disposal) до прихода Zerion
- Reversal entries не могут откатить disposals

**Решение**: заменить reversal на **Collect-Match-Commit** — не записывать в ledger до тех пор, пока решение (DeFi vs simple transfer) не принято окончательно.

## Architecture: Collect-Match-Commit

```
syncWallet():
  Phase 1: COLLECT (parallel)
    Alchemy: GetTransfers(fromBlock, toBlock)     → []Transfer
    Zerion:  GetTransactions(fromTime, toTime)     → []DeFiTransaction

  Phase 2: MATCH
    Group Alchemy transfers by tx_hash
    Match with Zerion by tx_hash
    → matched: DeFi operation (swap/deposit/withdraw)
    → unmatched Alchemy to EOA: simple transfer
    → unmatched Alchemy to contract: stage (buffer)

  Phase 3: COMMIT
    Matched → DeFi handler → ledger + TaxLots (final)
    Simple  → transfer handler → ledger + TaxLots (final)
    Staged  → buffer table (no ledger, no lots yet)

  Phase 4: EXPIRE STAGED
    staged_transfers WHERE status='pending' AND expires_at < NOW()
    → commit as transfer_in/transfer_out → ledger + TaxLots (final)
```

**Key invariant**: Ledger entries и TaxLots создаются только один раз, в момент финального решения. Нет reversal, нет удаления лотов.

## Changes to ADR-001

Replace section 3 (Sync Strategy) with Collect-Match-Commit description:
- Remove all reversal logic
- Add staging buffer concept
- Add grace period (3 min default)
- Show interaction with TaxLots — lots are never created prematurely

Add cross-reference to lot-based ADR explaining how the two systems interact.

## Changes to lot-based ADR

Add section "Interaction with Blockchain Sync" explaining:
- TaxLots are created at ledger commit time (never before)
- Collect-Match-Commit ensures commit happens after DeFi classification is final
- No reversal of lots is ever needed

## Files to update
- `docs/adr/001-alchemy-zerion-integration.md` — section 3 replacement
- `docs/adr/ADR-lot-based-cost-basis-system.md` — add interaction section

## Verification
- Both ADRs are internally consistent
- No reversal pattern remains
- TaxLot creation only happens at final commit
