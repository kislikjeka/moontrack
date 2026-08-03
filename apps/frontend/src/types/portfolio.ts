/**
 * AssetIdentity is how every asset-bearing response names an asset.
 *
 * `asset_id` is the registry UUID and the ONLY identifier — group by it, key
 * react-query on it, query lots by it. `asset_symbol` rides alongside as data,
 * never as an identity: two different assets can carry the same ticker, which is
 * precisely why grouping moved off it (#42).
 *
 * `symbol_ambiguous` says the ticker does not name this asset uniquely on its
 * chain. It is computed by the backend globally over the registry, so a label
 * does not change meaning depending on what else is on screen. When set, qualify
 * the ticker with a truncated `asset_contract`.
 */
export interface AssetIdentity {
  asset_id: string
  asset_symbol: string
  asset_contract: string
  symbol_ambiguous: boolean
}

export interface AssetHolding extends AssetIdentity {
  chain_id: string
  total_amount: string
  usd_value: string // Human-readable decimal, e.g. "41.15"
  current_price: string // Human-readable decimal, e.g. "82304.52"
}

export interface AssetBalance extends AssetIdentity {
  chain_id?: string // chain name, e.g. "ethereum", "base"
  amount: string
  usd_value: string // Human-readable decimal, e.g. "41.15"
  price: string // Human-readable decimal, e.g. "82304.52"
}

/**
 * The weighted average cost fields below are optional AND nullable, which reads
 * like an inconsistency next to `TaxLot.effective_cost_basis_per_unit` — that one
 * is always present and merely null when unknown (#79). Both spellings are
 * correct here because they come from different encoders: the lots endpoint
 * emits the key unconditionally, while these WAC fields carry `omitempty`, so an
 * unpriced position drops the key entirely rather than sending null.
 *
 * The distinction is in the wire format, never in the meaning: absent and null
 * are the same fact — "no priced lot backs this position" — and neither is zero.
 * Consumers must therefore treat a missing key and an explicit null identically,
 * which a plain truthiness check already does.
 */
export interface ChainHolding {
  chain_id: string
  amount: string
  usd_value: string
  wac?: string | null
}

export interface HoldingGroup extends AssetIdentity {
  total_amount: string
  total_usd_value: string
  price: string
  aggregated_wac?: string | null
  chains: ChainHolding[]
}

export interface WalletBalance {
  wallet_id: string
  wallet_name: string
  assets: AssetBalance[]
  holdings: HoldingGroup[]
  total_usd: string // Human-readable decimal, e.g. "41.15"
}

export interface PortfolioSummary {
  total_usd_value: string // Human-readable decimal, e.g. "41.15"
  total_assets: number
  asset_holdings: AssetHolding[]
  wallet_balances: WalletBalance[]
  last_updated: string

  /**
   * Whether `total_usd_value` was computed over an incomplete set of prices, and
   * how incomplete (#79). A total that omits unpriced lots understates the
   * portfolio, and saying so is the whole point — an unqualified number invites
   * the user to trust a figure the backend knows is partial.
   *
   * The two counts are NOT interchangeable. `pending_lot_count` resolves itself
   * on the next backfill; `unpriceable_lot_count` never will, and needs the user
   * to enter a cost basis by hand. Note that the backend derives
   * `pnl_is_partial` from the pending count alone, so it stays false while
   * unpriceable lots exist — render on the counts, not on the flag, or a
   * portfolio with only unpriceable lots reports itself as complete.
   *
   * Optional because a backend predating this change omits them; absent is read
   * as "nothing known to be missing", the same as zero.
   */
  pnl_is_partial?: boolean
  pending_lot_count?: number
  unpriceable_lot_count?: number
}
