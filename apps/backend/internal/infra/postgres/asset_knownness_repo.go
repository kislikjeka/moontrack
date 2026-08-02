package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kislikjeka/moontrack/internal/platform/sync"
)

// Compile-time checks: this one repository serves both sides of the knownness
// registry — the read side used by the sync hot path, and the queue side used by
// the background probe worker.
var (
	_ sync.KnownnessRegistry = (*AssetKnownnessRepository)(nil)
	_ sync.KnownnessQueue    = (*AssetKnownnessRepository)(nil)
)

// AssetKnownnessRepository is the knownness registry backed by the
// asset_knownness table, keyed on (chain, contract) (issue #58).
type AssetKnownnessRepository struct {
	pool *pgxpool.Pool
}

// NewAssetKnownnessRepository creates a PostgreSQL-backed knownness registry.
func NewAssetKnownnessRepository(pool *pgxpool.Pool) *AssetKnownnessRepository {
	return &AssetKnownnessRepository{pool: pool}
}

const knownnessColumns = `chain, contract, status, source, override, attempts, symbol`

// Get returns the stored record for an identity, or nil when it has never been
// seen. A miss is not an error: the caller's response to "never seen" is to
// enqueue it, not to fail.
func (r *AssetKnownnessRepository) Get(ctx context.Context, key sync.AssetKey) (*sync.KnownnessRecord, error) {
	if !key.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidAssetKey, key.String())
	}

	const query = `SELECT ` + knownnessColumns + `
		FROM asset_knownness WHERE chain = $1 AND contract = $2`

	var (
		out    sync.KnownnessRecord
		status string
		source string
	)
	err := r.pool.QueryRow(ctx, query, key.Chain, key.Contract).Scan(
		&out.Key.Chain, &out.Key.Contract, &status, &source,
		&out.Override, &out.Attempts, &out.Symbol,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read knownness for %s: %w", key.String(), err)
	}
	out.Status = sync.KnownnessStatus(status)
	out.Source = sync.KnownnessSource(source)
	return &out, nil
}

// Enqueue registers an identity for background probing.
//
// ON CONFLICT DO NOTHING, so it is idempotent and — more importantly — it never
// overwrites an existing verdict. The sync path calls this on every sighting of
// an unfamiliar asset, which for a busy wallet means constantly; resetting the
// row would restart the retry ladder forever and no identity would ever reach a
// terminal verdict.
func (r *AssetKnownnessRepository) Enqueue(ctx context.Context, key sync.AssetKey, symbol string) error {
	if !key.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidAssetKey, key.String())
	}

	const query = `
		INSERT INTO asset_knownness (chain, contract, symbol)
		VALUES ($1, $2, $3)
		ON CONFLICT (chain, contract) DO NOTHING
	`
	if _, err := r.pool.Exec(ctx, query, key.Chain, key.Contract, symbol); err != nil {
		return fmt.Errorf("failed to enqueue knownness probe for %s: %w", key.String(), err)
	}
	return nil
}

// ClaimReady locks and returns the next due pending identity, or nil when none
// is due. FOR UPDATE SKIP LOCKED so several workers can drain the queue without
// probing the same identity twice.
func (r *AssetKnownnessRepository) ClaimReady(ctx context.Context) (*sync.KnownnessRecord, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const selectQuery = `
		SELECT ` + knownnessColumns + `
		FROM asset_knownness
		WHERE status = 'pending' AND next_attempt_at <= NOW() AND locked_at IS NULL
		ORDER BY next_attempt_at
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`
	var (
		out    sync.KnownnessRecord
		status string
		source string
	)
	err = tx.QueryRow(ctx, selectQuery).Scan(
		&out.Key.Chain, &out.Key.Contract, &status, &source,
		&out.Override, &out.Attempts, &out.Symbol,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to claim knownness probe: %w", err)
	}
	out.Status = sync.KnownnessStatus(status)
	out.Source = sync.KnownnessSource(source)

	const lockQuery = `UPDATE asset_knownness SET locked_at = NOW()
		WHERE chain = $1 AND contract = $2`
	if _, err := tx.Exec(ctx, lockQuery, out.Key.Chain, out.Key.Contract); err != nil {
		return nil, fmt.Errorf("failed to lock knownness row: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &out, nil
}

// MarkKnown records a terminal "known" verdict from the given level.
func (r *AssetKnownnessRepository) MarkKnown(ctx context.Context, key sync.AssetKey, source sync.KnownnessSource) error {
	const query = `
		UPDATE asset_knownness
		SET status = 'known', source = $3, locked_at = NULL, last_error = NULL, updated_at = now()
		WHERE chain = $1 AND contract = $2
	`
	if _, err := r.pool.Exec(ctx, query, key.Chain, key.Contract, string(source)); err != nil {
		return fmt.Errorf("failed to mark %s known: %w", key.String(), err)
	}
	return nil
}

// MarkUnknown records the TERMINAL "unknown" verdict. Only the worker calls it,
// and only once the retry ladder is exhausted — never on a single failure.
func (r *AssetKnownnessRepository) MarkUnknown(ctx context.Context, key sync.AssetKey, attempts int, lastError string) error {
	const query = `
		UPDATE asset_knownness
		SET status = 'unknown', source = 'quotable', attempts = $3, last_error = $4,
		    locked_at = NULL, updated_at = now()
		WHERE chain = $1 AND contract = $2
	`
	if _, err := r.pool.Exec(ctx, query, key.Chain, key.Contract, attempts, lastError); err != nil {
		return fmt.Errorf("failed to mark %s unknown: %w", key.String(), err)
	}
	return nil
}

// Reschedule records a COUNTED failed attempt: the identity stays pending, the
// attempt count advances, and the next probe is pushed out by the backoff.
func (r *AssetKnownnessRepository) Reschedule(ctx context.Context, key sync.AssetKey, attempts int, nextAttemptAt time.Time, lastError string) error {
	const query = `
		UPDATE asset_knownness
		SET attempts = $3, next_attempt_at = $4, last_error = $5,
		    locked_at = NULL, updated_at = now()
		WHERE chain = $1 AND contract = $2
	`
	if _, err := r.pool.Exec(ctx, query, key.Chain, key.Contract, attempts, nextAttemptAt, lastError); err != nil {
		return fmt.Errorf("failed to reschedule %s: %w", key.String(), err)
	}
	return nil
}

// UnlockWithoutCounting releases the row WITHOUT advancing attempts.
//
// This is the mechanism that keeps a provider outage from convicting real
// tokens: a rate limit or a network error costs nothing but time. The identity
// stays exactly as far from a terminal verdict as it was before.
func (r *AssetKnownnessRepository) UnlockWithoutCounting(ctx context.Context, key sync.AssetKey, nextAttemptAt time.Time) error {
	const query = `
		UPDATE asset_knownness
		SET next_attempt_at = $3, locked_at = NULL, updated_at = now()
		WHERE chain = $1 AND contract = $2
	`
	if _, err := r.pool.Exec(ctx, query, key.Chain, key.Contract, nextAttemptAt); err != nil {
		return fmt.Errorf("failed to unlock %s: %w", key.String(), err)
	}
	return nil
}

// SetOverride records or clears a manual verdict (level 3).
//
// Passing nil clears the override, which restores whatever automatic verdict is
// underneath rather than re-probing — the override column is deliberately
// separate from `status` so the machine's opinion survives a human's.
func (r *AssetKnownnessRepository) SetOverride(ctx context.Context, key sync.AssetKey, override *bool) error {
	if !key.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidAssetKey, key.String())
	}
	const query = `
		INSERT INTO asset_knownness (chain, contract, override)
		VALUES ($1, $2, $3)
		ON CONFLICT (chain, contract) DO UPDATE SET override = EXCLUDED.override, updated_at = now()
	`
	if _, err := r.pool.Exec(ctx, query, key.Chain, key.Contract, override); err != nil {
		return fmt.Errorf("failed to set override for %s: %w", key.String(), err)
	}
	return nil
}

// ReapStale releases rows whose lock outlived the worker that took it, so a
// crashed probe does not park an identity forever.
func (r *AssetKnownnessRepository) ReapStale(ctx context.Context, staleAfter time.Duration) (int, error) {
	const query = `
		UPDATE asset_knownness
		SET locked_at = NULL
		WHERE status = 'pending' AND locked_at < NOW() - $1::interval
	`
	tag, err := r.pool.Exec(ctx, query, fmt.Sprintf("%d seconds", int(staleAfter.Seconds())))
	if err != nil {
		return 0, fmt.Errorf("failed to reap stale knownness locks: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
