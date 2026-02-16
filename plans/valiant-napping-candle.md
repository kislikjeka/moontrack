# Plan: Real Wallet E2E Sync Test on Base

## Context

Нужно **заменить** абстрактный тест-план (`docs/test-wallet-operations.md`) реальным пошаговым сценарием:
1. Зарегистрировать пользователя в MoonTrack
2. Добавить реальный кошелёк на Base (chain_id 8453)
3. Выполнить реальные операции на блокчейне (MetaMask/кошелёк): получить ETH, свап через 1inch, перевод на другой адрес
4. Подождать пока Zerion sync подхватит транзакции
5. Проверить через MoonTrack API что всё записалось правильно (транзакции, балансы, портфолио)

Zerion провайдер настроен, Base (8453) полностью поддерживается во всех слоях: wallet model, zerion adapter, chains config, sync processor.

## What Will Be Created

**Файл**: `/docs/test-wallet-operations.md` (перезаписываем существующий)

### Структура документа

#### Часть 1: Подготовка MoonTrack
- curl: register + login → получить JWT
- curl: create wallet с реальным Base адресом пользователя (chain_id: 8453)
- curl: проверить sync_status = "pending"
- Дождаться первого sync цикла (5 минут) или рестартнуть backend
- curl: проверить что sync_status стал "synced" (или "syncing"→"synced")
- curl: посмотреть какие транзакции sync уже подхватил (история кошелька)

#### Часть 2: Операции на блокчейне (руками в MetaMask)
Пошаговый чек-лист:

**Операция 1: Получить ETH на Base**
- Отправить ETH на свой Base адрес (с другого кошелька или bridge)
- Записать tx_hash, сумму, время

**Операция 2: Swap ETH → USDC на 1inch (Base)**
- Зайти на app.1inch.io, выбрать Base
- Свапнуть часть ETH в USDC
- Записать tx_hash, суммы in/out, gas

**Операция 3: Перевод USDC на другой адрес**
- Отправить часть USDC на другой свой адрес или друга
- Записать tx_hash, сумму, получателя

#### Часть 3: Проверка через MoonTrack API
После каждой операции (ждём sync цикл 5 мин):

- curl: GET /transactions — проверить что новая транзакция появилась
- curl: GET /transactions/{id} — проверить ledger entries (balanced, правильные типы)
- curl: GET /portfolio — проверить что балансы обновились
- Валидация:
  - transfer_in: 2 entries (asset_increase + income)
  - swap: 4-6 entries (clearing accounts + gas)
  - transfer_out: 2-4 entries (expense + asset_decrease + gas)
  - Все транзакции COMPLETED
  - SUM(debit) = SUM(credit) для каждой

#### Часть 4: Итоговая верификация
- curl: GET /portfolio — финальный снимок
- Сравнить балансы MoonTrack с реальными балансами на блокчейне (etherscan/basescan)
- curl: GET /transactions?type=swap — проверить что 1inch свап декодирован Zerion

### Ключевые curl-команды
Каждый шаг — конкретная curl команда с placeholder для адреса пользователя. Пользователь подставляет свой адрес при первом запуске.

### Что проверяем E2E
- Zerion sync: pending → syncing → synced lifecycle
- transfer_in handler: ETH deposit подхвачен
- swap handler: 1inch свап подхвачен и декодирован (protocol name, transfers_in/out)
- transfer_out handler: USDC перевод подхвачен
- Ledger: все entries balanced
- Portfolio: балансы совпадают с блокчейном
- Pricing: USD значения рассчитаны

## Verification
- Запустить `just dev`
- Выполнить curl из Части 1
- Выполнить операции из Части 2 (MetaMask)
- Выполнить проверки из Части 3
- Сверить с basescan.org

## Files to Modify
- `/docs/test-wallet-operations.md` — перезаписать новым содержимым
