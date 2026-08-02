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

// Fixed registry IDs so assertions stay deterministic across runs. The names
// are a reader's convenience only — nothing in the code under test derives
// meaning from a ticker any more, which is the point of the change these
// tests cover.
var (
	assetETH  = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	assetWBTC = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	assetUSDC = uuid.MustParse("33333333-3333-4333-8333-333333333333")
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

	err := svc.RecordSupply(ctx, pos.ID, assetETH, big.NewInt(1000), big.NewInt(500))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	supplyAsset := updated.FindAsset("supply", assetETH)
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

	err := svc.RecordSupply(ctx, pos.ID, assetETH, big.NewInt(1000), big.NewInt(500))
	require.NoError(t, err)

	err = svc.RecordSupply(ctx, pos.ID, assetWBTC, big.NewInt(2000), big.NewInt(1000))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	assert.Len(t, updated.SupplyAssets(), 2)

	ethAsset := updated.FindAsset("supply", assetETH)
	require.NotNil(t, ethAsset)
	assert.Equal(t, big.NewInt(1000), ethAsset.Amount)

	wbtcAsset := updated.FindAsset("supply", assetWBTC)
	require.NotNil(t, wbtcAsset)
	assert.Equal(t, big.NewInt(2000), wbtcAsset.Amount)
}

// Two different tokens that both call themselves USDC are two rows, because
// identity is the registry ID. Under the old ticker-keyed model the second
// supply matched the first by symbol and merged into it, silently reporting one
// balance of 3000 where the user holds 1000 of one token and 2000 of another.
func TestRecordSupply_SameTickerDifferentAssetsStaySeparate(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	usdcNative := assetUSDC
	usdcBridged := uuid.MustParse("44444444-4444-4444-8444-444444444444")

	err := svc.RecordSupply(ctx, pos.ID, usdcNative, big.NewInt(1000), big.NewInt(1000))
	require.NoError(t, err)

	err = svc.RecordSupply(ctx, pos.ID, usdcBridged, big.NewInt(2000), big.NewInt(2000))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	require.Len(t, updated.SupplyAssets(), 2, "same ticker must not collapse two assets into one row")

	native := updated.FindAsset("supply", usdcNative)
	require.NotNil(t, native)
	assert.Equal(t, big.NewInt(1000), native.Amount)

	bridged := updated.FindAsset("supply", usdcBridged)
	require.NotNil(t, bridged)
	assert.Equal(t, big.NewInt(2000), bridged.Amount)
}

func TestRecordSupply_AccumulatesOnSameAsset(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	err := svc.RecordSupply(ctx, pos.ID, assetETH, big.NewInt(1000), big.NewInt(500))
	require.NoError(t, err)

	err = svc.RecordSupply(ctx, pos.ID, assetETH, big.NewInt(500), big.NewInt(250))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	ethAsset := updated.FindAsset("supply", assetETH)
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
			ID: uuid.New(), PositionID: pos.ID, Side: "supply", Asset: assetETH,
			Amount:  big.NewInt(1000),
			TotalIn: big.NewInt(1000), TotalOut: big.NewInt(0),
			TotalInUSD: big.NewInt(500), TotalOutUSD: big.NewInt(0),
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		},
	}

	// Withdraw everything
	err := svc.RecordWithdraw(ctx, pos.ID, assetETH, big.NewInt(1000), big.NewInt(600))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	assert.Equal(t, StatusClosed, updated.Status)
	assert.NotNil(t, updated.ClosedAt)

	ethAsset := updated.FindAsset("supply", assetETH)
	require.NotNil(t, ethAsset)
	assert.Equal(t, 0, ethAsset.Amount.Sign(), "supply amount should be zero")
}

func TestRecordWithdraw_StaysOpenWithBorrow(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	pos.Assets = []LendingPositionAsset{
		{
			ID: uuid.New(), PositionID: pos.ID, Side: "supply", Asset: assetETH,
			Amount:  big.NewInt(1000),
			TotalIn: big.NewInt(1000), TotalOut: big.NewInt(0),
			TotalInUSD: big.NewInt(500), TotalOutUSD: big.NewInt(0),
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		},
		{
			ID: uuid.New(), PositionID: pos.ID, Side: "borrow", Asset: assetUSDC,
			Amount:  big.NewInt(500),
			TotalIn: big.NewInt(500), TotalOut: big.NewInt(0),
			TotalInUSD: big.NewInt(500), TotalOutUSD: big.NewInt(0),
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		},
	}

	// Withdraw all supply
	err := svc.RecordWithdraw(ctx, pos.ID, assetETH, big.NewInt(1000), big.NewInt(600))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	assert.Equal(t, StatusActive, updated.Status, "should stay open with outstanding borrow")
	assert.Nil(t, updated.ClosedAt)
}

func TestRecordBorrow_CreatesAsset(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	err := svc.RecordBorrow(ctx, pos.ID, assetUSDC, big.NewInt(2000), big.NewInt(2000))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	borrowAsset := updated.FindAsset("borrow", assetUSDC)
	require.NotNil(t, borrowAsset)
	// Identity is the registry ID and nothing else; decimals and contract are
	// no longer copied onto the row, so there is nothing here that could drift
	// from the registry.
	assert.Equal(t, assetUSDC, borrowAsset.Asset)
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
			ID: uuid.New(), PositionID: pos.ID, Side: "supply", Asset: assetETH,
			Amount:  big.NewInt(1000),
			TotalIn: big.NewInt(1000), TotalOut: big.NewInt(0),
			TotalInUSD: big.NewInt(500), TotalOutUSD: big.NewInt(0),
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		},
		{
			ID: uuid.New(), PositionID: pos.ID, Side: "borrow", Asset: assetUSDC,
			Amount:  big.NewInt(2000),
			TotalIn: big.NewInt(2000), TotalOut: big.NewInt(0),
			TotalInUSD: big.NewInt(2000), TotalOutUSD: big.NewInt(0),
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		},
	}

	err := svc.RecordRepay(ctx, pos.ID, assetUSDC, big.NewInt(1500), big.NewInt(1500))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	borrowAsset := updated.FindAsset("borrow", assetUSDC)
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
			ID: uuid.New(), PositionID: pos.ID, Side: "borrow", Asset: assetUSDC,
			Amount:  big.NewInt(500),
			TotalIn: big.NewInt(500), TotalOut: big.NewInt(0),
			TotalInUSD: big.NewInt(500), TotalOutUSD: big.NewInt(0),
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		},
	}

	err := svc.RecordRepay(ctx, pos.ID, assetUSDC, big.NewInt(500), big.NewInt(500))
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

	err := svc.RecordSupply(ctx, uuid.New(), assetETH, big.NewInt(100), big.NewInt(50))
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
			{Side: "supply", Asset: assetETH, Amount: big.NewInt(0)},
			{Side: "borrow", Asset: assetUSDC, Amount: big.NewInt(0)},
		},
	}
	assert.True(t, pos.ShouldClose())
}

func TestShouldClose_SupplyRemaining(t *testing.T) {
	pos := &LendingPosition{
		Assets: []LendingPositionAsset{
			{Side: "supply", Asset: assetETH, Amount: big.NewInt(100)},
			{Side: "borrow", Asset: assetUSDC, Amount: big.NewInt(0)},
		},
	}
	assert.False(t, pos.ShouldClose())
}

func TestFindAsset_NotFound(t *testing.T) {
	pos := &LendingPosition{
		Assets: []LendingPositionAsset{
			{Side: "supply", Asset: assetETH, Amount: big.NewInt(100)},
		},
	}
	assert.Nil(t, pos.FindAsset("borrow", assetETH))
	assert.Nil(t, pos.FindAsset("supply", assetWBTC))
	assert.NotNil(t, pos.FindAsset("supply", assetETH))
}
