package transactions

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/assetregistry"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// WalletRepository defines the interface for wallet lookups
type WalletRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*wallet.Wallet, error)
}

// AssetAmbiguity reports which of these assets carry a ticker that does not name
// them uniquely on their chain (#42).
//
// Only the ambiguity half is needed here: the ticker itself already travels in
// raw_data beside the entry, recorded at the time the transaction was written.
// The flag cannot come from there — it is a property of the registry as it
// stands NOW (a second contract with the same ticker may appear long after this
// transaction was booked), so it has to be read rather than remembered.
//
// It is a batch call because a list of transactions is resolved at once; per-row
// reads would issue one registry-wide query per row.
type AssetAmbiguity interface {
	GetMany(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]assetregistry.Asset, error)
}

// TransactionService provides read-only access to enriched transaction data
type TransactionService struct {
	ledgerService  *ledger.Service
	walletRepo     WalletRepository
	readerRegistry *ReaderRegistry
	resolver       *money.DecimalResolver
	assets         AssetAmbiguity // nilable — rows then render unflagged
}

// NewTransactionService creates a new transaction service
func NewTransactionService(
	ledgerService *ledger.Service,
	walletRepo WalletRepository,
	resolver *money.DecimalResolver,
) *TransactionService {
	return &TransactionService{
		ledgerService:  ledgerService,
		walletRepo:     walletRepo,
		readerRegistry: NewReaderRegistry(),
		resolver:       resolver,
	}
}

// WithAssetAmbiguity attaches the registry read that flags duplicate tickers.
//
// Optional so a bare service still lists transactions: without it every row
// renders as unambiguous, which is the pre-#42 behaviour rather than a wrong
// answer — the ticker and the amount are unchanged, only the disambiguating
// qualifier is absent.
func (s *TransactionService) WithAssetAmbiguity(a AssetAmbiguity) *TransactionService {
	s.assets = a
	return s
}

// assetQualifiers reads the contract and ambiguity flag for these assets.
//
// A registry failure yields an empty map, so rows render unflagged rather than
// the whole listing failing: the transaction's own facts do not depend on it.
func (s *TransactionService) assetQualifiers(ctx context.Context, ids []uuid.UUID) map[uuid.UUID]assetregistry.Asset {
	if s.assets == nil || len(ids) == 0 {
		return nil
	}
	byID, err := s.assets.GetMany(ctx, ids)
	if err != nil {
		return nil
	}
	return byID
}

// applyQualifier stamps the contract and ambiguity flag onto a list item.
func applyQualifier(item *TransactionListItem, byID map[uuid.UUID]assetregistry.Asset) {
	id, err := uuid.Parse(item.AssetID)
	if err != nil {
		return
	}
	if a, ok := byID[id]; ok {
		item.AssetContract = a.Contract
		item.SymbolAmbiguous = a.SymbolAmbiguous
	}
}

// ListTransactions returns enriched transactions for the given filters
func (s *TransactionService) ListTransactions(ctx context.Context, filters ledger.TransactionFilters) ([]TransactionListItem, error) {
	// Get raw transactions from ledger
	transactions, err := s.ledgerService.ListTransactions(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to list transactions: %w", err)
	}

	// Collect unique wallet IDs
	walletIDs := make(map[uuid.UUID]bool)
	for _, tx := range transactions {
		reader, ok := s.readerRegistry.GetReader(tx.Type)
		if !ok {
			continue
		}
		fields, err := reader.ReadForList(tx.RawData)
		if err != nil {
			continue
		}
		walletIDs[fields.WalletID] = true
	}

	// Batch fetch wallets
	wallets := make(map[uuid.UUID]*wallet.Wallet)
	for walletID := range walletIDs {
		w, err := s.walletRepo.GetByID(ctx, walletID)
		if err == nil && w != nil {
			wallets[walletID] = w
		}
	}

	// Enrich transactions
	result := make([]TransactionListItem, 0, len(transactions))
	for _, tx := range transactions {
		item, err := s.toListItem(ctx, tx, wallets)
		if err != nil {
			continue // Skip transactions that can't be enriched
		}
		result = append(result, *item)
	}

	// One registry read for the whole page, then stamp each row (#42).
	assetIDs := make([]uuid.UUID, 0, len(result))
	for _, item := range result {
		if id, err := uuid.Parse(item.AssetID); err == nil {
			assetIDs = append(assetIDs, id)
		}
	}
	if byID := s.assetQualifiers(ctx, assetIDs); byID != nil {
		for i := range result {
			applyQualifier(&result[i], byID)
		}
	}

	return result, nil
}

// GetTransaction returns a single transaction with full details and entries
func (s *TransactionService) GetTransaction(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*TransactionDetail, error) {
	// Get transaction with entries
	tx, err := s.ledgerService.GetTransaction(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("transaction not found: %w", err)
	}

	// Get reader for this transaction type
	reader, ok := s.readerRegistry.GetReader(tx.Type)
	if !ok {
		return nil, fmt.Errorf("unknown transaction type: %s", tx.Type)
	}

	// Parse transaction details
	fields, err := reader.ReadForDetail(tx.RawData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse transaction: %w", err)
	}

	// Authorization check: verify user owns the wallet
	w, err := s.walletRepo.GetByID(ctx, fields.WalletID)
	if err != nil || w == nil {
		return nil, fmt.Errorf("wallet not found")
	}
	if w.UserID != userID {
		return nil, fmt.Errorf("transaction not found") // Return 404 to prevent ID enumeration
	}

	// Build response
	walletName := w.Name
	displayAmount := s.formatDisplayAmountResolved(ctx, fields.Amount, fields.AssetSymbol)

	usdValue := ""
	if fields.USDValue != nil && fields.USDValue.Sign() > 0 {
		usdValue = money.FormatUSD(fields.USDValue)
	}

	detail := &TransactionDetail{
		TransactionListItem: TransactionListItem{
			ID:            tx.ID.String(),
			Type:          tx.Type.String(),
			TypeLabel:     tx.Type.Label(),
			AssetID:       fields.AssetID.String(),
			AssetSymbol:   fields.AssetSymbol,
			Amount:        fields.Amount.String(),
			DisplayAmount: displayAmount,
			Direction:     fields.Direction,
			WalletID:      fields.WalletID.String(),
			WalletName:    walletName,
			Status:        string(tx.Status),
			OccurredAt:    tx.OccurredAt.Format(time.RFC3339),
			USDValue:      usdValue,
			ChainID:       fields.ChainID,
		},
		Source:     tx.Source,
		ExternalID: tx.ExternalID,
		RecordedAt: tx.RecordedAt.Format(time.RFC3339),
		Notes:      fields.Notes,
		RawData:    tx.RawData,
		Entries:    s.toEntryResponses(ctx, tx.Entries, walletName, symbolsFromRawData(tx.RawData)),
	}

	// The header asset and every entry's asset in one registry read (#42). A
	// transaction routinely touches more than one asset — the gas leg is rarely
	// the token being moved — so this is a set, not a single id.
	assetIDs := make([]uuid.UUID, 0, len(tx.Entries)+1)
	assetIDs = append(assetIDs, fields.AssetID)
	for _, e := range tx.Entries {
		assetIDs = append(assetIDs, e.AssetID)
	}
	if byID := s.assetQualifiers(ctx, assetIDs); byID != nil {
		applyQualifier(&detail.TransactionListItem, byID)
		for i := range detail.Entries {
			if id, err := uuid.Parse(detail.Entries[i].AssetID); err == nil {
				if a, ok := byID[id]; ok {
					detail.Entries[i].AssetContract = a.Contract
					detail.Entries[i].SymbolAmbiguous = a.SymbolAmbiguous
				}
			}
		}
	}

	return detail, nil
}

// toListItem converts a domain transaction to a list item DTO
func (s *TransactionService) toListItem(ctx context.Context, tx *ledger.Transaction, wallets map[uuid.UUID]*wallet.Wallet) (*TransactionListItem, error) {
	reader, ok := s.readerRegistry.GetReader(tx.Type)
	if !ok {
		return nil, fmt.Errorf("unknown transaction type: %s", tx.Type)
	}

	fields, err := reader.ReadForList(tx.RawData)
	if err != nil {
		return nil, err
	}

	walletName := ""
	if w, ok := wallets[fields.WalletID]; ok {
		walletName = w.Name
	}

	displayAmount := s.formatDisplayAmountResolved(ctx, fields.Amount, fields.AssetSymbol)

	usdValue := ""
	if fields.USDValue != nil && fields.USDValue.Sign() > 0 {
		usdValue = money.FormatUSD(fields.USDValue)
	}

	return &TransactionListItem{
		ID:            tx.ID.String(),
		Type:          tx.Type.String(),
		TypeLabel:     tx.Type.Label(),
		AssetID:       fields.AssetID.String(),
		AssetSymbol:   fields.AssetSymbol,
		Amount:        fields.Amount.String(),
		DisplayAmount: displayAmount,
		Direction:     fields.Direction,
		WalletID:      fields.WalletID.String(),
		WalletName:    walletName,
		Status:        string(tx.Status),
		OccurredAt:    tx.OccurredAt.Format(time.RFC3339),
		USDValue:      usdValue,
		ChainID:       fields.ChainID,
	}, nil
}

// toEntryResponses converts domain entries to entry response DTOs.
//
// symbols maps a registry UUID to the ticker raw_data recorded for it (#59). A
// ledger entry carries only the UUID, so the ticker has to come from beside it;
// a transaction can touch several assets (the gas leg is rarely the same token
// as the transfer), which is why this is a map and not one symbol for the whole
// transaction. Assets with no recorded symbol render BLANK rather than as a
// UUID — a raw id in a field labelled "BTC" reads as data and is worse than an
// empty one. #42 finalises the API shape here.
func (s *TransactionService) toEntryResponses(ctx context.Context, entries []*ledger.Entry, walletName string, symbols map[uuid.UUID]string) []EntryResponse {
	result := make([]EntryResponse, len(entries))
	for i, entry := range entries {
		accountCode := ""
		accountLabel := ""

		if entry.Metadata != nil {
			if code, ok := entry.Metadata["account_code"].(string); ok {
				accountCode = code
			}
		}

		symbol := symbols[entry.AssetID]

		// Build human-readable account label. With no symbol the label falls
		// back to the account code, which is at least honest about being an
		// identifier — appending a bare UUID to "Income - " would not be.
		switch {
		case symbol == "":
			accountLabel = accountCode
		case strings.HasPrefix(accountCode, "wallet."):
			accountLabel = fmt.Sprintf("%s - %s", walletName, symbol)
		case strings.HasPrefix(accountCode, "income."):
			accountLabel = fmt.Sprintf("Income - %s", symbol)
		case strings.HasPrefix(accountCode, "expense."):
			accountLabel = fmt.Sprintf("Expense - %s", symbol)
		default:
			accountLabel = accountCode
		}

		displayAmount := s.formatDisplayAmountResolved(ctx, entry.Amount, symbol)

		result[i] = EntryResponse{
			ID:            entry.ID.String(),
			AccountCode:   accountCode,
			AccountLabel:  accountLabel,
			DebitCredit:   string(entry.DebitCredit),
			EntryType:     string(entry.EntryType),
			Amount:        entry.Amount.String(),
			DisplayAmount: displayAmount,
			AssetID:       entry.AssetID.String(),
			AssetSymbol:   symbol,
			USDValue:      money.FormatUSD(entry.USDValue),
		}
	}
	return result
}

// formatDisplayAmountResolved formats an amount for a human, using the SYMBOL
// to look up decimals and to label the result.
//
// It takes a ticker, not a registry id, deliberately (#59): both the decimal
// resolver and the rendered suffix are presentation concerns, and the resolver
// behind them is symbol-keyed. Passing the UUID here would resolve no decimals
// and print "1000000000 3f2b…" at the user.
func (s *TransactionService) formatDisplayAmountResolved(ctx context.Context, amount *big.Int, assetSymbol string) string {
	if amount == nil {
		return "0"
	}

	var decimals int
	if s.resolver != nil {
		decimals = s.resolver.ResolveSymbolOnly(ctx, assetSymbol)
	} else {
		decimals = money.GetDecimals(assetSymbol)
	}

	if decimals == 0 {
		return strings.TrimSpace(fmt.Sprintf("%s %s", amount.String(), assetSymbol))
	}

	humanReadable := money.FromBaseUnits(amount, decimals)
	return strings.TrimSpace(fmt.Sprintf("%s %s", humanReadable, assetSymbol))
}

// symbolsFromRawData harvests every (asset_id → asset_symbol) pair a
// transaction's raw_data recorded, so entries can be labelled by the ticker
// that was written next to their id at sync time (#59).
//
// It walks the flat fields, the fee pair, and the three transfer arrays,
// because the writers in sync emit the pair under several key spellings —
// asset_id/asset_symbol for transfers, fee_asset/fee_asset_symbol for gas, and
// asset_id/asset for lending. Reading them all here keeps the knowledge of
// those spellings in one place instead of in every reader.
func symbolsFromRawData(raw map[string]interface{}) map[uuid.UUID]string {
	out := make(map[uuid.UUID]string)

	put := func(idKey, symbolKey string, m map[string]interface{}) {
		idStr, _ := m[idKey].(string)
		symbol, _ := m[symbolKey].(string)
		if idStr == "" || symbol == "" {
			return
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return
		}
		// First spelling wins; they name the same registry row, so a later
		// one can only repeat it.
		if _, seen := out[id]; !seen {
			out[id] = symbol
		}
	}

	put("asset_id", "asset_symbol", raw)
	put("asset_id", "asset", raw) // lending flat fields
	put("fee_asset", "fee_asset_symbol", raw)
	put("native_asset_id", "fee_asset_symbol", raw)

	for _, key := range []string{"transfers", "transfers_in", "transfers_out"} {
		for _, m := range transferMaps(raw[key]) {
			put("asset_id", "asset_symbol", m)
		}
	}

	return out
}

// transferMaps normalises a raw_data transfer array, which survives a JSON
// roundtrip as []interface{} but arrives as []map[string]interface{} when it
// has not been through one.
func transferMaps(v interface{}) []map[string]interface{} {
	switch arr := v.(type) {
	case []map[string]interface{}:
		return arr
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(arr))
		for _, item := range arr {
			if m, ok := item.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}
