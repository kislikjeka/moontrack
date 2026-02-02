package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/core/user/auth"
	"github.com/kislikjeka/moontrack/internal/modules/wallet/domain"
	"github.com/kislikjeka/moontrack/internal/modules/wallet/service"
)

// WalletHandler handles wallet-related HTTP requests
type WalletHandler struct {
	walletService *service.WalletService
}

// NewWalletHandler creates a new wallet handler
func NewWalletHandler(walletService *service.WalletService) *WalletHandler {
	return &WalletHandler{
		walletService: walletService,
	}
}

// CreateWalletRequest represents the wallet creation request
type CreateWalletRequest struct {
	Name    string  `json:"name"`
	ChainID string  `json:"chain_id"`
	Address *string `json:"address,omitempty"`
}

// UpdateWalletRequest represents the wallet update request
type UpdateWalletRequest struct {
	Name    string  `json:"name"`
	ChainID string  `json:"chain_id"`
	Address *string `json:"address,omitempty"`
}

// WalletResponse represents a wallet response
type WalletResponse struct {
	ID        string  `json:"id"`
	UserID    string  `json:"user_id"`
	Name      string  `json:"name"`
	ChainID   string  `json:"chain_id"`
	Address   *string `json:"address,omitempty"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

// CreateWallet handles POST /wallets
func (h *WalletHandler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context (set by JWT middleware)
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req CreateWalletRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Create wallet domain object
	wallet := &domain.Wallet{
		UserID:  userID,
		Name:    req.Name,
		ChainID: req.ChainID,
		Address: req.Address,
	}

	// Create wallet via service
	createdWallet, err := h.walletService.Create(r.Context(), wallet)
	if err != nil {
		if err == domain.ErrDuplicateWalletName {
			respondWithError(w, http.StatusConflict, "wallet name already exists")
			return
		}
		if err == domain.ErrInvalidChainID {
			respondWithError(w, http.StatusBadRequest, "invalid chain ID")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to create wallet")
		return
	}

	// Convert to response
	response := toWalletResponse(createdWallet)
	respondWithJSON(w, http.StatusCreated, response)
}

// GetWallets handles GET /wallets
func (h *WalletHandler) GetWallets(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Get wallets via service
	wallets, err := h.walletService.List(r.Context(), userID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to fetch wallets")
		return
	}

	// Convert to response
	responses := make([]WalletResponse, 0, len(wallets))
	for _, wallet := range wallets {
		responses = append(responses, toWalletResponse(wallet))
	}

	respondWithJSON(w, http.StatusOK, responses)
}

// GetWallet handles GET /wallets/{id}
func (h *WalletHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Get wallet ID from URL
	walletIDStr := chi.URLParam(r, "id")
	walletID, err := uuid.Parse(walletIDStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid wallet ID")
		return
	}

	// Get wallet via service
	wallet, err := h.walletService.GetByID(r.Context(), walletID, userID)
	if err != nil {
		if err == domain.ErrWalletNotFound {
			respondWithError(w, http.StatusNotFound, "wallet not found")
			return
		}
		if err == domain.ErrUnauthorizedAccess {
			respondWithError(w, http.StatusForbidden, "access denied")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to fetch wallet")
		return
	}

	// Convert to response
	response := toWalletResponse(wallet)
	respondWithJSON(w, http.StatusOK, response)
}

// UpdateWallet handles PUT /wallets/{id}
func (h *WalletHandler) UpdateWallet(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Get wallet ID from URL
	walletIDStr := chi.URLParam(r, "id")
	walletID, err := uuid.Parse(walletIDStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid wallet ID")
		return
	}

	var req UpdateWalletRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Create wallet domain object
	wallet := &domain.Wallet{
		ID:      walletID,
		Name:    req.Name,
		ChainID: req.ChainID,
		Address: req.Address,
	}

	// Update wallet via service
	updatedWallet, err := h.walletService.Update(r.Context(), wallet, userID)
	if err != nil {
		if err == domain.ErrWalletNotFound {
			respondWithError(w, http.StatusNotFound, "wallet not found")
			return
		}
		if err == domain.ErrUnauthorizedAccess {
			respondWithError(w, http.StatusForbidden, "access denied")
			return
		}
		if err == domain.ErrDuplicateWalletName {
			respondWithError(w, http.StatusConflict, "wallet name already exists")
			return
		}
		if err == domain.ErrInvalidChainID {
			respondWithError(w, http.StatusBadRequest, "invalid chain ID")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to update wallet")
		return
	}

	// Convert to response
	response := toWalletResponse(updatedWallet)
	respondWithJSON(w, http.StatusOK, response)
}

// DeleteWallet handles DELETE /wallets/{id}
func (h *WalletHandler) DeleteWallet(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context
	userID, ok := auth.GetUserIDFromContext(r.Context())
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Get wallet ID from URL
	walletIDStr := chi.URLParam(r, "id")
	walletID, err := uuid.Parse(walletIDStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid wallet ID")
		return
	}

	// Delete wallet via service
	if err := h.walletService.Delete(r.Context(), walletID, userID); err != nil {
		if err == domain.ErrWalletNotFound {
			respondWithError(w, http.StatusNotFound, "wallet not found")
			return
		}
		if err == domain.ErrUnauthorizedAccess {
			respondWithError(w, http.StatusForbidden, "access denied")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to delete wallet")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Helper function to convert domain wallet to response
func toWalletResponse(wallet *domain.Wallet) WalletResponse {
	return WalletResponse{
		ID:        wallet.ID.String(),
		UserID:    wallet.UserID.String(),
		Name:      wallet.Name,
		ChainID:   wallet.ChainID,
		Address:   wallet.Address,
		CreatedAt: wallet.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: wallet.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

// Helper functions for JSON responses
func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}

func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, map[string]string{"error": message})
}
