package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kislikjeka/moontrack/internal/core/ledger/domain"
	"github.com/kislikjeka/moontrack/internal/core/ledger/repository"
)

// LedgerRepository implements the repository interface using PostgreSQL
type LedgerRepository struct {
	pool *pgxpool.Pool
}

// NewLedgerRepository creates a new PostgreSQL ledger repository
func NewLedgerRepository(pool *pgxpool.Pool) *LedgerRepository {
	return &LedgerRepository{pool: pool}
}

// Account operations

// CreateAccount creates a new account in the database
func (r *LedgerRepository) CreateAccount(ctx context.Context, account *domain.Account) error {
	if err := account.Validate(); err != nil {
		return fmt.Errorf("invalid account: %w", err)
	}

	metadataJSON, err := json.Marshal(account.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `
		INSERT INTO accounts (id, code, type, asset_id, wallet_id, chain_id, created_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	_, err = r.pool.Exec(ctx, query,
		account.ID,
		account.Code,
		string(account.Type),
		account.AssetID,
		account.WalletID,
		account.ChainID,
		account.CreatedAt,
		metadataJSON,
	)
	if err != nil {
		return fmt.Errorf("failed to create account: %w", err)
	}

	return nil
}

// GetAccount retrieves an account by ID
func (r *LedgerRepository) GetAccount(ctx context.Context, id uuid.UUID) (*domain.Account, error) {
	query := `
		SELECT id, code, type, asset_id, wallet_id, chain_id, created_at, metadata
		FROM accounts
		WHERE id = $1
	`

	var account domain.Account
	var metadataJSON []byte
	var walletID sql.NullString
	var chainID sql.NullString

	err := r.pool.QueryRow(ctx, query, id).Scan(
		&account.ID,
		&account.Code,
		&account.Type,
		&account.AssetID,
		&walletID,
		&chainID,
		&account.CreatedAt,
		&metadataJSON,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("account not found: %w", err)
		}
		return nil, fmt.Errorf("failed to get account: %w", err)
	}

	// Parse optional fields
	if walletID.Valid {
		wID, err := uuid.Parse(walletID.String)
		if err != nil {
			return nil, fmt.Errorf("invalid wallet ID: %w", err)
		}
		account.WalletID = &wID
	}

	if chainID.Valid {
		account.ChainID = &chainID.String
	}

	// Parse metadata
	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &account.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	return &account, nil
}

// GetAccountByCode retrieves an account by its code
func (r *LedgerRepository) GetAccountByCode(ctx context.Context, code string) (*domain.Account, error) {
	query := `
		SELECT id, code, type, asset_id, wallet_id, chain_id, created_at, metadata
		FROM accounts
		WHERE code = $1
	`

	var account domain.Account
	var metadataJSON []byte
	var walletID sql.NullString
	var chainID sql.NullString

	err := r.pool.QueryRow(ctx, query, code).Scan(
		&account.ID,
		&account.Code,
		&account.Type,
		&account.AssetID,
		&walletID,
		&chainID,
		&account.CreatedAt,
		&metadataJSON,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("account not found: %w", err)
		}
		return nil, fmt.Errorf("failed to get account: %w", err)
	}

	// Parse optional fields
	if walletID.Valid {
		wID, err := uuid.Parse(walletID.String)
		if err != nil {
			return nil, fmt.Errorf("invalid wallet ID: %w", err)
		}
		account.WalletID = &wID
	}

	if chainID.Valid {
		account.ChainID = &chainID.String
	}

	// Parse metadata
	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &account.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	return &account, nil
}

// FindAccountsByWallet retrieves all accounts for a given wallet
func (r *LedgerRepository) FindAccountsByWallet(ctx context.Context, walletID uuid.UUID) ([]*domain.Account, error) {
	query := `
		SELECT id, code, type, asset_id, wallet_id, chain_id, created_at, metadata
		FROM accounts
		WHERE wallet_id = $1
		ORDER BY created_at ASC
	`

	rows, err := r.pool.Query(ctx, query, walletID)
	if err != nil {
		return nil, fmt.Errorf("failed to query accounts: %w", err)
	}
	defer rows.Close()

	var accounts []*domain.Account
	for rows.Next() {
		var account domain.Account
		var metadataJSON []byte
		var wID sql.NullString
		var chainID sql.NullString

		err := rows.Scan(
			&account.ID,
			&account.Code,
			&account.Type,
			&account.AssetID,
			&wID,
			&chainID,
			&account.CreatedAt,
			&metadataJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan account: %w", err)
		}

		// Parse optional fields
		if wID.Valid {
			parsedWID, err := uuid.Parse(wID.String)
			if err != nil {
				return nil, fmt.Errorf("invalid wallet ID: %w", err)
			}
			account.WalletID = &parsedWID
		}

		if chainID.Valid {
			account.ChainID = &chainID.String
		}

		// Parse metadata
		if len(metadataJSON) > 0 {
			if err := json.Unmarshal(metadataJSON, &account.Metadata); err != nil {
				return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
			}
		}

		accounts = append(accounts, &account)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating accounts: %w", err)
	}

	return accounts, nil
}

// Transaction operations

// CreateTransaction creates a new transaction with its entries
func (r *LedgerRepository) CreateTransaction(ctx context.Context, tx *domain.Transaction) error {
	if err := tx.Validate(); err != nil {
		return fmt.Errorf("invalid transaction: %w", err)
	}

	rawDataJSON, err := json.Marshal(tx.RawData)
	if err != nil {
		return fmt.Errorf("failed to marshal raw data: %w", err)
	}

	metadataJSON, err := json.Marshal(tx.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Insert transaction
	txQuery := `
		INSERT INTO transactions (id, type, source, external_id, status, version, occurred_at, recorded_at, raw_data, metadata, error_message)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`

	_, err = r.pool.Exec(ctx, txQuery,
		tx.ID,
		tx.Type,
		tx.Source,
		tx.ExternalID,
		string(tx.Status),
		tx.Version,
		tx.OccurredAt,
		tx.RecordedAt,
		rawDataJSON,
		metadataJSON,
		tx.ErrorMessage,
	)
	if err != nil {
		return fmt.Errorf("failed to create transaction: %w", err)
	}

	// Insert entries
	for _, entry := range tx.Entries {
		if err := r.createEntry(ctx, entry); err != nil {
			return fmt.Errorf("failed to create entry: %w", err)
		}
	}

	return nil
}

// createEntry creates a single entry (helper method)
func (r *LedgerRepository) createEntry(ctx context.Context, entry *domain.Entry) error {
	if err := entry.Validate(); err != nil {
		return fmt.Errorf("invalid entry: %w", err)
	}

	metadataJSON, err := json.Marshal(entry.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `
		INSERT INTO entries (id, transaction_id, account_id, debit_credit, entry_type, amount, asset_id, usd_rate, usd_value, occurred_at, created_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`

	_, err = r.pool.Exec(ctx, query,
		entry.ID,
		entry.TransactionID,
		entry.AccountID,
		string(entry.DebitCredit),
		string(entry.EntryType),
		entry.Amount.String(), // Store big.Int as string (NUMERIC in DB)
		entry.AssetID,
		entry.USDRate.String(),
		entry.USDValue.String(),
		entry.OccurredAt,
		entry.CreatedAt,
		metadataJSON,
	)
	if err != nil {
		return fmt.Errorf("failed to insert entry: %w", err)
	}

	return nil
}

// GetTransaction retrieves a transaction by ID with its entries
func (r *LedgerRepository) GetTransaction(ctx context.Context, id uuid.UUID) (*domain.Transaction, error) {
	query := `
		SELECT id, type, source, external_id, status, version, occurred_at, recorded_at, raw_data, metadata, error_message
		FROM transactions
		WHERE id = $1
	`

	var tx domain.Transaction
	var rawDataJSON, metadataJSON []byte
	var externalID, errorMessage sql.NullString

	err := r.pool.QueryRow(ctx, query, id).Scan(
		&tx.ID,
		&tx.Type,
		&tx.Source,
		&externalID,
		&tx.Status,
		&tx.Version,
		&tx.OccurredAt,
		&tx.RecordedAt,
		&rawDataJSON,
		&metadataJSON,
		&errorMessage,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("transaction not found: %w", err)
		}
		return nil, fmt.Errorf("failed to get transaction: %w", err)
	}

	// Parse optional fields
	if externalID.Valid {
		tx.ExternalID = &externalID.String
	}

	if errorMessage.Valid {
		tx.ErrorMessage = &errorMessage.String
	}

	// Parse JSON fields
	if len(rawDataJSON) > 0 {
		if err := json.Unmarshal(rawDataJSON, &tx.RawData); err != nil {
			return nil, fmt.Errorf("failed to unmarshal raw data: %w", err)
		}
	}

	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &tx.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	// Load entries
	entries, err := r.GetEntriesByTransaction(ctx, tx.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get entries: %w", err)
	}
	tx.Entries = entries

	return &tx, nil
}

// FindTransactionsBySource finds a transaction by source and external ID
func (r *LedgerRepository) FindTransactionsBySource(ctx context.Context, source string, externalID string) (*domain.Transaction, error) {
	query := `
		SELECT id, type, source, external_id, status, version, occurred_at, recorded_at, raw_data, metadata, error_message
		FROM transactions
		WHERE source = $1 AND external_id = $2
	`

	var tx domain.Transaction
	var rawDataJSON, metadataJSON []byte
	var extID, errorMessage sql.NullString

	err := r.pool.QueryRow(ctx, query, source, externalID).Scan(
		&tx.ID,
		&tx.Type,
		&tx.Source,
		&extID,
		&tx.Status,
		&tx.Version,
		&tx.OccurredAt,
		&tx.RecordedAt,
		&rawDataJSON,
		&metadataJSON,
		&errorMessage,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("transaction not found: %w", err)
		}
		return nil, fmt.Errorf("failed to get transaction: %w", err)
	}

	// Parse optional fields
	if extID.Valid {
		tx.ExternalID = &extID.String
	}

	if errorMessage.Valid {
		tx.ErrorMessage = &errorMessage.String
	}

	// Parse JSON fields
	if len(rawDataJSON) > 0 {
		if err := json.Unmarshal(rawDataJSON, &tx.RawData); err != nil {
			return nil, fmt.Errorf("failed to unmarshal raw data: %w", err)
		}
	}

	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &tx.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	// Load entries
	entries, err := r.GetEntriesByTransaction(ctx, tx.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get entries: %w", err)
	}
	tx.Entries = entries

	return &tx, nil
}

// ListTransactions lists transactions with filters and pagination
func (r *LedgerRepository) ListTransactions(ctx context.Context, filters repository.TransactionFilters) ([]*domain.Transaction, error) {
	query := `
		SELECT id, type, source, external_id, status, version, occurred_at, recorded_at, raw_data, metadata, error_message
		FROM transactions
		WHERE 1=1
	`

	args := make([]interface{}, 0)
	argPos := 1

	if filters.Type != nil {
		query += fmt.Sprintf(" AND type = $%d", argPos)
		args = append(args, *filters.Type)
		argPos++
	}

	if filters.Status != nil {
		query += fmt.Sprintf(" AND status = $%d", argPos)
		args = append(args, string(*filters.Status))
		argPos++
	}

	if filters.FromDate != nil {
		query += fmt.Sprintf(" AND occurred_at >= $%d", argPos)
		args = append(args, *filters.FromDate)
		argPos++
	}

	if filters.ToDate != nil {
		query += fmt.Sprintf(" AND occurred_at <= $%d", argPos)
		args = append(args, *filters.ToDate)
		argPos++
	}

	query += " ORDER BY occurred_at DESC"

	if filters.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argPos)
		args = append(args, filters.Limit)
		argPos++
	}

	if filters.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argPos)
		args = append(args, filters.Offset)
		argPos++
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query transactions: %w", err)
	}
	defer rows.Close()

	var transactions []*domain.Transaction
	for rows.Next() {
		var tx domain.Transaction
		var rawDataJSON, metadataJSON []byte
		var externalID, errorMessage sql.NullString

		err := rows.Scan(
			&tx.ID,
			&tx.Type,
			&tx.Source,
			&externalID,
			&tx.Status,
			&tx.Version,
			&tx.OccurredAt,
			&tx.RecordedAt,
			&rawDataJSON,
			&metadataJSON,
			&errorMessage,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan transaction: %w", err)
		}

		// Parse optional fields
		if externalID.Valid {
			tx.ExternalID = &externalID.String
		}

		if errorMessage.Valid {
			tx.ErrorMessage = &errorMessage.String
		}

		// Parse JSON fields
		if len(rawDataJSON) > 0 {
			if err := json.Unmarshal(rawDataJSON, &tx.RawData); err != nil {
				return nil, fmt.Errorf("failed to unmarshal raw data: %w", err)
			}
		}

		if len(metadataJSON) > 0 {
			if err := json.Unmarshal(metadataJSON, &tx.Metadata); err != nil {
				return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
			}
		}

		transactions = append(transactions, &tx)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating transactions: %w", err)
	}

	return transactions, nil
}

// Entry operations

// GetEntriesByTransaction retrieves all entries for a transaction
func (r *LedgerRepository) GetEntriesByTransaction(ctx context.Context, transactionID uuid.UUID) ([]*domain.Entry, error) {
	query := `
		SELECT id, transaction_id, account_id, debit_credit, entry_type, amount, asset_id, usd_rate, usd_value, occurred_at, created_at, metadata
		FROM entries
		WHERE transaction_id = $1
		ORDER BY created_at ASC
	`

	rows, err := r.pool.Query(ctx, query, transactionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query entries: %w", err)
	}
	defer rows.Close()

	var entries []*domain.Entry
	for rows.Next() {
		entry, err := r.scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating entries: %w", err)
	}

	return entries, nil
}

// GetEntriesByAccount retrieves all entries for an account
func (r *LedgerRepository) GetEntriesByAccount(ctx context.Context, accountID uuid.UUID) ([]*domain.Entry, error) {
	query := `
		SELECT id, transaction_id, account_id, debit_credit, entry_type, amount, asset_id, usd_rate, usd_value, occurred_at, created_at, metadata
		FROM entries
		WHERE account_id = $1
		ORDER BY occurred_at ASC
	`

	rows, err := r.pool.Query(ctx, query, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to query entries: %w", err)
	}
	defer rows.Close()

	var entries []*domain.Entry
	for rows.Next() {
		entry, err := r.scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating entries: %w", err)
	}

	return entries, nil
}

// scanEntry scans a single entry from a row
func (r *LedgerRepository) scanEntry(row pgx.Row) (*domain.Entry, error) {
	var entry domain.Entry
	var amountStr, usdRateStr, usdValueStr string
	var metadataJSON []byte

	err := row.Scan(
		&entry.ID,
		&entry.TransactionID,
		&entry.AccountID,
		&entry.DebitCredit,
		&entry.EntryType,
		&amountStr,
		&entry.AssetID,
		&usdRateStr,
		&usdValueStr,
		&entry.OccurredAt,
		&entry.CreatedAt,
		&metadataJSON,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan entry: %w", err)
	}

	// Parse big.Int fields
	amount, ok := new(big.Int).SetString(amountStr, 10)
	if !ok {
		return nil, fmt.Errorf("failed to parse amount: %s", amountStr)
	}
	entry.Amount = amount

	usdRate, ok := new(big.Int).SetString(usdRateStr, 10)
	if !ok {
		return nil, fmt.Errorf("failed to parse usd_rate: %s", usdRateStr)
	}
	entry.USDRate = usdRate

	usdValue, ok := new(big.Int).SetString(usdValueStr, 10)
	if !ok {
		return nil, fmt.Errorf("failed to parse usd_value: %s", usdValueStr)
	}
	entry.USDValue = usdValue

	// Parse metadata
	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &entry.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	return &entry, nil
}

// Balance operations

// GetAccountBalance retrieves the current balance for an account/asset
func (r *LedgerRepository) GetAccountBalance(ctx context.Context, accountID uuid.UUID, assetID string) (*domain.AccountBalance, error) {
	query := `
		SELECT account_id, asset_id, balance, usd_value, last_updated
		FROM account_balances
		WHERE account_id = $1 AND asset_id = $2
	`

	var balance domain.AccountBalance
	var balanceStr, usdValueStr string

	err := r.pool.QueryRow(ctx, query, accountID, assetID).Scan(
		&balance.AccountID,
		&balance.AssetID,
		&balanceStr,
		&usdValueStr,
		&balance.LastUpdated,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			// Return zero balance if not found
			return &domain.AccountBalance{
				AccountID:   accountID,
				AssetID:     assetID,
				Balance:     big.NewInt(0),
				USDValue:    big.NewInt(0),
				LastUpdated: time.Now(),
			}, nil
		}
		return nil, fmt.Errorf("failed to get account balance: %w", err)
	}

	// Parse big.Int fields
	bal, ok := new(big.Int).SetString(balanceStr, 10)
	if !ok {
		return nil, fmt.Errorf("failed to parse balance: %s", balanceStr)
	}
	balance.Balance = bal

	usdVal, ok := new(big.Int).SetString(usdValueStr, 10)
	if !ok {
		return nil, fmt.Errorf("failed to parse usd_value: %s", usdValueStr)
	}
	balance.USDValue = usdVal

	return &balance, nil
}

// UpsertAccountBalance creates or updates an account balance
func (r *LedgerRepository) UpsertAccountBalance(ctx context.Context, balance *domain.AccountBalance) error {
	if err := balance.Validate(); err != nil {
		return fmt.Errorf("invalid balance: %w", err)
	}

	query := `
		INSERT INTO account_balances (account_id, asset_id, balance, usd_value, last_updated)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (account_id, asset_id)
		DO UPDATE SET
			balance = EXCLUDED.balance,
			usd_value = EXCLUDED.usd_value,
			last_updated = EXCLUDED.last_updated
	`

	_, err := r.pool.Exec(ctx, query,
		balance.AccountID,
		balance.AssetID,
		balance.Balance.String(),
		balance.USDValue.String(),
		balance.LastUpdated,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert account balance: %w", err)
	}

	return nil
}

// GetAccountBalances retrieves all balances for an account
func (r *LedgerRepository) GetAccountBalances(ctx context.Context, accountID uuid.UUID) ([]*domain.AccountBalance, error) {
	query := `
		SELECT account_id, asset_id, balance, usd_value, last_updated
		FROM account_balances
		WHERE account_id = $1
		ORDER BY asset_id ASC
	`

	rows, err := r.pool.Query(ctx, query, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to query account balances: %w", err)
	}
	defer rows.Close()

	var balances []*domain.AccountBalance
	for rows.Next() {
		var balance domain.AccountBalance
		var balanceStr, usdValueStr string

		err := rows.Scan(
			&balance.AccountID,
			&balance.AssetID,
			&balanceStr,
			&usdValueStr,
			&balance.LastUpdated,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan balance: %w", err)
		}

		// Parse big.Int fields
		bal, ok := new(big.Int).SetString(balanceStr, 10)
		if !ok {
			return nil, fmt.Errorf("failed to parse balance: %s", balanceStr)
		}
		balance.Balance = bal

		usdVal, ok := new(big.Int).SetString(usdValueStr, 10)
		if !ok {
			return nil, fmt.Errorf("failed to parse usd_value: %s", usdValueStr)
		}
		balance.USDValue = usdVal

		balances = append(balances, &balance)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating balances: %w", err)
	}

	return balances, nil
}

// CalculateBalanceFromEntries calculates the balance from ledger entries (for verification)
func (r *LedgerRepository) CalculateBalanceFromEntries(ctx context.Context, accountID uuid.UUID, assetID string) (*big.Int, error) {
	query := `
		SELECT
			COALESCE(SUM(
				CASE
					WHEN debit_credit = 'DEBIT' THEN amount::numeric
					WHEN debit_credit = 'CREDIT' THEN -amount::numeric
				END
			), 0) as balance
		FROM entries
		WHERE account_id = $1 AND asset_id = $2
	`

	var balanceStr string
	err := r.pool.QueryRow(ctx, query, accountID, assetID).Scan(&balanceStr)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate balance: %w", err)
	}

	balance, ok := new(big.Int).SetString(balanceStr, 10)
	if !ok {
		return nil, fmt.Errorf("failed to parse calculated balance: %s", balanceStr)
	}

	return balance, nil
}

// Transaction management (no-op implementations - transactions managed by context)
// Note: In a real implementation, you would use pgx transactions

func (r *LedgerRepository) BeginTx(ctx context.Context) (context.Context, error) {
	// TODO: Implement transaction management using pgx
	// For now, return the same context
	return ctx, nil
}

func (r *LedgerRepository) CommitTx(ctx context.Context) error {
	// TODO: Implement transaction commit
	return nil
}

func (r *LedgerRepository) RollbackTx(ctx context.Context) error {
	// TODO: Implement transaction rollback
	return nil
}
