package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/api/handlers"
	"github.com/kislikjeka/moontrack/internal/api/router"
	"github.com/kislikjeka/moontrack/internal/core/user/auth"
	"github.com/kislikjeka/moontrack/internal/core/user/repository/postgres"
	"github.com/kislikjeka/moontrack/internal/core/user/service"
	"github.com/kislikjeka/moontrack/internal/shared/config"
	"github.com/kislikjeka/moontrack/internal/shared/database"
)

// setupTestRouter creates a test router with real dependencies (test database)
func setupTestRouter(t *testing.T) http.Handler {
	t.Helper()

	// Load test config
	cfg := &config.Config{
		DatabaseURL: "postgres://postgres:postgres@localhost:5432/moontrack_test?sslmode=disable",
		JWTSecret:   "test-secret-key-minimum-32-characters-long-for-security",
		Port:        "8080",
		Env:         "test",
	}

	// Connect to test database
	ctx := context.Background()
	dbCfg := database.Config{
		URL: cfg.DatabaseURL,
	}
	db, err := database.NewPool(ctx, dbCfg)
	require.NoError(t, err, "failed to connect to test database")

	// Clean up database before test
	_, err = db.Exec(ctx, "TRUNCATE users CASCADE")
	require.NoError(t, err, "failed to clean test database")

	// Initialize repositories and services
	userRepo := postgres.NewUserRepository(db.Pool)
	userService := service.NewUserService(userRepo)
	jwtService := auth.NewJWTService(cfg.JWTSecret)

	// Create handlers
	authHandler := handlers.NewAuthHandler(userService, jwtService)

	// Create JWT middleware
	jwtMiddleware := auth.JWTMiddleware(jwtService)

	// Create router with JWT middleware on protected routes
	routerCfg := router.Config{
		AuthHandler:   authHandler,
		JWTMiddleware: jwtMiddleware,
	}
	r := router.New(routerCfg)

	return r
}

// T054: Integration test JWT middleware rejects invalid tokens
func TestJWTMiddleware_RejectsInvalidToken(t *testing.T) {
	r := setupTestRouter(t)

	tests := []struct {
		name        string
		authHeader  string
		description string
	}{
		{
			name:        "no authorization header",
			authHeader:  "",
			description: "request without Authorization header should be rejected",
		},
		{
			name:        "malformed authorization header",
			authHeader:  "InvalidFormat",
			description: "request with malformed Authorization header should be rejected",
		},
		{
			name:        "invalid token",
			authHeader:  "Bearer invalid.jwt.token",
			description: "request with invalid JWT token should be rejected",
		},
		{
			name:        "expired token",
			authHeader:  "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyLCJleHAiOjE1MTYyMzkwMjJ9.4Adcj0u6gqsHYy1Zp4Zx8N9ijKHKF8xKYPF4vQx1q2Y",
			description: "request with expired token should be rejected",
		},
		{
			name:        "token with wrong signature",
			authHeader:  "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoiMTIzNDU2NzgiLCJleHAiOjk5OTk5OTk5OTl9.wrongsignature",
			description: "request with token signed with wrong secret should be rejected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Try to access a protected endpoint (e.g., GET /wallets)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/wallets", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			// All invalid token attempts should return 401 Unauthorized
			assert.Equal(t, http.StatusUnauthorized, w.Code, tt.description)

			var resp map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err)

			assert.Contains(t, resp, "error", "response should contain error message")
		})
	}
}

func TestJWTMiddleware_AcceptsValidToken(t *testing.T) {
	r := setupTestRouter(t)

	// Step 1: Register a user and get a valid token
	registerBody := map[string]interface{}{
		"email":    "validtoken@example.com",
		"password": "SecureP@ssw0rd123",
	}
	body, err := json.Marshal(registerBody)
	require.NoError(t, err)

	regReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	regReq.Header.Set("Content-Type", "application/json")

	regW := httptest.NewRecorder()
	r.ServeHTTP(regW, regReq)
	require.Equal(t, http.StatusCreated, regW.Code, "registration should succeed")

	// Extract token from registration response
	var regResp map[string]interface{}
	err = json.Unmarshal(regW.Body.Bytes(), &regResp)
	require.NoError(t, err)
	require.Contains(t, regResp, "token")

	validToken := regResp["token"].(string)
	require.NotEmpty(t, validToken)

	// Step 2: Use the valid token to access a protected endpoint
	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallets", nil)
	req.Header.Set("Authorization", "Bearer "+validToken)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Should succeed (or return 200/404 depending on whether wallets exist, but NOT 401)
	assert.NotEqual(t, http.StatusUnauthorized, w.Code, "valid token should not return 401")
	assert.True(t, w.Code == http.StatusOK || w.Code == http.StatusNotFound || w.Code == http.StatusNoContent,
		"valid token should return success status code (200/204) or 404 if no wallets exist")
}

func TestJWTMiddleware_TokenExtractsUserID(t *testing.T) {
	r := setupTestRouter(t)

	// Step 1: Register a user
	registerBody := map[string]interface{}{
		"email":    "userid@example.com",
		"password": "SecureP@ssw0rd123",
	}
	body, err := json.Marshal(registerBody)
	require.NoError(t, err)

	regReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	regReq.Header.Set("Content-Type", "application/json")

	regW := httptest.NewRecorder()
	r.ServeHTTP(regW, regReq)
	require.Equal(t, http.StatusCreated, regW.Code)

	// Extract token and user ID
	var regResp map[string]interface{}
	err = json.Unmarshal(regW.Body.Bytes(), &regResp)
	require.NoError(t, err)

	validToken := regResp["token"].(string)
	user := regResp["user"].(map[string]interface{})
	expectedUserID := user["id"].(string)

	// Step 2: Access protected endpoint with token
	// The middleware should extract user_id from token and make it available in context
	// This is verified by the handler being able to return user-specific data
	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallets", nil)
	req.Header.Set("Authorization", "Bearer "+validToken)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// The important thing is that it doesn't return 401 (meaning token was validated and user ID extracted)
	assert.NotEqual(t, http.StatusUnauthorized, w.Code,
		"middleware should extract user ID from valid token and allow access")

	// Additional verification: if there's a user profile endpoint, we can verify the user ID matches
	profileReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	profileReq.Header.Set("Authorization", "Bearer "+validToken)

	profileW := httptest.NewRecorder()
	r.ServeHTTP(profileW, profileReq)

	if profileW.Code == http.StatusOK {
		var profileResp map[string]interface{}
		err = json.Unmarshal(profileW.Body.Bytes(), &profileResp)
		require.NoError(t, err)

		if userResp, ok := profileResp["user"].(map[string]interface{}); ok {
			actualUserID := userResp["id"].(string)
			assert.Equal(t, expectedUserID, actualUserID,
				"middleware should extract correct user ID from token")
		}
	}
}
