# Backend Go Implementation Plan: Zerion Integration + Lot-Based Cost Basis

## Codebase Analysis Summary

### Current Architecture

```
cmd/api/main.go                        ← DI, handler registration, server startup
internal/
├── ledger/
│   ├── model.go                       ← Transaction, Entry, Account, AccountBalance, TransactionType
│   ├── handler.go                     ← TransactionHandler[T], Handler, Registry
│   ├── service.go                     ← RecordTransaction(), accountResolver, transactionCommitter
│   ├── port.go                        ← Repository interface
│   └── errors.go                      ← Domain errors
├── platform/
│   ├── sync/
│   │   ├── port.go                    ← BlockchainClient, WalletRepository, AssetService interfaces
│   │   ├── processor.go              ← Transfer classification (incoming/outgoing/internal) + ledger recording
│   │   ├── service.go                ← Background sync loop with semaphore concurrency
│   │   ├── config.go                 ← Sync config (poll interval, max blocks, etc.)
│   │   └── asset_adapter.go          ← Maps symbol → CoinGecko ID → price
│   ├── wallet/
│   │   ├── model.go                   ← Wallet struct (SyncStatus, LastSyncBlock, LastSyncAt)
│   │   └── port.go / service.go
│   └── asset/
│       └── service.go                 ← Asset + price service
├── module/
│   ├── transfer/
│   │   ├── handler_in.go              ← TransferInHandler: DEBIT wallet + CREDIT income
│   │   ├── handler_out.go             ← TransferOutHandler: DEBIT expense + CREDIT wallet (+ gas)
│   │   ├── handler_internal.go        ← InternalTransferHandler: DEBIT dest + CREDIT source (+ gas)
│   │   └── model.go                   ← TransferInTransaction, TransferOutTransaction, etc.
│   ├── adjustment/
│   │   └── handler.go                 ← AssetAdjustmentHandler
│   ├── portfolio/
│   │   └── service.go                 ← PortfolioService (balances + USD values)
│   └── transactions/
│       └── service.go                 ← TransactionService (enriched read views)
├── transport/httpapi/
│   ├── router.go                      ← Chi router with all routes
│   └── handler/                       ← HTTP handlers (auth, wallet, tx, portfolio, asset)
└── infra/
    ├── gateway/
    │   ├── alchemy/                   ← Alchemy JSON-RPC client + SyncClientAdapter
    │   └── coingecko/                 ← CoinGecko price provider
    ├── postgres/                      ← Repos (ledger, wallet, asset, price)
    └── redis/                         ← Price caching
```

### Key Constraints

1. **TransactionType.IsValid()** is hardcoded in `model.go` (switch on known types). Registry.Register() calls IsValid() — new types will be rejected. Must be made registry-driven.
2. **VerifyBalance()** checks global SUM(debit) = SUM(credit). Swap entries need clearing account to satisfy this.
3. **accounts.type CHECK constraint** in DB only allows: `CRYPTO_WALLET`, `INCOME`, `EXPENSE`, `GAS_FEE`. Must add `CLEARING`.
4. **Sync pipeline** is block-based (Alchemy). Must switch to time-based (Zerion).
5. **Entry metadata** carries `account_code`, `wallet_id`, `chain_id` for account resolution.
6. **DB transaction isolation**: `transactionCommitter.commit()` uses `BeginTx/CommitTx` with `FOR UPDATE` locks on account_balances.

---

## Phase 0: Hotfix — TransferCategoriesForChain

**Goal**: Exclude `internal` category on L2 chains where Alchemy doesn't support it.

### Problem

`TransferCategoriesForChain()` in `types.go` only maps `chainsWithInternalSupport` for chains 1 and 137. This is already correct — L2 chains (42161, 10, 8453) get `[external, erc20]` without `internal`. However, the hotfix mentioned in the ADR suggests verifying this is working correctly and adding tests.

### File: `apps/backend/internal/infra/gateway/alchemy/types.go`

**Changes**: None needed — current implementation already excludes `internal` on L2. But add a test to verify.

### File: `apps/backend/internal/infra/gateway/alchemy/client_test.go` (new assertions)

```go
func TestTransferCategoriesForChain_L2_ExcludesInternal(t *testing.T) {
    l2Chains := []int64{42161, 10, 8453, 43114, 56}
    for _, chainID := range l2Chains {
        cats := TransferCategoriesForChain(chainID)
        assert.NotContains(t, cats, CategoryInternal,
            "chain %d should not include internal category", chainID)
        assert.Contains(t, cats, CategoryExternal)
        assert.Contains(t, cats, CategoryERC20)
    }
}

func TestTransferCategoriesForChain_L1_IncludesInternal(t *testing.T) {
    l1Chains := []int64{1, 137}
    for _, chainID := range l1Chains {
        cats := TransferCategoriesForChain(chainID)
        assert.Contains(t, cats, CategoryInternal)
    }
}
```

---

## Phase 1: Zerion Foundation

### 1.1 Zerion Client — `internal/infra/gateway/zerion/`

#### File: `internal/infra/gateway/zerion/types.go`

Zerion API response types mapped from their REST API.

```go
package zerion

import "time"

// TransactionResponse is the top-level response from GET /wallets/{address}/transactions/
type TransactionResponse struct {
    Links Links              `json:"links"`
    Data  []TransactionData  `json:"data"`
}

type Links struct {
    Self string `json:"self"`
    Next string `json:"next"` // pagination cursor
}

type TransactionData struct {
    Type       string               `json:"type"` // "transactions"
    ID         string               `json:"id"`   // Zerion transaction ID
    Attributes TransactionAttributes `json:"attributes"`
}

type TransactionAttributes struct {
    OperationType string    `json:"operation_type"` // trade, receive, send, deposit, withdraw, claim, execute, approve
    Hash          string    `json:"hash"`           // blockchain tx hash
    MinedAt       time.Time `json:"mined_at"`       // block timestamp (ISO 8601)
    SentFrom      string    `json:"sent_from"`      // sender address
    SentTo        string    `json:"sent_to"`        // receiver address
    Status        string    `json:"status"`         // confirmed, failed, pending
    Nonce         int       `json:"nonce"`
    Fee           *Fee      `json:"fee"`
    Transfers     []ZTransfer `json:"transfers"`
    Approvals     []Approval  `json:"approvals"`

    // Application info (DeFi protocol)
    ApplicationMetadata *ApplicationMeta `json:"application_metadata"`
}

type Fee struct {
    FungibleInfo *FungibleInfo `json:"fungible_info"`
    Quantity     Quantity      `json:"quantity"`
    Price        *float64      `json:"price"` // USD price per unit at tx time
    Value        *float64      `json:"value"` // total USD value
}

type ZTransfer struct {
    FungibleInfo *FungibleInfo `json:"fungible_info"`
    Direction    string        `json:"direction"` // "in", "out", "self"
    Quantity     Quantity      `json:"quantity"`
    Price        *float64      `json:"price"` // USD per unit at tx time
    Value        *float64      `json:"value"` // total USD value
    Sender       string        `json:"sender"`
    Recipient    string        `json:"recipient"`
}

type FungibleInfo struct {
    Name          string         `json:"name"`
    Symbol        string         `json:"symbol"`
    Icon          *IconInfo      `json:"icon"`
    Implementations map[string]Implementation `json:"implementations"` // chain → contract
}

type IconInfo struct {
    URL string `json:"url"`
}

type Implementation struct {
    Address  string `json:"address"`  // contract address
    Decimals int    `json:"decimals"` // token decimals
}

type Quantity struct {
    Int      string  `json:"int"`      // base units as string (USE THIS)
    Decimals int     `json:"decimals"`
    Float    float64 `json:"float"`    // NEVER USE for financial calculations
    Numeric  string  `json:"numeric"`
}

type Approval struct {
    FungibleInfo *FungibleInfo `json:"fungible_info"`
    Quantity     Quantity      `json:"quantity"`
    Sender       string        `json:"sender"`
}

type ApplicationMeta struct {
    Name string `json:"name"` // "Uniswap V3", "GMX", etc.
    Icon *IconInfo `json:"icon"`
}

// Chain ID mapping: Zerion string → EVM numeric
var ZerionChainToID = map[string]int64{
    "ethereum":            1,
    "polygon":             137,
    "arbitrum":            42161,
    "optimism":            10,
    "base":                8453,
    "avalanche":           43114,
    "binance-smart-chain": 56,
}

var IDToZerionChain = map[int64]string{
    1:     "ethereum",
    137:   "polygon",
    42161: "arbitrum",
    10:    "optimism",
    8453:  "base",
    43114: "avalanche",
    56:    "binance-smart-chain",
}
```

#### File: `internal/infra/gateway/zerion/client.go`

HTTP client for Zerion API. Uses API key in `Authorization: Basic` header (base64-encoded).

```go
package zerion

type Client struct {
    apiKey     string
    httpClient *http.Client
    baseURL    string
}

func NewClient(apiKey string) *Client

// GetTransactions fetches decoded transactions for a wallet since a given time.
// Handles pagination via Links.Next.
// Query params: filter[chain_ids]={chainID}&filter[min_mined_at]={sinceISO}&filter[asset_types]=fungible
func (c *Client) GetTransactions(ctx context.Context, address string, chainID string, since time.Time) ([]TransactionData, error)

// GetPositions fetches current DeFi positions for a wallet.
func (c *Client) GetPositions(ctx context.Context, address string, chainID string) ([]PositionData, error)
```

Key details:
- Base URL: `https://api.zerion.io/v1`
- Auth: `Authorization: Basic {base64(apiKey + ":")}`
- Rate limiting: Respect 429 with exponential backoff (max 3 retries)
- Pagination: Follow `links.next` until empty
- Request timeout: 30s

#### File: `internal/infra/gateway/zerion/adapter.go`

Converts Zerion API types → domain types from `sync/port.go`.

```go
package zerion

// SyncAdapter adapts Zerion client to sync.TransactionDataProvider interface
type SyncAdapter struct {
    client *Client
}

func NewSyncAdapter(client *Client) *SyncAdapter

// Implements sync.TransactionDataProvider
func (a *SyncAdapter) GetTransactions(ctx context.Context, address string, chainID int64, since time.Time) ([]sync.Transaction, error) {
    // 1. Convert chainID int64 → Zerion chain string (IDToZerionChain)
    // 2. Call client.GetTransactions()
    // 3. Convert each TransactionData → sync.Transaction using convertTransaction()
}

func (a *SyncAdapter) convertTransaction(td TransactionData, chainID int64) (sync.Transaction, error) {
    // Convert TransactionAttributes → sync.Transaction:
    //   - OperationType string → sync.OperationType
    //   - Transfers → []sync.Transfer with amount from Quantity.Int (big.Int)
    //   - Fee → sync.Fee
    //   - USD prices: multiply float64 by 1e8 → big.Int (scaled)
    //   - Decimals from FungibleInfo.Implementations[chain].Decimals
    //   - Contract address from FungibleInfo.Implementations[chain].Address
}
```

### 1.2 Domain Types — `internal/platform/sync/port.go`

Add new `TransactionDataProvider` interface and domain types alongside existing `BlockchainClient`.

```go
// --- New Zerion-based types ---

type OperationType string

const (
    OpTrade    OperationType = "trade"
    OpDeposit  OperationType = "deposit"
    OpWithdraw OperationType = "withdraw"
    OpClaim    OperationType = "claim"
    OpReceive  OperationType = "receive"
    OpSend     OperationType = "send"
    OpExecute  OperationType = "execute"
    OpApprove  OperationType = "approve"
    OpMint     OperationType = "mint"
    OpBurn     OperationType = "burn"
)

// Transaction represents a decoded blockchain transaction from Zerion
type Transaction struct {
    ID            string         // Zerion transaction ID
    TxHash        string         // Blockchain tx hash
    ChainID       int64
    OperationType OperationType
    Protocol      string         // "Uniswap V3", "GMX", "" for simple transfers
    Transfers     []Transfer     // (reuse existing Transfer struct with additions)
    Fee           *Fee
    MinedAt       time.Time
    Status        string         // "confirmed", "failed"
}

// Fee represents gas fee from Zerion
type Fee struct {
    AssetSymbol string
    Amount      *big.Int
    Decimals    int
    USDPrice    *big.Int // scaled by 10^8
}

// TransactionDataProvider is the port for decoded transaction data (Zerion)
type TransactionDataProvider interface {
    GetTransactions(ctx context.Context, address string, chainID int64, since time.Time) ([]Transaction, error)
}
```

### 1.3 Make TransactionType Registry-Driven

#### File: `internal/ledger/model.go`

**Change**: Make `IsValid()` accept registry-known types in addition to hardcoded ones. The simplest approach: add new constants and update the switch.

```go
// Add new transaction type constants
const (
    // ... existing types ...
    TxTypeSwap          TransactionType = "swap"
    TxTypeDeFiDeposit   TransactionType = "defi_deposit"
    TxTypeDeFiWithdraw  TransactionType = "defi_withdraw"
    TxTypeDeFiClaim     TransactionType = "defi_claim"
)

// Update AllTransactionTypes()
func AllTransactionTypes() []TransactionType {
    return []TransactionType{
        TxTypeTransferIn,
        TxTypeTransferOut,
        TxTypeInternalTransfer,
        TxTypeManualIncome,
        TxTypeManualOutcome,
        TxTypeAssetAdjustment,
        TxTypeSwap,
        TxTypeDeFiDeposit,
        TxTypeDeFiWithdraw,
        TxTypeDeFiClaim,
    }
}

// Update IsValid()
func (t TransactionType) IsValid() bool {
    switch t {
    case TxTypeTransferIn, TxTypeTransferOut, TxTypeInternalTransfer,
        TxTypeManualIncome, TxTypeManualOutcome, TxTypeAssetAdjustment,
        TxTypeSwap, TxTypeDeFiDeposit, TxTypeDeFiWithdraw, TxTypeDeFiClaim:
        return true
    }
    return false
}

// Update Label()
func (t TransactionType) Label() string {
    switch t {
    // ... existing cases ...
    case TxTypeSwap:
        return "Swap"
    case TxTypeDeFiDeposit:
        return "DeFi Deposit"
    case TxTypeDeFiWithdraw:
        return "DeFi Withdraw"
    case TxTypeDeFiClaim:
        return "DeFi Claim"
    default:
        return "Unknown"
    }
}

// Add new AccountType
const (
    // ... existing types ...
    AccountTypeClearing AccountType = "CLEARING" // Swap clearing account
)

// Update Account.Validate() to accept CLEARING type
```

### 1.4 Classification Logic — `internal/platform/sync/classifier.go` (new file)

```go
package sync

import "github.com/kislikjeka/moontrack/internal/ledger"

// Classifier maps Zerion operation types to ledger transaction types
type Classifier struct{}

func NewClassifier() *Classifier

func (c *Classifier) Classify(tx Transaction) ledger.TransactionType {
    switch tx.OperationType {
    case OpReceive:
        return ledger.TxTypeTransferIn
    case OpSend:
        return ledger.TxTypeTransferOut
    case OpTrade:
        return ledger.TxTypeSwap
    case OpDeposit, OpMint:
        return ledger.TxTypeDeFiDeposit
    case OpWithdraw, OpBurn:
        return ledger.TxTypeDeFiWithdraw
    case OpClaim:
        return ledger.TxTypeDeFiClaim
    case OpExecute:
        return c.classifyExecute(tx)
    case OpApprove:
        return "" // skip - no asset movement
    default:
        return "" // skip unknown
    }
}

func (c *Classifier) classifyExecute(tx Transaction) ledger.TransactionType {
    hasIn := false
    hasOut := false
    for _, t := range tx.Transfers {
        if t.Direction == DirectionIn { hasIn = true }
        if t.Direction == DirectionOut { hasOut = true }
    }
    switch {
    case hasIn && hasOut:
        return ledger.TxTypeSwap
    case hasIn:
        return ledger.TxTypeTransferIn
    case hasOut:
        return ledger.TxTypeTransferOut
    default:
        return "" // no transfers, skip
    }
}
```

### 1.5 New Zerion-Based Processor — `internal/platform/sync/zerion_processor.go` (new file)

Replaces the Alchemy-based `Processor` for the main sync pipeline.

```go
package sync

// ZerionProcessor handles Zerion transaction classification and ledger recording
type ZerionProcessor struct {
    walletRepo   WalletRepository
    ledgerSvc    LedgerService
    assetSvc     AssetService
    classifier   *Classifier
    logger       *slog.Logger
    addressCache map[string][]uuid.UUID
}

func NewZerionProcessor(walletRepo WalletRepository, ledgerSvc LedgerService, assetSvc AssetService, logger *slog.Logger) *ZerionProcessor

func (p *ZerionProcessor) ProcessTransaction(ctx context.Context, w *wallet.Wallet, tx Transaction) error {
    // 1. Classify: classifier.Classify(tx) → TransactionType
    // 2. Skip if empty (approve, unknown)
    // 3. Check for internal transfer: if receive/send, check counterparty
    // 4. Build raw data map from Zerion Transaction (type-specific)
    // 5. Call ledgerSvc.RecordTransaction() with idempotency (source="zerion", external_id="zerion_{tx.ID}")
    // 6. Handle duplicate errors silently
}

func (p *ZerionProcessor) buildSwapData(w *wallet.Wallet, tx Transaction) map[string]interface{} {
    // Extract sold/bought transfers from tx.Transfers
    // Map amounts from Quantity.Int → string (for money.BigInt)
    // Map USD prices from float64 → scaled big.Int string
}

func (p *ZerionProcessor) buildTransferInData(w *wallet.Wallet, tx Transaction) map[string]interface{} {
    // Similar to existing processor.recordIncomingTransfer()
    // But data comes from Zerion Transfer, not Alchemy Transfer
}

// ... similar builders for transfer_out, defi_deposit, defi_withdraw, defi_claim
```

### 1.6 Updated Sync Service — `internal/platform/sync/service.go`

**Changes to existing `service.go`**:

- Add `zerionProvider TransactionDataProvider` field
- Add `zerionProcessor *ZerionProcessor` field
- Modify `syncWallet()` to use Zerion time-based sync instead of Alchemy block-based
- Use `wallet.LastSyncAt` (already exists in model) as cursor instead of `wallet.LastSyncBlock`

```go
// Updated NewService signature
func NewService(
    config *Config,
    zerionProvider TransactionDataProvider, // NEW: replaces blockchainClient
    walletRepo WalletRepository,
    ledgerSvc LedgerService,
    assetSvc AssetService,
    logger *slog.Logger,
) *Service

// Updated syncWallet
func (s *Service) syncWallet(ctx context.Context, w *wallet.Wallet) error {
    // 1. Mark as syncing
    // 2. Determine cursor: w.LastSyncAt (or time.Time{} for initial sync)
    // 3. Zerion: GetTransactions(ctx, w.Address, w.ChainID, since)
    // 4. For each tx: zerionProcessor.ProcessTransaction(ctx, w, tx)
    // 5. Update cursor: walletRepo.SetSyncCompleted(ctx, w.ID, maxMinedAt)
}
```

### 1.7 Updated `WalletRepository` interface in `sync/port.go`

Add time-based sync completion method:

```go
// SetSyncCompletedAt marks wallet as synced with time-based cursor
SetSyncCompletedAt(ctx context.Context, walletID uuid.UUID, syncAt time.Time) error
```

And implement in `infra/postgres/wallet_repo.go`:

```go
func (r *WalletRepository) SetSyncCompletedAt(ctx context.Context, walletID uuid.UUID, syncAt time.Time) error {
    query := `
        UPDATE wallets
        SET sync_status = $1, last_sync_at = $2, sync_error = NULL, updated_at = $3
        WHERE id = $4
    `
    // ...
}
```

### 1.8 Updated `cmd/api/main.go`

```go
// Replace Alchemy sync setup with Zerion
var syncSvc *sync.Service
if cfg.ZerionAPIKey != "" {
    zerionClient := zerion.NewClient(cfg.ZerionAPIKey)
    zerionAdapter := zerion.NewSyncAdapter(zerionClient)

    syncConfig := &sync.Config{
        PollInterval:      cfg.SyncPollInterval,
        ConcurrentWallets: 3,
        Enabled:           true,
    }
    syncSvc = sync.NewService(syncConfig, zerionAdapter, walletRepo, ledgerSvc, syncAssetAdapter, log.Logger)
}
```

### 1.9 Config Update — `pkg/config/config.go`

Add `ZerionAPIKey` field:

```go
type Config struct {
    // ... existing ...
    ZerionAPIKey string
}

// In Load():
ZerionAPIKey: getEnv("ZERION_API_KEY", ""),
```

### 1.10 Environment Variables — `apps/backend/.env.example`

Add:
```
ZERION_API_KEY=your-zerion-api-key
```

---

## Phase 1.5: Lot-Based Cost Basis System

### 1.5.1 Database Migration: `000008_zerion_sync.up.sql`

```sql
-- 1. Add CLEARING to accounts type constraint
ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_type_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_type_check
    CHECK (type IN ('CRYPTO_WALLET', 'INCOME', 'EXPENSE', 'GAS_FEE', 'CLEARING'));

-- 2. Ensure last_sync_at column exists (already added in migration 007)
-- No changes needed — wallet.LastSyncAt already exists
```

### 1.5.2 Database Migration: `000009_tax_lots.up.sql`

```sql
-- Tax lots table
CREATE TABLE tax_lots (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id              UUID NOT NULL REFERENCES transactions(id),
    account_id                  UUID NOT NULL REFERENCES accounts(id),
    asset                       TEXT NOT NULL,

    quantity_acquired           NUMERIC(78,0) NOT NULL,
    quantity_remaining          NUMERIC(78,0) NOT NULL,
    acquired_at                 TIMESTAMPTZ NOT NULL,

    -- Auto-calculated cost basis (USD scaled by 10^8)
    auto_cost_basis_per_unit    NUMERIC(78,0) NOT NULL,
    auto_cost_basis_source      TEXT NOT NULL, -- 'swap_price' | 'fmv_at_transfer' | 'linked_transfer'

    -- User override (nullable, USD scaled by 10^8)
    override_cost_basis_per_unit NUMERIC(78,0),
    override_reason              TEXT,
    override_at                  TIMESTAMPTZ,

    -- Link to source lot (for internal transfers)
    linked_source_lot_id UUID REFERENCES tax_lots(id),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT positive_quantities CHECK (
        quantity_acquired > 0 AND quantity_remaining >= 0
        AND quantity_remaining <= quantity_acquired
    )
);

-- FIFO query index: open lots for an account+asset ordered by acquisition time
CREATE INDEX idx_tax_lots_fifo ON tax_lots (account_id, asset, acquired_at)
    WHERE quantity_remaining > 0;

CREATE INDEX idx_tax_lots_transaction ON tax_lots (transaction_id);
CREATE INDEX idx_tax_lots_account_asset ON tax_lots (account_id, asset);

-- Lot disposals table
CREATE TABLE lot_disposals (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id      UUID NOT NULL REFERENCES transactions(id),
    lot_id              UUID NOT NULL REFERENCES tax_lots(id),

    quantity_disposed   NUMERIC(78,0) NOT NULL,
    proceeds_per_unit   NUMERIC(78,0) NOT NULL, -- USD scaled by 10^8
    disposal_type       TEXT NOT NULL DEFAULT 'sale', -- 'sale' | 'internal_transfer'

    disposed_at         TIMESTAMPTZ NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT positive_disposal CHECK (quantity_disposed > 0),
    CONSTRAINT valid_disposal_type CHECK (disposal_type IN ('sale', 'internal_transfer'))
);

CREATE INDEX idx_lot_disposals_transaction ON lot_disposals (transaction_id);
CREATE INDEX idx_lot_disposals_lot ON lot_disposals (lot_id);

-- Override history table (audit trail)
CREATE TABLE lot_override_history (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lot_id              UUID NOT NULL REFERENCES tax_lots(id),
    previous_cost_basis NUMERIC(78,0), -- USD scaled by 10^8
    new_cost_basis      NUMERIC(78,0), -- USD scaled by 10^8
    reason              TEXT,
    changed_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_lot_override_history_lot ON lot_override_history (lot_id);

-- Effective cost basis view (priority: override > linked > auto)
CREATE VIEW tax_lots_effective AS
SELECT
    tl.*,
    COALESCE(
        tl.override_cost_basis_per_unit,
        COALESCE(
            source.override_cost_basis_per_unit,
            source.auto_cost_basis_per_unit
        ),
        tl.auto_cost_basis_per_unit
    ) AS effective_cost_basis_per_unit
FROM tax_lots tl
LEFT JOIN tax_lots source ON tl.linked_source_lot_id = source.id;

-- WAC materialized view
CREATE MATERIALIZED VIEW position_wac AS
SELECT
    tl.account_id,
    tl.asset,
    SUM(tl.quantity_remaining) AS total_quantity,
    CASE
        WHEN SUM(tl.quantity_remaining) = 0 THEN 0
        ELSE SUM(tl.quantity_remaining * tle.effective_cost_basis_per_unit)
            / SUM(tl.quantity_remaining)
    END AS weighted_avg_cost
FROM tax_lots tl
JOIN tax_lots_effective tle ON tl.id = tle.id
WHERE tl.quantity_remaining > 0
GROUP BY tl.account_id, tl.asset;

CREATE UNIQUE INDEX idx_position_wac_pk ON position_wac (account_id, asset);
```

### 1.5.3 Down Migration: `000009_tax_lots.down.sql`

```sql
DROP MATERIALIZED VIEW IF EXISTS position_wac;
DROP VIEW IF EXISTS tax_lots_effective;
DROP TABLE IF EXISTS lot_override_history;
DROP TABLE IF EXISTS lot_disposals;
DROP TABLE IF EXISTS tax_lots;
```

### 1.5.4 TaxLot Service — `internal/platform/taxlot/`

#### File: `internal/platform/taxlot/model.go`

```go
package taxlot

type TaxLot struct {
    ID                       uuid.UUID
    TransactionID            uuid.UUID
    AccountID                uuid.UUID
    Asset                    string
    QuantityAcquired         *big.Int
    QuantityRemaining        *big.Int
    AcquiredAt               time.Time
    AutoCostBasisPerUnit     *big.Int // USD scaled by 10^8
    AutoCostBasisSource      string   // swap_price | fmv_at_transfer | linked_transfer
    OverrideCostBasisPerUnit *big.Int // nullable
    OverrideReason           *string
    OverrideAt               *time.Time
    LinkedSourceLotID        *uuid.UUID
    CreatedAt                time.Time
}

type LotDisposal struct {
    ID               uuid.UUID
    TransactionID    uuid.UUID
    LotID            uuid.UUID
    QuantityDisposed *big.Int
    ProceedsPerUnit  *big.Int // USD scaled by 10^8
    DisposalType     string   // sale | internal_transfer
    DisposedAt       time.Time
    CreatedAt        time.Time
}

type CostBasisSource string
const (
    SourceSwapPrice      CostBasisSource = "swap_price"
    SourceFMVAtTransfer  CostBasisSource = "fmv_at_transfer"
    SourceLinkedTransfer CostBasisSource = "linked_transfer"
)
```

#### File: `internal/platform/taxlot/port.go`

```go
package taxlot

type Repository interface {
    CreateTaxLot(ctx context.Context, lot *TaxLot) error
    GetOpenLotsFIFO(ctx context.Context, accountID uuid.UUID, asset string) ([]*TaxLot, error)
    UpdateLotRemaining(ctx context.Context, lotID uuid.UUID, newRemaining *big.Int) error
    CreateDisposal(ctx context.Context, disposal *LotDisposal) error

    // For override
    GetTaxLot(ctx context.Context, id uuid.UUID) (*TaxLot, error)
    UpdateOverride(ctx context.Context, lotID uuid.UUID, costBasis *big.Int, reason string) error
    CreateOverrideHistory(ctx context.Context, lotID uuid.UUID, prev, new *big.Int, reason string) error

    // For queries
    GetLotsByAccountAndAsset(ctx context.Context, accountID uuid.UUID, asset string) ([]*TaxLot, error)
    GetDisposalsByTransaction(ctx context.Context, txID uuid.UUID) ([]*LotDisposal, error)

    // WAC
    RefreshWAC(ctx context.Context) error
}
```

#### File: `internal/platform/taxlot/service.go`

```go
package taxlot

type Service struct {
    repo Repository
}

func NewService(repo Repository) *Service

// CreateLot creates a tax lot when an asset is acquired
func (s *Service) CreateLot(ctx context.Context, lot *TaxLot) error

// DisposeFIFO disposes quantity from oldest lots first
// Returns error if insufficient balance
func (s *Service) DisposeFIFO(ctx context.Context, accountID uuid.UUID, asset string,
    quantity *big.Int, proceedsPerUnit *big.Int, txID uuid.UUID,
    disposalType string, disposedAt time.Time) error {
    // 1. GetOpenLotsFIFO (SELECT ... FOR UPDATE)
    // 2. Iterate lots oldest-first
    // 3. For each lot: dispose min(remaining, needed)
    // 4. Create LotDisposal record
    // 5. Update lot.QuantityRemaining
    // 6. If remaining > 0 after all lots: return error
}

// OverrideCostBasis sets a manual cost basis override on a lot
func (s *Service) OverrideCostBasis(ctx context.Context, lotID uuid.UUID, newCostBasis *big.Int, reason string) error {
    // 1. Get current lot
    // 2. Record in override history
    // 3. Update lot override fields
}
```

### 1.5.5 TaxLot Repository — `internal/infra/postgres/taxlot_repo.go` (new file)

Implements `taxlot.Repository` using pgx. Key method:

```go
func (r *TaxLotRepository) GetOpenLotsFIFO(ctx context.Context, accountID uuid.UUID, asset string) ([]*taxlot.TaxLot, error) {
    query := `
        SELECT id, transaction_id, account_id, asset,
               quantity_acquired, quantity_remaining, acquired_at,
               auto_cost_basis_per_unit, auto_cost_basis_source,
               override_cost_basis_per_unit, override_reason, override_at,
               linked_source_lot_id, created_at
        FROM tax_lots
        WHERE account_id = $1 AND asset = $2 AND quantity_remaining > 0
        ORDER BY acquired_at ASC
        FOR UPDATE
    `
    // ...
}
```

### 1.5.6 Integration with Ledger Commit Flow

The TaxLot system hooks into the existing flow **after** `transactionCommitter.commit()` succeeds. Two approaches:

**Option A (recommended)**: Add TaxLot creation as a post-commit hook in the `Service.RecordTransaction()` flow.

**Option B**: Have each handler return TaxLot instructions alongside entries.

Going with **Option A** — add a `TaxLotHook` interface:

```go
// In ledger/service.go — add after successful commit
type PostCommitHook interface {
    AfterCommit(ctx context.Context, tx *Transaction) error
}

// In RecordTransaction(), after step 7 (commit):
for _, hook := range s.postCommitHooks {
    if err := hook.AfterCommit(ctx, tx); err != nil {
        // Log error but don't fail the transaction
        // TaxLots can be reconciled later
    }
}
```

The TaxLot hook inspects `tx.Type` and `tx.RawData` to determine:
- Which entries represent asset acquisitions → create TaxLot
- Which entries represent asset disposals → FIFO dispose

---

## Phase 2: DeFi Handlers

### 2.1 SwapHandler — `internal/module/defi/handler_swap.go` (new)

```go
package defi

type SwapHandler struct {
    ledger.BaseHandler
    walletRepo WalletRepository
}

func NewSwapHandler(walletRepo WalletRepository) *SwapHandler {
    return &SwapHandler{
        BaseHandler: ledger.NewBaseHandler(ledger.TxTypeSwap),
        walletRepo:  walletRepo,
    }
}
```

**GenerateEntries logic** (clearing account pattern):

```
// Swap: Sell 1 ETH, Buy 2500 USDC
// 4 entries (+ 2 for gas if present):

DEBIT   wallet.{wallet_id}.USDC           asset_increase    2500_000000    (bought asset)
CREDIT  swap_clearing.{chain_id}          clearing          2500_000000    (mirror bought)
DEBIT   swap_clearing.{chain_id}          clearing          1_000000000000000000  (mirror sold)
CREDIT  wallet.{wallet_id}.ETH            asset_decrease    1_000000000000000000  (sold asset)

// Gas (if present):
DEBIT   gas.{chain_id}.ETH               gas_fee           gas_amount
CREDIT  wallet.{wallet_id}.ETH           asset_decrease    gas_amount
```

**Balance verification**: SUM(debit) = SUM(credit) globally.
- DEBIT 2500_000000 + DEBIT 1e18 + DEBIT gas = CREDIT 2500_000000 + CREDIT 1e18 + CREDIT gas

#### File: `internal/module/defi/model.go`

```go
type SwapTransaction struct {
    WalletID       uuid.UUID     `json:"wallet_id"`
    ChainID        int64         `json:"chain_id"`
    Protocol       string        `json:"protocol"`

    SoldAssetID    string        `json:"sold_asset_id"`
    SoldAmount     *money.BigInt `json:"sold_amount"`
    SoldDecimals   int           `json:"sold_decimals"`
    SoldUSDRate    *money.BigInt `json:"sold_usd_rate"`
    SoldContract   string        `json:"sold_contract"`

    BoughtAssetID  string        `json:"bought_asset_id"`
    BoughtAmount   *money.BigInt `json:"bought_amount"`
    BoughtDecimals int           `json:"bought_decimals"`
    BoughtUSDRate  *money.BigInt `json:"bought_usd_rate"`
    BoughtContract string        `json:"bought_contract"`

    GasAmount      *money.BigInt `json:"gas_amount"`
    GasUSDRate     *money.BigInt `json:"gas_usd_rate"`
    GasAssetID     string        `json:"gas_asset_id"`
    GasDecimals    int           `json:"gas_decimals"`

    TxHash         string        `json:"tx_hash"`
    BlockNumber    int64         `json:"block_number"`
    OccurredAt     time.Time     `json:"occurred_at"`
    UniqueID       string        `json:"unique_id"`
}
```

### 2.2 DeFiDepositHandler — `internal/module/defi/handler_deposit.go`

```
// Deposit: Send 1 ETH + 2500 USDC to LP pool, receive LP token
// Clearing account pattern for multi-asset balance:

DEBIT   wallet.{wallet_id}.{lp_token}   asset_increase    lp_amount     (received LP)
CREDIT  swap_clearing.{chain_id}         clearing          lp_amount     (mirror LP)
DEBIT   swap_clearing.{chain_id}         clearing          eth_amount    (mirror ETH)
CREDIT  wallet.{wallet_id}.ETH          asset_decrease    eth_amount    (sent ETH)
DEBIT   swap_clearing.{chain_id}         clearing          usdc_amount   (mirror USDC)
CREDIT  wallet.{wallet_id}.USDC         asset_decrease    usdc_amount   (sent USDC)
```

### 2.3 DeFiWithdrawHandler — `internal/module/defi/handler_withdraw.go`

Mirror of DeFiDepositHandler. Burns LP tokens, receives underlying assets.

### 2.4 DeFiClaimHandler — `internal/module/defi/handler_claim.go`

```
// Claim: Receive reward tokens
// Simple income pattern (like TransferInHandler):

DEBIT   wallet.{wallet_id}.{reward_asset}    asset_increase    reward_amount
CREDIT  income.defi.{chain_id}.{protocol}    income            reward_amount
```

### 2.5 Handler Registration — `cmd/api/main.go`

After existing handler registrations:

```go
// DeFi handlers
swapHandler := defi.NewSwapHandler(walletRepo)
handlerRegistry.Register(swapHandler)

defiDepositHandler := defi.NewDeFiDepositHandler(walletRepo)
handlerRegistry.Register(defiDepositHandler)

defiWithdrawHandler := defi.NewDeFiWithdrawHandler(walletRepo)
handlerRegistry.Register(defiWithdrawHandler)

defiClaimHandler := defi.NewDeFiClaimHandler(walletRepo)
handlerRegistry.Register(defiClaimHandler)
```

---

## Phase 3: API Layer

### 3.1 New Endpoints

Add to `transport/httpapi/router.go` inside the protected group:

```go
// Tax lots / PnL routes
if cfg.TaxLotHandler != nil {
    r.Get("/lots", cfg.TaxLotHandler.GetLots)           // ?account_id=&asset=
    r.Put("/lots/{id}/override", cfg.TaxLotHandler.OverrideCostBasis)
    r.Get("/pnl/realized", cfg.TaxLotHandler.GetRealizedPnL) // ?account_id=&date_range=
    r.Get("/positions/wac", cfg.TaxLotHandler.GetWAC)       // ?account_id=
}

// DeFi positions
if cfg.PortfolioHandler != nil {
    r.Get("/portfolio/defi-positions", cfg.PortfolioHandler.GetDeFiPositions)
}
```

### 3.2 Transport Handler — `internal/transport/httpapi/handler/taxlot.go` (new)

```go
type TaxLotHandler struct {
    taxLotSvc *taxlot.Service
}

// GetLots returns tax lots for an account+asset, enriched with effective cost basis
// Response shape (TanStack Query compatible):
// { data: [...], meta: { total: N } }

// OverrideCostBasis accepts PUT with { cost_basis_per_unit: "...", reason: "..." }

// GetRealizedPnL computes realized PnL from disposals in date range
// Uses tax_lots_effective view for on-the-fly PnL calculation
```

---

## Migration Order

Migrations must be applied in this order:

1. **`000008_zerion_sync.up.sql`** — Add CLEARING to accounts type CHECK constraint
2. **`000009_tax_lots.up.sql`** — Create tax_lots, lot_disposals, lot_override_history, views

These have no dependencies on each other beyond ordering. Both can be in a single migration if preferred, but separating keeps concerns clean.

**Note**: `wallets.last_sync_at` already exists from migration 007. No additional wallet schema changes needed.

---

## New/Modified File Summary

### New Files (17)

```
internal/infra/gateway/zerion/
├── client.go                          // Zerion HTTP client
├── types.go                           // Zerion API response types
└── adapter.go                         // Zerion → domain type adapter

internal/platform/sync/
├── classifier.go                      // operation_type → TransactionType mapping
└── zerion_processor.go                // Zerion-based transaction processor

internal/platform/taxlot/
├── model.go                           // TaxLot, LotDisposal models
├── port.go                            // Repository interface
└── service.go                         // FIFO disposal, override logic

internal/module/defi/
├── handler_swap.go                    // SwapHandler (clearing account)
├── handler_deposit.go                 // DeFiDepositHandler
├── handler_withdraw.go                // DeFiWithdrawHandler
├── handler_claim.go                   // DeFiClaimHandler
├── model.go                           // SwapTransaction, DeFiDepositTransaction, etc.
└── errors.go                          // DeFi-specific errors

internal/infra/postgres/
└── taxlot_repo.go                     // TaxLot PostgreSQL repository

internal/transport/httpapi/handler/
└── taxlot.go                          // Tax lot HTTP handlers

migrations/
├── 000008_zerion_sync.up.sql          // Account type constraint update
├── 000008_zerion_sync.down.sql
├── 000009_tax_lots.up.sql             // Tax lots schema
└── 000009_tax_lots.down.sql
```

### Modified Files (10)

```
internal/ledger/model.go               // Add TxTypeSwap, TxTypeDeFiDeposit, etc. + AccountTypeClearing
internal/ledger/service.go             // Add PostCommitHook for TaxLot integration
internal/platform/sync/port.go         // Add TransactionDataProvider, OperationType, Transaction types
internal/platform/sync/service.go      // Replace Alchemy pipeline with Zerion
internal/platform/sync/config.go       // Remove block-based config, add Zerion-specific
internal/infra/postgres/wallet_repo.go // Add SetSyncCompletedAt()
internal/transport/httpapi/router.go   // Add /lots, /pnl, /positions routes
cmd/api/main.go                        // Wire Zerion client, DeFi handlers, TaxLot service
pkg/config/config.go                   // Add ZerionAPIKey
apps/backend/.env.example              // Add ZERION_API_KEY
```

### Deprecated (not deleted, just unused by sync)

```
internal/infra/gateway/alchemy/adapter.go    // SyncClientAdapter no longer used by sync
internal/platform/sync/processor.go          // Alchemy-based processor replaced by zerion_processor
```

---

## Testing Strategy

### Unit Tests

1. **Classifier tests** (`classifier_test.go`):
   - Each operation_type maps to correct TransactionType
   - Execute classification with various transfer combinations
   - Approve and unknown types return empty (skip)

2. **SwapHandler tests** (`handler_swap_test.go`):
   - Entries are balanced (SUM debit = SUM credit)
   - Clearing account entries mirror wallet entries
   - Gas fee entries generated when gas present
   - Validation rejects invalid data

3. **DeFi handler tests** (deposit, withdraw, claim):
   - Balanced entries for each type
   - Multi-asset deposit/withdraw with clearing account

4. **FIFO disposal tests** (`service_test.go` in taxlot):
   - Single lot full disposal
   - Multi-lot partial disposal (oldest first)
   - Insufficient balance error
   - Concurrent disposal safety (mock FOR UPDATE)
   - Internal transfer disposal (disposal_type = "internal_transfer")

5. **Override tests**:
   - Override changes effective cost basis
   - Override history recorded
   - PnL recalculates with new cost basis
   - Internal transfer PnL stays 0 regardless of override

### Integration Tests

6. **Sync pipeline end-to-end** (`service_integration_test.go`):
   - Mock Zerion returns various operation_types
   - Verify correct handlers are called
   - Verify ledger transactions are created
   - Verify TaxLots are created for acquisitions
   - Verify FIFO disposals for outgoing transfers/swaps
   - Idempotency: duplicate Zerion tx is silently skipped

7. **Zerion adapter tests** (`adapter_test.go`):
   - Converts API response to domain types correctly
   - Handles null prices gracefully
   - Maps chain IDs correctly
   - Pagination follows links.next

### Test Helpers

- `testutil/zerion_mock.go` — Mock Zerion API server returning test fixtures
- Reuse existing `MockWalletRepository`, `MockLedgerService`, `MockAssetService`
- Add `MockTaxLotRepository` for tax lot tests

---

## Risk Considerations

1. **Zerion API changes**: Pin to v1 API. Types are generated from API docs.
2. **Clearing account balance**: Should always net to zero per transaction. Add a reconciliation check.
3. **FIFO disposal under concurrent syncs**: Different wallets are independent. Same wallet is sequential (existing semaphore).
4. **Large batch syncs**: Initial sync may return hundreds of transactions. Process sequentially, each in own DB transaction.
5. **Price precision**: Zerion returns float64 prices. Multiplying by 1e8 and converting to big.Int loses precision beyond 8 decimal places. Acceptable for USD values.
