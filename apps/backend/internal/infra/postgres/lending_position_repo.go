package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kislikjeka/moontrack/internal/platform/lendingposition"
)

type LendingPositionRepo struct {
	pool *pgxpool.Pool
}

func NewLendingPositionRepo(pool *pgxpool.Pool) *LendingPositionRepo {
	return &LendingPositionRepo{pool: pool}
}

func (r *LendingPositionRepo) Create(ctx context.Context, pos *lendingposition.LendingPosition) error {
	query := `
		INSERT INTO lending_positions (
			id, user_id, wallet_id, chain_id, protocol,
			interest_earned_usd,
			status, opened_at, closed_at,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6,
			$7, $8, $9,
			$10, $11
		)
	`

	_, err := r.pool.Exec(ctx, query,
		pos.ID, pos.UserID, pos.WalletID, pos.ChainID, pos.Protocol,
		pos.InterestEarnedUSD.String(),
		string(pos.Status), pos.OpenedAt, pos.ClosedAt,
		pos.CreatedAt, pos.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert lending_position: %w", err)
	}
	return nil
}

func (r *LendingPositionRepo) Update(ctx context.Context, pos *lendingposition.LendingPosition) error {
	query := `
		UPDATE lending_positions SET
			interest_earned_usd = $1,
			status = $2,
			closed_at = $3,
			updated_at = $4
		WHERE id = $5
	`

	_, err := r.pool.Exec(ctx, query,
		pos.InterestEarnedUSD.String(),
		string(pos.Status), pos.ClosedAt,
		pos.UpdatedAt, pos.ID,
	)
	if err != nil {
		return fmt.Errorf("update lending_position: %w", err)
	}
	return nil
}

func (r *LendingPositionRepo) UpsertAsset(ctx context.Context, asset *lendingposition.LendingPositionAsset) error {
	// The conflict target is (position_id, side, asset) with asset as a registry
	// UUID, so two same-ticker tokens no longer collide onto one row.
	query := `
		INSERT INTO lending_position_assets (
			id, position_id, side, asset,
			amount,
			total_in, total_out,
			total_in_usd, total_out_usd,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4,
			$5,
			$6, $7,
			$8, $9,
			$10, $11
		)
		ON CONFLICT (position_id, side, asset) DO UPDATE SET
			amount = $5,
			total_in = $6,
			total_out = $7,
			total_in_usd = $8,
			total_out_usd = $9,
			updated_at = $11
	`

	_, err := r.pool.Exec(ctx, query,
		asset.ID, asset.PositionID, asset.Side, asset.Asset,
		asset.Amount.String(),
		asset.TotalIn.String(), asset.TotalOut.String(),
		asset.TotalInUSD.String(), asset.TotalOutUSD.String(),
		asset.CreatedAt, asset.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert lending_position_asset: %w", err)
	}
	return nil
}

const lendingSelectColumns = `
	id, user_id, wallet_id, chain_id, protocol,
	interest_earned_usd,
	status, opened_at, closed_at,
	created_at, updated_at
`

const lendingAssetSelectColumns = `
	id, position_id, side, asset,
	amount,
	total_in, total_out,
	total_in_usd, total_out_usd,
	created_at, updated_at
`

func (r *LendingPositionRepo) GetByID(ctx context.Context, id uuid.UUID) (*lendingposition.LendingPosition, error) {
	query := `SELECT ` + lendingSelectColumns + ` FROM lending_positions WHERE id = $1`

	pos, err := r.scanOnePosition(r.pool.QueryRow(ctx, query, id))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get lending_position by id: %w", err)
	}

	assets, err := r.loadAssetsForPositions(ctx, []uuid.UUID{pos.ID})
	if err != nil {
		return nil, err
	}
	pos.Assets = assets[pos.ID]

	return pos, nil
}

func (r *LendingPositionRepo) FindActiveByWalletProtocolChain(ctx context.Context, walletID uuid.UUID, protocol, chainID string) (*lendingposition.LendingPosition, error) {
	query := `SELECT ` + lendingSelectColumns + `
		FROM lending_positions
		WHERE wallet_id = $1 AND protocol = $2 AND chain_id = $3 AND status = 'active'
		LIMIT 1`

	pos, err := r.scanOnePosition(r.pool.QueryRow(ctx, query, walletID, protocol, chainID))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("find active lending_position: %w", err)
	}

	assets, err := r.loadAssetsForPositions(ctx, []uuid.UUID{pos.ID})
	if err != nil {
		return nil, err
	}
	pos.Assets = assets[pos.ID]

	return pos, nil
}

func (r *LendingPositionRepo) ListByUser(ctx context.Context, userID uuid.UUID, status *lendingposition.Status, walletID *uuid.UUID, chainID *string) ([]*lendingposition.LendingPosition, error) {
	query := `SELECT ` + lendingSelectColumns + ` FROM lending_positions WHERE user_id = $1`
	args := []any{userID}
	argPos := 2

	if status != nil {
		query += fmt.Sprintf(" AND status = $%d", argPos)
		args = append(args, string(*status))
		argPos++
	}
	if walletID != nil {
		query += fmt.Sprintf(" AND wallet_id = $%d", argPos)
		args = append(args, *walletID)
		argPos++
	}
	if chainID != nil {
		query += fmt.Sprintf(" AND chain_id = $%d", argPos)
		args = append(args, *chainID)
		argPos++
	}

	query += " ORDER BY opened_at DESC"

	positions, err := r.scanManyPositions(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	if len(positions) == 0 {
		return positions, nil
	}

	// Batch-load all assets for returned positions
	posIDs := make([]uuid.UUID, len(positions))
	for i, p := range positions {
		posIDs[i] = p.ID
	}

	assetsMap, err := r.loadAssetsForPositions(ctx, posIDs)
	if err != nil {
		return nil, err
	}

	for _, p := range positions {
		p.Assets = assetsMap[p.ID]
	}

	return positions, nil
}

// loadAssetsForPositions batch-loads all assets for the given position IDs.
func (r *LendingPositionRepo) loadAssetsForPositions(ctx context.Context, posIDs []uuid.UUID) (map[uuid.UUID][]lendingposition.LendingPositionAsset, error) {
	if len(posIDs) == 0 {
		return nil, nil
	}

	// ORDER BY asset now sorts by registry UUID, not by ticker. That ordering is
	// arbitrary to a reader but stable across calls, which is all this needs:
	// it exists so a position's assets come back in the same order every time,
	// and any presentational ordering belongs to whoever renders the symbols.
	query := `SELECT ` + lendingAssetSelectColumns + `
		FROM lending_position_assets
		WHERE position_id = ANY($1)
		ORDER BY side, asset`

	rows, err := r.pool.Query(ctx, query, posIDs)
	if err != nil {
		return nil, fmt.Errorf("query lending_position_assets: %w", err)
	}
	defer rows.Close()

	result := make(map[uuid.UUID][]lendingposition.LendingPositionAsset)
	for rows.Next() {
		a, err := r.scanOneAsset(rows)
		if err != nil {
			return nil, fmt.Errorf("scan lending_position_asset: %w", err)
		}
		result[a.PositionID] = append(result[a.PositionID], *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lending_position_assets: %w", err)
	}

	return result, nil
}

func (r *LendingPositionRepo) scanOnePosition(row pgx.Row) (*lendingposition.LendingPosition, error) {
	var pos lendingposition.LendingPosition
	var status string
	var interestEarnedUSD string

	err := row.Scan(
		&pos.ID, &pos.UserID, &pos.WalletID, &pos.ChainID, &pos.Protocol,
		&interestEarnedUSD,
		&status, &pos.OpenedAt, &pos.ClosedAt,
		&pos.CreatedAt, &pos.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	pos.Status = lendingposition.Status(status)
	pos.InterestEarnedUSD = parseBigInt(interestEarnedUSD)

	return &pos, nil
}

func (r *LendingPositionRepo) scanOneAsset(row pgx.Row) (*lendingposition.LendingPositionAsset, error) {
	var a lendingposition.LendingPositionAsset
	var amount, totalIn, totalOut, totalInUSD, totalOutUSD string

	err := row.Scan(
		&a.ID, &a.PositionID, &a.Side, &a.Asset,
		&amount,
		&totalIn, &totalOut,
		&totalInUSD, &totalOutUSD,
		&a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	a.Amount = parseBigInt(amount)
	a.TotalIn = parseBigInt(totalIn)
	a.TotalOut = parseBigInt(totalOut)
	a.TotalInUSD = parseBigInt(totalInUSD)
	a.TotalOutUSD = parseBigInt(totalOutUSD)

	return &a, nil
}

func (r *LendingPositionRepo) scanManyPositions(ctx context.Context, query string, args ...any) ([]*lendingposition.LendingPosition, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query lending_positions: %w", err)
	}
	defer rows.Close()

	var positions []*lendingposition.LendingPosition
	for rows.Next() {
		pos, err := r.scanOnePosition(rows)
		if err != nil {
			return nil, fmt.Errorf("scan lending_position: %w", err)
		}
		positions = append(positions, pos)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lending_positions: %w", err)
	}
	return positions, nil
}
