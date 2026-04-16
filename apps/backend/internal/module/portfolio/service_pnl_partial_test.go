package portfolio

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockLotStatusCounter is an in-memory implementation of LotStatusCounter for tests.
type MockLotStatusCounter struct {
	pending     int
	unpriceable int
	err         error
}

func (m *MockLotStatusCounter) CountLotsByPriceStatus(_ context.Context, _ uuid.UUID) (int, int, error) {
	return m.pending, m.unpriceable, m.err
}

// TestPortfolioSummary_PnLIsPartial_WhenPendingLotsExist verifies that
// PnLIsPartial is true and counts are populated when pending lots are present.
func TestPortfolioSummary_PnLIsPartial_WhenPendingLotsExist(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	svc := NewPortfolioService(
		setupMockLedgerRepository(),
		setupMockWalletRepository(),
		setupMockPriceService(),
		nil,
		nil,
	).WithLotStatusCounter(&MockLotStatusCounter{pending: 3, unpriceable: 1})

	// Empty wallets — we only care about lot count fields.
	svc.walletRepo.(*MockWalletRepository).SetMockWallets(userID, nil)

	summary, err := svc.GetPortfolioSummary(ctx, userID)

	require.NoError(t, err)
	assert.True(t, summary.PnLIsPartial, "PnLIsPartial should be true when pending > 0")
	assert.Equal(t, 3, summary.PendingLotCount)
	assert.Equal(t, 1, summary.UnpriceableLotCount)
}

// TestPortfolioSummary_PnLIsPartial_FalseWhenNoPendingLots verifies that
// PnLIsPartial is false when there are no pending lots (even if unpriceable > 0).
func TestPortfolioSummary_PnLIsPartial_FalseWhenNoPendingLots(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	svc := NewPortfolioService(
		setupMockLedgerRepository(),
		setupMockWalletRepository(),
		setupMockPriceService(),
		nil,
		nil,
	).WithLotStatusCounter(&MockLotStatusCounter{pending: 0, unpriceable: 5})

	svc.walletRepo.(*MockWalletRepository).SetMockWallets(userID, nil)

	summary, err := svc.GetPortfolioSummary(ctx, userID)

	require.NoError(t, err)
	assert.False(t, summary.PnLIsPartial, "PnLIsPartial should be false when pending == 0")
	assert.Equal(t, 0, summary.PendingLotCount)
	assert.Equal(t, 5, summary.UnpriceableLotCount)
}

// TestPortfolioSummary_PnLFields_ZeroWhenNoCounter verifies that the fields
// remain zero-valued when no LotStatusCounter is wired (backward compat).
func TestPortfolioSummary_PnLFields_ZeroWhenNoCounter(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	svc := NewPortfolioService(
		setupMockLedgerRepository(),
		setupMockWalletRepository(),
		setupMockPriceService(),
		nil,
		nil,
	) // No WithLotStatusCounter

	svc.walletRepo.(*MockWalletRepository).SetMockWallets(userID, nil)

	summary, err := svc.GetPortfolioSummary(ctx, userID)

	require.NoError(t, err)
	assert.False(t, summary.PnLIsPartial)
	assert.Equal(t, 0, summary.PendingLotCount)
	assert.Equal(t, 0, summary.UnpriceableLotCount)
}
