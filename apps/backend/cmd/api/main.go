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

	"github.com/kislikjeka/moontrack/internal/infra/gateway/coingecko"
	"github.com/kislikjeka/moontrack/internal/infra/gateway/zerion"
	"github.com/kislikjeka/moontrack/internal/infra/postgres"
	infraRedis "github.com/kislikjeka/moontrack/internal/infra/redis"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/module/adjustment"
	"github.com/kislikjeka/moontrack/internal/module/defi"
	"github.com/kislikjeka/moontrack/internal/module/genesis"
	"github.com/kislikjeka/moontrack/internal/module/liquidity"
	"github.com/kislikjeka/moontrack/internal/module/portfolio"
	"github.com/kislikjeka/moontrack/internal/module/swap"
	"github.com/kislikjeka/moontrack/internal/module/transactions"
	"github.com/kislikjeka/moontrack/internal/module/transfer"
	"github.com/kislikjeka/moontrack/internal/platform/asset"
	"github.com/kislikjeka/moontrack/internal/platform/lpposition"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/taxlot"
	"github.com/kislikjeka/moontrack/pkg/money"
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
	coinGeckoClient := coingecko.NewClient(cfg.CoinGeckoAPIKey, log)
	priceCache := infraRedis.NewCache(redisClient, log)

	// Initialize Asset components (unified asset + price service)
	assetRepo := postgres.NewAssetRepository(db.Pool)
	priceHistoryRepo := postgres.NewPriceRepository(db.Pool)
	priceProvider := coingecko.NewPriceProviderAdapter(coinGeckoClient)
	assetSvc := asset.NewService(assetRepo, priceHistoryRepo, priceCache, priceProvider, log)
	log.Info("Asset service initialized")

	// Initialize repositories
	userRepo := postgres.NewUserRepository(db.Pool)
	ledgerRepo := postgres.NewLedgerRepository(db.Pool)
	walletRepo := postgres.NewWalletRepository(db.Pool)

	// Initialize handler registry for transaction types
	handlerRegistry := ledger.NewRegistry()

	// Initialize services
	userSvc := user.NewService(userRepo, log)
	jwtSvc := middleware.NewJWTService(cfg.JWTSecret)
	ledgerSvc := ledger.NewService(ledgerRepo, handlerRegistry, log)
	walletSvc := wallet.NewService(walletRepo, log)

	// Register tax lot hook (cost basis tracking)
	taxLotRepo := postgres.NewTaxLotRepository(db.Pool)
	taxLotHook := ledger.NewTaxLotHook(taxLotRepo, ledgerRepo, log)
	ledgerSvc.RegisterPostBalanceHook(taxLotHook)
	log.Info("TaxLot hook registered")

	// Initialize tax lot service (cost basis API)
	taxLotSvc := taxlot.NewService(taxLotRepo, ledgerRepo, walletRepo, log)

	// Register transaction handlers with the registry

	// Asset adjustment handler
	assetAdjHandler := adjustment.NewAssetAdjustmentHandler(ledgerSvc, log)
	handlerRegistry.Register(assetAdjHandler)
	log.Info("Registered asset adjustment handler")

	// Transfer handlers (blockchain-native transfers)
	transferInHandler := transfer.NewTransferInHandler(walletRepo, log)
	handlerRegistry.Register(transferInHandler)
	log.Info("Registered transfer in handler")

	transferOutHandler := transfer.NewTransferOutHandler(walletRepo, log)
	handlerRegistry.Register(transferOutHandler)
	log.Info("Registered transfer out handler")

	internalTransferHandler := transfer.NewInternalTransferHandler(walletRepo, log)
	handlerRegistry.Register(internalTransferHandler)
	log.Info("Registered internal transfer handler")

	// Swap handler (DEX token swaps)
	swapHandler := swap.NewSwapHandler(walletRepo, log)
	handlerRegistry.Register(swapHandler)
	log.Info("Registered swap handler")

	// DeFi handlers (deposit, withdraw, claim)
	defiDepositHandler := defi.NewDeFiDepositHandler(walletRepo, log)
	handlerRegistry.Register(defiDepositHandler)
	log.Info("Registered defi deposit handler")

	defiWithdrawHandler := defi.NewDeFiWithdrawHandler(walletRepo, log)
	handlerRegistry.Register(defiWithdrawHandler)
	log.Info("Registered defi withdraw handler")

	defiClaimHandler := defi.NewDeFiClaimHandler(walletRepo, log)
	handlerRegistry.Register(defiClaimHandler)
	log.Info("Registered defi claim handler")

	// Genesis balance handler (auto-created by sync to cover missing prior history)
	genesisHandler := genesis.NewHandler(log)
	handlerRegistry.Register(genesisHandler)
	log.Info("Registered genesis balance handler")

	// LP handlers (Uniswap V3 liquidity pool operations)
	lpDepositHandler := liquidity.NewLPDepositHandler(walletRepo, log)
	handlerRegistry.Register(lpDepositHandler)
	log.Info("Registered LP deposit handler")

	lpWithdrawHandler := liquidity.NewLPWithdrawHandler(walletRepo, log)
	handlerRegistry.Register(lpWithdrawHandler)
	log.Info("Registered LP withdraw handler")

	lpClaimFeesHandler := liquidity.NewLPClaimFeesHandler(walletRepo, log)
	handlerRegistry.Register(lpClaimFeesHandler)
	log.Info("Registered LP claim fees handler")

	// LP Position tracking
	lpPositionRepo := postgres.NewLPPositionRepo(db.Pool)
	lpPositionSvc := lpposition.NewService(lpPositionRepo, log)
	log.Info("LP Position service initialized")

	// Initialize decimal resolver (cascading: assets table → zerion_assets table → hardcoded)
	zerionAssetRepo := postgres.NewZerionAssetRepository(db.Pool)
	assetDecimalSrc := asset.NewDecimalSource(assetRepo)
	zerionDecimalSrc := sync.NewDecimalSource(zerionAssetRepo)
	decimalResolver := money.NewDecimalResolver(assetDecimalSrc, zerionDecimalSrc)
	log.Info("Decimal resolver initialized")

	// Initialize portfolio service (using price adapter for symbol→CoinGecko resolution)
	walletAdapter := portfolio.NewWalletRepositoryAdapter(walletRepo)
	portfolioPriceAdapter := portfolio.NewPortfolioPriceAdapter(assetSvc)
	wacAdapter := portfolio.NewWACAdapter(taxLotSvc)
	portfolioSvc := portfolio.NewPortfolioService(ledgerRepo, walletAdapter, portfolioPriceAdapter, wacAdapter, decimalResolver)
	log.Info("Portfolio service initialized")

	// Initialize transaction service (read-only, for enriched views)
	transactionSvc := transactions.NewTransactionService(ledgerSvc, walletRepo, decimalResolver)
	log.Info("Transaction service initialized")

	// Initialize blockchain sync service
	var syncSvc *sync.Service
	if cfg.ZerionAPIKey != "" {
		syncConfig := &sync.Config{
			PollInterval:        cfg.SyncPollInterval,
			ConcurrentWallets:   3,
			InitialSyncLookback: 2160 * time.Hour,
			Enabled:             true,
		}
		syncAssetAdapter := sync.NewSyncAssetAdapter(assetSvc)

		zerionClient := zerion.NewClient(cfg.ZerionAPIKey, log)
		zerionProvider := zerion.NewSyncAdapter(zerionClient)
		log.Info("Zerion sync provider initialized")

		rawTxRepo := postgres.NewRawTransactionRepository(db.Pool)

		syncSvc = sync.NewService(syncConfig, walletRepo, ledgerSvc, syncAssetAdapter, log, zerionProvider, zerionProvider, rawTxRepo, zerionAssetRepo, lpPositionSvc)
		log.Info("Sync service initialized",
			"poll_interval", cfg.SyncPollInterval,
			"provider", "zerion")
	} else {
		log.Warn("ZERION_API_KEY not set, sync disabled")
	}

	// Initialize HTTP handlers
	authHandler := handler.NewAuthHandler(userSvc, jwtSvc)
	var walletSyncSvc handler.SyncServiceInterface
	if syncSvc != nil {
		walletSyncSvc = syncSvc
	}
	walletHandler := handler.NewWalletHandler(walletSvc, walletSyncSvc)
	transactionHandler := handler.NewTransactionHandler(ledgerSvc, transactionSvc, assetSvc)
	portfolioHandler := handler.NewPortfolioHandler(portfolioSvc)
	assetHandler := handler.NewAssetHandler(assetSvc)
	taxLotHandler := handler.NewTaxLotHandler(taxLotSvc, decimalResolver)
	lpPositionHTTPHandler := handler.NewLPPositionHandler(lpPositionSvc)
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
		TaxLotHandler:      taxLotHandler,
		LPPositionHandler:  lpPositionHTTPHandler,
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
			Logger:    log,
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
