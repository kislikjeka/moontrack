package main

import (
	"context"
	_ "embed"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kislikjeka/moontrack/internal/api/handlers"
	"github.com/kislikjeka/moontrack/internal/api/router"
	"github.com/kislikjeka/moontrack/internal/core/ledger/handler"
	ledgerPostgres "github.com/kislikjeka/moontrack/internal/core/ledger/postgres"
	ledgerService "github.com/kislikjeka/moontrack/internal/core/ledger/service"
	pricingCache "github.com/kislikjeka/moontrack/internal/core/pricing/cache"
	pricingCoinGecko "github.com/kislikjeka/moontrack/internal/core/pricing/coingecko"
	pricingService "github.com/kislikjeka/moontrack/internal/core/pricing/service"
	"github.com/kislikjeka/moontrack/internal/core/user/auth"
	userPostgres "github.com/kislikjeka/moontrack/internal/core/user/repository/postgres"
	userService "github.com/kislikjeka/moontrack/internal/core/user/service"
	assetAdjustmentHandler "github.com/kislikjeka/moontrack/internal/modules/asset_adjustment/handler"
	manualTxHandler "github.com/kislikjeka/moontrack/internal/modules/manual_transaction/handler"
	portfolioAdapter "github.com/kislikjeka/moontrack/internal/modules/portfolio/adapter"
	portfolioService "github.com/kislikjeka/moontrack/internal/modules/portfolio/service"
	walletPostgres "github.com/kislikjeka/moontrack/internal/modules/wallet/repository/postgres"
	walletService "github.com/kislikjeka/moontrack/internal/modules/wallet/service"
	"github.com/kislikjeka/moontrack/internal/shared/config"
	"github.com/kislikjeka/moontrack/internal/shared/database"
	"github.com/kislikjeka/moontrack/internal/shared/logger"
	"github.com/redis/go-redis/v9"
)

//go:embed openapi.yaml
var openAPISpec []byte

func main() {
	// Create context that listens for termination signals
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	log := logger.NewDefault(cfg.Env)
	log.Info("Starting MoonTrack API server",
		"env", cfg.Env,
		"port", cfg.Port,
	)

	// Initialize database connection pool
	dbCfg := database.Config{
		URL: cfg.DatabaseURL,
	}
	db, err := database.NewPool(ctx, dbCfg)
	if err != nil {
		log.Error("Failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	log.Info("Database connection established")

	// Initialize Redis client for price caching
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisURL,
		Password: cfg.RedisPassword,
		DB:       0,
	})
	defer redisClient.Close()

	// Test Redis connection
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Error("Failed to connect to Redis", "error", err)
		os.Exit(1)
	}
	log.Info("Redis connection established")

	// Initialize pricing components
	coinGeckoClient := pricingCoinGecko.NewClient(cfg.CoinGeckoAPIKey)
	priceCache := pricingCache.NewCache(redisClient)
	priceSvc := pricingService.NewPriceService(coinGeckoClient, priceCache, nil) // TODO: Add price repository when implemented
	log.Info("Pricing service initialized")

	// Initialize repositories
	userRepo := userPostgres.NewUserRepository(db.Pool)
	ledgerRepo := ledgerPostgres.NewLedgerRepository(db.Pool)
	walletRepo := walletPostgres.NewWalletRepository(db.Pool)

	// Initialize handler registry for transaction types
	handlerRegistry := handler.NewRegistry()

	// Initialize services
	userSvc := userService.NewUserService(userRepo)
	jwtSvc := auth.NewJWTService(cfg.JWTSecret)
	ledgerSvc := ledgerService.NewLedgerService(ledgerRepo, handlerRegistry)
	walletSvc := walletService.NewWalletService(walletRepo)

	// Register transaction handlers with the registry
	assetAdjHandler := assetAdjustmentHandler.NewAssetAdjustmentHandler(ledgerSvc)
	handlerRegistry.Register(assetAdjHandler)
	log.Info("Registered asset adjustment handler")

	// Register manual transaction handlers
	manualIncomeHandler := manualTxHandler.NewManualIncomeHandler(priceSvc, walletRepo)
	handlerRegistry.Register(manualIncomeHandler)
	log.Info("Registered manual income handler")

	manualOutcomeHandler := manualTxHandler.NewManualOutcomeHandler(priceSvc, walletRepo, ledgerSvc)
	handlerRegistry.Register(manualOutcomeHandler)
	log.Info("Registered manual outcome handler")

	// Initialize portfolio service
	walletAdapter := portfolioAdapter.NewWalletRepositoryAdapter(walletRepo)
	portfolioSvc := portfolioService.NewPortfolioService(ledgerRepo, walletAdapter, priceSvc)
	log.Info("Portfolio service initialized")

	// Initialize HTTP handlers
	authHandler := handlers.NewAuthHandler(userSvc, jwtSvc)
	walletHandler := handlers.NewWalletHandler(walletSvc)
	transactionHandler := handlers.NewTransactionHandler(ledgerSvc)
	portfolioHandler := handlers.NewPortfolioHandler(portfolioSvc)
	docsHandler := handlers.NewDocsHandler(openAPISpec)

	// Create JWT middleware
	jwtMiddleware := auth.JWTMiddleware(jwtSvc)

	// Determine allowed origins for CORS
	allowedOrigins := []string{"http://localhost:5173"} // Vite default port
	if cfg.IsProduction() {
		// In production, read from environment variable
		if origins := os.Getenv("ALLOWED_ORIGINS"); origins != "" {
			allowedOrigins = []string{origins}
		}
	}

	// Create HTTP router
	routerCfg := router.Config{
		Logger:             log,
		AllowedOrigins:     allowedOrigins,
		AuthHandler:        authHandler,
		WalletHandler:      walletHandler,
		TransactionHandler: transactionHandler,
		PortfolioHandler:   portfolioHandler,
		DocsHandler:        docsHandler,
		JWTMiddleware:      jwtMiddleware,
	}
	r := router.New(routerCfg)

	// Create HTTP server
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start background price refresh job (every 5 minutes)
	priceRefreshTicker := time.NewTicker(5 * time.Minute)
	go func() {
		// Initial refresh on startup
		log.Info("Running initial price refresh")
		if err := priceSvc.RefreshPrices(ctx, pricingService.DefaultAssets()); err != nil {
			log.Warn("Initial price refresh failed", "error", err)
		}

		for {
			select {
			case <-ctx.Done():
				priceRefreshTicker.Stop()
				log.Info("Price refresh job stopped")
				return
			case <-priceRefreshTicker.C:
				log.Info("Running scheduled price refresh")
				refreshCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				if err := priceSvc.RefreshPrices(refreshCtx, pricingService.DefaultAssets()); err != nil {
					log.Warn("Scheduled price refresh failed", "error", err)
				} else {
					log.Info("Price refresh completed successfully")
				}
				cancel()
			}
		}
	}()

	// Start server in a goroutine
	go func() {
		log.Info("Server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("Server failed to start", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for termination signal
	<-ctx.Done()
	log.Info("Shutdown signal received")

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("Server shutdown failed", "error", err)
		os.Exit(1)
	}

	log.Info("Server stopped gracefully")
}
