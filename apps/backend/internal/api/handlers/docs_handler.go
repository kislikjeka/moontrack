package handlers

import (
	"net/http"
)

// DocsHandler handles API documentation requests
type DocsHandler struct {
	specContent []byte
}

// NewDocsHandler creates a new docs handler
func NewDocsHandler(specContent []byte) *DocsHandler {
	return &DocsHandler{
		specContent: specContent,
	}
}

// GetOpenAPISpec handles GET /docs - returns the OpenAPI specification
func (h *DocsHandler) GetOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	w.Write(h.specContent)
}

// GetOpenAPIJSON handles GET /docs/info - returns the OpenAPI specification info
func (h *DocsHandler) GetOpenAPIJSON(w http.ResponseWriter, r *http.Request) {
	respondWithJSON(w, http.StatusOK, map[string]string{
		"title":       "MoonTrack Portfolio Tracker API",
		"version":     "1.0.0",
		"docs_url":    "/docs",
		"description": "REST API for cryptocurrency portfolio tracking with JWT authentication",
	})
}
