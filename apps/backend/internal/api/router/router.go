package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/kislikjeka/moontrack/internal/api/handlers"
	"github.com/kislikjeka/moontrack/internal/api/middleware"
	"github.com/kislikjeka/moontrack/internal/shared/logger"
)

// Config holds router configuration
type Config struct {
	Logger              *logger.Logger
	AllowedOrigins      []string
	AuthHandler         *handlers.AuthHandler
	WalletHandler       *handlers.WalletHandler
	TransactionHandler  *handlers.TransactionHandler
	PortfolioHandler    *handlers.PortfolioHandler
	HealthHandler       *handlers.HealthHandler
	DocsHandler         *handlers.DocsHandler
	JWTMiddleware       func(http.Handler) http.Handler
}

// New creates a new HTTP router
func New(cfg Config) *chi.Mux {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimiddleware.RequestID)
	r.Use(middleware.Recovery(cfg.Logger))
	r.Use(middleware.Logger(cfg.Logger))
	r.Use(middleware.CORS(cfg.AllowedOrigins))
	r.Use(chimiddleware.Compress(5))
	r.Use(middleware.RateLimit()) // Rate limiting: 100 req/s with burst of 20

	// Health check endpoints (no authentication required)
	r.Get("/health", handlers.GetHealth)
	r.Get("/health/live", handlers.GetLiveness)
	if cfg.HealthHandler != nil {
		r.Get("/health/ready", cfg.HealthHandler.GetReadiness)
		r.Get("/health/detailed", cfg.HealthHandler.GetHealthDetailed)
	}

	// API documentation endpoint
	if cfg.DocsHandler != nil {
		r.Get("/docs", cfg.DocsHandler.GetOpenAPISpec)
		r.Get("/docs/info", cfg.DocsHandler.GetOpenAPIJSON)
	}

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		// Auth routes (public - no authentication required)
		if cfg.AuthHandler != nil {
			r.Post("/auth/register", cfg.AuthHandler.Register)
			r.Post("/auth/login", cfg.AuthHandler.Login)
		}

		// Protected routes (require JWT authentication)
		if cfg.JWTMiddleware != nil {
			r.Group(func(r chi.Router) {
				r.Use(cfg.JWTMiddleware)

				// Wallet routes
				if cfg.WalletHandler != nil {
					r.Post("/wallets", cfg.WalletHandler.CreateWallet)
					r.Get("/wallets", cfg.WalletHandler.GetWallets)
					r.Get("/wallets/{id}", cfg.WalletHandler.GetWallet)
					r.Put("/wallets/{id}", cfg.WalletHandler.UpdateWallet)
					r.Delete("/wallets/{id}", cfg.WalletHandler.DeleteWallet)
				}

				// Transaction routes
				if cfg.TransactionHandler != nil {
					r.Post("/transactions", cfg.TransactionHandler.CreateTransaction)
					r.Get("/transactions", cfg.TransactionHandler.GetTransactions)
				}

				// Portfolio routes
				if cfg.PortfolioHandler != nil {
					r.Get("/portfolio", cfg.PortfolioHandler.GetPortfolioSummary)
					r.Get("/portfolio/assets", cfg.PortfolioHandler.GetAssetBreakdown)
				}
			})
		}
	})

	return r
}
