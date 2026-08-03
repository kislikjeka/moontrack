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
	"github.com/kislikjeka/moontrack/internal/platform/assetregistry"
	"github.com/kislikjeka/moontrack/internal/platform/taxlot"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// TaxLotServiceInterface defines the interface for tax lot operations.
type TaxLotServiceInterface interface {
	GetLotsByWallet(ctx context.Context, userID, walletID uuid.UUID, asset uuid.UUID, chainID string) ([]*ledger.TaxLot, error)
	OverrideCostBasis(ctx context.Context, userID uuid.UUID, lotID uuid.UUID, costBasis *big.Int, reason string) error
	GetWAC(ctx context.Context, userID uuid.UUID, walletID *uuid.UUID) ([]taxlot.WACPosition, error)
	GetLotImpactByTransaction(ctx context.Context, userID, txID uuid.UUID) (*taxlot.TransactionLotImpact, error)
}

// TaxLotHandler handles tax lot HTTP requests.
type TaxLotHandler struct {
	taxLotService TaxLotServiceInterface
	assets        assetregistry.Reader   // nilable — see describeAssets
	resolver      *money.DecimalResolver // fallback when assets is unwired
}

// NewTaxLotHandler creates a new TaxLotHandler.
//
// assets is the registry reader rather than the narrower decimals-only port the
// tax-lot service uses. The endpoint needs two things off the same row — the
// scale to render quantities by and the ticker to label them with (#42) — and a
// port that answers only "decimals" cannot supply the second. Splitting them
// across two lookups is what let the ticker and the scale describe different
// assets before #59.
//
// Lot quantities are base-unit integers, so the wrong scale misplaces a decimal
// point in every quantity the endpoint renders — which is why it is a registry
// read and not a ticker table lookup.
func NewTaxLotHandler(taxLotService TaxLotServiceInterface, assets assetregistry.Reader, resolver *money.DecimalResolver) *TaxLotHandler {
	return &TaxLotHandler{taxLotService: taxLotService, assets: assets, resolver: resolver}
}

// --- Response types ---

// TaxLotResponse is the JSON representation of a tax lot.
//
// Asset is the registry UUID. AssetSymbol, AssetContract and SymbolAmbiguous
// ride alongside as presentation (#42): before them the endpoint shipped a bare
// UUID and nothing else, so a client could either render the UUID at the user or
// issue one /assets lookup per lot to find the ticker.
type TaxLotResponse struct {
	ID                        string  `json:"id"`
	TransactionID             string  `json:"transaction_id"`
	AccountID                 string  `json:"account_id"`
	ChainID                   string  `json:"chain_id,omitempty"`
	Asset                     string  `json:"asset"`
	AssetSymbol               string  `json:"asset_symbol"`
	AssetContract             string  `json:"asset_contract"`
	SymbolAmbiguous           bool    `json:"symbol_ambiguous"`
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
	AssetSymbol     string `json:"asset_symbol"`
	AssetContract   string `json:"asset_contract"`
	SymbolAmbiguous bool   `json:"symbol_ambiguous"`
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
	AcquiredLots []TaxLotResponse         `json:"acquired_lots"`
	Disposals    []DisposalDetailResponse `json:"disposals"`
	HasLotImpact bool                     `json:"has_lot_impact"`
}

// DisposalDetailResponse is the JSON representation of a disposal with lot metadata.
type DisposalDetailResponse struct {
	ID               string `json:"id"`
	LotID            string `json:"lot_id"`
	QuantityDisposed string `json:"quantity_disposed"`
	ProceedsPerUnit  string `json:"proceeds_per_unit"`
	// ProceedsStatus is "resolved", "pending" or "unpriceable". Clients
	// should treat the disposal as having no proceeds-driven PnL unless
	// ProceedsStatus == "resolved".
	ProceedsStatus   string `json:"proceeds_status"`
	DisposalType     string `json:"disposal_type"`
	DisposedAt       string `json:"disposed_at"`
	LotAsset         string `json:"lot_asset"`
	LotAssetSymbol   string `json:"lot_asset_symbol"`
	LotAssetContract string `json:"lot_asset_contract"`
	SymbolAmbiguous  bool   `json:"symbol_ambiguous"`
	LotAcquiredAt    string `json:"lot_acquired_at"`
	LotCostBasis     string `json:"lot_cost_basis_per_unit"`
	LotAutoSource    string `json:"lot_auto_cost_basis_source"`
	RealizedGainLoss string `json:"realized_gain_loss"`
	// PnLExcluded is true when RealizedGainLoss is not meaningful (e.g.,
	// the disposal is still pending price resolution).
	PnLExcluded bool `json:"pnl_excluded"`
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

	// asset is a registry UUID (#59). It used to be a ticker, which meant
	// "?asset=USDC" returned the lots of whichever USDC the query matched. A
	// malformed id is rejected here rather than passed on as uuid.Nil, which
	// would return an empty list that looks like "you hold none of this".
	assetParam := r.URL.Query().Get("asset")
	if assetParam == "" {
		respondWithError(w, http.StatusBadRequest, "asset is required")
		return
	}
	asset, err := uuid.Parse(assetParam)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "asset must be a valid asset UUID")
		return
	}

	chainID := r.URL.Query().Get("chain_id")

	lots, err := h.taxLotService.GetLotsByWallet(r.Context(), userID, walletID, asset, chainID)
	if err != nil {
		if errors.Is(err, taxlot.ErrWalletNotOwned) {
			respondWithError(w, http.StatusForbidden, "access denied")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to get tax lots")
		return
	}

	assetIDs := make([]uuid.UUID, 0, len(lots))
	for _, lot := range lots {
		assetIDs = append(assetIDs, lot.Asset)
	}
	desc := h.describeAssets(r.Context(), assetIDs)

	response := make([]TaxLotResponse, 0, len(lots))
	for _, lot := range lots {
		response = append(response, toTaxLotResponse(lot, desc.of(lot.Asset)))
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

	assetIDs := make([]uuid.UUID, 0, len(positions))
	for _, p := range positions {
		assetIDs = append(assetIDs, p.Asset)
	}
	described := h.describeAssets(r.Context(), assetIDs)

	response := make([]PositionWACResponse, 0, len(positions))
	for _, p := range positions {
		desc := described.of(p.Asset)
		response = append(response, PositionWACResponse{
			WalletID:        p.WalletID.String(),
			WalletName:      p.WalletName,
			AccountID:       p.AccountID.String(),
			ChainID:         p.ChainID,
			IsAggregated:    p.AccountID == uuid.Nil,
			Asset:           p.Asset.String(),
			AssetSymbol:     desc.symbol,
			AssetContract:   desc.contract,
			SymbolAmbiguous: desc.ambiguous,
			TotalQuantity:   money.FromBaseUnits(p.TotalQuantity, desc.decimals),
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

	// Both lists in one read — the disposals name the same assets as the lots
	// they came from more often than not.
	assetIDs := make([]uuid.UUID, 0, len(impact.AcquiredLots)+len(impact.Disposals))
	for _, lot := range impact.AcquiredLots {
		assetIDs = append(assetIDs, lot.Asset)
	}
	for _, d := range impact.Disposals {
		assetIDs = append(assetIDs, d.LotAsset)
	}
	described := h.describeAssets(r.Context(), assetIDs)

	acquiredLots := make([]TaxLotResponse, 0, len(impact.AcquiredLots))
	for _, lot := range impact.AcquiredLots {
		acquiredLots = append(acquiredLots, toTaxLotResponse(lot, described.of(lot.Asset)))
	}

	disposals := make([]DisposalDetailResponse, 0, len(impact.Disposals))
	for _, d := range impact.Disposals {
		desc := described.of(d.LotAsset)
		status := string(d.ProceedsStatus)
		if status == "" {
			status = "resolved"
		}
		disposals = append(disposals, DisposalDetailResponse{
			ID:               d.ID.String(),
			LotID:            d.LotID.String(),
			QuantityDisposed: money.FromBaseUnits(d.QuantityDisposed, desc.decimals),
			ProceedsPerUnit:  money.FormatUSD(d.ProceedsPerUnit),
			ProceedsStatus:   status,
			DisposalType:     string(d.DisposalType),
			DisposedAt:       d.DisposedAt.Format("2006-01-02T15:04:05Z07:00"),
			LotAsset:         d.LotAsset.String(),
			LotAssetSymbol:   desc.symbol,
			LotAssetContract: desc.contract,
			SymbolAmbiguous:  desc.ambiguous,
			LotAcquiredAt:    d.LotAcquiredAt.Format("2006-01-02T15:04:05Z07:00"),
			LotCostBasis:     money.FormatUSD(d.LotEffectiveCostBasisPerUnit),
			LotAutoSource:    string(d.LotAutoSource),
			RealizedGainLoss: money.FormatUSD(d.RealizedGainLoss),
			PnLExcluded:      d.RealizedGainLoss == nil || status != "resolved",
		})
	}

	respondWithJSON(w, http.StatusOK, TransactionLotImpactResponse{
		AcquiredLots: acquiredLots,
		Disposals:    disposals,
		HasLotImpact: impact.HasLotImpact,
	})
}

// assetDescription is how a lot's asset is rendered: the scale for its
// quantities and the label for its identity.
type assetDescription struct {
	symbol    string
	contract  string
	ambiguous bool
	decimals  int
}

// assetDescriber resolves the assets of one response in a single registry read.
//
// Every endpoint here renders a LIST, so the ids are all known before the first
// row is formatted. Reading them one at a time would issue N queries, each
// carrying the registry-wide window that computes the ambiguity flag; resolving
// them together pays that once. The zero value is usable and answers every id
// with the degraded description, which is what a handler built without a
// registry (tests) needs.
type assetDescriber struct {
	byID     map[uuid.UUID]assetregistry.Asset
	fallback assetDescription
}

// describeAssets resolves every id in one read.
//
// A registry error is not fatal: the endpoint degrades to unlabelled rows rather
// than failing outright, because the quantities and cost bases it exists to
// report do not depend on the registry.
func (h *TaxLotHandler) describeAssets(ctx context.Context, ids []uuid.UUID) assetDescriber {
	d := assetDescriber{fallback: h.fallbackDescription(ctx)}
	if h.assets == nil || len(ids) == 0 {
		return d
	}
	if byID, err := h.assets.GetMany(ctx, ids); err == nil {
		d.byID = byID
	}
	return d
}

// of returns the description for one id.
//
// A miss leaves the symbol EMPTY rather than filling it with the UUID's string
// form: a UUID where a ticker belongs is worse than a blank, because it reads as
// data. The scale then falls back to the resolver's default rather than a
// UUID-shaped lookup miss.
func (d assetDescriber) of(assetID uuid.UUID) assetDescription {
	a, ok := d.byID[assetID]
	if !ok {
		return d.fallback
	}
	return assetDescription{
		symbol:    a.Symbol,
		contract:  a.Contract,
		ambiguous: a.SymbolAmbiguous,
		decimals:  a.Decimals,
	}
}

// fallbackDescription is what an unresolvable asset renders as.
func (h *TaxLotHandler) fallbackDescription(ctx context.Context) assetDescription {
	if h.resolver != nil {
		return assetDescription{decimals: h.resolver.ResolveSymbolOnly(ctx, "")}
	}
	return assetDescription{decimals: money.GetDecimals("")}
}

// --- Helpers ---

func toTaxLotResponse(lot *ledger.TaxLot, desc assetDescription) TaxLotResponse {
	decimals := desc.decimals

	resp := TaxLotResponse{
		ID:                        lot.ID.String(),
		TransactionID:             lot.TransactionID.String(),
		AccountID:                 lot.AccountID.String(),
		ChainID:                   lot.ChainID,
		Asset:                     lot.Asset.String(),
		AssetSymbol:               desc.symbol,
		AssetContract:             desc.contract,
		SymbolAmbiguous:           desc.ambiguous,
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
