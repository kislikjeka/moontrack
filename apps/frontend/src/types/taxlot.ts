export type CostBasisSource =
  | 'swap_price'
  | 'fmv_at_transfer'
  | 'linked_transfer'
  | 'genesis_approximation'

/**
 * A tax lot.
 *
 * `asset` is the registry UUID — the identifier. `asset_symbol` is the label
 * that rides alongside it, and `asset_contract` / `symbol_ambiguous` qualify
 * that label when the ticker does not name the asset uniquely on its chain
 * (#42). Before those fields the endpoint shipped a bare UUID, so a client
 * could only render the id at the user or fetch /assets once per lot.
 */
export interface TaxLot {
  id: string
  transaction_id: string
  account_id: string
  chain_id?: string
  asset: string
  asset_symbol: string
  asset_contract: string
  symbol_ambiguous: boolean
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
  asset_symbol: string
  asset_contract: string
  symbol_ambiguous: boolean
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
  lot_asset_symbol: string
  lot_asset_contract: string
  symbol_ambiguous: boolean
  lot_acquired_at: string
  lot_cost_basis_per_unit: string
  lot_auto_cost_basis_source: CostBasisSource
  realized_gain_loss: string
}

export interface TransactionLotImpact {
  acquired_lots: TaxLot[]
  disposals: DisposalDetail[]
  has_lot_impact: boolean
}
