## 1. Backend: Transaction Type Enum

- [x] 1.1 Create `internal/core/ledger/domain/transaction_type.go` with `TransactionType` enum (`TxTypeManualIncome`, `TxTypeManualOutcome`, `TxTypeAssetAdjustment`) and `Label()`, `IsValid()` methods
- [x] 1.2 Update `manual_transaction/handler/income_handler.go` to use `ledgerdomain.TxTypeManualIncome` instead of local constant
- [x] 1.3 Update `manual_transaction/handler/outcome_handler.go` to use `ledgerdomain.TxTypeManualOutcome` instead of local constant
- [x] 1.4 Update `asset_adjustment/handler/handler.go` to use `ledgerdomain.TxTypeAssetAdjustment` instead of local constant

## 2. Backend: Transaction Readers

- [x] 2.1 Create `internal/modules/transactions/readers/reader.go` with `TransactionReader` interface and `ReaderRegistry`
- [x] 2.2 Add `ParseFromRawData()` method to `manual_transaction/domain/income.go` that parses raw_data map to Income struct
- [x] 2.3 Add `ParseFromRawData()` method to `manual_transaction/domain/outcome.go` that parses raw_data map to Outcome struct
- [x] 2.4 Add `ParseFromRawData()` method to `asset_adjustment/domain/transaction.go` that parses raw_data map to AssetAdjustment struct
- [x] 2.5 Create `internal/modules/transactions/readers/income_reader.go` implementing `TransactionReader` for manual_income
- [x] 2.6 Create `internal/modules/transactions/readers/outcome_reader.go` implementing `TransactionReader` for manual_outcome
- [x] 2.7 Create `internal/modules/transactions/readers/adjustment_reader.go` implementing `TransactionReader` for asset_adjustment

## 3. Backend: Transaction Service

- [x] 3.1 Create `internal/modules/transactions/dto/responses.go` with `TransactionListItem`, `TransactionDetail`, `EntryResponse` DTOs
- [x] 3.2 Create `internal/modules/transactions/service/service.go` with `TransactionService` interface and implementation
- [x] 3.3 Implement `ListTransactions()` method that enriches transactions with wallet_name, asset_symbol, display_amount
- [x] 3.4 Implement `GetTransaction()` method that returns transaction with ledger entries and full details
- [x] 3.5 Add amount formatting utility that converts base units to display format (e.g., "50000000" satoshi → "0.5 BTC")

## 4. Backend: API Endpoints

- [x] 4.1 Add `GET /transactions/:id` route to `internal/api/router/router.go`
- [x] 4.2 Update `transaction_handler.go` to use `TransactionService` for list endpoint (return enriched DTOs)
- [x] 4.3 Add `GetTransaction()` handler in `transaction_handler.go` for detail endpoint
- [x] 4.4 Add user authorization check in `GetTransaction()` - return 404 for other user's transactions
- [x] 4.5 Update OpenAPI spec `apps/backend/cmd/api/openapi.yaml` with `GET /transactions/{id}` endpoint and enriched response schemas

## 5. Frontend: Transaction Service

- [x] 5.1 Update `apps/frontend/src/services/transaction.ts` with `TransactionListItem` and `TransactionDetail` TypeScript interfaces
- [x] 5.2 Add `getTransaction(id: string)` API function to fetch single transaction with entries
- [x] 5.3 Update `getTransactions()` return type to use enriched `TransactionListItem[]`

## 6. Frontend: Transaction List Improvements

- [x] 6.1 Refactor `TransactionList.tsx` to display type_label, asset_symbol, display_amount, wallet_name instead of raw_data
- [x] 6.2 Add color styling for amounts (green for income, red for outcome)
- [x] 6.3 Make transaction rows clickable with navigation to `/transactions/:id`
- [x] 6.4 Add empty state message when no transactions exist

## 7. Frontend: Transaction Detail Page

- [x] 7.1 Create `apps/frontend/src/features/transactions/TransactionDetail.tsx` component
- [x] 7.2 Add route `/transactions/:id` to `App.jsx` pointing to TransactionDetail
- [x] 7.3 Implement transaction header showing type, status, date, primary amount
- [x] 7.4 Implement ledger entries table with columns: Account, Debit/Credit, Amount, Asset, USD Value
- [x] 7.5 Add loading state with spinner during data fetch
- [x] 7.6 Add error state with "Transaction not found" message and back-to-list button
- [x] 7.7 Add back navigation to transaction list

## 8. Integration & Testing

- [x] 8.1 Wire up `TransactionService` in `main.go` with dependencies (ledger service, wallet repo, asset repo)
- [x] 8.2 Verify transaction list displays correctly for all transaction types (income, outcome, adjustment)
- [x] 8.3 Verify transaction detail page loads entries and shows correct debit/credit balance
- [x] 8.4 Verify authorization: user cannot access another user's transaction details
