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
