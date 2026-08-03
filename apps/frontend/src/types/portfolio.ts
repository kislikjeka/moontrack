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

export interface ChainHolding {
  chain_id: string
  amount: string
  usd_value: string
  wac?: string
}

export interface HoldingGroup extends AssetIdentity {
  total_amount: string
  total_usd_value: string
  price: string
  aggregated_wac?: string
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
}
