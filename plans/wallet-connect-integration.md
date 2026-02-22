# Plan: MetaMask / WalletConnect Integration

## Goal
Добавить кнопку "Connect Wallet" в диалог создания кошелька, чтобы пользователь мог в один клик подключить MetaMask (или другой EVM-кошелек) и автоматически заполнить адрес и chain.

## Scope
**Фаза 1 (этот план)**: только frontend — автозаполнение адреса через Web3-кошелек. Без изменений бэкенда. Без SIWE-верификации.

**Фаза 2 (позже, если нужно)**: SIWE-верификация на бэкенде, пометка "verified" у кошелька.

## Подход
- **wagmi v2 + viem** — стандарт для React + EVM кошельков
- **Встроенные коннекторы wagmi** — MetaMask, WalletConnect, Coinbase Wallet, Injected (Rabby и т.д.)
- **Без RainbowKit/ConnectKit** — слишком тяжёлые для нашего кейса. Нам нужна только одна кнопка "Connect Wallet" в диалоге создания, а не полноценная auth-система. Свой минимальный UI.

## Что даёт пользователю
1. В диалоге "Create Wallet" появляется кнопка "Connect Wallet"
2. Нажатие → попап MetaMask / выбор кошелька
3. Пользователь подтверждает → address и chain_id автоматически заполняются
4. Пользователь вводит имя → создаёт кошелёк как обычно
5. Ручной ввод адреса по-прежнему работает (fallback)

## Реализация

### Шаг 1: Установить зависимости
```bash
cd apps/frontend && bun add wagmi viem @tanstack/react-query @walletconnect/ethereum-provider
```
Примечание: `@tanstack/react-query` уже стоит, wagmi его использует.

### Шаг 2: Настроить wagmi config

**Новый файл**: `apps/frontend/src/lib/wagmi.ts`

```ts
import { createConfig, http } from 'wagmi'
import { mainnet, polygon, arbitrum, optimism, base } from 'wagmi/chains'
import { injected, walletConnect, coinbaseWallet } from 'wagmi/connectors'

const projectId = import.meta.env.VITE_WALLETCONNECT_PROJECT_ID || ''

export const wagmiConfig = createConfig({
  chains: [mainnet, polygon, arbitrum, optimism, base],
  connectors: [
    injected(),                           // MetaMask, Rabby, etc.
    walletConnect({ projectId }),          // WalletConnect v2
    coinbaseWallet({ appName: 'MoonTrack' }),
  ],
  transports: {
    [mainnet.id]: http(),
    [polygon.id]: http(),
    [arbitrum.id]: http(),
    [optimism.id]: http(),
    [base.id]: http(),
  },
})
```

Chains совпадают с `SUPPORTED_CHAINS` во фронте. Транспорты используют дефолтные public RPC (достаточно для sign-only, мы не делаем RPC-вызовов).

### Шаг 3: Обернуть приложение в WagmiProvider

**Изменить**: `apps/frontend/src/app/App.tsx`

Обернуть в `<WagmiProvider config={wagmiConfig}>`. Поскольку `QueryClientProvider` уже есть, wagmi будет использовать его.

Важно: `WagmiProvider` должен быть **внутри** `QueryClientProvider` (wagmi v2 требует).

### Шаг 4: Компонент ConnectWalletButton

**Новый файл**: `apps/frontend/src/features/wallets/ConnectWalletButton.tsx`

Минимальный компонент:
- Кнопка "Connect Wallet" (variant="outline")
- При клике: вызывает `connect()` из wagmi с injected коннектором (MetaMask)
- Если injected нет → fallback на walletConnect
- После подключения: вызывает `onConnect(address, chainId)`
- Показывает состояние: idle → connecting → connected

Не делаем выбор коннектора (пока). `injected()` автоматически подхватит MetaMask/Rabby. Если кошелёк не установлен — покажем сообщение.

### Шаг 5: Интегрировать в CreateWalletDialog

**Изменить**: `apps/frontend/src/features/wallets/CreateWalletDialog.tsx`

1. Добавить `<ConnectWalletButton>` между описанием и полем адреса
2. Разделитель "or enter manually"
3. При `onConnect(address, chainId)`:
   - Заполнить поле address
   - Выбрать chainId в селекте (если chain поддерживается)
   - Заблокировать поля address и chain (пока пользователь не нажмёт "disconnect")
4. Кнопка disconnect / "change" для сброса

### Шаг 6: Environment variable

Добавить `VITE_WALLETCONNECT_PROJECT_ID` в `.env.example`.

WalletConnect Cloud project ID — бесплатный на https://cloud.walletconnect.com. Без него WalletConnect не будет работать, но injected (MetaMask) будет работать без project ID.

## Файлы (создать / изменить)

| Действие | Файл |
|----------|------|
| Создать | `apps/frontend/src/lib/wagmi.ts` |
| Создать | `apps/frontend/src/features/wallets/ConnectWalletButton.tsx` |
| Изменить | `apps/frontend/src/app/App.tsx` — добавить WagmiProvider |
| Изменить | `apps/frontend/src/features/wallets/CreateWalletDialog.tsx` — интегрировать ConnectWalletButton |
| Изменить | `apps/frontend/.env.example` — добавить VITE_WALLETCONNECT_PROJECT_ID |

## Что НЕ делаем
- Не трогаем бэкенд
- Не добавляем SIWE/верификацию подписи
- Не добавляем RainbowKit/ConnectKit (overkill)
- Не меняем DB-схему
- Не ограничиваем дублирование кошельков (read-only трекинг, данные публичные)

## Риски и решения
1. **WalletConnect project ID**: без него WalletConnect коннектор не работает. Решение: MetaMask/Rabby работают через injected() без ID. WalletConnect — опционален.
2. **Chain mismatch**: пользователь подключён к сети не из SUPPORTED_CHAINS. Решение: показать предупреждение, предложить переключить сеть или ввести адрес вручную.
3. **Размер бандла**: wagmi + viem ~50-80KB gzipped. Приемлемо для крипто-приложения, это целевая аудитория.
