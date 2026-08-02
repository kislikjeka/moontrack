package sync

import (
	"context"

	"github.com/kislikjeka/moontrack/internal/platform/price"
)

// ContractQuotabilityProbe is the price-side probe, stated in the price
// package's own vocabulary (chain and contract as plain strings).
type ContractQuotabilityProbe interface {
	IsQuotable(ctx context.Context, chain, contract string) error
}

// QuotabilityProbeAdapter adapts a contract-addressable price probe to the
// AssetKey vocabulary the knownness worker speaks.
//
// It exists so the price package never has to know what an AssetKey is: the
// dependency arrow runs sync → price, and this is the one place the two
// vocabularies meet.
type QuotabilityProbeAdapter struct {
	probe ContractQuotabilityProbe
}

// NewQuotabilityProbeAdapter wraps a price-side probe for the knownness worker.
func NewQuotabilityProbeAdapter(probe ContractQuotabilityProbe) *QuotabilityProbeAdapter {
	return &QuotabilityProbeAdapter{probe: probe}
}

// IsQuotable asks the price provider about an on-chain identity.
//
// A NATIVE key is refused outright, with ErrUnsupportedChain. The probe is
// addressed by contract and a native coin has none, so asking would be
// meaningless — and, more importantly, a native identity must never reach this
// path at all: level 1 grants it knownness by construction. Reaching here means
// the symbol check failed, i.e. this is a contract-less leg that is NOT the
// chain's coin, and the honest answer for it is "the provider cannot value
// this".
func (a *QuotabilityProbeAdapter) IsQuotable(ctx context.Context, key AssetKey) error {
	if key.IsNative() {
		return price.ErrUnsupportedChain
	}
	return a.probe.IsQuotable(ctx, key.Chain, key.Contract)
}
