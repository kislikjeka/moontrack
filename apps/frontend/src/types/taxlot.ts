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
  chain_id?: string
  is_aggregated?: boolean
  asset: string
  total_quantity: string
  weighted_avg_cost: string
}

export interface OverrideCostBasisRequest {
  cost_basis_per_unit: string
  reason: string
}

export interface DisposalDetail {
  id: string
  lot_id: string
  quantity_disposed: string
  proceeds_per_unit: string
  disposal_type: 'sale' | 'internal_transfer' | 'gas_fee'
  disposed_at: string
  lot_asset: string
  lot_acquired_at: string
  lot_cost_basis_per_unit: string
  lot_auto_cost_basis_source: CostBasisSource
}

export interface TransactionLotImpact {
  acquired_lots: TaxLot[]
  disposals: DisposalDetail[]
  has_lot_impact: boolean
}
