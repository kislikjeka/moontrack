# Sync & Price Flow

Документация описывает полный pipeline получения транзакций, определения цен и их хранения в MoonTrack.

---

## Оглавление

1. [Обзор архитектуры](#1-обзор-архитектуры)
2. [Sync Service — обнаружение транзакций](#2-sync-service--обнаружение-транзакций)
3. [Noves Translate API — источник декодированных транзакций](#3-noves-translate-api--источник-декодированных-транзакций)
4. [Обработка транзакций — от API до Ledger](#4-обработка-транзакций--от-api-до-ledger)
5. [CoinGecko — фоновое обновление цен](#5-coingecko--фоновое-обновление-цен)
6. [Иерархия получения цен (runtime)](#6-иерархия-получения-цен-runtime)
7. [Хранение данных (PostgreSQL + Redis)](#7-хранение-данных-postgresql--redis)
8. [Sequence Diagram — полный поток](#8-sequence-diagram--полный-поток)
9. [Ключевые файлы](#9-ключевые-файлы)
10. [Гарантии и деградация](#10-гарантии-и-деградация)

---

## 1. Обзор архитектуры

Система имеет **два независимых фоновых процесса** для работы с ценами и транзакциями:

```
┌──────────────────────────────────────────────────────────────────────┐
│                        Background Jobs                                │
│                                                                       │
│   ┌────────────────────┐              ┌────────────────────────┐     │
│   │   Sync Service      │              │   Price Updater         │     │
│   │   (poll wallets)    │              │   (every 5 min)         │     │
│   │                     │              │                          │     │
│   │   Noves API ───┐    │              │   CoinGecko API ──┐     │     │
│   │   (декодир.    │    │              │   (текущие цены   │     │     │
│   │    транзакции, │    │              │    для портфеля)  │     │     │
│   │    БЕЗ цен)    │    │              │                   │     │     │
│   └────────┬───────┘    │              └───────────┬────────┘     │
│            │                                       │               │
│   ┌────────▼───────────────────────────────────────▼──────────┐   │
│   │                    Storage Layer                            │   │
│   │                                                             │   │
│   │   PostgreSQL                        Redis                   │   │
│   │   ├─ entries.usd_rate    ◄─ CoinGecko ├─ price:{id}:usd    │   │
│   │   │  (цена на момент tx,   (price-     │  (60s TTL)          │   │
│   │   │   иммутабельна)         pipeline / │                     │   │
│   │   │                         backfill)  ├─ price:{id}:usd:stale│  │
│   │   ├─ price_history       ◄── CoinGecko  (24h TTL)          │   │
│   │   │  (текущие цены,                  │                      │   │
│   │   │   TimescaleDB)                   │                      │   │
│   └───┴──────────────────────────────────┴──────────────────────┘   │
└──────────────────────────────────────────────────────────────────────┘
```

**Ключевое разделение:**

| Задача | Источник | Хранение | Мутабельность |
|--------|----------|----------|---------------|
| Факт транзакции (движения активов) | Noves Translate API (декодированные транзакции, без цен) | `entries.amount`, `transactions` | Иммутабельны после записи |
| Цена на момент транзакции | CoinGecko / price-pipeline + backfill-джоб | `entries.usd_rate`, `entries.usd_value` | Иммутабельна после записи |
| Текущая цена актива | CoinGecko API (фоновый polling) | `price_history` + Redis cache | Обновляется каждые 5 мин |

> **Noves не возвращает USD-цены.** Провайдер синхронизации даёт только декодированные движения активов (что, куда, сколько). Цену на момент транзакции проставляет ценовой pipeline (CoinGecko + backfill-джоб) — это отличие от прежнего провайдера (ранее Zerion отдавал встроенные цены).

---

## 2. Sync Service — обнаружение транзакций

### Точка входа

`cmd/api/main.go:251-254` → `sync.Service.Run(ctx)`

### Polling Loop

```
sync.Service.Run(ctx)
  │
  ├─ Сразу: syncAllWallets(ctx)
  ├─ Запуск ticker (PollInterval)
  └─ На каждый tick: syncAllWallets(ctx)
```

**Файл:** `internal/platform/sync/service.go:66-104`

### Выбор кошельков для синхронизации

```
syncAllWallets(ctx)
  │
  ├─ walletRepo.GetWalletsForSync()
  │   → кошельки с sync_status='pending' или просроченные
  │
  ├─ Семафор (ConcurrentWallets=3)
  │
  └─ Для каждого кошелька (горутина) → syncWallet(ctx, wallet):
        │
        ├─ ClaimWalletForSync()          ◄── атомарный захват, защита от race condition
        │
        ├─ isInitial = (wallet.LastSyncAt == nil)
        │
        ├─ [ПЕРВИЧНАЯ синхронизация]  Collect → Reconcile → Process
        │     since = 0 (вся история; InitialSyncLookback=0)
        │
        └─ [ИНКРЕМЕНТАЛЬНАЯ]          Collect → Process
              since = collect_cursor_at, иначе last_sync_at
```

**Файл:** `internal/platform/sync/service.go:201-277` (`syncWallet`)

Провайдер синхронизации инжектируется через порты `TransactionDataProvider` +
`PositionDataProvider` (provider-agnostic). Никакого preferred/fallback между
провайдерами нет — активный провайдер один (Noves). Никакого
`syncWalletZerion`/`syncWalletAlchemy` в коде тоже нет.

### Двухфазный pipeline (три фазы, реконсиляция — только при первичной)

```
Collect (collector.go)   →   Reconcile (reconciler.go)   →   Process (processor.go)
  fan-out по чейнам           только initial sync             pending raws,
  wallet.GetSupportedChains() сверка с on-chain balances,     oldest-first,
  provider.GetTransactions()  синтетические genesis raws      классификация → ledger
  → raw_transactions          для положительных дельт
```

- **Phase 1 — Collect**: fan-out по `wallet.GetSupportedChains()`, вызов
  `provider.GetTransactions()` по каждому чейну, складывание в staging-таблицу
  `raw_transactions` (со статусом `pending`). Двигает `collect_cursor_at`.
- **Phase 2 — Reconcile** (только первичная синхронизация): тянет on-chain балансы
  через `PositionDataProvider.GetPositions`, сравнивает с расчётными net-flow из
  собранных raws; для положительных дельт создаёт синтетические genesis-raws.
- **Phase 3 — Process**: читает `pending` raws oldest-first, классифицирует,
  строит сбалансированные ledger-транзакции. Двигает `last_sync_at`.

Прогресс отражается в `wallets.sync_phase` (collecting → reconciling → processing → synced → idle).

### Concurrency

```
Wallet A goroutine:    [Collect → (Reconcile) → Process] → done
Wallet B goroutine:    [Collect → (Reconcile) → Process] → done
Wallet C goroutine:         [Collect → (Reconcile) → Process] → done
                       ─────────────────────────────────────────────→ time
```

- **Между кошельками** — полная параллельность (разные адреса, нет shared state)
- **Внутри кошелька** — строгая последовательность (raws обрабатываются в хронологическом порядке)
- **Race protection**: `ClaimWalletForSync()` ставит `sync_status = "syncing"` атомарно

---

## 3. Noves Translate API — источник декодированных транзакций

Noves — это активный провайдер синхронизации (ранее использовался Zerion). Он
отдаёт уже **классифицированные** транзакции с человекочитаемыми движениями
активов, но **без встроенных USD-цен**. Клиент обращается к API **по каждому
чейну отдельно**.

### HTTP Request — транзакции

```
GET https://translate.noves.fi/evm/{chain}/txs/{address}
  ?pageSize=100
  &sort=asc                       ← oldest-first
  &startTimestamp={since (Unix ms)}   ← только при since != 0
```

- Аутентификация: заголовок `apiKey: <key>` (НЕ Basic auth).
- `{chain}` — короткий slug Noves (`eth`, `base`, …); адаптер маппит доменный
  slug (`ethereum`, `base`) в Noves-slug.

**Файл:** `internal/infra/gateway/noves/client.go:174-211` (`GetTransactions`)

### HTTP Request — балансы (для реконсиляции)

```
GET https://translate.noves.fi/evm/{chain}/tokens/balancesOf/{address}
```

- Возвращает JSON-массив балансов. На «дегенеративных» кошельках (слишком много
  ERC20) отдаёт объект `{detail: ...}` — клиент трактует это как ошибку.

**Файл:** `internal/infra/gateway/noves/client.go:218-240` (`GetBalances`)

### Rate Limiting и transient-ошибки

- Retry на HTTP 429 **и** 5xx (и сетевых ошибках) с exponential backoff: 1s → 2s → 4s
- Максимум 3 попытки; при исчерпании — ошибка, которая валит текущий цикл sync

### Пагинация

- Следование по `nextPageUrl` (absolute URL) пока `hasNextPage == true`

### Структура ответа (упрощённая)

```
Transaction
  ├─ rawTransactionData
  │   ├─ transactionHash: "0x..."         ← blockchain tx hash
  │   ├─ timestamp: Unix seconds
  │   └─ transactionFee: { token, amount }
  │
  └─ classificationData
      ├─ type: "swap" | "sendToken" | "receiveToken" | "deposit" | ...
      ├─ protocol: { name: null | "Uniswap V3" }   ← обычно null
      ├─ sent[]      → исходящие движения (out)
      │   ├─ token: { symbol, decimals, address, name }
      │   ├─ amount: "1.5"                 ← DECIMAL-строка (человеко-единицы)
      │   ├─ action: "paidGas" фильтруется (дублирует fee)
      │   ├─ from / to: { address, name }
      │   └─ nft: {...}                    ← NFT-леги не эмитятся как fungible
      └─ received[]  → входящие движения (in), та же структура
```

**Файлы:**
- `internal/infra/gateway/noves/client.go` — HTTP-клиент, retry, пагинация
- `internal/infra/gateway/noves/adapter.go` — конвертация raw JSON → `sync.DecodedTransaction`
- `internal/infra/gateway/noves/positions.go` — конвертация балансов → `sync.OnChainPosition`

### Конвертация amounts: decimal-строка → base units

Noves отдаёт суммы **десятичными строками** (человеко-единицы). Адаптер
конвертирует их **точно** в base units (`money.ToBaseUnits`) по `token.decimals`.
Если дробных цифр больше, чем `decimals`, значение усекается, а транзакция
помечается `NeedsReview` (не теряется, но флагуется).

`protocol.name` обычно `null` → протокол выводится из имён контрагентов/NFT
(эвристики `Uniswap V3`, `Aave`) в `deriveProtocol`.

> **В ответе Noves НЕТ USD-цен.** Ни у transfer, ни у балансов
> (`OnChainPosition.USDPrice == nil`). USD-цену на момент транзакции проставляет
> ценовой pipeline (CoinGecko) и backfill-джоб — см. раздел 4.

---

## 4. Обработка транзакций — от API до Ledger

Фаза Process читает `pending` raws (oldest-first) и прогоняет каждую через
`TxBuilder.ProcessTransaction`. Синтетические genesis-raws обрабатываются
отдельной веткой (`processGenesis`).

### Полный pipeline обработки одной транзакции

```
processor.ProcessAll(ctx, wallet)  → для каждой pending raw:
  │
  ├─ 0. Десериализовать RawJSON → DecodedTransaction
  │     (synthetic genesis → processGenesis, обычная → txBuilder.ProcessTransaction)
  │
  ├─ 1. Skip если tx.Status == "failed"
  │
  ├─ 2. classifier.Classify(tx) → transaction type
  │     receive  → transfer_in
  │     send     → transfer_out
  │     trade    → swap
  │     approve  → skip (нет движения активов)
  │     execute  → classify по transfers[] direction (LP/lending эвристики)
  │
  ├─ 3. Детекция внутренних переводов
  │     Контрагент — свой кошелёк? → internal_transfer
  │
  ├─ 4. Построение rawData
  │     ┌─────────────────────────────────────────┐
  │     │  buildTransferInData() / OutData()       │
  │     │    "asset_symbol":      "ETH"            │
  │     │    "amount":            "1000000000..."   │ ← base units (wei)
  │     │    "decimals":          "18"              │
  │     │    "contract_address":  "0x..."           │
  │     │    "sender":            "0x..."           │
  │     │    "recipient":         "0x..."           │
  │     │    "direction":         "in"              │
  │     │                                           │
  │     │  buildSwapData()                          │
  │     │    "from_*":  исходящий токен             │
  │     │    "to_*":    входящий токен              │
  │     └─────────────────────────────────────────┘
  │        (USD-цены здесь НЕТ — Noves её не отдаёт)
  │
  └─ 5. ledgerSvc.RecordTransaction(ctx, txType, "noves", &externalID, minedAt, rawData)
        │
        ├─ Handler.ValidateData(ctx, rawData)
        ├─ Handler.Handle(ctx, rawData)
        │   → генерирует []Entry:
        │     Entry {
        │       amount:      quantity в base units
        │       usd_rate:    проставляется ценовым pipeline / backfill-джобом
        │       usd_value:   amount × usd_rate / 10^decimals
        │       occurred_at: timestamp транзакции
        │     }
        │
        ├─ Проверка: SUM(debits) == SUM(credits)
        ├─ INSERT INTO transactions (source='noves', external_id=...)
        └─ INSERT INTO entries (amount, occurred_at, ...)
```

**Файлы:**
- `internal/platform/sync/processor.go` — Phase 3, чтение pending raws
- `internal/platform/sync/tx_builder.go` — классификация + построение rawData по типам (`ProcessTransaction`, `buildTransferInData`, `buildSwapData`, …)

> Синтетические genesis-raws (из Phase 2) пишутся с source `sync_genesis`, тип
> `genesis_balance` — см. `processGenesis` в `processor.go`.

### USD-цены для синхронизированных транзакций

Поскольку Noves не отдаёт цены, `entries.usd_rate` для синхронизированных
транзакций проставляется **не провайдером**, а ценовым pipeline (CoinGecko,
раздел 5) и отдельным backfill-джобом, который дозаполняет исторические цены на
момент транзакции. Если цены на момент записи ещё нет — она проставляется позже,
при backfill.

### Idempotency

- `UNIQUE(source, external_id)` constraint на таблице `transactions` (не изменился)
- `external_id = {chain}:{txHash}` (в нижнем регистре), не id провайдера
- При повторном sync — silent skip, ошибки нет

### Обновление cursor после успешной обработки

```
walletRepo.SetSyncCompletedAt(ctx, walletID, lastSuccessfulMinedAt)
  → wallets.last_sync_at = lastMinedAt
  → wallets.sync_phase   = 'synced'
```

---

## 5. CoinGecko — фоновое обновление цен

Отдельный background job, **не связанный** с sync pipeline. Обеспечивает актуальные цены для отображения портфеля.

### Точка входа

`cmd/api/main.go:235-248` → `asset.PriceUpdater.Run(ctx)`

### Flow

```
PriceUpdater.Run(ctx)                          (каждые 5 минут)
  │
  ├─ repo.GetActiveAssets()                    ← все активные ассеты из DB
  │
  ├─ Разбивка на батчи (BatchSize=50)
  │
  └─ Для каждого батча:
       │
       ├─ priceProvider.GetCurrentPrices(ctx, coinGeckoIDs)
       │   │
       │   │  ┌──────────────────────────────────────────────────┐
       │   │  │  HTTP GET https://api.coingecko.com/api/v3       │
       │   │  │    /simple/price                                  │
       │   │  │    ?ids=bitcoin,ethereum,usd-coin,...             │
       │   │  │    &vs_currencies=usd                            │
       │   │  │    &precision=8                                   │
       │   │  │                                                   │
       │   │  │  Response: {"bitcoin": {"usd": 67000.50}, ...}   │
       │   │  │  → float64 × 10^8 → big.Int                     │
       │   │  └──────────────────────────────────────────────────┘
       │   │
       │   └─ Обработка 429 (rate limit)
       │
       └─ Для каждой полученной цены:
            │
            ├─ priceRepo.RecordPrice()
            │   → INSERT INTO price_history (time, asset_id, price_usd, source='coingecko')
            │     ON CONFLICT (asset_id, time) DO UPDATE
            │
            ├─ cache.Set(coinGeckoID, price)
            │   → Redis: price:{assetID}:usd         TTL=60s
            │
            └─ cache.SetStale(coinGeckoID, price)
                → Redis: price:{assetID}:usd:stale   TTL=24h
```

**Файлы:**
- `internal/platform/asset/updater.go:72-91` — PriceUpdater
- `internal/infra/gateway/coingecko/client.go:57-121` — CoinGecko client

---

## 6. Иерархия получения цен (runtime)

При запросе текущей цены актива (например, для отображения портфеля) — многоуровневый fallback:

```
asset.Service.GetCurrentPrice(assetID)
  │
  ├─ Layer 1: Redis Cache (60s TTL)
  │   cache.Get(assetID) → найдено? return
  │
  ├─ Layer 2: PostgreSQL price_history (окно 5 мин)
  │   priceRepo.GetRecentPrice(assetID, 5min)
  │   → найдено? обновить Redis cache, return
  │
  ├─ Layer 3: CoinGecko API (live, через circuit breaker)
  │   priceProvider.GetCurrentPrices([assetID])
  │   → сохранить в price_history + Redis, return
  │   │
  │   └─ Circuit Breaker:
  │       открывается после 3 failures
  │       cooldown 5 мин
  │       half-open test period
  │
  ├─ Layer 4: Stale Redis Cache (24h TTL)
  │   cache.GetStale(assetID) → return с флагом IsStale=true
  │
  └─ Layer 5: ErrPriceUnavailable
```

**Файл:** `internal/platform/asset/service.go:164-242`

---

## 7. Хранение данных (PostgreSQL + Redis)

### PostgreSQL: entries (цены на момент транзакции)

```sql
CREATE TABLE entries (
    id              UUID PRIMARY KEY,
    transaction_id  UUID NOT NULL REFERENCES transactions(id),
    account_id      UUID NOT NULL REFERENCES accounts(id),
    debit_credit    VARCHAR(6),              -- 'DEBIT' или 'CREDIT'
    entry_type      VARCHAR(50),
    amount          NUMERIC(78,0) NOT NULL,  -- количество в base units (wei, etc.)
    asset_id        VARCHAR(20) NOT NULL,
    usd_rate        NUMERIC(78,0) NOT NULL,  -- цена × 10^8 на момент tx
    usd_value       NUMERIC(78,0) NOT NULL,  -- = amount × usd_rate / 10^decimals
    occurred_at     TIMESTAMP NOT NULL,
    created_at      TIMESTAMP DEFAULT NOW(),
    metadata        JSONB
);
```

> `usd_rate` и `usd_value` — **иммутабельны** после записи. Это исторический факт: цена актива в момент совершения транзакции.

### PostgreSQL: price_history (текущие/исторические цены для портфеля)

```sql
CREATE TABLE price_history (
    time        TIMESTAMPTZ NOT NULL,
    asset_id    UUID NOT NULL REFERENCES assets(id),
    price_usd   NUMERIC(78,0) NOT NULL,     -- цена × 10^8
    volume_24h  NUMERIC(78,0),
    market_cap  NUMERIC(78,0),
    source      VARCHAR(20) NOT NULL DEFAULT 'coingecko',
    PRIMARY KEY (asset_id, time)
);

-- TimescaleDB hypertable: чанки по 7 дней
SELECT create_hypertable('price_history', 'time', chunk_time_interval => INTERVAL '7 days');
```

**Continuous aggregate** для daily OHLCV:

```sql
CREATE MATERIALIZED VIEW price_history_daily AS
SELECT
    time_bucket('1 day', time) AS day,
    asset_id,
    first(price_usd, time)  AS open,
    max(price_usd)           AS high,
    min(price_usd)           AS low,
    last(price_usd, time)   AS close,
    avg(volume_24h)          AS avg_volume
FROM price_history
GROUP BY day, asset_id;
```

### PostgreSQL: assets

```sql
CREATE TABLE assets (
    id                UUID PRIMARY KEY,
    symbol            VARCHAR(20) UNIQUE NOT NULL,
    name              VARCHAR(255) NOT NULL,
    coingecko_id      VARCHAR(100) UNIQUE NOT NULL,
    decimals          INT NOT NULL,
    chain_id          VARCHAR(50),
    contract_address  VARCHAR(255),
    market_cap_rank   INT,
    is_active         BOOLEAN DEFAULT TRUE,
    created_at        TIMESTAMP,
    updated_at        TIMESTAMP
);
```

### Redis: кеш цен

| Ключ | TTL | Описание |
|------|-----|----------|
| `price:{assetID}:usd` | 60s | Свежая цена (primary cache) |
| `price:{assetID}:usd:stale` | 24h | Устаревшая цена (fallback) |

**Формат значения** (JSON):
```json
{
    "asset_id": "bitcoin",
    "usd_price": "6700050000000",
    "updated_at": "2026-02-14T12:00:00Z",
    "source": "coingecko"
}
```

**Операции:**
- `Get(ctx, assetID)` — проверяет 60s кеш
- `GetStale(ctx, assetID)` — проверяет 24h кеш
- `Set(ctx, assetID, price, source)` — записывает 60s TTL
- `SetStale(ctx, assetID, price, source)` — записывает 24h TTL
- `GetMultiple(ctx, assetIDs)` — batch get через pipeline

**Файл:** `internal/infra/redis/cache.go`

---

## 8. Sequence Diagram — полный поток

```
    Sync Service          Noves API          Ledger         CoinGecko        Redis       PostgreSQL
        │                     │                │                │              │              │
  ══════╪═════════════════════╪════════════════╪════════════════╪══════════════╪══════════════╪═══
  SYNC  │                     │                │                │              │              │
  FLOW  │                     │                │                │              │              │
        │ Phase 1: Collect (fan-out по чейнам) │                │              │              │
        │──GetTransactions───▶│                │                │              │              │
        │   (chain, addr,     │                │                │              │              │
        │    since)           │                │                │              │              │
        │◀──Transactions──────│                │                │              │              │
        │   (декодир., БЕЗ    │                │                │              │              │
        │    USD-цен)         │                │                │              │              │
        │────────────────────┼────────────────┼────────────────┼──────────────┼─UPSERT raws─▶│
        │                     │                │                │              │ raw_transactions
        │                     │                │                │              │              │
        │ Phase 2: Reconcile (только initial)  │                │              │              │
        │──GetPositions──────▶│                │                │              │              │
        │◀──balances──────────│  сверка с net-flows → genesis raws для +дельт  │              │
        │                     │                │                │              │              │
        │ Phase 3: Process (pending raws, oldest-first)         │              │              │
        │  For each raw:      │                │                │              │              │
        │  ├─ Classify        │                │                │              │              │
        │  ├─ Build rawData   │                │                │              │              │
        │  │  (без usd_price) │                │                │              │              │
        │  │                  │                │                │              │              │
        │  └─RecordTransaction┼───────────────▶│                │              │              │
        │                     │                ├─ Validate      │              │              │
        │                     │                ├─ GenEntries     │              │              │
        │                     │                │  (usd_rate:    │              │              │
        │                     │                │   backfill)    │              │              │
        │                     │                ├────────────────┼──────────────┼──INSERT tx──▶│
        │                     │                ├────────────────┼──────────────┼──INSERT ─────▶│
        │                     │                │                │              │  entries      │
        │◀────────ok──────────┼────────────────│                │              │              │
        │                     │                │                │              │              │
        │──SetSyncCompleted───┼────────────────┼────────────────┼──────────────┼──UPDATE ────▶│
        │   (advance cursor)  │                │                │              │  wallets      │
        │                     │                │                │              │              │
  ══════╪═════════════════════╪════════════════╪════════════════╪══════════════╪══════════════╪═══
  PRICE │                     │                │                │              │              │
  FLOW  │                     │                │                │              │              │
  (5min)│                     │                │                │              │              │
        │                     │                │                │              │              │
  PriceUpdater                │                │                │              │              │
        │──GetActiveAssets────┼────────────────┼────────────────┼──────────────┼──SELECT─────▶│
        │◀─assets─────────────┼────────────────┼────────────────┼──────────────┼──────────────│
        │                     │                │                │              │              │
        │──GetCurrentPrices───┼────────────────┼───────────────▶│              │              │
        │   (batch of 50)     │                │                │              │              │
        │◀──{btc:67000,...}───┼────────────────┼────────────────│              │              │
        │                     │                │                │              │              │
        │─────────────────────┼────────────────┼────────────────┼──SET cache──▶│              │
        │                     │                │                │  (60s+24h)   │              │
        │─────────────────────┼────────────────┼────────────────┼──────────────┼──INSERT──────▶│
        │                     │                │                │              │ price_history │
        │                     │                │                │              │              │
  ══════╪═════════════════════╪════════════════╪════════════════╪══════════════╪══════════════╪═══
  QUERY │                     │                │                │              │              │
  FLOW  │                     │                │                │              │              │
  (API) │                     │                │                │              │              │
        │                     │                │                │              │              │
  GetCurrentPrice(asset)      │                │                │              │              │
        │─────────────────────┼────────────────┼────────────────┼──GET cache──▶│              │
        │  ◄─ hit?            │                │                │  ◄───────────│              │
        │  ◄─ miss →──────────┼────────────────┼────────────────┼──────────────┼──SELECT─────▶│
        │  ◄─ miss → ─────────┼────────────────┼───────────────▶│              │ price_history│
        │  ◄─ miss → ─────────┼────────────────┼────────────────┼──GET stale──▶│              │
        │  ◄─ miss → ErrPriceUnavailable        │               │              │              │
```

---

## 9. Ключевые файлы

| Компонент | Файл | Ключевые функции |
|-----------|------|------------------|
| **Sync Service** | `internal/platform/sync/service.go` | `Run()`, `syncAllWallets()`, `syncWallet()` |
| **Collector (Phase 1)** | `internal/platform/sync/collector.go` | `CollectAll()`, `CollectIncremental()`, `collect()` |
| **Reconciler (Phase 2)** | `internal/platform/sync/reconciler.go` | `Reconcile()`, `calculateNetFlows()`, `buildGenesisRaw()` |
| **Processor (Phase 3)** | `internal/platform/sync/processor.go` | `ProcessAll()`, `processGenesis()`, `processRegular()` |
| **Tx Builder** | `internal/platform/sync/tx_builder.go` | `ProcessTransaction()`, `buildTransferInData()`, `buildSwapData()` |
| **Noves Client** | `internal/infra/gateway/noves/client.go` | `GetTransactions()`, `GetBalances()`, `doRequest()` |
| **Noves Adapter** | `internal/infra/gateway/noves/adapter.go` | `GetTransactions()`, `convertTransaction()`, `deriveProtocol()` |
| **Noves Positions** | `internal/infra/gateway/noves/positions.go` | `GetPositions()`, `convertBalance()` |
| **CoinGecko Client** | `internal/infra/gateway/coingecko/client.go` | `GetCurrentPrices()`, `scaleFloatToBigInt()` |
| **Asset Service** | `internal/platform/asset/service.go` | `GetCurrentPrice()`, `GetBatchPrices()` |
| **Price Updater** | `internal/platform/asset/updater.go` | `Run()`, `updatePrices()`, `updateBatch()` |
| **Redis Cache** | `internal/infra/redis/cache.go` | `Get()`, `Set()`, `SetStale()`, `GetMultiple()` |
| **Price Repository** | `internal/infra/postgres/price_repo.go` | `RecordPrice()`, `GetPriceAt()`, `GetRecentPrice()` |
| **Ledger Service** | `internal/ledger/service.go` | `RecordTransaction()` |
| **DI Wiring** | `cmd/api/main.go` | Инициализация sync, price updater, handler registry |

---

## 10. Гарантии и деградация

### Гарантии

| Свойство | Механизм |
|----------|----------|
| **Нет дублей** | `UNIQUE(source, external_id)` на `transactions`. Повторный sync → silent skip |
| **Баланс entries** | `SUM(debits) == SUM(credits)` проверяется `VerifyBalance()` при каждой записи |
| **Хронологический порядок** | Транзакции обрабатываются по `mined_at`. Cursor двигается только вперёд |
| **Атомарность записи** | Каждая транзакция — отдельная DB transaction. Cursor обновляется только после успешного commit |
| **Финансовая точность** | `NUMERIC(78,0)` в DB, `math/big.Int` в Go. Никаких float64 в хранении |

### Деградация

| Сценарий | Поведение |
|----------|-----------|
| Noves API недоступен | Ошибка per-chain fetch валит текущий цикл sync → `sync_status = "error"` → retry на следующем цикле. Курсоры (`collect_cursor_at`/`last_sync_at`) не двигаются — при восстановлении подхватит всё |
| Noves rate limit (429) / 5xx | Retry с exponential backoff: 1s → 2s → 4s. Max 3 попытки. Если не удалось — sync fail, retry next cycle |
| Ценовой pipeline недоступен | Транзакция всё равно записывается (движения активов от Noves). `usd_rate` дозаполняется позже backfill-джобом на момент транзакции |
| Reconcile: on-chain баланс < расчётного | Genesis умеет только добавлять; расхождение сверх dust помечает кошелёк degraded (`sync_status='error'`), а не заминается молча (MT-SYNC-03) |
| CoinGecko API недоступен | Circuit breaker → fallback на stale Redis cache (24h). Portfolio показывает устаревшие цены с пометкой |
| Redis недоступен | Fallback на PostgreSQL `price_history`. Увеличенная latency, но данные доступны |
