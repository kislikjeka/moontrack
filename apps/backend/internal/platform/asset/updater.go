package asset

import (
	"context"
	"log"
	"time"
)

const (
	// DefaultUpdateInterval is the default interval between price updates
	DefaultUpdateInterval = 5 * time.Minute

	// DefaultBatchSize is the default number of assets per API call
	DefaultBatchSize = 50
)

// PriceUpdater periodically fetches and records prices for all active assets
type PriceUpdater struct {
	repo          Repository
	priceRepo     PriceRepository
	cache         PriceCache
	priceProvider PriceProvider
	interval      time.Duration
	batchSize     int
	logger        *log.Logger
}

// PriceUpdaterConfig holds configuration for the price updater
type PriceUpdaterConfig struct {
	Interval  time.Duration
	BatchSize int
	Logger    *log.Logger
}

// NewPriceUpdater creates a new price updater
func NewPriceUpdater(
	repo Repository,
	priceRepo PriceRepository,
	cache PriceCache,
	priceProvider PriceProvider,
	config *PriceUpdaterConfig,
) *PriceUpdater {
	interval := DefaultUpdateInterval
	batchSize := DefaultBatchSize
	var logger *log.Logger

	if config != nil {
		if config.Interval > 0 {
			interval = config.Interval
		}
		if config.BatchSize > 0 {
			batchSize = config.BatchSize
		}
		logger = config.Logger
	}

	if logger == nil {
		logger = log.Default()
	}

	return &PriceUpdater{
		repo:          repo,
		priceRepo:     priceRepo,
		cache:         cache,
		priceProvider: priceProvider,
		interval:      interval,
		batchSize:     batchSize,
		logger:        logger,
	}
}

// Run starts the price updater and runs until the context is cancelled
func (u *PriceUpdater) Run(ctx context.Context) {
	u.logger.Printf("PriceUpdater started (interval: %s, batch size: %d)", u.interval, u.batchSize)

	// Run immediately on start
	u.updatePrices(ctx)

	ticker := time.NewTicker(u.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			u.logger.Println("PriceUpdater stopped")
			return
		case <-ticker.C:
			u.updatePrices(ctx)
		}
	}
}

// updatePrices fetches and records prices for all active assets
func (u *PriceUpdater) updatePrices(ctx context.Context) {
	u.logger.Println("Starting price update cycle")

	// Get all active assets
	assets, err := u.repo.GetActiveAssets(ctx)
	if err != nil {
		u.logger.Printf("Failed to get active assets: %v", err)
		return
	}

	if len(assets) == 0 {
		u.logger.Println("No active assets to update")
		return
	}

	u.logger.Printf("Updating prices for %d assets", len(assets))

	// Process assets in batches
	var successCount, failCount int
	for i := 0; i < len(assets); i += u.batchSize {
		end := i + u.batchSize
		if end > len(assets) {
			end = len(assets)
		}

		batch := assets[i:end]
		success, fail := u.updateBatch(ctx, batch)
		successCount += success
		failCount += fail
	}

	u.logger.Printf("Price update complete: %d success, %d failed", successCount, failCount)
}

// updateBatch fetches and records prices for a batch of assets
func (u *PriceUpdater) updateBatch(ctx context.Context, assets []Asset) (success, fail int) {
	// Extract CoinGecko IDs
	coinGeckoIDs := make([]string, len(assets))
	idToAsset := make(map[string]*Asset)
	for i := range assets {
		coinGeckoIDs[i] = assets[i].CoinGeckoID
		idToAsset[assets[i].CoinGeckoID] = &assets[i]
	}

	// Fetch prices from provider
	prices, err := u.priceProvider.GetCurrentPrices(ctx, coinGeckoIDs)
	if err != nil {
		u.logger.Printf("Failed to fetch prices: %v", err)
		return 0, len(assets)
	}

	// Record prices
	for cgID, price := range prices {
		a, found := idToAsset[cgID]
		if !found {
			continue
		}

		pricePoint := &PricePoint{
			Time:     time.Now(),
			AssetID:  a.ID,
			PriceUSD: price,
			Source:   PriceSourceCoinGecko,
		}

		// Save to price_history
		if err := u.priceRepo.RecordPrice(ctx, pricePoint); err != nil {
			u.logger.Printf("Failed to record price for %s: %v", a.Symbol, err)
			fail++
			continue
		}

		// Update cache
		if u.cache != nil {
			_ = u.cache.Set(ctx, a.CoinGeckoID, price, "coingecko")
			_ = u.cache.SetStale(ctx, a.CoinGeckoID, price, "coingecko")
		}

		success++
	}

	// Count assets that didn't get a price from the API
	for i := range assets {
		if _, found := prices[assets[i].CoinGeckoID]; !found {
			fail++
		}
	}

	return success, fail
}

// RunOnce runs a single price update cycle (for testing)
func (u *PriceUpdater) RunOnce(ctx context.Context) {
	u.updatePrices(ctx)
}
