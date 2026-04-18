package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kislikjeka/moontrack/internal/ledger"
)

// TaxLotRepository implements ledger.TaxLotRepository using PostgreSQL.
type TaxLotRepository struct {
	pool *pgxpool.Pool
}

// NewTaxLotRepository creates a new PostgreSQL tax lot repository.
func NewTaxLotRepository(pool *pgxpool.Pool) *TaxLotRepository {
	return &TaxLotRepository{pool: pool}
}

// getTxFromContext retrieves the transaction from context if one exists.
// Uses the same txContextKey as LedgerRepository (both live in package postgres).
func (r *TaxLotRepository) getTxFromContext(ctx context.Context) pgx.Tx {
	if tx, ok := ctx.Value(txContextKey).(pgx.Tx); ok {
		return tx
	}
	return nil
}

// getQueryer returns the transaction if one exists in context, otherwise returns the pool.
func (r *TaxLotRepository) getQueryer(ctx context.Context) interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
} {
	if tx := r.getTxFromContext(ctx); tx != nil {
		return tx
	}
	return r.pool
}

// ---------------------------------------------------------------------------
// Tax Lot CRUD
// ---------------------------------------------------------------------------

// CreateTaxLot inserts a new tax lot row.
func (r *TaxLotRepository) CreateTaxLot(ctx context.Context, lot *ledger.TaxLot) error {
	query := `
		INSERT INTO tax_lots (
			id, transaction_id, account_id, asset,
			quantity_acquired, quantity_remaining, acquired_at,
			auto_cost_basis_per_unit, auto_cost_basis_source,
			override_cost_basis_per_unit, override_reason, override_at,
			linked_source_lot_id, created_at,
			price_status, price_resolution_attempts, price_next_retry_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
	`

	// Nullable *big.Int -> *string
	var autoCost *string
	if lot.AutoCostBasisPerUnit != nil {
		s := lot.AutoCostBasisPerUnit.String()
		autoCost = &s
	}

	var overrideCost *string
	if lot.OverrideCostBasisPerUnit != nil {
		s := lot.OverrideCostBasisPerUnit.String()
		overrideCost = &s
	}

	// Default price_status to 'resolved' when not explicitly set,
	// so existing callers that don't set PriceStatus continue to work.
	priceStatus := lot.PriceStatus
	if priceStatus == "" {
		priceStatus = ledger.PriceStatusResolved
	}

	q := r.getQueryer(ctx)
	_, err := q.Exec(ctx, query,
		lot.ID,
		lot.TransactionID,
		lot.AccountID,
		lot.Asset,
		lot.QuantityAcquired.String(),
		lot.QuantityRemaining.String(),
		lot.AcquiredAt,
		autoCost,
		string(lot.AutoCostBasisSource),
		overrideCost,
		lot.OverrideReason,
		lot.OverrideAt,
		lot.LinkedSourceLotID,
		lot.CreatedAt,
		string(priceStatus),
		lot.PriceResolutionAttempts,
		lot.PriceNextRetryAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create tax lot: %w", err)
	}
	return nil
}

// GetTaxLot retrieves a single tax lot by ID.
func (r *TaxLotRepository) GetTaxLot(ctx context.Context, id uuid.UUID) (*ledger.TaxLot, error) {
	query := `
		SELECT id, transaction_id, account_id, asset,
		       quantity_acquired, quantity_remaining, acquired_at,
		       auto_cost_basis_per_unit, auto_cost_basis_source,
		       override_cost_basis_per_unit, override_reason, override_at,
		       linked_source_lot_id, created_at,
		       price_status, price_resolution_attempts, price_next_retry_at
		FROM tax_lots
		WHERE id = $1
	`

	q := r.getQueryer(ctx)
	row := q.QueryRow(ctx, query, id)

	lot, err := r.scanTaxLot(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ledger.ErrLotNotFound
		}
		return nil, fmt.Errorf("failed to get tax lot: %w", err)
	}
	return lot, nil
}

// GetTaxLotForUpdate retrieves a single tax lot by ID with a row-level lock (FOR UPDATE).
// Must be called within a transaction context.
func (r *TaxLotRepository) GetTaxLotForUpdate(ctx context.Context, id uuid.UUID) (*ledger.TaxLot, error) {
	query := `
		SELECT id, transaction_id, account_id, asset,
		       quantity_acquired, quantity_remaining, acquired_at,
		       auto_cost_basis_per_unit, auto_cost_basis_source,
		       override_cost_basis_per_unit, override_reason, override_at,
		       linked_source_lot_id, created_at,
		       price_status, price_resolution_attempts, price_next_retry_at
		FROM tax_lots
		WHERE id = $1
		FOR UPDATE
	`

	q := r.getQueryer(ctx)
	row := q.QueryRow(ctx, query, id)

	lot, err := r.scanTaxLot(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ledger.ErrLotNotFound
		}
		return nil, fmt.Errorf("failed to get tax lot for update: %w", err)
	}
	return lot, nil
}

// GetOpenLotsFIFO returns all open lots for an account+asset ordered oldest-first,
// with SELECT ... FOR UPDATE to prevent concurrent consumption.
func (r *TaxLotRepository) GetOpenLotsFIFO(ctx context.Context, accountID uuid.UUID, asset string) ([]*ledger.TaxLot, error) {
	query := `
		SELECT id, transaction_id, account_id, asset,
		       quantity_acquired, quantity_remaining, acquired_at,
		       auto_cost_basis_per_unit, auto_cost_basis_source,
		       override_cost_basis_per_unit, override_reason, override_at,
		       linked_source_lot_id, created_at,
		       price_status, price_resolution_attempts, price_next_retry_at
		FROM tax_lots
		WHERE account_id = $1 AND asset = $2 AND quantity_remaining > 0
		ORDER BY acquired_at ASC, created_at ASC, id ASC
		FOR UPDATE
	`

	q := r.getQueryer(ctx)
	rows, err := q.Query(ctx, query, accountID, asset)
	if err != nil {
		return nil, fmt.Errorf("failed to query open lots FIFO: %w", err)
	}
	defer rows.Close()

	return r.collectTaxLots(rows)
}

// UpdateLotRemaining sets the quantity_remaining for a lot.
func (r *TaxLotRepository) UpdateLotRemaining(ctx context.Context, lotID uuid.UUID, newRemaining *big.Int) error {
	query := `
		UPDATE tax_lots
		SET quantity_remaining = $1
		WHERE id = $2
	`

	q := r.getQueryer(ctx)
	tag, err := q.Exec(ctx, query, newRemaining.String(), lotID)
	if err != nil {
		return fmt.Errorf("failed to update lot remaining: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("tax lot not found: %w", pgx.ErrNoRows)
	}
	return nil
}

// GetLotsByAccount returns all lots for an account+asset ordered by acquired_at.
func (r *TaxLotRepository) GetLotsByAccount(ctx context.Context, accountID uuid.UUID, asset string) ([]*ledger.TaxLot, error) {
	query := `
		SELECT id, transaction_id, account_id, asset,
		       quantity_acquired, quantity_remaining, acquired_at,
		       auto_cost_basis_per_unit, auto_cost_basis_source,
		       override_cost_basis_per_unit, override_reason, override_at,
		       linked_source_lot_id, created_at,
		       price_status, price_resolution_attempts, price_next_retry_at
		FROM tax_lots
		WHERE account_id = $1 AND asset = $2
		ORDER BY acquired_at ASC, created_at ASC, id ASC
	`

	q := r.getQueryer(ctx)
	rows, err := q.Query(ctx, query, accountID, asset)
	if err != nil {
		return nil, fmt.Errorf("failed to query lots by account: %w", err)
	}
	defer rows.Close()

	return r.collectTaxLots(rows)
}

// GetLotsByTransaction returns all lots for a given transaction ordered by acquired_at.
func (r *TaxLotRepository) GetLotsByTransaction(ctx context.Context, txID uuid.UUID) ([]*ledger.TaxLot, error) {
	query := `
		SELECT id, transaction_id, account_id, asset,
		       quantity_acquired, quantity_remaining, acquired_at,
		       auto_cost_basis_per_unit, auto_cost_basis_source,
		       override_cost_basis_per_unit, override_reason, override_at,
		       linked_source_lot_id, created_at,
		       price_status, price_resolution_attempts, price_next_retry_at
		FROM tax_lots
		WHERE transaction_id = $1
		ORDER BY acquired_at ASC, created_at ASC, id ASC
	`

	q := r.getQueryer(ctx)
	rows, err := q.Query(ctx, query, txID)
	if err != nil {
		return nil, fmt.Errorf("failed to query lots by transaction: %w", err)
	}
	defer rows.Close()

	return r.collectTaxLots(rows)
}

// ---------------------------------------------------------------------------
// Disposal CRUD
// ---------------------------------------------------------------------------

// CreateDisposal inserts a new lot disposal row.
func (r *TaxLotRepository) CreateDisposal(ctx context.Context, d *ledger.LotDisposal) error {
	query := `
		INSERT INTO lot_disposals (
			id, transaction_id, lot_id,
			quantity_disposed, proceeds_per_unit, disposal_type,
			disposed_at, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`

	q := r.getQueryer(ctx)
	_, err := q.Exec(ctx, query,
		d.ID,
		d.TransactionID,
		d.LotID,
		d.QuantityDisposed.String(),
		d.ProceedsPerUnit.String(),
		string(d.DisposalType),
		d.DisposedAt,
		d.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create disposal: %w", err)
	}
	return nil
}

// GetDisposalsByTransaction returns all disposals for a given transaction.
func (r *TaxLotRepository) GetDisposalsByTransaction(ctx context.Context, txID uuid.UUID) ([]*ledger.LotDisposal, error) {
	query := `
		SELECT id, transaction_id, lot_id,
		       quantity_disposed, proceeds_per_unit, disposal_type,
		       disposed_at, created_at
		FROM lot_disposals
		WHERE transaction_id = $1
		ORDER BY created_at ASC
	`

	q := r.getQueryer(ctx)
	rows, err := q.Query(ctx, query, txID)
	if err != nil {
		return nil, fmt.Errorf("failed to query disposals by transaction: %w", err)
	}
	defer rows.Close()

	return r.collectDisposals(rows)
}

// GetDisposalsByLot returns all disposals for a given lot.
func (r *TaxLotRepository) GetDisposalsByLot(ctx context.Context, lotID uuid.UUID) ([]*ledger.LotDisposal, error) {
	query := `
		SELECT id, transaction_id, lot_id,
		       quantity_disposed, proceeds_per_unit, disposal_type,
		       disposed_at, created_at
		FROM lot_disposals
		WHERE lot_id = $1
		ORDER BY created_at ASC
	`

	q := r.getQueryer(ctx)
	rows, err := q.Query(ctx, query, lotID)
	if err != nil {
		return nil, fmt.Errorf("failed to query disposals by lot: %w", err)
	}
	defer rows.Close()

	return r.collectDisposals(rows)
}

// ---------------------------------------------------------------------------
// Override management
// ---------------------------------------------------------------------------

// UpdateOverride sets the cost-basis override on a lot.
func (r *TaxLotRepository) UpdateOverride(ctx context.Context, lotID uuid.UUID, costBasis *big.Int, reason string) error {
	query := `
		UPDATE tax_lots
		SET override_cost_basis_per_unit = $1,
		    override_reason = $2,
		    override_at = $3
		WHERE id = $4
	`

	now := time.Now().UTC()
	q := r.getQueryer(ctx)
	tag, err := q.Exec(ctx, query, costBasis.String(), reason, now, lotID)
	if err != nil {
		return fmt.Errorf("failed to update override: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("tax lot not found: %w", pgx.ErrNoRows)
	}
	return nil
}

// ClearOverride removes the cost-basis override from a lot.
func (r *TaxLotRepository) ClearOverride(ctx context.Context, lotID uuid.UUID) error {
	query := `
		UPDATE tax_lots
		SET override_cost_basis_per_unit = NULL,
		    override_reason = NULL,
		    override_at = NULL
		WHERE id = $1
	`

	q := r.getQueryer(ctx)
	tag, err := q.Exec(ctx, query, lotID)
	if err != nil {
		return fmt.Errorf("failed to clear override: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("tax lot not found: %w", pgx.ErrNoRows)
	}
	return nil
}

// CreateOverrideHistory inserts a row into the override audit trail.
func (r *TaxLotRepository) CreateOverrideHistory(ctx context.Context, h *ledger.LotOverrideHistory) error {
	query := `
		INSERT INTO lot_override_history (
			id, lot_id, previous_cost_basis, new_cost_basis, reason, changed_at
		) VALUES ($1,$2,$3,$4,$5,$6)
	`

	// Nullable *big.Int -> *string
	var prevCost *string
	if h.PreviousCostBasis != nil {
		s := h.PreviousCostBasis.String()
		prevCost = &s
	}

	q := r.getQueryer(ctx)
	_, err := q.Exec(ctx, query,
		h.ID,
		h.LotID,
		prevCost,
		h.NewCostBasis.String(),
		h.Reason,
		h.ChangedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create override history: %w", err)
	}
	return nil
}

// GetOverrideHistory returns the full override audit trail for a lot.
func (r *TaxLotRepository) GetOverrideHistory(ctx context.Context, lotID uuid.UUID) ([]*ledger.LotOverrideHistory, error) {
	query := `
		SELECT id, lot_id, previous_cost_basis, new_cost_basis, reason, changed_at
		FROM lot_override_history
		WHERE lot_id = $1
		ORDER BY changed_at ASC
	`

	q := r.getQueryer(ctx)
	rows, err := q.Query(ctx, query, lotID)
	if err != nil {
		return nil, fmt.Errorf("failed to query override history: %w", err)
	}
	defer rows.Close()

	var history []*ledger.LotOverrideHistory
	for rows.Next() {
		var h ledger.LotOverrideHistory
		var prevCostStr sql.NullString
		var newCostStr string

		err := rows.Scan(
			&h.ID,
			&h.LotID,
			&prevCostStr,
			&newCostStr,
			&h.Reason,
			&h.ChangedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan override history: %w", err)
		}

		// Parse nullable previous cost basis
		if prevCostStr.Valid {
			v, ok := new(big.Int).SetString(prevCostStr.String, 10)
			if !ok {
				return nil, fmt.Errorf("failed to parse previous_cost_basis: %s", prevCostStr.String)
			}
			h.PreviousCostBasis = v
		}

		// Parse new cost basis
		v, ok := new(big.Int).SetString(newCostStr, 10)
		if !ok {
			return nil, fmt.Errorf("failed to parse new_cost_basis: %s", newCostStr)
		}
		h.NewCostBasis = v

		history = append(history, &h)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating override history: %w", err)
	}

	return history, nil
}

// ---------------------------------------------------------------------------
// WAC (weighted average cost)
// ---------------------------------------------------------------------------

// RefreshWAC refreshes the position_wac materialized view concurrently.
func (r *TaxLotRepository) RefreshWAC(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, "REFRESH MATERIALIZED VIEW CONCURRENTLY position_wac")
	if err != nil {
		return fmt.Errorf("failed to refresh position_wac: %w", err)
	}
	return nil
}

// GetWAC returns WAC positions for the given account IDs.
func (r *TaxLotRepository) GetWAC(ctx context.Context, accountIDs []uuid.UUID) ([]*ledger.PositionWAC, error) {
	if len(accountIDs) == 0 {
		return nil, nil
	}

	query := `
		SELECT account_id, asset, total_quantity, weighted_avg_cost
		FROM position_wac
		WHERE account_id = ANY($1)
		ORDER BY asset ASC
	`

	rows, err := r.pool.Query(ctx, query, accountIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to query position_wac: %w", err)
	}
	defer rows.Close()

	var positions []*ledger.PositionWAC
	for rows.Next() {
		var p ledger.PositionWAC
		var totalQtyStr string
		// weighted_avg_cost can be NULL when all lots for the position are
		// in pending price status (auto_cost_basis_per_unit IS NULL, so the
		// view's SUM(qty * effective) evaluates to NULL).
		var wacStr sql.NullString

		if err := rows.Scan(&p.AccountID, &p.Asset, &totalQtyStr, &wacStr); err != nil {
			return nil, fmt.Errorf("failed to scan position_wac row: %w", err)
		}

		totalQty, ok := new(big.Int).SetString(truncateDecimal(totalQtyStr), 10)
		if !ok {
			return nil, fmt.Errorf("failed to parse total_quantity: %s", totalQtyStr)
		}
		p.TotalQuantity = totalQty

		if wacStr.Valid {
			wac, ok := new(big.Int).SetString(truncateDecimal(wacStr.String), 10)
			if !ok {
				return nil, fmt.Errorf("failed to parse weighted_avg_cost: %s", wacStr.String)
			}
			p.WeightedAvgCost = wac
		} else {
			// All lots pending — leave WeightedAvgCost nil so callers can
			// detect the unresolved state.
			p.WeightedAvgCost = nil
		}

		positions = append(positions, &p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating position_wac: %w", err)
	}

	return positions, nil
}

// ---------------------------------------------------------------------------
// Pending-price resolution methods (migration 000025)
// ---------------------------------------------------------------------------

// ListPendingLotsByAssetAndTime returns all lots with price_status='pending'
// for the given asset symbol within the minute bucket containing at.
func (r *TaxLotRepository) ListPendingLotsByAssetAndTime(ctx context.Context, asset string, at time.Time) ([]*ledger.TaxLot, error) {
	minStart := at.UTC().Truncate(time.Minute)
	minEnd := minStart.Add(time.Minute)

	query := `
		SELECT id, transaction_id, account_id, asset,
		       quantity_acquired, quantity_remaining, acquired_at,
		       auto_cost_basis_per_unit, auto_cost_basis_source,
		       override_cost_basis_per_unit, override_reason, override_at,
		       linked_source_lot_id, created_at,
		       price_status, price_resolution_attempts, price_next_retry_at
		FROM tax_lots
		WHERE asset = $1 AND price_status = 'pending'
		  AND acquired_at >= $2 AND acquired_at < $3
	`

	q := r.getQueryer(ctx)
	rows, err := q.Query(ctx, query, asset, minStart, minEnd)
	if err != nil {
		return nil, fmt.Errorf("list pending lots by asset and time: %w", err)
	}
	defer rows.Close()

	return r.collectTaxLots(rows)
}

// ResolvePendingPrice sets auto_cost_basis_per_unit and transitions
// price_status to 'resolved'. Only affects rows where price_status='pending'.
func (r *TaxLotRepository) ResolvePendingPrice(ctx context.Context, lotID uuid.UUID, autoCostBasisPerUnit *big.Int, autoSource ledger.CostBasisSource) error {
	query := `
		UPDATE tax_lots
		SET auto_cost_basis_per_unit = $2,
		    auto_cost_basis_source = $3,
		    price_status = 'resolved',
		    price_next_retry_at = NULL
		WHERE id = $1 AND price_status = 'pending'
	`

	q := r.getQueryer(ctx)
	_, err := q.Exec(ctx, query, lotID, autoCostBasisPerUnit.String(), string(autoSource))
	if err != nil {
		return fmt.Errorf("resolve pending price: %w", err)
	}
	return nil
}

// MarkUnpriceable transitions price_status to 'unpriceable' for a pending lot.
func (r *TaxLotRepository) MarkUnpriceable(ctx context.Context, lotID uuid.UUID) error {
	query := `
		UPDATE tax_lots
		SET price_status = 'unpriceable',
		    price_next_retry_at = NULL
		WHERE id = $1 AND price_status = 'pending'
	`

	q := r.getQueryer(ctx)
	_, err := q.Exec(ctx, query, lotID)
	if err != nil {
		return fmt.Errorf("mark unpriceable: %w", err)
	}
	return nil
}

// MarkResolved transitions price_status to 'resolved' for any lot (pending or unpriceable).
// Used when a manual price is applied by the user via PUT /lots/{id}/manual-price.
func (r *TaxLotRepository) MarkResolved(ctx context.Context, lotID uuid.UUID) error {
	query := `
		UPDATE tax_lots
		SET price_status = 'resolved',
		    price_next_retry_at = NULL
		WHERE id = $1 AND price_status IN ('pending', 'unpriceable')
	`

	q := r.getQueryer(ctx)
	_, err := q.Exec(ctx, query, lotID)
	if err != nil {
		return fmt.Errorf("mark resolved: %w", err)
	}
	return nil
}

// CountLotsByPriceStatus returns the count of lots in 'pending' and 'unpriceable'
// price_status for the given user. The JOIN on accounts enforces multi-tenant isolation.
func (r *TaxLotRepository) CountLotsByPriceStatus(ctx context.Context, userID uuid.UUID) (pending, unpriceable int, err error) {
	query := `
		SELECT tl.price_status, COUNT(*)
		FROM tax_lots tl
		JOIN accounts a ON a.id = tl.account_id
		WHERE a.user_id = $1 AND tl.price_status IN ('pending', 'unpriceable')
		GROUP BY tl.price_status
	`

	q := r.getQueryer(ctx)
	rows, err := q.Query(ctx, query, userID)
	if err != nil {
		return 0, 0, fmt.Errorf("count lots by price status: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return 0, 0, fmt.Errorf("scan count lots by price status: %w", err)
		}
		switch status {
		case "pending":
			pending = count
		case "unpriceable":
			unpriceable = count
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("iterate count lots by price status: %w", err)
	}
	return pending, unpriceable, nil
}

// IncrementAttempt bumps the attempts counter and sets the next-retry time
// for a pending lot.
func (r *TaxLotRepository) IncrementAttempt(ctx context.Context, lotID uuid.UUID, attempts int, nextRetryAt time.Time) error {
	query := `
		UPDATE tax_lots
		SET price_resolution_attempts = $2,
		    price_next_retry_at = $3
		WHERE id = $1 AND price_status = 'pending'
	`

	q := r.getQueryer(ctx)
	_, err := q.Exec(ctx, query, lotID, attempts, nextRetryAt)
	if err != nil {
		return fmt.Errorf("increment attempt: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Internal scan helpers
// ---------------------------------------------------------------------------

// scanTaxLot scans a single tax lot from a pgx.Row.
func (r *TaxLotRepository) scanTaxLot(row pgx.Row) (*ledger.TaxLot, error) {
	var lot ledger.TaxLot
	var qtyAcquiredStr, qtyRemainingStr string
	var autoCostStr sql.NullString // nullable since migration 000025
	var overrideCostStr sql.NullString
	var overrideReason sql.NullString
	var overrideAt sql.NullTime
	var linkedLotID sql.NullString
	var priceNextRetryAt sql.NullTime

	err := row.Scan(
		&lot.ID,
		&lot.TransactionID,
		&lot.AccountID,
		&lot.Asset,
		&qtyAcquiredStr,
		&qtyRemainingStr,
		&lot.AcquiredAt,
		&autoCostStr,
		&lot.AutoCostBasisSource,
		&overrideCostStr,
		&overrideReason,
		&overrideAt,
		&linkedLotID,
		&lot.CreatedAt,
		&lot.PriceStatus,
		&lot.PriceResolutionAttempts,
		&priceNextRetryAt,
	)
	if err != nil {
		return nil, err
	}

	// Parse non-nullable big.Int fields
	qtyAcquired, ok := new(big.Int).SetString(qtyAcquiredStr, 10)
	if !ok {
		return nil, fmt.Errorf("failed to parse quantity_acquired: %s", qtyAcquiredStr)
	}
	lot.QuantityAcquired = qtyAcquired

	qtyRemaining, ok := new(big.Int).SetString(qtyRemainingStr, 10)
	if !ok {
		return nil, fmt.Errorf("failed to parse quantity_remaining: %s", qtyRemainingStr)
	}
	lot.QuantityRemaining = qtyRemaining

	// auto_cost_basis_per_unit is nullable for pending lots
	if autoCostStr.Valid {
		autoCost, ok := new(big.Int).SetString(autoCostStr.String, 10)
		if !ok {
			return nil, fmt.Errorf("failed to parse auto_cost_basis_per_unit: %s", autoCostStr.String)
		}
		lot.AutoCostBasisPerUnit = autoCost
	}

	// Parse other nullable fields
	if overrideCostStr.Valid {
		v, ok := new(big.Int).SetString(overrideCostStr.String, 10)
		if !ok {
			return nil, fmt.Errorf("failed to parse override_cost_basis_per_unit: %s", overrideCostStr.String)
		}
		lot.OverrideCostBasisPerUnit = v
	}

	if overrideReason.Valid {
		lot.OverrideReason = &overrideReason.String
	}

	if overrideAt.Valid {
		lot.OverrideAt = &overrideAt.Time
	}

	if linkedLotID.Valid {
		parsed, err := uuid.Parse(linkedLotID.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse linked_source_lot_id: %w", err)
		}
		lot.LinkedSourceLotID = &parsed
	}

	if priceNextRetryAt.Valid {
		lot.PriceNextRetryAt = &priceNextRetryAt.Time
	}

	return &lot, nil
}

// collectTaxLots iterates rows and returns a slice of tax lots.
func (r *TaxLotRepository) collectTaxLots(rows pgx.Rows) ([]*ledger.TaxLot, error) {
	var lots []*ledger.TaxLot
	for rows.Next() {
		lot, err := r.scanTaxLot(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan tax lot: %w", err)
		}
		lots = append(lots, lot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating tax lots: %w", err)
	}
	return lots, nil
}

// scanDisposal scans a single lot disposal from a pgx.Row.
func (r *TaxLotRepository) scanDisposal(row pgx.Row) (*ledger.LotDisposal, error) {
	var d ledger.LotDisposal
	var qtyDisposedStr, proceedsStr string

	err := row.Scan(
		&d.ID,
		&d.TransactionID,
		&d.LotID,
		&qtyDisposedStr,
		&proceedsStr,
		&d.DisposalType,
		&d.DisposedAt,
		&d.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	qtyDisposed, ok := new(big.Int).SetString(qtyDisposedStr, 10)
	if !ok {
		return nil, fmt.Errorf("failed to parse quantity_disposed: %s", qtyDisposedStr)
	}
	d.QuantityDisposed = qtyDisposed

	proceeds, ok := new(big.Int).SetString(proceedsStr, 10)
	if !ok {
		return nil, fmt.Errorf("failed to parse proceeds_per_unit: %s", proceedsStr)
	}
	d.ProceedsPerUnit = proceeds

	return &d, nil
}

// collectDisposals iterates rows and returns a slice of lot disposals.
func (r *TaxLotRepository) collectDisposals(rows pgx.Rows) ([]*ledger.LotDisposal, error) {
	var disposals []*ledger.LotDisposal
	for rows.Next() {
		d, err := r.scanDisposal(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan disposal: %w", err)
		}
		disposals = append(disposals, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating disposals: %w", err)
	}
	return disposals, nil
}

// truncateDecimal strips any decimal portion from a numeric string.
// PostgreSQL NUMERIC division can produce decimals; we truncate toward zero
// to fit big.Int parsing while preserving integer precision.
func truncateDecimal(s string) string {
	if i := strings.IndexByte(s, '.'); i >= 0 {
		return s[:i]
	}
	return s
}
