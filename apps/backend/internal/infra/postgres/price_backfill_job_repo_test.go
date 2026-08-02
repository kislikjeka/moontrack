//go:build integration

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/platform/price"
	"github.com/stretchr/testify/require"
)

// seedAsset inserts a minimal on-chain asset and returns its UUID.
// Each call uses a unique symbol and contract address to avoid constraint collisions.
func seedAsset(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	repo := NewAssetRepository(testDB.Pool)
	id := uuid.New()
	// Unique address and symbol per invocation to avoid unique constraint conflicts.
	// An EVM address is 20 bytes / 40 hex chars; a UUID supplies 16, so the
	// remaining 8 hex digits are zero-padded. %032x would produce a 32-char
	// address that fails contract-address validation.
	addr := fmt.Sprintf("0x%040x", [16]byte(id))
	symbol := fmt.Sprintf("T%s", id.String()[:6])
	a, _, err := repo.UpsertByOnChainIdentity(ctx, "ethereum", addr, symbol, "Test Token", 18)
	require.NoError(t, err)
	return a.ID
}

// resetJobQueue truncates price_backfill_jobs so each test starts clean.
// testDB.Reset() does not include this table (it only resets ledger tables).
func resetJobQueue(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	_, err := testDB.Pool.Exec(ctx, "TRUNCATE TABLE price_backfill_jobs CASCADE")
	require.NoError(t, err)
}

func TestPriceBackfillJobRepo_EnqueueIsIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	resetJobQueue(t)
	assetID := seedAsset(t)
	repo := NewPriceBackfillJobRepository(testDB.Pool)

	at := time.Now().UTC().Truncate(time.Minute)
	j1, err := repo.Enqueue(ctx, assetID, at)
	require.NoError(t, err)
	j2, err := repo.Enqueue(ctx, assetID, at)
	require.NoError(t, err)
	require.Equal(t, j1.ID, j2.ID)
}

func TestPriceBackfillJobRepo_ClaimReady_SkipsLocked(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	resetJobQueue(t)
	assetID := seedAsset(t)
	repo := NewPriceBackfillJobRepository(testDB.Pool)

	at := time.Now().UTC().Truncate(time.Minute)
	_, err := repo.Enqueue(ctx, assetID, at)
	require.NoError(t, err)

	j, err := repo.ClaimReady(ctx)
	require.NoError(t, err)
	require.NotNil(t, j)
	require.Equal(t, price.JobStatusInProgress, j.Status)

	j2, err := repo.ClaimReady(ctx)
	require.NoError(t, err)
	require.Nil(t, j2, "second claim should find nothing ready")
}

func TestPriceBackfillJobRepo_RescheduleTerminal(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	resetJobQueue(t)
	assetID := seedAsset(t)
	repo := NewPriceBackfillJobRepository(testDB.Pool)

	at := time.Now().UTC().Truncate(time.Minute)
	j, err := repo.Enqueue(ctx, assetID, at)
	require.NoError(t, err)

	err = repo.Reschedule(ctx, j.ID, 11, time.Now().Add(time.Hour), "exhausted", true)
	require.NoError(t, err)
	// Cannot be claimed anymore (status=failed)
	j2, err := repo.ClaimReady(ctx)
	require.NoError(t, err)
	require.Nil(t, j2)
}
