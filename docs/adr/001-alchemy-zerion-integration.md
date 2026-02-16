# ADR-001: Zerion as Single Transaction Data Source

## Status: Accepted
## Date: 2026-02-07
## Context: PRD Blockchain Data Integration

---

## 1. Context

MoonTrack использует double-entry accounting ledger для записи всех криптовалютных операций. Текущий data pipeline:

```
Alchemy (alchemy_getAssetTransfers)
  → SyncClientAdapter (converts to sync.Transfer)
    → Processor (classifies: incoming/outgoing/internal)
      → LedgerService.RecordTransaction()
        → Handler.GenerateEntries() (balanced debit/credit)
          → PostgreSQL (transactions + entries + account_balances)
```

### Проблема
Alchemy `alchemy_getAssetTransfers` возвращает **raw transfers** — "X ETH отправлено с адреса A на адрес B". Этого достаточно для простых переводов, но не для DeFi:

- **Swap на Uniswap**: видим "отправил 1 ETH на Router" + "получил 2500 USDC от Pool" как **две отдельные** записи `transfer_out` + `transfer_in`. Нет связи между ними, нет понимания что это swap.
- **LP deposit**: видим "отправил ETH + USDC на Pool contract" как два `transfer_out`. Не знаем что это LP, не знаем tokenId позиции.
- **GMX position**: видим "отправил USDC на GMX contract" как `transfer_out`. Не знаем что это opening perp position.

Zerion API решает эту проблему — возвращает **decoded transactions** с `operation_type`, связанными `transfers[]`, протоколом и USD values. При этом Zerion покрывает и простые переводы (`receive`/`send`), то есть может быть **единственным источником данных** для транзакций.

---

## 2. Decision

### Single-source architecture: Zerion (transactions) + Alchemy (RPC only)

```
┌──────────────────────────────────────────────────────────────────┐
│                       Data Sources                                │
│                                                                   │
│  ┌──────────────────┐              ┌──────────────────┐          │
│  │     Alchemy       │              │     Zerion        │          │
│  │  (RPC only)       │              │  (Transaction      │          │
│  │                   │              │   data source)     │          │
│  │ • eth_blockNumber │              │                    │          │
│  │ • eth_call        │              │ • /transactions/   │          │
│  │ • eth_subscribe   │              │ • /positions/      │          │
│  │ • ENS resolution  │              │ • /portfolio       │          │
│  │                   │              │ • /fungibles/charts│          │
│  │ NOT used for tx   │              │                    │          │
│  │ data anymore      │              │ ALL tx data        │          │
│  └──────────────────┘              └────────┬──────────┘          │
│                                             │                     │
│                                             │ zerion.Transaction[]│
│                                             │                     │
│  ┌──────────────────────────────────────────▼──────────┐         │
│  │                   Sync Service                       │         │
│  │                                                      │         │
│  │  1. Fetch transactions from Zerion (time-based)      │         │
│  │  2. Classify by operation_type                       │         │
│  │  3. Route to appropriate handler                     │         │
│  └────────────────────────┬─────────────────────────────┘        │
│                           │                                       │
│                           │  Classified operations                │
│                           │                                       │
│  ┌────────────────────────▼──────────────────────────────┐       │
│  │                  Processor                             │       │
│  │                                                        │       │
│  │  receive/send    → existing handlers (transfer_in/out) │       │
│  │  trade           → SwapHandler (new)                   │       │
│  │  deposit/mint    → DeFiDepositHandler (new)            │       │
│  │  withdraw/burn   → DeFiWithdrawHandler (new)           │       │
│  │  claim           → DeFiClaimHandler (new)              │       │
│  │  execute         → classify by transfers[]             │       │
│  └────────────────────────┬───────────────────────────────┘      │
│                           │                                       │
│                           ▼                                       │
│  ┌────────────────────────────────────────────────────────┐      │
│  │              Ledger Service (unchanged)                 │      │
│  │                                                         │      │
│  │  Handler Registry → GenerateEntries() → VerifyBalance() │     │
│  │  → Commit Transaction + Entries + Update Balances       │      │
│  └─────────────────────────────────────────────────────────┘     │
└──────────────────────────────────────────────────────────────────┘
```

### Почему single-source

Dual-source (Alchemy transfers + Zerion DeFi) создаёт фундаментальную проблему: два источника с разной задержкой индексации, разными cursor моделями (block vs timestamp), и необходимостью корреляции по tx_hash. Это приводит к:

- **Staging buffer** для unmatched transfers (сложность)
- **Grace period** с неочевидным выбором значения (если Zerion отстаёт > grace period — DeFi контекст потерян)
- **Конфликт с TaxLots**: если Alchemy записывает в ledger до Zerion, TaxLot может быть создан с неверным cost basis и частично списан через FIFO disposal

Zerion покрывает **все** типы транзакций: и простые переводы, и DeFi. Единственный trade-off — задержка ~3-5 минут. Для portfolio tracker это приемлемо.

### Роль Alchemy

Alchemy **остаётся в проекте**, но только как RPC provider:
- `eth_blockNumber` — текущий блок
- `eth_call` — чтение контрактов (балансы, allowances)
- `eth_subscribe` — WebSocket для real-time notifications (future)
- ENS resolution
- Contract verification

Alchemy **не используется** для получения транзакций (`alchemy_getAssetTransfers` больше не вызывается в sync pipeline).

---

## 3. Sync Strategy

### Простой pipeline

```
syncWallet(wallet):
  1. Zerion: GET /wallets/{addr}/transactions/?filter[min_mined_at]={last_sync_at}
  2. For each transaction:
     → Classify by operation_type
     → Route to handler
     → Commit to ledger + TaxLots (final)
  3. Update wallet.last_sync_at
```

Нет staging buffer, нет matching, нет grace period, нет correlation. Каждая транзакция записывается **один раз, с полным контекстом**.

### Как это работает

```
┌─────────────────────────────────────────────────────────────────┐
│                     syncWallet(wallet)                            │
│                                                                   │
│  ┌───────────────────────────────────────────────────────────┐   │
│  │ Step 1: Fetch from Zerion                                  │   │
│  │                                                             │   │
│  │ GET /wallets/{addr}/transactions/                          │   │
│  │   ?filter[min_mined_at]={last_sync_at}                     │   │
│  │   &filter[chain_ids]={chain_id}                            │   │
│  │                                                             │   │
│  │ → []zerion.Transaction (decoded, with operation_type)      │   │
│  └───────────────────────────────────────────────────────────┘   │
│                           │                                       │
│                           ▼                                       │
│  ┌───────────────────────────────────────────────────────────┐   │
│  │ Step 2: Classify and commit                                │   │
│  │                                                             │   │
│  │ For each transaction:                                      │   │
│  │   operation_type → handler selection                       │   │
│  │   handler.GenerateEntries() → balanced entries             │   │
│  │   ledger.Commit() → transaction + entries + TaxLots        │   │
│  └───────────────────────────────────────────────────────────┘   │
│                           │                                       │
│                           ▼                                       │
│  ┌───────────────────────────────────────────────────────────┐   │
│  │ Step 3: Update cursor                                      │   │
│  │                                                             │   │
│  │ wallet.last_sync_at = max(transaction.mined_at)            │   │
│  └───────────────────────────────────────────────────────────┘   │
│                                                                   │
└─────────────────────────────────────────────────────────────────┘
```

### Implementation

```go
func (s *Service) syncWallet(ctx context.Context, w *wallet.Wallet) error {
    // Step 1: Fetch all transactions since last sync
    txs, err := s.zerion.GetTransactions(ctx, w.Address, w.ChainID, w.LastSyncAt)
    if err != nil {
        return fmt.Errorf("zerion fetch: %w", err)
    }

    // Step 2: Classify and commit each transaction
    for _, ztx := range txs {
        if err := s.processTransaction(ctx, w, ztx); err != nil {
            return fmt.Errorf("process tx %s: %w", ztx.TxHash, err)
        }
    }

    // Step 3: Update cursor
    if len(txs) > 0 {
        w.LastSyncAt = txs[len(txs)-1].MinedAt
        if err := s.walletRepo.UpdateSyncCursor(ctx, w); err != nil {
            return fmt.Errorf("update cursor: %w", err)
        }
    }

    return nil
}

func (s *Service) processTransaction(ctx context.Context, w *wallet.Wallet, ztx ZerionTransaction) error {
    // Classify
    txType := s.classify(ztx)
    if txType == TxTypeSkip {
        return nil // approve, failed tx, etc.
    }

    // Build ledger transaction data
    ledgerTx, err := s.buildLedgerTransaction(w, ztx, txType)
    if err != nil {
        return err
    }

    // Commit to ledger (handler generates entries, verifies balance, writes)
    // TaxLots are created here — final, one-time, with correct cost basis
    return s.ledger.RecordTransaction(ctx, ledgerTx)
}
```

### Classification

```go
func (s *Service) classify(ztx ZerionTransaction) TransactionType {
    switch ztx.OperationType {
    case "receive":
        return TxTypeTransferIn
    case "send":
        return TxTypeTransferOut
    case "trade":
        return TxTypeSwap
    case "deposit", "mint":
        return TxTypeDeFiDeposit
    case "withdraw", "burn":
        return TxTypeDeFiWithdraw
    case "claim":
        return TxTypeDeFiClaim
    case "execute":
        return s.classifyExecute(ztx)
    case "approve":
        return TxTypeSkip // no asset movement
    default:
        return TxTypeSkip
    }
}
```

### Interaction with TaxLots (cost basis)

**Ключевой инвариант**: TaxLots создаются **в момент commit**, когда тип операции уже определён.

Поскольку Zerion — единственный источник, нет ситуации "записали raw transfer, потом перезаписали как DeFi". Каждая транзакция классифицируется **до** записи в ledger:

```
Zerion: trade (Uniswap swap ETH → USDC)
  → SwapHandler → ledger entries + TaxLot {cost_basis: swap_price}

Zerion: receive (simple ETH transfer)
  → TransferInHandler → ledger entry + TaxLot {cost_basis: FMV}
```

TaxLots создаются с правильным cost basis с первого раза. Нет необходимости в reversal или re-classification. FIFO disposal безопасен в любой момент после commit.

Подробнее о взаимодействии с lot-based системой: [ADR: Lot-Based Cost Basis System](./ADR-lot-based-cost-basis-system.md).

### Concurrency

Текущая модель: `syncAllWallets()` запускает горутины с семафором (max N concurrent).
Каждый `syncWallet()` — **последовательный pipeline для одного кошелька**.

```
Wallet A goroutine:    [Zerion fetch] → [classify + commit] → done
Wallet B goroutine:    [Zerion fetch] → [classify + commit] → done
Wallet C goroutine:         [Zerion fetch] → [classify + commit] → done
                       ─────────────────────────────────────────────→ time
```

**Между кошельками** — полная параллельность (разные адреса, нет shared state).
**Внутри одного кошелька** — строгая последовательность.

#### Race condition protection

Защита (уже есть): `walletRepo.SetSyncInProgress()` ставит `sync_status = "syncing"`. `GetWalletsForSync()` возвращает только кошельки с `sync_status != "syncing"`.

#### Zerion unavailability

```go
func (s *Service) syncWallet(ctx context.Context, w *wallet.Wallet) error {
    txs, err := s.zerion.GetTransactions(ctx, w.Address, w.ChainID, w.LastSyncAt)
    if err != nil {
        return fmt.Errorf("zerion fetch: %w", err)  // sync fails, retry next cycle
    }
    // ...
}
```

Если Zerion недоступен:
- Sync fail → `wallet.sync_status = "error"` → retry на следующем цикле
- `last_sync_at` **не обновляется** — при восстановлении подхватит все пропущенные транзакции
- Балансы не обновляются до восстановления Zerion (trade-off за простоту)

#### Zerion indexing lag

Zerion индексирует и декодирует транзакции с задержкой ~3-5 минут. Это означает:

```
Timeline:
  t=0:    Block mined with Uniswap swap
  t=3-5m: Zerion проиндексировал и декодировал как "trade"
  t=next: Sync cycle подхватывает → commit as swap → ledger + TaxLots
```

Задержка **приемлема** для portfolio tracker:
- Пользователь не ожидает real-time отображение каждой транзакции
- Балансы обновляются в пределах минут, не часов
- DeFi контекст всегда корректен (нет degradation к simple transfer)

### Edge Cases

| Сценарий | Поведение |
|---|---|
| Zerion ещё не проиндексировал tx | Транзакция появится в следующем sync цикле. `last_sync_at` не сдвигается дальше последней обработанной tx. |
| Zerion вернул tx как `receive`/`send` | Записываем как transfer_in/transfer_out. Корректно для простых переводов. |
| Zerion не покрыл protocol (unknown) | `execute` → classify by `transfers[]` direction. Worst case: записано как transfer. |
| Zerion вернул `approve` | Skip — нет движения активов. |
| Повторный sync (tx уже в ledger) | `UNIQUE(source, external_id)` — duplicate отклонён. Silent skip. |
| Zerion недоступен несколько часов | Sync fails, retries. При восстановлении — batch обработка всех пропущенных tx. |
| Zerion возвращает `price: null` | USD rate = 0. Обновляется позже через CoinGecko. |

---

## 4. New Transaction Types and Handlers

### 4.1 Mapping: Zerion `operation_type` → Ledger TransactionType

> **Требуется расширение:** Текущий `TransactionType.IsValid()` в `model.go` содержит hardcoded список допустимых типов. `Registry.Register()` вызывает `IsValid()` и отклоняет неизвестные типы. Для регистрации новых handlers (`swap`, `defi_deposit` и т.д.) необходимо либо расширить `IsValid()`, либо сделать его registry-driven (принимать любой тип, зарегистрированный в handler registry).

| Zerion `operation_type` | Ledger `TransactionType` | Handler | Description |
|---|---|---|---|
| `receive` | `transfer_in` (existing) | TransferInHandler | Incoming transfer |
| `send` | `transfer_out` (existing) | TransferOutHandler | Outgoing transfer |
| `trade` | **`swap`** (new) | **SwapHandler** | DEX swap (Uniswap, etc.) |
| `deposit` | **`defi_deposit`** (new) | **DeFiDepositHandler** | LP add, lending supply, staking |
| `withdraw` | **`defi_withdraw`** (new) | **DeFiWithdrawHandler** | LP remove, lending withdraw, unstake |
| `claim` | **`defi_claim`** (new) | **DeFiClaimHandler** | Rewards claim |
| `approve` | skip | — | No asset movement, no ledger entry |
| `execute` | analyze `transfers[]` | — | Route to appropriate handler based on transfers |
| `mint` | **`defi_deposit`** | DeFiDepositHandler | NFT/LP token mint |
| `burn` | **`defi_withdraw`** | DeFiWithdrawHandler | NFT/LP token burn |

### 4.2 SwapHandler (new)

**Trigger**: Zerion `operation_type: "trade"`

**Input data** (from Zerion `transfers[]`):
```go
type SwapTransaction struct {
    WalletID      uuid.UUID
    ChainID       int64
    Protocol      string        // "Uniswap V3", "GMX"

    // Asset sold
    SoldAssetID   string        // "ETH"
    SoldAmount    *money.BigInt // in base units (from quantity.int)
    SoldDecimals  int
    SoldUSDRate   *money.BigInt // from Zerion price
    SoldContract  string        // contract address

    // Asset bought
    BoughtAssetID  string       // "USDC"
    BoughtAmount   *money.BigInt
    BoughtDecimals int
    BoughtUSDRate  *money.BigInt
    BoughtContract string

    // Gas
    GasAmount     *money.BigInt
    GasUSDRate    *money.BigInt
    GasAssetID    string        // native token
    GasDecimals   int

    // Metadata
    TxHash        string
    BlockNumber   int64
    OccurredAt    time.Time
    UniqueID      string        // Zerion transaction ID
}
```

**Generated entries** (4-6 entries):
```
DEBIT   wallet.{wallet_id}.{bought_asset}    asset_increase    bought_amount
CREDIT  wallet.{wallet_id}.{sold_asset}      asset_decrease    sold_amount

// Gas fee (if present):
DEBIT   gas.{chain_id}.{native_asset}        gas_fee           gas_amount
CREDIT  wallet.{wallet_id}.{native_asset}    asset_decrease    gas_amount
```

**Balance verification**:

> **Важный архитектурный момент**: текущая `VerifyBalance()` проверяет **глобальную** сумму: `SUM(all DEBIT amounts) = SUM(all CREDIT amounts)`. Это НЕ per-asset проверка. Для swap без clearing account это не работает — мы дебетуем 2500 USDC и кредитуем 1 ETH. Суммы в base units не равны.
>
> **Решение**: Clearing account. SwapHandler создаёт entries, где каждый amount зеркалируется (дебет и кредит на одну и ту же сумму):
> ```
> DEBIT   wallet.ETH       1 ETH        (asset_increase — купили ETH)
> CREDIT  swap_clearing     1 ETH        (clearing — зеркалирует купленный ETH)
> DEBIT   swap_clearing     2500 USDC    (clearing — зеркалирует проданный USDC)
> CREDIT  wallet.USDC      2500 USDC    (asset_decrease — продали USDC)
> ```
> **Почему это работает**: каждый amount (1 ETH, 2500 USDC) появляется ровно один раз как DEBIT и один раз как CREDIT. Поэтому **глобальная** SUM(DEBIT) = SUM(CREDIT). Clearing account — транзитный счёт, у которого каждая запись зеркалируется, поддерживая нулевой чистый эффект.
>
> **Замечание**: `VerifyBalance()` НЕ проверяет per-asset баланс. Per-asset корректность обеспечивается архитектурно — SwapHandler гарантирует, что для каждого актива сумма debits = сумма credits. Но это convention, не enforcement в `VerifyBalance()`.

### 4.3 DeFiDepositHandler (new)

**Trigger**: Zerion `operation_type: "deposit"` / `"mint"`

**Ledger entries**:
```
// Отправили ETH + USDC в LP pool
CREDIT  wallet.{wallet_id}.ETH        asset_decrease    eth_amount
CREDIT  wallet.{wallet_id}.USDC       asset_decrease    usdc_amount

// Получили LP token / position
DEBIT   wallet.{wallet_id}.{lp_token} asset_increase    lp_amount

// Clearing entries для баланса (если суммы разных активов)
// Аналогично SwapHandler — через clearing account
```

### 4.4 DeFiWithdrawHandler (new)

**Trigger**: Zerion `operation_type: "withdraw"` / `"burn"`

**Ledger entries**: Зеркало DeFiDepositHandler.

### 4.5 DeFiClaimHandler (new)

**Trigger**: Zerion `operation_type: "claim"`

**Ledger entries**:
```
DEBIT   wallet.{wallet_id}.{reward_asset}    asset_increase    reward_amount
CREDIT  income.defi.{chain_id}.{protocol}    income            reward_amount
```

---

## 5. Zerion Data → Ledger Mapping Details

### 5.1 Amount Precision

Zerion возвращает:
```json
{
  "quantity": {
    "int": "1000000000000000000",   // ← USE THIS (string → big.Int)
    "decimals": 18,
    "float": 1.0,                   // ← NEVER USE
    "numeric": "1.0"
  }
}
```

Маппинг:
```go
amount := new(big.Int)
amount.SetString(zerionTransfer.Quantity.Int, 10)  // string → big.Int
// Прямо совместимо с NUMERIC(78,0) в PostgreSQL
```

### 5.2 USD Rate

Zerion возвращает `price` (float64, USD per token) в каждом transfer. Конвертация:
```go
// Zerion: price = 2500.50 (USD per ETH)
// MoonTrack: usd_rate scaled by 10^8
usdRate := new(big.Int)
priceScaled := zerionTransfer.Price * 1e8  // 250050000000
usdRate.SetInt64(int64(priceScaled))
```

### 5.3 Chain ID Mapping

Zerion использует string chain IDs: `"ethereum"`, `"arbitrum"`, `"base"`, `"optimism"`.
MoonTrack использует numeric: `1`, `42161`, `8453`, `10`.

Маппинг через lookup table:
```go
var zerionChainToID = map[string]int64{
    "ethereum":             1,
    "polygon":              137,
    "arbitrum":             42161,
    "optimism":             10,
    "base":                 8453,
    "avalanche":            43114,
    "binance-smart-chain":  56,
}
```

### 5.4 External ID (idempotency)

```go
externalID := fmt.Sprintf("zerion_%s", zerionTransaction.ID)
source := "zerion"
// DB constraint: UNIQUE(source, external_id)
```

---

## 6. Sync Flow (detailed)

### 6.1 Wallet Sync Lifecycle

```
                    ┌──────────────────┐
                    │  Wallet created   │
                    │  sync_status:     │
                    │  "pending"        │
                    └────────┬─────────┘
                             │
                    ┌────────▼──────────────────────────────┐
                    │  Zerion: fetch transactions             │
                    │  since last_sync_at                     │
                    └────────┬──────────────────────────────┘
                             │
                    ┌────────▼──────────────────────────────┐
                    │  For each transaction:                  │
                    │  classify → handler → ledger + TaxLots │
                    └────────┬──────────────────────────────┘
                             │
                    ┌────────▼──────────────────────────────┐
                    │  Update cursor: last_sync_at           │
                    └───────────────────────────────────────┘
```

### 6.2 Incremental Sync

```
1. Zerion: GET /wallets/{addr}/transactions/?filter[min_mined_at]={last_sync_at}
   → Returns all transactions since last sync (simple + DeFi, already decoded)

2. For each transaction:
   → classify by operation_type
   → route to handler (SwapHandler, TransferInHandler, etc.)
   → commit to ledger + create TaxLots

3. Update last_sync_at = max(mined_at)
```

### 6.3 Timing

```
Scenario A: Normal flow
─────────────────────────────────────────────────────────────
  t=0:      Block mined with Uniswap swap
  t=3-5min: Zerion indexed and decoded as "trade"
  t=next:   Sync cycle picks it up
            → SwapHandler → ledger + TaxLot (final, correct cost basis)
  Результат: swap в ledger ✓

Scenario B: Zerion unavailable
─────────────────────────────────────────────────────────────
  Cycle N..N+5:
    Zerion fetch fails → sync error, retry next cycle
    last_sync_at не двигается
  Cycle N+6 (Zerion восстановился):
    Fetches all transactions since last_sync_at
    → batch commit (all with correct classification)
  Результат: eventually consistent, все данные корректны ✓

Scenario C: High volume (many transactions in one batch)
─────────────────────────────────────────────────────────────
  Zerion returns 50+ transactions for one wallet
  → Sequential commit (chronological order)
  → Each in its own DB transaction for atomicity
  Результат: все транзакции записаны в правильном порядке ✓
```

### 6.4 Handling `execute` operation type

Zerion `operation_type: "execute"` — generic contract call. Нужен анализ `transfers[]`:

```go
func classifyExecute(tx zerion.Transaction) ledger.TransactionType {
    inTransfers := filterByDirection(tx.Transfers, "in")
    outTransfers := filterByDirection(tx.Transfers, "out")

    switch {
    case len(inTransfers) > 0 && len(outTransfers) > 0:
        return TxTypeSwap  // Has both in and out = trade-like
    case len(inTransfers) > 0 && len(outTransfers) == 0:
        return TxTypeTransferIn  // Only received
    case len(outTransfers) > 0 && len(inTransfers) == 0:
        return TxTypeTransferOut  // Only sent
    default:
        return TxTypeUnknown  // No transfers, skip
    }
}
```

---

## 7. Infrastructure Components

### 7.1 Zerion Client (`internal/infra/gateway/zerion/`)

```
internal/infra/gateway/zerion/
├── client.go       // HTTP client, auth, rate limiting
├── types.go        // Zerion API response types
└── adapter.go      // Converts Zerion types → domain types
```

**Port interface** (в `internal/platform/sync/port.go`):
```go
type TransactionDataProvider interface {
    // GetTransactions returns all transactions (simple + DeFi) for a wallet since given time
    GetTransactions(ctx context.Context, address string, chainID int64, since time.Time) ([]Transaction, error)

    // GetPositions returns current DeFi positions for a wallet
    GetPositions(ctx context.Context, address string, chainID int64) ([]DeFiPosition, error)
}
```

**Domain types** (в `internal/platform/sync/`):
```go
type Transaction struct {
    ID            string            // Zerion transaction ID
    TxHash        string            // Blockchain tx hash
    ChainID       int64
    OperationType OperationType     // trade, deposit, withdraw, claim, receive, send
    Protocol      string            // "Uniswap V3", "GMX", "" for simple transfers
    Transfers     []Transfer        // In/out asset movements
    Fee           *Fee              // Gas fee
    MinedAt       time.Time
}

type Transfer struct {
    Direction       string        // "in", "out", "self"
    AssetID         string        // Token symbol
    ContractAddress string
    Amount          *big.Int      // From quantity.int
    Decimals        int
    USDPrice        float64       // USD per token at tx time
    USDValue        float64       // Total USD value
}

type OperationType string
const (
    OpTrade    OperationType = "trade"
    OpDeposit  OperationType = "deposit"
    OpWithdraw OperationType = "withdraw"
    OpClaim    OperationType = "claim"
    OpReceive  OperationType = "receive"
    OpSend     OperationType = "send"
    OpExecute  OperationType = "execute"
    OpApprove  OperationType = "approve"
)
```

### 7.2 Database Changes

**Wallets table** — заменить block-based cursor на time-based:
```sql
-- Удалить Alchemy-specific cursors (если больше не нужны для sync)
-- Добавить Zerion time-based cursor
ALTER TABLE wallets ADD COLUMN last_sync_at TIMESTAMPTZ;
```

**Accounts table** — новые типы:
```sql
-- Clearing account для swaps (транзитный, всегда нулевой баланс)
-- Код: swap_clearing.{chain_id}
-- DeFi income account для rewards
-- Код: income.defi.{chain_id}.{protocol}
```

**Account types** — расширить enum:
```go
const (
    AccountTypeCryptoWallet AccountType = "CRYPTO_WALLET"
    AccountTypeIncome       AccountType = "INCOME"
    AccountTypeExpense      AccountType = "EXPENSE"
    AccountTypeGasFee       AccountType = "GAS_FEE"
    AccountTypeClearing     AccountType = "CLEARING"     // NEW: для swap балансировки
    AccountTypeDeFiIncome   AccountType = "DEFI_INCOME"  // NEW: для DeFi rewards
)
```

> **Требуется миграция:** Текущая схема имеет `CHECK (type IN ('CRYPTO_WALLET', 'INCOME', 'EXPENSE', 'GAS_FEE'))` на таблице `accounts`. Необходимо создать миграцию для добавления `'CLEARING'` и `'DEFI_INCOME'` в CHECK constraint:
> ```sql
> ALTER TABLE accounts DROP CONSTRAINT accounts_type_check;
> ALTER TABLE accounts ADD CONSTRAINT accounts_type_check
>     CHECK (type IN ('CRYPTO_WALLET', 'INCOME', 'EXPENSE', 'GAS_FEE', 'CLEARING', 'DEFI_INCOME'));
> ```

### 7.3 Handler Registration (в `cmd/api/main.go`)

```go
// Existing handlers
handlerRegistry.Register(transfer.NewTransferInHandler(walletRepo))
handlerRegistry.Register(transfer.NewTransferOutHandler(walletRepo))
handlerRegistry.Register(transfer.NewInternalTransferHandler(walletRepo))
handlerRegistry.Register(adjustment.NewAssetAdjustmentHandler(ledgerSvc))

// New DeFi handlers
handlerRegistry.Register(defi.NewSwapHandler(walletRepo))
handlerRegistry.Register(defi.NewDeFiDepositHandler(walletRepo))
handlerRegistry.Register(defi.NewDeFiWithdrawHandler(walletRepo))
handlerRegistry.Register(defi.NewDeFiClaimHandler(walletRepo))
```

---

## 8. Consistency Guarantees

### 8.1 No Double Counting

**Invariant**: Одна Zerion транзакция → максимум одна ledger transaction.

**Механизм**:
1. `source="zerion"`, `external_id="zerion_{zerion_tx_id}"`
2. `UNIQUE(source, external_id)` constraint — duplicate отклонён
3. Time-based cursor (`last_sync_at`) гарантирует что обработанные tx не запрашиваются повторно

### 8.2 Balance Integrity

**Invariant**: Для каждой ledger transaction: `SUM(DEBIT amounts) = SUM(CREDIT amounts)` **globally** (проверяется `VerifyBalance()`).

**Swap balance**: через clearing account — каждый amount зеркалируется как DEBIT и CREDIT, обеспечивая глобальный баланс. Per-asset корректность обеспечивается convention в SwapHandler (каждый актив дебетуется и кредитуется на одну сумму).

### 8.3 Idempotency

**Invariant**: Повторный sync не создаёт дублей.

**Механизм**: `UNIQUE(source, external_id)` constraint в PostgreSQL. При conflict — silent skip.

### 8.4 Ordering

**Invariant**: Транзакции записываются в хронологическом порядке.

**Механизм**: Zerion `mined_at` монотонно возрастает. `occurred_at` берётся из blockchain timestamp. Транзакции обрабатываются последовательно в порядке `mined_at`.

---

## 9. Error Handling and Degradation

| Сценарий | Поведение |
|---|---|
| Zerion API недоступен | Sync fail. `wallet.sync_status = "error"`. Retry на следующем цикле. `last_sync_at` не двигается — при восстановлении подхватит всё. |
| Zerion возвращает `price: null` | USD rate = 0. Обновляется позже через CoinGecko. |
| Zerion rate limit (429) | Exponential backoff. Max 3 retries. Если не удалось — sync fail, retry next cycle. |
| Zerion не покрыл protocol | `execute` → classify by `transfers[]`. Worst case: transfer_in/transfer_out. |
| Zerion indexing lag (~3-5 min) | Транзакция появится в следующем sync цикле. Приемлемо для portfolio tracker. |
| Duplicate transaction from Zerion | `UNIQUE(source, external_id)` → silent skip. |
| Zerion returns empty response | Нормально — нет новых транзакций. Cursor не двигается. |

---

## 10. Alternatives Considered

### Alternative 1: Moralis only (replace Alchemy)
**Отвергнут**: 819 data inconsistencies в сутки в бенчмарках vs 0 у Alchemy. Неприемлемо для double-entry accounting где каждая ошибка ломает балансы.

### Alternative 2: Custom indexer (Ponder/Envio)
**Отвергнут**: GMX V2 EventEmitter паттерн требует 5-10 дней кастомной разработки. Zerion покрывает GMX из коробки. Не оправдано для pet-проекта.

### Alternative 3: Etherscan + ABI decoding
**Отвергнут**: 5 отдельных API вызовов на кошелёк, rate limit 5 req/sec, нет decoded DeFi context — пришлось бы парсить ABI самим.

### Alternative 4: Dual-source Alchemy (transfers) + Zerion (DeFi context)
**Отвергнут**: Два источника с разной задержкой индексации требуют correlation по tx_hash, staging buffer для unmatched transfers, grace period для определения типа операции. Усложняет логику кратно. Конфликтует с lot-based cost basis — TaxLot может быть создан с неверным cost basis (из Alchemy raw transfer) и частично списан через FIFO disposal до прихода Zerion. Zerion покрывает все типы транзакций, dual-source не оправдан.

### Alternative 5: Alchemy-first, Zerion-upgrade (modify existing entries)
**Отвергнут**: Нарушает immutability ledger entries. В double-entry accounting записи не должны изменяться после commit.

### Alternative 6: Alchemy-first, Zerion-upgrade via reversal (сторнирование)
**Отвергнут**: Конфликтует с lot-based cost basis системой. TaxLot создаётся при записи raw transfer в ledger. Лот может быть частично списан через FIFO disposal до прихода Zerion. Reversal entries не могут откатить уже произведённые disposals.

---

## 11. Migration from Current Alchemy Pipeline

Текущий pipeline использует `alchemy_getAssetTransfers`. Миграция:

1. **Добавить Zerion client** — новый gateway, не трогая существующий Alchemy код
2. **Создать новый sync processor** — на базе Zerion, с classification и новыми handlers
3. **Переключить sync service** — `syncWallet()` вызывает Zerion вместо Alchemy
4. **Deprecated**: `SyncClientAdapter`, `alchemy_getAssetTransfers` вызовы в sync pipeline
5. **Сохранить**: Alchemy client для RPC операций (eth_call, eth_blockNumber)

Для кошельков с существующими транзакциями в ledger:
- Новые транзакции пишутся через Zerion
- Существующие Alchemy записи (`source="alchemy"`) остаются нетронутыми
- Zerion idempotency (`UNIQUE(source, external_id)`) предотвращает дублирование

---

## 12. Implementation Phases

### Phase 0: Hotfix (1 day)
- [ ] `TransferCategoriesForChain(chainID)` — исключить `internal` на L2
- [ ] Тесты
- [ ] Deploy

### Phase 1: Zerion Foundation (3-5 days)
- [ ] Zerion HTTP client (`internal/infra/gateway/zerion/`)
- [ ] Zerion types и adapter
- [ ] `TransactionDataProvider` port interface
- [ ] DB migration: `wallets.last_sync_at`
- [ ] DB migration: update `accounts.type` CHECK constraint to include `'CLEARING'`, `'DEFI_INCOME'`
- [ ] Clearing account type
- [ ] Extend `TransactionType.IsValid()` to accept new types (`swap`, `defi_deposit`, etc.) or make it registry-driven
- [ ] Sync service: replace Alchemy pipeline with Zerion single-source
- [ ] Classification logic (operation_type → handler routing)
- [ ] Integration test: end-to-end sync with Zerion mock

### Phase 2: DeFi Handlers (3-5 days)
- [ ] SwapHandler с clearing account балансировкой
- [ ] DeFiDepositHandler
- [ ] DeFiWithdrawHandler
- [ ] DeFiClaimHandler
- [ ] Handler registration в main.go
- [ ] `execute` operation classifier
- [ ] Tests for each handler (balanced entries)

### Phase 3: DeFi Positions API (2-3 days)
- [ ] Zerion positions endpoint integration
- [ ] DeFi positions domain model
- [ ] Transport layer: `GET /portfolio/defi-positions`
- [ ] Frontend: DeFi positions view

### Phase 4: Advanced (future)
- [ ] GMX perp position tracking (deep)
- [ ] Historical USD backfill for entries with usd_rate=0
- [ ] Solana support
