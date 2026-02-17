package transactions

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
)

// WalletRepository defines the interface for wallet lookups
type WalletRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*wallet.Wallet, error)
}

// TransactionService provides read-only access to enriched transaction data
type TransactionService struct {
	ledgerService  *ledger.Service
	walletRepo     WalletRepository
	readerRegistry *ReaderRegistry
}

// NewTransactionService creates a new transaction service
func NewTransactionService(
	ledgerService *ledger.Service,
	walletRepo WalletRepository,
) *TransactionService {
	return &TransactionService{
		ledgerService:  ledgerService,
		walletRepo:     walletRepo,
		readerRegistry: NewReaderRegistry(),
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
		item, err := s.toListItem(tx, wallets)
		if err != nil {
			continue // Skip transactions that can't be enriched
		}
		result = append(result, *item)
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
	displayAmount := FormatDisplayAmount(fields.Amount, fields.AssetID)

	usdValue := ""
	if fields.USDValue != nil && fields.USDValue.Sign() > 0 {
		usdValue = fields.USDValue.String()
	}

	detail := &TransactionDetail{
		TransactionListItem: TransactionListItem{
			ID:            tx.ID.String(),
			Type:          tx.Type.String(),
			TypeLabel:     tx.Type.Label(),
			AssetID:       fields.AssetID,
			AssetSymbol:   strings.ToUpper(fields.AssetID),
			Amount:        fields.Amount.String(),
			DisplayAmount: displayAmount,
			Direction:     fields.Direction,
			WalletID:      fields.WalletID.String(),
			WalletName:    walletName,
			Status:        string(tx.Status),
			OccurredAt:    tx.OccurredAt.Format(time.RFC3339),
			USDValue:      usdValue,
		},
		Source:     tx.Source,
		ExternalID: tx.ExternalID,
		RecordedAt: tx.RecordedAt.Format(time.RFC3339),
		Notes:      fields.Notes,
		RawData:    tx.RawData,
		Entries:    s.toEntryResponses(tx.Entries, walletName),
	}

	return detail, nil
}

// toListItem converts a domain transaction to a list item DTO
func (s *TransactionService) toListItem(tx *ledger.Transaction, wallets map[uuid.UUID]*wallet.Wallet) (*TransactionListItem, error) {
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

	displayAmount := FormatDisplayAmount(fields.Amount, fields.AssetID)

	usdValue := ""
	if fields.USDValue != nil && fields.USDValue.Sign() > 0 {
		usdValue = fields.USDValue.String()
	}

	return &TransactionListItem{
		ID:            tx.ID.String(),
		Type:          tx.Type.String(),
		TypeLabel:     tx.Type.Label(),
		AssetID:       fields.AssetID,
		AssetSymbol:   strings.ToUpper(fields.AssetID),
		Amount:        fields.Amount.String(),
		DisplayAmount: displayAmount,
		Direction:     fields.Direction,
		WalletID:      fields.WalletID.String(),
		WalletName:    walletName,
		Status:        string(tx.Status),
		OccurredAt:    tx.OccurredAt.Format(time.RFC3339),
		USDValue:      usdValue,
	}, nil
}

// toEntryResponses converts domain entries to entry response DTOs
func (s *TransactionService) toEntryResponses(entries []*ledger.Entry, walletName string) []EntryResponse {
	result := make([]EntryResponse, len(entries))
	for i, entry := range entries {
		accountCode := ""
		accountLabel := ""

		if entry.Metadata != nil {
			if code, ok := entry.Metadata["account_code"].(string); ok {
				accountCode = code
			}
		}

		// Build human-readable account label
		if strings.HasPrefix(accountCode, "wallet.") {
			accountLabel = fmt.Sprintf("%s - %s", walletName, strings.ToUpper(entry.AssetID))
		} else if strings.HasPrefix(accountCode, "income.") {
			accountLabel = fmt.Sprintf("Income - %s", strings.ToUpper(entry.AssetID))
		} else if strings.HasPrefix(accountCode, "expense.") {
			accountLabel = fmt.Sprintf("Expense - %s", strings.ToUpper(entry.AssetID))
		} else {
			accountLabel = accountCode
		}

		displayAmount := FormatDisplayAmount(entry.Amount, entry.AssetID)

		result[i] = EntryResponse{
			ID:            entry.ID.String(),
			AccountCode:   accountCode,
			AccountLabel:  accountLabel,
			DebitCredit:   string(entry.DebitCredit),
			EntryType:     string(entry.EntryType),
			Amount:        entry.Amount.String(),
			DisplayAmount: displayAmount,
			AssetID:       entry.AssetID,
			AssetSymbol:   strings.ToUpper(entry.AssetID),
			USDValue:      entry.USDValue.String(),
		}
	}
	return result
}

// FormatDisplayAmount converts base units to display format
// e.g., "50000000" satoshi → "0.5 BTC"
func FormatDisplayAmount(amount *big.Int, assetID string) string {
	if amount == nil {
		return "0"
	}

	decimals := getAssetDecimals(assetID)
	if decimals == 0 {
		return fmt.Sprintf("%s %s", amount.String(), strings.ToUpper(assetID))
	}

	// Convert to float for display
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	wholePart := new(big.Int).Div(amount, divisor)
	remainder := new(big.Int).Mod(amount, divisor)

	if remainder.Sign() == 0 {
		return fmt.Sprintf("%s %s", wholePart.String(), strings.ToUpper(assetID))
	}

	// Format with decimal places
	remainderStr := remainder.String()
	// Pad with leading zeros if needed
	for len(remainderStr) < decimals {
		remainderStr = "0" + remainderStr
	}
	// Trim trailing zeros
	remainderStr = strings.TrimRight(remainderStr, "0")

	return fmt.Sprintf("%s.%s %s", wholePart.String(), remainderStr, strings.ToUpper(assetID))
}

// getAssetDecimals returns the number of decimal places for an asset
// Used for display formatting only - does not make API calls
func getAssetDecimals(assetID string) int {
	// Common crypto asset decimals (synced with AssetService.nativeDecimals)
	decimals := map[string]int{
		// Native symbols
		"btc":   8,  // Bitcoin: satoshi
		"eth":   18, // Ethereum: wei
		"usdt":  6,  // Tether
		"usdc":  6,  // USD Coin
		"sol":   9,  // Solana: lamport
		"bnb":   18,
		"xrp":   6,
		"ada":   6, // Cardano: lovelace
		"doge":  8,
		"matic": 18,
		"dot":   10, // Polkadot
		"avax":  18,
		"link":  18,
		"trx":   6,
		"dai":   18,
		"wbtc":  8,
		"ltc":   8,
		"bch":   8,
		"ton":   9,
		"shib":  18,
		// CoinGecko IDs
		"bitcoin":          8,
		"ethereum":         18,
		"tether":           6,
		"usd-coin":         6,
		"solana":           9,
		"binancecoin":      18,
		"ripple":           6,
		"cardano":          6,
		"dogecoin":         8,
		"matic-network":    18,
		"polkadot":         10,
		"avalanche-2":      18,
		"chainlink":        18,
		"tron":             6,
		"litecoin":         8,
		"bitcoin-cash":     8,
		"the-open-network": 9,
		"shiba-inu":        18,
		"wrapped-bitcoin":  8,
	}

	if d, ok := decimals[strings.ToLower(assetID)]; ok {
		return d
	}

	// Default to 8 decimals for unknown assets
	return 8
}
