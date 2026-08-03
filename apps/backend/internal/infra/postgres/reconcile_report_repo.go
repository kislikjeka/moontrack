package postgres

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kislikjeka/moontrack/internal/platform/reconcilereport"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
)

var _ reconcilereport.LedgerReader = (*ReconcileReportRepository)(nil)

// ReconcileReportRepository loads the ledger side of the reconciliation report
// (issue #61).
//
// It is separate from LedgerRepository because it asks a question the
// application never asks: it wants BOTH statements of every balance at once —
// the one derived from the entries and the one materialized in
// account_balances — so the report can compare them against each other. Every
// other reader wants one authoritative number and would be right to hide the
// distinction.
type ReconcileReportRepository struct {
	pool *pgxpool.Pool
}

// NewReconcileReportRepository creates the report's ledger reader.
func NewReconcileReportRepository(pool *pgxpool.Pool) *ReconcileReportRepository {
	return &ReconcileReportRepository{pool: pool}
}

// Load reads EVERY account the wallet owns — the wallet namespace and the
// collateral and liability namespaces alike — each with its entries-derived
// balance, its materialized balance and the asset identity it addresses.
//
// It is not restricted to CRYPTO_WALLET, and that matters: #49's evidence for
// the defect it diagnosed was an asset sitting in
// `collateral..{walletID}.base.variableDebtBasUSDC`, so a materialization check
// or a split check that skipped that namespace would be blind to the very case
// that motivated it. The TRIANGLE narrows back to the wallet namespace on its
// own (LedgerSnapshot.LedgerBalances), because the provider's balancesOf can
// only see what the wallet address holds.
//
// The entries-derived balance is a LEFT JOIN aggregate rather than a read of
// account_balances, and that is the decision the whole triangle rests on: taking
// L from the materialization would make a stale cache read as a posting error,
// so the diagnosis edge F↔L would name the wrong defect. The materialized value
// is loaded too, but only as the subject of its own check.
//
// Accounts with no entries at all are still returned, with a zero balance. They
// take part in the "exactly one account per identity" check, where a split whose
// second half is currently empty is the same defect one posting away from
// mattering.
func (r *ReconcileReportRepository) Load(ctx context.Context, walletID uuid.UUID) (*reconcilereport.LedgerSnapshot, error) {
	const query = `
		SELECT
			a.id,
			a.code,
			a.type,
			ar.chain,
			ar.contract,
			ar.symbol,
			COALESCE(e.entries_balance, 0)::text AS entries_balance,
			ab.balance::text                     AS materialized_balance
		FROM accounts a
		JOIN asset_registry ar ON ar.id = a.asset_id
		LEFT JOIN (
			SELECT account_id,
			       SUM(CASE WHEN debit_credit = 'DEBIT' THEN amount ELSE -amount END) AS entries_balance
			FROM entries
			GROUP BY account_id
		) e ON e.account_id = a.id
		LEFT JOIN account_balances ab ON ab.account_id = a.id AND ab.asset_id = a.asset_id
		WHERE a.wallet_id = $1
		ORDER BY a.code
	`

	rows, err := r.pool.Query(ctx, query, walletID)
	if err != nil {
		return nil, fmt.Errorf("failed to query wallet accounts: %w", err)
	}
	defer rows.Close()

	snap := &reconcilereport.LedgerSnapshot{
		WalletID:     walletID,
		ChainCursors: map[string]string{},
	}

	for rows.Next() {
		var (
			acc            reconcilereport.LedgerAccount
			chain, cont    string
			entriesStr     string
			materializedIn *string
		)
		if err := rows.Scan(&acc.AccountID, &acc.Code, &acc.Type, &chain, &cont, &acc.Symbol,
			&entriesStr, &materializedIn); err != nil {
			return nil, fmt.Errorf("failed to scan wallet account: %w", err)
		}

		// The key is built through the same normalizer the rest of the system
		// uses, because it is MoonTrack's own identity on both sides here — the
		// deliberate duplication in the report applies to the PROVIDER's spelling,
		// which is what the report has to be able to disagree about.
		acc.Key = sync.NewAssetKey(chain, cont)

		balance, ok := new(big.Int).SetString(entriesStr, 10)
		if !ok {
			return nil, fmt.Errorf("account %s: cannot parse entries balance %q", acc.Code, entriesStr)
		}
		acc.EntriesBalance = balance

		if materializedIn != nil {
			m, ok := new(big.Int).SetString(*materializedIn, 10)
			if !ok {
				return nil, fmt.Errorf("account %s: cannot parse materialized balance %q", acc.Code, *materializedIn)
			}
			acc.MaterializedBalance = m
		}

		snap.Accounts = append(snap.Accounts, acc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read wallet accounts: %w", err)
	}

	cursors, err := r.chainCursors(ctx, walletID)
	if err != nil {
		return nil, err
	}
	snap.ChainCursors = cursors

	return snap, nil
}

// chainCursors reads how far collection got on each chain. It is half of the
// time gap the report prints instead of closing.
func (r *ReconcileReportRepository) chainCursors(ctx context.Context, walletID uuid.UUID) (map[string]string, error) {
	const query = `
		SELECT chain, collect_cursor_at
		FROM wallet_chain_sync
		WHERE wallet_id = $1
		ORDER BY chain
	`
	rows, err := r.pool.Query(ctx, query, walletID)
	if err != nil {
		return nil, fmt.Errorf("failed to query chain cursors: %w", err)
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var chain string
		var cursor *time.Time
		if err := rows.Scan(&chain, &cursor); err != nil {
			return nil, fmt.Errorf("failed to scan chain cursor: %w", err)
		}
		if cursor == nil {
			// An unset cursor is a fact worth stating, not an empty string that
			// reads as a rendering slip.
			out[chain] = "never"
			continue
		}
		out[chain] = cursor.UTC().Format(time.RFC3339)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read chain cursors: %w", err)
	}
	return out, nil
}

// KnownnessVerdicts reads the stored knownness verdict for every identity the
// registry has ever seen, so the report can tell a green "checked: unknown" row
// from a green "not checked yet" one.
//
// It reads the whole table rather than probing per identity, because the report
// needs the verdict for identities the LEDGER never saw — the spam positions
// the filter kept out — and there is no per-identity call that would ask about
// something the report does not yet know it will ask about.
func (r *ReconcileReportRepository) KnownnessVerdicts(ctx context.Context) (map[sync.AssetKey]string, error) {
	const query = `SELECT chain, contract, status FROM asset_knownness`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query knownness verdicts: %w", err)
	}
	defer rows.Close()

	out := map[sync.AssetKey]string{}
	for rows.Next() {
		var chain, contract, status string
		if err := rows.Scan(&chain, &contract, &status); err != nil {
			return nil, fmt.Errorf("failed to scan knownness verdict: %w", err)
		}
		out[sync.NewAssetKey(chain, contract)] = status
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read knownness verdicts: %w", err)
	}
	return out, nil
}
