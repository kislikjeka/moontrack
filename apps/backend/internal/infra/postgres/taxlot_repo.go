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
			linked_source_lot_id, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
	`

	// Nullable *big.Int -> *string
	var overrideCost *string
	if lot.OverrideCostBasisPerUnit != nil {
		s := lot.OverrideCostBasisPerUnit.String()
		overrideCost = &s
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
		lot.AutoCostBasisPerUnit.String(),
		string(lot.AutoCostBasisSource),
		overrideCost,
		lot.OverrideReason,
		lot.OverrideAt,
		lot.LinkedSourceLotID,
		lot.CreatedAt,
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
		       linked_source_lot_id, created_at
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
		       linked_source_lot_id, created_at
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
		       linked_source_lot_id, created_at
		FROM tax_lots
		WHERE account_id = $1 AND asset = $2 AND quantity_remaining > 0
		ORDER BY acquired_at ASC
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
		       linked_source_lot_id, created_at
		FROM tax_lots
		WHERE account_id = $1 AND asset = $2
		ORDER BY acquired_at ASC
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
		       linked_source_lot_id, created_at
		FROM tax_lots
		WHERE transaction_id = $1
		ORDER BY acquired_at ASC
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
		var totalQtyStr, wacStr string

		if err := rows.Scan(&p.AccountID, &p.Asset, &totalQtyStr, &wacStr); err != nil {
			return nil, fmt.Errorf("failed to scan position_wac row: %w", err)
		}

		totalQty, ok := new(big.Int).SetString(truncateDecimal(totalQtyStr), 10)
		if !ok {
			return nil, fmt.Errorf("failed to parse total_quantity: %s", totalQtyStr)
		}
		p.TotalQuantity = totalQty

		wac, ok := new(big.Int).SetString(truncateDecimal(wacStr), 10)
		if !ok {
			return nil, fmt.Errorf("failed to parse weighted_avg_cost: %s", wacStr)
		}
		p.WeightedAvgCost = wac

		positions = append(positions, &p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating position_wac: %w", err)
	}

	return positions, nil
}

// ---------------------------------------------------------------------------
// Internal scan helpers
// ---------------------------------------------------------------------------

// scanTaxLot scans a single tax lot from a pgx.Row.
func (r *TaxLotRepository) scanTaxLot(row pgx.Row) (*ledger.TaxLot, error) {
	var lot ledger.TaxLot
	var qtyAcquiredStr, qtyRemainingStr string
	var autoCostStr string
	var overrideCostStr sql.NullString
	var overrideReason sql.NullString
	var overrideAt sql.NullTime
	var linkedLotID sql.NullString

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

	autoCost, ok := new(big.Int).SetString(autoCostStr, 10)
	if !ok {
		return nil, fmt.Errorf("failed to parse auto_cost_basis_per_unit: %s", autoCostStr)
	}
	lot.AutoCostBasisPerUnit = autoCost

	// Parse nullable fields
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
