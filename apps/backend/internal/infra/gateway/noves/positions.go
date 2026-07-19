package noves

import (
	"context"

	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
)

// Compile-time check that SyncAdapter also implements PositionDataProvider. The
// reconciler (Phase 2) needs on-chain balances to detect genesis deltas; Noves
// exposes them per-chain via the balances endpoint, so the adapter fans out.
var _ sync.PositionDataProvider = (*SyncAdapter)(nil)

// GetPositions fetches on-chain token balances across the wallet's supported
// chains and converts them to domain positions. The Noves balances endpoint is
// per-chain (like the tx endpoint), so we own the fan-out loop here, mapping each
// domain chain slug to its Noves short slug. A per-chain fetch error aborts the
// whole call (the reconciler treats a positions failure as a hard error rather
// than reconciling against a partial balance set, which could fabricate a
// genesis for an asset whose real balance simply failed to load).
func (a *SyncAdapter) GetPositions(ctx context.Context, address string) ([]sync.OnChainPosition, error) {
	var result []sync.OnChainPosition

	for _, domainChain := range wallet.GetSupportedChains() {
		novesChain, ok := domainToNovesChain(domainChain)
		if !ok {
			continue // unsupported chain: nothing to fetch
		}

		items, err := a.client.GetBalances(ctx, novesChain, address)
		if err != nil {
			return nil, err
		}

		for _, item := range items {
			pos, ok := convertBalance(item, domainChain)
			if !ok {
				continue
			}
			result = append(result, pos)
		}
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
