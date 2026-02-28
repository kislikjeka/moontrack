package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kislikjeka/moontrack/internal/platform/lpposition"
)

type LPPositionRepo struct {
	pool *pgxpool.Pool
}

func NewLPPositionRepo(pool *pgxpool.Pool) *LPPositionRepo {
	return &LPPositionRepo{pool: pool}
}

func (r *LPPositionRepo) Create(ctx context.Context, pos *lpposition.LPPosition) error {
	query := `
		INSERT INTO lp_positions (
			id, user_id, wallet_id, chain_id, protocol, nft_token_id, contract_address,
			token0_symbol, token1_symbol, token0_contract, token1_contract, token0_decimals, token1_decimals,
			total_deposited_usd, total_withdrawn_usd, total_claimed_fees_usd,
			total_deposited_token0, total_deposited_token1, total_withdrawn_token0, total_withdrawn_token1,
			total_claimed_token0, total_claimed_token1,
			status, opened_at, closed_at, realized_pnl_usd, apr_bps,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13,
			$14, $15, $16,
			$17, $18, $19, $20,
			$21, $22,
			$23, $24, $25, $26, $27,
			$28, $29
		)
	`

	nftTokenID := sql.NullString{String: pos.NFTTokenID, Valid: pos.NFTTokenID != ""}
	contractAddress := sql.NullString{String: pos.ContractAddress, Valid: pos.ContractAddress != ""}
	var realizedPnL sql.NullString
	if pos.RealizedPnLUSD != nil {
		realizedPnL = sql.NullString{String: pos.RealizedPnLUSD.String(), Valid: true}
	}
	var aprBps sql.NullInt32
	if pos.APRBps != nil {
		aprBps = sql.NullInt32{Int32: int32(*pos.APRBps), Valid: true}
	}

	_, err := r.pool.Exec(ctx, query,
		pos.ID, pos.UserID, pos.WalletID, pos.ChainID, pos.Protocol, nftTokenID, contractAddress,
		pos.Token0Symbol, pos.Token1Symbol, pos.Token0Contract, pos.Token1Contract, pos.Token0Decimals, pos.Token1Decimals,
		pos.TotalDepositedUSD.String(), pos.TotalWithdrawnUSD.String(), pos.TotalClaimedFeesUSD.String(),
		pos.TotalDepositedToken0.String(), pos.TotalDepositedToken1.String(),
		pos.TotalWithdrawnToken0.String(), pos.TotalWithdrawnToken1.String(),
		pos.TotalClaimedToken0.String(), pos.TotalClaimedToken1.String(),
		string(pos.Status), pos.OpenedAt, pos.ClosedAt, realizedPnL, aprBps,
		pos.CreatedAt, pos.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert lp_position: %w", err)
	}
	return nil
}

func (r *LPPositionRepo) Update(ctx context.Context, pos *lpposition.LPPosition) error {
	query := `
		UPDATE lp_positions SET
			total_deposited_usd = $1, total_withdrawn_usd = $2, total_claimed_fees_usd = $3,
			total_deposited_token0 = $4, total_deposited_token1 = $5,
			total_withdrawn_token0 = $6, total_withdrawn_token1 = $7,
			total_claimed_token0 = $8, total_claimed_token1 = $9,
			status = $10, closed_at = $11, realized_pnl_usd = $12, apr_bps = $13,
			updated_at = $14
		WHERE id = $15
	`

	var realizedPnL sql.NullString
	if pos.RealizedPnLUSD != nil {
		realizedPnL = sql.NullString{String: pos.RealizedPnLUSD.String(), Valid: true}
	}
	var aprBps sql.NullInt32
	if pos.APRBps != nil {
		aprBps = sql.NullInt32{Int32: int32(*pos.APRBps), Valid: true}
	}

	_, err := r.pool.Exec(ctx, query,
		pos.TotalDepositedUSD.String(), pos.TotalWithdrawnUSD.String(), pos.TotalClaimedFeesUSD.String(),
		pos.TotalDepositedToken0.String(), pos.TotalDepositedToken1.String(),
		pos.TotalWithdrawnToken0.String(), pos.TotalWithdrawnToken1.String(),
		pos.TotalClaimedToken0.String(), pos.TotalClaimedToken1.String(),
		string(pos.Status), pos.ClosedAt, realizedPnL, aprBps,
		pos.UpdatedAt, pos.ID,
	)
	if err != nil {
		return fmt.Errorf("update lp_position: %w", err)
	}
	return nil
}

const selectColumns = `
	id, user_id, wallet_id, chain_id, protocol, nft_token_id, contract_address,
	token0_symbol, token1_symbol, token0_contract, token1_contract, token0_decimals, token1_decimals,
	total_deposited_usd, total_withdrawn_usd, total_claimed_fees_usd,
	total_deposited_token0, total_deposited_token1, total_withdrawn_token0, total_withdrawn_token1,
	total_claimed_token0, total_claimed_token1,
	status, opened_at, closed_at, realized_pnl_usd, apr_bps,
	created_at, updated_at
`

func (r *LPPositionRepo) GetByID(ctx context.Context, id uuid.UUID) (*lpposition.LPPosition, error) {
	query := `SELECT ` + selectColumns + ` FROM lp_positions WHERE id = $1`

	pos, err := r.scanOne(r.pool.QueryRow(ctx, query, id))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get lp_position by id: %w", err)
	}
	return pos, nil
}

func (r *LPPositionRepo) GetByNFTTokenID(ctx context.Context, walletID uuid.UUID, chainID, nftTokenID string) (*lpposition.LPPosition, error) {
	query := `SELECT ` + selectColumns + ` FROM lp_positions WHERE wallet_id = $1 AND chain_id = $2 AND nft_token_id = $3`

	pos, err := r.scanOne(r.pool.QueryRow(ctx, query, walletID, chainID, nftTokenID))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get lp_position by nft: %w", err)
	}
	return pos, nil
}

func (r *LPPositionRepo) FindOpenByTokenPair(ctx context.Context, walletID uuid.UUID, chainID, protocol, token0, token1 string) ([]*lpposition.LPPosition, error) {
	query := `SELECT ` + selectColumns + `
		FROM lp_positions
		WHERE wallet_id = $1 AND chain_id = $2 AND protocol = $3 AND status = 'open'
		  AND (
			(token0_contract = $4 AND token1_contract = $5) OR
			(token0_contract = $5 AND token1_contract = $4)
		  )
		ORDER BY opened_at ASC
	`

	return r.scanMany(ctx, query, walletID, chainID, protocol, token0, token1)
}

func (r *LPPositionRepo) ListByUser(ctx context.Context, userID uuid.UUID, status *lpposition.Status, walletID *uuid.UUID, chainID *string) ([]*lpposition.LPPosition, error) {
	query := `SELECT ` + selectColumns + ` FROM lp_positions WHERE user_id = $1`
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

	return r.scanMany(ctx, query, args...)
}

func (r *LPPositionRepo) scanOne(row pgx.Row) (*lpposition.LPPosition, error) {
	var pos lpposition.LPPosition
	var nftTokenID, contractAddress sql.NullString
	var token0Contract, token1Contract sql.NullString
	var status string
	var realizedPnL sql.NullString
	var aprBps sql.NullInt32

	var depositedUSD, withdrawnUSD, claimedFeesUSD string
	var depositedT0, depositedT1, withdrawnT0, withdrawnT1, claimedT0, claimedT1 string

	err := row.Scan(
		&pos.ID, &pos.UserID, &pos.WalletID, &pos.ChainID, &pos.Protocol, &nftTokenID, &contractAddress,
		&pos.Token0Symbol, &pos.Token1Symbol, &token0Contract, &token1Contract, &pos.Token0Decimals, &pos.Token1Decimals,
		&depositedUSD, &withdrawnUSD, &claimedFeesUSD,
		&depositedT0, &depositedT1, &withdrawnT0, &withdrawnT1,
		&claimedT0, &claimedT1,
		&status, &pos.OpenedAt, &pos.ClosedAt, &realizedPnL, &aprBps,
		&pos.CreatedAt, &pos.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if nftTokenID.Valid {
		pos.NFTTokenID = nftTokenID.String
	}
	if contractAddress.Valid {
		pos.ContractAddress = contractAddress.String
	}
	if token0Contract.Valid {
		pos.Token0Contract = token0Contract.String
	}
	if token1Contract.Valid {
		pos.Token1Contract = token1Contract.String
	}

	pos.Status = lpposition.Status(status)

	if realizedPnL.Valid {
		v, ok := new(big.Int).SetString(realizedPnL.String, 10)
		if ok {
			pos.RealizedPnLUSD = v
		}
	}
	if aprBps.Valid {
		v := int(aprBps.Int32)
		pos.APRBps = &v
	}

	pos.TotalDepositedUSD = parseBigInt(depositedUSD)
	pos.TotalWithdrawnUSD = parseBigInt(withdrawnUSD)
	pos.TotalClaimedFeesUSD = parseBigInt(claimedFeesUSD)
	pos.TotalDepositedToken0 = parseBigInt(depositedT0)
	pos.TotalDepositedToken1 = parseBigInt(depositedT1)
	pos.TotalWithdrawnToken0 = parseBigInt(withdrawnT0)
	pos.TotalWithdrawnToken1 = parseBigInt(withdrawnT1)
	pos.TotalClaimedToken0 = parseBigInt(claimedT0)
	pos.TotalClaimedToken1 = parseBigInt(claimedT1)

	return &pos, nil
}

func (r *LPPositionRepo) scanMany(ctx context.Context, query string, args ...any) ([]*lpposition.LPPosition, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query lp_positions: %w", err)
	}
	defer rows.Close()

	var positions []*lpposition.LPPosition
	for rows.Next() {
		pos, err := r.scanOne(rows)
		if err != nil {
			return nil, fmt.Errorf("scan lp_position: %w", err)
		}
		positions = append(positions, pos)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lp_positions: %w", err)
	}
	return positions, nil
}

func parseBigInt(s string) *big.Int {
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return big.NewInt(0)
	}
	return v
}
