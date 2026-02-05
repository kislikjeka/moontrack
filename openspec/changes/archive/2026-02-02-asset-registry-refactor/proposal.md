## Why

Логика работы с ассетами размазана между модулями `assets`, `pricing`, и `manual_transaction`. AssetID интерпретируется по-разному (символ vs CoinGecko ID), маппинги хранятся частично в hardcode, частично в Redis, отсутствует персистентное хранилище для справочника ассетов и исторических цен. Это создаёт сложности при добавлении новых источников данных и делает невозможным анализ исторической доходности портфеля.

## What Changes

- **Единый Asset Registry** — централизованный справочник ассетов в PostgreSQL с маппингом symbol → CoinGecko ID → внутренний ID
- **Price History в TimescaleDB** — персистентное хранение исторических цен с автоматическим retention и агрегацией
- **Unified Asset Service** — единая точка входа для всех операций с ассетами: поиск, резолюция символов, получение цен (текущих и исторических)
- **Миграция существующих модулей** — `assets`, `pricing`, `manual_transaction`, `portfolio` будут использовать новый Asset Registry вместо прямых обращений к CoinGecko/Redis
- **Removal of duplication** — удаление hardcoded `nativeDecimals`, дублирующих структур `Asset`/`AssetHolding`, разрозненных кэшей
- **BREAKING**: Изменение структуры `AssetID` в API — вместо символа будет использоваться внутренний UUID

## Capabilities

### New Capabilities
- `asset-registry`: Централизованный справочник ассетов в PostgreSQL — хранение метаданных (symbol, name, decimals, coingecko_id), единый источник правды для всех модулей
- `price-history`: Персистентное хранение исторических цен в TimescaleDB — OHLCV данные, автоматическая агрегация (1h → 1d → 1w), retention policies, запросы по временным диапазонам

### Modified Capabilities
- `asset-search`: Поиск будет обращаться к локальному Asset Registry с fallback на CoinGecko для новых ассетов
- `asset-price-lookup`: Цены будут сначала искаться в Price History, затем в кэше, затем в API

## Impact

**Backend:**
- Новые таблицы: `assets`, `price_history` (TimescaleDB hypertable)
- Новый модуль: `internal/core/asset_registry/`
- Рефакторинг: `internal/core/pricing/`, `internal/modules/assets/`, `internal/modules/manual_transaction/`
- Удаление: дублирующий код в `asset_service.go` (nativeDecimals, symbol mapping)

**Database:**
- Требуется TimescaleDB extension для PostgreSQL
- Миграции для создания таблиц и hypertable
- Возможная миграция существующих данных из Redis в PostgreSQL

**API:**
- **BREAKING**: `asset_id` в запросах/ответах меняется с символа на UUID
- Новые эндпоинты: `GET /assets/{id}/history?from=&to=&interval=`
- Обратная совместимость: временная поддержка символов с deprecation warning

**Frontend:**
- Обновление типов Asset в TypeScript
- Адаптация компонентов под новый формат asset_id

**Infrastructure:**
- TimescaleDB extension в PostgreSQL container
- Background job для периодического обновления цен
