## Context

The `TransactionService` in `internal/modules/transactions/service/` already implements full transaction enrichment logic via `ListTransactions()` method that returns `[]dto.TransactionListItem` with all required display fields. However, the API handler `TransactionHandler.GetTransactions()` bypasses this service and calls `LedgerService.ListTransactions()` directly, returning raw `TransactionResponse` structs without enrichment.

Current flow (broken):
```
GET /transactions → TransactionHandler.GetTransactions()
                  → LedgerService.ListTransactions()
                  → returns []*domain.Transaction
                  → converts to []TransactionResponse (basic fields only)
```

Required flow (working):
```
GET /transactions → TransactionHandler.GetTransactions()
                  → TransactionService.ListTransactions()
                  → returns []dto.TransactionListItem (enriched)
                  → returns directly as response
```

## Goals / Non-Goals

**Goals:**
- Wire `TransactionHandler.GetTransactions()` to use `TransactionService.ListTransactions()` for list endpoint
- Return `TransactionListItemResponse` matching the existing `dto.TransactionListItem` structure
- Maintain backwards compatibility for `GET /transactions/{id}` which already uses `TransactionService.GetTransaction()`

**Non-Goals:**
- Changing the enrichment logic itself (already working in TransactionService)
- Modifying how individual transaction details are fetched
- Adding new fields to the response beyond what frontend expects

## Decisions

### 1. Reuse existing TransactionService.ListTransactions()

**Decision:** Use the existing `TransactionService.ListTransactions()` method instead of creating new enrichment logic in the handler.

**Rationale:** The enrichment logic already exists and is tested. It handles:
- Reading transaction fields via type-specific readers (income, outcome, adjustment)
- Batch fetching wallets for wallet_name
- Formatting display amounts from base units
- Setting type labels and direction indicators

**Alternative considered:** Add enrichment logic directly in the handler - rejected because it would duplicate existing code.

### 2. Replace response struct in handler

**Decision:** Replace `TransactionResponse` and `TransactionListResponse` with new structs that match `dto.TransactionListItem`.

**Rationale:** The frontend expects specific field names (`type_label`, `display_amount`, `wallet_name`, etc.) that don't exist in the current handler response struct.

**Implementation:**
```go
// In transaction_handler.go
type TransactionListItemResponse struct {
    ID            string `json:"id"`
    Type          string `json:"type"`
    TypeLabel     string `json:"type_label"`
    AssetID       string `json:"asset_id"`
    AssetSymbol   string `json:"asset_symbol"`
    Amount        string `json:"amount"`
    DisplayAmount string `json:"display_amount"`
    Direction     string `json:"direction"`
    WalletID      string `json:"wallet_id"`
    WalletName    string `json:"wallet_name"`
    Status        string `json:"status"`
    OccurredAt    string `json:"occurred_at"`
    USDValue      string `json:"usd_value,omitempty"`
}

type TransactionListResponse struct {
    Transactions []TransactionListItemResponse `json:"transactions"`
    Total        int                           `json:"total"`
    Page         int                           `json:"page"`
    PageSize     int                           `json:"page_size"`
}
```

### 3. Keep create endpoint using LedgerService

**Decision:** Continue using `LedgerService.RecordTransaction()` for creating transactions, as that's its intended purpose.

**Rationale:** The LedgerService handles transaction validation, entry generation, and ledger commits. TransactionService is read-only by design.

## Risks / Trade-offs

### Breaking API change
**Risk:** Existing clients using `GET /transactions` will receive a different response structure.
**Mitigation:** This is expected - the previous response was incomplete. The change aligns the API with documented contract that frontend already expects.

### Total count accuracy
**Risk:** Current implementation uses `len(transactions)` for total count, which only counts returned items, not total matching items.
**Mitigation:** This is a pre-existing issue not introduced by this change. Can be addressed separately by adding count query if pagination accuracy becomes important.
