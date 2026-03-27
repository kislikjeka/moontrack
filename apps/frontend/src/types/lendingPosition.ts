export interface LendingPositionAsset {
  side: 'supply' | 'borrow'
  asset: string
  amount: string
  decimals: number
  contract?: string
  total_in: string
  total_out: string
  total_in_usd: string
  total_out_usd: string
}

export interface LendingPosition {
  id: string
  wallet_id: string
  chain_id: string
  protocol: string
  interest_earned_usd: string
  assets: LendingPositionAsset[]
  status: 'active' | 'closed'
  opened_at: string
  closed_at?: string
}
