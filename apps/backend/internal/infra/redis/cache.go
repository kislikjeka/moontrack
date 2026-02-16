package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/kislikjeka/moontrack/pkg/logger"
	"github.com/redis/go-redis/v9"
)

const (
	// DefaultTTL is the default TTL for cached prices (60 seconds per research.md)
	DefaultTTL = 60 * time.Second

	// StaleTTL is the TTL for stale cache fallback (24 hours per research.md)
	StaleTTL = 24 * time.Hour

	// KeyPrefix is the prefix for price cache keys
	KeyPrefix = "price:"
)

// Cache represents a Redis-backed price cache
type Cache struct {
	client *redis.Client
	ttl    time.Duration
	logger *logger.Logger
}

// NewCache creates a new price cache
func NewCache(client *redis.Client, log *logger.Logger) *Cache {
	return &Cache{
		client: client,
		ttl:    DefaultTTL,
		logger: log.WithField("component", "cache"),
	}
}

// NewCacheWithTTL creates a new price cache with custom TTL
func NewCacheWithTTL(client *redis.Client, ttl time.Duration, log *logger.Logger) *Cache {
	return &Cache{
		client: client,
		ttl:    ttl,
		logger: log.WithField("component", "cache"),
	}
}

// CachedPrice represents a cached price with metadata
type CachedPrice struct {
	AssetID   string    `json:"asset_id"`
	USDPrice  string    `json:"usd_price"` // big.Int serialized as string
	UpdatedAt time.Time `json:"updated_at"`
	Source    string    `json:"source"` // "coingecko" or "manual"
}

// Get retrieves a cached price for an asset
func (c *Cache) Get(ctx context.Context, assetID string) (*big.Int, bool, error) {
	key := fmt.Sprintf("%s%s:usd", KeyPrefix, assetID)

	val, err := c.client.Get(ctx, key).Result()
	if err == redis.Nil {
		c.logger.Debug("cache miss", "asset_id", assetID)
		return nil, false, nil // Not found
	}
	if err != nil {
		c.logger.Error("cache error", "operation", "get", "asset_id", assetID, "error", err)
		return nil, false, fmt.Errorf("failed to get cached price: %w", err)
	}

	// Deserialize cached price
	var cached CachedPrice
	if err := json.Unmarshal([]byte(val), &cached); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal cached price: %w", err)
	}

	// Parse big.Int from string
	price := new(big.Int)
	if _, ok := price.SetString(cached.USDPrice, 10); !ok {
		return nil, false, fmt.Errorf("failed to parse cached price: invalid number")
	}

	c.logger.Debug("cache hit", "asset_id", assetID)
	return price, true, nil
}

// Set stores a price in the cache with default TTL
func (c *Cache) Set(ctx context.Context, assetID string, price *big.Int, source string) error {
	return c.SetWithTTL(ctx, assetID, price, source, c.ttl)
}

// SetWithTTL stores a price in the cache with custom TTL
func (c *Cache) SetWithTTL(ctx context.Context, assetID string, price *big.Int, source string, ttl time.Duration) error {
	key := fmt.Sprintf("%s%s:usd", KeyPrefix, assetID)

	cached := CachedPrice{
		AssetID:   assetID,
		USDPrice:  price.String(),
		UpdatedAt: time.Now().UTC(),
		Source:    source,
	}

	data, err := json.Marshal(cached)
	if err != nil {
		return fmt.Errorf("failed to marshal price: %w", err)
	}

	if err := c.client.Set(ctx, key, data, ttl).Err(); err != nil {
		c.logger.Error("cache error", "operation", "set", "asset_id", assetID, "error", err)
		return fmt.Errorf("failed to set cached price: %w", err)
	}

	return nil
}

// SetStale stores a price in the stale cache (24-hour TTL for fallback)
func (c *Cache) SetStale(ctx context.Context, assetID string, price *big.Int, source string) error {
	key := fmt.Sprintf("%s%s:usd:stale", KeyPrefix, assetID)

	cached := CachedPrice{
		AssetID:   assetID,
		USDPrice:  price.String(),
		UpdatedAt: time.Now().UTC(),
		Source:    source,
	}

	data, err := json.Marshal(cached)
	if err != nil {
		return fmt.Errorf("failed to marshal stale price: %w", err)
	}

	if err := c.client.Set(ctx, key, data, StaleTTL).Err(); err != nil {
		return fmt.Errorf("failed to set stale cached price: %w", err)
	}

	return nil
}

// GetStale retrieves a price from the stale cache (fallback when API fails)
func (c *Cache) GetStale(ctx context.Context, assetID string) (*big.Int, bool, error) {
	key := fmt.Sprintf("%s%s:usd:stale", KeyPrefix, assetID)

	val, err := c.client.Get(ctx, key).Result()
	if err == redis.Nil {
		c.logger.Debug("stale cache miss", "asset_id", assetID)
		return nil, false, nil // Not found
	}
	if err != nil {
		c.logger.Error("cache error", "operation", "get_stale", "asset_id", assetID, "error", err)
		return nil, false, fmt.Errorf("failed to get stale cached price: %w", err)
	}

	// Deserialize cached price
	var cached CachedPrice
	if err := json.Unmarshal([]byte(val), &cached); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal stale cached price: %w", err)
	}

	// Parse big.Int from string
	price := new(big.Int)
	if _, ok := price.SetString(cached.USDPrice, 10); !ok {
		return nil, false, fmt.Errorf("failed to parse stale cached price: invalid number")
	}

	return price, true, nil
}

// GetMultiple retrieves cached prices for multiple assets
func (c *Cache) GetMultiple(ctx context.Context, assetIDs []string) (map[string]*big.Int, error) {
	if len(assetIDs) == 0 {
		return make(map[string]*big.Int), nil
	}

	// Build keys
	keys := make([]string, len(assetIDs))
	for i, assetID := range assetIDs {
		keys[i] = fmt.Sprintf("%s%s:usd", KeyPrefix, assetID)
	}

	// Get all prices in a pipeline
	pipe := c.client.Pipeline()
	cmds := make([]*redis.StringCmd, len(keys))
	for i, key := range keys {
		cmds[i] = pipe.Get(ctx, key)
	}

	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		// Ignore individual key misses, only fail on pipeline errors
	}

	// Parse results
	result := make(map[string]*big.Int)
	for i, cmd := range cmds {
		val, err := cmd.Result()
		if err == redis.Nil {
			continue // Skip missing keys
		}
		if err != nil {
			continue // Skip errors for individual keys
		}

		// Deserialize
		var cached CachedPrice
		if err := json.Unmarshal([]byte(val), &cached); err != nil {
			continue
		}

		// Parse big.Int
		price := new(big.Int)
		if _, ok := price.SetString(cached.USDPrice, 10); !ok {
			continue
		}

		result[assetIDs[i]] = price
	}

	return result, nil
}

// Delete removes a cached price
func (c *Cache) Delete(ctx context.Context, assetID string) error {
	key := fmt.Sprintf("%s%s:usd", KeyPrefix, assetID)
	return c.client.Del(ctx, key).Err()
}

// Clear removes all cached prices
func (c *Cache) Clear(ctx context.Context) error {
	pattern := fmt.Sprintf("%s*", KeyPrefix)
	iter := c.client.Scan(ctx, 0, pattern, 0).Iterator()

	pipe := c.client.Pipeline()
	count := 0
	for iter.Next(ctx) {
		pipe.Del(ctx, iter.Val())
		count++
		if count >= 100 {
			if _, err := pipe.Exec(ctx); err != nil {
				return fmt.Errorf("failed to clear cache: %w", err)
			}
			pipe = c.client.Pipeline()
			count = 0
		}
	}

	if count > 0 {
		if _, err := pipe.Exec(ctx); err != nil {
			return fmt.Errorf("failed to clear cache: %w", err)
		}
	}

	return iter.Err()
}
