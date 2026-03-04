package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/platform/lendingposition"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
)

// LendingPositionServiceInterface defines lending position operations for the HTTP handler
type LendingPositionServiceInterface interface {
	GetByID(ctx context.Context, id uuid.UUID) (*lendingposition.LendingPosition, error)
	ListByUser(ctx context.Context, userID uuid.UUID, status *lendingposition.Status, walletID *uuid.UUID, chainID *string) ([]*lendingposition.LendingPosition, error)
}

// LendingPositionHandler handles lending position HTTP requests
type LendingPositionHandler struct {
	svc LendingPositionServiceInterface
}

// NewLendingPositionHandler creates a new lending position handler
func NewLendingPositionHandler(svc LendingPositionServiceInterface) *LendingPositionHandler {
	return &LendingPositionHandler{svc: svc}
}

// LendingPositionResponse represents a lending position in the API response
type LendingPositionResponse struct {
	ID       string `json:"id"`
	WalletID string `json:"wallet_id"`
	ChainID  string `json:"chain_id"`
	Protocol string `json:"protocol"`

	SupplyAsset    string `json:"supply_asset"`
	SupplyAmount   string `json:"supply_amount"`
	SupplyDecimals int    `json:"supply_decimals"`
	SupplyContract string `json:"supply_contract,omitempty"`

	BorrowAsset    string `json:"borrow_asset,omitempty"`
	BorrowAmount   string `json:"borrow_amount"`
	BorrowDecimals int    `json:"borrow_decimals,omitempty"`
	BorrowContract string `json:"borrow_contract,omitempty"`

	TotalSupplied  string `json:"total_supplied"`
	TotalWithdrawn string `json:"total_withdrawn"`
	TotalBorrowed  string `json:"total_borrowed"`
	TotalRepaid    string `json:"total_repaid"`

	TotalSuppliedUSD  string `json:"total_supplied_usd"`
	TotalWithdrawnUSD string `json:"total_withdrawn_usd"`
	TotalBorrowedUSD  string `json:"total_borrowed_usd"`
	TotalRepaidUSD    string `json:"total_repaid_usd"`

	InterestEarnedUSD string `json:"interest_earned_usd"`

	Status   string  `json:"status"`
	OpenedAt string  `json:"opened_at"`
	ClosedAt *string `json:"closed_at,omitempty"`
}

// ListPositions handles GET /lending/positions
func (h *LendingPositionHandler) ListPositions(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var statusFilter *lendingposition.Status
	if s := r.URL.Query().Get("status"); s != "" {
		st := lendingposition.Status(s)
		statusFilter = &st
	}

	var walletIDFilter *uuid.UUID
	if wid := r.URL.Query().Get("wallet_id"); wid != "" {
		parsed, err := uuid.Parse(wid)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "invalid wallet_id")
			return
		}
		walletIDFilter = &parsed
	}

	var chainIDFilter *string
	if cid := r.URL.Query().Get("chain_id"); cid != "" {
		chainIDFilter = &cid
	}

	positions, err := h.svc.ListByUser(r.Context(), userID, statusFilter, walletIDFilter, chainIDFilter)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to list lending positions")
		return
	}

	response := make([]LendingPositionResponse, len(positions))
	for i, pos := range positions {
		response[i] = toLendingPositionResponse(pos)
	}

	respondWithJSON(w, http.StatusOK, response)
}

// GetPosition handles GET /lending/positions/{id}
func (h *LendingPositionHandler) GetPosition(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := chi.URLParam(r, "id")
	posID, err := uuid.Parse(idStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid position ID")
		return
	}

	pos, err := h.svc.GetByID(r.Context(), posID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to get lending position")
		return
	}
	if pos == nil {
		respondWithError(w, http.StatusNotFound, "lending position not found")
		return
	}

	respondWithJSON(w, http.StatusOK, toLendingPositionResponse(pos))
}

func toLendingPositionResponse(pos *lendingposition.LendingPosition) LendingPositionResponse {
	resp := LendingPositionResponse{
		ID:       pos.ID.String(),
		WalletID: pos.WalletID.String(),
		ChainID:  pos.ChainID,
		Protocol: pos.Protocol,

		SupplyAsset:    pos.SupplyAsset,
		SupplyAmount:   bigIntStr(pos.SupplyAmount),
		SupplyDecimals: pos.SupplyDecimals,
		SupplyContract: pos.SupplyContract,

		BorrowAsset:    pos.BorrowAsset,
		BorrowAmount:   bigIntStr(pos.BorrowAmount),
		BorrowDecimals: pos.BorrowDecimals,
		BorrowContract: pos.BorrowContract,

		TotalSupplied:  bigIntStr(pos.TotalSupplied),
		TotalWithdrawn: bigIntStr(pos.TotalWithdrawn),
		TotalBorrowed:  bigIntStr(pos.TotalBorrowed),
		TotalRepaid:    bigIntStr(pos.TotalRepaid),

		TotalSuppliedUSD:  bigIntStr(pos.TotalSuppliedUSD),
		TotalWithdrawnUSD: bigIntStr(pos.TotalWithdrawnUSD),
		TotalBorrowedUSD:  bigIntStr(pos.TotalBorrowedUSD),
		TotalRepaidUSD:    bigIntStr(pos.TotalRepaidUSD),

		InterestEarnedUSD: bigIntStr(pos.InterestEarnedUSD),

		Status:   string(pos.Status),
		OpenedAt: pos.OpenedAt.Format(time.RFC3339),
	}

	if pos.ClosedAt != nil {
		s := pos.ClosedAt.Format(time.RFC3339)
		resp.ClosedAt = &s
	}

	return resp
}
