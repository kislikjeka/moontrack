package main

import (
	"context"
	_ "embed"
	"fmt"
	stdlog "log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kislikjeka/moontrack/internal/infra/gateway/alchemy"
	"github.com/kislikjeka/moontrack/internal/infra/gateway/coingecko"
	"github.com/kislikjeka/moontrack/internal/infra/postgres"
	infraRedis "github.com/kislikjeka/moontrack/internal/infra/redis"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/module/adjustment"
	"github.com/kislikjeka/moontrack/internal/module/portfolio"
	"github.com/kislikjeka/moontrack/internal/module/transactions"
	"github.com/kislikjeka/moontrack/internal/module/transfer"
	"github.com/kislikjeka/moontrack/internal/platform/asset"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/user"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/handler"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
	"github.com/kislikjeka/moontrack/pkg/config"
	"github.com/kislikjeka/moontrack/pkg/logger"

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
	dbCfg := postgres.Config{
		URL: cfg.DatabaseURL,
	}
	db, err := postgres.NewPool(ctx, dbCfg)
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
	coinGeckoClient := coingecko.NewClient(cfg.CoinGeckoAPIKey)
	priceCache := infraRedis.NewCache(redisClient)

	// Initialize Asset components (unified asset + price service)
	assetRepo := postgres.NewAssetRepository(db.Pool)
	priceHistoryRepo := postgres.NewPriceRepository(db.Pool)
	priceProvider := coingecko.NewPriceProviderAdapter(coinGeckoClient)
	assetSvc := asset.NewService(assetRepo, priceHistoryRepo, priceCache, priceProvider)
	log.Info("Asset service initialized")

	// Initialize repositories
	userRepo := postgres.NewUserRepository(db.Pool)
	ledgerRepo := postgres.NewLedgerRepository(db.Pool)
	walletRepo := postgres.NewWalletRepository(db.Pool)

	// Initialize handler registry for transaction types
	handlerRegistry := ledger.NewRegistry()

	// Initialize services
	userSvc := user.NewService(userRepo)
	jwtSvc := middleware.NewJWTService(cfg.JWTSecret)
	ledgerSvc := ledger.NewService(ledgerRepo, handlerRegistry)
	walletSvc := wallet.NewService(walletRepo)

	// Register transaction handlers with the registry

	// Asset adjustment handler
	assetAdjHandler := adjustment.NewAssetAdjustmentHandler(ledgerSvc)
	handlerRegistry.Register(assetAdjHandler)
	log.Info("Registered asset adjustment handler")

	// Transfer handlers (blockchain-native transfers)
	transferInHandler := transfer.NewTransferInHandler(walletRepo)
	handlerRegistry.Register(transferInHandler)
	log.Info("Registered transfer in handler")

	transferOutHandler := transfer.NewTransferOutHandler(walletRepo)
	handlerRegistry.Register(transferOutHandler)
	log.Info("Registered transfer out handler")

	internalTransferHandler := transfer.NewInternalTransferHandler(walletRepo)
	handlerRegistry.Register(internalTransferHandler)
	log.Info("Registered internal transfer handler")

	// Initialize portfolio service (using AssetService for prices)
	walletAdapter := portfolio.NewWalletRepositoryAdapter(walletRepo)
	portfolioSvc := portfolio.NewPortfolioService(ledgerRepo, walletAdapter, assetSvc)
	log.Info("Portfolio service initialized")

	// Initialize transaction service (read-only, for enriched views)
	transactionSvc := transactions.NewTransactionService(ledgerSvc, walletRepo)
	log.Info("Transaction service initialized")

	// Initialize blockchain sync service (if Alchemy API key is configured)
	var syncSvc *sync.Service
	if cfg.AlchemyAPIKey != "" {
		// Load chains configuration
		chainsConfig, err := config.LoadChainsConfig(cfg.ChainsConfigPath)
		if err != nil {
			log.Warn("Failed to load chains config, sync service disabled", "error", err)
		} else {
			// Create Alchemy client
			alchemyClient := alchemy.NewClient(cfg.AlchemyAPIKey, chainsConfig)
			blockchainClient := alchemy.NewSyncClientAdapter(alchemyClient, chainsConfig)

			// Create asset adapter for sync (maps symbol → CoinGecko ID → price)
			syncAssetAdapter := sync.NewSyncAssetAdapter(assetSvc)

			// Create sync service
			syncConfig := &sync.Config{
				PollInterval:             cfg.SyncPollInterval,
				ConcurrentWallets:        3,
				InitialSyncBlockLookback: 1000000,
				MaxBlocksPerSync:         10000,
				Enabled:                  true,
			}
			syncSvc = sync.NewService(syncConfig, blockchainClient, walletRepo, ledgerSvc, syncAssetAdapter, log.Logger)
			log.Info("Blockchain sync service initialized",
				"poll_interval", cfg.SyncPollInterval,
				"chains_loaded", len(chainsConfig.Chains))
		}
	} else {
		log.Warn("ALCHEMY_API_KEY not configured, blockchain sync disabled")
	}

	// Initialize HTTP handlers
	authHandler := handler.NewAuthHandler(userSvc, jwtSvc)
	walletHandler := handler.NewWalletHandler(walletSvc)
	transactionHandler := handler.NewTransactionHandler(ledgerSvc, transactionSvc, assetSvc)
	portfolioHandler := handler.NewPortfolioHandler(portfolioSvc)
	assetHandler := handler.NewAssetHandler(assetSvc)
	docsHandler := handler.NewDocsHandler(openAPISpec)

	// Create JWT middleware
	jwtMiddleware := middleware.JWTMiddleware(jwtSvc)

	// Determine allowed origins for CORS
	allowedOrigins := []string{"http://localhost:5173", "http://localhost:5174"} // Vite ports
	if cfg.IsProduction() {
		// In production, read from environment variable
		if origins := os.Getenv("ALLOWED_ORIGINS"); origins != "" {
			allowedOrigins = []string{origins}
		}
	}

	// Create HTTP router
	routerCfg := httpapi.Config{
		Logger:             log,
		AllowedOrigins:     allowedOrigins,
		AuthHandler:        authHandler,
		WalletHandler:      walletHandler,
		TransactionHandler: transactionHandler,
		PortfolioHandler:   portfolioHandler,
		AssetHandler:       assetHandler,
		DocsHandler:        docsHandler,
		JWTMiddleware:      jwtMiddleware,
	}
	r := httpapi.NewRouter(routerCfg)

	// Create HTTP server
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start background price refresh job using Asset PriceUpdater
	priceUpdater := asset.NewPriceUpdater(
		assetRepo,
		priceHistoryRepo,
		priceCache,
		priceProvider,
		&asset.PriceUpdaterConfig{
			Interval:  5 * time.Minute,
			BatchSize: 50,
			Logger:    stdlog.New(os.Stdout, "[PriceUpdater] ", stdlog.LstdFlags),
		},
	)
	go priceUpdater.Run(ctx)
	log.Info("Price updater started (5 minute interval)")

	// Start blockchain sync service (if initialized)
	if syncSvc != nil {
		go syncSvc.Run(ctx)
		log.Info("Blockchain sync service started")
	}

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

	// Stop sync service gracefully
	if syncSvc != nil {
		syncSvc.Stop()
		log.Info("Blockchain sync service stopped")
	}

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("Server shutdown failed", "error", err)
		os.Exit(1)
	}

	log.Info("Server stopped gracefully")
}
