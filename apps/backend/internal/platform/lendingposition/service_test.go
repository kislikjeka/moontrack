package lendingposition

import (
	"context"
	"io"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/pkg/logger"
)

// mockRepo is an in-memory implementation of Repository for testing.
type mockRepo struct {
	positions map[uuid.UUID]*LendingPosition
}

func newMockRepo() *mockRepo {
	return &mockRepo{positions: make(map[uuid.UUID]*LendingPosition)}
}

func (r *mockRepo) Create(_ context.Context, pos *LendingPosition) error {
	r.positions[pos.ID] = pos
	return nil
}

func (r *mockRepo) Update(_ context.Context, pos *LendingPosition) error {
	r.positions[pos.ID] = pos
	return nil
}

func (r *mockRepo) GetByID(_ context.Context, id uuid.UUID) (*LendingPosition, error) {
	pos, ok := r.positions[id]
	if !ok {
		return nil, nil
	}
	return pos, nil
}

func (r *mockRepo) FindActiveByWalletAndAsset(_ context.Context, walletID uuid.UUID, protocol, chainID, supplyAsset, borrowAsset string) (*LendingPosition, error) {
	for _, pos := range r.positions {
		if pos.WalletID == walletID && pos.Protocol == protocol && pos.ChainID == chainID &&
			pos.SupplyAsset == supplyAsset && pos.Status == StatusActive {
			if borrowAsset == "" || pos.BorrowAsset == borrowAsset {
				return pos, nil
			}
		}
	}
	return nil, nil
}

func (r *mockRepo) ListByUser(_ context.Context, userID uuid.UUID, status *Status, walletID *uuid.UUID, chainID *string) ([]*LendingPosition, error) {
	var result []*LendingPosition
	for _, pos := range r.positions {
		if pos.UserID != userID {
			continue
		}
		if status != nil && pos.Status != *status {
			continue
		}
		if walletID != nil && pos.WalletID != *walletID {
			continue
		}
		if chainID != nil && pos.ChainID != *chainID {
			continue
		}
		result = append(result, pos)
	}
	return result, nil
}

func newTestService() (*Service, *mockRepo) {
	repo := newMockRepo()
	log := logger.New("test", io.Discard)
	svc := NewService(repo, log)
	return svc, repo
}

func createTestPosition(repo *mockRepo) *LendingPosition {
	pos := &LendingPosition{
		ID:       uuid.New(),
		UserID:   uuid.New(),
		WalletID: uuid.New(),
		ChainID:  "ethereum",
		Protocol: "Aave V3",

		SupplyAsset:    "ETH",
		SupplyAmount:   big.NewInt(0),
		SupplyDecimals: 18,
		SupplyContract: "0xeth",

		BorrowAmount: big.NewInt(0),

		TotalSupplied:  big.NewInt(0),
		TotalWithdrawn: big.NewInt(0),
		TotalBorrowed:  big.NewInt(0),
		TotalRepaid:    big.NewInt(0),

		TotalSuppliedUSD:  big.NewInt(0),
		TotalWithdrawnUSD: big.NewInt(0),
		TotalBorrowedUSD:  big.NewInt(0),
		TotalRepaidUSD:    big.NewInt(0),

		InterestEarnedUSD: big.NewInt(0),

		Status:    StatusActive,
		OpenedAt:  time.Now().UTC().Add(-24 * time.Hour),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	repo.positions[pos.ID] = pos
	return pos
}

func TestRecordSupply_UpdatesAggregates(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	err := svc.RecordSupply(ctx, pos.ID, big.NewInt(1000), big.NewInt(500))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	assert.Equal(t, big.NewInt(1000), updated.SupplyAmount)
	assert.Equal(t, big.NewInt(1000), updated.TotalSupplied)
	assert.Equal(t, big.NewInt(500), updated.TotalSuppliedUSD)
	assert.Equal(t, StatusActive, updated.Status)
}

func TestRecordWithdraw_ClosesWhenFullyWithdrawn(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	// Supply first
	pos.SupplyAmount = big.NewInt(1000)
	pos.TotalSupplied = big.NewInt(1000)
	pos.TotalSuppliedUSD = big.NewInt(500)

	// Withdraw everything
	err := svc.RecordWithdraw(ctx, pos.ID, big.NewInt(1000), big.NewInt(600))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	assert.Equal(t, StatusClosed, updated.Status)
	assert.NotNil(t, updated.ClosedAt)
	assert.Equal(t, 0, updated.SupplyAmount.Sign(), "supply amount should be zero")
}

func TestRecordWithdraw_StaysOpenWithBorrow(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	pos.SupplyAmount = big.NewInt(1000)
	pos.BorrowAmount = big.NewInt(500) // Outstanding borrow

	// Withdraw all supply
	err := svc.RecordWithdraw(ctx, pos.ID, big.NewInt(1000), big.NewInt(600))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	assert.Equal(t, StatusActive, updated.Status, "should stay open with outstanding borrow")
	assert.Nil(t, updated.ClosedAt)
}

func TestRecordBorrow_SetsBorrowAsset(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	err := svc.RecordBorrow(ctx, pos.ID, "USDC", 6, "0xusdc", big.NewInt(2000), big.NewInt(2000))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	assert.Equal(t, "USDC", updated.BorrowAsset)
	assert.Equal(t, 6, updated.BorrowDecimals)
	assert.Equal(t, "0xusdc", updated.BorrowContract)
	assert.Equal(t, big.NewInt(2000), updated.BorrowAmount)
	assert.Equal(t, big.NewInt(2000), updated.TotalBorrowed)
	assert.Equal(t, big.NewInt(2000), updated.TotalBorrowedUSD)
}

func TestRecordRepay_ReducesDebt(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	pos.BorrowAsset = "USDC"
	pos.BorrowAmount = big.NewInt(2000)

	err := svc.RecordRepay(ctx, pos.ID, big.NewInt(1500), big.NewInt(1500))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	assert.Equal(t, big.NewInt(500), updated.BorrowAmount)
	assert.Equal(t, big.NewInt(1500), updated.TotalRepaid)
	assert.Equal(t, big.NewInt(1500), updated.TotalRepaidUSD)
	assert.Equal(t, StatusActive, updated.Status)
}

func TestRecordRepay_ClosesWhenFullyRepaidAndNoSupply(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	pos.BorrowAmount = big.NewInt(500)
	pos.SupplyAmount = big.NewInt(0)

	err := svc.RecordRepay(ctx, pos.ID, big.NewInt(500), big.NewInt(500))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	assert.Equal(t, StatusClosed, updated.Status)
	assert.NotNil(t, updated.ClosedAt)
}

func TestRecordClaim_AddsInterest(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	err := svc.RecordClaim(ctx, pos.ID, big.NewInt(100))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	assert.Equal(t, big.NewInt(100), updated.InterestEarnedUSD)
	assert.Equal(t, StatusActive, updated.Status)

	// Claim again
	err = svc.RecordClaim(ctx, pos.ID, big.NewInt(50))
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(150), updated.InterestEarnedUSD)
}

func TestFindOrCreate_ReusesExisting(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	found, err := svc.FindOrCreate(ctx, pos.UserID, pos.WalletID,
		pos.Protocol, pos.ChainID, pos.SupplyAsset,
		pos.SupplyDecimals, pos.SupplyContract,
		time.Now(),
	)
	require.NoError(t, err)
	assert.Equal(t, pos.ID, found.ID, "should return existing position")
}

func TestFindOrCreate_CreatesNew(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	pos, err := svc.FindOrCreate(ctx, uuid.New(), uuid.New(),
		"Aave V3", "ethereum", "WBTC",
		8, "0xwbtc",
		time.Now(),
	)
	require.NoError(t, err)
	assert.NotNil(t, pos)
	assert.Equal(t, StatusActive, pos.Status)
	assert.Equal(t, "WBTC", pos.SupplyAsset)
	assert.Equal(t, 8, pos.SupplyDecimals)
}

func TestGetPosition_NotFound(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	err := svc.RecordSupply(ctx, uuid.New(), big.NewInt(100), big.NewInt(50))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "position not found")
}
