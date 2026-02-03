## Context

Текущая архитектура работы с ассетами:

- **Asset Module** (`internal/modules/assets/`) — поиск через CoinGecko API, кэширование в Redis, маппинг symbol→CoinGecko ID
- **Pricing Module** (`internal/core/pricing/`) — получение цен через CoinGecko, Redis cache (60s + 24h stale), circuit breaker
- **Manual Transaction** — использует `AssetID` (символ) + `PriceAssetID` (CoinGecko ID) для резолюции цен
- **Ledger/Portfolio** — оперирует символами (BTC, ETH), получает цены через PriceService

Проблемы:
1. AssetID неоднозначен — символ в ledger, CoinGecko ID в pricing
2. Маппинги дублируются (hardcoded `nativeDecimals` + Redis `asset:mapping:*`)
3. Нет персистентного хранения справочника и исторических цен
4. Невозможен анализ исторической доходности

## Goals / Non-Goals

**Goals:**
- Единый источник правды для ассетов в PostgreSQL
- Персистентное хранение исторических цен с эффективными запросами по времени
- Унифицированный AssetID (UUID) во всех модулях
- Сохранение multi-layer fallback для цен (Cache → DB -> API)
- Минимизация breaking changes для существующих клиентов

**Non-Goals:**
- Поддержка множественных источников цен (Binance, Kraken) — только CoinGecko
- Real-time price streaming через WebSocket
- Хранение on-chain данных (balances, transactions)
- Миграция исторических данных из внешних источников

## Decisions

### 1. Asset Registry в PostgreSQL (не отдельный сервис)

**Решение:** Хранить справочник ассетов в PostgreSQL как часть монолита.

**Альтернативы:**
- Отдельный микросервис — overhead для текущего масштаба
- Redis как primary storage — нет ACID, сложные запросы

**Обоснование:** Монолит уже использует PostgreSQL, добавление таблицы проще чем новый сервис. При росте можно вынести.

### 2. TimescaleDB для Price History

**Решение:** Использовать TimescaleDB extension вместо обычной PostgreSQL таблицы.

**Альтернативы:**
- Обычная таблица с индексами — деградация при росте данных
- InfluxDB/ClickHouse — дополнительная инфраструктура

**Обоснование:** TimescaleDB:
- Нативная интеграция с PostgreSQL (тот же connection pool)
- Автоматическое партиционирование по времени (chunks)
- Continuous aggregates для агрегации 1h→1d→1w
- Compression policies для старых данных
- Retention policies для автоочистки

### 3. Бизнес-ключ: symbol + chain_id

**Решение:** Уникальность ассета определяется комбинацией `(symbol, chain_id)`. UUID используется как внутренний технический ID.

**Альтернативы:**
- CoinGecko ID как бизнес-ключ — vendor lock-in
- Только symbol — неуникален (USDC на разных сетях)
- UUID без бизнес-ключа — нет связи с реальным миром

**Обоснование:**
- `symbol + chain` отражает деление на уровне реального мира
- Независимость от провайдеров данных (CoinGecko, CMC, Binance)
- UUID v7 для внутренних операций (foreign keys, API responses)

### 4. Multi-chain assets — отдельные записи

**Решение:** Хранить каждый деплоймент токена на разных сетях как отдельную запись в таблице `assets`.

**Альтернативы:**
- Единый asset с `chains` JSONB массивом — одна цена на все сети, не отражает depeg
- Иерархия base_asset + deployments — over-engineering для текущего scope
- Chain только в wallet/account — негде хранить contract_address централизованно

**Обоснование:**
- CoinGecko уже так делает (`usd-coin`, `usd-coin-solana`, `bridged-usdc-polygon`)
- Depeg protection — UST показал, что stablecoin может иметь разные цены на разных сетях
- Простая модель — один asset = один контракт
- Агрегация "весь USDC" решается на уровне UI группировкой по `symbol`

### 5. Структура таблицы assets

```sql
CREATE TABLE assets (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    symbol            VARCHAR(20) NOT NULL,           -- BTC, ETH, USDC
    name              VARCHAR(255) NOT NULL,          -- Bitcoin, Ethereum, USD Coin
    coingecko_id      VARCHAR(100) NOT NULL,          -- bitcoin, usd-coin-solana (обязателен на текущем этапе)
    decimals          SMALLINT NOT NULL DEFAULT 8,    -- 8 for BTC, 18 for ETH, 6 for USDC
    asset_type        VARCHAR(20) NOT NULL DEFAULT 'crypto',  -- crypto, fiat, custom
    chain_id          VARCHAR(20),                    -- ethereum, solana, polygon (NULL for native L1)
    contract_address  VARCHAR(100),                   -- 0xA0b86991c6218... (NULL for native)
    market_cap_rank   INTEGER,
    is_active         BOOLEAN NOT NULL DEFAULT true,
    metadata          JSONB DEFAULT '{}',             -- additional data
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT assets_coingecko_unique UNIQUE (coingecko_id),
    CONSTRAINT assets_symbol_chain_unique UNIQUE (symbol, chain_id)
);

CREATE INDEX idx_assets_symbol ON assets(symbol);
CREATE INDEX idx_assets_coingecko_id ON assets(coingecko_id);
CREATE INDEX idx_assets_chain ON assets(chain_id) WHERE chain_id IS NOT NULL;
CREATE INDEX idx_assets_active ON assets(is_active) WHERE is_active = true;
```

**Примечание:** `coingecko_id` обязателен на текущем этапе. В будущем при добавлении других провайдеров можно сделать nullable и добавить таблицу `asset_providers` для маппинга на разные источники.

**Примеры данных:**
```
| symbol | name      | chain_id  | coingecko_id        | contract_address        |
|--------|-----------|-----------|---------------------|-------------------------|
| BTC    | Bitcoin   | NULL      | bitcoin             | NULL                    |
| ETH    | Ethereum  | NULL      | ethereum            | NULL                    |
| USDC   | USD Coin  | ethereum  | usd-coin            | 0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48 |
| USDC   | USD Coin  | solana    | usd-coin-solana     | EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v |
| USDC   | USD Coin  | polygon   | bridged-usdc-polygon| 0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174 |
```

### 6. Структура таблицы price_history (TimescaleDB)

```sql
CREATE TABLE price_history (
    time            TIMESTAMPTZ NOT NULL,
    asset_id        UUID NOT NULL REFERENCES assets(id),
    price_usd       NUMERIC(78,0) NOT NULL,         -- scaled by 10^8
    volume_24h      NUMERIC(78,0),                  -- optional
    market_cap      NUMERIC(78,0),                  -- optional
    source          VARCHAR(20) NOT NULL DEFAULT 'coingecko',

    PRIMARY KEY (asset_id, time)
);

-- Convert to hypertable (7-day chunks)
SELECT create_hypertable('price_history', 'time', chunk_time_interval => INTERVAL '7 days');

-- Compression policy (compress chunks older than 30 days)
ALTER TABLE price_history SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'asset_id'
);
SELECT add_compression_policy('price_history', INTERVAL '30 days');

-- Retention policy (drop data older than 2 years)
SELECT add_retention_policy('price_history', INTERVAL '2 years');

-- Continuous aggregate for daily prices
CREATE MATERIALIZED VIEW price_history_daily
WITH (timescaledb.continuous) AS
SELECT
    asset_id,
    time_bucket('1 day', time) AS day,
    first(price_usd, time) AS open,
    max(price_usd) AS high,
    min(price_usd) AS low,
    last(price_usd, time) AS close,
    avg(volume_24h) AS avg_volume
FROM price_history
GROUP BY asset_id, time_bucket('1 day', time);

SELECT add_continuous_aggregate_policy('price_history_daily',
    start_offset => INTERVAL '3 days',
    end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '1 hour');
```

### 7. Архитектура модулей

```
internal/core/asset_registry/
├── domain/
│   ├── asset.go           # Asset entity
│   ├── price.go           # PricePoint, PriceHistory
│   └── errors.go
├── repository/
│   ├── asset_repository.go      # CRUD for assets
│   └── price_repository.go      # Price history queries
├── service/
│   └── registry_service.go      # Business logic
└── api/
    └── handler.go               # HTTP handlers
```

**RegistryService interface:**
```go
type RegistryService interface {
    // Asset operations
    GetAsset(ctx context.Context, id uuid.UUID) (*Asset, error)
    GetAssetBySymbol(ctx context.Context, symbol string, chainID *string) (*Asset, error)  // chainID nil = all chains
    GetAssetsBySymbol(ctx context.Context, symbol string) ([]Asset, error)  // returns all chains for symbol
    GetAssetByCoinGeckoID(ctx context.Context, cgID string) (*Asset, error)
    SearchAssets(ctx context.Context, query string) ([]Asset, error)  // returns with chain labels
    CreateAsset(ctx context.Context, asset *Asset) error

    // Price operations
    GetCurrentPrice(ctx context.Context, assetID uuid.UUID) (*PricePoint, error)
    GetPriceAt(ctx context.Context, assetID uuid.UUID, at time.Time) (*PricePoint, error)
    GetPriceHistory(ctx context.Context, assetID uuid.UUID, from, to time.Time, interval string) ([]PricePoint, error)
    RecordPrice(ctx context.Context, assetID uuid.UUID, price *big.Int, source string) error
}
```

**Резолюция символов при неоднозначности:**
- `GetAssetBySymbol("USDC", nil)` — возвращает ошибку `ErrAmbiguousSymbol` со списком вариантов
- `GetAssetBySymbol("USDC", "ethereum")` — возвращает конкретный asset
- `GetAssetsBySymbol("USDC")` — возвращает все варианты (USDC на всех сетях)
- `SearchAssets("USDC")` — возвращает все варианты с пометкой chain в UI

### 8. Стратегия получения цен (multi-layer)

```
GetCurrentPrice(assetID):
1. Redis cache (TTL 60s) → return if found
2. price_history (last 5 min) → return if found, update cache
3. CoinGecko API → return, save to price_history + cache
4. Redis stale cache (TTL 24h) → return with warning
5. Error: price unavailable
```

### 9. Background Price Updater

```go
type PriceUpdater struct {
    registry    RegistryService
    coingecko   *coingecko.Client
    interval    time.Duration  // 5 minutes
    batchSize   int           // 50 assets per request
}

func (u *PriceUpdater) Run(ctx context.Context) {
    ticker := time.NewTicker(u.interval)
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            u.updatePrices(ctx)
        }
    }
}
```

### 10. Синхронизация справочника ассетов

**Решение:** Готовый seed-список основных монет при запуске. Без автоматической синхронизации с CoinGecko.

**Seed list (~50 assets):**
- Top 20 по market cap (BTC, ETH, USDT, BNB, SOL, XRP, USDC, ADA, DOGE, TRX, etc.)
- Основные stablecoins на разных сетях (USDC/USDT на Ethereum, Solana, Polygon)
- Популярные DeFi токены (UNI, AAVE, LINK, etc.)

**Добавление новых ассетов:**
- Через API/admin интерфейс
- Вручную в миграциях
- В будущем: по запросу пользователя с модерацией

### 11. Миграция данных

**Решение:** Чистая миграция. Старые данные можно дропнуть (активная стадия разработки).

**Этапы:**
1. Добавить TimescaleDB extension
2. Создать таблицы `assets` и `price_history`
3. Запустить seed для начального списка ассетов
4. Обновить ledger entries: добавить `asset_uuid` колонку, заполнить через lookup по symbol
5. Обновить API handlers на работу с UUID
6. Обновить frontend
7. Удалить legacy код (hardcoded decimals, Redis symbol mappings)

**Очистка перед миграцией:**
```sql
-- Опционально: дропнуть старые тестовые данные
TRUNCATE entries, transactions, account_balances CASCADE;
```

## Risks / Trade-offs

**[TimescaleDB dependency]** → Mitigation: TimescaleDB is a PostgreSQL extension, not a separate service. Fallback to regular table with manual partitioning if needed.

**[CoinGecko vendor dependency]** → Mitigation: coingecko_id обязателен сейчас, но бизнес-ключ `symbol + chain` независим. При необходимости можно добавить другие провайдеры без изменения структуры.

**[CoinGecko rate limits]** → Mitigation: Batch requests (50 assets/request), 5-minute update interval, circuit breaker, stale cache fallback.

**[No price backfill]** → Mitigation: Исторические данные будут накапливаться постепенно. При необходимости можно backfill через CoinGecko API позже.

## Open Questions

Все вопросы решены.
