# Frontend Multi-Chain Wallet Adaptation

## Context

Backend мигрировал на multi-chain EVM кошелёк: один адрес = все сети. `chain_id` убран из `Wallet`, API возвращает `supported_chains: []string`. Фронтенд остался на старой модели с `chain_id: number` в формах, типах и компонентах. Нужно адаптировать UI: упростить форму, добавить иконки сетей, показать chain в транзакциях.

---

## 1. Chain Constants & Types

**Файл**: `apps/frontend/src/types/wallet.ts`

**Удалить**: `SUPPORTED_CHAINS` (numeric array), `ChainId` type, `getChainById()`, `getChainSymbol()`, `LEGACY_CHAIN_NAMES`

**Добавить**: `CHAIN_CONFIG` — единый конфиг сетей по Zerion chain name:

```typescript
export interface ChainConfig {
  id: string           // "ethereum", "base", etc.
  name: string         // "Ethereum", "Base"
  shortName: string    // "ETH", "BASE"
  color: string        // Brand hex: "#627EEA"
  explorerUrl: string  // "https://etherscan.io"
}

export const CHAIN_CONFIG: Record<string, ChainConfig> = {
  ethereum:  { id: 'ethereum',  name: 'Ethereum',       shortName: 'ETH',  color: '#627EEA', explorerUrl: 'https://etherscan.io' },
  polygon:   { id: 'polygon',   name: 'Polygon',        shortName: 'MATIC', color: '#8247E5', explorerUrl: 'https://polygonscan.com' },
  arbitrum:  { id: 'arbitrum',  name: 'Arbitrum One',   shortName: 'ARB',  color: '#28A0F0', explorerUrl: 'https://arbiscan.io' },
  optimism:  { id: 'optimism',  name: 'Optimism',       shortName: 'OP',   color: '#FF0420', explorerUrl: 'https://optimistic.etherscan.io' },
  base:      { id: 'base',      name: 'Base',           shortName: 'BASE', color: '#0052FF', explorerUrl: 'https://basescan.org' },
  avalanche: { id: 'avalanche', name: 'Avalanche',      shortName: 'AVAX', color: '#E84142', explorerUrl: 'https://snowtrace.io' },
  'binance-smart-chain': { id: 'binance-smart-chain', name: 'BNB Smart Chain', shortName: 'BSC', color: '#F0B90B', explorerUrl: 'https://bscscan.com' },
}
```

**Обновить интерфейсы**:
```typescript
export interface Wallet {
  id: string
  name: string
  address: string
  supported_chains?: string[]   // NEW: от бэкенда
  sync_status?: WalletSyncStatus
  last_sync_at?: string
  sync_error?: string
  created_at: string
  updated_at: string
  // REMOVED: chain_id, last_sync_block
}

export interface CreateWalletRequest {
  name: string
  address: string
  // REMOVED: chain_id
}
```

**Хелперы** — оставить `getChainName()` и `isValidEVMAddress()`, переписать на string:
```typescript
export function getChainName(chainId: string): string
export function getChainShortName(chainId: string): string
export function getChainConfig(chainId: string): ChainConfig | undefined
```

---

## 2. Explorer URLs

**Файл**: `apps/frontend/src/lib/format.ts`

- Удалить `EXPLORER_URLS: Record<number, string>`
- Импортировать `CHAIN_CONFIG`
- `getExplorerTxUrl(chainId: string, txHash)` — берёт URL из `CHAIN_CONFIG[chainId]`
- `getExplorerAddressUrl(chainId: string, address)` — аналогично

---

## 3. ChainIcon Component (SVG)

**Новый файл**: `apps/frontend/src/components/domain/ChainIcon.tsx`

Компонент для иконки сети — SVG логотипы каждой сети (ромб Ethereum, щит Arbitrum, и т.д.) внутри цветного круга.

```typescript
interface ChainIconProps {
  chainId: string
  size?: 'xs' | 'sm' | 'default'   // xs=16px, sm=20px, default=24px
  showTooltip?: boolean
  className?: string
}
```

- SVG paths для 7 сетей в `Record<string, ReactNode>`
- Fallback: цветной кружок с первой буквой (из `CHAIN_CONFIG[chainId].color`)
- `showTooltip` — оборачивает в Radix `Tooltip` с полным именем сети
- Белый border ring (`ring-2 ring-background`) когда используется как overlay

---

## 4. AssetIcon + Chain Overlay

**Файл**: `apps/frontend/src/components/domain/AssetIcon.tsx`

Добавить опциональный prop `chainId?: string`. Когда передан — рендерить `ChainIcon` в нижнем правом углу:

```typescript
interface AssetIconProps {
  symbol: string
  imageUrl?: string
  chainId?: string    // NEW
  size?: 'sm' | 'default' | 'lg'
  className?: string
}
```

Реализация: `relative` контейнер + `absolute -bottom-0.5 -right-0.5` для overlay. Размер overlay:
- `sm` asset → `xs` chain
- `default` asset → `xs` chain
- `lg` asset → `sm` chain

---

## 5. Wallet Creation Form

**Файл**: `apps/frontend/src/features/wallets/CreateWalletDialog.tsx`

- **Удалить**: импорт `Select*` компонентов, `SUPPORTED_CHAINS`
- **Удалить**: state `chainId`, валидацию chain, `chain_id: Number(chainId)` из мутации
- **Удалить**: JSX блок Select для выбора сети (строки 120-137)
- **Обновить**: описание → "Add an EVM wallet address. Assets across all supported chains will be tracked automatically."
- **Обновить**: placeholder → "My Wallet" вместо "My Ethereum Wallet"
- **Результат**: форма из 2 полей (Name + Address) вместо 3

---

## 6. WalletCard

**Файл**: `apps/frontend/src/components/domain/WalletCard.tsx`

- **Удалить**: `chainLabels` record целиком
- **Заменить** текстовый бейдж (ETH/BASE) на lucide `Wallet` иконку в цветном квадрате `bg-primary/10`
- **Добавить** (опционально): ряд маленьких `ChainIcon` под адресом если `wallet.supported_chains` есть:
  ```
  [E] [P] [A] [O] [B] +2
  ```
- Применить к обоим вариантам: `WalletCard` и `WalletCardCompact`

---

## 7. WalletDetailPage

**Файл**: `apps/frontend/src/features/wallets/WalletDetailPage.tsx`

- **Удалить**: `getChainName`, `getChainSymbol` импорты и использование
- **Заменить** бейдж на lucide `Wallet` иконку (12x12)
- **Заменить** `{chainName}` на:
  ```
  Multi-chain EVM  [E] [P] [A] [O] [B] [AV] [BSC]
  ```
- **Удалить**: `last_sync_block` (бэкенд больше не возвращает)

---

## 8. Backend: chain_id в Transaction List

**Цель**: добавить `chain_id` в ответ списка транзакций.

**8a.** `apps/backend/internal/module/transactions/reader.go`
- Добавить `ChainID string` в struct `ListFields`
- В каждом ReadForList (`TransferInReader`, `TransferOutReader`, `InternalTransferReader`): установить `fields.ChainID = transfer.ChainID`
- `AdjustmentReader`: оставить пустым (adjustments не привязаны к сети)

**8b.** `apps/backend/internal/module/transactions/dto.go`
- Добавить `ChainID string` в `TransactionListItem`

**8c.** `apps/backend/internal/module/transactions/service.go`
- В `toListItem()`: маппинг `fields.ChainID` → `item.ChainID`

**8d.** `apps/backend/internal/transport/httpapi/handler/transaction.go`
- Добавить `ChainID string \`json:"chain_id,omitempty"\`` в `TransactionListItemResponse`
- Маппинг из DTO в response

---

## 9. Transaction Lists (Frontend)

**9a.** `apps/frontend/src/types/transaction.ts`
- Добавить `chain_id?: string` в `TransactionListItem` и `TransactionDetail`

**9b.** `apps/frontend/src/features/transactions/TransactionsPage.tsx`
- Добавить колонку "Network" между "Type" и "Wallet"
- Ячейка: `ChainIcon` (xs) + `getChainShortName()` текст, или "-" если нет chain_id

**9c.** `apps/frontend/src/features/wallets/WalletTransactions.tsx`
- Аналогичная колонка "Network"

**9d.** `apps/frontend/src/features/transactions/TransactionDetailPage.tsx`
- Поле "Network" с `ChainIcon` (sm) + полное имя сети

**9e.** `apps/frontend/src/features/dashboard/RecentTransactions.tsx`
- Маленькая `ChainIcon` рядом с именем кошелька

---

## 10. Portfolio Types

**Файл**: `apps/frontend/src/types/portfolio.ts`
- Удалить `chain_id: string` из `WalletBalance` (бэкенд больше не возвращает)

---

## Порядок реализации

```
Wave 1 (параллельно, без зависимостей):
├── [1] Chain constants + types (wallet.ts)
├── [8] Backend: chain_id в transaction list
└── [10] Portfolio types cleanup

Wave 2 (зависит от Wave 1):
├── [2] Explorer URLs (format.ts) ← зависит от [1]
├── [3] ChainIcon component (новый файл) ← зависит от [1]
└── [5] Wallet creation form ← зависит от [1]

Wave 3 (зависит от Wave 2):
├── [4] AssetIcon overlay ← зависит от [3]
├── [6] WalletCard ← зависит от [1], [3]
├── [7] WalletDetailPage ← зависит от [1], [3]
└── [9] Transaction lists ← зависит от [1], [3], [8]

Wave 4:
└── Сборка, тесты, проверка
```

---

## Файлы для изменения

### Backend (4 файла)
- `apps/backend/internal/module/transactions/reader.go` — ListFields + ChainID
- `apps/backend/internal/module/transactions/dto.go` — TransactionListItem + ChainID
- `apps/backend/internal/module/transactions/service.go` — toListItem маппинг
- `apps/backend/internal/transport/httpapi/handler/transaction.go` — Response DTO

### Frontend (13 файлов)
- `src/types/wallet.ts` — CHAIN_CONFIG, Wallet interface, helpers
- `src/types/transaction.ts` — chain_id field
- `src/types/portfolio.ts` — remove chain_id
- `src/lib/format.ts` — explorer URLs
- `src/components/domain/ChainIcon.tsx` — **NEW**: SVG chain icons
- `src/components/domain/AssetIcon.tsx` — chain overlay
- `src/components/domain/WalletCard.tsx` — wallet icon, chain row
- `src/features/wallets/CreateWalletDialog.tsx` — remove chain selector
- `src/features/wallets/WalletDetailPage.tsx` — multi-chain display
- `src/features/wallets/WalletAssets.tsx` — asset icon with chain
- `src/features/wallets/WalletTransactions.tsx` — network column
- `src/features/transactions/TransactionsPage.tsx` — network column
- `src/features/transactions/TransactionDetailPage.tsx` — network field

---

## Верификация

1. `cd apps/backend && go build ./...` — компилируется
2. `cd apps/backend && go test ./...` — тесты проходят
3. `cd apps/frontend && npx tsc --noEmit` — TypeScript без ошибок
4. `cd apps/frontend && bun run build` — билд проходит
5. Визуально: форма кошелька — 2 поля (Name + Address), без chain selector
6. Визуально: карточки кошельков — иконка Wallet + ряд chain icons
7. Визуально: таблица транзакций — колонка Network с SVG иконками сетей
8. Визуально: таблица ассетов — иконки ассетов с overlay сети
9. API: `POST /wallets` без chain_id → 201 Created
10. API: `GET /transactions` возвращает `chain_id` в каждой транзакции
