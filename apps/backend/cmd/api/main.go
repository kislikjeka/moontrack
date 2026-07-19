package main

import (
	"context"
	_ "embed"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/kislikjeka/moontrack/internal/infra/gateway/coingecko"
	"github.com/kislikjeka/moontrack/internal/infra/gateway/defillama"
	"github.com/kislikjeka/moontrack/internal/infra/gateway/geckoterminal"
	"github.com/kislikjeka/moontrack/internal/infra/gateway/zerion"
	"github.com/kislikjeka/moontrack/internal/infra/postgres"
	infraRedis "github.com/kislikjeka/moontrack/internal/infra/redis"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/module/adjustment"
	"github.com/kislikjeka/moontrack/internal/module/defi"
	"github.com/kislikjeka/moontrack/internal/module/genesis"
	"github.com/kislikjeka/moontrack/internal/module/lending"
	"github.com/kislikjeka/moontrack/internal/module/liquidity"
	"github.com/kislikjeka/moontrack/internal/module/lots"
	"github.com/kislikjeka/moontrack/internal/module/portfolio"
	"github.com/kislikjeka/moontrack/internal/module/swap"
	"github.com/kislikjeka/moontrack/internal/module/transactions"
	"github.com/kislikjeka/moontrack/internal/module/transfer"
	"github.com/kislikjeka/moontrack/internal/platform/asset"
	"github.com/kislikjeka/moontrack/internal/platform/lendingposition"
	"github.com/kislikjeka/moontrack/internal/platform/lpposition"
	"github.com/kislikjeka/moontrack/internal/platform/price"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/taxlot"
	"github.com/kislikjeka/moontrack/internal/platform/user"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/handler"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
	"github.com/kislikjeka/moontrack/pkg/config"
	"github.com/kislikjeka/moontrack/pkg/logger"
	"github.com/kislikjeka/moontrack/pkg/money"

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

	// Lending handlers (AAVE supply, withdraw, borrow, repay, claim)
	lendingSupplyHandler := lending.NewLendingSupplyHandler(walletRepo, log)
	handlerRegistry.Register(lendingSupplyHandler)

	lendingWithdrawHandler := lending.NewLendingWithdrawHandler(walletRepo, log)
	handlerRegistry.Register(lendingWithdrawHandler)

	lendingBorrowHandler := lending.NewLendingBorrowHandler(walletRepo, log)
	handlerRegistry.Register(lendingBorrowHandler)

	lendingRepayHandler := lending.NewLendingRepayHandler(walletRepo, log)
	handlerRegistry.Register(lendingRepayHandler)

	lendingClaimHandler := lending.NewLendingClaimHandler(walletRepo, log)
	handlerRegistry.Register(lendingClaimHandler)
	log.Info("Registered lending handlers (supply, withdraw, borrow, repay, claim)")

	// LP Position tracking
	lpPositionRepo := postgres.NewLPPositionRepo(db.Pool)
	lpPositionSvc := lpposition.NewService(lpPositionRepo, log)
	log.Info("LP Position service initialized")

	// Lending Position tracking
	lendingPositionRepo := postgres.NewLendingPositionRepo(db.Pool)
	lendingPositionSvc := lendingposition.NewService(lendingPositionRepo, log)
	log.Info("Lending Position service initialized")

	// Initialize decimal resolver (cascading: assets table → sync asset store → hardcoded)
	syncAssetRepo := postgres.NewSyncAssetRepository(db.Pool)
	assetDecimalSrc := asset.NewDecimalSource(assetRepo)
	syncDecimalSrc := sync.NewDecimalSource(syncAssetRepo)
	decimalResolver := money.NewDecimalResolver(assetDecimalSrc, syncDecimalSrc)
	log.Info("Decimal resolver initialized")

	// Initialize portfolio service (using price adapter for symbol→CoinGecko resolution)
	walletAdapter := portfolio.NewWalletRepositoryAdapter(walletRepo)
	portfolioPriceAdapter := portfolio.NewPortfolioPriceAdapter(assetSvc)
	wacAdapter := portfolio.NewWACAdapter(taxLotSvc)
	portfolioSvc := portfolio.NewPortfolioService(ledgerRepo, walletAdapter, portfolioPriceAdapter, wacAdapter, decimalResolver).
		WithLotStatusCounter(taxLotRepo)
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
			InitialSyncLookback: 0, // fetch all available history
			Enabled:             true,
		}
		syncAssetAdapter := sync.NewSyncAssetAdapter(assetSvc)

		zerionClient := zerion.NewClient(cfg.ZerionAPIKey, log)
		txProvider := zerion.NewSyncAdapter(zerionClient)
		log.Info("Zerion sync provider initialized")

		rawTxRepo := postgres.NewRawTransactionRepository(db.Pool)

		priceBackfillJobRepo := postgres.NewPriceBackfillJobRepository(db.Pool)
		syncSvc = sync.NewService(syncConfig, walletRepo, ledgerSvc, syncAssetAdapter, log, txProvider, txProvider, rawTxRepo, syncAssetRepo, lpPositionSvc, lendingPositionSvc, assetSvc, priceBackfillJobRepo)
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
	lotsSvc := lots.NewService(taxLotRepo, ledgerRepo, walletRepo, assetSvc, log)
	lotsHandler := lots.NewHandler(lotsSvc)
	lpPositionHTTPHandler := handler.NewLPPositionHandler(lpPositionSvc)
	lendingPositionHTTPHandler := handler.NewLendingPositionHandler(lendingPositionSvc)
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
		Logger:                 log,
		AllowedOrigins:         allowedOrigins,
		AuthHandler:            authHandler,
		WalletHandler:          walletHandler,
		TransactionHandler:     transactionHandler,
		PortfolioHandler:       portfolioHandler,
		AssetHandler:           assetHandler,
		TaxLotHandler:          taxLotHandler,
		LotsHandler:            lotsHandler,
		LPPositionHandler:      lpPositionHTTPHandler,
		LendingPositionHandler: lendingPositionHTTPHandler,
		DocsHandler:            docsHandler,
		JWTMiddleware:          jwtMiddleware,
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

	// Wire price fallback providers, backfill worker, and resolved hook.
	// This pipeline is always-on — every sync that produces an unpriced asset
	// enqueues a backfill job, and the worker resolves it from
	// CoinGecko / GeckoTerminal / DefiLlama in priority order.
	{
		gtBaseURL := os.Getenv("GECKOTERMINAL_BASE_URL")
		if gtBaseURL == "" {
			gtBaseURL = "https://api.geckoterminal.com/api/v2"
		}
		dlBaseURL := os.Getenv("DEFILLAMA_BASE_URL")
		if dlBaseURL == "" {
			dlBaseURL = "https://coins.llama.fi"
		}
		dlMinConf := 0.9
		if v := os.Getenv("DEFILLAMA_MIN_CONFIDENCE"); v != "" {
			if parsed, err := strconv.ParseFloat(v, 64); err == nil {
				dlMinConf = parsed
			}
		}
		backfillRateSec := 1
		if v := os.Getenv("PRICE_BACKFILL_RATE_SECONDS"); v != "" {
			if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
				backfillRateSec = parsed
			}
		}

		// PRICE_BACKFILL_WORKERS is parsed but clamped to 1 for now.
		//
		// Running multiple workers against the same provider set requires a
		// distributed per-provider rate-limit coordinator (e.g. a Redis token
		// bucket) — otherwise each worker independently hits the 1 rps ceiling
		// and trips 429s, defeating the point. Tracked as
		// FOLLOWUP-PRICE-WORKER-SCALE; until then, refuse to start more than
		// one worker to avoid quietly ignoring the operator's configuration.
		backfillWorkers := 1
		if v := os.Getenv("PRICE_BACKFILL_WORKERS"); v != "" {
			if parsed, err := strconv.Atoi(v); err == nil && parsed > 1 {
				log.Warn("PRICE_BACKFILL_WORKERS > 1 is not yet supported; clamping to 1",
					"requested", parsed,
					"reason", "requires distributed per-provider rate-limit coordination (FOLLOWUP-PRICE-WORKER-SCALE)",
				)
			}
		}
		_ = backfillWorkers

		gtClient := geckoterminal.NewClient(geckoterminal.Config{BaseURL: gtBaseURL})
		dlClient := defillama.NewClient(defillama.Config{
			BaseURL:       dlBaseURL,
			MinConfidence: dlMinConf,
		})

		redisAdapter := infraRedis.NewPriceRedisAdapter(redisClient)
		priceHistCache := price.NewCache(redisAdapter, 30*24*time.Hour)

		providers := []price.Provider{
			price.NewCoinGeckoProvider(assetSvc),
			price.NewGeckoTerminalProvider(gtClient),
			price.NewDefiLlamaProvider(dlClient),
		}
		resolver := price.NewResolver(providers, priceHistCache, log)

		priceBackfillJobRepo := postgres.NewPriceBackfillJobRepository(db.Pool)
		resolvedHook := ledger.NewPriceResolvedHook(taxLotRepo, log)

		backfillWorker := price.NewBackfillWorker(price.WorkerDeps{
			Jobs:          priceBackfillJobRepo,
			Resolver:      resolver,
			AssetLookup:   assetSvc,
			PriceRecorder: priceHistoryRepo,
			OnResolved:    price.OnPriceResolvedFunc(resolvedHook),
			Logger:        log,
		})

		go backfillWorker.Run(ctx, time.Duration(backfillRateSec)*time.Second)
		go func() {
			tick := time.NewTicker(5 * time.Minute)
			defer tick.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
					_, _ = priceBackfillJobRepo.ReapStale(ctx, 10*time.Minute)
				}
			}
		}()
		log.Info("Price fallback worker started",
			"rate_seconds", backfillRateSec,
			"providers", []string{"coingecko", "geckoterminal", "defillama"},
		)
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
