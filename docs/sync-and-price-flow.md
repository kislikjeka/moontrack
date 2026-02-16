# Sync & Price Flow

Документация описывает полный pipeline получения транзакций, определения цен и их хранения в MoonTrack.

---

## Оглавление

1. [Обзор архитектуры](#1-обзор-архитектуры)
2. [Sync Service — обнаружение транзакций](#2-sync-service--обнаружение-транзакций)
3. [Zerion API — транзакции с встроенными ценами](#3-zerion-api--транзакции-с-встроенными-ценами)
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
│   │   Zerion API ──┐    │              │   CoinGecko API ──┐     │     │
│   │   (транзакции  │    │              │   (текущие цены   │     │     │
│   │    + цены на   │    │              │    для портфеля)  │     │     │
│   │    момент tx)  │    │              │                   │     │     │
│   └────────┬───────┘    │              └───────────┬────────┘     │
│            │                                       │               │
│   ┌────────▼───────────────────────────────────────▼──────────┐   │
│   │                    Storage Layer                            │   │
│   │                                                             │   │
│   │   PostgreSQL                        Redis                   │   │
│   │   ├─ entries.usd_rate    ◄── Zerion  ├─ price:{id}:usd     │   │
│   │   │  (цена на момент tx,             │  (60s TTL)           │   │
│   │   │   иммутабельна)                  │                      │   │
│   │   │                                  ├─ price:{id}:usd:stale│   │
│   │   ├─ price_history       ◄── CoinGecko  (24h TTL)          │   │
│   │   │  (текущие цены,                  │                      │   │
│   │   │   TimescaleDB)                   │                      │   │
│   └───┴──────────────────────────────────┴──────────────────────┘   │
└──────────────────────────────────────────────────────────────────────┘
```

**Ключевое разделение:**

| Задача | Источник | Хранение | Мутабельность |
|--------|----------|----------|---------------|
| Цена на момент транзакции | Zerion API (встроена в ответ) | `entries.usd_rate`, `entries.usd_value` | Иммутабельна после записи |
| Текущая цена актива | CoinGecko API (фоновый polling) | `price_history` + Redis cache | Обновляется каждые 5 мин |

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
  └─ Для каждого кошелька (горутина):
        │
        ├─ ClaimWalletForSync()          ◄── атомарный захват, защита от race condition
        │
        ├─ Определение cursor:
        │   wallet.LastSyncAt существует → since = LastSyncAt
        │   первичная синхронизация      → since = now - 90 дней
        │
        ├─ [PREFERRED] syncWalletZerion(ctx, wallet)
        │
        └─ [FALLBACK]  syncWalletAlchemy(ctx, wallet)
```

**Файл:** `internal/platform/sync/service.go:124-187`

### Concurrency

```
Wallet A goroutine:    [Zerion fetch] → [classify + commit] → done
Wallet B goroutine:    [Zerion fetch] → [classify + commit] → done
Wallet C goroutine:         [Zerion fetch] → [classify + commit] → done
                       ─────────────────────────────────────────────→ time
```

- **Между кошельками** — полная параллельность (разные адреса, нет shared state)
- **Внутри кошелька** — строгая последовательность (хронологический порядок)
- **Race protection**: `ClaimWalletForSync()` ставит `sync_status = "syncing"` атомарно

---

## 3. Zerion API — транзакции с встроенными ценами

### HTTP Request

```
GET https://api.zerion.io/v1/wallets/{address}/transactions/
  ?filter[chain_ids]={chainID}
  &filter[min_mined_at]={since (RFC3339)}
  &filter[asset_types]=fungible
  &filter[trash]=only_non_trash
```

**Файл:** `internal/infra/gateway/zerion/client.go:113-148`

### Rate Limiting

- Retry на HTTP 429 с exponential backoff: 1s → 2s → 4s
- Максимум 3 попытки

### Пагинация

- Следование по `Links.Next` (absolute URL) до исчерпания страниц

### Структура ответа (упрощённая)

```
TransactionData
  ├─ ID: string                           ← Zerion transaction ID (для idempotency)
  ├─ Attributes
  │   ├─ OperationType: "trade" | "receive" | "send" | ...
  │   ├─ Hash: "0x..."                    ← blockchain tx hash
  │   ├─ MinedAt: RFC3339 timestamp
  │   ├─ Status: "confirmed" | "pending" | "failed"
  │   │
  │   ├─ Fee
  │   │   ├─ FungibleInfo: { Symbol, Implementations }
  │   │   ├─ Quantity: { Int: "base_units", Decimals }
  │   │   └─ Price: *float64              ◄── USD цена gas token на момент tx
  │   │
  │   ├─ Transfers[]
  │   │   ├─ FungibleInfo: { Symbol, Implementations }
  │   │   ├─ Direction: "in" | "out"
  │   │   ├─ Quantity: { Int: "base_units", Decimals }
  │   │   ├─ Price: *float64              ◄── USD цена токена на момент tx
  │   │   ├─ Sender: "0x..."
  │   │   └─ Recipient: "0x..."
  │   │
  │   └─ ApplicationMD: { Name: "Uniswap V3" }
```

**Файлы:**
- `internal/infra/gateway/zerion/types.go:30-119` — типы ответа
- `internal/infra/gateway/zerion/adapter.go` — конвертация в domain типы

### Конвертация цен: float64 → big.Int

```go
// adapter.go:177-183
func usdFloatToBigInt(price float64) *big.Int {
    scaled := int64(price * 1e8)  // $67,000.50 → 6_700_050_000_000
    return big.NewInt(scaled)
}
```

- Масштабирование: × 10^8
- Безопасный диапазон: до ~$92 млрд (ограничение int64)
- Все цены далее хранятся как `NUMERIC(78,0)` / `big.Int`

> **Zerion — единственный источник цен на момент транзакции.** CoinGecko используется только для текущих цен портфеля.

---

## 4. Обработка транзакций — от API до Ledger

### Полный pipeline обработки одной транзакции

```
zerionProcessor.ProcessTransaction(ctx, wallet, tx)
  │
  ├─ 1. Skip если tx.Status == "failed"
  │
  ├─ 2. classifier.Classify(tx) → transaction type
  │     receive  → transfer_in
  │     send     → transfer_out
  │     trade    → swap
  │     approve  → skip (нет движения активов)
  │     execute  → classify по transfers[] direction
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
  │     │    "usd_price":         "6700050000000"   │ ← из Zerion Price
  │     │    "sender":            "0x..."           │
  │     │    "recipient":         "0x..."           │
  │     │    "direction":         "in"              │
  │     │                                           │
  │     │  buildSwapData()                          │
  │     │    "from_*":  исходящий токен + usd_price │
  │     │    "to_*":    входящий токен + usd_price  │
  │     └─────────────────────────────────────────┘
  │
  └─ 5. ledgerSvc.RecordTransaction(ctx, txType, "zerion", &externalID, minedAt, rawData)
        │
        ├─ Handler.ValidateData(ctx, rawData)
        ├─ Handler.Handle(ctx, rawData)
        │   → генерирует []Entry:
        │     Entry {
        │       amount:      quantity в base units
        │       usd_rate:    из rawData["usd_price"]    ◄── цена от Zerion
        │       usd_value:   amount × usd_rate / 10^decimals
        │       occurred_at: timestamp транзакции
        │     }
        │
        ├─ Проверка: SUM(debits) == SUM(credits)
        ├─ INSERT INTO transactions (source='zerion', external_id=...)
        └─ INSERT INTO entries (usd_rate, usd_value, ...)
```

**Файл:** `internal/platform/sync/zerion_processor.go`

### Idempotency

- `UNIQUE(source, external_id)` constraint на таблице `transactions`
- `external_id = zerion_{zerion_tx_id}`
- При повторном sync — silent skip, ошибки нет

### Обновление cursor после успешной обработки

```
walletRepo.SetSyncCompletedAt(ctx, walletID, lastSuccessfulMinedAt)
  → wallets.last_sync_at = lastMinedAt
  → wallets.sync_status = 'synced'
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
    Sync Service          Zerion API         Ledger         CoinGecko        Redis       PostgreSQL
        │                     │                │                │              │              │
  ══════╪═════════════════════╪════════════════╪════════════════╪══════════════╪══════════════╪═══
  SYNC  │                     │                │                │              │              │
  FLOW  │                     │                │                │              │              │
        │──GetTransactions───▶│                │                │              │              │
        │   (addr, chain,     │                │                │              │              │
        │    since)           │                │                │              │              │
        │◀──Transactions──────│                │                │              │              │
        │   with embedded     │                │                │              │              │
        │   USD prices ◄──────│                │                │              │              │
        │                     │                │                │              │              │
        │  For each tx:       │                │                │              │              │
        │  ├─ Classify        │                │                │              │              │
        │  ├─ Build rawData   │                │                │              │              │
        │  │  (incl usd_price │                │                │              │              │
        │  │   from Zerion)   │                │                │              │              │
        │  │                  │                │                │              │              │
        │  └─RecordTransaction┼───────────────▶│                │              │              │
        │                     │                ├─ Validate      │              │              │
        │                     │                ├─ GenEntries     │              │              │
        │                     │                │  (usd_rate from│              │              │
        │                     │                │   Zerion price)│              │              │
        │                     │                ├────────────────┼──────────────┼──INSERT tx──▶│
        │                     │                ├────────────────┼──────────────┼──INSERT ─────▶│
        │                     │                │                │              │  entries w/   │
        │◀────────ok──────────┼────────────────│                │              │  usd_rate     │
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
| **Sync Service** | `internal/platform/sync/service.go` | `Run()`, `syncAllWallets()`, `syncWalletZerion()` |
| **Zerion Processor** | `internal/platform/sync/zerion_processor.go` | `ProcessTransaction()`, `buildTransferInData()`, `buildSwapData()` |
| **Zerion Client** | `internal/infra/gateway/zerion/client.go` | `GetTransactions()`, `doRequest()` |
| **Zerion Adapter** | `internal/infra/gateway/zerion/adapter.go` | `convertTransaction()`, `usdFloatToBigInt()` |
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
| Zerion API недоступен | Sync fail → `sync_status = "error"` → retry на следующем цикле. `last_sync_at` не двигается — при восстановлении подхватит всё |
| Zerion rate limit (429) | Exponential backoff: 1s → 2s → 4s. Max 3 retries. Если не удалось — sync fail, retry next cycle |
| Zerion `price: null` | `usd_rate = 0` в entries. Транзакция записывается, цена отсутствует |
| Zerion indexing lag (~3-5 мин) | Транзакция появится в следующем sync цикле. Приемлемо для portfolio tracker |
| CoinGecko API недоступен | Circuit breaker → fallback на stale Redis cache (24h). Portfolio показывает устаревшие цены с пометкой |
| Redis недоступен | Fallback на PostgreSQL `price_history`. Увеличенная latency, но данные доступны |
