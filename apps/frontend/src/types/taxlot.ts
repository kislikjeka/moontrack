export type CostBasisSource =
  | 'swap_price'
  | 'fmv_at_transfer'
  | 'linked_transfer'
  | 'genesis_approximation'

export interface TaxLot {
  id: string
  transaction_id: string
  account_id: string
  asset: string
  quantity_acquired: string
  quantity_remaining: string
  acquired_at: string
  auto_cost_basis_per_unit: string
  auto_cost_basis_source: CostBasisSource
  override_cost_basis_per_unit?: string
  override_reason?: string
  override_at?: string
  effective_cost_basis_per_unit: string
  linked_source_lot_id?: string
}

export interface PositionWAC {
  wallet_id: string
  wallet_name: string
  account_id: string
  asset: string
  total_quantity: string
  weighted_avg_cost: string
}

export interface OverrideCostBasisRequest {
  cost_basis_per_unit: string
  reason: string
}
