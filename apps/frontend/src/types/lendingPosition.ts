export interface LendingPosition {
  id: string
  wallet_id: string
  chain_id: string
  protocol: string
  supply_asset: string
  supply_amount: string
  supply_decimals: number
  supply_contract?: string
  borrow_asset?: string
  borrow_amount: string
  borrow_decimals?: number
  borrow_contract?: string
  status: 'active' | 'closed'
  opened_at: string
  closed_at?: string
}
