package noves

import (
	"context"

	"github.com/kislikjeka/moontrack/internal/platform/sync"
)

// Compile-time check that SyncAdapter also implements PositionDataProvider. The
// reconciler (Phase 2) needs on-chain balances to detect where a position and the
// collected history disagree; Noves exposes them per-chain via the balances
// endpoint, and the reconciler owns the fan-out over the wallet's chain set
// (issue #27).
var _ sync.PositionDataProvider = (*SyncAdapter)(nil)

// GetPositions fetches on-chain token balances for a single chain and converts
// them to domain positions. Chain-aware: the Reconciler owns the fan-out over the
// wallet's chain set (issue #27) and calls this once per enabled chain, so a
// wallet reconciles only its enabled chains. The domain chain slug is mapped to
// its Noves short slug here; an unmapped (non-Compatible) chain yields no
// positions rather than an error.
func (a *SyncAdapter) GetPositions(ctx context.Context, address, chain string) ([]sync.OnChainPosition, error) {
	novesChain, ok := domainToNovesChain(chain)
	if !ok {
		return nil, nil // unsupported chain: nothing to fetch
	}

	items, err := a.client.GetBalances(ctx, novesChain, address)
	if err != nil {
		return nil, err
	}

	var result []sync.OnChainPosition
	for _, item := range items {
		pos, ok := convertBalance(item, chain)
		if !ok {
			continue
		}
		result = append(result, pos)
	}

	return result, nil
}

// convertBalance maps a Noves BalanceItem to a domain OnChainPosition on the
// given (canonical) chain. Zero/empty balances are dropped (the reconciler only
// acts on positive quantities anyway). Amounts are decimal strings converted to
// base units exactly, reusing the transfer-side conversion helpers.
func convertBalance(item BalanceItem, domainChain string) (sync.OnChainPosition, bool) {
	if item.Token == nil {
		return sync.OnChainPosition{}, false
	}

	// Reuse the transfer amount conversion. A review reason (excess precision) is
	// not actionable for a balance snapshot — the reconciler compares base units —
	// so we take the converted value and ignore the flag.
	quantity, _ := amountToBaseUnits(item.Balance, item.Token.Decimals, item.Token.Symbol)
	if quantity == nil || quantity.Sign() <= 0 {
		return sync.OnChainPosition{}, false
	}

	return sync.OnChainPosition{
		ChainID:         domainChain,
		AssetSymbol:     item.Token.Symbol,
		AssetName:       item.Token.Name,
		ContractAddress: normalizeContract(item.Token.Address),
		Decimals:        item.Token.Decimals,
		Quantity:        quantity,
		USDPrice:        nil, // Noves price is best-effort/null; MoonTrack prices via its own pipeline
		IconURL:         "",
	}, true
}
