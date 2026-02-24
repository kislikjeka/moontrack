export interface AssetHolding {
  asset_id: string
  total_amount: string
  usd_value: string // Human-readable decimal, e.g. "41.15"
  current_price: string // Human-readable decimal, e.g. "82304.52"
}

export interface AssetBalance {
  asset_id: string
  chain_id?: string // Zerion chain name, e.g. "ethereum", "base"
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

export interface HoldingGroup {
  asset_id: string
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
