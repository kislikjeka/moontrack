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
	assets    map[uuid.UUID]*LendingPositionAsset // keyed by asset ID
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		positions: make(map[uuid.UUID]*LendingPosition),
		assets:    make(map[uuid.UUID]*LendingPositionAsset),
	}
}

func (r *mockRepo) Create(_ context.Context, pos *LendingPosition) error {
	r.positions[pos.ID] = pos
	return nil
}

func (r *mockRepo) Update(_ context.Context, pos *LendingPosition) error {
	r.positions[pos.ID] = pos
	return nil
}

func (r *mockRepo) UpsertAsset(_ context.Context, asset *LendingPositionAsset) error {
	// Store asset by ID
	r.assets[asset.ID] = asset

	// Also update the position's Assets slice
	pos, ok := r.positions[asset.PositionID]
	if !ok {
		return nil
	}

	found := false
	for i := range pos.Assets {
		if pos.Assets[i].Side == asset.Side && pos.Assets[i].Asset == asset.Asset {
			pos.Assets[i] = *asset
			found = true
			break
		}
	}
	if !found {
		pos.Assets = append(pos.Assets, *asset)
	}
	return nil
}

func (r *mockRepo) GetByID(_ context.Context, id uuid.UUID) (*LendingPosition, error) {
	pos, ok := r.positions[id]
	if !ok {
		return nil, nil
	}
	return pos, nil
}

func (r *mockRepo) FindActiveByWalletProtocolChain(_ context.Context, walletID uuid.UUID, protocol, chainID string) (*LendingPosition, error) {
	for _, pos := range r.positions {
		if pos.WalletID == walletID && pos.Protocol == protocol && pos.ChainID == chainID &&
			pos.Status == StatusActive {
			return pos, nil
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

		InterestEarnedUSD: big.NewInt(0),

		Assets: nil,

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

	err := svc.RecordSupply(ctx, pos.ID, "ETH", 18, "0xeth", big.NewInt(1000), big.NewInt(500))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	supplyAsset := updated.FindAsset("supply", "ETH")
	require.NotNil(t, supplyAsset)
	assert.Equal(t, big.NewInt(1000), supplyAsset.Amount)
	assert.Equal(t, big.NewInt(1000), supplyAsset.TotalIn)
	assert.Equal(t, big.NewInt(500), supplyAsset.TotalInUSD)
	assert.Equal(t, StatusActive, updated.Status)
}

func TestRecordSupply_MultipleAssets(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	err := svc.RecordSupply(ctx, pos.ID, "ETH", 18, "0xeth", big.NewInt(1000), big.NewInt(500))
	require.NoError(t, err)

	err = svc.RecordSupply(ctx, pos.ID, "WBTC", 8, "0xwbtc", big.NewInt(2000), big.NewInt(1000))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	assert.Len(t, updated.SupplyAssets(), 2)

	ethAsset := updated.FindAsset("supply", "ETH")
	require.NotNil(t, ethAsset)
	assert.Equal(t, big.NewInt(1000), ethAsset.Amount)

	wbtcAsset := updated.FindAsset("supply", "WBTC")
	require.NotNil(t, wbtcAsset)
	assert.Equal(t, big.NewInt(2000), wbtcAsset.Amount)
}

func TestRecordSupply_AccumulatesOnSameAsset(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	err := svc.RecordSupply(ctx, pos.ID, "ETH", 18, "0xeth", big.NewInt(1000), big.NewInt(500))
	require.NoError(t, err)

	err = svc.RecordSupply(ctx, pos.ID, "ETH", 18, "0xeth", big.NewInt(500), big.NewInt(250))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	ethAsset := updated.FindAsset("supply", "ETH")
	require.NotNil(t, ethAsset)
	assert.Equal(t, big.NewInt(1500), ethAsset.Amount)
	assert.Equal(t, big.NewInt(1500), ethAsset.TotalIn)
	assert.Equal(t, big.NewInt(750), ethAsset.TotalInUSD)
}

func TestRecordWithdraw_ClosesWhenFullyWithdrawn(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	// Supply first
	pos.Assets = []LendingPositionAsset{
		{
			ID: uuid.New(), PositionID: pos.ID, Side: "supply", Asset: "ETH",
			Amount: big.NewInt(1000), Decimals: 18, Contract: "0xeth",
			TotalIn: big.NewInt(1000), TotalOut: big.NewInt(0),
			TotalInUSD: big.NewInt(500), TotalOutUSD: big.NewInt(0),
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		},
	}

	// Withdraw everything
	err := svc.RecordWithdraw(ctx, pos.ID, "ETH", big.NewInt(1000), big.NewInt(600))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	assert.Equal(t, StatusClosed, updated.Status)
	assert.NotNil(t, updated.ClosedAt)

	ethAsset := updated.FindAsset("supply", "ETH")
	require.NotNil(t, ethAsset)
	assert.Equal(t, 0, ethAsset.Amount.Sign(), "supply amount should be zero")
}

func TestRecordWithdraw_StaysOpenWithBorrow(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	pos.Assets = []LendingPositionAsset{
		{
			ID: uuid.New(), PositionID: pos.ID, Side: "supply", Asset: "ETH",
			Amount: big.NewInt(1000), Decimals: 18, Contract: "0xeth",
			TotalIn: big.NewInt(1000), TotalOut: big.NewInt(0),
			TotalInUSD: big.NewInt(500), TotalOutUSD: big.NewInt(0),
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		},
		{
			ID: uuid.New(), PositionID: pos.ID, Side: "borrow", Asset: "USDC",
			Amount: big.NewInt(500), Decimals: 6, Contract: "0xusdc",
			TotalIn: big.NewInt(500), TotalOut: big.NewInt(0),
			TotalInUSD: big.NewInt(500), TotalOutUSD: big.NewInt(0),
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		},
	}

	// Withdraw all supply
	err := svc.RecordWithdraw(ctx, pos.ID, "ETH", big.NewInt(1000), big.NewInt(600))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	assert.Equal(t, StatusActive, updated.Status, "should stay open with outstanding borrow")
	assert.Nil(t, updated.ClosedAt)
}

func TestRecordBorrow_CreatesAsset(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	err := svc.RecordBorrow(ctx, pos.ID, "USDC", 6, "0xusdc", big.NewInt(2000), big.NewInt(2000))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	borrowAsset := updated.FindAsset("borrow", "USDC")
	require.NotNil(t, borrowAsset)
	assert.Equal(t, "USDC", borrowAsset.Asset)
	assert.Equal(t, 6, borrowAsset.Decimals)
	assert.Equal(t, "0xusdc", borrowAsset.Contract)
	assert.Equal(t, big.NewInt(2000), borrowAsset.Amount)
	assert.Equal(t, big.NewInt(2000), borrowAsset.TotalIn)
	assert.Equal(t, big.NewInt(2000), borrowAsset.TotalInUSD)
}

func TestRecordRepay_ReducesDebt(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	pos.Assets = []LendingPositionAsset{
		{
			ID: uuid.New(), PositionID: pos.ID, Side: "supply", Asset: "ETH",
			Amount: big.NewInt(1000), Decimals: 18, Contract: "0xeth",
			TotalIn: big.NewInt(1000), TotalOut: big.NewInt(0),
			TotalInUSD: big.NewInt(500), TotalOutUSD: big.NewInt(0),
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		},
		{
			ID: uuid.New(), PositionID: pos.ID, Side: "borrow", Asset: "USDC",
			Amount: big.NewInt(2000), Decimals: 6, Contract: "0xusdc",
			TotalIn: big.NewInt(2000), TotalOut: big.NewInt(0),
			TotalInUSD: big.NewInt(2000), TotalOutUSD: big.NewInt(0),
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		},
	}

	err := svc.RecordRepay(ctx, pos.ID, "USDC", big.NewInt(1500), big.NewInt(1500))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	borrowAsset := updated.FindAsset("borrow", "USDC")
	require.NotNil(t, borrowAsset)
	assert.Equal(t, big.NewInt(500), borrowAsset.Amount)
	assert.Equal(t, big.NewInt(1500), borrowAsset.TotalOut)
	assert.Equal(t, big.NewInt(1500), borrowAsset.TotalOutUSD)
	assert.Equal(t, StatusActive, updated.Status)
}

func TestRecordRepay_ClosesWhenFullyRepaidAndNoSupply(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	pos.Assets = []LendingPositionAsset{
		{
			ID: uuid.New(), PositionID: pos.ID, Side: "borrow", Asset: "USDC",
			Amount: big.NewInt(500), Decimals: 6, Contract: "0xusdc",
			TotalIn: big.NewInt(500), TotalOut: big.NewInt(0),
			TotalInUSD: big.NewInt(500), TotalOutUSD: big.NewInt(0),
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		},
	}

	err := svc.RecordRepay(ctx, pos.ID, "USDC", big.NewInt(500), big.NewInt(500))
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
		pos.Protocol, pos.ChainID,
		time.Now(),
	)
	require.NoError(t, err)
	assert.Equal(t, pos.ID, found.ID, "should return existing position")
}

func TestFindOrCreate_CreatesNew(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	pos, err := svc.FindOrCreate(ctx, uuid.New(), uuid.New(),
		"Aave V3", "ethereum",
		time.Now(),
	)
	require.NoError(t, err)
	assert.NotNil(t, pos)
	assert.Equal(t, StatusActive, pos.Status)
}

func TestGetPosition_NotFound(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	err := svc.RecordSupply(ctx, uuid.New(), "ETH", 18, "0xeth", big.NewInt(100), big.NewInt(50))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "position not found")
}

func TestShouldClose_EmptyAssets(t *testing.T) {
	pos := &LendingPosition{Assets: nil}
	assert.True(t, pos.ShouldClose())
}

func TestShouldClose_AllZero(t *testing.T) {
	pos := &LendingPosition{
		Assets: []LendingPositionAsset{
			{Side: "supply", Asset: "ETH", Amount: big.NewInt(0)},
			{Side: "borrow", Asset: "USDC", Amount: big.NewInt(0)},
		},
	}
	assert.True(t, pos.ShouldClose())
}

func TestShouldClose_SupplyRemaining(t *testing.T) {
	pos := &LendingPosition{
		Assets: []LendingPositionAsset{
			{Side: "supply", Asset: "ETH", Amount: big.NewInt(100)},
			{Side: "borrow", Asset: "USDC", Amount: big.NewInt(0)},
		},
	}
	assert.False(t, pos.ShouldClose())
}

func TestFindAsset_NotFound(t *testing.T) {
	pos := &LendingPosition{
		Assets: []LendingPositionAsset{
			{Side: "supply", Asset: "ETH", Amount: big.NewInt(100)},
		},
	}
	assert.Nil(t, pos.FindAsset("borrow", "ETH"))
	assert.Nil(t, pos.FindAsset("supply", "WBTC"))
	assert.NotNil(t, pos.FindAsset("supply", "ETH"))
}
