package price

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
)

// RedisClient is the minimal subset of Redis operations the cache needs.
type RedisClient interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
}

// Cache dedupes historical-price lookups across users/providers.
// Historical prices are immutable, so we use a long TTL (30 days).
type Cache struct {
	r   RedisClient
	ttl time.Duration
}

func NewCache(r RedisClient, ttl time.Duration) *Cache {
	return &Cache{r: r, ttl: ttl}
}

func (c *Cache) historicalKey(src Source, assetID uuid.UUID, t time.Time) string {
	return fmt.Sprintf("price:hist:%s:%s:%s", src, assetID, t.UTC().Format("2006-01-02T15:04Z"))
}

type cachedHistorical struct {
	PriceStr   string  `json:"p"`
	TsUnix     int64   `json:"t"`
	Confidence float64 `json:"c"`
}

func (c *Cache) PutHistorical(ctx context.Context, src Source, assetID uuid.UUID, at time.Time, hp *HistoricalPrice) error {
	payload := cachedHistorical{
		PriceStr:   hp.PriceUSD.String(),
		TsUnix:     hp.Timestamp.Unix(),
		Confidence: hp.Confidence,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return c.r.Set(ctx, c.historicalKey(src, assetID, at), string(b), c.ttl)
}

func (c *Cache) GetHistorical(ctx context.Context, src Source, assetID uuid.UUID, at time.Time) (*HistoricalPrice, bool, error) {
	v, ok, err := c.r.Get(ctx, c.historicalKey(src, assetID, at))
	if err != nil || !ok {
		return nil, ok, err
	}
	var payload cachedHistorical
	if err := json.Unmarshal([]byte(v), &payload); err != nil {
		return nil, false, err
	}
	price := new(big.Int)
	price.SetString(payload.PriceStr, 10)
	return &HistoricalPrice{
		PriceUSD:   price,
		Timestamp:  time.Unix(payload.TsUnix, 0).UTC(),
		Confidence: payload.Confidence,
	}, true, nil
}
