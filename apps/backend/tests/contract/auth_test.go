package contract

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

	// Create router
	routerCfg := router.Config{
		AuthHandler:   authHandler,
		JWTMiddleware: jwtMiddleware,
	}
	r := router.New(routerCfg)

	return r
}

func TestAuthRegister_Success(t *testing.T) {
	r := setupTestRouter(t)

	// Prepare request
	reqBody := map[string]interface{}{
		"email":    "newuser@example.com",
		"password": "SecureP@ssw0rd123",
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()

	// Execute request
	r.ServeHTTP(w, req)

	// Assert response
	assert.Equal(t, http.StatusCreated, w.Code, "expected status 201 Created")

	var resp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Contains(t, resp, "user", "response should contain user")
	assert.Contains(t, resp, "token", "response should contain JWT token")

	user := resp["user"].(map[string]interface{})
	assert.Equal(t, "newuser@example.com", user["email"])
	assert.NotEmpty(t, user["id"])
	assert.NotContains(t, user, "password_hash", "password_hash should not be exposed")

	token := resp["token"].(string)
	assert.NotEmpty(t, token, "JWT token should not be empty")
}

func TestAuthRegister_DuplicateEmail(t *testing.T) {
	r := setupTestRouter(t)

	// First registration (should succeed)
	reqBody := map[string]interface{}{
		"email":    "duplicate@example.com",
		"password": "SecureP@ssw0rd123",
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")

	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	assert.Equal(t, http.StatusCreated, w1.Code, "first registration should succeed")

	// Second registration with same email (should fail)
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")

	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusConflict, w2.Code, "duplicate email should return 409 Conflict")

	var resp map[string]interface{}
	err = json.Unmarshal(w2.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Contains(t, resp, "error", "response should contain error message")
	assert.Contains(t, resp["error"], "already exists", "error should mention duplicate")
}

func TestAuthRegister_InvalidPassword(t *testing.T) {
	r := setupTestRouter(t)

	tests := []struct {
		name     string
		password string
		wantCode int
	}{
		{
			name:     "password too short",
			password: "short",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "empty password",
			password: "",
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody := map[string]interface{}{
				"email":    "user@example.com",
				"password": tt.password,
			}
			body, err := json.Marshal(reqBody)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantCode, w.Code)

			var resp map[string]interface{}
			err = json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err)

			assert.Contains(t, resp, "error", "response should contain error message")
		})
	}
}

func TestAuthRegister_InvalidEmail(t *testing.T) {
	r := setupTestRouter(t)

	tests := []struct {
		name  string
		email string
	}{
		{
			name:  "empty email",
			email: "",
		},
		{
			name:  "invalid email format",
			email: "not-an-email",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody := map[string]interface{}{
				"email":    tt.email,
				"password": "SecureP@ssw0rd123",
			}
			body, err := json.Marshal(reqBody)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)

			var resp map[string]interface{}
			err = json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err)

			assert.Contains(t, resp, "error", "response should contain error message")
		})
	}
}

// T053: Contract test POST /auth/login
func TestAuthLogin_Success(t *testing.T) {
	r := setupTestRouter(t)

	// First, register a user
	registerBody := map[string]interface{}{
		"email":    "logintest@example.com",
		"password": "SecureP@ssw0rd123",
	}
	body, err := json.Marshal(registerBody)
	require.NoError(t, err)

	regReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	regReq.Header.Set("Content-Type", "application/json")

	regW := httptest.NewRecorder()
	r.ServeHTTP(regW, regReq)
	require.Equal(t, http.StatusCreated, regW.Code, "registration should succeed")

	// Now login with the same credentials
	loginBody := map[string]interface{}{
		"email":    "logintest@example.com",
		"password": "SecureP@ssw0rd123",
	}
	body, err = json.Marshal(loginBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Assert response
	assert.Equal(t, http.StatusOK, w.Code, "expected status 200 OK")

	var resp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Contains(t, resp, "user", "response should contain user")
	assert.Contains(t, resp, "token", "response should contain JWT token")

	user := resp["user"].(map[string]interface{})
	assert.Equal(t, "logintest@example.com", user["email"])
	assert.NotEmpty(t, user["id"])

	token := resp["token"].(string)
	assert.NotEmpty(t, token, "JWT token should not be empty")
}

func TestAuthLogin_WrongPassword(t *testing.T) {
	r := setupTestRouter(t)

	// First, register a user
	registerBody := map[string]interface{}{
		"email":    "wrongpass@example.com",
		"password": "CorrectP@ssw0rd123",
	}
	body, err := json.Marshal(registerBody)
	require.NoError(t, err)

	regReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	regReq.Header.Set("Content-Type", "application/json")

	regW := httptest.NewRecorder()
	r.ServeHTTP(regW, regReq)
	require.Equal(t, http.StatusCreated, regW.Code, "registration should succeed")

	// Try to login with wrong password
	loginBody := map[string]interface{}{
		"email":    "wrongpass@example.com",
		"password": "WrongP@ssw0rd123",
	}
	body, err = json.Marshal(loginBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Assert response
	assert.Equal(t, http.StatusUnauthorized, w.Code, "expected status 401 Unauthorized")

	var resp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Contains(t, resp, "error", "response should contain error message")
}

func TestAuthLogin_NonexistentUser(t *testing.T) {
	r := setupTestRouter(t)

	// Try to login with email that doesn't exist
	loginBody := map[string]interface{}{
		"email":    "nonexistent@example.com",
		"password": "SomeP@ssw0rd123",
	}
	body, err := json.Marshal(loginBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Assert response
	assert.Equal(t, http.StatusUnauthorized, w.Code, "expected status 401 Unauthorized")

	var resp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Contains(t, resp, "error", "response should contain error message")
}
