package handler

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/platform/assetregistry"
	"github.com/kislikjeka/moontrack/internal/platform/price"
)

// AssetPriceReader is the price side of these endpoints: priority-ordered reads
// over price_history, keyed on the asset_registry UUID. Declared at the
// consumer, so the handler names only the two reads it calls out of the wider
// PriceReader (#59).
type AssetPriceReader interface {
	Current(ctx context.Context, assetID uuid.UUID) (*big.Int, price.Source, error)
	CurrentBatch(ctx context.Context, assetIDs []uuid.UUID) (map[uuid.UUID]price.Quote, error)
	History(ctx context.Context, assetID uuid.UUID, from, to time.Time, bucket string) ([]price.HistoryPoint, error)
}

// AssetHandler handles asset-related HTTP requests.
//
// It used to sit on asset.Service, a single object that owned identity, the
// CoinGecko client and a price cache all at once. That service went with the
// `assets` table in #59, so the handler now holds the two collaborators it
// actually needs, separately: the registry for identity and the price reader
// for prices.
type AssetHandler struct {
	registry assetregistry.Reader
	prices   AssetPriceReader
}

// NewAssetHandler creates a new asset handler.
func NewAssetHandler(registry assetregistry.Reader, prices AssetPriceReader) *AssetHandler {
	return &AssetHandler{registry: registry, prices: prices}
}

// maxAssetResults bounds every list/search response. The registry grows with
// the identities users actually hold, so an unbounded list is a table scan
// served to the browser.
const maxAssetResults = 100

// maxSearchResults keeps the search response the size the frontend's picker
// expects (it was capped at 10 in the handler before #59).
const maxSearchResults = 10

// AssetResponse represents an asset in the API response.
//
// SHAPE FINALIZED BY #42 — the pre-#59 shape minus the fields that no longer
// have a source, plus the ambiguity flag. Dropped, deliberately not faked:
// asset_type (the registry holds only on-chain identities, so every row would be
// "crypto"), market_cap_rank (came from the CoinGecko catalogue the `assets`
// table mirrored) and is_active (no such column — a registry row is an identity
// someone held, not a catalogue entry that can be deactivated).
//
// chain_id and contract_address come straight off the registry row and are
// always present. contract_address carries the `native` sentinel verbatim for a
// chain's native coin: it is NOT translated back to an empty string or null,
// because that translation is one of the four inconsistent spellings of
// nativeness #59 removes.
//
// symbol_ambiguous tells the client whether `symbol` names this asset uniquely
// on its chain. It is the flag a picker needs to decide whether to qualify a
// ticker with a truncated contract, and it is computed over the whole registry
// rather than over this response — see assetregistry.Asset.SymbolAmbiguous.
type AssetResponse struct {
	ID              string `json:"id"`
	Symbol          string `json:"symbol"`
	Name            string `json:"name"`
	CoinGeckoID     string `json:"coingecko_id"`
	Decimals        int    `json:"decimals"`
	ChainID         string `json:"chain_id"`
	ContractAddress string `json:"contract_address"`
	SymbolAmbiguous bool   `json:"symbol_ambiguous"`
}

// PriceResponse represents a price in the API response.
//
// is_stale is gone with the cache that produced it: prices are now read from
// price_history through the priority-ordered reader, which has no staleness
// notion — it returns the best recorded observation or nothing. The
// X-Price-Stale header is likewise no longer set. Whether the contract should
// carry a freshness signal, and what would compute it, is #42's call.
type PriceResponse struct {
	AssetID   string `json:"asset_id"`
	PriceUSD  string `json:"price_usd"`
	Source    string `json:"source"`
	Timestamp string `json:"timestamp"`
}

// PriceHistoryResponse represents price history in the API response.
type PriceHistoryResponse struct {
	AssetID  string               `json:"asset_id"`
	From     string               `json:"from"`
	To       string               `json:"to"`
	Interval string               `json:"interval"`
	Prices   []PricePointResponse `json:"prices"`
}

// PricePointResponse represents a single price point.
type PricePointResponse struct {
	Timestamp string `json:"timestamp"`
	PriceUSD  string `json:"price_usd"`
}

// BatchPriceRequest represents a batch price lookup request.
type BatchPriceRequest struct {
	AssetIDs []string `json:"asset_ids"`
}

// BatchPriceResponse represents a batch price lookup response.
type BatchPriceResponse struct {
	Prices []PriceResponse `json:"prices"`
}

// GetAssetByID handles GET /api/v1/assets/{id}
func (h *AssetHandler) GetAssetByID(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid asset ID format")
		return
	}

	row, err := h.registry.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, assetregistry.ErrNotFound) {
			respondWithError(w, http.StatusNotFound, "asset not found")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to get asset")
		return
	}

	respondWithJSON(w, http.StatusOK, toAssetResponse(row))
}

// ListAssets handles GET /api/v1/assets?symbol=&chain=
//
// The pre-#59 handler branched into four service methods here (by symbol+chain,
// by symbol, by chain, all active). The registry serves all four as one
// filtered read, and there is no "active" subset to fall back to — see
// assetregistry.Reader.
func (h *AssetHandler) ListAssets(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	chain := r.URL.Query().Get("chain")

	rows, err := h.registry.List(r.Context(), symbol, chain, maxAssetResults)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to list assets")
		return
	}

	respondWithJSON(w, http.StatusOK, map[string][]AssetResponse{"assets": toAssetResponses(rows)})
}

// SearchAssets handles GET /api/v1/assets/search?q=query
//
// Registry only. The old SearchAssetsWithFallback queried CoinGecko when the
// local table came up short and wrote the results back as asset rows; that
// fallback is dropped in #59 and is not reinstated here — see
// the postgres Search implementation for why it is not expressible against a (chain,
// contract) identity, and #42 for where provider-backed discovery belongs.
func (h *AssetHandler) SearchAssets(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	switch {
	case query == "":
		respondWithError(w, http.StatusBadRequest, "search query (q) is required")
		return
	case len(query) < 2:
		respondWithError(w, http.StatusBadRequest, "search query must be at least 2 characters")
		return
	case len(query) > 50:
		respondWithError(w, http.StatusBadRequest, "search query must not exceed 50 characters")
		return
	}

	rows, err := h.registry.Search(r.Context(), query, maxSearchResults)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to search assets")
		return
	}

	respondWithJSON(w, http.StatusOK, map[string][]AssetResponse{"assets": toAssetResponses(rows)})
}

// GetAssetPrice handles GET /api/v1/assets/{id}/price
func (h *AssetHandler) GetAssetPrice(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid asset ID format")
		return
	}

	// Existence is checked against the registry first so an unknown asset and a
	// known-but-unpriced one are distinguishable: both would otherwise surface
	// as ErrNotFound from the price reader, and the caller could not tell a
	// typo'd UUID from an asset still waiting on the backfill worker.
	if _, err := h.registry.Get(r.Context(), id); err != nil {
		if errors.Is(err, assetregistry.ErrNotFound) {
			respondWithError(w, http.StatusNotFound, "asset not found")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to get price")
		return
	}

	p, source, err := h.prices.Current(r.Context(), id)
	if err != nil {
		if errors.Is(err, price.ErrNotFound) {
			respondWithError(w, http.StatusNotFound, "price not available")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to get price")
		return
	}

	respondWithJSON(w, http.StatusOK, PriceResponse{
		AssetID:  id.String(),
		PriceUSD: p.String(),
		Source:   string(source),
		// The reader answers with the winning price and its source, not the
		// row's own timestamp. "now" is the honest reading of a current-price
		// response; a point-in-time answer is what /history and the resolver
		// are for.
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// GetBatchPrices handles POST /api/v1/assets/prices
func (h *AssetHandler) GetBatchPrices(w http.ResponseWriter, r *http.Request) {
	var req BatchPriceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.AssetIDs) == 0 {
		respondWithJSON(w, http.StatusOK, BatchPriceResponse{Prices: []PriceResponse{}})
		return
	}
	if len(req.AssetIDs) > maxAssetResults {
		respondWithError(w, http.StatusBadRequest, "maximum 100 assets per request")
		return
	}

	// Deduplicated, because the pre-#59 read keyed its result by UUID and so
	// answered a repeated asset once. Order of first appearance is kept.
	ids := make([]uuid.UUID, 0, len(req.AssetIDs))
	seen := make(map[uuid.UUID]struct{}, len(req.AssetIDs))
	for _, idStr := range req.AssetIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			continue // Skip invalid UUIDs, as before #59.
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	quotes, err := h.prices.CurrentBatch(r.Context(), ids)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to get prices")
		return
	}

	// The response is built by walking the REQUEST order, not the map, so a
	// caller pairing the array with its own input gets a stable ordering
	// instead of Go's randomized map iteration.
	//
	// An asset with no recorded price is absent from the array rather than
	// present with a null price: the pre-#59 batch read returned only the
	// assets it had prices for, and a caller iterating the array must not
	// mistake "not priced yet" for a price.
	now := time.Now().UTC().Format(time.RFC3339)
	out := make([]PriceResponse, 0, len(quotes))
	for _, id := range ids {
		q, ok := quotes[id]
		if !ok {
			continue
		}
		out = append(out, PriceResponse{
			AssetID:   id.String(),
			PriceUSD:  q.PriceUSD.String(),
			Source:    string(q.Source),
			Timestamp: now,
		})
	}

	respondWithJSON(w, http.StatusOK, BatchPriceResponse{Prices: out})
}

// GetPriceHistory handles GET /api/v1/assets/{id}/history?from=&to=&interval=
func (h *AssetHandler) GetPriceHistory(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid asset ID format")
		return
	}

	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	intervalStr := r.URL.Query().Get("interval")

	if fromStr == "" || toStr == "" {
		respondWithError(w, http.StatusBadRequest, "from and to parameters are required")
		return
	}

	from, err := parseBoundary(fromStr, false)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid from date format (use RFC3339 or YYYY-MM-DD)")
		return
	}
	to, err := parseBoundary(toStr, true)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid to date format (use RFC3339 or YYYY-MM-DD)")
		return
	}

	if intervalStr == "" {
		intervalStr = "1d"
	}
	switch intervalStr {
	case "1h", "1d", "1w":
	default:
		respondWithError(w, http.StatusBadRequest, "interval must be 1h, 1d, or 1w")
		return
	}

	if to.Before(from) {
		respondWithError(w, http.StatusBadRequest, "invalid time range")
		return
	}
	const maxRange = 365 * 24 * time.Hour
	if to.Sub(from) > maxRange {
		respondWithError(w, http.StatusBadRequest, "time range cannot exceed 1 year")
		return
	}

	if _, err := h.registry.Get(r.Context(), id); err != nil {
		if errors.Is(err, assetregistry.ErrNotFound) {
			respondWithError(w, http.StatusNotFound, "asset not found")
			return
		}
		respondWithError(w, http.StatusInternalServerError, "failed to get price history")
		return
	}

	points, err := h.prices.History(r.Context(), id, from, to, intervalStr)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to get price history")
		return
	}

	prices := make([]PricePointResponse, len(points))
	for i, p := range points {
		prices[i] = PricePointResponse{
			Timestamp: p.Time.Format(time.RFC3339),
			PriceUSD:  p.PriceUSD.String(),
		}
	}

	respondWithJSON(w, http.StatusOK, PriceHistoryResponse{
		AssetID:  id.String(),
		From:     from.Format(time.RFC3339),
		To:       to.Format(time.RFC3339),
		Interval: intervalStr,
		Prices:   prices,
	})
}

// parseBoundary accepts RFC3339 or YYYY-MM-DD, as the pre-#59 handler did. A
// bare date used as the upper bound is stretched to the end of that day, so
// `to=2026-01-31` includes the 31st rather than stopping at its midnight.
func parseBoundary(s string, endOfDay bool) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, err
	}
	if endOfDay {
		t = t.Add(24*time.Hour - time.Second)
	}
	return t, nil
}

func toAssetResponses(rows []assetregistry.Asset) []AssetResponse {
	out := make([]AssetResponse, len(rows))
	for i := range rows {
		out[i] = toAssetResponse(&rows[i])
	}
	return out
}

// toAssetResponse converts a registry row to an API response.
func toAssetResponse(a *assetregistry.Asset) AssetResponse {
	return AssetResponse{
		ID:          a.ID.String(),
		Symbol:      a.Symbol,
		Name:        a.Name,
		CoinGeckoID: a.CoinGeckoID,
		Decimals:    a.Decimals,
		ChainID:     a.Chain,
		// Verbatim, including the `native` sentinel — see AssetResponse.
		ContractAddress: a.Contract,
		SymbolAmbiguous: a.SymbolAmbiguous,
	}
}
