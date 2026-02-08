# Implementation Phases Plan

## Phase 1: Foundation — Critical Fixes & Infrastructure Prerequisites
**Platform:** Backend
**Goal:** Address the 5 critical issues from architecture validation and lay the infrastructure groundwork (new transaction types, account types, config, migration) so that all subsequent feature phases can build on a solid foundation.
**Depends on:** None (first phase)

### Changes

#### Backend

##### 1. Atomic Wallet Sync Claim (Critical Fix #1)

Two WalletRepository interfaces must be updated: the sync-specific subset in `sync/port.go` and the full repository in `wallet/port.go`. The concrete implementation is in `infra/postgres/wallet_repo.go`.

- [x] File: `apps/backend/internal/platform/sync/port.go` (sync WalletRepository interface, line 63) — Add `ClaimWalletForSync(ctx context.Context, walletID uuid.UUID) (bool, error)` method. This atomically sets `sync_status = 'syncing'` only if current status is not already `'syncing'`, returning true if claimed successfully.
- [x] File: `apps/backend/internal/platform/wallet/port.go` (full wallet.Repository interface, line 11) — Add matching `ClaimWalletForSync(ctx context.Context, walletID uuid.UUID) (bool, error)` method so `postgres.WalletRepository` satisfies both interfaces.
- [x] File: `apps/backend/internal/infra/postgres/wallet_repo.go` — Implement `ClaimWalletForSync()` using `UPDATE wallets SET sync_status = 'syncing', sync_started_at = NOW(), sync_error = NULL, updated_at = NOW() WHERE id = $1 AND sync_status != 'syncing' RETURNING id`. Return `true` if rows affected = 1, `false` if 0.
- [x] File: `apps/backend/internal/platform/sync/service.go` — In `syncWallet()` (line 178), replace the call to `s.walletRepo.SetSyncInProgress(ctx, w.ID)` with `s.walletRepo.ClaimWalletForSync(ctx, w.ID)`. If claim returns `(false, nil)`, skip the wallet (another instance is syncing it) and return nil. Log at Debug level.

##### 2. Stale Sync Recovery (Critical Fix #2)
- [x] File: `apps/backend/internal/platform/wallet/model.go` — Add `SyncStartedAt *time.Time` field to `Wallet` struct (line 38, after `SyncError`). This field does not currently exist on the struct.
- [x] File: `apps/backend/internal/infra/postgres/wallet_repo.go` — Update `GetWalletsForSync()` query (line 222) to also include wallets stuck in `'syncing'` state where `sync_started_at < NOW() - INTERVAL '10 minutes'`. Update the WHERE clause from `WHERE sync_status IN ('pending', 'error', 'synced')` to `WHERE sync_status IN ('pending', 'error', 'synced') OR (sync_status = 'syncing' AND sync_started_at < NOW() - INTERVAL '10 minutes')`. Add `sync_started_at` to the SELECT column list and to the `rows.Scan()` call (currently scans 11 fields, needs to scan 12).
- [x] File: `apps/backend/internal/infra/postgres/wallet_repo.go` — Also update `GetWalletsByAddress()` (line 273) and all other wallet-scanning queries to include `sync_started_at` in SELECT and Scan, since the `Wallet` struct now has the new field.

##### 3. Robust Duplicate Detection (Critical Fix #3)
- [x] File: `apps/backend/internal/platform/sync/processor.go` — Replace the string-matching `isDuplicateError()` function (lines 292-301) with PostgreSQL error code check. The current implementation uses `strings.Contains()` on `"duplicate"`, `"unique constraint"`, `"already exists"` which is brittle. New implementation should use `pgconn.PgError` type assertion: `var pgErr *pgconn.PgError; if errors.As(err, &pgErr) && pgErr.Code == "23505" { return true }`. Add imports for `errors` and `github.com/jackc/pgx/v5/pgconn`.

##### 4. New Transaction Type Constants (Foundation for DeFi handlers)
- [x] File: `apps/backend/internal/ledger/model.go` — Add 4 new `TransactionType` constants after line 25 (after `TxTypeAssetAdjustment`):
  - `TxTypeSwap TransactionType = "swap"`
  - `TxTypeDeFiDeposit TransactionType = "defi_deposit"`
  - `TxTypeDeFiWithdraw TransactionType = "defi_withdraw"`
  - `TxTypeDeFiClaim TransactionType = "defi_claim"`
- [x] File: `apps/backend/internal/ledger/model.go` — Update `AllTransactionTypes()` (line 28) to include the 4 new types in the returned slice.
- [x] File: `apps/backend/internal/ledger/model.go` — Update `IsValid()` (line 40) switch to include `TxTypeSwap, TxTypeDeFiDeposit, TxTypeDeFiWithdraw, TxTypeDeFiClaim` in the valid cases.
- [x] File: `apps/backend/internal/ledger/model.go` — Update `Label()` (line 55) switch to add cases returning `"Swap"`, `"DeFi Deposit"`, `"DeFi Withdraw"`, `"DeFi Claim"` respectively.

##### 5. New Account Type: CLEARING (Foundation for swap handler)
- [x] File: `apps/backend/internal/ledger/model.go` — Add `AccountTypeClearing AccountType = "CLEARING"` constant after line 278 (after `AccountTypeGasFee`).
- [x] File: `apps/backend/internal/ledger/model.go` — Update `Account.Validate()` (line 313) to accept `AccountTypeClearing` in the switch. CLEARING accounts should not have a wallet_id, similar to INCOME/EXPENSE/GAS_FEE — add it to the `case AccountTypeIncome, AccountTypeExpense, AccountTypeGasFee:` arm (line 328).

##### 6. New Entry Type: clearing (Foundation for swap handler)
- [x] File: `apps/backend/internal/ledger/model.go` — Add `EntryTypeClearing EntryType = "clearing"` constant after line 203 (after `EntryTypeGasFee`). No CHECK constraint exists on `entries.entry_type` in the database (verified: `000001_create_schema.up.sql` line 72 uses `VARCHAR(50) NOT NULL` with no CHECK), so no migration is needed for this.

##### 7. Zerion API Key Config
- [x] File: `apps/backend/pkg/config/config.go` — Add `ZerionAPIKey string` field to `Config` struct (after `AlchemyAPIKey` on line 31). In `Load()` (line 36), add `ZerionAPIKey: getEnv("ZERION_API_KEY", ""),` to the struct literal.
- [x] File: `apps/backend/.env.example` — Add `ZERION_API_KEY=your-zerion-api-key-here` line after `CHAINS_CONFIG_PATH` (after line 17).

##### 8. Cross-User Internal Transfer Prevention (Important Fix #7)

The sync `Processor` currently uses `isUserWallet()` (line 117) and `getWalletByAddress()` (line 146), both of which call `walletRepo.GetWalletsByAddress()` — a query that matches addresses across ALL users. This means User A's sync could incorrectly classify User B's wallet as an internal transfer counterparty.

- [x] File: `apps/backend/internal/platform/sync/port.go` — Add `GetWalletsByAddressAndUserID(ctx context.Context, address string, userID uuid.UUID) ([]*wallet.Wallet, error)` to the sync `WalletRepository` interface (line 63).
- [x] File: `apps/backend/internal/platform/wallet/port.go` — Add matching `GetWalletsByAddressAndUserID(ctx context.Context, address string, userID uuid.UUID) ([]*wallet.Wallet, error)` to the full `wallet.Repository` interface (line 11).
- [x] File: `apps/backend/internal/infra/postgres/wallet_repo.go` — Implement `GetWalletsByAddressAndUserID()` with `SELECT ... FROM wallets WHERE lower(address) = lower($1) AND user_id = $2`. Pattern matches existing `GetWalletsByAddress()` (line 271) but with user_id filter.
- [x] File: `apps/backend/internal/platform/sync/processor.go` — Update `isUserWallet()` (line 117) to accept `userID uuid.UUID` parameter and call `walletRepo.GetWalletsByAddressAndUserID(ctx, address, userID)` instead of `walletRepo.GetWalletsByAddress(ctx, address)`. The `addressCache` key should be scoped to include userID to avoid cross-user cache hits (e.g., key = `userID.String() + ":" + address`).
- [x] File: `apps/backend/internal/platform/sync/processor.go` — Update `getWalletByAddress()` (line 146) to accept `userID uuid.UUID` parameter and call the user-scoped query.
- [x] File: `apps/backend/internal/platform/sync/processor.go` — Update `classifyTransfer()` (line 75) to pass `w.UserID` to `isUserWallet()`. Update `recordInternalTransfer()` (line 227) to pass `w.UserID` to `getWalletByAddress()`. No changes needed to `ProcessTransfer()` signature since it already receives `*wallet.Wallet` which has `UserID`.

#### Database Migrations

##### Migration 000008: Add CLEARING account type + sync_started_at
- [x] File: `apps/backend/migrations/000008_foundation.up.sql`
  ```sql
  -- 1. Add CLEARING to accounts type CHECK constraint
  -- Original constraint from 000001_create_schema.up.sql line 32:
  --   CHECK (type IN ('CRYPTO_WALLET', 'INCOME', 'EXPENSE', 'GAS_FEE'))
  ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_type_check;
  ALTER TABLE accounts ADD CONSTRAINT accounts_type_check
      CHECK (type IN ('CRYPTO_WALLET', 'INCOME', 'EXPENSE', 'GAS_FEE', 'CLEARING'));

  -- 2. Add sync_started_at column for stale sync detection
  -- Wallets table already has sync_status, last_sync_block, last_sync_at from 000007
  ALTER TABLE wallets ADD COLUMN IF NOT EXISTS sync_started_at TIMESTAMPTZ;

  -- 3. Index for stale sync recovery queries
  CREATE INDEX IF NOT EXISTS idx_wallets_sync_started_at
      ON wallets(sync_started_at)
      WHERE sync_status = 'syncing';
  ```
- [x] File: `apps/backend/migrations/000008_foundation.down.sql`
  ```sql
  -- Revert sync_started_at
  DROP INDEX IF EXISTS idx_wallets_sync_started_at;
  ALTER TABLE wallets DROP COLUMN IF EXISTS sync_started_at;

  -- Revert account type constraint (remove CLEARING)
  -- WARNING: Will fail if CLEARING accounts exist - delete them first
  ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_type_check;
  ALTER TABLE accounts ADD CONSTRAINT accounts_type_check
      CHECK (type IN ('CRYPTO_WALLET', 'INCOME', 'EXPENSE', 'GAS_FEE'));
  ```

**Note on entry_type**: There is NO CHECK constraint on `entries.entry_type` in the database (`000001_create_schema.up.sql` line 72 defines it as `VARCHAR(50) NOT NULL` with no CHECK). Therefore, no migration is needed to allow the new `clearing` entry type.

#### Frontend
- No frontend changes in Phase 1.

### Tests

- [x] Test: `apps/backend/internal/platform/sync/processor_test.go` — Add unit test for the new `isDuplicateError()` implementation. Test with a real `pgconn.PgError{Code: "23505"}` (should return true), `pgconn.PgError{Code: "23503"}` (foreign key violation, should return false), a generic error (should return false), and nil (should return false).
- [x] Test: `apps/backend/internal/ledger/model_test.go` — Add tests verifying new transaction types (`swap`, `defi_deposit`, `defi_withdraw`, `defi_claim`) pass `IsValid()`, have correct `Label()` output, and appear in `AllTransactionTypes()`.
- [x] Test: `apps/backend/internal/ledger/model_test.go` — Add test verifying `AccountTypeClearing` is accepted in `Account.Validate()` without a `WalletID`, and that it is rejected if a `WalletID` is provided (consistent with INCOME/EXPENSE/GAS_FEE behavior).
- [x] Test: `apps/backend/internal/infra/gateway/alchemy/types_test.go` — Add tests from Phase 0 verifying `TransferCategoriesForChain()` excludes `internal` on L2 chains (42161, 10, 8453, 43114, 56) and includes it on L1 (1, 137).
- [x] Test: Existing sync service tests continue to pass. The new `ClaimWalletForSync` and `GetWalletsByAddressAndUserID` methods must be added to any mock implementations of the `WalletRepository` interface.

### Acceptance Criteria
- [x] `go build ./...` succeeds with all changes
- [x] All existing tests pass (`go test ./...`)
- [x] New transaction types `swap`, `defi_deposit`, `defi_withdraw`, `defi_claim` are valid per `IsValid()`
- [x] `AccountTypeClearing` is accepted in `Account.Validate()` without a wallet ID
- [ ] Migration 000008 applies cleanly on a fresh database after migrations 000001-000007
- [ ] Migration 000008 rolls back cleanly
- [x] `ClaimWalletForSync()` atomically claims wallets — concurrent calls for the same wallet only succeed once
- [x] `GetWalletsForSync()` returns wallets stuck in `'syncing'` for >10 minutes (via `sync_started_at` check)
- [x] `isDuplicateError()` uses PostgreSQL error code `23505` instead of string matching
- [x] Zerion API key is loaded from `ZERION_API_KEY` env var (no validation required, empty = disabled)
- [x] `isUserWallet()` and `getWalletByAddress()` only match wallets belonging to the same user (using `w.UserID`)

### Risk Mitigation
- **CHECK constraint drop/recreate locks accounts table** -> Low risk: `accounts` table has very low write frequency (accounts only created during first transaction for a new asset). Brief lock is acceptable.
- **Interface changes break existing code** -> Both `sync/port.go` WalletRepository and `wallet/port.go` Repository interfaces get new methods. Any existing mock implementations (in tests) must add stubs for `ClaimWalletForSync` and `GetWalletsByAddressAndUserID`. This is additive (no existing methods are changed).
- **Stale sync threshold too aggressive/conservative** -> 10-minute threshold is hardcoded in the SQL query. Can be moved to `sync.Config` later if needed.
- **Migration on existing data** -> Migration is purely additive (new column, relaxed constraint). No data transformation required. Safe to run on existing production data.
- **Wallet struct scan breakage** -> Adding `SyncStartedAt` to `Wallet` requires updating ALL queries that scan into `*wallet.Wallet` to include the new column. Grep for `rows.Scan` calls in `wallet_repo.go` to find every scan site. Currently: `GetWalletsForSync()` (line 244), `GetWalletsByAddress()` (line 287), and any other wallet-fetching queries.

### Notes
- Phase 0 (Alchemy L2 category test) from the backend plan is folded into this phase as a test-only item.
- TaxLot genesis migration (Critical Fix #4) is deferred to the phase where TaxLot tables are created, since it depends on the tax_lots schema existing first.
- Clearing account balance monitoring (Critical Fix #5) is deferred to the phase that introduces the swap handler, since monitoring is meaningless until clearing accounts are created.
- The `processor.go` changes for user-scoped wallet lookup require the `Processor` to know the current wallet's `UserID`. Since `ProcessTransfer()` already receives `*wallet.Wallet` which has `UserID` (see `wallet/model.go` line 31), this information is available without changes to the processor's public API.
- Code deploying new types should be deployed BEFORE running migration 000008, so the application can handle CLEARING accounts when the constraint is relaxed. In practice, this is safe because no CLEARING accounts will be created until the swap handler (Phase 2+) is deployed.
- There are two separate `WalletRepository` interfaces: `internal/platform/sync/port.go` defines a minimal subset used by the sync service, while `internal/platform/wallet/port.go` defines the full interface. The concrete `postgres.WalletRepository` implements both. New methods must be added to both interfaces.

---

## Phase 2: Zerion Client & Adapter
**Platform:** Backend
**Goal:** Build the Zerion HTTP client, API types, and domain adapter so we can fetch and convert decoded blockchain transactions from Zerion's REST API into domain types -- without changing the sync pipeline yet.
**Depends on:** Phase 1 (uses `ZerionAPIKey` from config)

### Changes

#### Backend

##### 1. Zerion API Types (NEW file: `internal/infra/gateway/zerion/types.go`)

Zerion REST API response types. These are pure data structures with JSON tags, mapping exactly to Zerion's `GET /wallets/{address}/transactions/` response shape.

- [x] File: `apps/backend/internal/infra/gateway/zerion/types.go` (NEW) — Create the following types:
  - `TransactionResponse` — top-level response with `Links` and `Data []TransactionData`
  - `Links` — pagination with `Self string` and `Next string`
  - `TransactionData` — JSON:API envelope with `Type`, `ID`, `Attributes TransactionAttributes`
  - `TransactionAttributes` — core data: `OperationType`, `Hash`, `MinedAt` (time.Time), `SentFrom`, `SentTo`, `Status`, `Nonce`, `Fee *Fee`, `Transfers []ZTransfer`, `Approvals []Approval`, `ApplicationMetadata *ApplicationMeta`
  - `Fee` — gas fee: `FungibleInfo`, `Quantity`, `Price *float64`, `Value *float64`
  - `ZTransfer` — individual asset movement: `FungibleInfo`, `Direction` ("in"/"out"/"self"), `Quantity`, `Price *float64`, `Value *float64`, `Sender`, `Recipient`
  - `FungibleInfo` — token metadata: `Name`, `Symbol`, `Icon *IconInfo`, `Implementations map[string]Implementation` (chain string -> contract info)
  - `IconInfo` — `URL string`
  - `Implementation` — `Address string`, `Decimals int`
  - `Quantity` — amounts: `Int string` (USE THIS for financial math), `Decimals int`, `Float float64` (NEVER use), `Numeric string`
  - `Approval` — token approval: `FungibleInfo`, `Quantity`, `Sender`
  - `ApplicationMeta` — DeFi protocol info: `Name string`, `Icon *IconInfo`
- [x] File: `apps/backend/internal/infra/gateway/zerion/types.go` — Add chain ID bidirectional mapping:
  - `ZerionChainToID map[string]int64` — maps "ethereum" -> 1, "polygon" -> 137, "arbitrum" -> 42161, "optimism" -> 10, "base" -> 8453, "avalanche" -> 43114, "bsc" -> 56
  - `IDToZerionChain map[int64]string` — reverse mapping
  - `ErrUnsupportedChain` error for unknown chain IDs

##### 2. Zerion HTTP Client (NEW file: `internal/infra/gateway/zerion/client.go`)

HTTP client following the same patterns as the existing Alchemy client (`internal/infra/gateway/alchemy/client.go`): constructor with API key, `*http.Client` with timeout, request helper method.

- [x] File: `apps/backend/internal/infra/gateway/zerion/client.go` (NEW) — Create `Client` struct with fields:
  - `apiKey string`
  - `httpClient *http.Client` (30s timeout, matching Alchemy's `requestTimeout` at alchemy/client.go line 16)
  - `baseURL string` (default: `https://api.zerion.io/v1`)
- [x] File: `apps/backend/internal/infra/gateway/zerion/client.go` — `NewClient(apiKey string) *Client` constructor. Sets `httpClient` with 30s timeout, `baseURL` to default.
- [x] File: `apps/backend/internal/infra/gateway/zerion/client.go` — Private `doRequest(ctx, method, url string, params url.Values) ([]byte, error)` helper:
  - Builds HTTP request with `http.NewRequestWithContext`
  - Sets `Authorization: Basic {base64(apiKey + ":")}` header (encode `apiKey + ":"` in base64)
  - Sets `Accept: application/json`
  - Handles HTTP 429 with exponential backoff: initial 1s, factor 2x, max 3 retries. Return `RateLimitError` after exhausting retries.
  - Returns response body bytes on 200 OK
  - Returns descriptive error on non-200/429 status codes
- [x] File: `apps/backend/internal/infra/gateway/zerion/client.go` — `GetTransactions(ctx, address string, chainID string, since time.Time) ([]TransactionData, error)`:
  - Endpoint: `GET /wallets/{address}/transactions/`
  - Query params: `filter[chain_ids]={chainID}`, `filter[min_mined_at]={since.Format(time.RFC3339)}`, `filter[asset_types]=fungible`, `filter[trash]=only_non_trash`
  - Handles pagination: follow `Links.Next` URL until `Next` is empty
  - Accumulates all `TransactionData` from all pages
  - Returns full list of transactions since the given time
- [x] File: `apps/backend/internal/infra/gateway/zerion/client.go` — `RateLimitError` type (following existing Alchemy pattern at alchemy/client.go lines 244-258):
  - `RetryAfter time.Duration`
  - `Message string`
  - `Error() string` method
  - `IsRateLimitError(err error) bool` helper function (uses errors.As for wrapped error support)

##### 3. Domain Types for TransactionDataProvider (MODIFY: `internal/platform/sync/port.go`)

Add new domain types alongside the existing `Transfer` and `BlockchainClient` types. These represent Zerion's decoded transaction model at the domain level. The existing `Transfer` struct (port.go lines 31-45) is Alchemy-specific and remains unchanged.

- [x] File: `apps/backend/internal/platform/sync/port.go` — Add `OperationType string` type and constants after the existing `TransferType` constants (after line 28):
  - `OpTrade OperationType = "trade"`
  - `OpDeposit OperationType = "deposit"`
  - `OpWithdraw OperationType = "withdraw"`
  - `OpClaim OperationType = "claim"`
  - `OpReceive OperationType = "receive"`
  - `OpSend OperationType = "send"`
  - `OpExecute OperationType = "execute"`
  - `OpApprove OperationType = "approve"`
  - `OpMint OperationType = "mint"`
  - `OpBurn OperationType = "burn"`
- [x] File: `apps/backend/internal/platform/sync/port.go` — Add `DecodedTransaction` struct (named to avoid collision with `ledger.Transaction`):
  ```
  ID            string
  TxHash        string
  ChainID       int64
  OperationType OperationType
  Protocol      string         // "Uniswap V3", "GMX", "" for simple transfers
  Transfers     []DecodedTransfer
  Fee           *DecodedFee
  MinedAt       time.Time
  Status        string         // "confirmed", "failed"
  ```
- [x] File: `apps/backend/internal/platform/sync/port.go` — Add `DecodedTransfer` struct (named to avoid collision with existing `Transfer`):
  ```
  AssetSymbol     string
  ContractAddress string
  Decimals        int
  Amount          *big.Int       // base units from Quantity.Int
  Direction       TransferDirection  // "in", "out"
  Sender          string
  Recipient       string
  USDPrice        *big.Int       // scaled by 10^8 (from float64 * 1e8)
  ```
- [x] File: `apps/backend/internal/platform/sync/port.go` — Add `DecodedFee` struct:
  ```
  AssetSymbol string
  Amount      *big.Int
  Decimals    int
  USDPrice    *big.Int  // scaled by 10^8
  ```
- [x] File: `apps/backend/internal/platform/sync/port.go` — Add `TransactionDataProvider` interface (after existing `BlockchainClient` interface, after line 60):
  ```go
  type TransactionDataProvider interface {
      GetTransactions(ctx context.Context, address string, chainID int64, since time.Time) ([]DecodedTransaction, error)
  }
  ```

##### 4. Zerion Adapter (NEW file: `internal/infra/gateway/zerion/adapter.go`)

Converts Zerion API types to domain types. Follows the same pattern as `alchemy/adapter.go` which adapts Alchemy types to `sync.Transfer`. The adapter implements `sync.TransactionDataProvider`.

- [x] File: `apps/backend/internal/infra/gateway/zerion/adapter.go` (NEW) — Create `SyncAdapter` struct:
  - `client *Client`
- [x] File: `apps/backend/internal/infra/gateway/zerion/adapter.go` — `NewSyncAdapter(client *Client) *SyncAdapter` constructor.
- [x] File: `apps/backend/internal/infra/gateway/zerion/adapter.go` — Compile-time interface check: `var _ sync.TransactionDataProvider = (*SyncAdapter)(nil)` (matching pattern at alchemy/adapter.go line 28).
- [x] File: `apps/backend/internal/infra/gateway/zerion/adapter.go` — `GetTransactions(ctx, address string, chainID int64, since time.Time) ([]sync.DecodedTransaction, error)`:
  1. Convert `chainID int64` to Zerion chain string using `IDToZerionChain` map. Return `ErrUnsupportedChain` if not found.
  2. Call `client.GetTransactions(ctx, address, zerionChain, since)`.
  3. Convert each `TransactionData` to `sync.DecodedTransaction` using `convertTransaction()`.
  4. Skip failed conversions, return accumulated results.
- [x] File: `apps/backend/internal/infra/gateway/zerion/adapter.go` — Private `convertTransaction(td TransactionData, chainID int64, zerionChain string) (sync.DecodedTransaction, error)`:
  - Map `td.Attributes.OperationType` string to `sync.OperationType` constant
  - Extract protocol name from `td.Attributes.ApplicationMD.Name` (nil-safe)
  - Convert each `td.Attributes.Transfers` ([]ZTransfer) to `[]sync.DecodedTransfer` via `convertTransfer()`
  - Convert `td.Attributes.Fee` to `*sync.DecodedFee` via `convertFee()`
  - Parse `td.Attributes.MinedAt` from RFC3339 string
  - Set `ID = td.ID`, `TxHash = td.Attributes.Hash`, `Status = td.Attributes.Status`
- [x] File: `apps/backend/internal/infra/gateway/zerion/adapter.go` — Private `convertTransfer(zt ZTransfer, zerionChain string) sync.DecodedTransfer`:
  - Amount: parse `zt.Quantity.Int` string to `*big.Int` using `new(big.Int).SetString(zt.Quantity.Int, 10)`. **NEVER uses `zt.Quantity.Float`**.
  - AssetSymbol: from `zt.FungibleInfo.Symbol`
  - ContractAddress: from `zt.FungibleInfo.Implementations[zerionChainName].Address` (lookup by chain). Empty string if not found (native token). Lowercased.
  - Decimals: from `zt.FungibleInfo.Implementations[zerionChainName].Decimals`. Fallback to `zt.Quantity.Decimals` if implementation not found.
  - Direction: mapped to `sync.TransferDirection` ("in" -> DirectionIn, else DirectionOut)
  - Sender/Recipient: `zt.Sender`, `zt.Recipient` (lowercased)
  - USDPrice: if `zt.Price != nil`, compute `math.Round(*price * 1e8)` -> big.Int. If nil, USDPrice is nil.
- [x] File: `apps/backend/internal/infra/gateway/zerion/adapter.go` — Private `convertFee(fee *Fee, zerionChain string) *sync.DecodedFee`:
  - Return nil if `fee == nil`
  - Amount: parse `fee.Quantity.Int` string to `*big.Int`
  - AssetSymbol: from `fee.FungibleInfo.Symbol`
  - Decimals: from `fee.FungibleInfo.Implementations[zerionChainName].Decimals` or `fee.Quantity.Decimals`
  - USDPrice: if `fee.Price != nil`, compute `math.Round(*price * 1e8)` -> big.Int. If nil, USDPrice is nil.

#### Frontend
- No frontend changes in Phase 2.

### Tests

- [x] Test: `apps/backend/internal/infra/gateway/zerion/types_test.go` (NEW) — Test chain ID mappings:
  - Every entry in `ZerionChainToID` has a matching reverse entry in `IDToZerionChain`
  - Known chains map correctly: "ethereum" <-> 1, "arbitrum" <-> 42161, etc.
  - Unknown chain string returns 0/empty from map lookup

- [x] Test: `apps/backend/internal/infra/gateway/zerion/client_test.go` (NEW) — Test client using `httptest.NewServer`:
  - **Auth header**: Verify request includes `Authorization: Basic {base64(apiKey + ":")}` header
  - **Pagination**: Mock server returns 2 pages (first response has `links.next`, second has empty `links.next`). Verify client accumulates all transactions from both pages.
  - **Rate limit retry**: Mock server returns 429 on first request, 200 on second. Verify client retries and succeeds.
  - **Rate limit exhaustion**: Mock server returns 429 three times. Verify client returns `RateLimitError` after max retries.
  - **Query params**: Verify correct `filter[chain_ids]`, `filter[min_mined_at]`, `filter[asset_types]=fungible` params are sent.
  - **Context cancellation**: Verify rate limit backoff respects context cancellation.
  - **Error responses**: Verify non-200/429 status codes return descriptive error.

- [x] Test: `apps/backend/internal/infra/gateway/zerion/adapter_test.go` (NEW) — Test adapter conversion:
  - **Basic conversion**: Mock `TransactionData` with known values. Verify `sync.DecodedTransaction` fields are correctly populated.
  - **Amount precision**: Verify `Quantity.Int = "1000000000000000000"` (1 ETH in wei) converts to correct `*big.Int`. Verify `Quantity.Float` is never used.
  - **USD price scaling**: Verify `Price = 3500.12` converts to `big.Int(350012000000)` (3500.12 * 1e8).
  - **Null price handling**: Verify `Price = nil` results in `USDPrice = nil`.
  - **Fee conversion**: Verify fee with known gas amount and price converts correctly. Verify nil fee results in nil `DecodedFee`.
  - **Protocol extraction**: Verify `ApplicationMD.Name = "Uniswap V3"` appears as `Protocol` field. Verify nil `ApplicationMD` results in empty `Protocol`.
  - **Chain ID conversion**: Verify all 7 chains map correctly for implementation lookups.
  - **Unsupported chain**: Verify `chainID = 999999` returns `ErrUnsupportedChain`.
  - **Contract address resolution**: Verify correct contract address is extracted from `FungibleInfo.Implementations["ethereum"].Address`. Verify native token (no implementation) gets empty contract address. Addresses lowercased.
  - **Direction mapping**: Verify "in", "out" directions map to DirectionIn/DirectionOut.
  - **Multiple transfers**: Verify a swap transaction with 2 transfers (one "in", one "out") converts to 2 `DecodedTransfer` entries.
  - **Nil safety**: Tests for nil FungibleInfo, nil Fee, nil ApplicationMD, empty Quantity.Int.
  - **Skip invalid**: Transactions with unparseable MinedAt are skipped.

- [x] Test: `apps/backend/internal/platform/sync/port_test.go` — Verify new domain types compile and are usable (basic struct creation test). This is primarily a compile-check since the types are plain structs.

### Acceptance Criteria
- [x] `go build ./...` succeeds with all new files
- [x] All existing tests pass (`go test ./...`)
- [x] `zerion.NewClient(apiKey)` creates a client with 30s timeout and correct base URL
- [x] Client sends `Authorization: Basic {base64(apiKey + ":")}` header on every request
- [x] Client handles 429 with exponential backoff (1s, 2s, 4s) and returns `RateLimitError` after 3 retries
- [x] Client paginates via `links.next` until empty and returns all accumulated transactions
- [x] Adapter converts `Quantity.Int` (string) to `*big.Int` for all amounts -- never uses `Quantity.Float`
- [x] Adapter converts USD prices from `float64` to `*big.Int` scaled by 10^8
- [x] Adapter handles nil `Price`, nil `Fee`, nil `ApplicationMetadata` gracefully (no panics)
- [x] Adapter returns `ErrUnsupportedChain` for unknown chain IDs
- [x] `TransactionDataProvider` interface is defined in `sync/port.go` and implemented by `zerion.SyncAdapter`
- [x] Domain types `DecodedTransaction`, `DecodedTransfer`, `DecodedFee`, `OperationType` are defined in `sync/port.go`
- [x] New code does NOT modify the existing sync pipeline (service.go, processor.go) -- those changes are Phase 3

### Risk Mitigation
- **Zerion API changes** -> Pin to v1 API (`https://api.zerion.io/v1`). Types are mapped from current API docs. If Zerion changes response shape, only `zerion/types.go` needs updating.
- **USD price precision loss** -> Converting `float64 * 1e8` to `int64` loses precision beyond 8 decimal places. This is acceptable for USD values (we never need sub-cent precision beyond 6 decimal places). For very large prices (>$92M), `int64` overflow is possible with `int64(*price * 1e8)` -- use `math.Round(*price * 1e8)` and check for overflow, or use `big.NewFloat` for the conversion.
- **Zerion Quantity.Int could be empty** -> Defensive check: if `Quantity.Int` is empty string, treat as `big.NewInt(0)` rather than panicking on `SetString`.
- **Naming collision with sync.Transfer** -> New domain types are named `DecodedTransaction`, `DecodedTransfer`, `DecodedFee` to coexist with existing `Transfer` struct in the same `sync` package. The existing `Transfer` type remains for the Alchemy pipeline until it is deprecated.
- **Chain not in mapping** -> `IDToZerionChain` doesn't cover all chains. Adapter returns `ErrUnsupportedChain` which caller can handle gracefully (skip that chain, log warning).

### Notes
- The Zerion client does NOT replace the Alchemy client yet. Both can coexist. The switchover happens in Phase 3 when the sync pipeline is updated.
- The `TransactionDataProvider` interface in `sync/port.go` lives alongside the existing `BlockchainClient` interface. They serve different purposes: `BlockchainClient` is block-range-based (Alchemy), `TransactionDataProvider` is time-based (Zerion).
- The adapter pattern matches the existing codebase: `alchemy/adapter.go` has `SyncClientAdapter` implementing `sync.BlockchainClient`; `zerion/adapter.go` has `SyncAdapter` implementing `sync.TransactionDataProvider`.
- The `doRequest` helper includes exponential backoff for 429s directly in the client (not delegated to the caller), matching the architecture validation requirement (Section 1.2: "Zerion 429 handling: exponential backoff, max 3 retries").
- `GetPositions` from the backend plan (for DeFi positions endpoint) is intentionally deferred to a later phase. Phase 2 only implements `GetTransactions`.
- The new domain types use the `Decoded` prefix to distinguish from the existing Alchemy-oriented `Transfer` struct. This naming convention makes it clear these represent Zerion's decoded/classified transactions, not raw blockchain transfers.

---

## Phase 3: Classifier + Zerion Sync Pipeline
**Platform:** Backend
**Goal:** Connect Zerion to the sync pipeline by building the classifier, Zerion processor, updating the sync service to use time-based cursors, and wiring everything together in main.go -- making Zerion the active sync data source.
**Depends on:** Phase 1 (new tx types, account types, atomic sync claim, user-scoped wallet lookup), Phase 2 (Zerion client, adapter, domain types)

### Changes

#### Backend

##### 1. Classifier (NEW file: `internal/platform/sync/classifier.go`)

Maps Zerion `OperationType` to `ledger.TransactionType`. This is a pure function with no dependencies -- easy to test in isolation.

- [ ] File: `apps/backend/internal/platform/sync/classifier.go` (NEW) — Create `Classifier` struct (stateless, no fields).
- [ ] File: `apps/backend/internal/platform/sync/classifier.go` — `NewClassifier() *Classifier` constructor.
- [ ] File: `apps/backend/internal/platform/sync/classifier.go` — `Classify(tx DecodedTransaction) ledger.TransactionType` method. Mapping:
  - `OpReceive` -> `ledger.TxTypeTransferIn`
  - `OpSend` -> `ledger.TxTypeTransferOut`
  - `OpTrade` -> `ledger.TxTypeSwap`
  - `OpDeposit`, `OpMint` -> `ledger.TxTypeDeFiDeposit`
  - `OpWithdraw`, `OpBurn` -> `ledger.TxTypeDeFiWithdraw`
  - `OpClaim` -> `ledger.TxTypeDeFiClaim`
  - `OpExecute` -> delegates to `classifyExecute(tx)`
  - `OpApprove` -> `""` (empty string = skip, no asset movement)
  - Unknown/default -> `""` (skip)
- [ ] File: `apps/backend/internal/platform/sync/classifier.go` — Private `classifyExecute(tx DecodedTransaction) ledger.TransactionType`:
  - Scans `tx.Transfers` for direction: tracks `hasIn` (direction == "in") and `hasOut` (direction == "out")
  - Both in + out -> `ledger.TxTypeSwap`
  - Only in -> `ledger.TxTypeTransferIn`
  - Only out -> `ledger.TxTypeTransferOut`
  - No transfers -> `""` (skip)

##### 2. Zerion Processor (NEW file: `internal/platform/sync/zerion_processor.go`)

Handles classification, internal transfer detection, raw data building, and ledger recording for Zerion transactions. Follows the same architectural role as the existing `Processor` (processor.go) but operates on `DecodedTransaction` instead of `Transfer`.

- [ ] File: `apps/backend/internal/platform/sync/zerion_processor.go` (NEW) — Create `ZerionProcessor` struct:
  ```
  walletRepo   WalletRepository
  ledgerSvc    LedgerService
  classifier   *Classifier
  logger       *slog.Logger
  addressCache map[string][]uuid.UUID  // userID:address -> wallet IDs
  ```
  Note: No `assetSvc` field needed -- Zerion already provides USD prices in the transaction data. The existing `Processor` (processor.go line 26) uses `assetSvc` to look up prices from CoinGecko, but Zerion provides `Price` fields directly.

- [ ] File: `apps/backend/internal/platform/sync/zerion_processor.go` — `NewZerionProcessor(walletRepo WalletRepository, ledgerSvc LedgerService, logger *slog.Logger) *ZerionProcessor` constructor. Creates `Classifier` internally.

- [ ] File: `apps/backend/internal/platform/sync/zerion_processor.go` — `ProcessTransaction(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) error`:
  1. **Skip non-confirmed**: If `tx.Status != "confirmed"`, skip (log Debug).
  2. **Classify**: Call `p.classifier.Classify(tx)`. If result is `""`, skip (log Debug "skipping operation_type=%s").
  3. **Internal transfer check**: If classified as `TxTypeTransferIn` or `TxTypeTransferOut`, check if counterparty is user's own wallet:
     - For `TxTypeTransferIn`: find the first transfer with Direction == "in", check if `transfer.Sender` is user's wallet via `isUserWallet(ctx, transfer.Sender, w.UserID)` (Phase 1 user-scoped lookup).
     - For `TxTypeTransferOut`: find the first transfer with Direction == "out", check if `transfer.Recipient` is user's wallet via `isUserWallet(ctx, transfer.Recipient, w.UserID)`.
     - If counterparty is user's wallet: reclassify as `TxTypeInternalTransfer`.
     - **Internal transfer dedup**: If Direction is "in" (receiving side), skip -- the sending side will record it (matching existing pattern at processor.go line 253).
  4. **Build raw data**: Call type-specific builder (`buildTransferInData`, `buildTransferOutData`, `buildSwapData`, `buildInternalTransferData`, `buildDeFiDepositData`, `buildDeFiWithdrawData`, `buildDeFiClaimData`).
  5. **Build external ID**: `externalID := "zerion_" + tx.ID` (ensures uniqueness, no collision with Alchemy's `source="blockchain"` records).
  6. **Record**: Call `p.ledgerSvc.RecordTransaction(ctx, txType, "zerion", &externalID, tx.MinedAt, rawData)`.
  7. **Handle duplicate**: If `isDuplicateError(err)` (Phase 1 improved version using pgconn error code), skip silently (log Debug). Otherwise return error.

- [ ] File: `apps/backend/internal/platform/sync/zerion_processor.go` — Private `isUserWallet(ctx, address string, userID uuid.UUID) bool`:
  - Lowercase the address.
  - Cache key: `userID.String() + ":" + address` (user-scoped, matching Phase 1 design).
  - Check cache first. If hit, return true.
  - Call `p.walletRepo.GetWalletsByAddressAndUserID(ctx, address, userID)` (Phase 1 method).
  - If wallets found, cache IDs and return true.
  - Otherwise return false.

- [ ] File: `apps/backend/internal/platform/sync/zerion_processor.go` — Private `getWalletByAddress(ctx, address string, userID uuid.UUID) *uuid.UUID`:
  - Same pattern as existing `getWalletByAddress()` in processor.go line 146, but uses user-scoped query.
  - Returns the first matching wallet's ID, or nil.

- [ ] File: `apps/backend/internal/platform/sync/zerion_processor.go` — Private raw data builders. Each returns `map[string]interface{}`:
  - **`buildTransferInData(w, tx)`**: Extracts the first "in"-direction transfer. Sets `wallet_id`, `asset_id` (from `DecodedTransfer.AssetSymbol`), `decimals`, `amount` (from `DecodedTransfer.Amount.String()`), `usd_rate` (from `DecodedTransfer.USDPrice.String()`), `chain_id`, `tx_hash`, `from_address` (from `DecodedTransfer.Sender`), `contract_address`, `occurred_at` (from `tx.MinedAt`), `unique_id` ("zerion_" + tx.ID), `protocol` (from `tx.Protocol`).
  - **`buildTransferOutData(w, tx)`**: Extracts the first "out"-direction transfer. Same fields but with `to_address` instead of `from_address`. Also includes gas fee data if `tx.Fee != nil`: `gas_amount`, `gas_usd_rate`, `gas_asset_id`, `gas_decimals`.
  - **`buildInternalTransferData(w, tx)`**: Sets both `source_wallet_id` and `dest_wallet_id`. Determines which wallet is source/dest based on transfer direction. Resolves counterparty wallet ID via `getWalletByAddress()`.
  - **`buildSwapData(w, tx)`**: Extracts "out"-direction transfers as sold assets and "in"-direction transfers as bought assets. Sets `sold_asset_id`, `sold_amount`, `sold_decimals`, `sold_usd_rate`, `sold_contract`, `bought_asset_id`, `bought_amount`, `bought_decimals`, `bought_usd_rate`, `bought_contract`. Includes gas fee data if present. Sets `protocol`.
  - **`buildDeFiDepositData(w, tx)`**: Similar to swap -- "out" transfers are deposited assets, "in" transfers are received tokens (LP/receipt tokens). Sets `protocol`.
  - **`buildDeFiWithdrawData(w, tx)`**: Inverse of deposit -- "out" transfers are burned/returned tokens, "in" transfers are withdrawn underlying assets. Sets `protocol`.
  - **`buildDeFiClaimData(w, tx)`**: Extracts "in"-direction transfers as claimed rewards. Sets `protocol`. Simple income pattern (like `buildTransferInData` but with DeFi context).

- [ ] File: `apps/backend/internal/platform/sync/zerion_processor.go` — `ClearCache()` method (matching existing `Processor.ClearCache()` at processor.go line 304).

##### 3. Sync Config Update (MODIFY: `internal/platform/sync/config.go`)

Remove block-based config fields that are Alchemy-specific. Add Zerion-specific config.

- [ ] File: `apps/backend/internal/platform/sync/config.go` — Add `InitialSyncLookback time.Duration` field (time-based equivalent of `InitialSyncBlockLookback`). Default: `2160 * time.Hour` (~90 days). This is how far back to look on a wallet's first Zerion sync.
- [ ] File: `apps/backend/internal/platform/sync/config.go` — Keep existing `InitialSyncBlockLookback` and `MaxBlocksPerSync` fields for backward compatibility (existing Alchemy code references them). Mark with comment `// Deprecated: Alchemy-specific, unused with Zerion provider`.
- [ ] File: `apps/backend/internal/platform/sync/config.go` — Update `DefaultConfig()` to set `InitialSyncLookback: 2160 * time.Hour`.

##### 4. Sync Service Update (MODIFY: `internal/platform/sync/service.go`)

This is the most critical change. The service switches from block-based (Alchemy) to time-based (Zerion) sync, with proper cursor safety.

- [ ] File: `apps/backend/internal/platform/sync/service.go` — Update `Service` struct (line 16) to add Zerion fields:
  ```go
  type Service struct {
      config            *Config
      blockchainClient  BlockchainClient         // Deprecated: Alchemy
      zerionProvider    TransactionDataProvider   // NEW: Zerion
      walletRepo        WalletRepository
      processor         *Processor               // Deprecated: Alchemy
      zerionProcessor   *ZerionProcessor          // NEW: Zerion
      logger            *slog.Logger
      wg                sync.WaitGroup
      stopCh            chan struct{}
      mu                sync.RWMutex
      running           bool
  }
  ```

- [ ] File: `apps/backend/internal/platform/sync/service.go` — Update `NewService()` signature (line 29) to accept Zerion provider:
  ```go
  func NewService(
      config *Config,
      zerionProvider TransactionDataProvider,
      walletRepo WalletRepository,
      ledgerSvc LedgerService,
      assetSvc AssetService,
      logger *slog.Logger,
  ) *Service
  ```
  Inside the constructor:
  - Create `ZerionProcessor` via `NewZerionProcessor(walletRepo, ledgerSvc, logger)`.
  - Set `zerionProvider` and `zerionProcessor` on the service.
  - Remove `blockchainClient` parameter and `Processor` creation.
  - The `assetSvc` parameter is kept for backward compat but may not be used by ZerionProcessor.

- [ ] File: `apps/backend/internal/platform/sync/service.go` — Rewrite `syncWallet()` (line 170) to use Zerion time-based sync:
  ```
  func (s *Service) syncWallet(ctx context.Context, w *wallet.Wallet) error {
      // 1. Claim wallet atomically (Phase 1)
      claimed, err := s.walletRepo.ClaimWalletForSync(ctx, w.ID)
      if err != nil { return err }
      if !claimed {
          s.logger.Debug("wallet already being synced, skipping", "wallet_id", w.ID)
          return nil
      }

      // 2. Determine time cursor
      var since time.Time
      if w.LastSyncAt != nil {
          since = *w.LastSyncAt
      } else {
          // Initial sync: look back InitialSyncLookback from now
          since = time.Now().Add(-s.config.InitialSyncLookback)
      }

      // 3. Fetch transactions from Zerion
      transactions, err := s.zerionProvider.GetTransactions(ctx, w.Address, w.ChainID, since)
      if err != nil {
          errMsg := fmt.Sprintf("failed to get transactions from Zerion: %v", err)
          _ = s.walletRepo.SetSyncError(ctx, w.ID, errMsg)
          return fmt.Errorf("failed to get transactions: %w", err)
      }

      // 4. Process each transaction sequentially, track last successful cursor
      var lastSuccessfulMinedAt *time.Time
      var processErrors []error
      for _, tx := range transactions {
          if err := s.zerionProcessor.ProcessTransaction(ctx, w, tx); err != nil {
              s.logger.Error("failed to process transaction",
                  "wallet_id", w.ID,
                  "tx_hash", tx.TxHash,
                  "zerion_id", tx.ID,
                  "error", err)
              processErrors = append(processErrors, err)
              // CRITICAL: Do NOT advance cursor past this failed transaction.
              // Stop advancing the cursor -- all subsequent txs are "after" this one.
              break
          }
          // Track the mined_at of the last SUCCESSFULLY committed transaction
          lastSuccessfulMinedAt = &tx.MinedAt
      }

      // 5. Update cursor ONLY to last successfully committed transaction
      if lastSuccessfulMinedAt != nil {
          if err := s.walletRepo.SetSyncCompletedAt(ctx, w.ID, *lastSuccessfulMinedAt); err != nil {
              return fmt.Errorf("failed to mark sync completed: %w", err)
          }
      } else if len(processErrors) == 0 {
          // No transactions fetched and no errors -- mark as synced with current time
          // (wallet is up to date)
          if err := s.walletRepo.SetSyncCompletedAt(ctx, w.ID, time.Now()); err != nil {
              return fmt.Errorf("failed to mark sync completed: %w", err)
          }
      } else {
          // First transaction failed, cursor not advanced
          errMsg := fmt.Sprintf("sync failed on first transaction: %v", processErrors[0])
          _ = s.walletRepo.SetSyncError(ctx, w.ID, errMsg)
      }

      // 6. Log results
      ...
      return nil
  }
  ```
  **Key cursor safety rule** (from architecture-validation Section 2.3): If transaction N fails, we break out of the loop and only advance the cursor to transaction N-1's `MinedAt`. On the next sync cycle, Zerion will return transaction N again (since `filter[min_mined_at]` is set to N-1's timestamp). Idempotency via `UNIQUE(source, external_id)` ensures already-committed transactions are silently skipped.

- [ ] File: `apps/backend/internal/platform/sync/service.go` — Update logging in `syncWallet()`: replace block-range references (`last_sync_block`, `start_block`, `end_block`) with time-based references (`last_sync_at`, `since`, `cursor_advanced_to`).

- [ ] File: `apps/backend/internal/platform/sync/service.go` — `syncAllWallets()` (line 113) and `Run()` (line 55): No changes needed -- these methods are provider-agnostic (they call `syncWallet()` which handles the provider difference internally).

##### 5. WalletRepository: SetSyncCompletedAt (MODIFY: port.go + wallet_repo.go)

- [ ] File: `apps/backend/internal/platform/sync/port.go` — Add `SetSyncCompletedAt(ctx context.Context, walletID uuid.UUID, syncAt time.Time) error` to the `WalletRepository` interface (after `SetSyncCompleted` at line 74). This is the time-based completion method for Zerion sync.
- [ ] File: `apps/backend/internal/platform/wallet/port.go` — Add matching `SetSyncCompletedAt(ctx context.Context, walletID uuid.UUID, syncAt time.Time) error` to the full `wallet.Repository` interface.
- [ ] File: `apps/backend/internal/infra/postgres/wallet_repo.go` — Implement `SetSyncCompletedAt()`:
  ```go
  func (r *WalletRepository) SetSyncCompletedAt(ctx context.Context, walletID uuid.UUID, syncAt time.Time) error {
      query := `
          UPDATE wallets
          SET sync_status = $1, last_sync_at = $2, sync_error = NULL, updated_at = $3
          WHERE id = $4
      `
      result, err := r.pool.Exec(ctx, query, wallet.SyncStatusSynced, syncAt, time.Now(), walletID)
      if err != nil {
          return fmt.Errorf("failed to set sync completed: %w", err)
      }
      if result.RowsAffected() == 0 {
          return wallet.ErrWalletNotFound
      }
      return nil
  }
  ```
  Note: This method does NOT update `last_sync_block` (it's a time-based cursor, not block-based). The existing `SetSyncCompleted()` (wallet_repo.go line 360) remains for backward compatibility but is no longer called by the Zerion sync pipeline.

##### 6. Updated main.go Wiring (MODIFY: `cmd/api/main.go`)

- [ ] File: `apps/backend/cmd/api/main.go` — Add import for `"github.com/kislikjeka/moontrack/internal/infra/gateway/zerion"`.
- [ ] File: `apps/backend/cmd/api/main.go` — Replace the Alchemy sync setup block (lines 139-169) with Zerion-based setup. The new logic:
  ```go
  // Initialize sync service
  var syncSvc *sync.Service
  if cfg.ZerionAPIKey != "" {
      // Create Zerion client and adapter
      zerionClient := zerion.NewClient(cfg.ZerionAPIKey)
      zerionAdapter := zerion.NewSyncAdapter(zerionClient)

      // Create sync service with Zerion provider
      syncConfig := &sync.Config{
          PollInterval:         cfg.SyncPollInterval,
          ConcurrentWallets:    3,
          InitialSyncLookback:  2160 * time.Hour, // ~90 days
          Enabled:              true,
      }

      syncAssetAdapter := sync.NewSyncAssetAdapter(assetSvc)
      syncSvc = sync.NewService(syncConfig, zerionAdapter, walletRepo, ledgerSvc, syncAssetAdapter, log.Logger)
      log.Info("Zerion sync service initialized", "poll_interval", cfg.SyncPollInterval)
  } else if cfg.AlchemyAPIKey != "" {
      // Fallback to Alchemy (deprecated)
      log.Warn("Using deprecated Alchemy sync. Migrate to Zerion by setting ZERION_API_KEY")
      // ... existing Alchemy setup code ...
  } else {
      log.Warn("No sync API key configured (ZERION_API_KEY or ALCHEMY_API_KEY), sync disabled")
  }
  ```
  Zerion takes priority. Alchemy is kept as fallback but logged as deprecated.
- [ ] File: `apps/backend/cmd/api/main.go` — Update `NewService()` call to match the new signature (removes `blockchainClient` parameter, adds `zerionProvider`).

#### Frontend
- No frontend changes in Phase 3.

### Tests

- [ ] Test: `apps/backend/internal/platform/sync/classifier_test.go` (NEW) — Comprehensive classifier tests:
  - `OpReceive` -> `TxTypeTransferIn`
  - `OpSend` -> `TxTypeTransferOut`
  - `OpTrade` -> `TxTypeSwap`
  - `OpDeposit` -> `TxTypeDeFiDeposit`
  - `OpMint` -> `TxTypeDeFiDeposit`
  - `OpWithdraw` -> `TxTypeDeFiWithdraw`
  - `OpBurn` -> `TxTypeDeFiWithdraw`
  - `OpClaim` -> `TxTypeDeFiClaim`
  - `OpApprove` -> `""` (skip)
  - Unknown operation type -> `""` (skip)
  - `OpExecute` with both in+out transfers -> `TxTypeSwap`
  - `OpExecute` with only in transfers -> `TxTypeTransferIn`
  - `OpExecute` with only out transfers -> `TxTypeTransferOut`
  - `OpExecute` with no transfers -> `""` (skip)
  - `OpExecute` with "self" direction transfers only -> `""` (skip)

- [ ] Test: `apps/backend/internal/platform/sync/zerion_processor_test.go` (NEW) — Processor tests using mock `WalletRepository` and mock `LedgerService`:
  - **Basic transfer_in**: Receive transaction records as `TxTypeTransferIn` with `source="zerion"`, `externalID="zerion_{txID}"`.
  - **Basic transfer_out**: Send transaction records as `TxTypeTransferOut`.
  - **Swap**: Trade transaction with 1 "out" + 1 "in" transfer records as `TxTypeSwap`. Verify raw data includes `sold_*` and `bought_*` fields.
  - **Internal transfer detection**: Receive transaction where sender is user's own wallet -> reclassified as `TxTypeInternalTransfer`. Verify sending side records it, receiving side skips.
  - **Approve skip**: Approve transaction is skipped (no `RecordTransaction` call).
  - **Failed status skip**: Transaction with `Status="failed"` is skipped.
  - **Duplicate handling**: `RecordTransaction` returns pgconn error code 23505 -> silently skipped, no error returned.
  - **USD prices from Zerion**: Verify raw data `usd_rate` comes from `DecodedTransfer.USDPrice`, not from AssetService.
  - **Gas fee data**: Transfer out with `tx.Fee != nil` -> raw data includes `gas_amount`, `gas_usd_rate`, `gas_asset_id`.
  - **DeFi deposit/withdraw/claim**: Verify correct tx types and raw data builders for each DeFi operation.
  - **Protocol field**: Verify `protocol` is included in raw data for DeFi transactions.

- [ ] Test: `apps/backend/internal/platform/sync/service_test.go` — Update existing service tests (or add new ones) for Zerion sync flow:
  - **Cursor safety**: Mock provider returns 3 transactions. Mock ledger succeeds on tx 1 and 2, fails on tx 3. Verify `SetSyncCompletedAt` is called with tx 2's `MinedAt` (not tx 3's, not `time.Now()`).
  - **Cursor safety -- first tx fails**: Mock provider returns transactions, first one fails. Verify `SetSyncError` is called, `SetSyncCompletedAt` is NOT called.
  - **Cursor safety -- all succeed**: All transactions succeed. Verify `SetSyncCompletedAt` is called with the last tx's `MinedAt`.
  - **Empty result**: No transactions returned, no errors. Verify `SetSyncCompletedAt` called with `time.Now()`.
  - **Initial sync**: Wallet with `LastSyncAt == nil`. Verify `since` is `time.Now() - InitialSyncLookback`.
  - **Incremental sync**: Wallet with `LastSyncAt` set. Verify `since` equals `*w.LastSyncAt`.
  - **Atomic claim skip**: `ClaimWalletForSync` returns false. Verify no `GetTransactions` call, no error returned.
  - **Provider error**: `GetTransactions` returns error. Verify `SetSyncError` is called with descriptive message.

- [ ] Test: `apps/backend/internal/infra/postgres/wallet_repo_test.go` — Add test for `SetSyncCompletedAt()` (integration test if DB available, or verify SQL query structure).

### Acceptance Criteria
- [ ] `go build ./...` succeeds with all changes
- [ ] All existing tests pass (`go test ./...`)
- [ ] Classifier correctly maps all 10 `OperationType` values to their expected `TransactionType`
- [ ] `classifyExecute()` correctly infers swap/transfer_in/transfer_out/skip from transfer directions
- [ ] ZerionProcessor records transactions with `source="zerion"` and `externalID="zerion_{txID}"`
- [ ] ZerionProcessor uses user-scoped wallet lookup for internal transfer detection (`GetWalletsByAddressAndUserID`)
- [ ] ZerionProcessor skips `approve` and `failed` transactions
- [ ] ZerionProcessor handles duplicate errors silently (via improved `isDuplicateError` from Phase 1)
- [ ] Sync service uses `LastSyncAt` (time-based cursor), not `LastSyncBlock`
- [ ] **Cursor safety**: Cursor ONLY advances to the `MinedAt` of the last successfully committed transaction. If a transaction fails, cursor does NOT advance past it.
- [ ] `SetSyncCompletedAt()` updates `last_sync_at` and `sync_status` without touching `last_sync_block`
- [ ] main.go wires Zerion client -> adapter -> sync service when `ZERION_API_KEY` is set
- [ ] main.go falls back to Alchemy when only `ALCHEMY_API_KEY` is set (backward compatible)
- [ ] Existing Alchemy code (client, adapter, processor) is NOT deleted -- only unused by the new sync path
- [ ] No cross-source data collision: Zerion records use `source="zerion"`, Alchemy records use `source="blockchain"` -- `UNIQUE(source, external_id)` prevents conflicts

### Risk Mitigation
- **NewService signature change breaks compilation** -> This is a breaking change to the `NewService` function. All callers (main.go, tests) must be updated in the same commit. The Alchemy fallback path in main.go will need its own `NewService` call with a different pattern (or keep a `NewAlchemyService` factory).
- **Cursor safety is critical for correctness** -> The `break` on first error in the processing loop is the key design choice. Without it, a failed tx in the middle allows later txs to succeed and advance the cursor, permanently skipping the failed tx. The architecture validation (Section 2.3) specifically flagged this. Test coverage for this scenario is mandatory.
- **Alchemy -> Zerion migration gap** -> When switching from Alchemy to Zerion for the first time, a wallet's `LastSyncAt` might be stale (set by Alchemy) while `LastSyncBlock` was the real cursor. For existing wallets with `LastSyncAt != nil`, Zerion will start from that timestamp. For wallets with `LastSyncAt == nil` but `LastSyncBlock != nil`, they will fall into the "initial sync" path (look back InitialSyncLookback). This is safe because idempotency handles any overlap. Architecture validation (Section 2.6) recommends ensuring overlap rather than gaps.
- **ZerionProcessor duplicates Processor logic** -> The raw data builders in ZerionProcessor have structural similarity to the existing Processor's record methods. This is intentional duplication rather than premature abstraction. The two processors handle fundamentally different input types (`DecodedTransaction` vs `Transfer`) and will diverge further as DeFi features are added. Extract common patterns only if they prove stable.
- **Internal transfer dedup for cross-chain** -> If user has wallets on different chains, a "transfer" from chain A wallet to chain B wallet is NOT an internal transfer (it would be a bridge). The internal transfer check uses `GetWalletsByAddressAndUserID` which matches addresses across all chains. For EVM, the same address on different chains IS the same key. Bridge detection is out of scope for Phase 3.

### Notes
- The existing `Processor` (processor.go) and `BlockchainClient` interface are NOT deleted. They remain in the codebase for backward compatibility and can be removed in a future cleanup phase once Zerion is proven stable in production.
- The `NewService()` signature change means the Alchemy fallback in main.go needs special handling. Two options: (a) create a separate `NewAlchemyService()` factory that wraps the old signature, or (b) make `zerionProvider` an optional parameter (nil = disabled). Option (b) is simpler -- if `zerionProvider` is nil, the service falls back to block-based sync using `blockchainClient`. The plan uses option (a) for clarity: the Alchemy path in main.go keeps using the old `NewService` signature until it's fully deprecated.
  - **Revised approach**: To avoid maintaining two constructors, modify `NewService()` to accept both providers as optional (`blockchainClient BlockchainClient` and `zerionProvider TransactionDataProvider`). Inside `syncWallet()`, check `s.zerionProvider != nil` to decide which path. This is the least disruptive approach.
- The `SyncAssetAdapter` (asset_adapter.go) is still passed to `NewService` for backward compatibility but is not used by `ZerionProcessor` since Zerion provides prices inline.
- DeFi raw data builders (`buildSwapData`, `buildDeFiDepositData`, etc.) produce the same `map[string]interface{}` shape that the DeFi handlers (Phase 4+) will expect. The exact field names must be coordinated with the handler implementations.
- Zerion returns transactions in chronological order by `mined_at`. The sequential processing loop in `syncWallet()` relies on this ordering for correct cursor advancement.

---

## Phase 4: DeFi Handlers (Swap, Deposit, Withdraw, Claim)
**Platform:** Backend
**Goal:** Implement the four DeFi transaction handlers that process swap, deposit, withdraw, and claim operations using the clearing account pattern for balanced double-entry accounting.
**Depends on:** Phase 1 (new TransactionType constants, AccountTypeClearing, EntryTypeClearing, accounts.type CHECK constraint migration), Phase 3 (ZerionProcessor raw data builders produce the map shapes these handlers consume)

### Changes

#### Backend

##### 1. Account Resolver Update for Clearing Accounts

The `accountResolver.parseAccountCode()` in `ledger/service.go` (lines 308-321) only recognizes `wallet.`, `income.`, `expense.`, `gas.` prefixes. Clearing accounts use the `swap_clearing.` prefix. This must be updated to support the new CLEARING account type.

- [ ] File: `apps/backend/internal/ledger/service.go` — In `parseAccountCode()` (line 308), add a new case for clearing account prefix:
  ```go
  case len(code) > 14 && code[:14] == "swap_clearing.":
      accountType = AccountTypeClearing
  ```
  This goes between the `gas.` case and the `default:` case. The prefix `swap_clearing.` matches the account code format `swap_clearing.{chain_id}` used by SwapHandler and DeFi handlers.

- [ ] File: `apps/backend/internal/ledger/model.go` — In `Account.Validate()` (lines 323-336), add `AccountTypeClearing` to the accepted types. Clearing accounts do NOT have a wallet ID (they are chain-scoped, not wallet-scoped). Add to the `case AccountTypeIncome, AccountTypeExpense, AccountTypeGasFee:` branch:
  ```go
  case AccountTypeIncome, AccountTypeExpense, AccountTypeGasFee, AccountTypeClearing:
  ```
  This already validates that `WalletID` is nil for these types, which is correct for clearing accounts.

##### 2. Balance Update Logic for Clearing Entries

The `transactionCommitter.updateBalances()` in `ledger/service.go` (lines 496-524) and `transactionValidator.validateAccountBalances()` (lines 401-443) only process entries with `EntryType` of `asset_increase` or `asset_decrease`.

**Design Decision**: Clearing entries (`EntryTypeClearing`) should NOT update `account_balances`. The clearing account is a pass-through: it always nets to zero within a single transaction. Tracking its balance in `account_balances` is unnecessary and would just add zero-balance noise. Instead, clearing account balance correctness is verified by:
1. The existing `VerifyBalance()` check (SUM debit = SUM credit) per transaction
2. The periodic clearing account monitoring job (Phase 5, Critical Fix #5)

Therefore: **No changes needed** to `updateBalances()` or `validateAccountBalances()`. The `EntryTypeClearing` entries pass through balance validation (skipped by the `!= asset_increase && != asset_decrease` filter) and are persisted as immutable entries for audit.

##### 3. DeFi Module — Model Types

- [ ] File: `apps/backend/internal/module/defi/model.go` (NEW) — Define transaction data structures for each DeFi handler. These struct field names must EXACTLY match the keys in the `map[string]interface{}` produced by `ZerionProcessor.buildSwapData()` etc. from Phase 3.

  **SwapTransaction**:
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
  With `Validate()` method: requires non-nil WalletID, non-empty SoldAssetID and BoughtAssetID, positive SoldAmount and BoughtAmount, positive ChainID, non-empty TxHash, OccurredAt not in future.
  With helper methods: `GetSoldAmount()`, `GetBoughtAmount()`, `GetSoldUSDRate()`, `GetBoughtUSDRate()`, `GetGasAmount()`, `GetGasUSDRate()` — all return `*big.Int`, following the same pattern as `TransferOutTransaction`.

  **DeFiDepositTransaction**:
  ```go
  type DeFiDepositTransaction struct {
      WalletID       uuid.UUID           `json:"wallet_id"`
      ChainID        int64               `json:"chain_id"`
      Protocol       string              `json:"protocol"`

      // Assets sent to the protocol (underlying)
      SentAssets      []DeFiAssetTransfer `json:"sent_assets"`

      // Assets received from the protocol (LP tokens, receipts)
      ReceivedAssets  []DeFiAssetTransfer `json:"received_assets"`

      GasAmount      *money.BigInt `json:"gas_amount"`
      GasUSDRate     *money.BigInt `json:"gas_usd_rate"`
      GasAssetID     string        `json:"gas_asset_id"`
      GasDecimals    int           `json:"gas_decimals"`

      TxHash         string        `json:"tx_hash"`
      BlockNumber    int64         `json:"block_number"`
      OccurredAt     time.Time     `json:"occurred_at"`
      UniqueID       string        `json:"unique_id"`
  }

  type DeFiAssetTransfer struct {
      AssetID         string        `json:"asset_id"`
      Amount          *money.BigInt `json:"amount"`
      Decimals        int           `json:"decimals"`
      USDRate         *money.BigInt `json:"usd_rate"`
      ContractAddress string        `json:"contract_address"`
  }
  ```
  With `Validate()`: requires at least 1 sent asset AND at least 1 received asset, all amounts positive, non-empty TxHash.

  **DeFiWithdrawTransaction**: Same shape as `DeFiDepositTransaction` but semantically inverted (SentAssets are LP tokens being burned, ReceivedAssets are underlying assets received back).

  **DeFiClaimTransaction**:
  ```go
  type DeFiClaimTransaction struct {
      WalletID        uuid.UUID     `json:"wallet_id"`
      ChainID         int64         `json:"chain_id"`
      Protocol        string        `json:"protocol"`

      RewardAssetID   string        `json:"reward_asset_id"`
      RewardAmount    *money.BigInt `json:"reward_amount"`
      RewardDecimals  int           `json:"reward_decimals"`
      RewardUSDRate   *money.BigInt `json:"reward_usd_rate"`
      RewardContract  string        `json:"reward_contract"`

      GasAmount       *money.BigInt `json:"gas_amount"`
      GasUSDRate      *money.BigInt `json:"gas_usd_rate"`
      GasAssetID      string        `json:"gas_asset_id"`
      GasDecimals     int           `json:"gas_decimals"`

      TxHash          string        `json:"tx_hash"`
      BlockNumber     int64         `json:"block_number"`
      OccurredAt      time.Time     `json:"occurred_at"`
      UniqueID        string        `json:"unique_id"`
  }
  ```

##### 4. DeFi Module — Error Definitions

- [ ] File: `apps/backend/internal/module/defi/errors.go` (NEW) — Define module-specific errors, following the pattern in `transfer/errors.go`:
  ```go
  var (
      ErrInvalidWalletID         = errors.New("invalid wallet ID")
      ErrInvalidAssetID          = errors.New("invalid asset ID")
      ErrInvalidAmount           = errors.New("invalid amount: must be positive")
      ErrInvalidUSDRate          = errors.New("invalid USD rate: must be non-negative")
      ErrOccurredAtInFuture      = errors.New("occurred_at cannot be in the future")
      ErrInvalidTxHash           = errors.New("invalid transaction hash")
      ErrInvalidChainID          = errors.New("invalid chain ID")
      ErrWalletNotFound          = errors.New("wallet not found")
      ErrUnauthorized            = errors.New("unauthorized: wallet does not belong to user")
      ErrNoSentAssets            = errors.New("DeFi deposit/withdraw requires at least one sent asset")
      ErrNoReceivedAssets        = errors.New("DeFi deposit/withdraw requires at least one received asset")
      ErrMissingSoldAsset        = errors.New("swap requires sold asset")
      ErrMissingBoughtAsset      = errors.New("swap requires bought asset")
  )
  ```

##### 5. DeFi Module — WalletRepository Interface

- [ ] File: `apps/backend/internal/module/defi/handler_swap.go` (NEW) — Define a local `WalletRepository` interface (same pattern as `transfer/handler_in.go` line 25-27):
  ```go
  type WalletRepository interface {
      GetByID(ctx context.Context, walletID uuid.UUID) (*wallet.Wallet, error)
  }
  ```
  This interface is defined once and shared across all defi handlers in the same package.

##### 6. SwapHandler

- [ ] File: `apps/backend/internal/module/defi/handler_swap.go` (NEW) — Implements `ledger.Handler` for `TxTypeSwap`.

  Structure:
  ```go
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

  **Handle()** and **ValidateData()**: Follow the exact same JSON marshal/unmarshal pattern as `TransferInHandler.Handle()` (handler_in.go lines 38-57) and `TransferInHandler.ValidateData()` (lines 60-94). Validate wallet ownership using `middleware.GetUserIDFromContext(ctx)`.

  **GenerateEntries()** — Produces 4 entries (+ 2 optional for gas) using the clearing account pattern:

  ```
  Entry 1: DEBIT  wallet.{wallet_id}.{bought_asset}     asset_increase   bought_amount
           Metadata: account_code, wallet_id, chain_id, tx_hash, bought_contract, protocol
  Entry 2: CREDIT swap_clearing.{chain_id}               clearing         bought_amount
           Metadata: account_code, chain_id
  Entry 3: DEBIT  swap_clearing.{chain_id}               clearing         sold_amount
           Metadata: account_code, chain_id
  Entry 4: CREDIT wallet.{wallet_id}.{sold_asset}        asset_decrease   sold_amount
           Metadata: account_code, wallet_id, chain_id, tx_hash, sold_contract, protocol

  // Gas (if gas_amount > 0):
  Entry 5: DEBIT  gas.{chain_id}.{gas_asset}             gas_fee          gas_amount
           Metadata: account_code, chain_id, tx_hash
  Entry 6: CREDIT wallet.{wallet_id}.{gas_asset}         asset_decrease   gas_amount
           Metadata: account_code, wallet_id, chain_id, tx_hash
  ```

  **Balance verification**: The amounts of entries 1+2 (bought) match, and entries 3+4 (sold) match. SUM(debit) = bought_amount + sold_amount + gas_amount = SUM(credit). The clearing entries use DIFFERENT asset amounts (bought vs sold) but since VerifyBalance() checks global sums, this balances correctly: DEBIT bought_amount + DEBIT sold_amount = CREDIT bought_amount + CREDIT sold_amount.

  **Critical note on clearing entry asset IDs**: Entry 2 (CREDIT clearing, bought_amount) has `AssetID = bought_asset`. Entry 3 (DEBIT clearing, sold_amount) has `AssetID = sold_asset`. Both use the same `account_code = swap_clearing.{chain_id}` but different asset IDs. This is correct -- the clearing account is a pass-through, not an asset-specific account. The entries are recorded for audit purposes with the correct asset references.

  **USD rate/value calculation**: Use the same pattern as `TransferOutHandler.GenerateEntries()` (handler_out.go lines 101-113). Each entry calculates its own USD value: `(amount * usd_rate) / 10^(decimals + 8)`.

##### 7. DeFiDepositHandler

- [ ] File: `apps/backend/internal/module/defi/handler_deposit.go` (NEW) — Implements `ledger.Handler` for `TxTypeDeFiDeposit`.

  Structure: Same embed pattern as SwapHandler.

  **GenerateEntries()** — For each received asset (LP tokens) and each sent asset (underlying):

  ```
  // For each received asset (LP tokens):
  Entry: DEBIT  wallet.{wallet_id}.{lp_asset}     asset_increase   lp_amount
  Entry: CREDIT swap_clearing.{chain_id}           clearing         lp_amount

  // For each sent asset (underlying deposited):
  Entry: DEBIT  swap_clearing.{chain_id}           clearing         underlying_amount
  Entry: CREDIT wallet.{wallet_id}.{underlying}    asset_decrease   underlying_amount

  // Gas (optional, same pattern as SwapHandler)
  ```

  The number of entries is dynamic: `2 * len(received) + 2 * len(sent) + (0 or 2 for gas)`.

  **Balance**: For each asset pair, DEBIT amount == CREDIT amount on clearing. Global SUM(debit) = SUM(credit) because each amount appears once as debit and once as credit.

##### 8. DeFiWithdrawHandler

- [ ] File: `apps/backend/internal/module/defi/handler_withdraw.go` (NEW) — Implements `ledger.Handler` for `TxTypeDeFiWithdraw`.

  Mirror of DeFiDepositHandler. Burns LP tokens (sent assets), receives underlying (received assets).

  ```
  // For each sent asset (LP tokens being burned):
  Entry: DEBIT  swap_clearing.{chain_id}           clearing         lp_amount
  Entry: CREDIT wallet.{wallet_id}.{lp_asset}      asset_decrease   lp_amount

  // For each received asset (underlying withdrawn):
  Entry: DEBIT  wallet.{wallet_id}.{underlying}    asset_increase   underlying_amount
  Entry: CREDIT swap_clearing.{chain_id}           clearing         underlying_amount

  // Gas (optional)
  ```

##### 9. DeFiClaimHandler

- [ ] File: `apps/backend/internal/module/defi/handler_claim.go` (NEW) — Implements `ledger.Handler` for `TxTypeDeFiClaim`.

  Simple income pattern, identical to `TransferInHandler` (handler_in.go lines 96-165) but with a DeFi-specific income account code:

  ```
  Entry 1: DEBIT  wallet.{wallet_id}.{reward_asset}         asset_increase   reward_amount
           Metadata: account_code = wallet.{wallet_id}.{reward_asset}, wallet_id, chain_id, protocol
  Entry 2: CREDIT income.defi.{chain_id}.{protocol}         income           reward_amount
           Metadata: account_code = income.defi.{chain_id}.{protocol}, chain_id, protocol

  // Gas (optional):
  Entry 3: DEBIT  gas.{chain_id}.{gas_asset}                gas_fee          gas_amount
  Entry 4: CREDIT wallet.{wallet_id}.{gas_asset}            asset_decrease   gas_amount
  ```

  The income account code uses `income.defi.{chain_id}.{protocol}` instead of `income.{chain_id}.{asset_id}` to track DeFi income by protocol. The account resolver's `income.` prefix match (line 312) already handles this.

##### 10. Handler Registration in main.go

- [ ] File: `apps/backend/cmd/api/main.go` — After the existing transfer handler registrations (lines 117-128), add DeFi handler registrations:
  ```go
  import "github.com/kislikjeka/moontrack/internal/module/defi"

  // DeFi handlers
  swapHandler := defi.NewSwapHandler(walletRepo)
  if err := handlerRegistry.Register(swapHandler); err != nil {
      log.Error("Failed to register swap handler", "error", err)
      os.Exit(1)
  }
  log.Info("Registered swap handler")

  defiDepositHandler := defi.NewDeFiDepositHandler(walletRepo)
  if err := handlerRegistry.Register(defiDepositHandler); err != nil {
      log.Error("Failed to register DeFi deposit handler", "error", err)
      os.Exit(1)
  }
  log.Info("Registered DeFi deposit handler")

  defiWithdrawHandler := defi.NewDeFiWithdrawHandler(walletRepo)
  if err := handlerRegistry.Register(defiWithdrawHandler); err != nil {
      log.Error("Failed to register DeFi withdraw handler", "error", err)
      os.Exit(1)
  }
  log.Info("Registered DeFi withdraw handler")

  defiClaimHandler := defi.NewDeFiClaimHandler(walletRepo)
  if err := handlerRegistry.Register(defiClaimHandler); err != nil {
      log.Error("Failed to register DeFi claim handler", "error", err)
      os.Exit(1)
  }
  log.Info("Registered DeFi claim handler")
  ```
  Note: `walletRepo` is `postgres.NewWalletRepository(db.Pool)` (line 99), which satisfies the `defi.WalletRepository` interface since it has `GetByID()`.

#### Frontend
- No frontend changes in Phase 4.

#### Database Migrations
- No new migrations in Phase 4. The CLEARING account type was added in Phase 1's migration 000008. New clearing accounts are created dynamically by the account resolver when the first swap/deposit/withdraw transaction is processed.

### Tests

- [ ] Test: `apps/backend/internal/module/defi/handler_swap_test.go` (NEW) — SwapHandler tests:
  - **Balanced entries**: Swap 1 ETH -> 2500 USDC. Verify SUM(debit) = SUM(credit). Verify 4 entries generated (no gas).
  - **With gas**: Swap with gas fee. Verify 6 entries generated. Gas entries use correct native asset ID.
  - **Clearing account codes**: Verify clearing entries have `account_code = swap_clearing.{chain_id}` and correct chain ID in metadata.
  - **Wallet account codes**: Verify debit entry has `account_code = wallet.{wallet_id}.{bought_asset}`, credit entry has `account_code = wallet.{wallet_id}.{sold_asset}`.
  - **USD value calculation**: Verify USD values are correctly computed as `(amount * usd_rate) / 10^(decimals + 8)`.
  - **Validation -- missing sold asset**: SwapTransaction with empty SoldAssetID -> error.
  - **Validation -- missing bought asset**: SwapTransaction with empty BoughtAssetID -> error.
  - **Validation -- zero sold amount**: SwapTransaction with SoldAmount = 0 -> error.
  - **Validation -- wallet ownership**: Context user != wallet owner -> ErrUnauthorized.
  - **Validation -- wallet not found**: Non-existent wallet ID -> ErrWalletNotFound.

- [ ] Test: `apps/backend/internal/module/defi/handler_deposit_test.go` (NEW) — DeFiDepositHandler tests:
  - **Single LP deposit**: Send 1 ETH + 2500 USDC, receive 1 LP token. Verify 6 entries (2 received + 4 sent). Balanced.
  - **Multi-asset deposit**: Send 3 underlying assets, receive 1 LP token. Verify 8 entries. Balanced.
  - **With gas**: Verify gas entries appended. Balanced.
  - **Validation -- no sent assets**: Empty SentAssets -> ErrNoSentAssets.
  - **Validation -- no received assets**: Empty ReceivedAssets -> ErrNoReceivedAssets.

- [ ] Test: `apps/backend/internal/module/defi/handler_withdraw_test.go` (NEW) — DeFiWithdrawHandler tests:
  - **Mirror of deposit tests**: Send LP token, receive underlying assets. Verify correct debit/credit direction (opposite of deposit).
  - **Balanced entries**: Verify SUM(debit) = SUM(credit) for all scenarios.

- [ ] Test: `apps/backend/internal/module/defi/handler_claim_test.go` (NEW) — DeFiClaimHandler tests:
  - **Simple claim**: Receive reward tokens. Verify 2 entries: DEBIT wallet (asset_increase), CREDIT income.defi (income).
  - **Income account code**: Verify `account_code = income.defi.{chain_id}.{protocol}` for the income entry.
  - **With gas**: Verify 4 entries total. Balanced.
  - **Zero USD rate**: Claim with unknown price (usd_rate = 0). Should succeed, not error.

- [ ] Test: `apps/backend/internal/ledger/service_test.go` — Add or update tests for account resolver:
  - **Clearing account creation**: Entry with `account_code = swap_clearing.1` creates account with `Type = CLEARING`, `WalletID = nil`.
  - **Clearing account reuse**: Second transaction on same chain reuses the existing clearing account.

### Acceptance Criteria
- [ ] `go build ./...` succeeds with all changes
- [ ] All existing tests pass (`go test ./...`)
- [ ] SwapHandler generates balanced entries (4 without gas, 6 with gas) using clearing account pattern
- [ ] DeFiDepositHandler handles variable number of sent/received assets with dynamic entry count
- [ ] DeFiWithdrawHandler is the mirror of DeFiDepositHandler (opposite direction)
- [ ] DeFiClaimHandler uses `income.defi.{chain_id}.{protocol}` account code for DeFi-specific income tracking
- [ ] Account resolver correctly creates CLEARING type accounts for `swap_clearing.*` account codes
- [ ] `Account.Validate()` accepts CLEARING type accounts with `WalletID = nil`
- [ ] Clearing entries (`EntryTypeClearing`) do NOT affect `account_balances` (by design -- only `asset_increase`/`asset_decrease` update balances)
- [ ] All 4 handlers registered in main.go without errors
- [ ] All model `Validate()` methods reject invalid data (missing assets, zero amounts, future dates)
- [ ] Wallet ownership verification works for all 4 handlers (consistent with transfer handlers)
- [ ] Raw data field names in defi/model.go structs match the keys produced by ZerionProcessor raw data builders from Phase 3

### Risk Mitigation
- **Account resolver prefix matching is order-dependent** -> The `swap_clearing.` check (14 chars) must come BEFORE shorter prefixes. Since all current prefixes are 4-8 chars and `swap_clearing.` is 14 chars, there's no conflict. But if a new prefix starting with "swap" is added later, length-based ordering matters.
- **Clearing account balance drift** -> If a handler bug produces unbalanced clearing entries, the clearing account accumulates a non-zero balance with no automated detection. This is addressed by the Clearing Account Monitoring job in Phase 5 (Critical Fix #5). In the interim, the per-transaction `VerifyBalance()` check catches any global imbalance within a single transaction.
- **Multi-asset DeFi entries increase entry count** -> A deposit with 5 underlying assets + 1 LP token + gas = 14 entries. This is more than typical transactions (2-4 entries). No hard limit exists on entry count, but test with realistic maximums.
- **USD rate coordination with ZerionProcessor** -> The raw data maps produced by Phase 3's `buildSwapData()` etc. must use the exact JSON keys that Phase 4's model structs expect (e.g., `sold_amount` not `sellAmount`). This must be verified during implementation. The plan specifies `snake_case` keys consistently.
- **`income.defi.` prefix still matches `income.`** -> The account resolver matches `income.` prefix (7 chars) which covers both `income.{chain_id}` and `income.defi.{chain_id}`. Both produce `AccountTypeIncome` accounts. This is correct -- DeFi income is still income.

### Notes
- The DeFi handlers follow the exact same structural pattern as the transfer handlers in `internal/module/transfer/`. The key difference is the clearing account pattern for swaps/deposits/withdrawals, where amounts of different assets pass through a clearing account to maintain global balance.
- The `EntryTypeClearing` value was added in Phase 1. It is intentionally NOT processed by `updateBalances()` because clearing accounts should always net to zero within a transaction. If we tracked clearing balances, they'd always be zero, adding storage overhead for no value.
- The `DeFiAssetTransfer` struct is shared between deposit and withdraw models. This is not a premature abstraction -- both genuinely handle lists of multi-asset transfers in the same format.
- Gas fee handling in all 4 handlers follows the same pattern as `TransferOutHandler.GenerateEntries()` (handler_out.go lines 164-222). The native asset ID comes from the transaction data (populated by ZerionProcessor from Zerion's fee info).
- The WalletRepository interface in the defi package is a minimal subset (`GetByID` only), same as the transfer package. This avoids coupling the defi module to the full wallet.Repository interface.

---

## Phase 5: Tax Lots & Cost Basis System
**Platform:** Backend
**Goal:** Implement the lot-based cost basis tracking system with FIFO disposal, cost basis override, genesis lots for existing data, and clearing account monitoring. This phase integrates with the ledger commit flow via a post-commit hook.
**Depends on:** Phase 1 (new TransactionType constants, database foundation), Phase 4 (DeFi handlers that produce asset acquisitions/disposals)

### Changes

#### Backend

##### 1. Database Migration — Tax Lots Schema

- [ ] File: `apps/backend/migrations/000009_tax_lots.up.sql` (NEW) — Create the tax lots tables, views, and indexes:

  ```sql
  -- Tax lots table: tracks each acquisition of an asset
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

      -- Link to source lot (for internal transfers, carries cost basis)
      linked_source_lot_id         UUID REFERENCES tax_lots(id),

      created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

      CONSTRAINT positive_quantities CHECK (
          quantity_acquired > 0
          AND quantity_remaining >= 0
          AND quantity_remaining <= quantity_acquired
      ),
      CONSTRAINT non_negative_cost_basis CHECK (
          auto_cost_basis_per_unit >= 0
      ),
      CONSTRAINT override_cost_basis_non_negative CHECK (
          override_cost_basis_per_unit IS NULL OR override_cost_basis_per_unit >= 0
      )
  );

  -- FIFO query index: open lots for an account+asset ordered by acquisition time
  CREATE INDEX idx_tax_lots_fifo ON tax_lots (account_id, asset, acquired_at)
      WHERE quantity_remaining > 0;

  CREATE INDEX idx_tax_lots_transaction ON tax_lots (transaction_id);
  CREATE INDEX idx_tax_lots_account_asset ON tax_lots (account_id, asset);

  -- Lot disposals table: tracks each disposal from a lot
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
      CONSTRAINT valid_disposal_type CHECK (disposal_type IN ('sale', 'internal_transfer')),
      CONSTRAINT non_negative_proceeds CHECK (proceeds_per_unit >= 0)
  );

  CREATE INDEX idx_lot_disposals_transaction ON lot_disposals (transaction_id);
  CREATE INDEX idx_lot_disposals_lot ON lot_disposals (lot_id);

  -- Override history table (audit trail for cost basis changes)
  CREATE TABLE lot_override_history (
      id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
      lot_id              UUID NOT NULL REFERENCES tax_lots(id),
      previous_cost_basis NUMERIC(78,0),
      new_cost_basis      NUMERIC(78,0),
      reason              TEXT NOT NULL, -- Required for audit trail
      changed_at          TIMESTAMPTZ NOT NULL DEFAULT now()
  );

  CREATE INDEX idx_lot_override_history_lot ON lot_override_history (lot_id);

  -- Effective cost basis view: priority is override > linked_lot > auto
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

  -- WAC (Weighted Average Cost) materialized view
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

- [ ] File: `apps/backend/migrations/000009_tax_lots.down.sql` (NEW):
  ```sql
  DROP MATERIALIZED VIEW IF EXISTS position_wac;
  DROP VIEW IF EXISTS tax_lots_effective;
  DROP TABLE IF EXISTS lot_override_history;
  DROP TABLE IF EXISTS lot_disposals;
  DROP TABLE IF EXISTS tax_lots;
  ```

##### 2. TaxLot Domain Model

- [ ] File: `apps/backend/internal/platform/taxlot/model.go` (NEW) — Domain types:

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
      AutoCostBasisPerUnit     *big.Int    // USD scaled by 10^8
      AutoCostBasisSource      CostBasisSource
      OverrideCostBasisPerUnit *big.Int    // nullable
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
      ProceedsPerUnit  *big.Int    // USD scaled by 10^8
      DisposalType     DisposalType
      DisposedAt       time.Time
      CreatedAt        time.Time
  }

  type CostBasisSource string
  const (
      SourceSwapPrice      CostBasisSource = "swap_price"
      SourceFMVAtTransfer  CostBasisSource = "fmv_at_transfer"
      SourceLinkedTransfer CostBasisSource = "linked_transfer"
  )

  type DisposalType string
  const (
      DisposalSale             DisposalType = "sale"
      DisposalInternalTransfer DisposalType = "internal_transfer"
  )
  ```

##### 3. TaxLot Repository Interface

- [ ] File: `apps/backend/internal/platform/taxlot/port.go` (NEW) — Repository interface:

  ```go
  package taxlot

  type Repository interface {
      // Core FIFO operations
      CreateTaxLot(ctx context.Context, lot *TaxLot) error
      GetOpenLotsFIFO(ctx context.Context, accountID uuid.UUID, asset string) ([]*TaxLot, error)
      UpdateLotRemaining(ctx context.Context, lotID uuid.UUID, newRemaining *big.Int) error
      CreateDisposal(ctx context.Context, disposal *LotDisposal) error

      // Override operations
      GetTaxLot(ctx context.Context, id uuid.UUID) (*TaxLot, error)
      UpdateOverride(ctx context.Context, lotID uuid.UUID, costBasis *big.Int, reason string) error
      CreateOverrideHistory(ctx context.Context, lotID uuid.UUID, prev, newCB *big.Int, reason string) error

      // Query operations
      GetLotsByAccountAndAsset(ctx context.Context, accountID uuid.UUID, asset string) ([]*TaxLot, error)
      GetDisposalsByTransaction(ctx context.Context, txID uuid.UUID) ([]*LotDisposal, error)
      GetLotsByTransaction(ctx context.Context, txID uuid.UUID) ([]*TaxLot, error)

      // WAC
      RefreshWAC(ctx context.Context) error
  }
  ```

  **Critical**: `GetOpenLotsFIFO()` MUST use `SELECT ... FOR UPDATE` to prevent concurrent disposal of the same lots. The `ORDER BY acquired_at ASC` ensures deterministic locking order (prevents deadlocks per architecture validation Section 3.2).

##### 4. TaxLot Repository Implementation

- [ ] File: `apps/backend/internal/infra/postgres/taxlot_repo.go` (NEW) — PostgreSQL implementation of `taxlot.Repository`.

  Key method signatures:
  ```go
  type TaxLotRepository struct {
      pool *pgxpool.Pool
  }

  func NewTaxLotRepository(pool *pgxpool.Pool) *TaxLotRepository
  ```

  **GetOpenLotsFIFO** (most critical method):
  ```sql
  SELECT id, transaction_id, account_id, asset,
         quantity_acquired, quantity_remaining, acquired_at,
         auto_cost_basis_per_unit, auto_cost_basis_source,
         override_cost_basis_per_unit, override_reason, override_at,
         linked_source_lot_id, created_at
  FROM tax_lots
  WHERE account_id = $1 AND asset = $2 AND quantity_remaining > 0
  ORDER BY acquired_at ASC
  FOR UPDATE
  ```
  Uses the `idx_tax_lots_fifo` partial index. The `FOR UPDATE` lock ensures atomicity with the existing `transactionCommitter` DB transaction.

  **Important**: This repository's methods must work within the existing `ledger.Repository.BeginTx/CommitTx` transaction context. The `ctx` parameter carries the transaction handle. The repository must accept the `pgxpool.Pool` for non-transactional queries but use the transaction from context when available (same pattern as `ledger_repo.go`).

  **RefreshWAC**:
  ```sql
  REFRESH MATERIALIZED VIEW CONCURRENTLY position_wac
  ```

##### 5. TaxLot Service

- [ ] File: `apps/backend/internal/platform/taxlot/service.go` (NEW) — Business logic:

  ```go
  type Service struct {
      repo Repository
  }

  func NewService(repo Repository) *Service
  ```

  **CreateLot** — Creates a TaxLot when an asset is acquired:
  ```go
  func (s *Service) CreateLot(ctx context.Context, lot *TaxLot) error {
      // Validate: quantity > 0, cost basis >= 0, valid source
      return s.repo.CreateTaxLot(ctx, lot)
  }
  ```

  **DisposeFIFO** — Disposes quantity from oldest lots first:
  ```go
  func (s *Service) DisposeFIFO(
      ctx context.Context,
      accountID uuid.UUID,
      asset string,
      quantity *big.Int,
      proceedsPerUnit *big.Int,
      txID uuid.UUID,
      disposalType DisposalType,
      disposedAt time.Time,
  ) error {
      // 1. GetOpenLotsFIFO (SELECT ... FOR UPDATE)
      lots, err := s.repo.GetOpenLotsFIFO(ctx, accountID, asset)
      // 2. remaining := quantity (copy)
      // 3. For each lot (oldest first):
      //    disposeQty := min(lot.QuantityRemaining, remaining)
      //    Create LotDisposal { LotID: lot.ID, QuantityDisposed: disposeQty, ProceedsPerUnit, ... }
      //    Update lot.QuantityRemaining -= disposeQty
      //    remaining -= disposeQty
      //    if remaining == 0: break
      // 4. If remaining > 0: return ErrInsufficientBalance
      // 5. Return nil
  }
  ```

  **OverrideCostBasis** — Sets manual cost basis:
  ```go
  func (s *Service) OverrideCostBasis(
      ctx context.Context,
      lotID uuid.UUID,
      newCostBasis *big.Int,
      reason string,
  ) error {
      // 1. Get current lot
      // 2. Record in override history (previous value)
      // 3. Update lot override fields
  }
  ```

##### 6. Post-Commit Hook Integration in Ledger Service

The architecture validation (Section 3.3) requires TaxLot operations to run INSIDE the same DB transaction as the ledger commit. This is critical: if ledger entries succeed but tax lot creation fails, or vice versa, the data becomes inconsistent.

**Design**: Rather than a post-commit hook (which runs AFTER commit and would be in a separate transaction), integrate TaxLot operations into `transactionCommitter.commit()`, between `updateBalances()` and `CommitTx()`.

- [ ] File: `apps/backend/internal/ledger/service.go` — Add `PostCommitHook` interface and integrate into the commit flow:

  ```go
  // PostCommitHook runs inside the DB transaction after entries are committed
  // but before the transaction is finalized. This ensures atomicity.
  type PostCommitHook interface {
      AfterEntries(ctx context.Context, tx *Transaction) error
  }
  ```

- [ ] File: `apps/backend/internal/ledger/service.go` — Add `postCommitHooks []PostCommitHook` field to `Service` struct (line 14):
  ```go
  type Service struct {
      repo            Repository
      handlerRegistry *Registry
      accountResolver *accountResolver
      validator       *transactionValidator
      committer       *transactionCommitter
      postCommitHooks []PostCommitHook  // NEW
  }
  ```

- [ ] File: `apps/backend/internal/ledger/service.go` — Add `RegisterPostCommitHook()` method:
  ```go
  func (s *Service) RegisterPostCommitHook(hook PostCommitHook) {
      s.postCommitHooks = append(s.postCommitHooks, hook)
  }
  ```

- [ ] File: `apps/backend/internal/ledger/service.go` — In `transactionCommitter.commit()` (lines 455-488), add hook execution between `updateBalances()` and `CommitTx()`. Since `committer` doesn't have access to hooks, pass them via method parameter or restructure. Simplest approach: move hook execution to `Service.RecordTransaction()` by splitting `commit()`:

  **Revised approach**: Add `hooks []PostCommitHook` parameter to `commit()`:
  ```go
  func (c *transactionCommitter) commit(ctx context.Context, tx *Transaction, hooks []PostCommitHook) error {
      txCtx, err := c.repo.BeginTx(ctx)
      // ... existing code ...

      if err := c.updateBalances(txCtx, tx); err != nil {
          return fmt.Errorf("failed to update balances: %w", err)
      }

      // Run post-commit hooks INSIDE the DB transaction
      for _, hook := range hooks {
          if err := hook.AfterEntries(txCtx, tx); err != nil {
              return fmt.Errorf("post-commit hook failed: %w", err)
          }
      }

      if err := c.repo.CommitTx(txCtx); err != nil {
          return fmt.Errorf("failed to commit transaction: %w", err)
      }
      // ...
  }
  ```

  And update the call in `RecordTransaction()` (line 118):
  ```go
  if err := s.committer.commit(ctx, tx, s.postCommitHooks); err != nil {
  ```

##### 7. TaxLot Hook Implementation

- [ ] File: `apps/backend/internal/platform/taxlot/hook.go` (NEW) — Implements `ledger.PostCommitHook`:

  ```go
  type TaxLotHook struct {
      svc *Service
  }

  func NewTaxLotHook(svc *Service) *TaxLotHook

  func (h *TaxLotHook) AfterEntries(ctx context.Context, tx *ledger.Transaction) error {
      // Inspect tx.Type and tx.Entries to determine:
      // 1. Asset acquisitions (DEBIT wallet with asset_increase) -> CreateLot
      // 2. Asset disposals (CREDIT wallet with asset_decrease) -> DisposeFIFO
      //
      // Skip if tx.Type is internal_transfer (handled specially)
      //
      // For each acquisition entry:
      //   - Determine cost basis source based on tx.Type:
      //     - transfer_in -> SourceFMVAtTransfer (use entry.USDRate)
      //     - swap -> SourceSwapPrice (use the sold asset's USD rate as implied price)
      //     - defi_claim -> SourceFMVAtTransfer (use entry.USDRate)
      //     - defi_deposit -> SourceSwapPrice (LP token valued at sum of underlying)
      //     - defi_withdraw -> SourceFMVAtTransfer (underlying valued at entry.USDRate)
      //   - CreateLot with quantity = entry.Amount, account = entry.AccountID
      //
      // For each disposal entry:
      //   - Determine proceeds based on tx.Type
      //   - DisposeFIFO with quantity = entry.Amount
      //   - On ErrInsufficientBalance: log warning, don't fail the transaction
      //     (This handles the case where lots haven't been backfilled yet)
  }
  ```

  **Critical decision on insufficient balance**: If FIFO disposal can't find enough lots (e.g., because genesis lots haven't been created yet), the hook should log a warning but NOT fail the transaction. The ledger entry is the source of truth; tax lots are supplementary. A reconciliation job can create missing lots later.

##### 8. TaxLot Hook Registration in main.go

- [ ] File: `apps/backend/cmd/api/main.go` — After ledger service initialization (line 107), register the TaxLot hook:
  ```go
  // Initialize TaxLot service
  taxLotRepo := postgres.NewTaxLotRepository(db.Pool)
  taxLotSvc := taxlot.NewService(taxLotRepo)
  taxLotHook := taxlot.NewTaxLotHook(taxLotSvc)
  ledgerSvc.RegisterPostCommitHook(taxLotHook)
  log.Info("TaxLot hook registered")
  ```
  Import: `"github.com/kislikjeka/moontrack/internal/platform/taxlot"`

##### 9. Genesis Lots Migration (Critical Fix #4)

This addresses architecture validation Section 4.2 and Edge Case #8: existing transactions have no TaxLots. Without genesis lots, any sell/swap of existing assets will fail FIFO disposal with "Insufficient balance".

- [ ] File: `apps/backend/internal/platform/taxlot/genesis.go` (NEW) — Genesis lot creation:

  ```go
  // CreateGenesisLots creates TaxLots for existing account balances.
  // For each CRYPTO_WALLET account with positive balance:
  //   - Create a genesis TaxLot with:
  //     quantity_acquired = quantity_remaining = current balance
  //     acquired_at = earliest transaction occurred_at for this account (or account.created_at)
  //     auto_cost_basis_per_unit = current USD rate (FMV at migration time)
  //     auto_cost_basis_source = "fmv_at_transfer"
  //     transaction_id = earliest transaction ID for this account
  //
  // This is a one-time migration function, NOT part of normal operation.
  func (s *Service) CreateGenesisLots(ctx context.Context, ledgerRepo ledger.Repository) error
  ```

  **Run once**: This should be called during application startup with a flag or as a one-time CLI command, NOT on every restart. Use a flag table or check if any tax_lots exist before running.

  **Alternative**: A SQL data migration (000010) that creates genesis lots from existing data:
  ```sql
  INSERT INTO tax_lots (transaction_id, account_id, asset, quantity_acquired, quantity_remaining,
                         acquired_at, auto_cost_basis_per_unit, auto_cost_basis_source)
  SELECT
      (SELECT t.id FROM transactions t
       JOIN entries e ON e.transaction_id = t.id
       WHERE e.account_id = ab.account_id AND e.asset_id = ab.asset_id
       ORDER BY t.occurred_at ASC LIMIT 1) as transaction_id,
      ab.account_id,
      ab.asset_id,
      ab.balance,
      ab.balance,
      (SELECT MIN(t.occurred_at) FROM transactions t
       JOIN entries e ON e.transaction_id = t.id
       WHERE e.account_id = ab.account_id AND e.asset_id = ab.asset_id) as acquired_at,
      COALESCE(ab.usd_value * 100000000 / NULLIF(ab.balance, 0), 0) as auto_cost_basis_per_unit,
      'fmv_at_transfer'
  FROM account_balances ab
  JOIN accounts a ON a.id = ab.account_id
  WHERE a.type = 'CRYPTO_WALLET'
    AND ab.balance > 0
    AND NOT EXISTS (SELECT 1 FROM tax_lots tl WHERE tl.account_id = ab.account_id AND tl.asset = ab.asset_id);
  ```

  **Recommendation**: Use the SQL migration approach (000010) for simplicity and atomicity. The Go function is for more complex scenarios (e.g., backfilling with historical prices).

- [ ] File: `apps/backend/migrations/000010_genesis_tax_lots.up.sql` (NEW) — Genesis lots data migration:
  ```sql
  -- Create genesis tax lots for existing CRYPTO_WALLET accounts with positive balances
  -- This is idempotent: only creates lots where none exist for the account+asset
  INSERT INTO tax_lots (transaction_id, account_id, asset, quantity_acquired, quantity_remaining,
                         acquired_at, auto_cost_basis_per_unit, auto_cost_basis_source)
  SELECT
      sub.first_tx_id,
      ab.account_id,
      ab.asset_id,
      ab.balance,
      ab.balance,
      sub.first_occurred_at,
      CASE WHEN ab.balance > 0
           THEN COALESCE(ab.usd_value * 100000000 / ab.balance, 0)
           ELSE 0
      END,
      'fmv_at_transfer'
  FROM account_balances ab
  JOIN accounts a ON a.id = ab.account_id
  CROSS JOIN LATERAL (
      SELECT t.id as first_tx_id, t.occurred_at as first_occurred_at
      FROM transactions t
      JOIN entries e ON e.transaction_id = t.id
      WHERE e.account_id = ab.account_id AND e.asset_id = ab.asset_id
      ORDER BY t.occurred_at ASC
      LIMIT 1
  ) sub
  WHERE a.type = 'CRYPTO_WALLET'
    AND ab.balance > 0
    AND NOT EXISTS (
        SELECT 1 FROM tax_lots tl
        WHERE tl.account_id = ab.account_id AND tl.asset = ab.asset_id
    );
  ```

- [ ] File: `apps/backend/migrations/000010_genesis_tax_lots.down.sql` (NEW):
  ```sql
  -- Remove genesis lots (lots with auto_cost_basis_source = 'fmv_at_transfer' and no linked lot)
  -- Only safe to run if no disposals have been made against genesis lots
  DELETE FROM tax_lots
  WHERE auto_cost_basis_source = 'fmv_at_transfer'
    AND linked_source_lot_id IS NULL
    AND quantity_remaining = quantity_acquired;
  ```

##### 10. Clearing Account Monitoring (Critical Fix #5)

This addresses architecture validation Edge Case #20: clearing accounts should always have zero net balance. A non-zero balance indicates a handler bug.

- [ ] File: `apps/backend/internal/platform/taxlot/clearing_monitor.go` (NEW) — Periodic clearing account balance check:

  ```go
  type ClearingMonitor struct {
      ledgerRepo ledger.Repository
      logger     *slog.Logger
      interval   time.Duration
  }

  func NewClearingMonitor(ledgerRepo ledger.Repository, logger *slog.Logger) *ClearingMonitor

  func (m *ClearingMonitor) Run(ctx context.Context) {
      ticker := time.NewTicker(m.interval) // default: 1 hour
      for {
          select {
          case <-ctx.Done():
              return
          case <-ticker.C:
              m.check(ctx)
          }
      }
  }

  func (m *ClearingMonitor) check(ctx context.Context) {
      // 1. Find all CLEARING type accounts
      // 2. For each, get account balance
      // 3. If balance != 0, log.Error with account details
      //    (In production, this would trigger an alert)
  }
  ```

  Note: This uses `ledger.Repository` to query accounts and balances. The `FindAccountsByType(AccountTypeClearing)` method does not currently exist on the Repository interface. Two options:
  - (a) Add `FindAccountsByType(ctx, AccountType) ([]*Account, error)` to `ledger.Repository` and implement in postgres
  - (b) Use a raw SQL query via a dedicated ClearingMonitor repository
  Recommendation: Option (a) is cleaner and may be useful elsewhere.

- [ ] File: `apps/backend/internal/ledger/port.go` — Add to Repository interface:
  ```go
  FindAccountsByType(ctx context.Context, accountType AccountType) ([]*Account, error)
  ```

- [ ] File: `apps/backend/internal/infra/postgres/ledger_repo.go` — Implement `FindAccountsByType()`:
  ```sql
  SELECT id, code, type, asset_id, wallet_id, chain_id, created_at, metadata
  FROM accounts
  WHERE type = $1
  ```

- [ ] File: `apps/backend/cmd/api/main.go` — Start clearing monitor:
  ```go
  clearingMonitor := taxlot.NewClearingMonitor(ledgerRepo, log.Logger)
  go clearingMonitor.Run(ctx)
  log.Info("Clearing account monitor started (1 hour interval)")
  ```

#### Frontend
- No frontend changes in Phase 5. Frontend tax lot UI is deferred to a later phase.

#### Database Migrations
- 000009: Tax lots schema (tables, views, indexes)
- 000010: Genesis tax lots data migration (backfill existing balances)

### Tests

- [ ] Test: `apps/backend/internal/platform/taxlot/service_test.go` (NEW) — TaxLot service tests:
  - **CreateLot**: Create a lot, verify fields persisted.
  - **DisposeFIFO -- single lot, full disposal**: 100 units acquired, dispose 100. Lot remaining = 0. One disposal record created.
  - **DisposeFIFO -- single lot, partial disposal**: 100 acquired, dispose 60. Remaining = 40. Disposal quantity = 60.
  - **DisposeFIFO -- multi-lot, oldest first**: Lot A (50 units, acquired 2024-01-01), Lot B (80 units, acquired 2024-02-01). Dispose 70. Lot A remaining = 0 (fully consumed), Lot B remaining = 60. Two disposal records.
  - **DisposeFIFO -- insufficient balance**: 100 total remaining, try to dispose 150. Returns `ErrInsufficientBalance`. No disposals created, no lot quantities changed (transaction rolled back).
  - **DisposeFIFO -- empty lots**: No open lots exist. Returns `ErrInsufficientBalance`.
  - **OverrideCostBasis**: Override, verify lot fields updated. Override history record created with previous and new values.
  - **OverrideCostBasis -- remove override**: Set override to nil. Falls back to auto cost basis.

- [ ] Test: `apps/backend/internal/platform/taxlot/hook_test.go` (NEW) — Hook integration tests:
  - **Transfer in creates lot**: Transaction with `TxTypeTransferIn` and one `asset_increase` entry. Verify lot created with `SourceFMVAtTransfer`, quantity = entry amount, account = entry account.
  - **Swap creates lot + disposal**: Transaction with `TxTypeSwap`. Verify: lot created for bought asset, disposal triggered for sold asset.
  - **DeFi claim creates lot**: Transaction with `TxTypeDeFiClaim` and one `asset_increase`. Verify lot created.
  - **DeFi deposit creates lots + disposals**: Lot for LP token received, disposals for underlying sent.
  - **DeFi withdraw creates lots + disposals**: Lots for underlying received, disposal for LP token sent.
  - **Internal transfer -- linked lot**: Transaction with `TxTypeInternalTransfer`. Source wallet lot disposed with `DisposalInternalTransfer`. Destination wallet lot created with `SourceLinkedTransfer` and `LinkedSourceLotID` pointing to source lot.
  - **Insufficient balance on disposal -- warning, no failure**: Disposal for asset with no lots. Verify transaction is NOT failed, warning is logged.

- [ ] Test: `apps/backend/internal/infra/postgres/taxlot_repo_test.go` (NEW) — Repository tests (integration if DB available):
  - **GetOpenLotsFIFO ordering**: Create 3 lots with different acquired_at. Verify returned in ascending order.
  - **GetOpenLotsFIFO filters**: Create lots, set one to remaining = 0. Verify it's excluded from results.
  - **UpdateLotRemaining**: Verify quantity updates correctly. Verify constraint: remaining <= acquired.
  - **RefreshWAC**: Verify materialized view refresh completes without error.

- [ ] Test: `apps/backend/internal/platform/taxlot/clearing_monitor_test.go` (NEW):
  - **All clearing accounts zero**: No errors logged.
  - **Non-zero clearing account**: Error logged with account details.

- [ ] Test: `apps/backend/internal/ledger/service_test.go` — Add tests for post-commit hook:
  - **Hook called after successful commit**: Register mock hook, record transaction, verify `AfterEntries` called with correct transaction.
  - **Hook failure rolls back transaction**: Hook returns error. Verify entire transaction (entries + balances) rolled back.
  - **Multiple hooks**: Register 2 hooks. Both called in order.

### Acceptance Criteria
- [ ] `go build ./...` succeeds with all changes
- [ ] All existing tests pass (`go test ./...`)
- [ ] Migration 000009 creates tax_lots, lot_disposals, lot_override_history tables, tax_lots_effective view, position_wac materialized view
- [ ] Migration 000010 creates genesis lots for all CRYPTO_WALLET accounts with positive balances
- [ ] Genesis lots migration is idempotent (running twice creates no duplicates)
- [ ] FIFO disposal selects lots in `acquired_at ASC` order with `FOR UPDATE` lock
- [ ] FIFO disposal creates correct disposal records and updates lot remaining quantities
- [ ] FIFO disposal with insufficient balance returns error (not panic)
- [ ] Override cost basis updates override fields and records history
- [ ] Override removal (set to NULL) falls back to auto cost basis via `tax_lots_effective` view
- [ ] `PostCommitHook.AfterEntries()` runs INSIDE the DB transaction (same txCtx as entries/balances)
- [ ] Hook failure causes full transaction rollback (entries, balances, and lots all rolled back)
- [ ] TaxLotHook creates lots on asset acquisitions (transfer_in, swap bought, claim, deposit LP, withdraw underlying)
- [ ] TaxLotHook triggers FIFO disposal on asset disposals (transfer_out, swap sold, deposit underlying, withdraw LP)
- [ ] TaxLotHook logs warning but does NOT fail on insufficient lots (graceful degradation)
- [ ] Internal transfer creates linked lot with `SourceLinkedTransfer` and `LinkedSourceLotID`
- [ ] Internal transfer disposal uses `DisposalInternalTransfer` type
- [ ] Clearing monitor checks all CLEARING accounts for zero balance
- [ ] Clearing monitor logs error for non-zero clearing account balance
- [ ] `FindAccountsByType(CLEARING)` method works on ledger.Repository
- [ ] position_wac materialized view can be refreshed concurrently

### Risk Mitigation
- **TaxLot operations inside ledger transaction increase transaction duration** -> FIFO disposal involves SELECT FOR UPDATE + multiple UPDATEs + INSERTs. For a typical disposal across 1-3 lots, this adds ~5-10ms. For a swap touching hundreds of micro-lots, it could be slower. Monitor transaction durations after deployment. If problematic, consider batch processing or reducing lot fragmentation.
- **Genesis lots cost basis approximation** -> Genesis lots use current `usd_value / balance` as cost basis. This is an approximation (average of all historical acquisitions at current prices). Users can override individual lot cost bases via the API. Document this limitation.
- **Hook failure causes full rollback** -> If the TaxLot hook fails (e.g., DB error creating lot), the entire ledger transaction is rolled back. This is by design -- we prefer consistency over availability. However, a persistent hook failure (e.g., bug in hook logic) would block ALL transactions. Mitigation: extensive testing + the hook logs warnings for insufficient lots rather than erroring.
- **FOR UPDATE deadlock between concurrent wallets** -> Two different wallets syncing simultaneously may both dispose lots from the same shared account (e.g., if user moved assets between wallets). FIFO's consistent `acquired_at ASC` ordering prevents deadlocks within the same account. Cross-account locking doesn't occur because each wallet touches its own accounts.
- **Migration 000010 on large datasets** -> The genesis lots migration uses a CROSS JOIN LATERAL subquery. For databases with many accounts, this could be slow. Consider running during a maintenance window. The migration is idempotent so it can be restarted if interrupted.
- **Clearing monitor adds a new Repository method** -> Adding `FindAccountsByType` to `ledger.Repository` requires updating all implementations (postgres, and any test mocks). This is a small interface change but needs to be coordinated.

### Notes
- The `PostCommitHook` name is somewhat misleading since it runs BEFORE commit. A better name might be `TransactionHook` or `PreCommitHook`, but `PostCommitHook` (meaning "after entries are committed to the transaction object, before DB commit") is used to match the architecture documentation terminology.
- The `AfterEntries` method receives the full `*Transaction` including entries with resolved AccountIDs. This is essential for the TaxLotHook to know which accounts are affected.
- The clearing monitor is placed in the `taxlot` package because it's logically part of the cost basis system's integrity checks. It could alternatively live in a dedicated `monitoring` package.
- The position_wac materialized view refresh is lazy -- triggered before reads, not after writes. This avoids slowing down transaction commits. A 30-60 second staleness is acceptable for portfolio display.
- The `lot_override_history.reason` column is NOT NULL (differs from `tax_lots.override_reason` which is nullable). This is intentional -- every override change must have a documented reason for audit purposes.
- TaxLot queries use the existing DB transaction context passed through `ctx`. The `pgxpool.Pool` transactions propagated via context in `ledger_repo.go` (BeginTx/CommitTx) also work for the TaxLot repository because both repositories share the same pool and the transaction is scoped to the context.

---

## Phase 6: Frontend — Transaction Type Enhancements
**Platform:** Frontend
**Goal:** Extend the frontend to display new DeFi transaction types (swap, defi_deposit, defi_withdraw, defi_claim) in the existing transactions list and detail pages, including protocol badges and swap-specific display.
**Depends on:** Phase 1 (new TransactionType constants on backend), Phase 4 (DeFi handlers that produce these transaction types)

### Changes

#### Frontend

##### 1. Extend TransactionType Union

- [ ] File: `apps/frontend/src/types/transaction.ts` (line 2-6) — Extend the `TransactionType` union with 4 new DeFi types:
  ```typescript
  export type TransactionType =
    | 'transfer_in'
    | 'transfer_out'
    | 'internal_transfer'
    | 'asset_adjustment'
    | 'swap'              // NEW
    | 'defi_deposit'      // NEW
    | 'defi_withdraw'     // NEW
    | 'defi_claim'        // NEW
  ```

##### 2. Extend TransactionListItem Interface

- [ ] File: `apps/frontend/src/types/transaction.ts` (line 11-25) — Add optional DeFi fields to `TransactionListItem`:
  ```typescript
  export interface TransactionListItem {
    // ...existing fields...
    protocol?: string           // NEW: "Uniswap V3", "GMX V2", etc.
    // Swap-specific display fields (populated by backend for swap transactions):
    sold_asset_symbol?: string  // NEW
    sold_amount?: string        // NEW: display amount (formatted)
    bought_asset_symbol?: string // NEW
    bought_amount?: string      // NEW: display amount (formatted)
  }
  ```

##### 3. Extend TransactionDetail Interface

- [ ] File: `apps/frontend/src/types/transaction.ts` (line 40-47) — Add swap/DeFi detail fields to `TransactionDetail`:
  ```typescript
  export interface TransactionDetail extends TransactionListItem {
    // ...existing fields...
    // Swap detail fields (NEW):
    sold_asset_id?: string
    sold_display_amount?: string
    sold_usd_value?: string
    bought_asset_id?: string
    bought_display_amount?: string
    bought_usd_value?: string
    // DeFi context (NEW):
    chain_name?: string
  }
  ```

##### 4. Update TransactionTypeBadge

- [ ] File: `apps/frontend/src/components/domain/TransactionTypeBadge.tsx` — Update imports to add new icons and extend `typeConfig` (lines 13-41).

  Add to imports (line 1):
  ```typescript
  import { ArrowDownLeft, ArrowUpRight, ArrowLeftRight, RefreshCw, Repeat, ArrowDownToLine, ArrowUpFromLine, Gift } from 'lucide-react'
  ```

  Extend `typeConfig` with 4 new entries after `asset_adjustment` (line 40):
  ```typescript
  swap: {
    label: 'Swap',
    icon: Repeat,
    variant: 'swap',
  },
  defi_deposit: {
    label: 'Deposit',
    icon: ArrowDownToLine,
    variant: 'liquidity',
  },
  defi_withdraw: {
    label: 'Withdraw',
    icon: ArrowUpFromLine,
    variant: 'liquidity',
  },
  defi_claim: {
    label: 'Claim',
    icon: Gift,
    variant: 'profit',
  },
  ```

  Update the `variant` type in the `typeConfig` Record type (line 18) to include the new variants:
  ```typescript
  variant: 'profit' | 'loss' | 'transfer' | 'swap' | 'liquidity'
  ```

  Note: Badge variants `swap`, `liquidity`, and `profit` already exist in `badge.tsx` (lines 23, 20, 18). No changes needed to the Badge component itself.

##### 5. Create ProtocolBadge Component

- [ ] File: `apps/frontend/src/components/domain/ProtocolBadge.tsx` (NEW) — Small inline badge showing protocol name:
  ```typescript
  import { Badge } from '@/components/ui/badge'
  import { cn } from '@/lib/utils'

  interface ProtocolBadgeProps {
    protocol: string
    size?: 'sm' | 'default'
    className?: string
  }

  export function ProtocolBadge({ protocol, size = 'sm', className }: ProtocolBadgeProps) {
    return (
      <Badge
        variant="outline"
        className={cn(
          size === 'sm' ? 'text-[10px] px-1.5 py-0' : 'text-xs',
          className
        )}
      >
        {protocol}
      </Badge>
    )
  }
  ```

##### 6. Update TransactionFilters

- [ ] File: `apps/frontend/src/features/transactions/TransactionFilters.tsx` (lines 16-21) — Add new DeFi types to the `transactionTypes` array:
  ```typescript
  const transactionTypes: { value: TransactionType; label: string }[] = [
    { value: 'transfer_in', label: 'Transfer In' },
    { value: 'transfer_out', label: 'Transfer Out' },
    { value: 'internal_transfer', label: 'Internal Transfer' },
    { value: 'asset_adjustment', label: 'Adjustment' },
    { value: 'swap', label: 'Swap' },                // NEW
    { value: 'defi_deposit', label: 'DeFi Deposit' }, // NEW
    { value: 'defi_withdraw', label: 'DeFi Withdraw' }, // NEW
    { value: 'defi_claim', label: 'DeFi Claim' },    // NEW
  ]
  ```

##### 7. Update TransactionsPage for Swap Display

- [ ] File: `apps/frontend/src/features/transactions/TransactionsPage.tsx` — Update the transaction table rows (lines 93-129) to handle swap transactions differently:
  - In the "Type" column (line 96-98): After the `TransactionTypeBadge`, conditionally render a `ProtocolBadge` if `tx.protocol` exists.
  - In the "Asset" column (line 108-111): For swap transactions, show `{sold_asset} → {bought_asset}` instead of just `{asset_symbol}`.
  - In the "Amount" column (line 113-116): For swap transactions, show `-{sold_amount} / +{bought_amount}` instead of single amount.

  Specific changes to the table body:
  ```tsx
  {/* Type column with optional protocol badge */}
  <TableCell>
    <Link to={`/transactions/${tx.id}`} className="flex items-center gap-1.5">
      <TransactionTypeBadge type={tx.type} />
      {tx.protocol && <ProtocolBadge protocol={tx.protocol} />}
    </Link>
  </TableCell>

  {/* Asset column - swap vs normal */}
  <TableCell>
    <Link to={`/transactions/${tx.id}`} className="font-medium">
      {tx.type === 'swap' && tx.sold_asset_symbol && tx.bought_asset_symbol
        ? `${tx.sold_asset_symbol} → ${tx.bought_asset_symbol}`
        : tx.asset_symbol}
    </Link>
  </TableCell>

  {/* Amount column - swap vs normal */}
  <TableCell className="text-right font-mono">
    <Link to={`/transactions/${tx.id}`}>
      {tx.type === 'swap' && tx.sold_amount && tx.bought_amount
        ? `${tx.sold_amount} → ${tx.bought_amount}`
        : tx.display_amount}
    </Link>
  </TableCell>
  ```

  Add import for `ProtocolBadge`:
  ```typescript
  import { ProtocolBadge } from '@/components/domain/ProtocolBadge'
  ```

##### 8. Create SwapDetailSection

- [ ] File: `apps/frontend/src/features/transactions/SwapDetailSection.tsx` (NEW) — Sold/bought side-by-side display for swap transaction detail:
  ```typescript
  import { ArrowRight } from 'lucide-react'
  import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
  import type { TransactionDetail } from '@/types/transaction'

  interface SwapDetailSectionProps {
    transaction: TransactionDetail
  }

  export function SwapDetailSection({ transaction }: SwapDetailSectionProps) {
    // Renders two cards side by side: "Sold" and "Bought"
    // Each shows: asset symbol, display amount, USD value
    // Arrow icon between them
    // Uses formatUSDValue for USD display
  }
  ```

##### 9. Update TransactionDetailPage

- [ ] File: `apps/frontend/src/features/transactions/TransactionDetailPage.tsx` — Add protocol info and swap detail sections:

  After the header section (line 82), before the details card (line 85):
  - If `transaction.protocol` exists, show a protocol info row in the details card with protocol name and chain name.
  - If `transaction.type === 'swap'`, render the `SwapDetailSection` component between the header and details card.

  Add to the details card `<dl>` (line 90): conditional protocol and chain rows:
  ```tsx
  {transaction.protocol && (
    <div>
      <dt className="text-sm text-muted-foreground">Protocol</dt>
      <dd className="font-medium flex items-center gap-2">
        <ProtocolBadge protocol={transaction.protocol} size="default" />
      </dd>
    </div>
  )}
  {transaction.chain_name && (
    <div>
      <dt className="text-sm text-muted-foreground">Chain</dt>
      <dd className="font-medium">{transaction.chain_name}</dd>
    </div>
  )}
  ```

  Before the details card, add swap section:
  ```tsx
  {transaction.type === 'swap' && (
    <SwapDetailSection transaction={transaction} />
  )}
  ```

  Add imports:
  ```typescript
  import { SwapDetailSection } from './SwapDetailSection'
  import { ProtocolBadge } from '@/components/domain/ProtocolBadge'
  ```

#### Backend
- No backend code changes in Phase 6. The backend already returns the new transaction types and protocol field when transactions are created by the Zerion sync pipeline (Phases 1-4). The existing `GET /transactions` and `GET /transactions/{id}` endpoints are type-agnostic -- they return whatever `type` the transaction has.
- **Note**: The backend's `TransactionService` in `module/transactions/service.go` may need to be updated to populate `protocol`, `sold_asset_symbol`, `bought_asset_symbol`, `sold_amount`, `bought_amount` from `raw_data` for the enriched transaction list view. This is a minor backend enhancement that should be coordinated with this phase.

### Tests

- [ ] Test: `apps/frontend/src/components/domain/TransactionTypeBadge.test.tsx` — Update or add tests:
  - Renders `swap` type with Repeat icon and "Swap" label and `swap` variant
  - Renders `defi_deposit` type with ArrowDownToLine icon and "Deposit" label and `liquidity` variant
  - Renders `defi_withdraw` type with ArrowUpFromLine icon and "Withdraw" label and `liquidity` variant
  - Renders `defi_claim` type with Gift icon and "Claim" label and `profit` variant

- [ ] Test: `apps/frontend/src/components/domain/ProtocolBadge.test.tsx` (NEW) — Basic render tests:
  - Renders protocol name text
  - Renders with `outline` variant
  - Renders in `sm` and `default` sizes

- [ ] Test: `apps/frontend/src/features/transactions/TransactionsPage.test.tsx` — Update with swap transaction rendering:
  - Swap transaction row shows `ETH → USDC` in asset column
  - Swap transaction row shows protocol badge
  - Non-swap transactions render normally (regression)

### Acceptance Criteria
- [ ] `bun run test --run` passes in `apps/frontend/`
- [ ] `bun run build` succeeds in `apps/frontend/`
- [ ] `bun run lint` passes in `apps/frontend/`
- [ ] TransactionTypeBadge renders correct icon, label, and variant for all 8 transaction types
- [ ] TransactionFilters dropdown includes all 8 transaction types
- [ ] Swap transactions in list show `sold → bought` in asset and amount columns
- [ ] Protocol badge appears next to type badge when protocol field is present
- [ ] SwapDetailSection shows sold/bought side by side on transaction detail page
- [ ] Protocol and chain info shown in transaction detail when present
- [ ] Existing transaction types (transfer_in, transfer_out, internal_transfer, asset_adjustment) still render correctly (no regressions)

### Risk Mitigation
- **Backend must populate enriched fields** -> The swap-specific display fields (`sold_asset_symbol`, `bought_asset_symbol`, etc.) must come from the backend's transaction list response. If the backend only stores these in `raw_data`, the `TransactionService` needs to extract and populate them in the list response. Coordinate with backend Phase 4 implementation.
- **Badge variant type narrowing** -> The `typeConfig` Record uses a union variant type. If a transaction arrives with an unknown type (e.g., future type added to backend), `typeConfig[type]` will be `undefined`. Add a fallback: `const config = typeConfig[type] ?? { label: type, icon: RefreshCw, variant: 'transfer' as const }`.

### Notes
- No new routes, pages, services, or hooks are needed in Phase 6. This phase only modifies existing components and types.
- The Badge component already has `swap` and `liquidity` variants defined (badge.tsx lines 23, 20). No CSS changes needed.
- The `ProtocolBadge` uses the `outline` Badge variant for a subtle, non-colored appearance since protocols are informational, not action-oriented.
- The transaction list API response shape is unchanged -- the backend will include new fields (`protocol`, `sold_asset_symbol`, etc.) in the existing response. Frontend types are extended with optional fields to remain backward compatible.

---

## Phase 7: Frontend — DeFi Positions Page + Backend API
**Platform:** Both (Frontend + Backend)
**Goal:** Create the DeFi Positions page that shows active DeFi positions from Zerion, with backend endpoint to fetch positions.
**Depends on:** Phase 2 (Zerion client), Phase 6 (ProtocolBadge component)

### Changes

#### Backend

##### 1. Zerion GetPositions Client Method

- [ ] File: `apps/backend/internal/infra/gateway/zerion/client.go` — Add `GetPositions()` method (already specified in Phase 2's backend plan but not included in Phase 2's scope):
  ```go
  func (c *Client) GetPositions(ctx context.Context, address string, chainID string) ([]PositionData, error)
  ```
  Calls Zerion API: `GET /v1/wallets/{address}/positions/?filter[chain_ids]={chainID}&filter[position_types]=deposit,staked,locked,lending,farming`

- [ ] File: `apps/backend/internal/infra/gateway/zerion/types.go` — Add position response types:
  ```go
  type PositionResponse struct {
      Links Links          `json:"links"`
      Data  []PositionData `json:"data"`
  }

  type PositionData struct {
      Type       string              `json:"type"` // "positions"
      ID         string              `json:"id"`
      Attributes PositionAttributes  `json:"attributes"`
  }

  type PositionAttributes struct {
      PositionType string                `json:"position_type"` // deposit, staked, locked, lending, farming
      Name         string                `json:"name"`          // "ETH/USDC Pool"
      Protocol     string                `json:"protocol"`      // "Uniswap V3"
      Value        *float64              `json:"value"`         // total USD value
      Changes      map[string]float64    `json:"changes"`       // 24h, etc.
      FungibleInfo *FungibleInfo         `json:"fungible_info"` // LP token info
      Quantity     Quantity              `json:"quantity"`
      Implementations map[string]Implementation `json:"implementations"`
  }
  ```

##### 2. DeFi Positions Transport Handler

- [ ] File: `apps/backend/internal/transport/httpapi/handler/defi.go` (NEW) — HTTP handler for DeFi positions:
  ```go
  type DeFiHandler struct {
      zerionClient *zerion.Client
      walletRepo   wallet.Repository
  }

  func NewDeFiHandler(zerionClient *zerion.Client, walletRepo wallet.Repository) *DeFiHandler

  // GetDeFiPositions handles GET /portfolio/defi-positions?wallet_id={id}
  func (h *DeFiHandler) GetDeFiPositions(w http.ResponseWriter, r *http.Request)
  ```
  Logic: Get user's wallets (filtered by wallet_id if provided), for each wallet call Zerion GetPositions, aggregate and return.

##### 3. Router Update

- [ ] File: `apps/backend/internal/transport/httpapi/router.go` — Add `DeFiHandler` to Config struct (line 14) and route (inside protected group, after portfolio routes line 86):
  ```go
  // Config
  DeFiHandler *handler.DeFiHandler

  // Route
  if cfg.DeFiHandler != nil {
      r.Get("/portfolio/defi-positions", cfg.DeFiHandler.GetDeFiPositions)
  }
  ```

##### 4. main.go Wiring

- [ ] File: `apps/backend/cmd/api/main.go` — Create and wire DeFiHandler (after Zerion client initialization from Phase 3):
  ```go
  var defiHandler *handler.DeFiHandler
  if zerionClient != nil {
      defiHandler = handler.NewDeFiHandler(zerionClient, walletRepo)
  }
  ```
  Add `DeFiHandler: defiHandler` to router Config.

#### Frontend

##### 5. DeFi Types

- [ ] File: `apps/frontend/src/types/defi.ts` (NEW) — Type definitions for DeFi positions:
  ```typescript
  export interface DefiPosition {
    id: string
    wallet_id: string
    wallet_name: string
    chain_id: number
    protocol: string
    position_type: 'deposit' | 'staked' | 'locked' | 'lending' | 'farming'
    name: string
    underlying_tokens: UnderlyingToken[]
    total_usd_value: string
    updated_at: string
  }

  export interface UnderlyingToken {
    asset_id: string
    symbol: string
    amount: string
    usd_value: string
  }

  export interface DefiPositionsResponse {
    positions: DefiPosition[]
    total_value: string
  }
  ```

##### 6. DeFi Service

- [ ] File: `apps/frontend/src/services/defi.ts` (NEW) — API calls following `services/transaction.ts` pattern:
  ```typescript
  import api from './api'
  import type { DefiPositionsResponse } from '@/types/defi'

  export const defiService = {
    async getPositions(walletId?: string): Promise<DefiPositionsResponse> {
      const response = await api.get<DefiPositionsResponse>('/portfolio/defi-positions', {
        params: walletId ? { wallet_id: walletId } : undefined,
      })
      return response.data
    },
  }
  ```

##### 7. DeFi Positions Hook

- [ ] File: `apps/frontend/src/hooks/useDefiPositions.ts` (NEW) — TanStack Query hook following `hooks/useTransactions.ts` pattern:
  ```typescript
  import { useQuery } from '@tanstack/react-query'
  import { defiService } from '@/services/defi'
  import type { DefiPositionsResponse } from '@/types/defi'

  export function useDefiPositions(walletId?: string) {
    return useQuery<DefiPositionsResponse>({
      queryKey: ['defi-positions', walletId],
      queryFn: () => defiService.getPositions(walletId),
    })
  }
  ```

##### 8. DefiPositionCard Component

- [ ] File: `apps/frontend/src/components/domain/DefiPositionCard.tsx` (NEW) — Card component for displaying a single DeFi position. Uses `Card` from shadcn/ui, `ProtocolBadge` from Phase 6, `Badge` for chain/position type.

  Shows: protocol badge, chain name, position name, underlying tokens with amounts and USD values, total USD value, wallet name.

##### 9. DefiPositionsPage

- [ ] File: `apps/frontend/src/features/defi/DefiPositionsPage.tsx` (NEW) — Main page component following `TransactionsPage` pattern:
  - Page header with title "DeFi Positions" and wallet filter dropdown (same `Select` pattern as `TransactionFilters`)
  - Summary stats row: Total Value, Active Positions, Protocols (using `StatCard` from `DashboardPage` pattern)
  - Position cards grouped by protocol
  - Empty state with `Layers` icon: "No DeFi positions found. Positions will appear after your wallets sync DeFi activity."
  - Loading skeleton matching card layout
  - Uses `useDefiPositions(walletId)` hook and `useWallets()` for the filter dropdown

##### 10. Route and Navigation

- [ ] File: `apps/frontend/src/app/App.tsx` — Add route for DeFi positions page (after wallet routes, line 59):
  ```tsx
  import DefiPositionsPage from '@/features/defi/DefiPositionsPage'

  <Route
    path="/defi-positions"
    element={
      <ProtectedRoute>
        <Layout>
          <DefiPositionsPage />
        </Layout>
      </ProtectedRoute>
    }
  />
  ```

- [ ] File: `apps/frontend/src/components/layout/Sidebar.tsx` — Add DeFi nav item to `navItems` array (line 24-29), between Wallets and Transactions:
  ```typescript
  import { LayoutDashboard, Wallet, ArrowLeftRight, Settings, LogOut, ChevronLeft, ChevronRight, Moon, Layers } from 'lucide-react'

  const navItems = [
    { to: '/dashboard', label: 'Dashboard', icon: LayoutDashboard },
    { to: '/wallets', label: 'Wallets', icon: Wallet },
    { to: '/defi-positions', label: 'DeFi', icon: Layers },  // NEW
    { to: '/transactions', label: 'Transactions', icon: ArrowLeftRight },
    { to: '/settings', label: 'Settings', icon: Settings },
  ]
  ```

### Tests

- [ ] Test: `apps/frontend/src/features/defi/DefiPositionsPage.test.tsx` (NEW):
  - Renders page title "DeFi Positions"
  - Shows positions when data is returned
  - Shows empty state when no positions
  - Shows loading skeleton while fetching

- [ ] Test: `apps/frontend/src/components/domain/DefiPositionCard.test.tsx` (NEW):
  - Renders protocol name, position name, total value
  - Renders underlying tokens with amounts

- [ ] Test: Backend `handler/defi_test.go` (NEW):
  - Returns positions for user's wallets
  - Filters by wallet_id parameter
  - Returns empty array when no positions
  - Returns 401 for unauthenticated request

### Acceptance Criteria
- [ ] `bun run test --run` and `bun run build` and `bun run lint` pass in `apps/frontend/`
- [ ] `go build ./...` and `go test ./...` pass in `apps/backend/`
- [ ] `GET /portfolio/defi-positions` returns DeFi positions from Zerion
- [ ] DeFi Positions page renders positions grouped by protocol
- [ ] Wallet filter dropdown works on DeFi Positions page
- [ ] "DeFi" nav item appears in sidebar between "Wallets" and "Transactions"
- [ ] Empty state shown when no DeFi positions exist
- [ ] Page is protected (requires authentication)

### Notes
- The DeFi positions endpoint calls Zerion directly on each request (no caching in Phase 7). Future optimization: cache positions in Redis with a 5-minute TTL.
- The `DefiPositionCard` component is reused in Phase 9 on the WalletDetailPage DeFi tab.
- Position types from Zerion (`deposit`, `staked`, `locked`, `lending`, `farming`) are displayed as badges on the card.

---

## Phase 8: Frontend — Cost Basis & Tax Lots Page + Backend API
**Platform:** Both (Frontend + Backend)
**Goal:** Create the Cost Basis page with asset overview, expandable lot detail, cost basis override dialog, and transfer linking dialog, backed by new API endpoints.
**Depends on:** Phase 5 (TaxLot service, repository, database tables)

### Changes

#### Backend

##### 1. TaxLot HTTP Handler

- [ ] File: `apps/backend/internal/transport/httpapi/handler/taxlot.go` (NEW) — HTTP handlers for tax lot operations:
  ```go
  type TaxLotHandler struct {
      taxLotSvc *taxlot.Service
      taxLotRepo taxlot.Repository
      ledgerRepo ledger.Repository
  }

  func NewTaxLotHandler(taxLotSvc *taxlot.Service, taxLotRepo taxlot.Repository, ledgerRepo ledger.Repository) *TaxLotHandler

  // GetLots handles GET /lots?account_id={id}&asset={asset}
  func (h *TaxLotHandler) GetLots(w http.ResponseWriter, r *http.Request)

  // OverrideCostBasis handles PUT /lots/{id}/override
  // Body: { "cost_basis_per_unit": "180000000000", "reason": "Actual purchase price" }
  func (h *TaxLotHandler) OverrideCostBasis(w http.ResponseWriter, r *http.Request)

  // GetWAC handles GET /positions/wac?account_id={id}
  func (h *TaxLotHandler) GetWAC(w http.ResponseWriter, r *http.Request)
  ```

  **Important**: `GetLots` must verify lot ownership through account -> wallet -> user chain. `OverrideCostBasis` must verify lot ownership AND validate positive cost basis and non-empty reason.

##### 2. Router Update

- [ ] File: `apps/backend/internal/transport/httpapi/router.go` — Add `TaxLotHandler` to Config struct and routes (inside protected group):
  ```go
  // Config
  TaxLotHandler *handler.TaxLotHandler

  // Routes
  if cfg.TaxLotHandler != nil {
      r.Get("/lots", cfg.TaxLotHandler.GetLots)
      r.Put("/lots/{id}/override", cfg.TaxLotHandler.OverrideCostBasis)
      r.Get("/positions/wac", cfg.TaxLotHandler.GetWAC)
  }
  ```

##### 3. main.go Wiring

- [ ] File: `apps/backend/cmd/api/main.go` — Wire TaxLotHandler (after taxLotSvc initialization from Phase 5):
  ```go
  taxLotHandler := handler.NewTaxLotHandler(taxLotSvc, taxLotRepo, ledgerRepo)
  ```
  Add `TaxLotHandler: taxLotHandler` to router Config.

#### Frontend

##### 4. TaxLot Types

- [ ] File: `apps/frontend/src/types/taxlot.ts` (NEW) — Type definitions matching backend response shapes:
  ```typescript
  export interface TaxLot {
    id: string
    transaction_id: string
    account_id: string
    asset: string
    quantity_acquired: string
    quantity_remaining: string
    acquired_at: string
    auto_cost_basis_per_unit: string
    auto_cost_basis_source: 'swap_price' | 'fmv_at_transfer' | 'linked_transfer'
    override_cost_basis_per_unit?: string
    override_reason?: string
    override_at?: string
    effective_cost_basis_per_unit: string
    linked_source_lot_id?: string
  }

  export interface PositionWAC {
    account_id: string
    asset: string
    total_quantity: string
    weighted_avg_cost: string
  }

  export interface OverrideCostBasisRequest {
    cost_basis_per_unit: string
    reason: string
  }
  ```

##### 5. TaxLot Service

- [ ] File: `apps/frontend/src/services/taxlot.ts` (NEW) — API calls:
  ```typescript
  import api from './api'
  import type { TaxLot, PositionWAC, OverrideCostBasisRequest } from '@/types/taxlot'

  export const taxLotService = {
    async getLots(accountId: string, asset: string): Promise<TaxLot[]> {
      const response = await api.get<TaxLot[]>('/lots', {
        params: { account_id: accountId, asset },
      })
      return response.data
    },
    async overrideCostBasis(lotId: string, data: OverrideCostBasisRequest): Promise<void> {
      await api.put(`/lots/${lotId}/override`, data)
    },
    async getWAC(accountId?: string): Promise<PositionWAC[]> {
      const response = await api.get<PositionWAC[]>('/positions/wac', {
        params: accountId ? { account_id: accountId } : undefined,
      })
      return response.data
    },
  }
  ```

##### 6. TaxLot Hooks

- [ ] File: `apps/frontend/src/hooks/useTaxLots.ts` (NEW) — Query and mutation hooks:
  ```typescript
  export function useTaxLots(accountId: string, asset: string)
  export function useOverrideCostBasis()
  export function usePositionWAC(accountId?: string)
  ```
  Following the same pattern as `hooks/useTransactions.ts`. `useOverrideCostBasis` invalidates `['tax-lots']`, `['portfolio']`, and `['position-wac']` query keys on success.

##### 7. LotDetailTable Component

- [ ] File: `apps/frontend/src/components/domain/LotDetailTable.tsx` (NEW) — Expandable table showing individual tax lots for an asset:
  - Columns: Lot #, Acquired Date, Quantity Acquired, Remaining, Cost/Unit (effective), Source, Actions
  - "Edit" button in actions column opens the override dialog
  - Source column shows badge: "Swap", "FMV", "Linked"
  - Uses `Table` components from shadcn/ui

##### 8. CostBasisOverrideDialog Component

- [ ] File: `apps/frontend/src/components/domain/CostBasisOverrideDialog.tsx` (NEW) — Modal dialog for editing cost basis on a tax lot:
  - Uses shadcn `Dialog`, `Input`, `Label`, `Button`, `Textarea`
  - Shows current lot info: asset, quantity, current auto cost basis, source
  - Input for new cost basis per unit (USD)
  - Required textarea for reason
  - Calls `useOverrideCostBasis()` mutation on submit
  - Shows toast on success, inline error on failure

##### 9. CostBasisPage

- [ ] File: `apps/frontend/src/features/cost-basis/CostBasisPage.tsx` (NEW) — Main page:
  - Page header with title "Cost Basis" and wallet/asset filter dropdowns
  - Asset overview table: Asset, Holdings (quantity), WAC, Current Price, Unrealized PnL
  - Click on row to expand showing `LotDetailTable` for that asset
  - Empty state with Calculator icon: "No tax lots yet. Cost basis is tracked automatically when assets are acquired through swaps or transfers."
  - Uses `usePositionWAC()` hook for overview, `useTaxLots(accountId, asset)` for expanded detail

##### 10. Route and Navigation

- [ ] File: `apps/frontend/src/app/App.tsx` — Add route for Cost Basis page:
  ```tsx
  import CostBasisPage from '@/features/cost-basis/CostBasisPage'

  <Route
    path="/cost-basis"
    element={
      <ProtectedRoute>
        <Layout>
          <CostBasisPage />
        </Layout>
      </ProtectedRoute>
    }
  />
  ```

- [ ] File: `apps/frontend/src/components/layout/Sidebar.tsx` — Add Cost Basis nav item after Transactions:
  ```typescript
  import { ..., Calculator } from 'lucide-react'

  const navItems = [
    { to: '/dashboard', label: 'Dashboard', icon: LayoutDashboard },
    { to: '/wallets', label: 'Wallets', icon: Wallet },
    { to: '/defi-positions', label: 'DeFi', icon: Layers },
    { to: '/transactions', label: 'Transactions', icon: ArrowLeftRight },
    { to: '/cost-basis', label: 'Cost Basis', icon: Calculator },  // NEW
    { to: '/settings', label: 'Settings', icon: Settings },
  ]
  ```

### Tests

- [ ] Test: Frontend page renders, override dialog works, WAC displays correctly
- [ ] Test: Backend `handler/taxlot_test.go` — GetLots, OverrideCostBasis, GetWAC endpoints with ownership verification

### Acceptance Criteria
- [ ] `bun run test --run` and `bun run build` pass in `apps/frontend/`
- [ ] `go build ./...` and `go test ./...` pass in `apps/backend/`
- [ ] `GET /lots?account_id=&asset=` returns tax lots for authenticated user's account
- [ ] `PUT /lots/{id}/override` updates cost basis with reason
- [ ] `GET /positions/wac` returns WAC per asset
- [ ] Cost Basis page shows asset overview with WAC
- [ ] Clicking asset row expands to show individual lots
- [ ] Override dialog saves new cost basis and refreshes data
- [ ] "Cost Basis" nav item appears in sidebar after "Transactions"
- [ ] Lot ownership verified on all endpoints (user cannot access other users' lots)

### Notes
- The `LinkTransferDialog` component is deferred to a future phase. It requires additional backend logic to find matching transfer_out candidates.
- WAC data comes from the `position_wac` materialized view. The backend refreshes it lazily before serving the response.
- USD values use the same scaled integer format (x 10^8) as all other financial values in the app. Frontend formats them the same way.

---

## Phase 9: Frontend — PnL Page + Dashboard Enhancements
**Platform:** Both (Frontend + Backend)
**Goal:** Create the PnL reporting page, add PnL stat card to dashboard, and enhance WalletDetailPage with DeFi tab and WAC columns.
**Depends on:** Phase 5 (TaxLot disposal data for PnL), Phase 7 (DeFi page components for reuse), Phase 8 (WAC hooks)

### Changes

#### Backend

##### 1. PnL Endpoint

- [ ] File: `apps/backend/internal/transport/httpapi/handler/taxlot.go` — Add PnL method to existing TaxLotHandler:
  ```go
  // GetRealizedPnL handles GET /pnl/realized?account_id={id}&start_date={}&end_date={}
  func (h *TaxLotHandler) GetRealizedPnL(w http.ResponseWriter, r *http.Request)
  ```
  Logic: Query `lot_disposals` joined with `tax_lots_effective` for the user's accounts, compute PnL per disposal as `(proceeds_per_unit - effective_cost_basis_per_unit) * quantity_disposed`. Filter by date range. Return total and per-disposal breakdown.

  Response shape:
  ```json
  {
    "total_realized_pnl": "125000000000",
    "disposals": [
      {
        "id": "...",
        "transaction_id": "...",
        "asset": "ETH",
        "quantity_disposed": "1000000000000000000",
        "cost_basis_per_unit": "180000000000",
        "proceeds_per_unit": "250000000000",
        "realized_pnl": "70000000000",
        "disposal_type": "sale",
        "disposed_at": "2025-02-05T12:00:00Z"
      }
    ]
  }
  ```

##### 2. Router Update

- [ ] File: `apps/backend/internal/transport/httpapi/router.go` — Add PnL route (inside TaxLotHandler routes):
  ```go
  r.Get("/pnl/realized", cfg.TaxLotHandler.GetRealizedPnL)
  ```

##### 3. Portfolio Summary Enhancement

- [ ] File: `apps/backend/internal/module/portfolio/service.go` — Extend portfolio summary response to include `unrealized_pnl` field. Computed as: for each asset with WAC, `(current_price - wac) * holdings`. This requires joining portfolio data with position_wac view.

#### Frontend

##### 4. PnL Types

- [ ] File: `apps/frontend/src/types/pnl.ts` (NEW):
  ```typescript
  export interface RealizedPnLResponse {
    total_realized_pnl: string
    disposals: PnLDisposal[]
  }

  export interface PnLDisposal {
    id: string
    transaction_id: string
    asset: string
    quantity_disposed: string
    cost_basis_per_unit: string
    proceeds_per_unit: string
    realized_pnl: string
    disposal_type: 'sale' | 'internal_transfer'
    disposed_at: string
  }
  ```

##### 5. PnL Service

- [ ] File: `apps/frontend/src/services/pnl.ts` (NEW):
  ```typescript
  import api from './api'
  import type { RealizedPnLResponse } from '@/types/pnl'

  export const pnlService = {
    async getRealizedPnL(params?: {
      account_id?: string
      start_date?: string
      end_date?: string
    }): Promise<RealizedPnLResponse> {
      const response = await api.get<RealizedPnLResponse>('/pnl/realized', { params })
      return response.data
    },
  }
  ```

##### 6. PnL Hook

- [ ] File: `apps/frontend/src/hooks/usePnl.ts` (NEW):
  ```typescript
  export function useRealizedPnL(params?: { account_id?: string; start_date?: string; end_date?: string })
  ```

##### 7. DisposalHistoryTable Component

- [ ] File: `apps/frontend/src/components/domain/DisposalHistoryTable.tsx` (NEW) — Table showing realized PnL per disposal:
  - Columns: Date, Asset, Quantity, Cost Basis, Proceeds, PnL
  - PnL column uses `PnLValue` component (existing) for green/red coloring
  - Internal transfers show PnL as $0 with muted styling
  - Each row links to transaction detail page via `transaction_id`

##### 8. PnlSummaryCard Component

- [ ] File: `apps/frontend/src/components/domain/PnlSummaryCard.tsx` (NEW) — Compact card showing PnL summary:
  - Uses `Card` from shadcn/ui
  - Shows: Realized PnL (with color), number of trades
  - Uses existing `PnLValue` component for color coding

##### 9. PnlPage

- [ ] File: `apps/frontend/src/features/pnl/PnlPage.tsx` (NEW) — Main PnL page:
  - Page header with title "Profit & Loss" and date range filter
  - Summary cards row: Realized PnL (uses `PnlSummaryCard`)
  - Disposal history table (`DisposalHistoryTable`)
  - Empty state with TrendingUp icon: "No PnL data yet. PnL is calculated when you sell or swap assets."
  - Uses `useRealizedPnL()` hook

##### 10. Dashboard PnL Enhancement

- [ ] File: `apps/frontend/src/features/dashboard/DashboardPage.tsx` — Add PnL stat card to the stats grid (lines 59-84).

  Replace the 4th stat card (Transactions) or add a 5th card showing unrealized PnL:
  ```tsx
  <StatCard
    label="Unrealized PnL"
    value={formatUSD(unrealizedPnl)}
    icon={TrendingUp}
    iconColor={unrealizedPnl >= 0 ? 'profit' : 'loss'}
  />
  ```

  This requires the portfolio summary to include `unrealized_pnl` (from backend enhancement in this phase).

##### 11. WalletDetailPage DeFi Tab

- [ ] File: `apps/frontend/src/features/wallets/WalletDetailPage.tsx` — Add "DeFi" tab alongside existing "Assets" and "Transactions" tabs (lines 200-216):
  ```tsx
  import { useDefiPositions } from '@/hooks/useDefiPositions'
  import { DefiPositionCard } from '@/components/domain/DefiPositionCard'

  // Inside component, add hook:
  const { data: defiData, isLoading: defiLoading } = useDefiPositions(id)

  // Add tab:
  <TabsList>
    <TabsTrigger value="assets">Assets</TabsTrigger>
    <TabsTrigger value="defi">DeFi</TabsTrigger>
    <TabsTrigger value="transactions">Transactions</TabsTrigger>
  </TabsList>

  <TabsContent value="defi">
    {defiData?.positions?.length ? (
      <div className="grid gap-4">
        {defiData.positions.map((position) => (
          <DefiPositionCard key={position.id} position={position} />
        ))}
      </div>
    ) : (
      <p className="text-center text-muted-foreground py-8">No DeFi positions for this wallet</p>
    )}
  </TabsContent>
  ```

##### 12. Route and Navigation

- [ ] File: `apps/frontend/src/app/App.tsx` — Add PnL route:
  ```tsx
  import PnlPage from '@/features/pnl/PnlPage'

  <Route
    path="/pnl"
    element={
      <ProtectedRoute>
        <Layout>
          <PnlPage />
        </Layout>
      </ProtectedRoute>
    }
  />
  ```

- [ ] File: `apps/frontend/src/components/layout/Sidebar.tsx` — Add PnL nav item after Cost Basis:
  ```typescript
  import { ..., TrendingUp } from 'lucide-react'

  const navItems = [
    { to: '/dashboard', label: 'Dashboard', icon: LayoutDashboard },
    { to: '/wallets', label: 'Wallets', icon: Wallet },
    { to: '/defi-positions', label: 'DeFi', icon: Layers },
    { to: '/transactions', label: 'Transactions', icon: ArrowLeftRight },
    { to: '/cost-basis', label: 'Cost Basis', icon: Calculator },
    { to: '/pnl', label: 'PnL', icon: TrendingUp },  // NEW
    { to: '/settings', label: 'Settings', icon: Settings },
  ]
  ```

### Tests

- [ ] Test: Frontend PnL page renders, disposal history displays, PnL colors correct
- [ ] Test: Backend `GetRealizedPnL` returns correct PnL calculations
- [ ] Test: WalletDetailPage DeFi tab renders positions

### Acceptance Criteria
- [ ] `bun run test --run` and `bun run build` pass in `apps/frontend/`
- [ ] `go build ./...` and `go test ./...` pass in `apps/backend/`
- [ ] `GET /pnl/realized` returns realized PnL with disposal breakdown
- [ ] PnL page shows summary card and disposal history table
- [ ] Disposal PnL values are color-coded (green positive, red negative)
- [ ] Internal transfer disposals show $0 PnL
- [ ] Dashboard shows unrealized PnL stat card
- [ ] WalletDetailPage has "DeFi" tab showing positions for that wallet
- [ ] "PnL" nav item appears in sidebar after "Cost Basis"
- [ ] All new pages are protected (require authentication)

### Risk Mitigation
- **Unrealized PnL computation** -> Requires joining portfolio data with position_wac. If the WAC materialized view is stale, unrealized PnL may be slightly off. The backend should refresh WAC before serving the portfolio response.
- **PnL for internal transfers** -> Internal transfer disposals have `disposal_type = 'internal_transfer'`. The backend must return `realized_pnl = 0` for these regardless of cost basis differences. The frontend displays them with muted styling.

### Notes
- The `PnLValue` component already exists in `components/domain/` and handles green/red color coding.
- The dashboard PnL stat card uses `unrealized_pnl` from the portfolio summary. This requires the backend portfolio service to compute it (joining account balances with WAC and current prices).
- The WalletDetailPage DeFi tab reuses the `DefiPositionCard` component from Phase 7 and the `useDefiPositions` hook (passing the wallet ID as filter).
- Phase 9 is the final phase. After completion, all planned features from the frontend-ui-plan.md and backend-go-plan.md are implemented.
