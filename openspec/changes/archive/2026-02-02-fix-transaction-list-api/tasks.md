## 1. Update Handler Response Structs

- [x] 1.1 Add `TransactionListItemResponse` struct to `transaction_handler.go` with fields matching `dto.TransactionListItem` (id, type, type_label, asset_id, asset_symbol, amount, display_amount, direction, wallet_id, wallet_name, status, occurred_at, usd_value)
- [x] 1.2 Update `TransactionListResponse` to use `[]TransactionListItemResponse` instead of `[]TransactionResponse`

## 2. Wire TransactionService to Handler

- [x] 2.1 Update `TransactionHandler` struct to include `transactionService *txService.TransactionService` field (already present, verify it's used)
- [x] 2.2 Modify `GetTransactions()` method to call `transactionService.ListTransactions()` instead of `ledgerService.ListTransactions()`
- [x] 2.3 Convert `[]dto.TransactionListItem` to `[]TransactionListItemResponse` in the handler response
- [x] 2.4 Remove unused `toTransactionResponse()` helper function and old `TransactionResponse` struct if no longer needed

## 3. Verify and Test

- [x] 3.1 Run backend build to verify compilation: `just backend-build`
- [x] 3.2 Run backend tests: `just backend-test`
- [ ] 3.3 Manual test: create a transaction and verify `GET /transactions` returns enriched fields (type_label, display_amount, wallet_name)
