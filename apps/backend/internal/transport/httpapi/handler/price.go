package handler

import "net/http"

const (
	// HeaderPriceStale indicates that the returned price is from stale cache
	HeaderPriceStale = "X-Price-Stale"
)

// SetPriceStaleHeader sets the X-Price-Stale header if the price is stale
func SetPriceStaleHeader(w http.ResponseWriter, isStale bool) {
	if isStale {
		w.Header().Set(HeaderPriceStale, "true")
	}
}
