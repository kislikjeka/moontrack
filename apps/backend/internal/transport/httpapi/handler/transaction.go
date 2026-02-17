package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/module/transactions"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// RegistryServiceInterface defines the interface for asset registry operations needed by TransactionHandler
type RegistryServiceInterface interface {
	GetDecimals(ctx context.Context, coinGeckoID string) (int, error)
}

// LedgerServiceInterface defines the interface for ledger operations needed by TransactionHandler
type LedgerServiceInterface interface {
	RecordTransaction(ctx context.Context, transactionType ledger.TransactionType, source string, externalID *string, occurredAt time.Time, rawData map[string]interface{}) (*ledger.Transaction, error)
}

// TransactionServiceInterface defines the interface for transaction read operations
type TransactionServiceInterface interface {
	ListTransactions(ctx context.Context, filters ledger.TransactionFilters) ([]transactions.TransactionListItem, error)
	GetTransaction(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*transactions.TransactionDetail, error)
}

// TransactionHandler handles transaction-related HTTP requests
type TransactionHandler struct {
	ledgerService      LedgerServiceInterface
	transactionService TransactionServiceInterface
	registryService    RegistryServiceInterface
}

// NewTransactionHandler creates a new transaction handler
func NewTransactionHandler(ledgerService LedgerServiceInterface, transactionService TransactionServiceInterface, registrySvc RegistryServiceInterface) *TransactionHandler {
	return &TransactionHandler{
		ledgerService:      ledgerService,
		transactionService: transactionService,
		registryService:    registrySvc,
	}
}

// CreateTransactionRequest represents the transaction creation request
type CreateTransactionRequest struct {
	Type        string                 `json:"type"` // manual_income, manual_outcome, asset_adjustment
	WalletID    string                 `json:"wallet_id"`
	AssetID     string                 `json:"asset_id"`
	CoingeckoID *string                `json:"coingecko_id,omitempty"` // CoinGecko ID for price lookup (e.g., "bitcoin" for BTC)
	Amount      string                 `json:"amount"`                 // String representation of big.Int
	USDRate     *string                `json:"usd_rate,omitempty"`     // Optional: manual USD rate (string representation of big.Int scaled by 10^8)
	OccurredAt  string                 `json:"occurred_at"`            // RFC3339 format
	Notes       string                 `json:"notes,omitempty"`
	Data        map[string]interface{} `json:"data,omitempty"` // Additional transaction-specific data
}

// TransactionResponse represents a transaction response (used for create/single)
type TransactionResponse struct {
	ID           string                 `json:"id"`
	Type         string                 `json:"type"`
	Source       string                 `json:"source"`
	ExternalID   *string                `json:"external_id,omitempty"`
	Status       string                 `json:"status"`
	OccurredAt   string                 `json:"occurred_at"`
	RecordedAt   string                 `json:"recorded_at"`
	RawData      map[string]interface{} `json:"raw_data,omitempty"`
	ErrorMessage *string                `json:"error_message,omitempty"`
}

// TransactionListItemResponse represents an enriched transaction item for list view
type TransactionListItemResponse struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	TypeLabel     string `json:"type_label"`
	AssetID       string `json:"asset_id"`
	AssetSymbol   string `json:"asset_symbol"`
	Amount        string `json:"amount"`
	DisplayAmount string `json:"display_amount"`
	Direction     string `json:"direction"`
	WalletID      string `json:"wallet_id"`
	WalletName    string `json:"wallet_name"`
	Status        string `json:"status"`
	OccurredAt    string `json:"occurred_at"`
	USDValue      string `json:"usd_value,omitempty"`
}

// TransactionListResponse represents a paginated list of transactions
type TransactionListResponse struct {
	Transactions []TransactionListItemResponse `json:"transactions"`
	Total        int                           `json:"total"`
	Page         int                           `json:"page"`
	PageSize     int                           `json:"page_size"`
}

// CreateTransaction handles POST /transactions
func (h *TransactionHandler) CreateTransaction(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context (set by JWT middleware)
	_, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req CreateTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate transaction type
	if req.Type == "" {
		respondWithError(w, http.StatusBadRequest, "transaction type is required")
		return
	}

	txType := ledger.TransactionType(req.Type)
	if !txType.IsValid() {
		respondWithError(w, http.StatusBadRequest, "invalid transaction type")
		return
	}

	// Parse wallet ID
	walletID, err := uuid.Parse(req.WalletID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid wallet ID")
		return
	}

	// Parse occurred_at timestamp
	occurredAt, err := time.Parse(time.RFC3339, req.OccurredAt)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid occurred_at format (use RFC3339)")
		return
	}

	// Determine the asset ID to use for price lookup and decimals
	// Use CoinGecko ID if provided, otherwise fall back to asset_id
	priceAssetID := req.AssetID
	if req.CoingeckoID != nil && *req.CoingeckoID != "" {
		priceAssetID = *req.CoingeckoID
	}

	// Get decimals for the asset and convert amount from human-readable to base units
	decimals, _ := h.registryService.GetDecimals(r.Context(), priceAssetID)
	amount, err := money.ToBaseUnits(req.Amount, decimals)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid amount format")
		return
	}

	// Parse USD rate if provided (always use 8 decimals for USD)
	var usdRate *big.Int
	if req.USDRate != nil && *req.USDRate != "" {
		usdRate, err = money.ToBaseUnits(*req.USDRate, 8)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "invalid USD rate format")
			return
		}
	}

	// Build transaction data based on type
	txData := map[string]interface{}{
		"wallet_id":      walletID.String(),
		"asset_id":       req.AssetID,
		"price_asset_id": priceAssetID, // Used for price lookup
		"decimals":       decimals,     // Asset decimals for USD value calculation
		"amount":         amount.String(),
		"occurred_at":    occurredAt.Format(time.RFC3339),
		"notes":          req.Notes,
	}

	// Add USD rate if provided
	if usdRate != nil {
		txData["usd_rate"] = usdRate.String()
		txData["price_source"] = "manual"
	}

	// Merge additional data
	for k, v := range req.Data {
		if _, exists := txData[k]; !exists {
			txData[k] = v
		}
	}

	// Record transaction via ledger service
	transaction, err := h.ledgerService.RecordTransaction(r.Context(), txType, "manual", nil, occurredAt, txData)
	if err != nil {
		// Handle specific errors
		if err.Error() == "wallet not found" {
			respondWithError(w, http.StatusNotFound, "wallet not found")
			return
		}
		if err.Error() == "insufficient balance" {
			respondWithError(w, http.StatusBadRequest, "insufficient balance")
			return
		}
		if err.Error() == "transaction type not registered" || err.Error() == "transaction type not supported: handler not registered" {
			respondWithError(w, http.StatusBadRequest, "invalid transaction type")
			return
		}

		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create transaction: %v", err))
		return
	}

	// Convert to response
	response := toTransactionResponse(transaction)
	respondWithJSON(w, http.StatusCreated, response)
}

// GetTransactions handles GET /transactions
func (h *TransactionHandler) GetTransactions(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Parse query parameters
	query := r.URL.Query()

	// Pagination
	page, _ := strconv.Atoi(query.Get("page"))
	if page < 1 {
		page = 1
	}

	pageSize, _ := strconv.Atoi(query.Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	// Filters
	walletID := query.Get("wallet_id")
	txType := query.Get("type")
	startDate := query.Get("start_date")
	endDate := query.Get("end_date")

	// Build filter options
	filters := ledger.TransactionFilters{
		UserID: &userID,
		Limit:  pageSize,
		Offset: (page - 1) * pageSize,
	}

	if walletID != "" {
		walletUUID, err := uuid.Parse(walletID)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "invalid wallet_id")
			return
		}
		filters.WalletID = &walletUUID
	}

	if txType != "" {
		filters.Type = &txType
	}

	if startDate != "" {
		if _, err := time.Parse(time.RFC3339, startDate); err != nil {
			respondWithError(w, http.StatusBadRequest, "invalid start_date format (use RFC3339)")
			return
		}
		filters.FromDate = &startDate
	}

	if endDate != "" {
		if _, err := time.Parse(time.RFC3339, endDate); err != nil {
			respondWithError(w, http.StatusBadRequest, "invalid end_date format (use RFC3339)")
			return
		}
		filters.ToDate = &endDate
	}

	// Get enriched transactions via transaction service
	txns, err := h.transactionService.ListTransactions(r.Context(), filters)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to fetch transactions")
		return
	}

	// For now, total is just the length of results. In production, we'd need a count query
	total := len(txns)

	// Convert DTOs to response
	txResponses := make([]TransactionListItemResponse, len(txns))
	for i, tx := range txns {
		txResponses[i] = TransactionListItemResponse{
			ID:            tx.ID,
			Type:          tx.Type,
			TypeLabel:     tx.TypeLabel,
			AssetID:       tx.AssetID,
			AssetSymbol:   tx.AssetSymbol,
			Amount:        tx.Amount,
			DisplayAmount: tx.DisplayAmount,
			Direction:     tx.Direction,
			WalletID:      tx.WalletID,
			WalletName:    tx.WalletName,
			Status:        tx.Status,
			OccurredAt:    tx.OccurredAt,
			USDValue:      tx.USDValue,
		}
	}

	response := TransactionListResponse{
		Transactions: txResponses,
		Total:        total,
		Page:         page,
		PageSize:     pageSize,
	}

	respondWithJSON(w, http.StatusOK, response)
}

// GetTransaction handles GET /transactions/{id}
func (h *TransactionHandler) GetTransaction(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Parse transaction ID from URL
	txID := chi.URLParam(r, "id")
	if txID == "" {
		respondWithError(w, http.StatusBadRequest, "transaction ID is required")
		return
	}

	id, err := uuid.Parse(txID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid transaction ID")
		return
	}

	// Get transaction with authorization check
	detail, err := h.transactionService.GetTransaction(r.Context(), id, userID)
	if err != nil {
		// Return 404 for not found or unauthorized (to prevent ID enumeration)
		respondWithError(w, http.StatusNotFound, "transaction not found")
		return
	}

	respondWithJSON(w, http.StatusOK, detail)
}

// Helper function to convert domain transaction to response
func toTransactionResponse(tx *ledger.Transaction) TransactionResponse {
	return TransactionResponse{
		ID:           tx.ID.String(),
		Type:         tx.Type.String(),
		Source:       tx.Source,
		ExternalID:   tx.ExternalID,
		Status:       string(tx.Status),
		OccurredAt:   tx.OccurredAt.Format(time.RFC3339),
		RecordedAt:   tx.RecordedAt.Format(time.RFC3339),
		RawData:      tx.RawData,
		ErrorMessage: tx.ErrorMessage,
	}
}
