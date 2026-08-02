package handler

import (
	"context"
	"log/slog"
	"math/big"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/platform/lpposition"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// LPPositionServiceInterface defines LP position operations for the HTTP handler
type LPPositionServiceInterface interface {
	GetByID(ctx context.Context, id uuid.UUID) (*lpposition.LPPosition, error)
	ListByUser(ctx context.Context, userID uuid.UUID, status *lpposition.Status, walletID *uuid.UUID, chainID *string) ([]*lpposition.LPPosition, error)
}

// LPPositionHandler handles LP position HTTP requests
type LPPositionHandler struct {
	svc LPPositionServiceInterface
}

// NewLPPositionHandler creates a new LP position handler
func NewLPPositionHandler(svc LPPositionServiceInterface) *LPPositionHandler {
	return &LPPositionHandler{svc: svc}
}

// LPPositionResponse represents a single LP position in the API response
type LPPositionResponse struct {
	ID              string `json:"id"`
	WalletID        string `json:"wallet_id"`
	ChainID         string `json:"chain_id"`
	Protocol        string `json:"protocol"`
	NFTTokenID      string `json:"nft_token_id,omitempty"`
	ContractAddress string `json:"contract_address,omitempty"`

	// The pair is reported as two asset_registry ids (#59). Symbol, contract
	// and decimals used to be denormalized onto the position row and are now
	// attributes of the registry row each id names — a second copy here could
	// only ever disagree with it, and a native token in a pair was spelled as
	// an empty contract, which made "which token is this" ambiguous. Resolving
	// these ids to display attributes for the API response is #42's work.
	Token0AssetID string `json:"token0_asset_id"`
	Token1AssetID string `json:"token1_asset_id"`

	TotalDepositedUSD   string `json:"total_deposited_usd"`
	TotalWithdrawnUSD   string `json:"total_withdrawn_usd"`
	TotalClaimedFeesUSD string `json:"total_claimed_fees_usd"`

	TotalDepositedToken0 string `json:"total_deposited_token0"`
	TotalDepositedToken1 string `json:"total_deposited_token1"`
	TotalWithdrawnToken0 string `json:"total_withdrawn_token0"`
	TotalWithdrawnToken1 string `json:"total_withdrawn_token1"`
	TotalClaimedToken0   string `json:"total_claimed_token0"`
	TotalClaimedToken1   string `json:"total_claimed_token1"`

	RemainingToken0 string `json:"remaining_token0"`
	RemainingToken1 string `json:"remaining_token1"`

	Status   string  `json:"status"`
	OpenedAt string  `json:"opened_at"`
	ClosedAt *string `json:"closed_at,omitempty"`

	RealizedPnLUSD *string `json:"realized_pnl_usd,omitempty"`
	APRBps         *int    `json:"apr_bps,omitempty"`
}

// ListPositions handles GET /lp/positions
func (h *LPPositionHandler) ListPositions(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Parse optional filters
	var statusFilter *lpposition.Status
	if s := r.URL.Query().Get("status"); s != "" {
		st := lpposition.Status(s)
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
		slog.Error("failed to list LP positions", "error", err, "user_id", userID)
		respondWithError(w, http.StatusInternalServerError, "failed to list LP positions")
		return
	}

	response := make([]LPPositionResponse, len(positions))
	for i, pos := range positions {
		response[i] = toLPPositionResponse(pos)
	}

	respondWithJSON(w, http.StatusOK, response)
}

// GetPosition handles GET /lp/positions/{id}
func (h *LPPositionHandler) GetPosition(w http.ResponseWriter, r *http.Request) {
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
		slog.Error("failed to get LP position", "error", err, "position_id", posID)
		respondWithError(w, http.StatusInternalServerError, "failed to get LP position")
		return
	}
	if pos == nil {
		respondWithError(w, http.StatusNotFound, "LP position not found")
		return
	}

	respondWithJSON(w, http.StatusOK, toLPPositionResponse(pos))
}

func toLPPositionResponse(pos *lpposition.LPPosition) LPPositionResponse {
	resp := LPPositionResponse{
		ID:              pos.ID.String(),
		WalletID:        pos.WalletID.String(),
		ChainID:         pos.ChainID,
		Protocol:        pos.Protocol,
		NFTTokenID:      pos.NFTTokenID,
		ContractAddress: pos.ContractAddress,

		Token0AssetID: pos.Token0AssetID.String(),
		Token1AssetID: pos.Token1AssetID.String(),

		TotalDepositedUSD:   money.FormatUSD(pos.TotalDepositedUSD),
		TotalWithdrawnUSD:   money.FormatUSD(pos.TotalWithdrawnUSD),
		TotalClaimedFeesUSD: money.FormatUSD(pos.TotalClaimedFeesUSD),

		TotalDepositedToken0: bigIntStr(pos.TotalDepositedToken0),
		TotalDepositedToken1: bigIntStr(pos.TotalDepositedToken1),
		TotalWithdrawnToken0: bigIntStr(pos.TotalWithdrawnToken0),
		TotalWithdrawnToken1: bigIntStr(pos.TotalWithdrawnToken1),
		TotalClaimedToken0:   bigIntStr(pos.TotalClaimedToken0),
		TotalClaimedToken1:   bigIntStr(pos.TotalClaimedToken1),

		RemainingToken0: bigIntStr(pos.RemainingToken0()),
		RemainingToken1: bigIntStr(pos.RemainingToken1()),

		Status:   string(pos.Status),
		OpenedAt: pos.OpenedAt.Format(time.RFC3339),
		APRBps:   pos.APRBps,
	}

	if pos.ClosedAt != nil {
		s := pos.ClosedAt.Format(time.RFC3339)
		resp.ClosedAt = &s
	}
	if pos.RealizedPnLUSD != nil {
		s := money.FormatUSD(pos.RealizedPnLUSD)
		resp.RealizedPnLUSD = &s
	}

	return resp
}

func bigIntStr(v *big.Int) string {
	if v == nil {
		return "0"
	}
	return v.String()
}
