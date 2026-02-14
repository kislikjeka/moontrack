package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
)

// WalletServiceInterface defines the interface for wallet operations
type WalletServiceInterface interface {
	Create(ctx context.Context, w *wallet.Wallet) (*wallet.Wallet, error)
	List(ctx context.Context, userID uuid.UUID) ([]*wallet.Wallet, error)
	GetByID(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*wallet.Wallet, error)
	Update(ctx context.Context, w *wallet.Wallet, userID uuid.UUID) (*wallet.Wallet, error)
	Delete(ctx context.Context, id uuid.UUID, userID uuid.UUID) error
}

// SyncServiceInterface defines the interface for wallet sync operations
type SyncServiceInterface interface {
	SyncWallet(ctx context.Context, walletID uuid.UUID) error
}

// WalletHandler handles wallet-related HTTP requests
type WalletHandler struct {
	walletService WalletServiceInterface
	syncService   SyncServiceInterface
}

// NewWalletHandler creates a new wallet handler
func NewWalletHandler(walletService WalletServiceInterface, syncService SyncServiceInterface) *WalletHandler {
	return &WalletHandler{
		walletService: walletService,
		syncService:   syncService,
	}
}

// CreateWalletRequest represents the wallet creation request
type CreateWalletRequest struct {
	Name    string `json:"name"`
	ChainID int64  `json:"chain_id"`
	Address string `json:"address"`
}

// UpdateWalletRequest represents the wallet update request
type UpdateWalletRequest struct {
	Name string `json:"name"`
}

// WalletResponse represents a wallet response
type WalletResponse struct {
	ID            string  `json:"id"`
	UserID        string  `json:"user_id"`
	Name          string  `json:"name"`
	ChainID       int64   `json:"chain_id"`
	ChainName     string  `json:"chain_name"`
	Address       string  `json:"address"`
	SyncStatus string  `json:"sync_status"`
	LastSyncAt *string `json:"last_sync_at,omitempty"`
	SyncError     *string `json:"sync_error,omitempty"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

// WalletsListResponse represents the response for listing wallets
type WalletsListResponse struct {
	Wallets []WalletResponse `json:"wallets"`
}

// CreateWallet handles POST /wallets
func (h *WalletHandler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context (set by JWT middleware)
	userID, ok := middleware.GetUserIDFromContext(r.Context())
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
	wlt := &wallet.Wallet{
		UserID:  userID,
		Name:    req.Name,
		ChainID: req.ChainID,
		Address: req.Address,
	}

	// Create wallet via service
	createdWallet, err := h.walletService.Create(r.Context(), wlt)
	if err != nil {
		if errors.Is(err, wallet.ErrDuplicateWalletName) {
			respondWithError(w, http.StatusConflict, "wallet name already exists")
			return
		}
		if errors.Is(err, wallet.ErrDuplicateAddress) {
			respondWithError(w, http.StatusConflict, "wallet address already exists for this chain")
			return
		}
		if errors.Is(err, wallet.ErrInvalidChainID) {
			respondWithError(w, http.StatusBadRequest, "invalid chain ID")
			return
		}
		if errors.Is(err, wallet.ErrInvalidAddress) {
			respondWithError(w, http.StatusBadRequest, "invalid EVM address format")
			return
		}
		if errors.Is(err, wallet.ErrMissingAddress) {
			respondWithError(w, http.StatusBadRequest, "wallet address is required")
			return
		}
		if errors.Is(err, wallet.ErrInvalidChecksum) {
			respondWithError(w, http.StatusBadRequest, "invalid EVM address checksum")
			return
		}
		if errors.Is(err, wallet.ErrUserNotFound) {
			respondWithError(w, http.StatusUnauthorized, "user not found, please re-login")
			return
		}
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create wallet: %v", err))
		return
	}

	// Convert to response
	response := toWalletResponse(createdWallet)
	respondWithJSON(w, http.StatusCreated, response)
}

// GetWallets handles GET /wallets
func (h *WalletHandler) GetWallets(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context
	userID, ok := middleware.GetUserIDFromContext(r.Context())
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
	for _, wlt := range wallets {
		responses = append(responses, toWalletResponse(wlt))
	}

	respondWithJSON(w, http.StatusOK, WalletsListResponse{Wallets: responses})
}

// GetWallet handles GET /wallets/{id}
func (h *WalletHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context
	userID, ok := middleware.GetUserIDFromContext(r.Context())
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
	wlt, err := h.walletService.GetByID(r.Context(), walletID, userID)
	if err != nil {
		if errors.Is(err, wallet.ErrWalletNotFound) {
			respondWithError(w, http.StatusNotFound, "wallet not found")
			return
		}
		if errors.Is(err, wallet.ErrUnauthorizedAccess) {
			respondWithError(w, http.StatusForbidden, "access denied")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to fetch wallet")
		return
	}

	// Convert to response
	response := toWalletResponse(wlt)
	respondWithJSON(w, http.StatusOK, response)
}

// UpdateWallet handles PUT /wallets/{id}
func (h *WalletHandler) UpdateWallet(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context
	userID, ok := middleware.GetUserIDFromContext(r.Context())
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

	// Create wallet domain object (only name can be updated)
	wlt := &wallet.Wallet{
		ID:   walletID,
		Name: req.Name,
	}

	// Update wallet via service
	updatedWallet, err := h.walletService.Update(r.Context(), wlt, userID)
	if err != nil {
		if errors.Is(err, wallet.ErrWalletNotFound) {
			respondWithError(w, http.StatusNotFound, "wallet not found")
			return
		}
		if errors.Is(err, wallet.ErrUnauthorizedAccess) {
			respondWithError(w, http.StatusForbidden, "access denied")
			return
		}
		if errors.Is(err, wallet.ErrDuplicateWalletName) {
			respondWithError(w, http.StatusConflict, "wallet name already exists")
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
	userID, ok := middleware.GetUserIDFromContext(r.Context())
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
		if errors.Is(err, wallet.ErrWalletNotFound) {
			respondWithError(w, http.StatusNotFound, "wallet not found")
			return
		}
		if errors.Is(err, wallet.ErrUnauthorizedAccess) {
			respondWithError(w, http.StatusForbidden, "access denied")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to delete wallet")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// TriggerSync handles POST /wallets/{id}/sync
func (h *WalletHandler) TriggerSync(w http.ResponseWriter, r *http.Request) {
	if h.syncService == nil {
		respondWithError(w, http.StatusServiceUnavailable, "sync service not available")
		return
	}

	// Get user ID from context
	userID, ok := middleware.GetUserIDFromContext(r.Context())
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

	// Verify wallet belongs to user
	_, err = h.walletService.GetByID(r.Context(), walletID, userID)
	if err != nil {
		if errors.Is(err, wallet.ErrWalletNotFound) {
			respondWithError(w, http.StatusNotFound, "wallet not found")
			return
		}
		if errors.Is(err, wallet.ErrUnauthorizedAccess) {
			respondWithError(w, http.StatusForbidden, "access denied")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to verify wallet ownership")
		return
	}

	// Trigger sync
	if err := h.syncService.SyncWallet(r.Context(), walletID); err != nil {
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("failed to trigger sync: %v", err))
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"status": "sync triggered"})
}

// Helper function to convert domain wallet to response
func toWalletResponse(wlt *wallet.Wallet) WalletResponse {
	resp := WalletResponse{
		ID:            wlt.ID.String(),
		UserID:        wlt.UserID.String(),
		Name:          wlt.Name,
		ChainID:       wlt.ChainID,
		ChainName:     wallet.GetChainName(wlt.ChainID),
		Address:       wlt.Address,
		SyncStatus: string(wlt.SyncStatus),
		SyncError:  wlt.SyncError,
		CreatedAt:     wlt.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:     wlt.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	if wlt.LastSyncAt != nil {
		syncAt := wlt.LastSyncAt.Format("2006-01-02T15:04:05Z07:00")
		resp.LastSyncAt = &syncAt
	}

	return resp
}
