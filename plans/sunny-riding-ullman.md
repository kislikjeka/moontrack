# План исправления недочётов Blockchain Wallet Sync

## Анализ текущего состояния

### Что реализовано ✅

| Компонент | Статус | Детали |
|-----------|--------|--------|
| **Transfer Handlers** | ✅ | handler_in, handler_out, handler_internal полностью реализованы |
| **Handler Interface** | ✅ | Type(), Handle(), ValidateData() реализованы |
| **Валидация входов** | ✅ | Отрицательные суммы, будущие даты, пустые asset_id |
| **Авторизация** | ✅ | Проверка владения кошельком через middleware.GetUserIDFromContext |
| **Double-Entry Balance** | ✅ | Все entries сбалансированы (debit = credit) |
| **Unit Tests Transfer** | ✅ | 744 строки, 26+ тест-кейсов |
| **Ledger Types** | ✅ | transfer_in, transfer_out, internal_transfer добавлены |
| **Wallet Model** | ✅ | ChainID, SyncStatus, LastSyncBlock, LastSyncAt, SyncError |
| **Wallet Validation** | ✅ | ValidateEVMAddress с EIP-55 checksum |
| **Migration** | ✅ | 000007_blockchain_sync.sql |
| **Sync Service** | ✅ | Background polling, concurrent wallets |
| **Alchemy Client** | ✅ | GetAssetTransfers, пагинация, adapter |
| **Chains Config** | ✅ | 7 EVM цепей |
| **Processor** | ✅ | Классификация, idempotency, USD rates |
| **Main Wiring** | ✅ | Все компоненты интегрированы |
| **Ledger Concurrent Tests** | ✅ | NoDoubleSpend, CorrectTotal, NoDuplicates |

### Что отсутствует ❌

| Компонент | Проблема | Приоритет |
|-----------|----------|-----------|
| **Integration Tests для Transfer** | Нет `//go:build integration` тестов для transfer handlers | HIGH |
| **Tests для Sync Service** | Нет тестов для platform/sync/ | HIGH |
| **Tests для Alchemy Client** | Нет тестов для infra/gateway/alchemy/ | MEDIUM |
| **Concurrent Tests для Transfer** | Нет тестов concurrent sync одного кошелька | MEDIUM |
| **Row-level Locking в Transfer** | Handlers не используют GetAccountBalanceForUpdate напрямую | LOW* |
| **Reconciliation Tests** | Нет тестов ReconcileBalance после blockchain sync | MEDIUM |

*Note: Row-level locking используется в ledger.Service.RecordTransaction, поэтому handlers не требуют прямого вызова.

---

## План исправления

### Phase 1: Integration Tests для Transfer Handlers (HIGH)

**Файл**: `internal/module/transfer/handler_integration_test.go`

```go
//go:build integration
```

**Тесты**:
1. `TestTransferIn_E2E_CreatesBalancedEntries` - полный flow с реальной БД
2. `TestTransferOut_E2E_DecreasesBalance` - проверка списания
3. `TestInternalTransfer_E2E_MovesBalance` - проверка перемещения
4. `TestTransfer_Reconciliation_Passes` - reconcile после transfer

**Критические проверки**:
- Записи в transactions и entries
- Баланс аккаунтов после операции
- Reconciliation pass

---

### Phase 2: Tests для Sync Service (HIGH)

**Файл**: `internal/platform/sync/service_test.go`

**Unit Tests**:
1. `TestProcessor_ClassifyTransfer_Incoming` - классификация входящих
2. `TestProcessor_ClassifyTransfer_Outgoing` - классификация исходящих
3. `TestProcessor_ClassifyTransfer_Internal` - обнаружение internal
4. `TestProcessor_Idempotency_NoDuplicates` - повторная обработка
5. `TestProcessor_USDRate_GracefulDegradation` - нет цены → "0"

**Файл**: `internal/platform/sync/service_integration_test.go`

**Integration Tests**:
1. `TestSyncService_SyncWallet_RecordsTransfers` - полный sync flow
2. `TestSyncService_ConcurrentWalletSync_NoRace` - parallel sync
3. `TestSyncService_InternalTransfer_RecordedOnce` - не дублирует

---

### Phase 3: Tests для Alchemy Client (MEDIUM)

**Файл**: `internal/infra/gateway/alchemy/client_test.go`

**Unit Tests (с mock HTTP)**:
1. `TestClient_GetAssetTransfers_ParsesResponse` - парсинг ответа
2. `TestClient_GetAssetTransfers_Pagination` - обработка pageKey
3. `TestClient_RateLimitError_RetryAfter` - обработка 429
4. `TestAdapter_ConvertTransfer_CorrectFields` - преобразование типов

---

### Phase 4: Concurrent Sync Tests (MEDIUM)

**Файл**: `internal/platform/sync/service_concurrent_test.go`

```go
//go:build integration
```

**Тесты**:
1. `TestSyncService_ConcurrentSameWallet_NoDoubleRecord` - два sync одного кошелька
2. `TestSyncService_ConcurrentDifferentWallets_AllSucceed` - parallel разных

---

### Phase 5: Reconciliation Tests (MEDIUM)

**Файл**: `internal/module/transfer/reconciliation_test.go`

```go
//go:build integration
```

**Тесты**:
1. `TestTransferIn_Reconciliation_AfterMultipleTransfers`
2. `TestTransferOut_Reconciliation_WithGas`
3. `TestInternalTransfer_Reconciliation_BothWallets`

---

## Структура файлов для создания

```
apps/backend/internal/
├── module/transfer/
│   ├── handler_integration_test.go    # NEW
│   └── reconciliation_test.go         # NEW
├── platform/sync/
│   ├── service_test.go                # NEW
│   ├── service_integration_test.go    # NEW
│   └── service_concurrent_test.go     # NEW
└── infra/gateway/alchemy/
    └── client_test.go                 # NEW
```

---

## Критические файлы для модификации

Нет файлов для модификации - только создание новых тестов.

---

## Верификация

После реализации:

```bash
# Unit tests
cd apps/backend && go test -v ./internal/module/transfer/...
cd apps/backend && go test -v ./internal/platform/sync/...
cd apps/backend && go test -v ./internal/infra/gateway/alchemy/...

# Integration tests
cd apps/backend && go test -v -tags=integration ./internal/module/transfer/...
cd apps/backend && go test -v -tags=integration ./internal/platform/sync/...

# All with race detector
cd apps/backend && go test -v -tags=integration -race ./...
```

---

## Security Checklist (из ledger-development skill)

Для новых интеграционных тестов проверить:

- [ ] Uses `GetAccountBalanceForUpdate()` для concurrent balance checks (проверяется косвенно через ledger.Service)
- [ ] Checks wallet ownership via `middleware.GetUserIDFromContext(ctx)` ✅ (уже в handlers)
- [ ] Rejects negative amounts ✅ (уже в handlers)
- [ ] Rejects future dates ✅ (уже в handlers)
- [ ] Rejects unbalanced entries ✅ (уже тестируется)
- [ ] Validates all required fields ✅ (уже тестируется)
- [ ] Uses parameterized queries (infra layer)
- [ ] Entries are IMMUTABLE ✅ (ledger core)

---

## Порядок реализации

1. **Phase 1** - Integration tests transfer (1-2 часа)
2. **Phase 2** - Sync service tests (2-3 часа)
3. **Phase 3** - Alchemy client tests (1 час)
4. **Phase 4** - Concurrent sync tests (1 час)
5. **Phase 5** - Reconciliation tests (1 час)

**Total**: ~6-8 часов работы
