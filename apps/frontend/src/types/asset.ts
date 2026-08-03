/**
 * Asset is one `asset_registry` row as `/assets*` returns it.
 *
 * Identity in the registry is keyed on `(chain, contract)`, so both halves are
 * always present — neither is optional. `contract_address` carries the literal
 * `'native'` for a chain's native coin rather than an empty string: the backend
 * deliberately stopped translating that sentinel back at the edge (#59), and
 * re-introducing the translation here would put one of the four inconsistent
 * spellings of nativeness straight back.
 *
 * `asset_type`, `market_cap_rank` and `is_active` are absent rather than faked.
 * The registry has no source for them: it holds identities someone actually
 * held, not catalogue entries that can be ranked or deactivated.
 *
 * `coingecko_id` is the empty string when the asset has no listing.
 */
export interface Asset {
  id: string
  symbol: string
  name: string
  coingecko_id: string
  decimals: number
  chain_id: string
  contract_address: string
  /**
   * True when `symbol` does not name this asset uniquely on its chain, so
   * showing the ticker alone would render two different assets identically.
   *
   * Computed by the backend globally over the registry, not over the response
   * (#42) — otherwise the same token would be labelled `USDC` in one view and
   * `USDC · 0xaf88…` in another, depending only on what else was in view.
   * Clients qualify the ticker with a truncated `contract_address` when set.
   */
  symbol_ambiguous: boolean
}
