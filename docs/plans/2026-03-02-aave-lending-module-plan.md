# AAVE Lending Module Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add lending module for AAVE with collateral/liability account types, cost basis preservation on supply/withdraw, and LendingPosition tracking.

**Architecture:** New account types (COLLATERAL, LIABILITY) with dedicated entry types. Supply/withdraw transfer tax lots like internal_transfer. Borrow creates wallet asset + liability. LendingPosition entity aggregates position state. Zerion classifier routes AAVE protocol to lending_* types.

**Tech Stack:** Go 1.24, PostgreSQL (NUMERIC(78,0)), Chi router, existing ledger/handler registry pattern.

---

### Task 1: DB Migration — New Account Types & Lending Positions Table

**Files:**
- Create: `apps/backend/migrations/000023_lending_positions.up.sql`
- Create: `apps/backend/migrations/000023_lending_positions.down.sql`

**Step 1: Write the UP migration**

```sql
-- Add COLLATERAL and LIABILITY to accounts type constraint
ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_type_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_type_check
    CHECK (type IN ('CRYPTO_WALLET', 'INCOME', 'EXPENSE', 'GAS_FEE', 'CLEARING', 'COLLATERAL', 'LIABILITY'));

-- Create lending_positions table
CREATE TABLE lending_positions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id),
    wallet_id       UUID NOT NULL REFERENCES wallets(id),
    chain_id        VARCHAR(50) NOT NULL,
    protocol        VARCHAR(100) NOT NULL,

    supply_asset         VARCHAR(50),
    supply_amount        NUMERIC(78,0) NOT NULL DEFAULT 0,
    supply_decimals      SMALLINT NOT NULL DEFAULT 18,
    supply_contract      VARCHAR(255),

    borrow_asset         VARCHAR(50),
    borrow_amount        NUMERIC(78,0) NOT NULL DEFAULT 0,
    borrow_decimals      SMALLINT NOT NULL DEFAULT 18,
    borrow_contract      VARCHAR(255),

    total_supplied       NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_withdrawn      NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_borrowed       NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_repaid         NUMERIC(78,0) NOT NULL DEFAULT 0,

    total_supplied_usd   NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_withdrawn_usd  NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_borrowed_usd   NUMERIC(78,0) NOT NULL DEFAULT 0,
    total_repaid_usd     NUMERIC(78,0) NOT NULL DEFAULT 0,

    interest_earned_usd  NUMERIC(78,0) NOT NULL DEFAULT 0,

    status          VARCHAR(20) NOT NULL DEFAULT 'active',
    opened_at       TIMESTAMPTZ NOT NULL,
    closed_at       TIMESTAMPTZ,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_lending_positions_user_id ON lending_positions(user_id);
CREATE INDEX idx_lending_positions_wallet_id ON lending_positions(wallet_id);
CREATE INDEX idx_lending_positions_status ON lending_positions(status);
CREATE UNIQUE INDEX idx_lending_positions_unique_active
    ON lending_positions(wallet_id, protocol, chain_id, supply_asset, borrow_asset)
    WHERE status = 'active';
```

**Step 2: Write the DOWN migration**

```sql
DROP TABLE IF EXISTS lending_positions;

ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_type_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_type_check
    CHECK (type IN ('CRYPTO_WALLET', 'INCOME', 'EXPENSE', 'GAS_FEE', 'CLEARING'));
```

**Step 3: Run migration**

Run: `cd apps/backend && just migrate-up` (or `just migrate-up` from root)
Expected: Migration applied successfully.

**Step 4: Verify migration**

Run: `cd apps/backend && just db-connect` then `\d lending_positions` and check `SELECT conname, consrc FROM pg_constraint WHERE conname = 'accounts_type_check';`
Expected: Table exists, constraint includes COLLATERAL and LIABILITY.

**Step 5: Commit**

```bash
git add apps/backend/migrations/000023_lending_positions.*
git commit -m "feat(lending): add migration for COLLATERAL/LIABILITY account types and lending_positions table"
```

---

### Task 2: Ledger Model — Add Account Types, Entry Types, Transaction Types

**Files:**
- Modify: `apps/backend/internal/ledger/model.go`

**Step 1: Add transaction types (after line 38)**

Add to the const block (lines 13-39):
```go
	// Lending
	TxTypeLendingSupply   TransactionType = "lending_supply"
	TxTypeLendingWithdraw TransactionType = "lending_withdraw"
	TxTypeLendingBorrow   TransactionType = "lending_borrow"
	TxTypeLendingRepay    TransactionType = "lending_repay"
	TxTypeLendingClaim    TransactionType = "lending_claim"
```

Add to `AllTransactionTypes()` (lines 42-59): append all 5 new types.

Add to `IsValid()` switch (lines 62-72): add 5 new cases.

Add to `Label()` switch (lines 80-113): add 5 new labels:
- `"Lending Supply"`, `"Lending Withdraw"`, `"Lending Borrow"`, `"Lending Repay"`, `"Lending Claim"`

**Step 2: Add entry types (after line 245)**

Add to the const block (lines 236-246):
```go
	EntryTypeCollateralIncrease EntryType = "collateral_increase"
	EntryTypeCollateralDecrease EntryType = "collateral_decrease"
	EntryTypeLiabilityIncrease  EntryType = "liability_increase"
	EntryTypeLiabilityDecrease  EntryType = "liability_decrease"
```

**Step 3: Add account types (after line 321)**

Add to the const block (lines 316-322):
```go
	AccountTypeCollateral AccountType = "COLLATERAL"
	AccountTypeLiability  AccountType = "LIABILITY"
```

Add helper methods:
```go
func (a *Account) IsCollateralAccount() bool { return a.Type == AccountTypeCollateral }
func (a *Account) IsLiabilityAccount() bool  { return a.Type == AccountTypeLiability }
```

**Step 4: Verify build**

Run: `cd apps/backend && go build ./...`
Expected: Build succeeds.

**Step 5: Commit**

```bash
git add apps/backend/internal/ledger/model.go
git commit -m "feat(ledger): add COLLATERAL/LIABILITY account types, lending entry/transaction types"
```

---

### Task 3: Ledger Service — Update Balance Logic & Account Resolution for New Types

**Files:**
- Modify: `apps/backend/internal/ledger/service.go`

**Step 1: Update `updateBalances()` to handle collateral and liability entry types**

At line 571, the current filter is:
```go
if entry.EntryType != EntryTypeAssetIncrease && entry.EntryType != EntryTypeAssetDecrease {
    continue
}
```

Replace with a helper function that determines if an entry type affects balances and its sign:
```go
func entryBalanceChange(entry *Entry) *big.Int {
	switch entry.EntryType {
	case EntryTypeAssetIncrease:
		return new(big.Int).Set(entry.Amount) // positive
	case EntryTypeAssetDecrease:
		return new(big.Int).Neg(new(big.Int).Set(entry.Amount)) // negative
	case EntryTypeCollateralIncrease:
		return new(big.Int).Set(entry.Amount) // positive
	case EntryTypeCollateralDecrease:
		return new(big.Int).Neg(new(big.Int).Set(entry.Amount)) // negative
	case EntryTypeLiabilityIncrease:
		return new(big.Int).Set(entry.Amount) // positive (debt grows)
	case EntryTypeLiabilityDecrease:
		return new(big.Int).Neg(new(big.Int).Set(entry.Amount)) // negative (debt shrinks)
	default:
		return nil // not a balance-affecting entry
	}
}
```

Update `updateBalances()` to use this function instead of `entry.SignedAmount()`.

**Step 2: Update `parseAccountCode()` to handle collateral. and liability. prefixes**

At line 343, add cases to the switch:
```go
case len(code) > 11 && code[:11] == "collateral.":
    accountType = AccountTypeCollateral
case len(code) > 10 && code[:10] == "liability.":
    accountType = AccountTypeLiability
```

**Step 3: Write test for entryBalanceChange**

Create test in existing ledger test file or inline:
```go
func TestEntryBalanceChange(t *testing.T) {
    amount := big.NewInt(1000)
    tests := []struct {
        entryType EntryType
        expected  *big.Int
    }{
        {EntryTypeAssetIncrease, big.NewInt(1000)},
        {EntryTypeAssetDecrease, big.NewInt(-1000)},
        {EntryTypeCollateralIncrease, big.NewInt(1000)},
        {EntryTypeCollateralDecrease, big.NewInt(-1000)},
        {EntryTypeLiabilityIncrease, big.NewInt(1000)},
        {EntryTypeLiabilityDecrease, big.NewInt(-1000)},
        {EntryTypeGasFee, nil},
        {EntryTypeClearing, nil},
        {EntryTypeIncome, nil},
    }
    for _, tt := range tests {
        t.Run(string(tt.entryType), func(t *testing.T) {
            entry := &Entry{Amount: new(big.Int).Set(amount), EntryType: tt.entryType}
            result := entryBalanceChange(entry)
            if tt.expected == nil {
                assert.Nil(t, result)
            } else {
                assert.Equal(t, tt.expected.String(), result.String())
            }
        })
    }
}
```

**Step 4: Run tests**

Run: `cd apps/backend && go test -v ./internal/ledger/... -run TestEntryBalanceChange`
Expected: All cases pass.

**Step 5: Verify build**

Run: `cd apps/backend && go build ./...`
Expected: Build succeeds.

**Step 6: Commit**

```bash
git add apps/backend/internal/ledger/service.go apps/backend/internal/ledger/service_test.go
git commit -m "feat(ledger): support collateral/liability balance updates and account resolution"
```

---

### Task 4: TaxLotHook — Support Lending Cost Basis Carry-Over

**Files:**
- Modify: `apps/backend/internal/ledger/taxlot_hook.go`
- Modify: `apps/backend/internal/ledger/taxlot_model.go`

**Step 1: Add new cost basis source in `taxlot_model.go`**

After line 17, add:
```go
CostBasisLendingCarryOver CostBasisSource = "lending_carry_over"
```

Add new disposal type after line 26:
```go
DisposalTypeLendingTransfer DisposalType = "lending_transfer"
```

**Step 2: Update TaxLotHook to handle COLLATERAL accounts**

In `taxlot_hook.go`, the filter at line 60 is:
```go
if acct.Type != AccountTypeCryptoWallet {
    continue
}
```

Change to:
```go
if acct.Type != AccountTypeCryptoWallet && acct.Type != AccountTypeCollateral {
    continue
}
```

This means tax lots are tracked for both wallet AND collateral accounts.

**Step 3: Update `classifyDisposalType()` for lending transactions**

Add cases in `classifyDisposalType()` (around line 175):
```go
case TxTypeLendingSupply, TxTypeLendingWithdraw:
    return DisposalTypeLendingTransfer
```

**Step 4: Update `classifyCostBasisSource()` for lending transactions**

Add cases in `classifyCostBasisSource()` (around line 191):
```go
case TxTypeLendingSupply, TxTypeLendingWithdraw:
    return CostBasisLendingCarryOver
```

**Step 5: Ensure lending supply/withdraw carry cost basis like internal transfers**

The existing `weightedAvgCostBasis()` logic (line 210) runs for `TxTypeInternalTransfer`. Extend to also run for `TxTypeLendingSupply` and `TxTypeLendingWithdraw`:

In the acquisition processing block (around lines 130-168), where `LinkedSourceLotID` is set for internal transfers:
```go
// Link source lot for cost basis carry-over
if tx.Type == TxTypeInternalTransfer || tx.Type == TxTypeLendingSupply || tx.Type == TxTypeLendingWithdraw {
    // existing linking logic
}
```

Similarly, where `weightedAvgCostBasis()` is called to override the FMV cost basis:
```go
if tx.Type == TxTypeInternalTransfer || tx.Type == TxTypeLendingSupply || tx.Type == TxTypeLendingWithdraw {
    // existing weighted avg logic
}
```

**Step 6: Ensure LIABILITY accounts are skipped by TaxLotHook**

No change needed — the filter at line 60 already excludes LIABILITY since we only added COLLATERAL to the allowlist.

**Step 7: Run existing tax lot tests**

Run: `cd apps/backend && go test -v ./internal/ledger/... -run TaxLot`
Expected: All existing tests pass.

**Step 8: Verify build**

Run: `cd apps/backend && go build ./...`
Expected: Build succeeds.

**Step 9: Commit**

```bash
git add apps/backend/internal/ledger/taxlot_hook.go apps/backend/internal/ledger/taxlot_model.go
git commit -m "feat(taxlot): support lending carry-over cost basis for supply/withdraw"
```

---

### Task 5: Lending Module — Model & Entry Generation

**Files:**
- Create: `apps/backend/internal/module/lending/model.go`
- Create: `apps/backend/internal/module/lending/entries.go`

**Step 1: Create the model**

File: `apps/backend/internal/module/lending/model.go`

```go
package lending

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"moontrack/internal/money"
)

// LendingTransaction represents an AAVE lending operation.
type LendingTransaction struct {
	WalletID    uuid.UUID     `json:"wallet_id"`
	TxHash      string        `json:"tx_hash"`
	ChainID     string        `json:"chain_id"`
	OccurredAt  time.Time     `json:"occurred_at"`
	Protocol    string        `json:"protocol,omitempty"`
	Asset       string        `json:"asset"`
	Amount      *money.BigInt `json:"amount"`
	Decimals    int           `json:"decimals"`
	USDPrice    *money.BigInt `json:"usd_price,omitempty"`
	ContractAddress string    `json:"contract_address,omitempty"`
	FeeAsset    string        `json:"fee_asset,omitempty"`
	FeeAmount   *money.BigInt `json:"fee_amount,omitempty"`
	FeeDecimals int           `json:"fee_decimals,omitempty"`
	FeeUSDPrice *money.BigInt `json:"fee_usd_price,omitempty"`
}

func (t *LendingTransaction) Validate() error {
	if t.WalletID == uuid.Nil {
		return fmt.Errorf("wallet_id is required")
	}
	if t.TxHash == "" {
		return fmt.Errorf("tx_hash is required")
	}
	if t.ChainID == "" {
		return fmt.Errorf("chain_id is required")
	}
	if t.Asset == "" {
		return fmt.Errorf("asset is required")
	}
	if t.Amount == nil || t.Amount.IsZero() {
		return fmt.Errorf("amount is required and must be positive")
	}
	if t.Decimals <= 0 {
		return fmt.Errorf("decimals must be positive")
	}
	return nil
}
```

**Step 2: Create the entry generation**

File: `apps/backend/internal/module/lending/entries.go`

Five entry generation functions following the design doc entry patterns:

- `generateSupplyEntries(txn *LendingTransaction) ([]*ledger.Entry, error)` — wallet→collateral
- `generateWithdrawEntries(txn *LendingTransaction) ([]*ledger.Entry, error)` — collateral→wallet
- `generateBorrowEntries(txn *LendingTransaction) ([]*ledger.Entry, error)` — liability→wallet
- `generateRepayEntries(txn *LendingTransaction) ([]*ledger.Entry, error)` — wallet→liability
- `generateClaimEntries(txn *LendingTransaction) ([]*ledger.Entry, error)` — income→wallet
- `generateGasFeeEntries(txn *LendingTransaction) ([]*ledger.Entry, error)` — gas fee (reuse defi pattern)

Each function follows the exact entry patterns from the design doc. Use `fmt.Sprintf` for account codes:
- Collateral: `collateral.%s.%s.%s.%s` (protocol, walletID, chainID, asset)
- Liability: `liability.%s.%s.%s.%s` (protocol, walletID, chainID, asset)
- Wallet: `wallet.%s.%s.%s` (walletID, chainID, asset)
- Gas: `gas.%s.%s` (chainID, feeAsset)
- Income: `income.lending.%s.%s` (chainID, asset)

USD calculation: `usdValue = (amount * usdRate) / 10^decimals` using `money.CalcUSDValue()`.

**Step 3: Write tests for entry generation**

Create `apps/backend/internal/module/lending/entries_test.go`:
- `TestGenerateSupplyEntries` — 2 entries (collateral_increase DEBIT, asset_decrease CREDIT), balanced
- `TestGenerateWithdrawEntries` — 2 entries (asset_increase DEBIT, collateral_decrease CREDIT), balanced
- `TestGenerateBorrowEntries` — 2 entries (asset_increase DEBIT, liability_increase CREDIT), balanced
- `TestGenerateRepayEntries` — 2 entries (liability_decrease DEBIT, asset_decrease CREDIT), balanced
- `TestGenerateClaimEntries` — 2 entries (asset_increase DEBIT, income CREDIT), balanced
- `TestGenerateGasFeeEntries` — 2 entries (gas_fee DEBIT, asset_decrease CREDIT), balanced

Each test: create LendingTransaction, generate entries, assert count, assert balanced (sum debits = sum credits), assert entry types, assert account codes in metadata.

Use `assertEntriesBalanced` helper (same pattern as `defi/handler_test.go:39-52`).

**Step 4: Run tests**

Run: `cd apps/backend && go test -v ./internal/module/lending/...`
Expected: All pass.

**Step 5: Commit**

```bash
git add apps/backend/internal/module/lending/
git commit -m "feat(lending): add model and entry generation for all 5 lending operations"
```

---

### Task 6: Lending Module — 5 Handlers

**Files:**
- Create: `apps/backend/internal/module/lending/handler_supply.go`
- Create: `apps/backend/internal/module/lending/handler_withdraw.go`
- Create: `apps/backend/internal/module/lending/handler_borrow.go`
- Create: `apps/backend/internal/module/lending/handler_repay.go`
- Create: `apps/backend/internal/module/lending/handler_claim.go`

Each handler follows the exact pattern from `defi/handler_deposit.go`:

```go
type LendingSupplyHandler struct {
	ledger.BaseHandler
	walletRepo WalletRepository
	logger     *logger.Logger
}

func NewLendingSupplyHandler(walletRepo WalletRepository, log *logger.Logger) *LendingSupplyHandler {
	return &LendingSupplyHandler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeLendingSupply),
		walletRepo:  walletRepo,
		logger:      log.WithField("component", "lending_supply"),
	}
}
```

Each handler has: `Handle()`, `ValidateData()`, `GenerateEntries()`.

**WalletRepository interface** — define once at package level (same as defi):
```go
type WalletRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*wallet.Wallet, error)
}
```

**Step 1: Create all 5 handlers**

Each handler:
1. Unmarshals `map[string]interface{}` into `LendingTransaction`
2. Validates (calls `txn.Validate()` + wallet ownership check)
3. Generates entries via the appropriate function from `entries.go`
4. Appends gas fee entries if present

**Step 2: Write handler tests**

Create `apps/backend/internal/module/lending/handler_test.go`:
- Mock WalletRepository (same pattern as `defi/handler_test.go`)
- Test each handler's `Handle()` method with valid data → entries balanced
- Test `ValidateData()` with missing fields → errors
- Test wallet authorization → ErrUnauthorized

**Step 3: Run tests**

Run: `cd apps/backend && go test -v ./internal/module/lending/...`
Expected: All pass.

**Step 4: Verify build**

Run: `cd apps/backend && go build ./...`
Expected: Build succeeds.

**Step 5: Commit**

```bash
git add apps/backend/internal/module/lending/
git commit -m "feat(lending): add supply/withdraw/borrow/repay/claim handlers"
```

---

### Task 7: LendingPosition — Model, Repository, Service

**Files:**
- Create: `apps/backend/internal/platform/lendingposition/model.go`
- Create: `apps/backend/internal/platform/lendingposition/repository.go`
- Create: `apps/backend/internal/platform/lendingposition/service.go`
- Create: `apps/backend/internal/platform/lendingposition/service_test.go`
- Create: `apps/backend/internal/infra/postgres/lending_position_repo.go`

**Step 1: Create model**

Follow LP position pattern (`lpposition/model.go`):

```go
package lendingposition

type Status string

const (
	StatusActive Status = "active"
	StatusClosed Status = "closed"
)

type LendingPosition struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	WalletID  uuid.UUID
	ChainID   string
	Protocol  string

	SupplyAsset    string
	SupplyAmount   *big.Int // current supply balance
	SupplyDecimals int
	SupplyContract string

	BorrowAsset    string
	BorrowAmount   *big.Int // current borrow balance
	BorrowDecimals int
	BorrowContract string

	TotalSupplied    *big.Int
	TotalWithdrawn   *big.Int
	TotalBorrowed    *big.Int
	TotalRepaid      *big.Int

	TotalSuppliedUSD  *big.Int
	TotalWithdrawnUSD *big.Int
	TotalBorrowedUSD  *big.Int
	TotalRepaidUSD    *big.Int

	InterestEarnedUSD *big.Int

	Status    Status
	OpenedAt  time.Time
	ClosedAt  *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}
```

**Step 2: Create repository interface**

```go
type Repository interface {
	Create(ctx context.Context, pos *LendingPosition) error
	Update(ctx context.Context, pos *LendingPosition) error
	GetByID(ctx context.Context, id uuid.UUID) (*LendingPosition, error)
	FindActiveByWalletAndAsset(ctx context.Context, walletID uuid.UUID, protocol, chainID, supplyAsset, borrowAsset string) (*LendingPosition, error)
	ListByUser(ctx context.Context, userID uuid.UUID, status *Status, walletID *uuid.UUID, chainID *string) ([]*LendingPosition, error)
}
```

**Step 3: Create service**

Methods following LP position pattern:
- `FindOrCreate(ctx, userID, walletID, protocol, chainID, supplyAsset, supplyDecimals, supplyContract string) (*LendingPosition, error)`
- `RecordSupply(ctx, positionID uuid.UUID, amount, usdValue *big.Int) error` — adds to TotalSupplied, SupplyAmount
- `RecordWithdraw(ctx, positionID uuid.UUID, amount, usdValue *big.Int) error` — adds to TotalWithdrawn, subtracts from SupplyAmount, may close
- `RecordBorrow(ctx, positionID uuid.UUID, borrowAsset string, borrowDecimals int, borrowContract string, amount, usdValue *big.Int) error` — adds to TotalBorrowed, BorrowAmount
- `RecordRepay(ctx, positionID uuid.UUID, amount, usdValue *big.Int) error` — adds to TotalRepaid, subtracts from BorrowAmount, may close
- `RecordClaim(ctx, positionID uuid.UUID, usdValue *big.Int) error` — adds to InterestEarnedUSD
- `GetByID(ctx, id uuid.UUID) (*LendingPosition, error)`
- `ListByUser(ctx, userID uuid.UUID, status *Status, walletID *uuid.UUID, chainID *string) ([]*LendingPosition, error)`

Position closes when `SupplyAmount <= 0 AND BorrowAmount <= 0`.

**Step 4: Write service tests**

Follow `lpposition/service_test.go` pattern:
- In-memory mock repository
- `TestRecordSupply_UpdatesAggregates`
- `TestRecordWithdraw_ClosesWhenFullyWithdrawn`
- `TestRecordBorrow_SetsBorrowAsset`
- `TestRecordRepay_ReducesDebt`
- `TestRecordClaim_AddsInterest`
- `TestFindOrCreate_ReusesExisting`

**Step 5: Create postgres repository**

Follow `infra/postgres/lp_position_repo.go` pattern. SQL CRUD operations.

**Step 6: Run tests**

Run: `cd apps/backend && go test -v ./internal/platform/lendingposition/...`
Expected: All pass.

**Step 7: Verify build**

Run: `cd apps/backend && go build ./...`
Expected: Build succeeds.

**Step 8: Commit**

```bash
git add apps/backend/internal/platform/lendingposition/ apps/backend/internal/infra/postgres/lending_position_repo.go
git commit -m "feat(lending): add LendingPosition model, service, and postgres repository"
```

---

### Task 8: Zerion Classifier & Processor — Route AAVE to Lending Types

**Files:**
- Modify: `apps/backend/internal/platform/sync/classifier.go`
- Modify: `apps/backend/internal/platform/sync/zerion_processor.go`

**Step 1: Update classifier to detect AAVE protocol**

In `classifier.go`, add AAVE-specific classification (similar to Uniswap V3 at line 21):

```go
func (c *Classifier) Classify(tx *DecodedTransaction) ledger.TransactionType {
    // ... existing code ...

    // AAVE lending protocol
    if c.isAAVE(tx.Protocol) {
        if lt := c.classifyLending(tx); lt != "" {
            return lt
        }
    }

    // ... existing switch on OperationType ...
}

func (c *Classifier) isAAVE(protocol string) bool {
    return protocol == "AAVE" || protocol == "Aave" || protocol == "Aave V3" || protocol == "Aave V2"
}

func (c *Classifier) classifyLending(tx *DecodedTransaction) ledger.TransactionType {
    switch tx.OperationType {
    case OpDeposit, OpMint:
        return ledger.TxTypeLendingSupply
    case OpWithdraw, OpBurn:
        return ledger.TxTypeLendingWithdraw
    case OpClaim:
        return ledger.TxTypeLendingClaim
    case OpReceive:
        // Borrow looks like "receive" from AAVE
        if c.hasClaimAct(tx) {
            return ledger.TxTypeLendingClaim
        }
        return ledger.TxTypeLendingBorrow
    case OpSend:
        // Repay looks like "send" to AAVE
        return ledger.TxTypeLendingRepay
    default:
        return c.classifyLendingFromTransfers(tx)
    }
}

func (c *Classifier) classifyLendingFromTransfers(tx *DecodedTransaction) ledger.TransactionType {
    hasIn := false
    hasOut := false
    for _, t := range tx.Transfers {
        if t.Direction == DirectionIn {
            hasIn = true
        }
        if t.Direction == DirectionOut {
            hasOut = true
        }
    }
    if hasIn && !hasOut {
        return ledger.TxTypeLendingBorrow
    }
    if hasOut && !hasIn {
        return ledger.TxTypeLendingRepay
    }
    if hasIn && hasOut {
        // Could be supply (send asset, receive aToken) or withdraw (send aToken, receive asset)
        // For now, treat as supply (most common for deposit+mint combo)
        return ledger.TxTypeLendingSupply
    }
    return ""
}
```

**Step 2: Add builder functions in zerion_processor.go**

```go
func (p *ZerionProcessor) buildLendingSupplyData(w *wallet.Wallet, tx *DecodedTransaction) map[string]interface{} {
    data := buildBaseData(w, tx)
    // Find the outgoing transfer (the asset being supplied)
    for _, t := range tx.Transfers {
        if t.Direction == DirectionOut {
            data["asset"] = t.AssetSymbol
            data["amount"] = t.Amount.String()
            data["decimals"] = t.Decimals
            data["contract_address"] = t.ContractAddress
            if t.USDPrice != nil {
                data["usd_price"] = t.USDPrice.String()
            }
            break
        }
    }
    return data
}
```

Similarly for `buildLendingWithdrawData` (look for IN transfer), `buildLendingBorrowData` (look for IN transfer), `buildLendingRepayData` (look for OUT transfer), `buildLendingClaimData` (look for IN transfer).

**Step 3: Update ProcessTransaction switch to handle lending types**

In `zerion_processor.go` around lines 68-92, add cases:
```go
case ledger.TxTypeLendingSupply:
    data = p.buildLendingSupplyData(w, tx)
case ledger.TxTypeLendingWithdraw:
    data = p.buildLendingWithdrawData(w, tx)
case ledger.TxTypeLendingBorrow:
    data = p.buildLendingBorrowData(w, tx)
case ledger.TxTypeLendingRepay:
    data = p.buildLendingRepayData(w, tx)
case ledger.TxTypeLendingClaim:
    data = p.buildLendingClaimData(w, tx)
```

**Step 4: Add post-processing for lending positions**

After ledger recording (around line 106), add lending position handling:
```go
if p.lendingPositionSvc != nil {
    switch txType {
    case ledger.TxTypeLendingSupply:
        p.handleLendingSupply(ctx, w, tx)
    case ledger.TxTypeLendingWithdraw:
        p.handleLendingWithdraw(ctx, w, tx)
    case ledger.TxTypeLendingBorrow:
        p.handleLendingBorrow(ctx, w, tx)
    case ledger.TxTypeLendingRepay:
        p.handleLendingRepay(ctx, w, tx)
    case ledger.TxTypeLendingClaim:
        p.handleLendingClaim(ctx, w, tx)
    }
}
```

**Step 5: Implement lending post-process handlers**

Follow LP position pattern (`handleLPDeposit` etc.):
- `handleLendingSupply`: FindOrCreate position, RecordSupply
- `handleLendingWithdraw`: Find position, RecordWithdraw
- `handleLendingBorrow`: Find position, RecordBorrow (sets borrow asset)
- `handleLendingRepay`: Find position, RecordRepay
- `handleLendingClaim`: Find position, RecordClaim

**Step 6: Add `lendingPositionSvc` to ZerionProcessor struct**

Add field `lendingPositionSvc LendingPositionService` to struct and constructor.

Define interface:
```go
type LendingPositionService interface {
    FindOrCreate(ctx context.Context, ...) (*lendingposition.LendingPosition, error)
    RecordSupply(ctx context.Context, ...) error
    RecordWithdraw(ctx context.Context, ...) error
    RecordBorrow(ctx context.Context, ...) error
    RecordRepay(ctx context.Context, ...) error
    RecordClaim(ctx context.Context, ...) error
}
```

**Step 7: Run existing classifier tests**

Run: `cd apps/backend && go test -v ./internal/platform/sync/...`
Expected: All existing tests pass.

**Step 8: Verify build**

Run: `cd apps/backend && go build ./...`
Expected: Build succeeds.

**Step 9: Commit**

```bash
git add apps/backend/internal/platform/sync/
git commit -m "feat(sync): classify AAVE transactions as lending types and post-process positions"
```

---

### Task 9: DI Wiring — Register Handlers & Services in main.go

**Files:**
- Modify: `apps/backend/cmd/api/main.go`

**Step 1: Create and register lending handlers (after LP handlers, around line 179)**

```go
// Lending handlers
lendingSupplyHandler := lending.NewLendingSupplyHandler(walletRepo, log)
handlerRegistry.Register(lendingSupplyHandler)

lendingWithdrawHandler := lending.NewLendingWithdrawHandler(walletRepo, log)
handlerRegistry.Register(lendingWithdrawHandler)

lendingBorrowHandler := lending.NewLendingBorrowHandler(walletRepo, log)
handlerRegistry.Register(lendingBorrowHandler)

lendingRepayHandler := lending.NewLendingRepayHandler(walletRepo, log)
handlerRegistry.Register(lendingRepayHandler)

lendingClaimHandler := lending.NewLendingClaimHandler(walletRepo, log)
handlerRegistry.Register(lendingClaimHandler)
```

**Step 2: Create LendingPosition service (after LP position service, around line 184)**

```go
lendingPositionRepo := postgres.NewLendingPositionRepo(db.Pool)
lendingPositionSvc := lendingposition.NewService(lendingPositionRepo, log)
```

**Step 3: Pass lendingPositionSvc to sync service (around line 221)**

Add `lendingPositionSvc` parameter to `sync.NewService()`.

**Step 4: Create HTTP handler and add to router config**

```go
lendingPositionHTTPHandler := handler.NewLendingPositionHandler(lendingPositionSvc)
```

Add to `httpapi.Config`:
```go
LendingPositionHandler: lendingPositionHTTPHandler,
```

**Step 5: Verify build**

Run: `cd apps/backend && go build ./...`
Expected: Build succeeds.

**Step 6: Commit**

```bash
git add apps/backend/cmd/api/main.go
git commit -m "feat(lending): wire lending handlers, position service, and HTTP handler in DI"
```

---

### Task 10: HTTP Handler & Routes — Lending Positions API

**Files:**
- Create: `apps/backend/internal/transport/httpapi/handler/lending_position.go`
- Modify: `apps/backend/internal/transport/httpapi/router.go`

**Step 1: Create HTTP handler**

Follow LP position handler pattern (`handler/lp_position.go`):

```go
type LendingPositionHandler struct {
    svc LendingPositionServiceInterface
}

type LendingPositionServiceInterface interface {
    GetByID(ctx context.Context, id uuid.UUID) (*lendingposition.LendingPosition, error)
    ListByUser(ctx context.Context, userID uuid.UUID, status *lendingposition.Status, walletID *uuid.UUID, chainID *string) ([]*lendingposition.LendingPosition, error)
}
```

Methods:
- `ListPositions(w, r)` — `GET /lending/positions` with optional `status`, `wallet_id`, `chain_id` query params
- `GetPosition(w, r)` — `GET /lending/positions/{id}`

Response struct `LendingPositionResponse` with all fields as strings (big.Int → `.String()`).

**Step 2: Add routes in router.go**

Add to Config struct:
```go
LendingPositionHandler *handler.LendingPositionHandler
```

Add routes (after LP position routes, around line 103):
```go
if cfg.LendingPositionHandler != nil {
    r.Get("/lending/positions", cfg.LendingPositionHandler.ListPositions)
    r.Get("/lending/positions/{id}", cfg.LendingPositionHandler.GetPosition)
}
```

**Step 3: Verify build**

Run: `cd apps/backend && go build ./...`
Expected: Build succeeds.

**Step 4: Commit**

```bash
git add apps/backend/internal/transport/httpapi/handler/lending_position.go apps/backend/internal/transport/httpapi/router.go
git commit -m "feat(lending): add HTTP endpoints for lending positions"
```

---

### Task 11: Integration Test — End-to-End Lending Flow

**Files:**
- Create: `apps/backend/internal/module/lending/integration_test.go` (or add to existing test file)

**Step 1: Write integration test for full lending cycle**

Test: Supply ETH → Borrow USDC → Repay USDC → Withdraw ETH → Claim rewards

Each step:
1. Build data map (same format as Zerion processor builds)
2. Call `ledgerSvc.RecordTransaction()`
3. Verify entries are balanced
4. Verify account balances are correct
5. Verify tax lots are created/disposed correctly

Focus assertions:
- After supply: collateral account has balance, wallet decreased, tax lots transferred
- After borrow: wallet has USDC, liability account has balance
- After repay: liability decreased, wallet USDC decreased
- After withdraw: wallet has ETH back, collateral is zero
- After claim: wallet has reward, income account credited

**Step 2: Run integration test**

Run: `cd apps/backend && go test -v ./internal/module/lending/... -run TestIntegration`
Expected: Full cycle passes.

**Step 3: Run all tests**

Run: `cd apps/backend && go test ./... -v -short`
Expected: All tests pass, including existing ones.

**Step 4: Commit**

```bash
git add apps/backend/internal/module/lending/
git commit -m "test(lending): add integration test for full lending cycle"
```

---

### Task 12: Final Verification & Cleanup

**Step 1: Run linter**

Run: `cd apps/backend && golangci-lint run ./...`
Expected: No new lint issues.

**Step 2: Run full test suite**

Run: `just backend-test`
Expected: All tests pass.

**Step 3: Verify build**

Run: `cd apps/backend && go build ./...`
Expected: Build succeeds.

**Step 4: Final commit if any cleanup needed**

```bash
git add -A && git commit -m "chore(lending): cleanup and lint fixes"
```
