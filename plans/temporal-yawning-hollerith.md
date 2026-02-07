# Frontend: Blockchain Sync Support

## Цель
Обновить фронтенд для поддержки blockchain sync функционала из плана `noble-weaving-pumpkin.md`:
- Wallet становится EVM-only с обязательным адресом
- Новые типы транзакций (transfer_in, transfer_out, internal_transfer)
- UI для отображения sync статуса кошелька
- Упрощение формы создания транзакций (только asset_adjustment)

---

## Изменения

### 1. Types (`apps/frontend/src/types/`)

#### 1.1. `wallet.ts` - Обновить типы кошелька

```typescript
// Новый тип sync статуса
export type WalletSyncStatus = 'pending' | 'syncing' | 'synced' | 'error'

export interface Wallet {
  id: string
  name: string
  chain_id: number              // ИЗМЕНЕНО: number вместо string
  address: string               // ИЗМЕНЕНО: обязателен
  sync_status: WalletSyncStatus // НОВОЕ
  last_sync_block?: number      // НОВОЕ
  last_sync_at?: string         // НОВОЕ
  sync_error?: string           // НОВОЕ
  created_at: string
  updated_at: string
}

export interface CreateWalletRequest {
  name: string
  chain_id: number              // ИЗМЕНЕНО: number
  address: string               // ИЗМЕНЕНО: обязателен
}

export interface UpdateWalletRequest {
  name?: string
  // chain_id и address НЕ редактируются
}

// Только EVM сети с числовыми ID
export const SUPPORTED_CHAINS = [
  { id: 1, name: 'Ethereum', symbol: 'ETH' },
  { id: 137, name: 'Polygon', symbol: 'MATIC' },
  { id: 42161, name: 'Arbitrum', symbol: 'ETH' },
  { id: 10, name: 'Optimism', symbol: 'ETH' },
  { id: 8453, name: 'Base', symbol: 'ETH' },
] as const

export type ChainId = (typeof SUPPORTED_CHAINS)[number]['id']

// Helpers
export function getChainById(chainId: number) {
  return SUPPORTED_CHAINS.find(c => c.id === chainId)
}

export function isValidEVMAddress(address: string): boolean {
  return /^0x[a-fA-F0-9]{40}$/.test(address)
}
```

#### 1.2. `transaction.ts` - Обновить типы транзакций

```typescript
// Новые типы (заменяют manual_income/manual_outcome)
export type TransactionType =
  | 'transfer_in'        // Входящий перевод (от sync)
  | 'transfer_out'       // Исходящий перевод (от sync)
  | 'internal_transfer'  // Между своими кошельками
  | 'asset_adjustment'   // Ручная корректировка

export type TransactionDirection = 'in' | 'out' | 'adjustment' | 'internal'

// CreateTransactionRequest - только для adjustment
export interface CreateTransactionRequest {
  type: 'asset_adjustment'
  wallet_id: string
  asset_id: string
  coingecko_id?: string
  amount: string
  new_balance: string
  usd_rate?: string
  occurred_at?: string
  notes?: string
}
```

---

### 2. Services & Hooks

#### 2.1. `services/wallet.ts` - Добавить triggerSync

```typescript
export async function triggerWalletSync(walletId: string): Promise<void> {
  await api.post(`/wallets/${walletId}/sync`)
}
```

#### 2.2. `hooks/useWallets.ts` - Добавить useTriggerSync

```typescript
export function useTriggerSync() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (walletId: string) => triggerWalletSync(walletId),
    onSuccess: (_data, walletId) => {
      queryClient.invalidateQueries({ queryKey: ['wallets'] })
      queryClient.invalidateQueries({ queryKey: ['wallets', walletId] })
    },
  })
}
```

---

### 3. Domain Components

#### 3.1. НОВЫЙ: `components/domain/SyncStatusBadge.tsx`

Badge для отображения sync статуса:
- `pending` - Clock icon, outline variant
- `syncing` - Loader2 icon с анимацией, secondary variant
- `synced` - CheckCircle icon, default variant
- `error` - AlertCircle icon, destructive variant

#### 3.2. `components/domain/TransactionTypeBadge.tsx` - Добавить новые типы

```typescript
const typeConfig = {
  transfer_in: { label: 'Transfer In', icon: ArrowDownLeft, variant: 'profit' },
  transfer_out: { label: 'Transfer Out', icon: ArrowUpRight, variant: 'loss' },
  internal_transfer: { label: 'Internal', icon: ArrowLeftRight, variant: 'transfer' },
  asset_adjustment: { label: 'Adjustment', icon: RefreshCw, variant: 'transfer' },
}
```

#### 3.3. `components/domain/WalletCard.tsx` - Обновить

- chainIcons с числовыми ключами (1, 137, 42161, 10, 8453)
- Добавить SyncStatusBadge
- Показывать last_sync_at

---

### 4. Wallet Features

#### 4.1. `features/wallets/CreateWalletDialog.tsx` - Обязательный address

- Address field обязателен
- Валидация EVM адреса (0x + 40 hex символов)
- chain_id передаётся как number
- Error state для невалидного адреса
- Подсказка: "Transactions will be synced automatically"

#### 4.2. `features/wallets/WalletDetailPage.tsx` - Sync UI

- chainNames с числовыми ключами
- SyncStatusBadge в header
- Кнопка "Sync Now" (RefreshCw icon)
- Alert для sync_error
- Показывать last_sync_at и last_sync_block
- УБРАТЬ кнопку "Add Transaction"

---

### 5. Transaction Features

#### 5.1. `features/transactions/TransactionFilters.tsx` - Новые типы

```typescript
const transactionTypes = [
  { value: 'transfer_in', label: 'Transfer In' },
  { value: 'transfer_out', label: 'Transfer Out' },
  { value: 'internal_transfer', label: 'Internal Transfer' },
  { value: 'asset_adjustment', label: 'Adjustment' },
]
```

#### 5.2. `features/transactions/TransactionFormPage.tsx` - Упростить

- Убрать выбор типа (только asset_adjustment)
- Обновить заголовок: "Balance Adjustment"
- Обновить описание: объяснить что транзакции синхронизируются автоматически

#### 5.3. `features/transactions/TransactionsPage.tsx` - UI изменения

- Кнопка "Balance Adjustment" вместо "New Transaction"
- Обновить empty state

---

### 6. Utilities

#### 6.1. `lib/format.ts` - Добавить explorer URLs

```typescript
export function getExplorerTxUrl(chainId: number, txHash: string): string
export function getExplorerAddressUrl(chainId: number, address: string): string
```

---

## Файлы для изменения

| Файл | Действие |
|------|----------|
| `src/types/wallet.ts` | Обновить: chain_id→number, address required, sync поля |
| `src/types/transaction.ts` | Обновить: новые типы транзакций |
| `src/services/wallet.ts` | Добавить: triggerWalletSync |
| `src/hooks/useWallets.ts` | Добавить: useTriggerSync |
| `src/components/domain/SyncStatusBadge.tsx` | СОЗДАТЬ |
| `src/components/domain/TransactionTypeBadge.tsx` | Обновить: новые типы |
| `src/components/domain/WalletCard.tsx` | Обновить: sync status, chain_id |
| `src/components/domain/index.ts` | Добавить: export SyncStatusBadge |
| `src/features/wallets/CreateWalletDialog.tsx` | Обновить: обязательный address |
| `src/features/wallets/WalletDetailPage.tsx` | Обновить: sync UI |
| `src/features/transactions/TransactionFilters.tsx` | Обновить: новые типы |
| `src/features/transactions/TransactionFormPage.tsx` | Обновить: только adjustment |
| `src/features/transactions/TransactionsPage.tsx` | Обновить: UI |
| `src/lib/format.ts` | Добавить: explorer URL helpers |

---

## Порядок реализации

1. **Types** (блокирует всё)
   - wallet.ts
   - transaction.ts

2. **Services & Hooks**
   - services/wallet.ts
   - hooks/useWallets.ts

3. **Domain Components**
   - SyncStatusBadge.tsx (новый)
   - TransactionTypeBadge.tsx
   - WalletCard.tsx

4. **Wallet Features**
   - CreateWalletDialog.tsx
   - WalletDetailPage.tsx

5. **Transaction Features**
   - TransactionFilters.tsx
   - TransactionFormPage.tsx
   - TransactionsPage.tsx

6. **Utilities**
   - format.ts

---

## Верификация

1. **Создание кошелька**: address обязателен, валидация EVM, только 5 сетей
2. **Список кошельков**: sync status badge отображается
3. **Детали кошелька**: sync info, кнопка Sync Now работает
4. **Транзакции**: новые типы отображаются с правильными badge
5. **Форма adjustment**: работает без выбора типа
6. **TypeScript**: нет ошибок типизации
7. **Тесты**: `bun run test --run` проходит
