// apps/backend/internal/platform/price/model.go
package price

import (
	"context"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/platform/asset"
)

// Source identifies which provider produced a price record.
type Source string

const (
	SourceCoinGecko     Source = "coingecko"
	SourceZerion        Source = "zerion"
	SourceGeckoTerminal Source = "geckoterminal"
	SourceDefiLlama     Source = "defillama"
	SourceManual        Source = "manual"
)

// PriceStatus is the lifecycle status of a tax lot's cost basis.
type PriceStatus string

const (
	PriceStatusResolved    PriceStatus = "resolved"
	PriceStatusPending     PriceStatus = "pending"
	PriceStatusUnpriceable PriceStatus = "unpriceable"
)

// HistoricalPrice is a provider's response to a point-in-time price lookup.
type HistoricalPrice struct {
	PriceUSD   *big.Int  // scaled 10^8
	Timestamp  time.Time // actual point-in-time the price is for
	Confidence float64   // 0..1; 1.0 for providers without a confidence field
}

// Provider is the fallback-provider contract.
type Provider interface {
	Name() Source
	GetPrice(ctx context.Context, a asset.Asset) (*big.Int, error)
	GetHistoricalPrice(ctx context.Context, a asset.Asset, t time.Time) (*HistoricalPrice, error)
}

// PriceReader exposes priority-ordered reads over price_history.
type PriceReader interface {
	Current(ctx context.Context, assetID uuid.UUID) (*big.Int, Source, error)
	Historical(ctx context.Context, assetID uuid.UUID, ts time.Time) (*HistoricalPrice, Source, error)
}
