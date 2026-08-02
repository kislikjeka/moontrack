//go:build integration

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/platform/price"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/stretchr/testify/require"
)

// seedAsset inserts a minimal asset into the registry and returns its UUID.
// Each call uses a unique contract address to avoid constraint collisions.
//
// It seeds asset_registry rather than the `assets` table it used to (#59):
// price_history.asset_id and price_backfill_jobs.asset_id are now FKs into the
// registry, so a row seeded anywhere else would fail the constraint.
func seedAsset(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	repo := NewAssetRegistryRepository(testDB.Pool)
	id := uuid.New()
	// Unique address per invocation to avoid unique constraint conflicts. An EVM
	// address is 20 bytes / 40 hex chars; a UUID supplies 16, so the remaining 8
	// hex digits are zero-padded.
	addr := fmt.Sprintf("0x%040x", [16]byte(id))
	symbol := fmt.Sprintf("T%s", id.String()[:6])
	a, err := repo.Resolve(ctx, sync.NewAssetKey("ethereum", addr), symbol, "Test Token", 18)
	require.NoError(t, err)
	return a.ID
}

// seedAssetTicker returns the registry id for a ticker, inserting the row on
// first use and returning the same id afterwards.
//
// Unlike seedAsset it is STABLE per ticker, which is what a ledger test needs:
// entries, accounts, account_balances and tax_lots all carry an FK into
// asset_registry since #59, so two entries meant to be "the same asset" have to
// resolve to one row or the balance they are supposed to net out against does
// not exist. Resolve is idempotent on (chain, contract), so repeated calls
// across tests converge on one id — and testDB.Reset does not truncate
// asset_registry, so the row outlives the reset between tests.
func seedAssetTicker(t *testing.T, ticker string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	repo := NewAssetRegistryRepository(testDB.Pool)
	// A deterministic pseudo-contract per ticker: distinct tickers must not
	// collide on the registry's UNIQUE (chain, contract).
	addr := fmt.Sprintf("0x%040x", []byte(fmt.Sprintf("%-20s", ticker))[:20])
	a, err := repo.Resolve(ctx, sync.NewAssetKey("ethereum", addr), ticker, ticker, 18)
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
