export interface AssetHolding {
  asset_id: string
  total_amount: string
  usd_value: string // Human-readable decimal, e.g. "41.15"
  current_price: string // Human-readable decimal, e.g. "82304.52"
}

export interface AssetBalance {
  asset_id: string
  amount: string
  usd_value: string // Human-readable decimal, e.g. "41.15"
  price: string // Human-readable decimal, e.g. "82304.52"
}

export interface WalletBalance {
  wallet_id: string
  wallet_name: string
  chain_id: string
  assets: AssetBalance[]
  total_usd: string // Human-readable decimal, e.g. "41.15"
}

export interface PortfolioSummary {
  total_usd_value: string // Human-readable decimal, e.g. "41.15"
  total_assets: number
  asset_holdings: AssetHolding[]
  wallet_balances: WalletBalance[]
  last_updated: string
}
