## Context

Currently, the transaction list displays raw ledger data (`raw_data`) directly to users, which is opaque and hard to understand. The `GET /transactions` endpoint returns transaction data without human-readable formatting, and there's no endpoint to view transaction details with ledger entries.

The frontend `TransactionList.tsx` renders transactions via a `TransactionItem` component, but lacks meaningful display of asset names, amounts, and wallet context. Users need a clear view of their transactions with the ability to drill down into double-entry accounting details.

Existing architecture:
- **Ledger service** (`internal/core/ledger/`) - low-level double-entry accounting engine
- **Transaction handler** (`internal/api/handlers/transaction_handler.go`) - HTTP layer directly using ledger service
- **Transaction domain** - stores `Transaction` with `Entries` slice, `RawData` as JSONB
- **Transaction type handlers** (`internal/modules/manual_transaction/`, `internal/modules/asset_adjustment/`) - implement `TransactionHandler[T]` interface for validation and entry generation

Current write flow:
```
POST /transactions
    → transaction_handler.CreateTransaction()
    → ledgerService.RecordTransaction(type, data)
    → registry.GetHandler(type)  // finds manual_income, outcome, or adjustment handler
    → handler.Validate()
    → handler.ToEntries()
    → commit to DB
```

The existing transaction type handlers (manual_income, manual_outcome, asset_adjustment) are responsible for:
- Validating transaction-specific business rules
- Generating balanced ledger entries (debit = credit)
- They remain untouched by this change

## Goals / Non-Goals

**Goals:**
- Create a transaction service module that formats transactions for UI consumption
- Add `GET /transactions/:id` endpoint returning transaction with entries
- Extend `GET /transactions` response with human-readable fields (asset_symbol, display_amount, wallet_name)
- Build transaction detail page showing all parameters and ledger entries

**Non-Goals:**
- Modifying ledger core internals or entry immutability
- Adding new transaction types
- Changing double-entry accounting logic
- Real-time transaction updates (WebSockets)

## Decisions

### 1. Transaction Service as READ-ONLY Module

**Decision:** Create `internal/modules/transactions/` with a service layer that wraps ledger queries and adds UI formatting. This service is **read-only** — it does NOT create transactions.

**Architecture with new service:**

```
┌─────────────────────────────────────────────────────────────┐
│                        API Layer                            │
│  ┌───────────────────────────────────────────────────────┐  │
│  │              transaction_handler.go                    │  │
│  │  POST /transactions  │  GET /transactions(:id)        │  │
│  └──────────┬───────────┴────────────┬───────────────────┘  │
└─────────────┼────────────────────────┼──────────────────────┘
              │                        │
              ▼                        ▼
┌─────────────────────┐    ┌─────────────────────────────────┐
│   ledger_service    │    │   transaction_service (NEW)     │
│   (write path)      │    │   (read path + formatting)      │
│                     │    │                                 │
│ RecordTransaction() │    │ ListTransactions() → enrich     │
│ ValidateEntries()   │    │ GetTransaction()  → format      │
│ Commit()            │    │                                 │
└─────────────────────┘    └────────────┬────────────────────┘
         ▲                              │
         │                              │ uses for raw data
         │                              ▼
         │                 ┌─────────────────────┐
         │                 │   ledger_service    │
         │                 │   (read methods)    │
         │                 └─────────────────────┘
         │
    Handler Registry (unchanged)
    ┌────┴────┬────────────┐
    ▼         ▼            ▼
┌────────┐ ┌────────┐ ┌──────────┐
│ income │ │outcome │ │adjustment│   ← These modules are NOT modified
└────────┘ └────────┘ └──────────┘
```

**Separation of concerns:**
- **Write path** (unchanged): `POST /transactions` → `ledger_service` → handler registry → `manual_income`/`outcome`/`adjustment` handlers
- **Read path** (new): `GET /transactions` → `transaction_service` → `ledger_service` (read) → enrich & format → return DTOs

**Rationale:** Keeps ledger core focused on low-level accounting. The transaction service handles:
- Joining wallet names, asset symbols to transactions
- Formatting amounts for display (base units → human readable)
- Aggregating data from multiple sources

Existing transaction type handlers (`manual_income`, `manual_outcome`, `asset_adjustment`) continue to work exactly as before — they are only invoked during transaction creation, not during reads.

**Alternatives considered:**
- *Extend ledger service* — rejected because it violates single responsibility; ledger should remain low-level
- *Format in HTTP handler* — rejected because formatting logic would be duplicated and untestable
- *Modify existing handlers* — rejected; handlers are for write-path validation/entry-generation only

### 2. Centralized Transaction Type Enum

**Problem:** Transaction types are currently defined as local string constants in each handler module:

```go
// income_handler.go
const TransactionTypeManualIncome = "manual_income"

// outcome_handler.go
const TransactionTypeManualOutcome = "manual_outcome"

// asset_adjustment/handler.go
const TransactionTypeAssetAdjustment = "asset_adjustment"
```

This leads to string duplication and potential typos across codebase.

**Decision:** Create a centralized `TransactionType` enum in ledger domain.

```go
// internal/core/ledger/domain/transaction_type.go
type TransactionType string

const (
    TxTypeManualIncome     TransactionType = "manual_income"
    TxTypeManualOutcome    TransactionType = "manual_outcome"
    TxTypeAssetAdjustment  TransactionType = "asset_adjustment"
)

// AllTransactionTypes returns all valid transaction types
func AllTransactionTypes() []TransactionType {
    return []TransactionType{
        TxTypeManualIncome,
        TxTypeManualOutcome,
        TxTypeAssetAdjustment,
    }
}

// IsValid checks if the transaction type is valid
func (t TransactionType) IsValid() bool {
    switch t {
    case TxTypeManualIncome, TxTypeManualOutcome, TxTypeAssetAdjustment:
        return true
    }
    return false
}

// String returns the string representation
func (t TransactionType) String() string {
    return string(t)
}

// Label returns human-readable label for UI
func (t TransactionType) Label() string {
    switch t {
    case TxTypeManualIncome:
        return "Income"
    case TxTypeManualOutcome:
        return "Outcome"
    case TxTypeAssetAdjustment:
        return "Adjustment"
    default:
        return "Unknown"
    }
}
```

**Migration:** Update existing handlers to use the enum:

```go
// income_handler.go - BEFORE
const TransactionTypeManualIncome = "manual_income"

// income_handler.go - AFTER
import ledgerdomain "github.com/kislikjeka/moontrack/internal/core/ledger/domain"
// Use ledgerdomain.TxTypeManualIncome
```

**Rationale:**
- Single source of truth for transaction types
- Compile-time safety — typos caught by compiler
- `Label()` method provides UI-friendly names
- `IsValid()` enables validation without hardcoded strings

### 3. Transaction Readers Pattern (Parsing raw_data)

**Problem:** Each transaction type handler writes different structure to `raw_data`:

```go
// manual_income stores:
raw_data = {"wallet_id": "...", "asset_id": "BTC", "amount": "50000000", "notes": "..."}

// asset_adjustment stores:
raw_data = {"wallet_id": "...", "asset_id": "ETH", "old_balance": "...", "new_balance": "...", "reason": "..."}
```

The transaction service needs to parse these type-specific structures to extract display fields.

**Decision:** Create separate Reader components in the transactions module that import domain types from handler modules. Use the centralized `TransactionType` enum for type matching.

```
internal/modules/transactions/
├── service/
│   └── service.go           # Main service, delegates to readers
├── readers/
│   ├── reader.go            # Reader interface + registry
│   ├── income_reader.go     # Parses manual_income, uses manual_transaction/domain
│   ├── outcome_reader.go    # Parses manual_outcome, uses manual_transaction/domain
│   └── adjustment_reader.go # Parses asset_adjustment, uses asset_adjustment/domain
└── dto/
    └── responses.go         # TransactionListItem, TransactionDetail, etc.
```

**Reader interface and registry:**

```go
// internal/modules/transactions/readers/reader.go
import ledgerdomain "github.com/kislikjeka/moontrack/internal/core/ledger/domain"

type TransactionReader interface {
    // Type returns the transaction type this reader handles
    Type() ledgerdomain.TransactionType

    // ReadForList extracts display fields for list view
    ReadForList(raw map[string]interface{}) (*ListFields, error)

    // ReadForDetail extracts all fields for detail view
    ReadForDetail(raw map[string]interface{}) (*DetailFields, error)
}

type ListFields struct {
    WalletID  uuid.UUID
    AssetID   string
    Amount    *big.Int
    Direction string  // "in", "out", "adjustment"
}

type DetailFields struct {
    ListFields
    Notes       string
    ExtraFields map[string]interface{}  // Type-specific fields for display
}

// ReaderRegistry holds all transaction readers
type ReaderRegistry struct {
    readers map[ledgerdomain.TransactionType]TransactionReader
}

func NewReaderRegistry() *ReaderRegistry {
    r := &ReaderRegistry{
        readers: make(map[ledgerdomain.TransactionType]TransactionReader),
    }

    // Register all readers at creation time
    r.register(&IncomeReader{})
    r.register(&OutcomeReader{})
    r.register(&AdjustmentReader{})

    return r
}

func (r *ReaderRegistry) register(reader TransactionReader) {
    r.readers[reader.Type()] = reader
}

func (r *ReaderRegistry) GetReader(txType ledgerdomain.TransactionType) (TransactionReader, bool) {
    reader, ok := r.readers[txType]
    return reader, ok
}
```

**Example reader implementation:**

```go
// internal/modules/transactions/readers/income_reader.go
import (
    ledgerdomain "github.com/kislikjeka/moontrack/internal/core/ledger/domain"
    incomeDomain "github.com/kislikjeka/moontrack/internal/modules/manual_transaction/domain"
)

type IncomeReader struct{}

func (r *IncomeReader) Type() ledgerdomain.TransactionType {
    return ledgerdomain.TxTypeManualIncome
}

func (r *IncomeReader) ReadForList(raw map[string]interface{}) (*ListFields, error) {
    income, err := incomeDomain.ParseFromRawData(raw)  // Uses domain's parsing logic
    if err != nil {
        return nil, err
    }
    return &ListFields{
        WalletID:  income.WalletID,
        AssetID:   income.AssetID,
        Amount:    income.Amount,
        Direction: "in",
    }, nil
}
```

**Dependencies:**

```
transactions/readers/income_reader.go
    → imports ledger/domain (for TransactionType enum)
    → imports manual_transaction/domain (for ManualIncome struct and parsing)

transactions/readers/adjustment_reader.go
    → imports ledger/domain (for TransactionType enum)
    → imports asset_adjustment/domain (for AssetAdjustment struct and parsing)
```

**Rationale:**
- Handler modules remain unchanged (write-only)
- Domain types are reused — readers import them, no duplication
- Each reader is co-located with service, not scattered across handler modules
- Type matching uses enum, not strings — compiler catches errors
- Adding new transaction type requires: (1) add to enum, (2) handler in its module, (3) reader in transactions/readers

**Alternatives considered:**
- *Extend handler interface with Read methods* — rejected; mixes read/write concerns, changes existing interface
- *Standardize raw_data format* — rejected; requires migrating existing data, changes all handlers
- *Duplicate parsing logic* — rejected; leads to drift between write and read

### 3. Enriched Response DTOs

**Decision:** Create separate response types in the transaction module:

```go
// TransactionListItem - for GET /transactions
type TransactionListItem struct {
    ID            string  `json:"id"`
    Type          string  `json:"type"`
    TypeLabel     string  `json:"type_label"`      // "Income", "Outcome", "Adjustment"
    AssetID       string  `json:"asset_id"`
    AssetSymbol   string  `json:"asset_symbol"`    // "BTC", "ETH"
    Amount        string  `json:"amount"`          // Base units as string
    DisplayAmount string  `json:"display_amount"`  // "0.5 BTC"
    WalletID      string  `json:"wallet_id"`
    WalletName    string  `json:"wallet_name"`     // "My Hardware Wallet"
    Status        string  `json:"status"`
    OccurredAt    string  `json:"occurred_at"`
    USDValue      string  `json:"usd_value,omitempty"`
}

// TransactionDetail - for GET /transactions/:id
type TransactionDetail struct {
    TransactionListItem
    Source       string                 `json:"source"`
    ExternalID   *string                `json:"external_id,omitempty"`
    RecordedAt   string                 `json:"recorded_at"`
    Notes        string                 `json:"notes,omitempty"`
    RawData      map[string]interface{} `json:"raw_data,omitempty"`
    Entries      []EntryResponse        `json:"entries"`
}

// EntryResponse - ledger entry for detail view
type EntryResponse struct {
    ID           string `json:"id"`
    AccountCode  string `json:"account_code"`    // "wallet:btc:abc123"
    AccountLabel string `json:"account_label"`   // "My Wallet - BTC"
    DebitCredit  string `json:"debit_credit"`    // "DEBIT" or "CREDIT"
    EntryType    string `json:"entry_type"`      // "asset_increase", "income"
    Amount       string `json:"amount"`
    DisplayAmount string `json:"display_amount"`
    AssetSymbol  string `json:"asset_symbol"`
    USDValue     string `json:"usd_value,omitempty"`
}
```

**Rationale:** Explicit DTOs decouple API response format from domain models. Frontend receives ready-to-display data without parsing `raw_data`.

**Alternatives considered:**
- *Return raw domain objects* — rejected; exposes internals and requires frontend parsing
- *GraphQL* — overkill for this scope; REST with enriched DTOs is simpler

### 3. Amount Formatting Strategy

**Decision:** Server returns both raw `amount` (string of base units) and `display_amount` (formatted string like "0.5 BTC").

**Rationale:**
- Raw amount preserves precision for any calculations
- Display amount is ready for UI without frontend needing asset decimal knowledge
- Follows existing principle: "Amount conversions to display units happen ONLY at presentation layer" — this is the presentation layer (API response)

**Alternatives considered:**
- *Frontend formats* — rejected; requires shipping decimal configs to frontend, duplicates logic
- *Only display amount* — rejected; loses precision for potential recalculations

### 4. Transaction Detail Endpoint Design

**Decision:** `GET /transactions/:id` returns transaction with entries in a single call.

**Rationale:** Single round-trip for detail view. Entries are always needed on detail page, so eager loading is appropriate.

**Alternatives considered:**
- *Separate `/transactions/:id/entries`* — rejected; creates unnecessary request for mandatory data

### 5. Frontend Detail Page Approach

**Decision:** New `TransactionDetail.tsx` page at route `/transactions/:id` using TanStack Query.

**Rationale:** Consistent with existing patterns (`TransactionList.tsx`). Uses React Router for navigation, TanStack Query for data fetching.

**Component structure:**
- Header with back navigation
- Transaction summary card (type, amount, wallet, date)
- Ledger entries table showing debit/credit balance

## Risks / Trade-offs

**[Performance] Wallet/Asset lookup on every list request** → Initial implementation joins data in-memory. If slow, add Redis caching for wallet/asset lookups or denormalize into transactions table.

**[Complexity] Formatting logic duplication** → Centralize all formatting in transaction service. Use shared formatters for amounts/dates.

**[Breaking change] Response format changes** → Frontend and backend deploy together. If needed for backwards compatibility, add `v2` field or accept header versioning.

**[Data consistency] Wallet/asset names change after transaction** → Display current names with transaction timestamp. Consider storing snapshot at transaction time if historical accuracy becomes critical.

**[Dependency direction] Transaction module depends on handler modules** → This is intentional one-way dependency. Handler modules don't know about transactions module. If handler domain types change, readers must be updated — but this is acceptable since both changes happen in same codebase.

**[New transaction types] Adding a new type requires a new reader** → When adding new transaction type (e.g., `swap`), developer must also add `SwapReader` in transactions module. Document this in handler module README.

## Integration with Existing Modules

### What stays the same

| Module | Location | Role | Changes |
|--------|----------|------|---------|
| `manual_transaction` | `internal/modules/manual_transaction/` | Validates income/outcome, generates entries | None |
| `asset_adjustment` | `internal/modules/asset_adjustment/` | Validates adjustments, generates entries | None |
| `ledger_service` | `internal/core/ledger/service/` | Core double-entry engine | None (uses existing read methods) |
| Handler registry | `ledger_service` | Routes transaction types to handlers | None |

### What's new

| Component | Location | Role |
|-----------|----------|------|
| `transaction_service` | `internal/modules/transactions/service/` | Read-only service for enriched transaction data |
| `TransactionReader` interface | `internal/modules/transactions/readers/` | Interface for parsing type-specific raw_data |
| `IncomeReader` | `internal/modules/transactions/readers/` | Parses manual_income, imports `manual_transaction/domain` |
| `OutcomeReader` | `internal/modules/transactions/readers/` | Parses manual_outcome, imports `manual_transaction/domain` |
| `AdjustmentReader` | `internal/modules/transactions/readers/` | Parses asset_adjustment, imports `asset_adjustment/domain` |
| `TransactionListItem` DTO | `internal/modules/transactions/dto/` | Response type for list endpoint |
| `TransactionDetail` DTO | `internal/modules/transactions/dto/` | Response type for detail endpoint |

### Module dependencies

```
internal/modules/transactions/
    ├── imports → internal/modules/manual_transaction/domain  (for Income, Outcome structs)
    ├── imports → internal/modules/asset_adjustment/domain    (for Adjustment struct)
    └── imports → internal/core/ledger/service                (for reading raw transactions)
```

This creates a one-way dependency: `transactions` module depends on handler modules for domain types, but handler modules know nothing about `transactions` module.

### Data flow examples

**Creating a transaction (unchanged):**
```
User: POST /transactions {type: "manual_income", ...}
  → transaction_handler.CreateTransaction()
  → ledgerService.RecordTransaction("manual_income", data)
  → registry.GetHandler("manual_income")
  → incomeHandler.Validate(data)
  → incomeHandler.ToEntries(data)  // returns balanced entries
  → ledgerService.Commit(tx, entries)
  → Response: {id, type, status, ...}
```

**Reading transactions (new path):**
```
User: GET /transactions
  → transaction_handler.GetTransactions()
  → transactionService.ListTransactions(filters)
      → ledgerService.ListTransactions(filters)  // raw transactions
      → for each transaction:
          → reader := readerRegistry.GetReader(tx.Type)  // finds IncomeReader, etc.
          → fields := reader.ReadForList(tx.RawData)     // parses type-specific data
      → walletRepo.GetByIDs(walletIDs)           // batch lookup
      → assetRepo.GetByIDs(assetIDs)             // batch lookup
      → formatTransactions(fields, wallets, assets) // enrich with names
  → Response: [{id, type, type_label, wallet_name, display_amount, ...}]
```

**Reading single transaction with entries (new endpoint):**
```
User: GET /transactions/:id
  → transaction_handler.GetTransaction()
  → transactionService.GetTransaction(id)
      → ledgerService.GetTransactionWithEntries(id)
      → enrich transaction + entries
      → format for display
  → Response: {id, ..., entries: [{account_label, debit_credit, display_amount, ...}]}
```
