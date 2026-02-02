package handlers

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/core/ledger/domain"
	"github.com/kislikjeka/moontrack/internal/core/ledger/repository"
	ledgerService "github.com/kislikjeka/moontrack/internal/core/ledger/service"
	"github.com/kislikjeka/moontrack/internal/core/user/auth"
)

// TransactionHandler handles transaction-related HTTP requests
type TransactionHandler struct {
	ledgerService *ledgerService.LedgerService
}

// NewTransactionHandler creates a new transaction handler
func NewTransactionHandler(ledgerService *ledgerService.LedgerService) *TransactionHandler {
	return &TransactionHandler{
		ledgerService: ledgerService,
	}
}

// CreateTransactionRequest represents the transaction creation request
type CreateTransactionRequest struct {
	Type       string                 `json:"type"`        // manual_income, manual_outcome, asset_adjustment
	WalletID   string                 `json:"wallet_id"`
	AssetID    string                 `json:"asset_id"`
	Amount     string                 `json:"amount"`      // String representation of big.Int
	USDRate    *string                `json:"usd_rate,omitempty"` // Optional: manual USD rate (string representation of big.Int scaled by 10^8)
	OccurredAt string                 `json:"occurred_at"` // RFC3339 format
	Notes      string                 `json:"notes,omitempty"`
	Data       map[string]interface{} `json:"data,omitempty"` // Additional transaction-specific data
}

// TransactionResponse represents a transaction response
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

// TransactionListResponse represents a paginated list of transactions
type TransactionListResponse struct {
	Transactions []TransactionResponse `json:"transactions"`
	Total        int                   `json:"total"`
	Page         int                   `json:"page"`
	PageSize     int                   `json:"page_size"`
}

// CreateTransaction handles POST /transactions
func (h *TransactionHandler) CreateTransaction(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context (set by JWT middleware)
	_, ok := auth.GetUserIDFromContext(r.Context())
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

	// Parse wallet ID
	walletID, err := uuid.Parse(req.WalletID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid wallet ID")
		return
	}

	// Parse amount
	amount := new(big.Int)
	if _, ok := amount.SetString(req.Amount, 10); !ok {
		respondWithError(w, http.StatusBadRequest, "invalid amount format")
		return
	}

	// Parse USD rate if provided
	var usdRate *big.Int
	if req.USDRate != nil && *req.USDRate != "" {
		usdRate = new(big.Int)
		if _, ok := usdRate.SetString(*req.USDRate, 10); !ok {
			respondWithError(w, http.StatusBadRequest, "invalid USD rate format")
			return
		}
	}

	// Parse occurred_at timestamp
	occurredAt, err := time.Parse(time.RFC3339, req.OccurredAt)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid occurred_at format (use RFC3339)")
		return
	}

	// Build transaction data based on type
	txData := map[string]interface{}{
		"wallet_id":   walletID.String(),
		"asset_id":    req.AssetID,
		"amount":      amount.String(),
		"occurred_at": occurredAt.Format(time.RFC3339),
		"notes":       req.Notes,
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
	transaction, err := h.ledgerService.RecordTransaction(r.Context(), req.Type, "manual", nil, occurredAt, txData)
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
	userID, ok := auth.GetUserIDFromContext(r.Context())
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
	filters := repository.TransactionFilters{
		UserID: &userID,
		Limit:  pageSize,
		Offset: (page - 1) * pageSize,
	}

	if walletID != "" {
		_, err := uuid.Parse(walletID)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "invalid wallet_id")
			return
		}
		// Note: WalletID filtering would require adding to TransactionFilters struct
		// For now, we'll skip this filter or implement it in the repository layer
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

	// Get transactions via ledger service
	transactions, err := h.ledgerService.ListTransactions(r.Context(), filters)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to fetch transactions")
		return
	}

	// For now, total is just the length of results. In production, we'd need a count query
	total := len(transactions)

	// Convert to response
	txResponses := make([]TransactionResponse, len(transactions))
	for i, tx := range transactions {
		txResponses[i] = toTransactionResponse(tx)
	}

	response := TransactionListResponse{
		Transactions: txResponses,
		Total:        total,
		Page:         page,
		PageSize:     pageSize,
	}

	respondWithJSON(w, http.StatusOK, response)
}

// Helper function to convert domain transaction to response
func toTransactionResponse(tx *domain.Transaction) TransactionResponse {
	return TransactionResponse{
		ID:           tx.ID.String(),
		Type:         tx.Type,
		Source:       tx.Source,
		ExternalID:   tx.ExternalID,
		Status:       string(tx.Status),
		OccurredAt:   tx.OccurredAt.Format(time.RFC3339),
		RecordedAt:   tx.RecordedAt.Format(time.RFC3339),
		RawData:      tx.RawData,
		ErrorMessage: tx.ErrorMessage,
	}
}
