package lots

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
)

// maxRequestBodyBytes caps the JSON body size accepted by the manual-price endpoint.
// Defense-in-depth against memory-exhaustion DoS via slow/large POSTs.
const maxRequestBodyBytes = 64 * 1024 // 64 KiB

// Service is the interface the handler depends on.
type Service interface {
	SetManualPrice(ctx context.Context, userID uuid.UUID, lotID uuid.UUID, priceUSD string, reason string) error
}

// Handler handles HTTP requests for lot manual-price operations.
type Handler struct {
	svc Service
}

// NewHandler creates a new lots Handler.
func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

// setManualPriceRequest is the JSON body for PUT /lots/{id}/manual-price.
type setManualPriceRequest struct {
	PriceUSD string `json:"price_usd"`
	Reason   string `json:"reason"`
}

// SetManualPrice handles PUT /lots/{id}/manual-price.
// It allows a user to set a cost basis for lots that went unpriceable.
func (h *Handler) SetManualPrice(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	lotIDStr := chi.URLParam(r, "id")
	lotID, err := uuid.Parse(lotIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid lot ID")
		return
	}

	// Bound the request body. http.MaxBytesReader causes reads past the limit
	// to return an error of the form "http: request body too large". We surface
	// that as 413 Request Entity Too Large; anything else is 400 Bad Request.
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)

	var req setManualPriceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			respondError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.PriceUSD == "" {
		respondError(w, http.StatusBadRequest, "price_usd is required")
		return
	}

	if req.Reason == "" {
		respondError(w, http.StatusBadRequest, "reason is required")
		return
	}

	if len(req.Reason) > 1000 {
		respondError(w, http.StatusBadRequest, "reason must be 1000 characters or less")
		return
	}

	if err := h.svc.SetManualPrice(r.Context(), userID, lotID, req.PriceUSD, req.Reason); err != nil {
		switch {
		case errors.Is(err, ErrInvalidPrice):
			respondError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrLotNotFound):
			respondError(w, http.StatusNotFound, "tax lot not found")
		case errors.Is(err, ErrLotNotOwned):
			respondError(w, http.StatusForbidden, "access denied")
		default:
			respondError(w, http.StatusInternalServerError, "failed to set manual price")
		}
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "manual price applied"})
}

// respondError writes a JSON error response.
func respondError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	//nolint:errcheck
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// respondJSON writes a JSON success response.
func respondJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	//nolint:errcheck
	json.NewEncoder(w).Encode(data)
}
