package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/api/handlers"
	"github.com/kislikjeka/moontrack/internal/api/router"
	"github.com/kislikjeka/moontrack/internal/core/ledger/handler"
	ledgerPostgres "github.com/kislikjeka/moontrack/internal/core/ledger/postgres"
	ledgerService "github.com/kislikjeka/moontrack/internal/core/ledger/service"
	"github.com/kislikjeka/moontrack/internal/core/user/auth"
	userPostgres "github.com/kislikjeka/moontrack/internal/core/user/repository/postgres"
	userService "github.com/kislikjeka/moontrack/internal/core/user/service"
	assetAdjustmentHandler "github.com/kislikjeka/moontrack/internal/modules/asset_adjustment/handler"
	manualTransactionHandler "github.com/kislikjeka/moontrack/internal/modules/manual_transaction/handler"
	walletPostgres "github.com/kislikjeka/moontrack/internal/modules/wallet/repository/postgres"
	walletService "github.com/kislikjeka/moontrack/internal/modules/wallet/service"
	"github.com/kislikjeka/moontrack/internal/shared/database"
)

// setupTransactionTestRouter creates a test router with transaction handlers
func setupTransactionTestRouter(t *testing.T) (http.Handler, string, uuid.UUID, uuid.UUID) {
	t.Helper()

	ctx := context.Background()

	// Connect to test database
	dbCfg := database.Config{
		URL: "postgres://postgres:postgres@localhost:5432/moontrack_test?sslmode=disable",
	}
	db, err := database.NewPool(ctx, dbCfg)
	require.NoError(t, err, "failed to connect to test database")

	// Clean up database before test
	_, err = db.Exec(ctx, "TRUNCATE users, wallets, accounts, transactions, entries, account_balances CASCADE")
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

	// Initialize ledger services
	ledgerRepo := ledgerPostgres.NewLedgerRepository(db.Pool)
	handlerRegistry := handler.NewHandlerRegistry()

	// Create and register transaction handlers
	incomeHandler := manualTransactionHandler.NewManualIncomeHandler(walletRepo, nil) // nil for price service in tests
	outcomeHandler := manualTransactionHandler.NewManualOutcomeHandler(walletRepo, nil)
	adjustmentHandler := assetAdjustmentHandler.NewAssetAdjustmentHandler(walletRepo, nil)

	handlerRegistry.Register("manual_income", incomeHandler)
	handlerRegistry.Register("manual_outcome", outcomeHandler)
	handlerRegistry.Register("asset_adjustment", adjustmentHandler)

	ledgerSvc := ledgerService.NewLedgerService(ledgerRepo, handlerRegistry)
	transactionHandler := handlers.NewTransactionHandler(ledgerSvc)

	// Create JWT middleware
	jwtMiddleware := auth.JWTMiddleware(jwtService)

	// Create router
	routerCfg := router.Config{
		AuthHandler:        authHandler,
		WalletHandler:      walletHandler,
		TransactionHandler: transactionHandler,
		JWTMiddleware:      jwtMiddleware,
	}
	r := router.New(routerCfg)

	// Create a test user and get JWT token
	user, err := userSvc.Register(ctx, "testuser@example.com", "SecureP@ssw0rd123")
	require.NoError(t, err, "failed to create test user")

	token, err := jwtService.GenerateToken(user.ID, user.Email)
	require.NoError(t, err, "failed to generate JWT token")

	// Create a test wallet
	wallet, err := walletSvc.Create(ctx, walletService.CreateWalletRequest{
		UserID:  user.ID,
		Name:    "Test Wallet",
		ChainID: "ethereum",
		Address: "0x1234567890abcdef",
	})
	require.NoError(t, err, "failed to create test wallet")

	return r, token, user.ID, wallet.ID
}

// TestCreateTransaction_ManualIncome tests successful manual income transaction creation (T114)
func TestCreateTransaction_ManualIncome(t *testing.T) {
	r, token, _, walletID := setupTransactionTestRouter(t)

	// Prepare request for manual income
	amount := big.NewInt(1000000000000000000) // 1 ETH in wei
	usdRate := big.NewInt(200000000000)       // $2000.00 USD scaled by 10^8
	usdRateStr := usdRate.String()

	reqBody := map[string]interface{}{
		"type":        "manual_income",
		"wallet_id":   walletID.String(),
		"asset_id":    "ETH",
		"amount":      amount.String(),
		"usd_rate":    usdRateStr,
		"occurred_at": time.Now().Format(time.RFC3339),
		"notes":       "Test income transaction",
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()

	// Execute request
	r.ServeHTTP(w, req)

	// Assert response
	assert.Equal(t, http.StatusCreated, w.Code, "expected status 201 Created, got: %s", w.Body.String())

	var resp map[string]interface{}
	err = json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)

	assert.NotEmpty(t, resp["id"], "transaction ID should not be empty")
	assert.Equal(t, "manual_income", resp["type"])
	assert.Equal(t, "COMPLETED", resp["status"])
}

// TestCreateTransaction_ManualOutcome tests successful manual outcome transaction creation (T114)
func TestCreateTransaction_ManualOutcome(t *testing.T) {
	r, token, _, walletID := setupTransactionTestRouter(t)

	ctx := context.Background()

	// First, create an income transaction to have balance
	amount := big.NewInt(2000000000000000000) // 2 ETH in wei
	usdRate := big.NewInt(200000000000)       // $2000.00 USD
	usdRateStr := usdRate.String()

	incomeReq := map[string]interface{}{
		"type":        "manual_income",
		"wallet_id":   walletID.String(),
		"asset_id":    "ETH",
		"amount":      amount.String(),
		"usd_rate":    usdRateStr,
		"occurred_at": time.Now().Format(time.RFC3339),
		"notes":       "Initial balance",
	}
	body, _ := json.Marshal(incomeReq)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "failed to create income: %s", w.Body.String())

	// Now create an outcome transaction
	outcomeAmount := big.NewInt(1000000000000000000) // 1 ETH in wei
	outcomeReq := map[string]interface{}{
		"type":        "manual_outcome",
		"wallet_id":   walletID.String(),
		"asset_id":    "ETH",
		"amount":      outcomeAmount.String(),
		"usd_rate":    usdRateStr,
		"occurred_at": time.Now().Format(time.RFC3339),
		"notes":       "Test outcome transaction",
	}
	body, err := json.Marshal(outcomeReq)
	require.NoError(t, err)

	req = httptest.NewRequest(http.MethodPost, "/api/v1/transactions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w = httptest.NewRecorder()

	// Execute request
	r.ServeHTTP(w, req)

	// Assert response
	_ = ctx
	assert.Equal(t, http.StatusCreated, w.Code, "expected status 201 Created, got: %s", w.Body.String())

	var resp map[string]interface{}
	err = json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)

	assert.NotEmpty(t, resp["id"])
	assert.Equal(t, "manual_outcome", resp["type"])
	assert.Equal(t, "COMPLETED", resp["status"])
}

// TestCreateTransaction_AssetAdjustment tests successful asset adjustment transaction (T114)
func TestCreateTransaction_AssetAdjustment(t *testing.T) {
	r, token, _, walletID := setupTransactionTestRouter(t)

	// Prepare request for asset adjustment
	newBalance := big.NewInt(5000000000000000000) // 5 ETH in wei
	usdRate := big.NewInt(200000000000)           // $2000.00 USD
	usdRateStr := usdRate.String()

	reqBody := map[string]interface{}{
		"type":        "asset_adjustment",
		"wallet_id":   walletID.String(),
		"asset_id":    "ETH",
		"amount":      newBalance.String(),
		"usd_rate":    usdRateStr,
		"occurred_at": time.Now().Format(time.RFC3339),
		"notes":       "Initial balance adjustment",
		"data": map[string]interface{}{
			"new_balance": newBalance.String(),
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()

	// Execute request
	r.ServeHTTP(w, req)

	// Assert response
	assert.Equal(t, http.StatusCreated, w.Code, "expected status 201 Created, got: %s", w.Body.String())

	var resp map[string]interface{}
	err = json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)

	assert.NotEmpty(t, resp["id"])
	assert.Equal(t, "asset_adjustment", resp["type"])
	assert.Equal(t, "COMPLETED", resp["status"])
}

// TestCreateTransaction_ValidationErrors tests validation errors (T114)
func TestCreateTransaction_ValidationErrors(t *testing.T) {
	r, token, _, walletID := setupTransactionTestRouter(t)

	tests := []struct {
		name           string
		requestBody    map[string]interface{}
		expectedStatus int
		expectedError  string
	}{
		{
			name: "missing transaction type",
			requestBody: map[string]interface{}{
				"wallet_id":   walletID.String(),
				"asset_id":    "ETH",
				"amount":      "1000000000000000000",
				"occurred_at": time.Now().Format(time.RFC3339),
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "transaction type is required",
		},
		{
			name: "invalid wallet ID",
			requestBody: map[string]interface{}{
				"type":        "manual_income",
				"wallet_id":   "invalid-uuid",
				"asset_id":    "ETH",
				"amount":      "1000000000000000000",
				"occurred_at": time.Now().Format(time.RFC3339),
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid wallet ID",
		},
		{
			name: "invalid amount format",
			requestBody: map[string]interface{}{
				"type":        "manual_income",
				"wallet_id":   walletID.String(),
				"asset_id":    "ETH",
				"amount":      "not-a-number",
				"occurred_at": time.Now().Format(time.RFC3339),
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid amount format",
		},
		{
			name: "invalid occurred_at format",
			requestBody: map[string]interface{}{
				"type":        "manual_income",
				"wallet_id":   walletID.String(),
				"asset_id":    "ETH",
				"amount":      "1000000000000000000",
				"occurred_at": "invalid-date",
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid occurred_at format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(tt.requestBody)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			var resp map[string]interface{}
			err = json.NewDecoder(w.Body).Decode(&resp)
			require.NoError(t, err)

			assert.Contains(t, resp["error"], tt.expectedError)
		})
	}
}

// TestGetTransactions_Success tests successful transaction listing with pagination (T115)
func TestGetTransactions_Success(t *testing.T) {
	r, token, _, walletID := setupTransactionTestRouter(t)

	// Create multiple transactions
	amount := big.NewInt(1000000000000000000) // 1 ETH
	usdRate := big.NewInt(200000000000)       // $2000
	usdRateStr := usdRate.String()

	for i := 0; i < 5; i++ {
		reqBody := map[string]interface{}{
			"type":        "manual_income",
			"wallet_id":   walletID.String(),
			"asset_id":    "ETH",
			"amount":      amount.String(),
			"usd_rate":    usdRateStr,
			"occurred_at": time.Now().Add(time.Duration(-i) * time.Hour).Format(time.RFC3339),
			"notes":       fmt.Sprintf("Transaction %d", i+1),
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusCreated, w.Code)
	}

	// Get transactions with pagination
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions?page=1&page_size=3", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)

	assert.NotNil(t, resp["transactions"])
	assert.Equal(t, float64(1), resp["page"])
	assert.Equal(t, float64(3), resp["page_size"])

	transactions := resp["transactions"].([]interface{})
	assert.LessOrEqual(t, len(transactions), 3, "should respect page_size")
}

// TestGetTransactions_WithFilters tests transaction listing with filters (T115)
func TestGetTransactions_WithFilters(t *testing.T) {
	r, token, _, walletID := setupTransactionTestRouter(t)

	// Create transactions with different types and dates
	baseTime := time.Now()

	// Income transaction
	incomeReq := map[string]interface{}{
		"type":        "manual_income",
		"wallet_id":   walletID.String(),
		"asset_id":    "ETH",
		"amount":      big.NewInt(1000000000000000000).String(),
		"usd_rate":    big.NewInt(200000000000).String(),
		"occurred_at": baseTime.Format(time.RFC3339),
		"notes":       "Income",
	}
	body, _ := json.Marshal(incomeReq)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// Test filter by type
	req = httptest.NewRequest(http.MethodGet, "/api/v1/transactions?type=manual_income", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)

	transactions := resp["transactions"].([]interface{})
	for _, tx := range transactions {
		txMap := tx.(map[string]interface{})
		assert.Equal(t, "manual_income", txMap["type"])
	}
}

// TestGetTransactions_Unauthorized tests unauthorized access (T115)
func TestGetTransactions_Unauthorized(t *testing.T) {
	r, _, _, _ := setupTransactionTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions", nil)
	// No Authorization header

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
