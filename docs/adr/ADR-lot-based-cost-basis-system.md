# ADR: Lot-Based Cost Basis System over Double-Entry Ledger

**Status:** Accepted
**Date:** 2026-02-07
**Deciders:** Evgenii

---

## Context

Система портфель-трекера использует double-entry ledger для учёта всех транзакций. Поверх ledger необходим механизм отслеживания стоимости приобретения (cost basis) каждого актива для расчёта PnL и налоговой отчётности.

Ключевые ограничения:
- Транзакции поступают автоматически из блокчейна
- При `transfer_in` реальная цена покупки может быть неизвестна системе
- Пользователь должен иметь возможность ретроспективно скорректировать cost basis
- PnL должен пересчитываться при любых изменениях cost basis

## Decision

Реализовать **lot-based систему** поверх double-entry ledger с FIFO-списанием и поддержкой user overrides. PnL не хранится в записях о списании, а рассчитывается на лету из лотов.

---

## Architecture Overview

### Слои системы

```
┌─────────────────────────────────────────────┐
│              Presentation Layer              │
│     WAC view │ PnL reports │ Positions       │
├─────────────────────────────────────────────┤
│            Tax / Analytics Layer             │
│        TaxLot │ LotDisposal │ Overrides      │
├─────────────────────────────────────────────┤
│            Accounting Layer                  │
│     Transaction │ Entry │ Account              │
├─────────────────────────────────────────────┤
│            Data Ingestion Layer              │
│     Blockchain sync │ Manual input            │
└─────────────────────────────────────────────┘
```

Accounting Layer — source of truth для балансов (debit = credit).
Tax Layer — source of truth для cost basis и PnL.
Связь между слоями — через `transaction_id`.

---

## Data Model

### Entity Relationship

```
Transaction 1──* Entry ──* Account
     │
     ├──* TaxLot (создаётся при поступлении актива)
     │      │
     │      ├── 0..1 linked_source_lot (при связке трансферов)
     │      │
     │      └──* LotDisposal (создаётся при выбытии)
     │
     └──* LotOverrideHistory (audit trail)
```

### Transaction

Верхнеуровневая запись о событии.

```sql
CREATE TABLE transactions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type            TEXT NOT NULL,  -- 'swap' | 'transfer_in' | 'transfer_out' | 'internal_transfer'
    timestamp       TIMESTAMPTZ NOT NULL,
    metadata        JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

> **Примечание:** Для парирования internal transfers (связь transfer_out → transfer_in) используется `metadata JSONB` с ключом `linked_transfer_id`. Это позволяет избежать отдельной миграции для нового столбца. Текущая схема `transactions` не имеет столбца `linked_transfer_id`.

### Entry (в коде: таблица `entries`)

Double-entry записи. Хранят USD snapshot на момент транзакции.

```sql
-- Таблица в БД: entries (НЕ journal_entries)
CREATE TABLE entries (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id  UUID NOT NULL REFERENCES transactions(id),
    account_id      UUID NOT NULL REFERENCES accounts(id),
    asset_id        TEXT NOT NULL,          -- 'SOL', 'USDC', 'ETH'
    amount          NUMERIC(78,0) NOT NULL, -- в base units (lamports, wei)
    debit_credit    TEXT NOT NULL,          -- 'debit' | 'credit'

    -- USD snapshot (информационный, НЕ source of truth для cost basis)
    -- Все USD значения масштабированы на 10^8 (как и везде в кодовой базе)
    usd_rate        NUMERIC(78,0),         -- цена за единицу × 10^8
    usd_value       NUMERIC(78,0),         -- общая стоимость × 10^8

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

**Важно:** `usd_value` — это snapshot рыночной цены. Он не меняется и не используется для расчёта PnL. PnL считается из tax lots.

### TaxLot

Создаётся при каждом поступлении актива на аккаунт.

```sql
CREATE TABLE tax_lots (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id      UUID NOT NULL REFERENCES transactions(id),
    account_id          UUID NOT NULL REFERENCES accounts(id),
    asset               TEXT NOT NULL,
    
    quantity_acquired   NUMERIC(78,0) NOT NULL,
    quantity_remaining  NUMERIC(78,0) NOT NULL,
    acquired_at         TIMESTAMPTZ NOT NULL,
    
    -- Автоматически рассчитанная стоимость (USD × 10^8, как usd_rate в entries)
    auto_cost_basis_per_unit    NUMERIC(78,0) NOT NULL,
    auto_cost_basis_source      TEXT NOT NULL,  -- 'swap_price' | 'fmv_at_transfer' | 'linked_transfer'

    -- Пользовательское переопределение (nullable, USD × 10^8)
    override_cost_basis_per_unit NUMERIC(78,0),
    override_reason              TEXT,
    override_at                  TIMESTAMPTZ,
    
    -- Связка с исходным лотом (для internal transfers)
    linked_source_lot_id UUID REFERENCES tax_lots(id),
    
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    CONSTRAINT positive_quantities CHECK (
        quantity_acquired > 0 AND quantity_remaining >= 0
        AND quantity_remaining <= quantity_acquired
    )
);

CREATE INDEX idx_tax_lots_fifo ON tax_lots (account_id, asset, acquired_at)
    WHERE quantity_remaining > 0;
```

### Effective Cost Basis (view)

```sql
-- Порядок приоритета:
-- 1. Ручной override
-- 2. Cost basis из связанного лота (internal transfer) — one level deep
-- 3. Автоматически рассчитанный

-- JOIN с tax_lots (не с самим view — PostgreSQL не поддерживает self-referencing views)
-- Ограничение: глубина цепочки linked lots = 1.
-- Для цепочки A → B → C: лот C получает cost basis от B, но не транзитивно от A.
-- На практике это покрывает 100% кейсов: internal transfer создаёт один linked lot.
CREATE VIEW tax_lots_effective AS
SELECT
    tl.*,
    COALESCE(
        tl.override_cost_basis_per_unit,
        COALESCE(
            source.override_cost_basis_per_unit,
            source.auto_cost_basis_per_unit
        ),
        tl.auto_cost_basis_per_unit
    ) AS effective_cost_basis_per_unit
FROM tax_lots tl
LEFT JOIN tax_lots source ON tl.linked_source_lot_id = source.id;
```

**Ограничение:** Связки глубиной > 1 (A → B → C) не поддерживаются через view. Если потребуется — заменить на PostgreSQL функцию `get_effective_cost_basis(lot_id)` с рекурсивным CTE.

### LotDisposal

Запись о списании из конкретного лота. **Не хранит PnL** — он вычисляется на лету.

```sql
CREATE TABLE lot_disposals (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id  UUID NOT NULL REFERENCES transactions(id),
    lot_id          UUID NOT NULL REFERENCES tax_lots(id),

    quantity_disposed   NUMERIC(78,0) NOT NULL,
    proceeds_per_unit   NUMERIC(78,0) NOT NULL,  -- цена продажи/обмена за единицу (USD × 10^8)
    disposal_type       TEXT NOT NULL DEFAULT 'sale',  -- 'sale' | 'internal_transfer'

    disposed_at     TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT positive_disposal CHECK (quantity_disposed > 0),
    CONSTRAINT valid_disposal_type CHECK (disposal_type IN ('sale', 'internal_transfer'))
);
```

PnL для каждого disposal:
```sql
-- realized PnL = (proceeds - effective_cost_basis) × quantity
-- Internal transfers ALWAYS have PnL = 0 regardless of cost basis changes
SELECT
    d.id,
    d.quantity_disposed,
    d.proceeds_per_unit,
    tl.effective_cost_basis_per_unit,
    CASE
        WHEN d.disposal_type = 'internal_transfer' THEN 0
        ELSE (d.proceeds_per_unit - tl.effective_cost_basis_per_unit) * d.quantity_disposed
    END AS realized_pnl
FROM lot_disposals d
JOIN tax_lots_effective tl ON d.lot_id = tl.id;
```

> **Зачем `disposal_type`:** Internal transfer disposal устанавливает `proceeds = cost_basis` для PnL = 0. Но если пользователь позже переопределит cost basis через override, PnL станет ненулевым — что неверно для внутреннего перевода. `disposal_type = 'internal_transfer'` гарантирует PnL = 0 безотносительно cost basis changes.

### LotOverrideHistory

Audit trail изменений cost basis.

```sql
CREATE TABLE lot_override_history (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lot_id              UUID NOT NULL REFERENCES tax_lots(id),
    previous_cost_basis NUMERIC(78,0),  -- USD × 10^8
    new_cost_basis      NUMERIC(78,0),  -- USD × 10^8
    reason              TEXT,
    changed_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

---

## Operation Flows

### 1. Swap (Buy SOL for USDC)

```
Input: Buy 10 SOL @ $50, spending 500 USDC

1. INSERT transaction {type: 'swap'}
2. INSERT entries:
   - {account: wallet, asset: SOL, amount: +10 SOL, direction: debit, usd_value: 500}
   - {account: wallet, asset: USDC, amount: -500 USDC, direction: credit, usd_value: 500}
3. INSERT tax_lot:
   - {asset: SOL, qty_acquired: 10, qty_remaining: 10,
      auto_cost_basis: 50, source: 'swap_price'}
4. FIFO disposal на USDC лоты — обязательно для корректного `quantity_remaining`
```

### 2. Sell (Sell SOL for USDC) — FIFO

```
Input: Sell 5 SOL @ $80, receive 400 USDC
Existing lots: Lot A (3 SOL, cost $40), Lot B (7 SOL, cost $55)

1. INSERT transaction {type: 'swap'}
2. INSERT entries:
   - {account: wallet, asset: SOL, amount: -5, direction: credit, usd_value: 400}
   - {account: wallet, asset: USDC, amount: +400, direction: debit, usd_value: 400}
3. FIFO disposal algorithm:
   a. Lot A: 3 SOL available → dispose 3
      - INSERT lot_disposal {lot: A, qty: 3, proceeds: 80}
      - UPDATE lot A: qty_remaining = 0
   b. Lot B: need 2 more → dispose 2
      - INSERT lot_disposal {lot: B, qty: 2, proceeds: 80}
      - UPDATE lot B: qty_remaining = 5
4. INSERT tax_lot for USDC {qty: 400, cost_basis: 1.0}

Calculated PnL (on read):
  Lot A: (80 - 40) × 3 = +120
  Lot B: (80 - 55) × 2 = +50
  Total realized PnL: +170
```

### 3. Internal Transfer

```
Input: Transfer 10 SOL from wallet_A to wallet_B
Source lot: Lot X (10 SOL, effective_cost: $45)

1. INSERT transaction {type: 'internal_transfer', metadata: {linked_transfer_id: ...}}
2. INSERT entries:
   - {account: wallet_A, asset: SOL, amount: -10, direction: credit}
   - {account: wallet_B, asset: SOL, amount: +10, direction: debit}
3. FIFO disposal on wallet_A:
   - INSERT lot_disposal {lot: X, qty: 10, proceeds: 45, disposal_type: 'internal_transfer'}
   - UPDATE lot X: qty_remaining = 0
4. INSERT tax_lot on wallet_B:
   - {qty: 10, auto_cost_basis: 45, source: 'linked_transfer',
      linked_source_lot_id: X}
```

### 4. Transfer In (External, Unknown Cost Basis)

```
Input: Receive 10 SOL from external address, FMV = $47

1. INSERT transaction {type: 'transfer_in'}
2. INSERT entry:
   - {account: wallet, asset: SOL, amount: +10, direction: debit, usd_value: 470}
3. INSERT tax_lot:
   - {qty: 10, auto_cost_basis: 47, source: 'fmv_at_transfer'}

Later — user override:
4. UPDATE tax_lot SET
     override_cost_basis_per_unit = 45,
     override_reason = 'Purchased on Binance at $45',
     override_at = now()
5. INSERT lot_override_history {lot, prev: NULL, new: 45, reason: ...}
```

После override все PnL, использующие этот лот, автоматически пересчитываются (т.к. считаются на лету).

---

## FIFO Disposal Algorithm

```
function disposeFIFO(account_id, asset, quantity_to_dispose, proceeds_per_unit, transaction_id):
    remaining = quantity_to_dispose
    
    lots = SELECT * FROM tax_lots
           WHERE account_id = $account_id
             AND asset = $asset
             AND quantity_remaining > 0
           ORDER BY acquired_at ASC  -- FIFO
           FOR UPDATE                -- lock rows
    
    for lot in lots:
        if remaining <= 0:
            break
        
        dispose_qty = min(lot.quantity_remaining, remaining)
        
        INSERT lot_disposal {
            transaction_id,
            lot_id: lot.id,
            quantity_disposed: dispose_qty,
            proceeds_per_unit
        }
        
        UPDATE tax_lots SET quantity_remaining = quantity_remaining - dispose_qty
            WHERE id = lot.id
        
        remaining -= dispose_qty
    
    if remaining > 0:
        ERROR "Insufficient balance: cannot dispose {remaining} more {asset}"
```

**Критично:** `FOR UPDATE` блокирует лоты на время операции для предотвращения race conditions.

---

## WAC Materialized View

```sql
CREATE MATERIALIZED VIEW position_wac AS
SELECT
    tl.account_id,
    tl.asset,
    SUM(tl.quantity_remaining) AS total_quantity,
    SUM(tl.quantity_remaining * tle.effective_cost_basis_per_unit)
        / NULLIF(SUM(tl.quantity_remaining), 0) AS weighted_avg_cost
FROM tax_lots tl
JOIN tax_lots_effective tle ON tl.id = tle.id
WHERE tl.quantity_remaining > 0
GROUP BY tl.account_id, tl.asset;

CREATE UNIQUE INDEX idx_position_wac_pk ON position_wac (account_id, asset);
```

### Refresh Strategy

**НЕ используем trigger-based refresh.** `REFRESH MATERIALIZED VIEW CONCURRENTLY` на каждый INSERT/UPDATE в `tax_lots` создаёт bottleneck при параллельной синхронизации нескольких кошельков (ADR-001 допускает полную параллельность между кошельками).

Вместо этого — **lazy refresh** (обновление по запросу):

```go
// Refresh перед чтением, если данные устарели
func (r *Repo) RefreshWACIfStale(ctx context.Context, maxAge time.Duration) error {
    // Проверяем timestamp последнего refresh
    // Если > maxAge — выполняем REFRESH MATERIALIZED VIEW CONCURRENTLY
    // Иначе — используем кэшированные данные
}
```

Альтернатива — cron refresh каждые 30-60 секунд. Выбор зависит от паттерна использования.

---

## Override Rules

### Priority Chain

```
effective_cost_basis = COALESCE(
    override_cost_basis_per_unit,       -- 1. Ручной override (высший приоритет)
    linked_lot.effective_cost_basis,    -- 2. Из связанного лота
    auto_cost_basis_per_unit            -- 3. Автоматически рассчитанный (fallback)
)
```

### Constraints

- Override может быть установлен или снят в любой момент
- При снятии override (set to NULL) система откатывается к auto или linked значению
- Override на лоте, который уже полностью списан — допустим (меняет исторический PnL)
- Каждое изменение override фиксируется в `lot_override_history`

---

## Interaction with Blockchain Sync

> **Prerequisite:** Полная реализация lot-based системы для DeFi операций (swaps, deposits, etc.) требует ADR-001 (Zerion integration). С текущим Alchemy pipeline можно реализовать лоты только для простых переводов (`transfer_in`, `transfer_out`, `internal_transfer`). Cost basis для DeFi операций (swap price, LP position value) доступен только через Zerion decoded transactions.

Lot-based cost basis система тесно взаимодействует с sync pipeline (см. [ADR-001: Zerion as Single Transaction Data Source](./001-alchemy-zerion-integration.md)).

### Zerion single-source → correct cost basis from the start

Zerion возвращает decoded транзакции с `operation_type` (trade, receive, deposit, etc.). Это означает, что тип операции **известен до записи в ledger**, и TaxLot создаётся сразу с правильным cost basis:

```
Zerion: trade (Uniswap swap ETH → USDC)
  → SwapHandler → ledger entries
  → TaxLot: {asset: USDC, cost_basis: swap_price, source: 'swap_price'}

Zerion: receive (simple ETH transfer)
  → TransferInHandler → ledger entry
  → TaxLot: {asset: ETH, cost_basis: FMV, source: 'fmv_at_transfer'}
```

Нет ситуации "записали как transfer, потом перезаписали как swap" — каждая транзакция классифицируется **до** создания ledger entries и TaxLots.

### Инвариант

**TaxLot immutability after creation**: после создания `auto_cost_basis` никогда не меняется системой. Может быть переопределён пользователем через `override_cost_basis_per_unit`, но автоматическое значение сохраняется.

**FIFO disposal безопасен в любой момент**: поскольку TaxLot создаётся с финальным cost basis, partial disposal не может привести к некорректному PnL.

---

## Consequences

### Positive
- Единый источник правды для cost basis — таблица `tax_lots`
- PnL always consistent — нет рассинхрона между лотами и disposal
- Override не ломает автоматические данные — `auto_cost_basis` сохраняется
- Гибкость метода списания — FIFO можно заменить на LIFO/WAC без изменения модели данных

### Negative
- PnL считается на лету — дороже по CPU для больших портфелей (mitigation: materialized views для отчётов)
- `quantity_remaining` — мутируемое поле на лоте (единственное исключение из иммутабельности)
- Recursive view для linked lots может быть сложен в PostgreSQL (mitigation: ограничить глубину связок или resolve при записи)

### Risks
- При большом количестве лотов FIFO disposal может быть медленным → решается индексом `idx_tax_lots_fifo`
- WAC materialized view refresh может быть тяжёлым → используем lazy/periodic refresh (см. секцию WAC Materialized View выше)
