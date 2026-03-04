package postgres

import (
	"context"
	"database/sql"
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
			supply_asset, supply_amount, supply_decimals, supply_contract,
			borrow_asset, borrow_amount, borrow_decimals, borrow_contract,
			total_supplied, total_withdrawn, total_borrowed, total_repaid,
			total_supplied_usd, total_withdrawn_usd, total_borrowed_usd, total_repaid_usd,
			interest_earned_usd,
			status, opened_at, closed_at,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9,
			$10, $11, $12, $13,
			$14, $15, $16, $17,
			$18, $19, $20, $21,
			$22,
			$23, $24, $25,
			$26, $27
		)
	`

	supplyContract := sql.NullString{String: pos.SupplyContract, Valid: pos.SupplyContract != ""}
	borrowAsset := sql.NullString{String: pos.BorrowAsset, Valid: pos.BorrowAsset != ""}
	borrowContract := sql.NullString{String: pos.BorrowContract, Valid: pos.BorrowContract != ""}

	_, err := r.pool.Exec(ctx, query,
		pos.ID, pos.UserID, pos.WalletID, pos.ChainID, pos.Protocol,
		pos.SupplyAsset, pos.SupplyAmount.String(), pos.SupplyDecimals, supplyContract,
		borrowAsset, pos.BorrowAmount.String(), pos.BorrowDecimals, borrowContract,
		pos.TotalSupplied.String(), pos.TotalWithdrawn.String(), pos.TotalBorrowed.String(), pos.TotalRepaid.String(),
		pos.TotalSuppliedUSD.String(), pos.TotalWithdrawnUSD.String(), pos.TotalBorrowedUSD.String(), pos.TotalRepaidUSD.String(),
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
			supply_amount = $1, borrow_asset = $2, borrow_amount = $3,
			borrow_decimals = $4, borrow_contract = $5,
			total_supplied = $6, total_withdrawn = $7, total_borrowed = $8, total_repaid = $9,
			total_supplied_usd = $10, total_withdrawn_usd = $11, total_borrowed_usd = $12, total_repaid_usd = $13,
			interest_earned_usd = $14,
			status = $15, closed_at = $16,
			updated_at = $17
		WHERE id = $18
	`

	borrowAsset := sql.NullString{String: pos.BorrowAsset, Valid: pos.BorrowAsset != ""}
	borrowContract := sql.NullString{String: pos.BorrowContract, Valid: pos.BorrowContract != ""}

	_, err := r.pool.Exec(ctx, query,
		pos.SupplyAmount.String(), borrowAsset, pos.BorrowAmount.String(),
		pos.BorrowDecimals, borrowContract,
		pos.TotalSupplied.String(), pos.TotalWithdrawn.String(), pos.TotalBorrowed.String(), pos.TotalRepaid.String(),
		pos.TotalSuppliedUSD.String(), pos.TotalWithdrawnUSD.String(), pos.TotalBorrowedUSD.String(), pos.TotalRepaidUSD.String(),
		pos.InterestEarnedUSD.String(),
		string(pos.Status), pos.ClosedAt,
		pos.UpdatedAt, pos.ID,
	)
	if err != nil {
		return fmt.Errorf("update lending_position: %w", err)
	}
	return nil
}

const lendingSelectColumns = `
	id, user_id, wallet_id, chain_id, protocol,
	supply_asset, supply_amount, supply_decimals, supply_contract,
	borrow_asset, borrow_amount, borrow_decimals, borrow_contract,
	total_supplied, total_withdrawn, total_borrowed, total_repaid,
	total_supplied_usd, total_withdrawn_usd, total_borrowed_usd, total_repaid_usd,
	interest_earned_usd,
	status, opened_at, closed_at,
	created_at, updated_at
`

func (r *LendingPositionRepo) GetByID(ctx context.Context, id uuid.UUID) (*lendingposition.LendingPosition, error) {
	query := `SELECT ` + lendingSelectColumns + ` FROM lending_positions WHERE id = $1`

	pos, err := r.scanOneLending(r.pool.QueryRow(ctx, query, id))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get lending_position by id: %w", err)
	}
	return pos, nil
}

func (r *LendingPositionRepo) FindActiveByWalletAndAsset(ctx context.Context, walletID uuid.UUID, protocol, chainID, supplyAsset, borrowAsset string) (*lendingposition.LendingPosition, error) {
	query := `SELECT ` + lendingSelectColumns + `
		FROM lending_positions
		WHERE wallet_id = $1 AND protocol = $2 AND chain_id = $3 AND supply_asset = $4 AND status = 'active'`
	args := []any{walletID, protocol, chainID, supplyAsset}

	if borrowAsset != "" {
		query += ` AND borrow_asset = $5`
		args = append(args, borrowAsset)
	}

	query += ` LIMIT 1`

	pos, err := r.scanOneLending(r.pool.QueryRow(ctx, query, args...))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("find active lending_position: %w", err)
	}
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

	return r.scanManyLending(ctx, query, args...)
}

func (r *LendingPositionRepo) scanOneLending(row pgx.Row) (*lendingposition.LendingPosition, error) {
	var pos lendingposition.LendingPosition
	var supplyContract, borrowAsset, borrowContract sql.NullString
	var status string

	var supplyAmount, borrowAmount string
	var totalSupplied, totalWithdrawn, totalBorrowed, totalRepaid string
	var totalSuppliedUSD, totalWithdrawnUSD, totalBorrowedUSD, totalRepaidUSD string
	var interestEarnedUSD string

	err := row.Scan(
		&pos.ID, &pos.UserID, &pos.WalletID, &pos.ChainID, &pos.Protocol,
		&pos.SupplyAsset, &supplyAmount, &pos.SupplyDecimals, &supplyContract,
		&borrowAsset, &borrowAmount, &pos.BorrowDecimals, &borrowContract,
		&totalSupplied, &totalWithdrawn, &totalBorrowed, &totalRepaid,
		&totalSuppliedUSD, &totalWithdrawnUSD, &totalBorrowedUSD, &totalRepaidUSD,
		&interestEarnedUSD,
		&status, &pos.OpenedAt, &pos.ClosedAt,
		&pos.CreatedAt, &pos.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if supplyContract.Valid {
		pos.SupplyContract = supplyContract.String
	}
	if borrowAsset.Valid {
		pos.BorrowAsset = borrowAsset.String
	}
	if borrowContract.Valid {
		pos.BorrowContract = borrowContract.String
	}

	pos.Status = lendingposition.Status(status)

	pos.SupplyAmount = parseBigInt(supplyAmount)
	pos.BorrowAmount = parseBigInt(borrowAmount)
	pos.TotalSupplied = parseBigInt(totalSupplied)
	pos.TotalWithdrawn = parseBigInt(totalWithdrawn)
	pos.TotalBorrowed = parseBigInt(totalBorrowed)
	pos.TotalRepaid = parseBigInt(totalRepaid)
	pos.TotalSuppliedUSD = parseBigInt(totalSuppliedUSD)
	pos.TotalWithdrawnUSD = parseBigInt(totalWithdrawnUSD)
	pos.TotalBorrowedUSD = parseBigInt(totalBorrowedUSD)
	pos.TotalRepaidUSD = parseBigInt(totalRepaidUSD)
	pos.InterestEarnedUSD = parseBigInt(interestEarnedUSD)

	return &pos, nil
}

func (r *LendingPositionRepo) scanManyLending(ctx context.Context, query string, args ...any) ([]*lendingposition.LendingPosition, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query lending_positions: %w", err)
	}
	defer rows.Close()

	var positions []*lendingposition.LendingPosition
	for rows.Next() {
		pos, err := r.scanOneLending(rows)
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

