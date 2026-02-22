package handler

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/taxlot"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// TaxLotServiceInterface defines the interface for tax lot operations.
type TaxLotServiceInterface interface {
	GetLotsByWallet(ctx context.Context, userID, walletID uuid.UUID, asset string) ([]*ledger.TaxLot, error)
	OverrideCostBasis(ctx context.Context, userID uuid.UUID, lotID uuid.UUID, costBasis *big.Int, reason string) error
	GetWAC(ctx context.Context, userID uuid.UUID, walletID *uuid.UUID) ([]taxlot.WACPosition, error)
	GetLotImpactByTransaction(ctx context.Context, userID, txID uuid.UUID) (*taxlot.TransactionLotImpact, error)
}

// TaxLotHandler handles tax lot HTTP requests.
type TaxLotHandler struct {
	taxLotService TaxLotServiceInterface
}

// NewTaxLotHandler creates a new TaxLotHandler.
func NewTaxLotHandler(taxLotService TaxLotServiceInterface) *TaxLotHandler {
	return &TaxLotHandler{taxLotService: taxLotService}
}

// --- Response types ---

// TaxLotResponse is the JSON representation of a tax lot.
type TaxLotResponse struct {
	ID                        string  `json:"id"`
	TransactionID             string  `json:"transaction_id"`
	AccountID                 string  `json:"account_id"`
	Asset                     string  `json:"asset"`
	QuantityAcquired          string  `json:"quantity_acquired"`
	QuantityRemaining         string  `json:"quantity_remaining"`
	AcquiredAt                string  `json:"acquired_at"`
	AutoCostBasisPerUnit      string  `json:"auto_cost_basis_per_unit"`
	AutoCostBasisSource       string  `json:"auto_cost_basis_source"`
	OverrideCostBasisPerUnit  *string `json:"override_cost_basis_per_unit,omitempty"`
	OverrideReason            *string `json:"override_reason,omitempty"`
	OverrideAt                *string `json:"override_at,omitempty"`
	EffectiveCostBasisPerUnit string  `json:"effective_cost_basis_per_unit"`
	LinkedSourceLotID         *string `json:"linked_source_lot_id,omitempty"`
}

// PositionWACResponse is the JSON representation of a WAC position.
type PositionWACResponse struct {
	WalletID        string `json:"wallet_id"`
	WalletName      string `json:"wallet_name"`
	AccountID       string `json:"account_id"`
	ChainID         string `json:"chain_id"`
	IsAggregated    bool   `json:"is_aggregated"`
	Asset           string `json:"asset"`
	TotalQuantity   string `json:"total_quantity"`
	WeightedAvgCost string `json:"weighted_avg_cost"`
}

// --- Envelope types ---

// TaxLotsListResponse is the JSON envelope for listing tax lots.
type TaxLotsListResponse struct {
	Lots []TaxLotResponse `json:"lots"`
}

// WACPositionsResponse is the JSON envelope for WAC positions.
type WACPositionsResponse struct {
	Positions []PositionWACResponse `json:"positions"`
}

// TransactionLotImpactResponse is the JSON envelope for transaction lot impact.
type TransactionLotImpactResponse struct {
	AcquiredLots []TaxLotResponse        `json:"acquired_lots"`
	Disposals    []DisposalDetailResponse `json:"disposals"`
	HasLotImpact bool                     `json:"has_lot_impact"`
}

// DisposalDetailResponse is the JSON representation of a disposal with lot metadata.
type DisposalDetailResponse struct {
	ID               string `json:"id"`
	LotID            string `json:"lot_id"`
	QuantityDisposed string `json:"quantity_disposed"`
	ProceedsPerUnit  string `json:"proceeds_per_unit"`
	DisposalType     string `json:"disposal_type"`
	DisposedAt       string `json:"disposed_at"`
	LotAsset         string `json:"lot_asset"`
	LotAcquiredAt    string `json:"lot_acquired_at"`
	LotCostBasis     string `json:"lot_cost_basis_per_unit"`
	LotAutoSource    string `json:"lot_auto_cost_basis_source"`
}

// --- Request types ---

// OverrideCostBasisRequest is the JSON request body for overriding cost basis.
type OverrideCostBasisRequest struct {
	CostBasisPerUnit string `json:"cost_basis_per_unit"`
	Reason           string `json:"reason"`
}

// --- Handlers ---

// GetLots handles GET /lots?wallet_id={id}&asset={asset}
func (h *TaxLotHandler) GetLots(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	walletIDStr := r.URL.Query().Get("wallet_id")
	if walletIDStr == "" {
		respondWithError(w, http.StatusBadRequest, "wallet_id is required")
		return
	}
	walletID, err := uuid.Parse(walletIDStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid wallet_id")
		return
	}

	asset := r.URL.Query().Get("asset")
	if asset == "" {
		respondWithError(w, http.StatusBadRequest, "asset is required")
		return
	}

	lots, err := h.taxLotService.GetLotsByWallet(r.Context(), userID, walletID, asset)
	if err != nil {
		if errors.Is(err, taxlot.ErrWalletNotOwned) {
			respondWithError(w, http.StatusForbidden, "access denied")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to get tax lots")
		return
	}

	response := make([]TaxLotResponse, 0, len(lots))
	for _, lot := range lots {
		response = append(response, toTaxLotResponse(lot))
	}

	respondWithJSON(w, http.StatusOK, TaxLotsListResponse{Lots: response})
}

// OverrideCostBasis handles PUT /lots/{id}/override
func (h *TaxLotHandler) OverrideCostBasis(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	lotIDStr := chi.URLParam(r, "id")
	lotID, err := uuid.Parse(lotIDStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid lot ID")
		return
	}

	var req OverrideCostBasisRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.CostBasisPerUnit == "" {
		respondWithError(w, http.StatusBadRequest, "cost_basis_per_unit is required")
		return
	}
	if req.Reason == "" {
		respondWithError(w, http.StatusBadRequest, "reason is required")
		return
	}

	if len(req.Reason) > 1000 {
		respondWithError(w, http.StatusBadRequest, "reason must be 1000 characters or less")
		return
	}

	// Convert USD string (e.g., "1.80") to big.Int scaled 10^8
	costBasis, err := money.ToBaseUnits(req.CostBasisPerUnit, 8)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid cost_basis_per_unit format")
		return
	}

	if costBasis.Sign() < 0 {
		respondWithError(w, http.StatusBadRequest, "cost_basis_per_unit must be non-negative")
		return
	}

	if err := h.taxLotService.OverrideCostBasis(r.Context(), userID, lotID, costBasis, req.Reason); err != nil {
		if errors.Is(err, taxlot.ErrLotNotOwned) {
			respondWithError(w, http.StatusForbidden, "access denied")
			return
		}
		if errors.Is(err, taxlot.ErrLotNotFound) {
			respondWithError(w, http.StatusNotFound, "tax lot not found")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to override cost basis")
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"status": "override applied"})
}

// GetWAC handles GET /positions/wac?wallet_id={id}
func (h *TaxLotHandler) GetWAC(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var walletID *uuid.UUID
	if walletIDStr := r.URL.Query().Get("wallet_id"); walletIDStr != "" {
		id, err := uuid.Parse(walletIDStr)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "invalid wallet_id")
			return
		}
		walletID = &id
	}

	positions, err := h.taxLotService.GetWAC(r.Context(), userID, walletID)
	if err != nil {
		if errors.Is(err, taxlot.ErrWalletNotOwned) {
			respondWithError(w, http.StatusForbidden, "access denied")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to get WAC positions")
		return
	}

	response := make([]PositionWACResponse, 0, len(positions))
	for _, p := range positions {
		decimals := money.GetDecimals(p.Asset)
		response = append(response, PositionWACResponse{
			WalletID:        p.WalletID.String(),
			WalletName:      p.WalletName,
			AccountID:       p.AccountID.String(),
			ChainID:         p.ChainID,
			IsAggregated:    p.AccountID == uuid.Nil,
			Asset:           p.Asset,
			TotalQuantity:   money.FromBaseUnits(p.TotalQuantity, decimals),
			WeightedAvgCost: money.FormatUSD(p.WeightedAvgCost),
		})
	}

	respondWithJSON(w, http.StatusOK, WACPositionsResponse{Positions: response})
}

// GetTransactionLots handles GET /transactions/{id}/lots
func (h *TaxLotHandler) GetTransactionLots(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		respondWithError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	txIDStr := chi.URLParam(r, "id")
	txID, err := uuid.Parse(txIDStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid transaction ID")
		return
	}

	impact, err := h.taxLotService.GetLotImpactByTransaction(r.Context(), userID, txID)
	if err != nil {
		if errors.Is(err, taxlot.ErrLotNotOwned) {
			respondWithError(w, http.StatusForbidden, "access denied")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to get transaction lots")
		return
	}

	acquiredLots := make([]TaxLotResponse, 0, len(impact.AcquiredLots))
	for _, lot := range impact.AcquiredLots {
		acquiredLots = append(acquiredLots, toTaxLotResponse(lot))
	}

	disposals := make([]DisposalDetailResponse, 0, len(impact.Disposals))
	for _, d := range impact.Disposals {
		decimals := money.GetDecimals(d.LotAsset)
		disposals = append(disposals, DisposalDetailResponse{
			ID:               d.ID.String(),
			LotID:            d.LotID.String(),
			QuantityDisposed: money.FromBaseUnits(d.QuantityDisposed, decimals),
			ProceedsPerUnit:  money.FormatUSD(d.ProceedsPerUnit),
			DisposalType:     string(d.DisposalType),
			DisposedAt:       d.DisposedAt.Format("2006-01-02T15:04:05Z07:00"),
			LotAsset:         d.LotAsset,
			LotAcquiredAt:    d.LotAcquiredAt.Format("2006-01-02T15:04:05Z07:00"),
			LotCostBasis:     money.FormatUSD(d.LotEffectiveCostBasisPerUnit),
			LotAutoSource:    string(d.LotAutoSource),
		})
	}

	respondWithJSON(w, http.StatusOK, TransactionLotImpactResponse{
		AcquiredLots: acquiredLots,
		Disposals:    disposals,
		HasLotImpact: impact.HasLotImpact,
	})
}

// --- Helpers ---

func toTaxLotResponse(lot *ledger.TaxLot) TaxLotResponse {
	decimals := money.GetDecimals(lot.Asset)

	resp := TaxLotResponse{
		ID:                        lot.ID.String(),
		TransactionID:             lot.TransactionID.String(),
		AccountID:                 lot.AccountID.String(),
		Asset:                     lot.Asset,
		QuantityAcquired:          money.FromBaseUnits(lot.QuantityAcquired, decimals),
		QuantityRemaining:         money.FromBaseUnits(lot.QuantityRemaining, decimals),
		AcquiredAt:                lot.AcquiredAt.Format("2006-01-02T15:04:05Z07:00"),
		AutoCostBasisPerUnit:      money.FormatUSD(lot.AutoCostBasisPerUnit),
		AutoCostBasisSource:       string(lot.AutoCostBasisSource),
		EffectiveCostBasisPerUnit: money.FormatUSD(lot.EffectiveCostBasisPerUnit()),
	}

	if lot.OverrideCostBasisPerUnit != nil {
		formatted := money.FormatUSD(lot.OverrideCostBasisPerUnit)
		resp.OverrideCostBasisPerUnit = &formatted
	}

	if lot.OverrideReason != nil {
		resp.OverrideReason = lot.OverrideReason
	}

	if lot.OverrideAt != nil {
		at := lot.OverrideAt.Format("2006-01-02T15:04:05Z07:00")
		resp.OverrideAt = &at
	}

	if lot.LinkedSourceLotID != nil {
		id := lot.LinkedSourceLotID.String()
		resp.LinkedSourceLotID = &id
	}

	return resp
}
