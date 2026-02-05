# Fix: Интеграция AssetService в Sync Processor

## Проблема

Sync processor не получает USD rate для транзакций:
- `sync.NewService` не принимает `assetSvc`
- `processor.recordIncomingTransfer/recordOutgoingTransfer/recordInternalTransfer` не добавляют `usd_rate` в data
- Handlers получают `usd_rate = 0`, что делает USD value бесполезным

## Решение

Добавить AssetService в sync processor для получения текущих цен при записи транзакций.

**Выбор источника цен**: CoinGecko через существующий `asset.Service`.

> **Примечание**: Alchemy также предлагает [Prices API](https://www.alchemy.com/docs/reference/prices-api-quickstart) (300 req/hr бесплатно), но выбран CoinGecko как уже реализованный и протестированный источник.

### Ключевая сложность

Transfer использует `AssetSymbol` (ETH, USDC), но `asset.Service.GetCurrentPriceByCoinGeckoID()` требует CoinGecko ID (ethereum, usd-coin). Нужен маппинг.

**Решение**: Создать `SyncAssetAdapter` который:
1. Получает asset по символу через `assetRepo.GetBySymbol()`
2. Извлекает `CoinGeckoID`
3. Вызывает `assetSvc.GetCurrentPriceByCoinGeckoID()`

---

## Изменения

### 1. Обновить интерфейс AssetService в port.go

**Файл**: `internal/platform/sync/port.go`

```go
// AssetService defines asset operations for sync
type AssetService interface {
    // GetPriceBySymbol returns the current USD price for an asset by symbol (scaled by 10^8)
    // Returns nil if price unavailable (graceful degradation)
    GetPriceBySymbol(ctx context.Context, symbol string, chainID int64) (*big.Int, error)
}
```

Удалить неиспользуемые `GetOrCreateAsset` и `GetOrCreateAssetParams`.

---

### 2. Создать SyncAssetAdapter

**Новый файл**: `internal/platform/sync/asset_adapter.go`

```go
package sync

import (
    "context"
    "math/big"

    "github.com/kislikjeka/moontrack/internal/platform/asset"
)

// SyncAssetAdapter adapts asset.Service for sync operations
type SyncAssetAdapter struct {
    assetSvc *asset.Service
}

// NewSyncAssetAdapter creates a new adapter
func NewSyncAssetAdapter(assetSvc *asset.Service) *SyncAssetAdapter {
    return &SyncAssetAdapter{assetSvc: assetSvc}
}

// GetPriceBySymbol returns the current USD price for an asset by symbol
// Maps symbol to CoinGecko ID and fetches price
func (a *SyncAssetAdapter) GetPriceBySymbol(ctx context.Context, symbol string, chainID int64) (*big.Int, error) {
    // Native assets have known CoinGecko IDs
    coinGeckoID := a.getNativeCoinGeckoID(symbol, chainID)
    if coinGeckoID == "" {
        // For ERC-20 tokens, try to find by symbol
        // This is a best-effort lookup
        assets, err := a.assetSvc.GetAssetsBySymbol(ctx, symbol)
        if err != nil || len(assets) == 0 {
            return nil, nil // Price unavailable - graceful degradation
        }
        coinGeckoID = assets[0].CoinGeckoID
    }

    if coinGeckoID == "" {
        return nil, nil
    }

    price, err := a.assetSvc.GetCurrentPriceByCoinGeckoID(ctx, coinGeckoID)
    if err != nil {
        return nil, nil // Graceful degradation
    }

    return price, nil
}

// getNativeCoinGeckoID returns CoinGecko ID for native chain assets
func (a *SyncAssetAdapter) getNativeCoinGeckoID(symbol string, chainID int64) string {
    switch symbol {
    case "ETH":
        return "ethereum"
    case "MATIC":
        return "matic-network"
    case "AVAX":
        return "avalanche-2"
    case "BNB":
        return "binancecoin"
    default:
        return ""
    }
}
```

---

### 3. Добавить AssetService в Processor

**Файл**: `internal/platform/sync/processor.go`

**Изменить структуру:**
```go
type Processor struct {
    walletRepo   WalletRepository
    ledgerSvc    LedgerService
    assetSvc     AssetService  // NEW
    logger       *slog.Logger
    addressCache map[string][]uuid.UUID
}
```

**Изменить конструктор:**
```go
func NewProcessor(
    walletRepo WalletRepository,
    ledgerSvc LedgerService,
    assetSvc AssetService,  // NEW
    logger *slog.Logger,
) *Processor {
    return &Processor{
        walletRepo:   walletRepo,
        ledgerSvc:    ledgerSvc,
        assetSvc:     assetSvc,  // NEW
        logger:       logger,
        addressCache: make(map[string][]uuid.UUID),
    }
}
```

**Добавить helper метод:**
```go
// getTransferUSDRate returns the USD rate for a transfer asset
// Returns "0" if price unavailable (graceful degradation)
func (p *Processor) getTransferUSDRate(ctx context.Context, symbol string, chainID int64) string {
    if p.assetSvc == nil {
        return "0"
    }

    price, err := p.assetSvc.GetPriceBySymbol(ctx, symbol, chainID)
    if err != nil || price == nil {
        p.logger.Debug("price unavailable for asset", "symbol", symbol, "chain_id", chainID)
        return "0"
    }

    return price.String()
}
```

**Обновить recordIncomingTransfer:**
```go
func (p *Processor) recordIncomingTransfer(ctx context.Context, w *wallet.Wallet, transfer Transfer) error {
    // Get USD rate
    usdRate := p.getTransferUSDRate(ctx, transfer.AssetSymbol, transfer.ChainID)

    data := map[string]interface{}{
        "wallet_id":        w.ID.String(),
        "asset_id":         transfer.AssetSymbol,
        "decimals":         transfer.Decimals,
        "amount":           money.NewBigInt(transfer.Amount).String(),
        "usd_rate":         usdRate,  // NEW
        "chain_id":         transfer.ChainID,
        // ... rest unchanged
    }
    // ...
}
```

**Аналогично обновить recordOutgoingTransfer и recordInternalTransfer.**

Для recordOutgoingTransfer и recordInternalTransfer также добавить `gas_usd_rate` для нативного токена.

---

### 4. Обновить Service

**Файл**: `internal/platform/sync/service.go`

**Изменить конструктор:**
```go
func NewService(
    config *Config,
    blockchainClient BlockchainClient,
    walletRepo WalletRepository,
    ledgerSvc LedgerService,
    assetSvc AssetService,  // NEW
    logger *slog.Logger,
) *Service {
    // ...
    processor := NewProcessor(walletRepo, ledgerSvc, assetSvc, logger)  // Updated
    // ...
}
```

---

### 5. Обновить main.go

**Файл**: `cmd/api/main.go`

```go
// Создать sync service (если Alchemy ключ настроен)
if cfg.AlchemyAPIKey != "" {
    // ... existing code ...

    // Create asset adapter for sync
    syncAssetAdapter := sync.NewSyncAssetAdapter(assetSvc)  // NEW

    syncSvc = sync.NewService(
        syncConfig,
        blockchainClient,
        walletRepo,
        ledgerSvc,
        syncAssetAdapter,  // NEW
        log.Logger,
    )
}
```

---

## Файлы для изменения

| Файл | Изменение |
|------|-----------|
| `internal/platform/sync/port.go` | Упростить интерфейс AssetService |
| `internal/platform/sync/asset_adapter.go` | NEW - adapter для маппинга symbol → price |
| `internal/platform/sync/processor.go` | Добавить assetSvc, helper, обновить 3 метода |
| `internal/platform/sync/service.go` | Добавить assetSvc в конструктор |
| `cmd/api/main.go` | Создать adapter, передать в NewService |

---

## Верификация

1. **Build**: `go build ./...`
2. **Tests**: `go test ./internal/module/transfer/... ./internal/platform/sync/...`
3. **Manual check**: Убедиться что при sync транзакции получают ненулевой usd_rate для известных токенов (ETH, MATIC, etc.)

---

## Отложенные задачи (не в этом PR)

Следующие тесты требуются по ledger-development skill, но выходят за рамки этого исправления:

1. Integration tests с TestContainers для sync service
2. Concurrent tests для параллельной синхронизации кошельков
3. Idempotency tests для обработки дубликатов
