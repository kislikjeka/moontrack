export interface AssetHolding {
  asset_id: string
  total_amount: string
  usd_value: string
  current_price: string
}

export interface AssetBalance {
  asset_id: string
  amount: string
  usd_value: string
  price: string
}

export interface WalletBalance {
  wallet_id: string
  wallet_name: string
  chain_id: string
  assets: AssetBalance[]
  total_usd: string
}

export interface PortfolioSummary {
  total_usd_value: string
  total_assets: number
  asset_holdings: AssetHolding[]
  wallet_balances: WalletBalance[]
  last_updated: string
}
