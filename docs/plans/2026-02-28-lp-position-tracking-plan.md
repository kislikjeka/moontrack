# LP Position Tracking (Uniswap V3) — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Track Uniswap V3 LP positions with full accounting (deposit, withdraw, claim fees), PnL/APR calculation, and API endpoints.

**Architecture:** New `module/liquidity` handlers reuse existing DeFi entry patterns. New `platform/lpposition` service manages LP position lifecycle. Sync pipeline classifier extended for Uniswap V3 protocol detection.

**Tech Stack:** Go 1.24, PostgreSQL (NUMERIC(78,0)), Chi router, existing ledger/TaxLotHook infrastructure.

**Design doc:** `docs/plans/2026-02-28-lp-position-tracking-design.md`

---

### Task 1: Add LP Transaction Type Constants

**Files:**
- Modify: `apps/backend/internal/ledger/model.go`

**Step 1: Add 3 new transaction type constants**

After the existing DeFi constants block (after `TxTypeDefiClaim`), add:

```go
// LP (Liquidity Pool) transaction types
TxTypeLPDeposit   TransactionType = "lp_deposit"    // Add liquidity to LP
TxTypeLPWithdraw  TransactionType = "lp_withdraw"   // Remove liquidity from LP
TxTypeLPClaimFees TransactionType = "lp_claim_fees" // Collect LP trading fees
```

**Step 2: Update `AllTransactionTypes()` — add the 3 new types to the slice**

**Step 3: Update `IsValid()` — add the 3 new types to the switch**

**Step 4: Update `Label()` — add labels:**
- `TxTypeLPDeposit` → `"LP Deposit"`
- `TxTypeLPWithdraw` → `"LP Withdraw"`
- `TxTypeLPClaimFees` → `"LP Claim Fees"`

**Step 5: Verify build**

Run: `cd apps/backend && go build ./...`

**Step 6: Commit**

```
feat(ledger): add LP transaction type constants
```

---

### Task 2: Create LP Position Domain Model and Repository Interface

**Files:**
- Create: `apps/backend/internal/platform/lpposition/model.go`
- Create: `apps/backend/internal/platform/lpposition/port.go`

**Step 1: Create model.go**

```go
package lpposition

import (
	"math/big"
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusOpen   Status = "open"
	StatusClosed Status = "closed"
)

type LPPosition struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	WalletID        uuid.UUID
	ChainID         string
	Protocol        string
	NFTTokenID      string // Uniswap V3 position NFT ID, empty if unknown
	ContractAddress string

	Token0Symbol   string
	Token1Symbol   string
	Token0Contract string
	Token1Contract string
	Token0Decimals int
	Token1Decimals int

	TotalDepositedUSD   *big.Int
	TotalWithdrawnUSD   *big.Int
	TotalClaimedFeesUSD *big.Int

	TotalDepositedToken0  *big.Int
	TotalDepositedToken1  *big.Int
	TotalWithdrawnToken0  *big.Int
	TotalWithdrawnToken1  *big.Int
	TotalClaimedToken0    *big.Int
	TotalClaimedToken1    *big.Int

	Status    Status
	OpenedAt  time.Time
	ClosedAt  *time.Time

	RealizedPnLUSD *big.Int
	APRBps         *int // basis points, nil if not calculated

	CreatedAt time.Time
	UpdatedAt time.Time
}

// RemainingToken0 returns deposited - withdrawn for token0.
// May be negative due to impermanent loss.
func (p *LPPosition) RemainingToken0() *big.Int {
	return new(big.Int).Sub(p.TotalDepositedToken0, p.TotalWithdrawnToken0)
}

// RemainingToken1 returns deposited - withdrawn for token1.
func (p *LPPosition) RemainingToken1() *big.Int {
	return new(big.Int).Sub(p.TotalDepositedToken1, p.TotalWithdrawnToken1)
}

// IsFullyWithdrawn returns true if both token remainders are <= 0.
func (p *LPPosition) IsFullyWithdrawn() bool {
	return p.RemainingToken0().Sign() <= 0 && p.RemainingToken1().Sign() <= 0
}
```

**Step 2: Create port.go**

```go
package lpposition

import (
	"context"

	"github.com/google/uuid"
)

type Repository interface {
	Create(ctx context.Context, pos *LPPosition) error
	Update(ctx context.Context, pos *LPPosition) error
	GetByID(ctx context.Context, id uuid.UUID) (*LPPosition, error)
	GetByNFTTokenID(ctx context.Context, walletID uuid.UUID, chainID, nftTokenID string) (*LPPosition, error)
	FindOpenByTokenPair(ctx context.Context, walletID uuid.UUID, chainID, protocol, token0, token1 string) ([]*LPPosition, error)
	ListByUser(ctx context.Context, userID uuid.UUID, status *Status, walletID *uuid.UUID, chainID *string) ([]*LPPosition, error)
}
```

**Step 3: Verify build**

Run: `cd apps/backend && go build ./...`

**Step 4: Commit**

```
feat(lpposition): add domain model and repository interface
```

---

### Task 3: Create LP Position Service

**Files:**
- Create: `apps/backend/internal/platform/lpposition/service.go`
- Create: `apps/backend/internal/platform/lpposition/service_test.go`

**Step 1: Write failing test for RecordDeposit**

In `service_test.go`, create a mock repo and test that `RecordDeposit` updates aggregates correctly. Test the multi-deposit/withdraw scenario: deposit $100 → withdraw $30 → deposit $50 → withdraw all.

**Step 2: Implement service.go**

```go
package lpposition

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/pkg/logger"
)

type Service struct {
	repo   Repository
	logger *logger.Logger
}

func NewService(repo Repository, log *logger.Logger) *Service {
	return &Service{
		repo:   repo,
		logger: log.WithField("component", "lpposition"),
	}
}

type TokenInfo struct {
	Symbol   string
	Contract string
	Decimals int
}

// FindOrCreate looks up an LP position by NFT token ID, or creates a new one.
func (s *Service) FindOrCreate(ctx context.Context, userID, walletID uuid.UUID, chainID, protocol, nftTokenID, contractAddress string, token0, token1 TokenInfo, openedAt time.Time) (*LPPosition, error) {
	if nftTokenID != "" {
		pos, err := s.repo.GetByNFTTokenID(ctx, walletID, chainID, nftTokenID)
		if err != nil {
			return nil, fmt.Errorf("find by nft: %w", err)
		}
		if pos != nil {
			return pos, nil
		}
	}

	pos := &LPPosition{
		ID:              uuid.New(),
		UserID:          userID,
		WalletID:        walletID,
		ChainID:         chainID,
		Protocol:        protocol,
		NFTTokenID:      nftTokenID,
		ContractAddress: contractAddress,

		Token0Symbol:   token0.Symbol,
		Token1Symbol:   token1.Symbol,
		Token0Contract: token0.Contract,
		Token1Decimals: token1.Decimals,
		Token0Decimals: token0.Decimals,
		Token1Contract: token1.Contract,

		TotalDepositedUSD:    big.NewInt(0),
		TotalWithdrawnUSD:    big.NewInt(0),
		TotalClaimedFeesUSD:  big.NewInt(0),
		TotalDepositedToken0: big.NewInt(0),
		TotalDepositedToken1: big.NewInt(0),
		TotalWithdrawnToken0: big.NewInt(0),
		TotalWithdrawnToken1: big.NewInt(0),
		TotalClaimedToken0:   big.NewInt(0),
		TotalClaimedToken1:   big.NewInt(0),

		Status:   StatusOpen,
		OpenedAt: openedAt,

		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	if err := s.repo.Create(ctx, pos); err != nil {
		return nil, fmt.Errorf("create position: %w", err)
	}

	s.logger.Info("LP position created",
		"position_id", pos.ID,
		"nft_token_id", nftTokenID,
		"token0", token0.Symbol,
		"token1", token1.Symbol,
	)

	return pos, nil
}

// FindOpenByTokenPair finds an open LP position by token pair (heuristic for withdraw/claim).
// Returns the oldest open position if multiple found.
func (s *Service) FindOpenByTokenPair(ctx context.Context, walletID uuid.UUID, chainID, protocol, token0, token1 string) (*LPPosition, error) {
	positions, err := s.repo.FindOpenByTokenPair(ctx, walletID, chainID, protocol, token0, token1)
	if err != nil {
		return nil, fmt.Errorf("find by token pair: %w", err)
	}

	if len(positions) == 0 {
		return nil, nil
	}

	if len(positions) > 1 {
		s.logger.Warn("multiple open LP positions for same token pair, using oldest",
			"wallet_id", walletID,
			"chain_id", chainID,
			"token0", token0,
			"token1", token1,
			"count", len(positions),
		)
	}

	return positions[0], nil // oldest first (repo sorts by opened_at ASC)
}

// RecordDeposit updates aggregates after a deposit.
func (s *Service) RecordDeposit(ctx context.Context, positionID uuid.UUID, token0Amt, token1Amt, usdValue *big.Int) error {
	pos, err := s.repo.GetByID(ctx, positionID)
	if err != nil {
		return fmt.Errorf("get position: %w", err)
	}
	if pos == nil {
		return fmt.Errorf("position not found: %s", positionID)
	}

	pos.TotalDepositedUSD.Add(pos.TotalDepositedUSD, usdValue)
	pos.TotalDepositedToken0.Add(pos.TotalDepositedToken0, token0Amt)
	pos.TotalDepositedToken1.Add(pos.TotalDepositedToken1, token1Amt)
	pos.UpdatedAt = time.Now().UTC()

	return s.repo.Update(ctx, pos)
}

// RecordWithdraw updates aggregates after a withdrawal. Closes position if fully withdrawn.
func (s *Service) RecordWithdraw(ctx context.Context, positionID uuid.UUID, token0Amt, token1Amt, usdValue *big.Int) error {
	pos, err := s.repo.GetByID(ctx, positionID)
	if err != nil {
		return fmt.Errorf("get position: %w", err)
	}
	if pos == nil {
		return fmt.Errorf("position not found: %s", positionID)
	}

	pos.TotalWithdrawnUSD.Add(pos.TotalWithdrawnUSD, usdValue)
	pos.TotalWithdrawnToken0.Add(pos.TotalWithdrawnToken0, token0Amt)
	pos.TotalWithdrawnToken1.Add(pos.TotalWithdrawnToken1, token1Amt)
	pos.UpdatedAt = time.Now().UTC()

	if pos.IsFullyWithdrawn() {
		s.closePosition(pos)
	}

	return s.repo.Update(ctx, pos)
}

// RecordClaimFees updates aggregates after a fee claim.
func (s *Service) RecordClaimFees(ctx context.Context, positionID uuid.UUID, token0Amt, token1Amt, usdValue *big.Int) error {
	pos, err := s.repo.GetByID(ctx, positionID)
	if err != nil {
		return fmt.Errorf("get position: %w", err)
	}
	if pos == nil {
		return fmt.Errorf("position not found: %s", positionID)
	}

	pos.TotalClaimedFeesUSD.Add(pos.TotalClaimedFeesUSD, usdValue)
	pos.TotalClaimedToken0.Add(pos.TotalClaimedToken0, token0Amt)
	pos.TotalClaimedToken1.Add(pos.TotalClaimedToken1, token1Amt)
	pos.UpdatedAt = time.Now().UTC()

	return s.repo.Update(ctx, pos)
}

func (s *Service) closePosition(pos *LPPosition) {
	now := time.Now().UTC()
	pos.Status = StatusClosed
	pos.ClosedAt = &now

	// realized PnL = withdrawn + claimed - deposited
	pnl := new(big.Int).Add(pos.TotalWithdrawnUSD, pos.TotalClaimedFeesUSD)
	pnl.Sub(pnl, pos.TotalDepositedUSD)
	pos.RealizedPnLUSD = pnl

	// APR in basis points = (pnl / deposited) / years * 10000
	if pos.TotalDepositedUSD.Sign() > 0 && pos.ClosedAt != nil {
		duration := pos.ClosedAt.Sub(pos.OpenedAt)
		if duration > 0 {
			// apr = pnl * 10000 * seconds_per_year / (deposited * duration_seconds)
			secondsPerYear := big.NewInt(365 * 24 * 3600)
			numerator := new(big.Int).Mul(pnl, big.NewInt(10000))
			numerator.Mul(numerator, secondsPerYear)
			denominator := new(big.Int).Mul(pos.TotalDepositedUSD, big.NewInt(int64(duration.Seconds())))
			if denominator.Sign() > 0 {
				apr := new(big.Int).Div(numerator, denominator)
				aprInt := int(apr.Int64())
				pos.APRBps = &aprInt
			}
		}
	}

	s.logger.Info("LP position closed",
		"position_id", pos.ID,
		"realized_pnl", pos.RealizedPnLUSD,
		"apr_bps", pos.APRBps,
	)
}

// GetByID returns a position by ID.
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (*LPPosition, error) {
	return s.repo.GetByID(ctx, id)
}

// ListByUser returns positions for a user, with optional filters.
func (s *Service) ListByUser(ctx context.Context, userID uuid.UUID, status *Status, walletID *uuid.UUID, chainID *string) ([]*LPPosition, error) {
	return s.repo.ListByUser(ctx, userID, status, walletID, chainID)
}
```

**Step 3: Write service_test.go with key scenarios**

Test the core aggregation logic using a mock repository:
- `TestRecordDeposit_UpdatesAggregates`
- `TestRecordWithdraw_ClosesWhenFullyWithdrawn`
- `TestMultiDepositWithdraw` (the $100→-$30→+$50→-all scenario)
- `TestClosePosition_CalculatesPnLAndAPR`
- `TestFindOpenByTokenPair_WarnsOnMultiple`

**Step 4: Run tests**

Run: `cd apps/backend && go test ./internal/platform/lpposition/... -v`

**Step 5: Commit**

```
feat(lpposition): implement LP position service with aggregation and PnL
```

---

### Task 4: Create Database Migration

**Files:**
- Create: `apps/backend/migrations/000022_lp_positions.up.sql`
- Create: `apps/backend/migrations/000022_lp_positions.down.sql`

**Step 1: Create up migration**

Use the SQL from the design doc. See `docs/plans/2026-02-28-lp-position-tracking-design.md` section "Table: lp_positions".

**Step 2: Create down migration**

```sql
DROP TABLE IF EXISTS lp_positions;
```

**Step 3: Run migration**

Run: `just migrate-up`

**Step 4: Commit**

```
feat(db): add lp_positions migration
```

---

### Task 5: Implement PostgreSQL LP Position Repository

**Files:**
- Create: `apps/backend/internal/infra/postgres/lp_position_repo.go`

**Step 1: Implement the `lpposition.Repository` interface**

Follow the pattern from existing repos (e.g., `postgres/taxlot_repo.go`, `postgres/wallet_repo.go`):
- Use `pgxpool.Pool`
- Use `*big.Int` with `pgtype.Numeric` conversion
- `FindOpenByTokenPair` must sort by `opened_at ASC` and match token pair in both orders (token0/token1 or token1/token0)
- `ListByUser` supports optional filters via dynamic WHERE clauses

**Step 2: Verify build**

Run: `cd apps/backend && go build ./...`

**Step 3: Commit**

```
feat(infra): implement PostgreSQL LP position repository
```

---

### Task 6: Create Liquidity Module Handlers

**Files:**
- Create: `apps/backend/internal/module/liquidity/model.go`
- Create: `apps/backend/internal/module/liquidity/errors.go`
- Create: `apps/backend/internal/module/liquidity/entries.go`
- Create: `apps/backend/internal/module/liquidity/handler_deposit.go`
- Create: `apps/backend/internal/module/liquidity/handler_withdraw.go`
- Create: `apps/backend/internal/module/liquidity/handler_claim_fees.go`

**Step 1: Create model.go**

`LPTransaction` struct — extends `DeFiTransaction` pattern with `NFTTokenID` and `LPPositionID` fields. Include `Validate()`, `ValidateClaim()`, `TransfersIn()`, `TransfersOut()` methods (same as DeFi model).

**Step 2: Create errors.go**

Same error set as `defi/errors.go`.

**Step 3: Create entries.go**

Reuse the exact same entry generation logic as `defi/entries.go`:
- `generateSwapLikeEntries()` for deposit/withdraw
- `generateLPClaimEntries()` for claim fees — uses `income.lp.{chain}.{asset}` instead of `income.defi.{chain}.{asset}`
- `generateGasFeeEntries()` — identical to DeFi

**Step 4: Create handler_deposit.go**

Follow `defi/handler_deposit.go` exactly:
- Embed `ledger.BaseHandler` with `ledger.TxTypeLPDeposit`
- Same `Handle()`, `ValidateData()`, `GenerateEntries()` pattern
- `GenerateEntries` calls `generateSwapLikeEntries()` + `generateGasFeeEntries()`

**Step 5: Create handler_withdraw.go**

Same pattern with `ledger.TxTypeLPWithdraw`.

**Step 6: Create handler_claim_fees.go**

Follow `defi/handler_claim.go`:
- Uses `ledger.TxTypeLPClaimFees`
- `ValidateData` calls `ValidateClaim()` (requires at least one IN transfer)
- `GenerateEntries` creates `income.lp.{chain}.{asset}` entries

**Step 7: Write tests for entry generation**

Create `apps/backend/internal/module/liquidity/entries_test.go`:
- Test entries are balanced (sum debits = sum credits)
- Test deposit generates asset_decrease + clearing entries
- Test withdraw generates asset_increase + clearing entries
- Test claim generates asset_increase + income entries
- Test gas fee entries

**Step 8: Run tests and verify build**

Run: `cd apps/backend && go test ./internal/module/liquidity/... -v && go build ./...`

**Step 9: Commit**

```
feat(liquidity): implement LP deposit, withdraw, claim fee handlers
```

---

### Task 7: Extend Zerion Adapter — NFT Token ID and Acts

**Files:**
- Modify: `apps/backend/internal/infra/gateway/zerion/types.go`
- Modify: `apps/backend/internal/infra/gateway/zerion/adapter.go`
- Modify: `apps/backend/internal/platform/sync/model.go`

**Step 1: Add `Act` type to `zerion/types.go`**

After the `TransactionAttributes` struct, add:

```go
// Act represents a single action within a Zerion transaction
type Act struct {
	ID   string           `json:"id"`
	Type string           `json:"type"` // "deposit", "withdraw", "claim", "execute", etc.
}
```

Add `Acts` field to `TransactionAttributes`:

```go
Acts []Act `json:"acts"`
```

**Step 2: Add fields to `DecodedTransaction` in `sync/model.go`**

```go
NFTTokenID string   // Uniswap V3 NFT position ID, empty if not applicable
Acts       []string // Action types from Zerion acts array (e.g., ["claim", "execute"])
```

**Step 3: Update `convertTransaction()` in `zerion/adapter.go`**

After building transfers, extract NFT token ID from skipped NFT transfers:

```go
var nftTokenID string
for _, zt := range td.Attributes.Transfers {
	if zt.NftInfo != nil && zt.NftInfo.TokenID != "" {
		nftTokenID = zt.NftInfo.TokenID
		break
	}
}
```

Extract acts:

```go
acts := make([]string, 0, len(td.Attributes.Acts))
for _, act := range td.Attributes.Acts {
	acts = append(acts, act.Type)
}
```

Set both on the returned `DecodedTransaction`:

```go
NFTTokenID: nftTokenID,
Acts:       acts,
```

**Step 4: Verify build**

Run: `cd apps/backend && go build ./...`

**Step 5: Commit**

```
feat(zerion): extract NFT token ID and acts from Zerion responses
```

---

### Task 8: Update Classifier for LP Classification

**Files:**
- Modify: `apps/backend/internal/platform/sync/classifier.go`
- Create or modify: `apps/backend/internal/platform/sync/classifier_test.go`

**Step 1: Write failing tests**

```go
func TestClassify_UniV3Deposit(t *testing.T) {
	c := NewClassifier()
	tx := DecodedTransaction{
		OperationType: OpDeposit,
		Protocol:      "Uniswap V3",
		Transfers:     []DecodedTransfer{{Direction: DirectionOut, AssetSymbol: "ETH", Amount: big.NewInt(1)}},
	}
	assert.Equal(t, ledger.TxTypeLPDeposit, c.Classify(tx))
}

func TestClassify_UniV3Withdraw(t *testing.T) {
	c := NewClassifier()
	tx := DecodedTransaction{
		OperationType: OpWithdraw,
		Protocol:      "Uniswap V3",
		Transfers:     []DecodedTransfer{{Direction: DirectionIn, AssetSymbol: "ETH", Amount: big.NewInt(1)}},
	}
	assert.Equal(t, ledger.TxTypeLPWithdraw, c.Classify(tx))
}

func TestClassify_UniV3ClaimFees(t *testing.T) {
	c := NewClassifier()
	tx := DecodedTransaction{
		OperationType: OpReceive,
		Protocol:      "Uniswap V3",
		Acts:          []string{"claim"},
		Transfers:     []DecodedTransfer{{Direction: DirectionIn, AssetSymbol: "USDC", Amount: big.NewInt(1)}},
	}
	assert.Equal(t, ledger.TxTypeLPClaimFees, c.Classify(tx))
}

func TestClassify_NonUniDeposit_StaysDeFi(t *testing.T) {
	c := NewClassifier()
	tx := DecodedTransaction{
		OperationType: OpDeposit,
		Protocol:      "Aave",
		Transfers:     []DecodedTransfer{{Direction: DirectionOut, AssetSymbol: "ETH", Amount: big.NewInt(1)}},
	}
	assert.Equal(t, ledger.TxTypeDefiDeposit, c.Classify(tx))
}

func TestClassify_ReceiveNonClaim_StaysTransferIn(t *testing.T) {
	c := NewClassifier()
	tx := DecodedTransaction{
		OperationType: OpReceive,
		Protocol:      "Uniswap V3",
		Acts:          []string{"execute"},
		Transfers:     []DecodedTransfer{{Direction: DirectionIn, AssetSymbol: "ETH", Amount: big.NewInt(1)}},
	}
	assert.Equal(t, ledger.TxTypeTransferIn, c.Classify(tx))
}
```

**Step 2: Update Classify() to check protocol**

```go
func (c *Classifier) Classify(tx DecodedTransaction) ledger.TransactionType {
	if len(tx.Transfers) == 0 {
		return ""
	}

	// Uniswap V3 LP-specific classification
	if c.isUniswapV3(tx.Protocol) {
		if lpType := c.classifyLP(tx); lpType != "" {
			return lpType
		}
	}

	// Existing classification (unchanged)
	switch tx.OperationType {
	// ... (keep all existing cases unchanged)
	}
}

func (c *Classifier) isUniswapV3(protocol string) bool {
	return protocol == "Uniswap V3"
}

func (c *Classifier) classifyLP(tx DecodedTransaction) ledger.TransactionType {
	switch tx.OperationType {
	case OpDeposit, OpMint:
		return ledger.TxTypeLPDeposit
	case OpWithdraw, OpBurn:
		return ledger.TxTypeLPWithdraw
	case OpReceive:
		if c.hasClaimAct(tx.Acts) {
			return ledger.TxTypeLPClaimFees
		}
		return "" // fall through to default classification
	default:
		return "" // fall through to default classification
	}
}

func (c *Classifier) hasClaimAct(acts []string) bool {
	for _, act := range acts {
		if act == "claim" {
			return true
		}
	}
	return false
}
```

**Step 3: Run tests**

Run: `cd apps/backend && go test ./internal/platform/sync/... -v -run TestClassify`

**Step 4: Commit**

```
feat(sync): classify Uniswap V3 LP transactions separately from generic DeFi
```

---

### Task 9: Extend ZerionProcessor with LP Data Builders

**Files:**
- Modify: `apps/backend/internal/platform/sync/zerion_processor.go`

**Step 1: Add LP data builder methods**

```go
func (p *ZerionProcessor) buildLPDepositData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	data["transfers"] = p.buildTransferArray(tx.Transfers)
	data["operation_type"] = string(tx.OperationType)
	if tx.NFTTokenID != "" {
		data["nft_token_id"] = tx.NFTTokenID
	}
	return data
}

func (p *ZerionProcessor) buildLPWithdrawData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	data["transfers"] = p.buildTransferArray(tx.Transfers)
	data["operation_type"] = string(tx.OperationType)
	return data
}

func (p *ZerionProcessor) buildLPClaimFeesData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	data["transfers"] = p.buildTransferArray(tx.Transfers)
	data["operation_type"] = string(tx.OperationType)
	return data
}
```

**Step 2: Add cases to ProcessTransaction switch**

In the `switch txType` block, add before the `default` case:

```go
case ledger.TxTypeLPDeposit:
	data = p.buildLPDepositData(w, tx)
case ledger.TxTypeLPWithdraw:
	data = p.buildLPWithdrawData(w, tx)
case ledger.TxTypeLPClaimFees:
	data = p.buildLPClaimFeesData(w, tx)
```

**Step 3: Verify build**

Run: `cd apps/backend && go build ./...`

**Step 4: Commit**

```
feat(sync): add LP data builders to ZerionProcessor
```

---

### Task 10: Add LP Position Post-Processing to Processor

**Files:**
- Modify: `apps/backend/internal/platform/sync/zerion_processor.go`
- Modify: `apps/backend/internal/platform/sync/service.go` (NewService parameters)

**Step 1: Add `LPPositionService` interface to sync package**

In `port.go`, add:

```go
// LPPositionService manages LP position lifecycle
type LPPositionService interface {
	FindOrCreate(ctx context.Context, userID, walletID uuid.UUID, chainID, protocol, nftTokenID, contractAddress string, token0, token1 lpposition.TokenInfo, openedAt time.Time) (*lpposition.LPPosition, error)
	FindOpenByTokenPair(ctx context.Context, walletID uuid.UUID, chainID, protocol, token0, token1 string) (*lpposition.LPPosition, error)
	RecordDeposit(ctx context.Context, positionID uuid.UUID, token0Amt, token1Amt, usdValue *big.Int) error
	RecordWithdraw(ctx context.Context, positionID uuid.UUID, token0Amt, token1Amt, usdValue *big.Int) error
	RecordClaimFees(ctx context.Context, positionID uuid.UUID, token0Amt, token1Amt, usdValue *big.Int) error
}
```

**Step 2: Inject `LPPositionService` into ZerionProcessor**

Add `lpPositionSvc LPPositionService` field and update `NewZerionProcessor` constructor.

**Step 3: Add post-processing after RecordTransaction**

After the successful `ledgerSvc.RecordTransaction()` call, add LP position update logic:

```go
// Post-process LP transactions: update LP position
if s.lpPositionSvc != nil {
	switch txType {
	case ledger.TxTypeLPDeposit:
		s.handleLPDeposit(ctx, w, tx, data)
	case ledger.TxTypeLPWithdraw:
		s.handleLPWithdraw(ctx, w, tx, data)
	case ledger.TxTypeLPClaimFees:
		s.handleLPClaimFees(ctx, w, tx, data)
	}
}
```

Implement `handleLPDeposit`, `handleLPWithdraw`, `handleLPClaimFees` methods that:
1. Extract token pair info from transfers (OUT transfers for deposit, IN for withdraw/claim)
2. Call `FindOrCreate` (deposit) or `FindOpenByTokenPair` (withdraw/claim)
3. Calculate token0/token1 amounts and total USD value from transfers
4. Call `RecordDeposit`/`RecordWithdraw`/`RecordClaimFees`

**Step 4: Update `sync.NewService()` to accept and pass through `LPPositionService`**

**Step 5: Verify build**

Run: `cd apps/backend && go build ./...`

**Step 6: Commit**

```
feat(sync): add LP position post-processing to sync pipeline
```

---

### Task 11: Create HTTP Handler for LP Positions

**Files:**
- Create: `apps/backend/internal/transport/httpapi/handler/lp_position.go`
- Modify: `apps/backend/internal/transport/httpapi/router.go`

**Step 1: Create handler**

Follow `handler/portfolio.go` pattern:
- Define `LPPositionServiceInterface`
- `LPPositionHandler` struct
- `ListPositions` — GET /lp/positions (with query param filters: status, wallet_id, chain_id)
- `GetPosition` — GET /lp/positions/{id} (returns position details with PnL)
- `GetPositionTransactions` — GET /lp/positions/{id}/transactions (queries ledger transactions by metadata `lp_position_id`)

**Step 2: Add to router config**

Add `LPPositionHandler *handler.LPPositionHandler` to `Config` struct.

Add routes inside the protected group:

```go
if cfg.LPPositionHandler != nil {
	r.Get("/lp/positions", cfg.LPPositionHandler.ListPositions)
	r.Get("/lp/positions/{id}", cfg.LPPositionHandler.GetPosition)
	r.Get("/lp/positions/{id}/transactions", cfg.LPPositionHandler.GetPositionTransactions)
}
```

**Step 3: Verify build**

Run: `cd apps/backend && go build ./...`

**Step 4: Commit**

```
feat(transport): add LP position HTTP endpoints
```

---

### Task 12: Wire Everything in main.go

**Files:**
- Modify: `apps/backend/cmd/api/main.go`

**Step 1: Add imports**

```go
"github.com/kislikjeka/moontrack/internal/module/liquidity"
"github.com/kislikjeka/moontrack/internal/platform/lpposition"
```

**Step 2: Add LP Position wiring after taxLotSvc initialization**

```go
// LP Position
lpPositionRepo := postgres.NewLPPositionRepo(db.Pool)
lpPositionSvc := lpposition.NewService(lpPositionRepo, log)
log.Info("LP Position service initialized")
```

**Step 3: Register LP handlers after DeFi handlers**

```go
// LP handlers (Uniswap V3 liquidity pool operations)
lpDepositHandler := liquidity.NewLPDepositHandler(walletRepo, log)
handlerRegistry.Register(lpDepositHandler)
log.Info("Registered LP deposit handler")

lpWithdrawHandler := liquidity.NewLPWithdrawHandler(walletRepo, log)
handlerRegistry.Register(lpWithdrawHandler)
log.Info("Registered LP withdraw handler")

lpClaimFeesHandler := liquidity.NewLPClaimFeesHandler(walletRepo, log)
handlerRegistry.Register(lpClaimFeesHandler)
log.Info("Registered LP claim fees handler")
```

**Step 4: Pass lpPositionSvc to sync service**

Update the `sync.NewService()` call to include `lpPositionSvc`.

**Step 5: Add LP Position HTTP handler**

```go
lpPositionHandler := handler.NewLPPositionHandler(lpPositionSvc)
```

Add to router config:

```go
LPPositionHandler: lpPositionHandler,
```

**Step 6: Verify build**

Run: `cd apps/backend && go build ./...`

**Step 7: Commit**

```
feat: wire LP position tracking into DI and router
```

---

### Task 13: Update Transaction Service for LP Types

**Files:**
- Modify: `apps/backend/internal/module/transactions/service.go`

**Step 1: Add LP type readers**

In the transaction type switch/map that converts raw data to `TransactionListItem`, add cases for `lp_deposit`, `lp_withdraw`, `lp_claim_fees` that display both token amounts (similar to swap display logic).

**Step 2: Verify build**

Run: `cd apps/backend && go build ./...`

**Step 3: Commit**

```
feat(transactions): add LP transaction type rendering
```

---

### Task 14: Integration Test — Full LP Lifecycle

**Files:**
- Modify or create: `apps/backend/internal/platform/sync/service_integration_test.go`

**Step 1: Write integration test**

Test the full lifecycle: deposit → claim fees → partial withdraw → full withdraw → position closed.

Use the Zerion response format from `zerion_response.json` as test fixtures. Verify:
- Ledger entries are balanced at each step
- Tax lots created/consumed correctly
- LP position aggregates match expected values
- Position closes when fully withdrawn
- PnL and APR are calculated at close

**Step 2: Run integration tests**

Run: `cd apps/backend && go test ./internal/platform/sync/... -v -run TestSync_LP`

**Step 3: Commit**

```
test: add LP lifecycle integration test
```

---

### Task 15: Final Verification

**Step 1: Run all tests**

Run: `cd apps/backend && go test ./... -v -short`

**Step 2: Run linter**

Run: `cd apps/backend && golangci-lint run`

**Step 3: Verify full build**

Run: `cd apps/backend && go build ./...`

**Step 4: Final commit if any fixes needed**

---

## Task Dependency Graph

```
Task 1 (constants) ──────────────────────────────────┐
Task 2 (model + port) ──┬── Task 3 (service) ──┐    │
                         │                       │    │
Task 4 (migration) ─── Task 5 (postgres repo) ──┤    │
                                                  │    │
Task 6 (handlers) ────────────────────────────────┤    │
                                                  │    │
Task 7 (zerion adapter) ── Task 8 (classifier) ──┤    │
                                                  │    │
Task 9 (processor builders) ── Task 10 (post-process) │
                                                  │    │
Task 11 (HTTP handler) ───────────────────────────┤    │
                                                  │    │
Task 12 (main.go wiring) ◄────────────────────────┘    │
                                                       │
Task 13 (tx service) ◄─────────────────────────────────┘
                         │
Task 14 (integration test) ◄── all above
                         │
Task 15 (final verification)
```

Tasks 1-2 can run in parallel. Tasks 4-5 can run in parallel with tasks 6-8. Task 12 depends on all prior tasks.
