// apps/backend/internal/platform/price/quotability.go
package price

import (
	"context"
	"math/big"
)

// ContractPriceClient is a price source addressable by (chain, contract) — the
// identity the known-asset filter resolves on. DefiLlamaClient already satisfies
// it; the narrower interface is declared here so the quotability probe depends
// on the one method it uses rather than on a whole provider.
type ContractPriceClient interface {
	GetCurrentPrice(ctx context.Context, chain, addr string) (*big.Int, error)
}

// QuotabilityProbe answers "does the price provider quote this (chain,
// contract)" — level 2 of the known-asset filter (issue #58, decision #37).
//
// It deliberately asks for a PRICE and not for a legitimacy judgement. Measured
// on real data, a legitimacy verifier (token lists as an address verifier) cuts
// 4 of 5 real DeFi positions, because a debt token and an LP share are not coins
// and appear in no list — yet both have to be valued. Quotability covers 15/16
// of the real positions, including a debt token quoted at −0.9997. A NEGATIVE
// price is a perfectly good answer: the question is whether the asset can be
// valued, not whether the value is pleasant.
type QuotabilityProbe struct {
	client ContractPriceClient
}

// NewQuotabilityProbe builds the probe over a contract-addressable price client.
func NewQuotabilityProbe(client ContractPriceClient) *QuotabilityProbe {
	return &QuotabilityProbe{client: client}
}

// IsQuotable returns nil when the provider quotes the identity.
//
// The error is returned UNWRAPPED so the caller's errors.Is checks see the
// provider's own classification: ErrRateLimited and ErrTransient mean "ask
// again, this says nothing about the asset", while ErrNotFound, ErrLowConfidence
// and ErrUnsupportedChain are real negative answers. That distinction is what
// keeps a provider outage from convicting a real token.
func (p *QuotabilityProbe) IsQuotable(ctx context.Context, chain, contract string) error {
	_, err := p.client.GetCurrentPrice(ctx, chain, contract)
	return err
}
