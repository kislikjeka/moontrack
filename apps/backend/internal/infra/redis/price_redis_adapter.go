package redis

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// PriceRedisAdapter wraps a *redis.Client and satisfies the price.RedisClient interface.
// price.RedisClient requires:
//
//	Get(ctx, key) (string, bool, error)
//	Set(ctx, key, value string, ttl time.Duration) error
type PriceRedisAdapter struct {
	c *goredis.Client
}

// NewPriceRedisAdapter creates a PriceRedisAdapter from the existing go-redis client.
func NewPriceRedisAdapter(c *goredis.Client) *PriceRedisAdapter {
	return &PriceRedisAdapter{c: c}
}

// Get retrieves a string value from Redis. Returns ("", false, nil) on cache miss.
func (a *PriceRedisAdapter) Get(ctx context.Context, key string) (string, bool, error) {
	val, err := a.c.Get(ctx, key).Result()
	if err == goredis.Nil {
		return "", false, nil
	}
	if err != nil {
		// Swallow Redis errors gracefully — cache misses are acceptable.
		return "", false, err
	}
	return val, true, nil
}

// Set stores a string value with the given TTL.
func (a *PriceRedisAdapter) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return a.c.Set(ctx, key, value, ttl).Err()
}
