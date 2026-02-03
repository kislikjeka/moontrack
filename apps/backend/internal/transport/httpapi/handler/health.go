package handler

import (
	"context"
	"net/http"
	"time"
)

// DatabasePinger defines the interface for checking database connectivity
type DatabasePinger interface {
	Ping(ctx context.Context) error
}

// HealthHandler handles health check requests
type HealthHandler struct {
	db DatabasePinger
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(db DatabasePinger) *HealthHandler {
	return &HealthHandler{
		db: db,
	}
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status  string            `json:"status"`
	Version string            `json:"version"`
	Checks  map[string]string `json:"checks"`
	Uptime  string            `json:"uptime,omitempty"`
}

var startTime = time.Now()

// GetHealth handles GET /health
// Basic health check - returns 200 OK if service is running
func GetHealth(w http.ResponseWriter, r *http.Request) {
	response := HealthResponse{
		Status:  "ok",
		Version: "1.0.0",
		Uptime:  time.Since(startTime).String(),
		Checks:  map[string]string{},
	}

	respondWithJSON(w, http.StatusOK, response)
}

// GetHealthDetailed handles GET /health/detailed
// Detailed health check - includes database connectivity
func (h *HealthHandler) GetHealthDetailed(w http.ResponseWriter, r *http.Request) {
	checks := make(map[string]string)
	status := "ok"

	// Check database connectivity
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.db.Ping(ctx); err != nil {
		checks["database"] = "unhealthy: " + err.Error()
		status = "degraded"
	} else {
		checks["database"] = "healthy"
	}

	// Add more checks here (Redis, external APIs, etc.)
	checks["api"] = "healthy"

	httpStatus := http.StatusOK
	if status == "degraded" {
		httpStatus = http.StatusServiceUnavailable
	}

	response := HealthResponse{
		Status:  status,
		Version: "1.0.0",
		Uptime:  time.Since(startTime).String(),
		Checks:  checks,
	}

	respondWithJSON(w, httpStatus, response)
}

// GetReadiness handles GET /health/ready
// Readiness probe for Kubernetes - checks if service is ready to accept traffic
func (h *HealthHandler) GetReadiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	// Check critical dependencies
	if err := h.db.Ping(ctx); err != nil {
		respondWithError(w, http.StatusServiceUnavailable, "database not ready")
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// GetLiveness handles GET /health/live
// Liveness probe for Kubernetes - checks if service is alive
func GetLiveness(w http.ResponseWriter, r *http.Request) {
	respondWithJSON(w, http.StatusOK, map[string]string{"status": "alive"})
}
