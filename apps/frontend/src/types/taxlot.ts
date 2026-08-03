export type CostBasisSource =
  | 'swap_price'
  | 'fmv_at_transfer'
  | 'linked_transfer'
  | 'genesis_approximation'
  | 'lending_carry_over'

/**
 * Whether the backend managed to price a lot or a disposal (#79).
 *
 * - `resolved`   — the money field carries a real number.
 * - `pending`    — no price yet; a later backfill may supply one.
 * - `unpriceable`— no price will ever arrive (no quote source for this asset).
 *
 * The status is what distinguishes "we don't know" from "it was worth nothing".
 * The money field itself is `null` in both non-resolved cases, and a `null` must
 * never reach the user as `$0.00`.
 */
export type PriceStatus = 'resolved' | 'pending' | 'unpriceable'

/**
 * A tax lot.
 *
 * `asset` is the registry UUID — the identifier. `asset_symbol` is the label
 * that rides alongside it, and `asset_contract` / `symbol_ambiguous` qualify
 * that label when the ticker does not name the asset uniquely on its chain
 * (#42). Before those fields the endpoint shipped a bare UUID, so a client
 * could only render the id at the user or fetch /assets once per lot.
 *
 * The cost-basis fields are nullable on purpose (#79): a lot acquired at a price
 * the backend could not resolve has no cost basis, which is not the same fact as
 * a cost basis of zero. `price_status` says which case a `null` is.
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
  auto_cost_basis_per_unit: string | null
  auto_cost_basis_source: CostBasisSource
  override_cost_basis_per_unit?: string
  override_reason?: string
  override_at?: string
  effective_cost_basis_per_unit: string | null
  price_status: PriceStatus
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
  // Null when no lot behind the position could be priced — a position of unknown
  // cost, not a position that cost nothing.
  weighted_avg_cost: string | null
}

export interface OverrideCostBasisRequest {
  cost_basis_per_unit: string
  reason: string
}

/**
 * One FIFO consumption of a lot.
 *
 * `proceeds_status` mirrors `TaxLot.price_status` for the disposal side, and
 * `pnl_excluded` says the realized gain/loss could not be computed at all
 * (either side unpriced), so it must not be rendered as a signed number.
 */
export interface DisposalDetail {
  id: string
  lot_id: string
  quantity_disposed: string
  proceeds_per_unit: string | null
  proceeds_status: PriceStatus
  disposal_type: 'sale' | 'internal_transfer' | 'gas_fee'
  disposed_at: string
  lot_asset: string
  lot_asset_symbol: string
  lot_asset_contract: string
  symbol_ambiguous: boolean
  lot_acquired_at: string
  lot_cost_basis_per_unit: string | null
  lot_auto_cost_basis_source: CostBasisSource
  realized_gain_loss: string | null
  pnl_excluded: boolean
}

export interface TransactionLotImpact {
  acquired_lots: TaxLot[]
  disposals: DisposalDetail[]
  has_lot_impact: boolean
}
