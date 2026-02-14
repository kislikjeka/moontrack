# PRD: Blockchain Data Integration

## Status: Draft
## Author: AI-assisted
## Date: 2026-02-07

---

## 1. Problem Statement

MoonTrack — crypto portfolio tracker с double-entry accounting. Текущая интеграция с Alchemy покрывает базовые переводы (ETH, ERC-20), но имеет критические ограничения:

1. **Internal transfers не работают на L2** — Alchemy `alchemy_getAssetTransfers` поддерживает категорию `internal` только на Ethereum и Polygon. На Base, Arbitrum, Optimism — ошибка API. Это означает потерю данных о переводах ETH через контракты (DeFi withdrawals, мультисиги, WETH unwrap).

2. **Нет DeFi контекста** — система видит "отправил 500 USDC на `0x47c0...`", но не понимает что это "открыл позицию на GMX". Без decoded context невозможно:
   - Отличить swap от обычного перевода
   - Отслеживать LP позиции (Uniswap V3)
   - Считать PnL по DeFi операциям
   - Показать пользователю понятную историю

3. **Нет поддержки DeFi протоколов** — планируется работа с Uniswap pools, GMX pools. Текущая архитектура не имеет handlers для swap, LP deposit/withdraw, perp position open/close.

---

## 2. User Needs (выявлены в процессе исследования)

### Приоритеты пользователя:
- Исторические USD-значения на момент транзакции (P&L, налоги)
- GMX: отслеживание покупки/продажи GM токенов, расчёт PnL
- LP Pools: полный функционал (позиции + история + rewards)
- Pet-проект → друзья/знакомые (личное использование)
- Начать бесплатно, готов платить при упирании в лимиты
- Только EVM сейчас, Solana потом
- Готов к комбинации API

### Целевые сети:
- Ethereum Mainnet
- Arbitrum One
- Base
- Optimism (future)

### Целевые протоколы (Phase 1):
- Uniswap V3 (swaps, LP positions)
- GMX V2 (GM tokens, perp positions)

### Целевые протоколы (Phase 2+):
- Aave (lending/borrowing)
- Lido (staking)
- Другие по мере необходимости

---

## 3. Requirements

### 3.1 Functional Requirements

#### FR-1: Полная история транзакций
Система должна собирать все типы активности кошелька:
- Native token transfers (external) — ETH отправка/получение
- ERC-20 token transfers — USDC, WETH и т.д.
- Internal transfers — ETH через контракты (на всех поддерживаемых сетях)
- DeFi операции — swaps, LP operations, perp positions

#### FR-2: Decoded DeFi контекст
Каждая DeFi транзакция должна быть размечена:
- Тип операции: `trade`, `deposit`, `withdraw`, `claim`, `approve`
- Протокол: Uniswap V3, GMX V2, Aave и т.д.
- Входящие и исходящие активы с точными суммами
- USD value на момент транзакции

#### FR-3: DeFi позиции
Система должна отслеживать текущие DeFi позиции:
- LP позиции (пул, доля, underlying tokens)
- Staked позиции
- Lending/borrowing позиции
- GMX GM token позиции

#### FR-4: PnL расчёт
Для каждой DeFi операции система должна:
- Записывать USD стоимость на момент транзакции
- Поддерживать расчёт realized PnL (по закрытым позициям)

#### FR-5: Double-entry consistency
Все операции должны создавать сбалансированные ledger entries:
- SUM(DEBIT) = SUM(CREDIT) для каждой транзакции
- Новые типы операций (swap, LP) — через handler registry без изменения ядра

#### FR-6: Idempotency
- Дедупликация по `(source, external_id)` — Zerion `transaction_id` как external_id
- Одна blockchain транзакция → одна ledger транзакция (не дублировать)

### 3.2 Non-Functional Requirements

#### NFR-1: Стоимость
- Free tier провайдеров должно хватать для 5-10 кошельков
- Escalation path при росте (платные планы)

#### NFR-2: Точность финансовых данных
- Суммы хранятся как `NUMERIC(78,0)` / `math/big.Int` — никогда float
- USD rates масштабированы на 10^8
- Данные из Zerion `quantity.int` (строка) → `big.Int` напрямую

#### NFR-3: Расширяемость
- Новые протоколы добавляются через handler registry
- Новые сети добавляются через `chains.yaml`
- Новые провайдеры добавляются как adapters за port interfaces

#### NFR-4: Graceful degradation
- Если Zerion недоступен — sync fail, retry на следующем цикле. `last_sync_at` не обновляется — при восстановлении подхватит все пропущенные транзакции (см. ADR-001 Section 9)
- Если USD rate недоступен — записывается 0, обновляется позже через CoinGecko
- Если провайдер возвращает неполные данные — логируется, не крашится

---

## 4. Data Sources Architecture

### Текущее состояние:
```
Alchemy → Raw transfers (external, erc20, internal на ETH/Polygon)
CoinGecko → Текущие цены (через Redis cache)
```

### Целевое состояние (см. [ADR-001](../adr/001-alchemy-zerion-integration.md)):
```
Zerion     → Single transaction data source: all transfers + decoded DeFi, positions, historical USD
Alchemy    → RPC only: eth_blockNumber, eth_call, eth_subscribe, ENS (NOT used for transaction data)
CoinGecko  → Текущие цены для portfolio valuation (keep)
```

### Опционально (Phase 2+):
```
Etherscan V2 → Internal transfers на L2 (если Zerion не покрывает)
```

---

## 5. Provider Selection Rationale

### Почему Alchemy (keep as RPC):
- Лучший RPC-провайдер: 30M CU/мес free tier
- Нулевые data inconsistencies в бенчмарках
- WebSocket для real-time notifications (future)
- Уже интегрирован
- **Роль после ADR-001**: только RPC (eth_blockNumber, eth_call, eth_subscribe, ENS). `alchemy_getAssetTransfers` больше не используется для sync (заменён на Zerion).

### Почему Zerion (add):
- 8,000+ decoded DeFi протоколов
- GMX на Arbitrum явно поддержан
- `operation_type` enum чётко маппится на ledger handlers
- `quantity.int` строка → `big.Int` без потери точности
- Исторические USD на момент транзакции
- PnL из коробки
- Free tier: 2,000 req/day (достаточно для 5-10 кошельков)
- Используется Uniswap и Kraken — проверен на масштабе

### Почему НЕ Moralis:
- 819 data inconsistencies в сутки в бенчмарках — неприемлемо для финансового приложения

### Почему НЕ только Etherscan:
- Нет decoded DeFi контекста
- 5 отдельных вызовов на кошелёк
- Rate limit 5 req/sec

### Почему НЕ custom indexing (Ponder/Envio):
- GMX V2 EventEmitter паттерн — 5-10 дней только на парсер
- Overkill для pet-проекта
- Zerion покрывает 95% задач из коробки

---

## 6. Phasing

### Phase 0: Hotfix (immediate)
- Исключить `internal` категорию на L2 в Alchemy client
- Без этого фикса кошельки на Base/Arbitrum не синкаются

### Phase 1: Zerion Foundation (основная работа)
- Zerion client в `internal/infra/gateway/zerion/`
- Replace Alchemy sync pipeline with Zerion single-source (см. ADR-001)
- Маппинг Zerion `operation_type` → ledger transaction types
- Classification logic (receive/send → existing handlers, trade/deposit/withdraw/claim → new)
- DB migration: `wallets.last_sync_at`, clearing account types

### Phase 1.5: DeFi Handlers
- Новые ledger handlers: swap (с clearing account), LP deposit/withdraw, claim
- DeFi позиции endpoint

### Phase 2: Advanced DeFi
- GMX perp positions tracking
- Aave lending/borrowing
- Staking rewards (Lido)
- Etherscan V2 для internal transfers на L2 (если нужно)

### Phase 3: Beyond EVM
- Solana поддержка (Zerion + Alchemy оба поддерживают)

---

## 7. Success Metrics

- Все кошельки на Base/Arbitrum синкаются без ошибок (Phase 0)
- Uniswap swaps отображаются как "Trade" с in/out активами (Phase 1)
- LP позиции видны в portfolio с underlying tokens (Phase 1)
- GMX GM token операции записаны в ledger с корректным PnL (Phase 1-2)
- Все ledger транзакции сбалансированы (SUM debit = SUM credit) (always)
- Нет дублей транзакций — idempotency через `UNIQUE(source, external_id)` (always)
