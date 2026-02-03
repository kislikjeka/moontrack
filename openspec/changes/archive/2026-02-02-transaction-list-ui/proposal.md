## Why

Текущий список транзакций отображает сырые данные леджера (raw_data), что непрозрачно для пользователя. Нужен понятный интерфейс: список транзакций с типом и ключевыми параметрами, и отдельная страница детализации с ledger entries.

## What Changes

- **Новый transaction service** (`internal/modules/transactions/`) — отдельный сервис для работы с транзакциями, агрегирует разные типы, форматирует для UI. Ledger остаётся низкоуровневым (только entries и балансы)
- **Новая страница детализации транзакции** (`/transactions/:id`) — показывает полные параметры транзакции и список ledger entries (debit/credit)
- **Улучшенный список транзакций** — отображает тип, актив, сумму, дату вместо сырого raw_data
- **Новый API endpoint** `GET /transactions/:id` — возвращает транзакцию с entries
- **Расширенный response формат** — человекочитаемые поля (asset, amount, wallet_name) в список и entries в детализацию

## Capabilities

### New Capabilities
- `transaction-service`: Сервис для работы с транзакциями — агрегация типов, форматирование для API, загрузка с entries
- `transaction-detail-view`: Страница детализации одной транзакции с ledger entries и всеми параметрами

### Modified Capabilities
- (нет существующих спек для модификации)

## Impact

**Backend:**
- `apps/backend/internal/modules/transactions/` — **новый модуль** transaction service
- `apps/backend/internal/api/handlers/transaction_handler.go` — рефакторинг на использование нового сервиса
- `apps/backend/internal/api/router/router.go` — новый route `GET /transactions/:id`

**Frontend:**
- `apps/frontend/src/features/transactions/TransactionList.tsx` — рефакторинг отображения
- `apps/frontend/src/features/transactions/TransactionDetail.tsx` — новая страница
- `apps/frontend/src/services/transaction.ts` — расширение интерфейсов
- `apps/frontend/src/App.jsx` — новый route

**API:**
- Новый endpoint: `GET /transactions/:id`
- Расширение response `GET /transactions` — добавить wallet_name, display_amount, asset_symbol
