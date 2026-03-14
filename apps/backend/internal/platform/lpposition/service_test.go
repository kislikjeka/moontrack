package lpposition

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
	positions map[uuid.UUID]*LPPosition
}

func newMockRepo() *mockRepo {
	return &mockRepo{positions: make(map[uuid.UUID]*LPPosition)}
}

func (r *mockRepo) Create(_ context.Context, pos *LPPosition) error {
	r.positions[pos.ID] = pos
	return nil
}

func (r *mockRepo) Update(_ context.Context, pos *LPPosition) error {
	r.positions[pos.ID] = pos
	return nil
}

func (r *mockRepo) GetByID(_ context.Context, id uuid.UUID) (*LPPosition, error) {
	pos, ok := r.positions[id]
	if !ok {
		return nil, nil
	}
	return pos, nil
}

func (r *mockRepo) GetByNFTTokenID(_ context.Context, walletID uuid.UUID, chainID, nftTokenID string) (*LPPosition, error) {
	for _, pos := range r.positions {
		if pos.WalletID == walletID && pos.ChainID == chainID && pos.NFTTokenID == nftTokenID {
			return pos, nil
		}
	}
	return nil, nil
}

func (r *mockRepo) FindOpenByTokenPair(_ context.Context, walletID uuid.UUID, chainID, protocol, token0, token1 string) ([]*LPPosition, error) {
	var result []*LPPosition
	for _, pos := range r.positions {
		if pos.WalletID == walletID && pos.ChainID == chainID && pos.Protocol == protocol && pos.Status == StatusOpen {
			if (pos.Token0Contract == token0 && pos.Token1Contract == token1) ||
				(pos.Token0Contract == token1 && pos.Token1Contract == token0) {
				result = append(result, pos)
			}
		}
	}
	return result, nil
}

func (r *mockRepo) ListByUser(_ context.Context, userID uuid.UUID, status *Status, walletID *uuid.UUID, chainID *string) ([]*LPPosition, error) {
	var result []*LPPosition
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

func createTestPosition(repo *mockRepo) *LPPosition {
	pos := &LPPosition{
		ID:                   uuid.New(),
		UserID:               uuid.New(),
		WalletID:             uuid.New(),
		ChainID:              "ethereum",
		Protocol:             "Uniswap V3",
		NFTTokenID:           "12345",
		Token0Symbol:         "ETH",
		Token1Symbol:         "USDC",
		Token0Contract:       "0xeth",
		Token1Contract:       "0xusdc",
		Token0Decimals:       18,
		Token1Decimals:       6,
		TotalDepositedUSD:    big.NewInt(0),
		TotalWithdrawnUSD:    big.NewInt(0),
		TotalClaimedFeesUSD:  big.NewInt(0),
		TotalDepositedToken0: big.NewInt(0),
		TotalDepositedToken1: big.NewInt(0),
		TotalWithdrawnToken0: big.NewInt(0),
		TotalWithdrawnToken1: big.NewInt(0),
		TotalClaimedToken0:   big.NewInt(0),
		TotalClaimedToken1:   big.NewInt(0),
		Status:               StatusOpen,
		OpenedAt:             time.Now().UTC().Add(-24 * time.Hour),
		CreatedAt:            time.Now().UTC(),
		UpdatedAt:            time.Now().UTC(),
	}
	repo.positions[pos.ID] = pos
	return pos
}

func TestRecordDeposit_UpdatesAggregates(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	err := svc.RecordDeposit(ctx, pos.ID, big.NewInt(1000), big.NewInt(2000), big.NewInt(500))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	assert.Equal(t, big.NewInt(500), updated.TotalDepositedUSD)
	assert.Equal(t, big.NewInt(1000), updated.TotalDepositedToken0)
	assert.Equal(t, big.NewInt(2000), updated.TotalDepositedToken1)
	assert.Equal(t, StatusOpen, updated.Status)
}

func TestRecordWithdraw_ClosesWhenFullyWithdrawn(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	// Deposit first
	pos.TotalDepositedToken0 = big.NewInt(1000)
	pos.TotalDepositedToken1 = big.NewInt(2000)
	pos.TotalDepositedUSD = big.NewInt(500)

	// Withdraw everything
	err := svc.RecordWithdraw(ctx, pos.ID, big.NewInt(1000), big.NewInt(2000), big.NewInt(600))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	assert.Equal(t, StatusClosed, updated.Status)
	assert.NotNil(t, updated.ClosedAt)
	assert.NotNil(t, updated.RealizedPnLUSD)
	// PnL = withdrawn(600) + claimed(0) - deposited(500) = 100
	assert.Equal(t, big.NewInt(100), updated.RealizedPnLUSD)
}

func TestMultiDepositWithdraw(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	// Deposit $100 worth
	err := svc.RecordDeposit(ctx, pos.ID, big.NewInt(100), big.NewInt(200), big.NewInt(100))
	require.NoError(t, err)

	// Withdraw $30 worth (partial)
	err = svc.RecordWithdraw(ctx, pos.ID, big.NewInt(30), big.NewInt(60), big.NewInt(30))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	assert.Equal(t, StatusOpen, updated.Status, "should remain open after partial withdraw")

	// Deposit $50 more
	err = svc.RecordDeposit(ctx, pos.ID, big.NewInt(50), big.NewInt(100), big.NewInt(50))
	require.NoError(t, err)

	assert.Equal(t, big.NewInt(150), updated.TotalDepositedUSD)
	assert.Equal(t, big.NewInt(150), updated.TotalDepositedToken0)
	assert.Equal(t, big.NewInt(300), updated.TotalDepositedToken1)

	// Withdraw all remaining: 150-30=120 token0, 300-60=240 token1
	err = svc.RecordWithdraw(ctx, pos.ID, big.NewInt(120), big.NewInt(240), big.NewInt(130))
	require.NoError(t, err)

	updated = repo.positions[pos.ID]
	assert.Equal(t, StatusClosed, updated.Status)
	// PnL = withdrawn(30+130) + claimed(0) - deposited(150) = 10
	assert.Equal(t, big.NewInt(10), updated.RealizedPnLUSD)
}

func TestClosePosition_CalculatesPnLAndAPR(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	// Set opened_at to exactly 365 days ago for easy APR calculation
	pos.OpenedAt = time.Now().UTC().Add(-365 * 24 * time.Hour)

	// Deposit 1000 USD
	pos.TotalDepositedUSD = big.NewInt(1000)
	pos.TotalDepositedToken0 = big.NewInt(100)
	pos.TotalDepositedToken1 = big.NewInt(200)

	// Claim fees worth 50 USD
	err := svc.RecordClaimFees(ctx, pos.ID, big.NewInt(5), big.NewInt(10), big.NewInt(50))
	require.NoError(t, err)

	// Withdraw all with value 1100 USD
	err = svc.RecordWithdraw(ctx, pos.ID, big.NewInt(100), big.NewInt(200), big.NewInt(1100))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	assert.Equal(t, StatusClosed, updated.Status)
	// PnL = withdrawn(1100) + claimed(50) - deposited(1000) = 150
	assert.Equal(t, big.NewInt(150), updated.RealizedPnLUSD)
	// APR = 150/1000 * 10000 = 1500 bps (~15% APR over ~1 year)
	require.NotNil(t, updated.APRBps)
	// Allow small tolerance due to duration not being exactly 365 days
	assert.InDelta(t, 1500, *updated.APRBps, 10)
}

func TestRecordWithdraw_ClosesWithImpermanentLoss(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	// Deposit 1000 token0, 2000 token1
	pos.TotalDepositedToken0 = big.NewInt(1000)
	pos.TotalDepositedToken1 = big.NewInt(2000)
	pos.TotalDepositedUSD = big.NewInt(500)

	// Withdraw with impermanent loss: over-withdraw token0, under-withdraw token1.
	// token0: 1050 withdrawn (50 over), token1: 1960 withdrawn (40 under).
	// Net remaining = (1000-1050) + (2000-1960) = -50 + 40 = -10 <= 0 → closed.
	err := svc.RecordWithdraw(ctx, pos.ID, big.NewInt(1050), big.NewInt(1960), big.NewInt(500))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	assert.Equal(t, StatusClosed, updated.Status, "should close when net remaining <= 0 despite positive token1 remainder")
	assert.NotNil(t, updated.ClosedAt)
}

func TestRecordWithdraw_StaysOpenWithPositiveNetRemaining(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	// Deposit 1000 token0, 2000 token1
	pos.TotalDepositedToken0 = big.NewInt(1000)
	pos.TotalDepositedToken1 = big.NewInt(2000)
	pos.TotalDepositedUSD = big.NewInt(500)

	// Partial withdraw: net remaining = (1000-500) + (2000-800) = 500 + 1200 = 1700 > 0
	err := svc.RecordWithdraw(ctx, pos.ID, big.NewInt(500), big.NewInt(800), big.NewInt(250))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	assert.Equal(t, StatusOpen, updated.Status, "should remain open with positive net remaining")
}

func TestRecordClaimFees_ClosesAlreadyWithdrawnPosition(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	pos.OpenedAt = time.Now().UTC().Add(-24 * time.Hour)

	// Simulate a position that was fully withdrawn but not yet closed
	// (e.g., withdraw happened before IsFullyWithdrawn was fixed).
	pos.TotalDepositedToken0 = big.NewInt(1000)
	pos.TotalDepositedToken1 = big.NewInt(2000)
	pos.TotalDepositedUSD = big.NewInt(500)
	pos.TotalWithdrawnToken0 = big.NewInt(1000)
	pos.TotalWithdrawnToken1 = big.NewInt(2000)
	pos.TotalWithdrawnUSD = big.NewInt(500)
	// Position is fully withdrawn but still open
	pos.Status = StatusOpen

	// Claim fees
	err := svc.RecordClaimFees(ctx, pos.ID, big.NewInt(5), big.NewInt(10), big.NewInt(3))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	assert.Equal(t, StatusClosed, updated.Status, "claim should close already-withdrawn position")
	assert.NotNil(t, updated.ClosedAt)
	// PnL = withdrawn(500) + claimed(3) - deposited(500) = 3
	assert.Equal(t, big.NewInt(3), updated.RealizedPnLUSD)
}

func TestRecordClaimFees_UpdatesAggregates(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	// Ensure position has deposits so it's not considered fully withdrawn
	pos.TotalDepositedToken0 = big.NewInt(1000)
	pos.TotalDepositedToken1 = big.NewInt(2000)
	pos.TotalDepositedUSD = big.NewInt(500)

	err := svc.RecordClaimFees(ctx, pos.ID, big.NewInt(50), big.NewInt(100), big.NewInt(25))
	require.NoError(t, err)

	updated := repo.positions[pos.ID]
	assert.Equal(t, big.NewInt(25), updated.TotalClaimedFeesUSD)
	assert.Equal(t, big.NewInt(50), updated.TotalClaimedToken0)
	assert.Equal(t, big.NewInt(100), updated.TotalClaimedToken1)
	assert.Equal(t, StatusOpen, updated.Status, "claim should not close position with remaining deposits")
}

func TestFindOrCreate_ReusesExistingByNFT(t *testing.T) {
	svc, repo := newTestService()
	pos := createTestPosition(repo)
	ctx := context.Background()

	found, err := svc.FindOrCreate(ctx, pos.UserID, pos.WalletID, pos.ChainID, pos.Protocol, pos.NFTTokenID, "",
		TokenInfo{Symbol: "ETH", Contract: "0xeth", Decimals: 18},
		TokenInfo{Symbol: "USDC", Contract: "0xusdc", Decimals: 6},
		time.Now(),
	)
	require.NoError(t, err)
	assert.Equal(t, pos.ID, found.ID, "should return existing position by NFT token ID")
}

func TestFindOrCreate_CreatesNewWhenNoNFTMatch(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	pos, err := svc.FindOrCreate(ctx, uuid.New(), uuid.New(), "ethereum", "Uniswap V3", "99999", "",
		TokenInfo{Symbol: "WBTC", Contract: "0xwbtc", Decimals: 8},
		TokenInfo{Symbol: "ETH", Contract: "0xeth", Decimals: 18},
		time.Now(),
	)
	require.NoError(t, err)
	assert.NotNil(t, pos)
	assert.Equal(t, StatusOpen, pos.Status)
	assert.Equal(t, "WBTC", pos.Token0Symbol)
	assert.Equal(t, "ETH", pos.Token1Symbol)
}

func TestFindOpenByTokenPair_WarnsOnMultiple(t *testing.T) {
	svc, repo := newTestService()
	ctx := context.Background()

	walletID := uuid.New()
	userID := uuid.New()

	// Create two positions with same token pair
	for i := 0; i < 2; i++ {
		pos := &LPPosition{
			ID:                   uuid.New(),
			UserID:               userID,
			WalletID:             walletID,
			ChainID:              "ethereum",
			Protocol:             "Uniswap V3",
			Token0Contract:       "0xeth",
			Token1Contract:       "0xusdc",
			TotalDepositedUSD:    big.NewInt(0),
			TotalWithdrawnUSD:    big.NewInt(0),
			TotalClaimedFeesUSD:  big.NewInt(0),
			TotalDepositedToken0: big.NewInt(0),
			TotalDepositedToken1: big.NewInt(0),
			TotalWithdrawnToken0: big.NewInt(0),
			TotalWithdrawnToken1: big.NewInt(0),
			TotalClaimedToken0:   big.NewInt(0),
			TotalClaimedToken1:   big.NewInt(0),
			Status:               StatusOpen,
			OpenedAt:             time.Now().UTC(),
		}
		repo.positions[pos.ID] = pos
	}

	// Should return one of them without error (oldest by repo)
	found, err := svc.FindOpenByTokenPair(ctx, walletID, "ethereum", "Uniswap V3", "0xeth", "0xusdc")
	require.NoError(t, err)
	assert.NotNil(t, found)
}
