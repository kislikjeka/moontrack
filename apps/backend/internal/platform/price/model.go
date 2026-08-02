// apps/backend/internal/platform/price/model.go
package price

import (
	"context"
	"math/big"
	"time"

	"github.com/google/uuid"
)

// nativeContract is the registry's spelling of a chain's native coin
// (asset_registry.contract = 'native', issue #56).
//
// It is duplicated here rather than imported from the sync package that owns it,
// because price must not depend on sync — see the Asset doc below. The constant
// is a stored data value, not a shared abstraction: it is fixed by the
// asset_registry rows on disk, so the two declarations cannot drift apart
// without a migration that would have to rewrite those rows anyway.
const nativeContract = "native"

// Asset is the identity a price provider needs, and nothing more: who to ask
// about (the registry UUID, for caching and for writing price_history) and the
// two addressing schemes the providers actually accept — a CoinGecko slug, or
// an on-chain (chain, contract) pair.
//
// It is declared HERE rather than reused from another package (#59). It used to
// be asset.Asset, a row of the `assets` table that this ticket drops; the
// obvious replacement, the registry row, lives in the sync package, and a
// price → sync import would couple two platform peers so that the price chain
// depends on the blockchain-sync vocabulary (AssetKey, native sentinel) it has
// no business knowing. A four-field value type owned by the price package keeps
// the arrow pointing the other way: whoever holds registry rows adapts them
// into this, and the provider chain stays addressable from anywhere.
//
// Contract carries the registry's spelling verbatim, including the `native`
// sentinel for a chain's native coin. DefiLlama is asked about it exactly as it
// is asked about a token address — see DefiLlamaProvider for why that is not an
// error, and why native coins can still be priced.
type Asset struct {
	// ID is the asset_registry UUID. It keys the historical-price cache and is
	// the asset_id written to price_history.
	ID uuid.UUID

	// CoinGeckoID is the provider slug ("ethereum", "usd-coin"), empty when the
	// asset has no CoinGecko listing. Only CoinGeckoProvider reads it.
	CoinGeckoID string

	// Chain and Contract are the on-chain identity, as stored in the registry.
	// Only the contract-addressable providers read them.
	Chain    string
	Contract string
}

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

// Quote is a current price plus the source that won the priority ordering. It
// is the value half of a batch current-price read, where the asset UUID is the
// map key and so does not need repeating in the value.
type Quote struct {
	PriceUSD *big.Int // scaled 10^8
	Source   Source
}

// HistoryPoint is one bucketed price observation over a time range, as the
// /assets/{id}/history endpoint serves it (#59).
//
// It is distinct from HistoricalPrice: that is a PROVIDER's answer to "what was
// this worth at this instant", carrying a confidence. This is a row already
// recorded in price_history, aggregated into a bucket — the price is a fact we
// stored, so there is nothing to be confident about.
type HistoryPoint struct {
	Time     time.Time
	PriceUSD *big.Int // scaled 10^8
	Source   Source
}

// Provider is the fallback-provider contract.
type Provider interface {
	Name() Source
	GetPrice(ctx context.Context, a Asset) (*big.Int, error)
	GetHistoricalPrice(ctx context.Context, a Asset, t time.Time) (*HistoricalPrice, error)
}

// PriceReader exposes priority-ordered reads over price_history.
type PriceReader interface {
	Current(ctx context.Context, assetID uuid.UUID) (*big.Int, Source, error)
	Historical(ctx context.Context, assetID uuid.UUID, ts time.Time) (*HistoricalPrice, Source, error)
}
