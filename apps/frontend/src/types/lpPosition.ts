export interface LPPosition {
  id: string
  wallet_id: string
  chain_id: string
  protocol: string
  nft_token_id?: string
  contract_address?: string
  token0_symbol: string
  token1_symbol: string
  token0_decimals: number
  token1_decimals: number
  total_deposited_usd: string
  total_withdrawn_usd: string
  total_claimed_fees_usd: string
  remaining_token0: string
  remaining_token1: string
  status: 'open' | 'closed'
  opened_at: string
  closed_at?: string
  realized_pnl_usd?: string
  apr_bps?: number
}
