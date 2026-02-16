# Blockchain Wallet Sync Implementation Plan

## Цель
Добавить автоматическую синхронизацию транзакций из EVM кошельков через Alchemy API. Кошельки становятся ТОЛЬКО крипто-кошельками с обязательным валидным адресом.

## Ключевые решения
- **Убираем manual_income/manual_outcome** → заменяем на blockchain-native типы
- **Кошелек = обязательный EVM адрес** (валидируем при создании)
- **Polling в фоне** с конфигурируемым интервалом (дефолт 5 мин)
- **Alchemy API** для получения истории транзакций
- **Multichain** поддержка EVM сетей из YAML конфига

## Архитектурное соответствие

План следует layered architecture (R1-R8 из architecture-rules.md):

```
Layer 5: transport      → HTTP handlers (wallet sync trigger endpoint)
Layer 4: ingestion      → (не используется напрямую, но sync service аналог)
Layer 3: module/transfer → Handlers для transfer_in, transfer_out, internal_transfer
Layer 2: platform/sync   → Sync service (orchestration, classification)
Layer 1: ledger         → Новые TransactionTypes
Layer 0: infra/gateway/alchemy → Alchemy API client
```

**Dependency flow:**
- `module/transfer` → imports `ledger`, `platform/wallet`, `platform/asset`
- `platform/sync` → imports `ledger`, `platform/wallet`, `platform/asset`, `module/transfer` (types only)
- `infra/gateway/alchemy` → imports `platform/sync` (implements interface)

---

## Изменения

### 1. Database Migration (000007_blockchain_sync.sql)

**wallets table:**
```sql
ALTER TABLE wallets
    ALTER COLUMN address SET NOT NULL,
    ALTER COLUMN chain_id TYPE BIGINT,  -- 1=ETH, 137=Polygon, 42161=Arbitrum
    ADD COLUMN sync_status VARCHAR(20) NOT NULL DEFAULT 'pending',
    ADD COLUMN last_sync_block BIGINT,
    ADD COLUMN last_sync_at TIMESTAMP,
    ADD COLUMN sync_error TEXT;

-- Unique constraint: один адрес на сеть на юзера
CREATE UNIQUE INDEX idx_wallets_user_chain_address
    ON wallets(user_id, chain_id, lower(address));
```

**transactions table update:**
```sql
UPDATE transactions SET type = 'transfer_in' WHERE type = 'manual_income';
UPDATE transactions SET type = 'transfer_out' WHERE type = 'manual_outcome';
```

---

### 2. New Transaction Types (`internal/ledger/model.go`)

Заменяем manual типы на blockchain-native:

```go
const (
    TxTypeTransferIn       TransactionType = "transfer_in"       // Входящий перевод
    TxTypeTransferOut      TransactionType = "transfer_out"      // Исходящий перевод
    TxTypeInternalTransfer TransactionType = "internal_transfer" // Между своими кошельками
    TxTypeAssetAdjustment  TransactionType = "asset_adjustment"  // Оставляем для корректировок
)
```

**Ledger Entry Mapping:**

| Тип | DEBIT | CREDIT |
|-----|-------|--------|
| transfer_in | wallet.{id}.{asset} (asset_increase) | income.{chain}.{asset} |
| transfer_out | expense.{chain}.{asset} + gas.{chain}.{native} | wallet.{id}.{asset} + wallet.{id}.{native} |
| internal_transfer | wallet.{dst}.{asset} | wallet.{src}.{asset} + wallet.{src}.{native} (gas) |

---

### 3. Wallet Model Updates (`internal/platform/wallet/`)

**model.go:**
```go
type SyncStatus string

const (
    SyncStatusPending SyncStatus = "pending"
    SyncStatusSyncing SyncStatus = "syncing"
    SyncStatusSynced  SyncStatus = "synced"
    SyncStatusError   SyncStatus = "error"
)

type Wallet struct {
    ID            uuid.UUID   `json:"id" db:"id"`
    UserID        uuid.UUID   `json:"user_id" db:"user_id"`
    Name          string      `json:"name" db:"name"`
    ChainID       int64       `json:"chain_id" db:"chain_id"`      // EVM chain ID (1, 137, 42161)
    Address       string      `json:"address" db:"address"`        // REQUIRED, validated
    SyncStatus    SyncStatus  `json:"sync_status" db:"sync_status"`
    LastSyncBlock *int64      `json:"last_sync_block" db:"last_sync_block"`
    LastSyncAt    *time.Time  `json:"last_sync_at" db:"last_sync_at"`
    SyncError     *string     `json:"sync_error,omitempty" db:"sync_error"`
    CreatedAt     time.Time   `json:"created_at" db:"created_at"`
    UpdatedAt     time.Time   `json:"updated_at" db:"updated_at"`
}
```

**validation.go (новый файл):**
```go
func ValidateEVMAddress(address string) (string, error) {
    // Regex: ^0x[a-fA-F0-9]{40}$
    // Return EIP-55 checksummed address
}
```

---

### 4. Alchemy Client (`internal/infra/gateway/alchemy/`)

**Паттерн из `infra/gateway/coingecko/` (per R4: Gateway is infrastructure):**

- `client.go` - HTTP клиент для JSON-RPC
- `types.go` - Request/Response структуры
- `adapter.go` - Implements `sync.BlockchainClient` interface (dependency inversion)

**Ключевой метод:**
```go
func (c *Client) GetAssetTransfers(ctx context.Context, chainID int64, params AssetTransferParams) (*AssetTransferResponse, error)
```

Использует `alchemy_getAssetTransfers` API для получения:
- ERC-20 transfers
- Native token transfers (ETH, MATIC)
- Internal transactions

**Adapter pattern (как в `coingecko/adapter.go`):**
```go
// infra/gateway/alchemy/adapter.go
type SyncClientAdapter struct {
    client *Client
}

// Implements sync.BlockchainClient
func (a *SyncClientAdapter) GetAssetTransfers(...) ([]sync.AssetTransfer, error) {
    // Transform Alchemy types to sync domain types
}
```

---

### 5. Chain Configuration (`config/chains.yaml`)

```yaml
chains:
  - chain_id: 1
    name: "Ethereum Mainnet"
    alchemy_network: "eth-mainnet"
    native_asset: "ETH"
    native_decimals: 18
    native_coingecko_id: "ethereum"

  - chain_id: 137
    name: "Polygon"
    alchemy_network: "polygon-mainnet"
    native_asset: "MATIC"
    native_decimals: 18
    native_coingecko_id: "matic-network"

  - chain_id: 42161
    name: "Arbitrum One"
    alchemy_network: "arb-mainnet"
    native_asset: "ETH"
    native_decimals: 18
    native_coingecko_id: "ethereum"

  - chain_id: 10
    name: "Optimism"
    alchemy_network: "opt-mainnet"
    native_asset: "ETH"
    native_decimals: 18
    native_coingecko_id: "ethereum"

  - chain_id: 8453
    name: "Base"
    alchemy_network: "base-mainnet"
    native_asset: "ETH"
    native_decimals: 18
    native_coingecko_id: "ethereum"
```

**ENV variables:**
```bash
ALCHEMY_API_KEY=your-api-key
SYNC_POLL_INTERVAL=5m
CHAINS_CONFIG_PATH=config/chains.yaml
```

---

### 6. Sync Service (`internal/platform/sync/`)

**Новый сервис (паттерн как у PriceUpdater в `platform/asset/updater.go:73`):**

- `service.go` - Background polling, wallet sync orchestration
- `processor.go` - Transfer classification, ledger entry creation
- `port.go` - Interfaces for dependencies (R2: interfaces in consumer's package)
- `config.go` - Config loading

**Interfaces in port.go (per R2):**
```go
// platform/sync/port.go
type BlockchainClient interface {
    GetAssetTransfers(ctx context.Context, chainID int64, params TransferParams) ([]AssetTransfer, error)
}

type WalletRepository interface {
    GetWalletsForSync(ctx context.Context) ([]*wallet.Wallet, error)
    UpdateSyncState(ctx context.Context, walletID uuid.UUID, status SyncStatus, lastBlock *int64) error
}
```

**Ключевая логика:**

1. **Polling loop** - каждые N минут синхронизирует все кошельки (паттерн из `updater.go:73-91`)
2. **Initial sync** - загружает всю историю при добавлении кошелька
3. **Incremental sync** - только новые блоки после последней синхронизации
4. **Idempotency** - `UNIQUE(source, external_id)` в transactions предотвращает дубли
5. **Internal transfer detection** - кеш адресов для определения переводов между своими кошельками
6. **Row-level locking** - использует `GetAccountBalanceForUpdate()` для concurrent safety

---

### 7. Transfer Handlers (`internal/module/transfer/`)

**Структура модуля (per R3: flat, no sub-packages):**

```
module/transfer/
├── model.go              # Domain types: TransferIn, TransferOut, InternalTransfer
├── errors.go             # Module-specific errors ONLY (reuse ledger errors per R8)
├── handler_in.go         # TransferInHandler
├── handler_in_test.go
├── handler_out.go        # TransferOutHandler
├── handler_out_test.go
├── handler_internal.go   # InternalTransferHandler
└── handler_internal_test.go
```

**Handler interface (implements `ledger.Handler`):**
```go
// Паттерн из manual/handler_income.go:24-28
type TransferInHandler struct {
    ledger.BaseHandler
    assetService  AssetService    // для получения цен
    walletRepo    WalletRepository // для проверки ownership
}

func (h *TransferInHandler) Type() ledger.TransactionType { return ledger.TxTypeTransferIn }
func (h *TransferInHandler) ValidateData(ctx context.Context, data map[string]interface{}) error
func (h *TransferInHandler) Handle(ctx context.Context, data map[string]interface{}) ([]*ledger.Entry, error)
```

**Blockchain metadata в Entry.Metadata:**
- tx_hash
- block_number
- from_address
- to_address
- contract_address (для ERC-20)

**Error ownership (per R8):**
- Reuse `ledger.ErrInsufficientBalance` - не переопределять
- Module-specific only: `ErrDuplicateTransfer`, `ErrInvalidBlockchainData`

---

### 8. Удаляемый код

- `internal/module/manual/handler_income.go` - удалить
- `internal/module/manual/handler_outcome.go` - удалить
- `internal/module/manual/` - вся директория

---

### 9. Wiring в main.go (per R7)

**All wiring happens in `cmd/api/main.go`:**

```go
// Create Alchemy client (infra layer)
chainsConfig, _ := config.LoadChainsConfig(cfg.ChainsConfigPath)
alchemyClient := alchemy.NewClient(cfg.AlchemyAPIKey, chainsConfig.Chains)
blockchainClient := alchemy.NewSyncClientAdapter(alchemyClient)

// Create sync service (platform layer)
syncConfig := &sync.Config{
    PollInterval:      cfg.SyncPollInterval,
    ConcurrentWallets: 3,
}
syncSvc := sync.NewService(syncConfig, blockchainClient, walletRepo, ledgerSvc, assetSvc, log)

// Register transfer handlers (module layer)
transferInHandler := transfer.NewTransferInHandler(assetSvc, walletRepo)
transferOutHandler := transfer.NewTransferOutHandler(assetSvc, walletRepo, ledgerSvc)
internalTransferHandler := transfer.NewInternalTransferHandler(assetSvc, walletRepo)

handlerRegistry.Register(transferInHandler)
handlerRegistry.Register(transferOutHandler)
handlerRegistry.Register(internalTransferHandler)

// Start background sync (like PriceUpdater at line 185)
go syncSvc.Run(ctx)
```

---

## Структура файлов

```
apps/backend/
├── config/
│   └── chains.yaml                      # NEW
├── internal/
│   ├── infra/gateway/
│   │   └── alchemy/                     # NEW
│   │       ├── client.go
│   │       ├── types.go
│   │       └── adapter.go
│   ├── ledger/
│   │   └── model.go                     # UPDATE: new tx types
│   ├── module/
│   │   ├── transfer/                    # NEW
│   │   │   ├── handler_in.go
│   │   │   ├── handler_out.go
│   │   │   ├── handler_internal.go
│   │   │   └── model.go
│   │   ├── adjustment/                  # KEEP
│   │   └── manual/                      # DELETE
│   └── platform/
│       ├── sync/                        # NEW
│       │   ├── service.go
│       │   ├── processor.go
│       │   ├── port.go
│       │   └── config.go
│       └── wallet/
│           ├── model.go                 # UPDATE
│           ├── validation.go            # NEW
│           └── service.go               # UPDATE
├── migrations/
│   └── 000007_blockchain_sync.sql       # NEW
├── pkg/config/
│   ├── config.go                        # UPDATE
│   └── chains.go                        # NEW
└── cmd/api/main.go                      # UPDATE: wire sync service
```

---

## Критические файлы для модификации

| Файл | Изменение |
|------|-----------|
| `internal/platform/wallet/model.go` | Новые поля: Address required, ChainID int64, SyncStatus |
| `internal/ledger/model.go` | Новые типы: transfer_in, transfer_out, internal_transfer |
| `pkg/config/config.go` | Новые env vars: ALCHEMY_API_KEY, SYNC_POLL_INTERVAL |
| `cmd/api/main.go` | Wire Alchemy client, Sync service, новые handlers |
| `internal/infra/postgres/wallet_repo.go` | Новые методы для sync state |

---

## Порядок реализации

### Phase 1: Foundation
1. Создать миграцию БД
2. Добавить EVM address validation
3. Обновить wallet model и service
4. Создать chains.yaml и config loader

### Phase 2: Alchemy Integration
1. Создать Alchemy client
2. Реализовать GetAssetTransfers с пагинацией
3. Написать интеграционные тесты

### Phase 3: Transfer Handlers
1. Добавить новые типы в ledger/model.go
2. Создать transfer_in handler
3. Создать transfer_out handler
4. Создать internal_transfer handler
5. Удалить manual handlers

### Phase 4: Sync Service
1. Создать sync service структуру
2. Реализовать single wallet sync
3. Добавить transfer classification
4. Реализовать background polling
5. Добавить idempotency checks

### Phase 5: Integration
1. Wire up в main.go
2. Добавить API endpoint для manual sync trigger
3. E2E тестирование

---

## Верификация

### Unit Tests (REQUIRED)

1. **Handler validation tests** (`module/transfer/handler_*_test.go`):
   - Negative amounts rejected
   - Future dates rejected
   - Empty asset IDs rejected
   - Invalid UUIDs handled
   - Nil amounts handled

2. **Double-entry balance tests**:
   ```go
   // CRITICAL: Verify SUM(debits) = SUM(credits)
   debitSum := new(big.Int)
   creditSum := new(big.Int)
   for _, entry := range entries {
       if entry.DebitCredit == ledger.Debit {
           debitSum.Add(debitSum, entry.Amount)
       } else {
           creditSum.Add(creditSum, entry.Amount)
       }
   }
   assert.Equal(t, 0, debitSum.Cmp(creditSum))
   ```

3. **Authorization tests**:
   - Wallet ownership verification (user can only sync own wallets)
   - Cross-user access rejection → returns `ErrUnauthorized`

### Integration Tests (REQUIRED)

4. **TestContainers integration**:
   ```go
   //go:build integration
   func TestMain(m *testing.M) {
       testDB = testutil.NewTestDB(ctx)
       // ...
   }
   ```

5. **Concurrent sync tests**:
   - Multiple wallets syncing in parallel
   - No race conditions in balance updates
   - Row-level locking with `SELECT ... FOR UPDATE`

6. **Idempotency tests**:
   - Same transaction processed twice → no duplicates
   - `UNIQUE(source, external_id)` constraint works

### Security Tests (REQUIRED per security-checklist.md)

7. **Row-level locking**:
   - `GetAccountBalanceForUpdate()` used for balance checks
   - Double-spend prevention in concurrent scenarios

8. **Entry immutability**:
   - No UpdateEntry method exists
   - Corrections use adjustment transactions

9. **SQL injection prevention**:
   - All queries use parameterized statements ($1, $2)

### E2E Tests

10. **Full flow**:
    - Create wallet with valid EVM address
    - Trigger sync
    - Verify transactions appear in ledger
    - Verify balances are correct
    - Reconciliation passes

11. **Manual test**: Add real wallet on testnet, verify sync

---

## Security Checklist (per ledger-development skill)

Before submitting code:

- [ ] Uses `GetAccountBalanceForUpdate()` for concurrent balance checks
- [ ] Checks wallet ownership via `middleware.GetUserIDFromContext(ctx)`
- [ ] Rejects negative amounts (`Amount.Sign() < 0`)
- [ ] Rejects future dates (`OccurredAt.After(time.Now())`)
- [ ] Rejects unbalanced entries (`debitSum != creditSum`)
- [ ] Validates all required fields (wallet_id, asset_id, amount)
- [ ] Uses parameterized queries (no SQL injection)
- [ ] Entries are IMMUTABLE - never update, only create
- [ ] All operations wrapped in database transaction
- [ ] Proper rollback on any error
