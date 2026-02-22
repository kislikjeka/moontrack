# Multi-Chain EVM Wallet

## Context

Сейчас кошелёк привязан к одной сети (`chain_id` в таблице `wallets`). Один и тот же адрес на разных сетях — разные кошельки. Это неудобно и не соответствует реальности: в EVM один адрес работает на всех сетях.

**Цель**: один EVM-кошелёк = один адрес, автоматически работающий на всех поддерживаемых сетях (Ethereum, Polygon, Arbitrum, Optimism, Base, Avalanche, BSC). Данные можно полностью вайпнуть (прототип).

**Chain ID = строка** (Zerion chain name): `"base"`, `"ethereum"`, `"polygon"` и т.д. — везде вместо числовых ID.

---

## 1. Миграция БД

**Файл**: новая миграция `apps/backend/migrations/000011_multichain_wallet.up.sql`

```sql
-- Вайп финансовых данных (порядок важен для FK)
TRUNCATE account_balances, entries, transactions, accounts, wallets CASCADE;

-- Убираем chain_id из wallets
ALTER TABLE wallets DROP COLUMN IF EXISTS chain_id;

-- Убираем старые индексы/constraints
DROP INDEX IF EXISTS idx_wallets_user_chain_address;

-- Новый constraint: один адрес на юзера (без chain)
CREATE UNIQUE INDEX idx_wallets_user_address ON wallets(user_id, lower(address));
```

Sync-поля (`sync_status`, `last_sync_at`, `sync_error`, `sync_started_at`) **остаются на wallets** — один курсор и один статус.

---

## 2. Chain ID: int64 → string

**Глобальное изменение**: chain_id повсюду меняется с `int64` (1, 8453, 137) на `string` (Zerion chain names: `"ethereum"`, `"base"`, `"polygon"`).

### Что меняется:
- `DecodedTransaction.ChainID` в `sync/port.go`: `int64` → `string`
- Все handler model structs (`transfer/model.go`, `swap/model.go`): `ChainID int64` → `ChainID string`
- Все format strings в handlers: `%d` → `%s`
- `wallet/model.go`: `supportedEVMChains` map key `int64` → `string`
- `zerion/types.go`: `ZerionChainToID` / `IDToZerionChain` → заменить на `SupportedChains map[string]string` (zerion name → display name)

### Пример account codes:
```
wallet.550e8400-e29b-41d4-a716-446655440000.base.USDC
income.base.USDC
expense.ethereum.ETH
gas.arbitrum.ETH
clearing.polygon.USDC
```

---

## 3. Wallet Model

**Файлы**: `internal/platform/wallet/model.go`, `port.go`, `service.go`, `validation.go`

### model.go
- **Удалить** `ChainID int64` из `Wallet` struct
- **Переделать** `supportedEVMChains`: `map[string]string` — ключ: zerion chain name, значение: display name
  ```go
  var supportedEVMChains = map[string]string{
      "ethereum":  "Ethereum",
      "polygon":   "Polygon",
      "arbitrum":  "Arbitrum One",
      "optimism":  "Optimism",
      "base":      "Base",
      "avalanche": "Avalanche",
      "bsc":       "BNB Smart Chain",
  }
  ```
- `GetSupportedChains() []string` — возвращает ключи (zerion names)
- `GetChainName(chain string) string` — display name по zerion name
- `IsValidChain(chain string) bool` — проверка поддержки

### validation.go
- Убрать `IsValidEVMChainID()` из `ValidateCreate()`, оставить проверку address + name

### port.go
- `ExistsByUserChainAndAddress` → `ExistsByUserAndAddress(ctx, userID, address) (bool, error)`
- Sync-методы остаются

### service.go
- `Create`: `ExistsByUserAndAddress` вместо `ExistsByUserChainAndAddress`

---

## 4. Wallet Repository

**Файл**: `internal/infra/postgres/wallet_repo.go`

- Убрать `chain_id` из INSERT/SELECT/Scan
- `ExistsByUserChainAndAddress` → `ExistsByUserAndAddress` (SQL без chain_id)
- Sync-методы без изменений

---

## 5. Ledger Account Codes

**Файл**: `internal/ledger/service.go`

Wallet account code получает chain (строка):

| Тип | Старый формат | Новый формат |
|-----|--------------|-------------|
| Wallet | `wallet.{uuid}.{asset}` | `wallet.{uuid}.{chain}.{asset}` |
| Income | `income.{chain_id}.{asset}` | `income.{chain}.{asset}` (формат тот же, тип chain стал string) |
| Expense/Gas/Clearing | аналогично | chain как string |

Резолвер аккаунтов (`parseAccountCode`) не нужно менять — читает `wallet_id` и `chain_id` из metadata (уже ожидает string).

`GetBalance` — добавить `chain string`, формировать `wallet.%s.%s.%s`.

---

## 6. Transaction Handlers — Account Code Update

Во всех handler-ах меняется:
1. **Model structs**: `ChainID int64` → `ChainID string`
2. **Validation**: `ChainID <= 0` → `ChainID == ""`
3. **Account codes**: `%d` → `%s`

```go
// Было:
fmt.Sprintf("wallet.%s.%s", walletIDStr, assetID)
fmt.Sprintf("income.%d.%s", txn.ChainID, txn.AssetID)
// Стало:
fmt.Sprintf("wallet.%s.%s.%s", walletIDStr, txn.ChainID, assetID)
fmt.Sprintf("income.%s.%s", txn.ChainID, txn.AssetID)
```

**Файлы**:
- `internal/module/transfer/model.go` — ChainID type в 3 structs
- `internal/module/transfer/handler_in.go:136` — wallet + income account_code
- `internal/module/transfer/handler_out.go` — wallet + expense + gas account_code
- `internal/module/transfer/handler_internal.go` — source/dest wallet + gas account_code
- `internal/module/swap/model.go` — ChainID type
- `internal/module/swap/handler.go` — wallet + clearing + gas account_code
- `internal/module/defi/` — wallet account_code (если существуют)

---

## 7. Zerion Client & Adapter

### client.go
**Файл**: `internal/infra/gateway/zerion/client.go`

Изменить сигнатуру: `chainID string` → `chainIDs []string` (массив). Внутри метода join в comma-separated string.

```go
// Было:
func (c *Client) GetTransactions(ctx, address, chainID string, since)
    params.Set("filter[chain_ids]", chainID)

// Стало:
func (c *Client) GetTransactions(ctx, address string, chainIDs []string, since)
    params.Set("filter[chain_ids]", strings.Join(chainIDs, ","))
```

### types.go
**Файл**: `internal/infra/gateway/zerion/types.go`

Добавить `Relationships` в `TransactionData` для определения chain из response:
```go
type TransactionData struct {
    Type          string                `json:"type"`
    ID            string                `json:"id"`
    Attributes    TransactionAttributes `json:"attributes"`
    Relationships Relationships         `json:"relationships"`
}

type Relationships struct {
    Chain ChainRelation `json:"chain"`
}

type ChainRelation struct {
    Data ChainData `json:"data"`
}

type ChainData struct {
    Type string `json:"type"`
    ID   string `json:"id"` // "base", "ethereum", etc.
}
```

Убрать/упростить `ZerionChainToID` и `IDToZerionChain` — chain теперь строка, маппинг не нужен.

### adapter.go
**Файл**: `internal/infra/gateway/zerion/adapter.go`

```go
// Было: один chainID int64
func (a *SyncAdapter) GetTransactions(ctx, address string, chainID int64, since)

// Стало: без chainID, берёт все supported chains
func (a *SyncAdapter) GetTransactions(ctx, address string, since time.Time)
```

- Передаёт `wallet.GetSupportedChains()` в `client.GetTransactions`
- `convertTransaction`: chain берётся из `td.Relationships.Chain.Data.ID` (строка)
- `convertTransfer`: `zerionChain` передаётся напрямую (уже строка)
- Если chain не в supported list — skip с log warning

### sync port.go
**Файл**: `internal/platform/sync/port.go`

```go
// Было:
GetTransactions(ctx, address string, chainID int64, since time.Time) ([]DecodedTransaction, error)
// Стало:
GetTransactions(ctx, address string, since time.Time) ([]DecodedTransaction, error)
```

`DecodedTransaction.ChainID`: `int64` → `string`

---

## 8. Sync Service

**Файл**: `internal/platform/sync/service.go`

- `syncWallet`: убрать `w.ChainID` из вызова `zerionProvider.GetTransactions`
- Убрать лог `chain_id` из wallet sync

### Zerion Processor — Bridge fix
**Файл**: `internal/platform/sync/zerion_processor.go`

В `detectInternalTransfer` (~строка 113) добавить guard:
```go
if strings.EqualFold(counterpartyAddr, w.Address) {
    continue // Bridge, not internal transfer
}
```

---

## 9. HTTP API

**Файл**: `internal/transport/httpapi/handler/wallet.go`

### CreateWalletRequest
```go
type CreateWalletRequest struct {
    Name    string `json:"name"`
    Address string `json:"address"`
}
```

### WalletResponse
```go
type WalletResponse struct {
    ID              string   `json:"id"`
    UserID          string   `json:"user_id"`
    Name            string   `json:"name"`
    Address         string   `json:"address"`
    SupportedChains []string `json:"supported_chains"` // ["ethereum","polygon","base",...]
    SyncStatus      string   `json:"sync_status"`
    LastSyncAt      *string  `json:"last_sync_at,omitempty"`
    SyncError       *string  `json:"sync_error,omitempty"`
    CreatedAt       string   `json:"created_at"`
    UpdatedAt       string   `json:"updated_at"`
}
```

---

## 10. Portfolio Module

**Файлы**: `internal/module/portfolio/service.go`, `adapter.go`

- Убрать `ChainID` из portfolio `Wallet` struct
- `WalletRepositoryAdapter`: убрать маппинг ChainID

---

## 11. DI Wiring

**Файл**: `cmd/api/main.go` — минимальные изменения.

---

## Порядок реализации (Team, 4 волны)

### Wave 1 — Независимые слои (3 агента параллельно)

**Agent A: Wallet + Migration**
- `migrations/000011_multichain_wallet.up.sql` + `.down.sql`
- `wallet/model.go` — удалить ChainID, string-based supportedEVMChains
- `wallet/port.go` — ExistsByUserAndAddress
- `wallet/validation.go` — убрать chain validation
- `wallet/service.go` — ExistsByUserAndAddress
- `infra/postgres/wallet_repo.go` — убрать chain_id из SQL

**Agent B: Zerion + Sync Port**
- `zerion/types.go` — Relationships struct, убрать ZerionChainToID/IDToZerionChain
- `zerion/client.go` — chainIDs []string
- `zerion/adapter.go` — multi-chain, chain из response
- `sync/port.go` — ChainID string, убрать chainID из TransactionDataProvider interface

**Agent C: Handler Models**
- `transfer/model.go` — ChainID int64 → string (3 structs + validation)
- `swap/model.go` — ChainID int64 → string

### Wave 2 — Зависит от Wave 1 (2 агента параллельно)

**Agent D: Handler Account Codes** (зависит от Agent C)
- `transfer/handler_in.go` — wallet + income account_code: %s format, add chain to wallet
- `transfer/handler_out.go` — wallet + expense + gas account_code
- `transfer/handler_internal.go` — source/dest wallet + gas account_code
- `swap/handler.go` — wallet + clearing + gas account_code
- `defi/` handlers — wallet account_code (если существуют)

**Agent E: Sync Service** (зависит от Agent A + B)
- `sync/service.go` — убрать w.ChainID из вызовов
- `sync/zerion_processor.go` — bridge fix в detectInternalTransfer

### Wave 3 — Зависит от Wave 1+2 (2 агента параллельно)

**Agent F: HTTP + Portfolio** (зависит от Agent A)
- `transport/httpapi/handler/wallet.go` — request/response без chain_id
- `module/portfolio/service.go` — убрать ChainID из Wallet struct
- `module/portfolio/adapter.go` — убрать маппинг ChainID

**Agent G: Ledger** (зависит от Agent D)
- `ledger/service.go` — GetBalance с chain string

### Wave 4 — Финализация

**Agent H: DI + Build + Test**
- `cmd/api/main.go` — wiring
- `go build ./...`
- `go test ./...`

---

## Верификация

1. `just db-reset && just migrate-up` — миграция без ошибок
2. `cd apps/backend && go build ./...` — компилируется
3. `cd apps/backend && go test ./...` — тесты проходят
4. Создать кошелёк через API без chain_id → 201 Created
5. Trigger sync → Zerion вызывается с `filter[chain_ids]=ethereum,polygon,arbitrum,optimism,base,avalanche,bsc`
6. Account codes в БД: `wallet.{uuid}.base.USDC` формат
7. Portfolio — агрегированные данные по всем сетям

---

## Риски

- **Bridge detection**: без фикса в `detectInternalTransfer` мосты теряют incoming. Фикс включён.
- **Zerion chain format**: chain из `relationships.chain.data.id`. Стабильное поле API.
- **Negative balance**: existing two-phase sync достаточен.
- **Unknown chains**: если Zerion вернёт неподдерживаемую сеть — skip с логом.
