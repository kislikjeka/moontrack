export type AssetType = 'crypto' | 'fiat' | 'custom'

export interface Asset {
  id: string
  symbol: string
  name: string
  coingecko_id: string
  decimals: number
  asset_type: AssetType
  chain_id?: string
  contract_address?: string
  market_cap_rank?: number
  is_active: boolean
}

export interface PriceResponse {
  asset_id: string
  price_usd: string
  source: string
  timestamp: string
  is_stale?: boolean
}

export interface PriceHistoryPoint {
  timestamp: string
  price_usd: string
}

export interface PriceHistoryResponse {
  asset_id: string
  from: string
  to: string
  interval: '1h' | '1d' | '1w'
  prices: PriceHistoryPoint[]
}
