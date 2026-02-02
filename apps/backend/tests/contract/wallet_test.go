package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/api/handlers"
	"github.com/kislikjeka/moontrack/internal/api/router"
	"github.com/kislikjeka/moontrack/internal/core/user/auth"
	userPostgres "github.com/kislikjeka/moontrack/internal/core/user/repository/postgres"
	userService "github.com/kislikjeka/moontrack/internal/core/user/service"
	walletPostgres "github.com/kislikjeka/moontrack/internal/modules/wallet/repository/postgres"
	walletService "github.com/kislikjeka/moontrack/internal/modules/wallet/service"
	"github.com/kislikjeka/moontrack/internal/shared/database"
)

// setupWalletTestRouter creates a test router with wallet handlers
func setupWalletTestRouter(t *testing.T) (http.Handler, string) {
	t.Helper()

	ctx := context.Background()

	// Connect to test database
	dbCfg := database.Config{
		URL: "postgres://postgres:postgres@localhost:5432/moontrack_test?sslmode=disable",
	}
	db, err := database.NewPool(ctx, dbCfg)
	require.NoError(t, err, "failed to connect to test database")

	// Clean up database before test
	_, err = db.Exec(ctx, "TRUNCATE users, wallets CASCADE")
	require.NoError(t, err, "failed to clean test database")

	// Initialize user services (for authentication)
	userRepo := userPostgres.NewUserRepository(db.Pool)
	userSvc := userService.NewUserService(userRepo)
	jwtSecret := "test-secret-key-minimum-32-characters-long-for-security"
	jwtService := auth.NewJWTService(jwtSecret)
	authHandler := handlers.NewAuthHandler(userSvc, jwtService)

	// Initialize wallet services
	walletRepo := walletPostgres.NewWalletRepository(db.Pool)
	walletSvc := walletService.NewWalletService(walletRepo)
	walletHandler := handlers.NewWalletHandler(walletSvc)

	// Create JWT middleware
	jwtMiddleware := auth.JWTMiddleware(jwtService)

	// Create router
	routerCfg := router.Config{
		AuthHandler:   authHandler,
		WalletHandler: walletHandler,
		JWTMiddleware: jwtMiddleware,
	}
	r := router.New(routerCfg)

	// Create a test user and get JWT token
	user, err := userSvc.Register(ctx, "testuser@example.com", "SecureP@ssw0rd123")
	require.NoError(t, err, "failed to create test user")

	token, err := jwtService.GenerateToken(user.ID, user.Email)
	require.NoError(t, err, "failed to generate JWT token")

	return r, token
}

// TestCreateWallet_Success tests successful wallet creation (T078)
func TestCreateWallet_Success(t *testing.T) {
	r, token := setupWalletTestRouter(t)

	// Prepare request
	reqBody := map[string]interface{}{
		"name":     "My Ethereum Wallet",
		"chain_id": "ethereum",
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()

	// Execute request
	r.ServeHTTP(w, req)

	// Assert response
	assert.Equal(t, http.StatusCreated, w.Code, "expected status 201 Created")

	var resp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.NotEmpty(t, resp["id"], "wallet ID should not be empty")
	assert.Equal(t, "My Ethereum Wallet", resp["name"])
	assert.Equal(t, "ethereum", resp["chain_id"])
	assert.NotEmpty(t, resp["user_id"])
	assert.NotEmpty(t, resp["created_at"])
	assert.NotEmpty(t, resp["updated_at"])
}

// TestCreateWallet_MissingName tests validation error for missing name (T078)
func TestCreateWallet_MissingName(t *testing.T) {
	r, token := setupWalletTestRouter(t)

	// Prepare request with missing name
	reqBody := map[string]interface{}{
		"chain_id": "ethereum",
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()

	// Execute request
	r.ServeHTTP(w, req)

	// Assert response
	assert.Equal(t, http.StatusInternalServerError, w.Code, "expected validation error")

	var resp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp, "error")
}

// TestCreateWallet_InvalidChainID tests validation error for invalid chain (T078)
func TestCreateWallet_InvalidChainID(t *testing.T) {
	r, token := setupWalletTestRouter(t)

	// Prepare request with invalid chain ID
	reqBody := map[string]interface{}{
		"name":     "Test Wallet",
		"chain_id": "invalid-chain",
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()

	// Execute request
	r.ServeHTTP(w, req)

	// Assert response
	assert.Equal(t, http.StatusBadRequest, w.Code, "expected status 400 Bad Request")

	var resp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp["error"], "invalid chain ID")
}

// TestCreateWallet_DuplicateName tests duplicate wallet name error (T078)
func TestCreateWallet_DuplicateName(t *testing.T) {
	r, token := setupWalletTestRouter(t)

	// Create first wallet
	reqBody := map[string]interface{}{
		"name":     "Duplicate Wallet",
		"chain_id": "ethereum",
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code, "first wallet creation should succeed")

	// Try to create second wallet with same name
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/wallets", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+token)

	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Assert response
	assert.Equal(t, http.StatusConflict, w2.Code, "expected status 409 Conflict")

	var resp map[string]interface{}
	err = json.Unmarshal(w2.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp["error"], "wallet name already exists")
}

// TestCreateWallet_Unauthorized tests unauthorized access (T078)
func TestCreateWallet_Unauthorized(t *testing.T) {
	r, _ := setupWalletTestRouter(t)

	// Prepare request without token
	reqBody := map[string]interface{}{
		"name":     "Test Wallet",
		"chain_id": "ethereum",
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/wallets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No Authorization header

	w := httptest.NewRecorder()

	// Execute request
	r.ServeHTTP(w, req)

	// Assert response
	assert.Equal(t, http.StatusUnauthorized, w.Code, "expected status 401 Unauthorized")
}

// TestGetWallets_ReturnsUserWalletsOnly tests that GET /wallets returns only user's wallets (T079)
func TestGetWallets_ReturnsUserWalletsOnly(t *testing.T) {
	r, token := setupWalletTestRouter(t)

	// Create wallets for the authenticated user
	walletNames := []string{"Wallet 1", "Wallet 2", "Wallet 3"}
	for _, name := range walletNames {
		reqBody := map[string]interface{}{
			"name":     name,
			"chain_id": "ethereum",
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/wallets", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusCreated, w.Code, "wallet creation should succeed")
	}

	// Get wallets
	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallets", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Assert response
	assert.Equal(t, http.StatusOK, w.Code, "expected status 200 OK")

	var wallets []map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &wallets)
	require.NoError(t, err)

	// Should return exactly 3 wallets
	assert.Len(t, wallets, 3, "should return 3 wallets")

	// Verify wallet names
	returnedNames := make(map[string]bool)
	for _, wallet := range wallets {
		name, ok := wallet["name"].(string)
		require.True(t, ok, "wallet name should be a string")
		returnedNames[name] = true
	}

	for _, expectedName := range walletNames {
		assert.True(t, returnedNames[expectedName], "wallet %s should be in response", expectedName)
	}
}

// TestGetWallets_EmptyList tests empty wallet list (T079)
func TestGetWallets_EmptyList(t *testing.T) {
	r, token := setupWalletTestRouter(t)

	// Get wallets without creating any
	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallets", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Assert response
	assert.Equal(t, http.StatusOK, w.Code, "expected status 200 OK")

	var wallets []map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &wallets)
	require.NoError(t, err)

	assert.Len(t, wallets, 0, "should return empty array")
}

// TestGetWallet_Success tests successful wallet retrieval by ID (T080)
func TestGetWallet_Success(t *testing.T) {
	r, token := setupWalletTestRouter(t)

	// Create a wallet first
	reqBody := map[string]interface{}{
		"name":     "Test Wallet",
		"chain_id": "bitcoin",
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/wallets", bytes.NewReader(body))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+token)

	createW := httptest.NewRecorder()
	r.ServeHTTP(createW, createReq)
	require.Equal(t, http.StatusCreated, createW.Code)

	var createdWallet map[string]interface{}
	err = json.Unmarshal(createW.Body.Bytes(), &createdWallet)
	require.NoError(t, err)

	walletID := createdWallet["id"].(string)

	// Get the wallet by ID
	getReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/wallets/%s", walletID), nil)
	getReq.Header.Set("Authorization", "Bearer "+token)

	getW := httptest.NewRecorder()
	r.ServeHTTP(getW, getReq)

	// Assert response
	assert.Equal(t, http.StatusOK, getW.Code, "expected status 200 OK")

	var wallet map[string]interface{}
	err = json.Unmarshal(getW.Body.Bytes(), &wallet)
	require.NoError(t, err)

	assert.Equal(t, walletID, wallet["id"])
	assert.Equal(t, "Test Wallet", wallet["name"])
	assert.Equal(t, "bitcoin", wallet["chain_id"])
}

// TestGetWallet_NotFound tests wallet not found error (T080)
func TestGetWallet_NotFound(t *testing.T) {
	r, token := setupWalletTestRouter(t)

	// Try to get non-existent wallet
	fakeID := uuid.New().String()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/wallets/%s", fakeID), nil)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Assert response
	assert.Equal(t, http.StatusNotFound, w.Code, "expected status 404 Not Found")

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp["error"], "wallet not found")
}

// TestGetWallet_InvalidID tests invalid wallet ID format (T080)
func TestGetWallet_InvalidID(t *testing.T) {
	r, token := setupWalletTestRouter(t)

	// Try to get wallet with invalid ID
	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallets/invalid-id", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Assert response
	assert.Equal(t, http.StatusBadRequest, w.Code, "expected status 400 Bad Request")

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp["error"], "invalid wallet ID")
}
