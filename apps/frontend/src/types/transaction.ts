// Transaction types - transfer_in/transfer_out from sync, internal_transfer between own wallets, asset_adjustment manual
export type TransactionType =
  | 'transfer_in'
  | 'transfer_out'
  | 'internal_transfer'
  | 'asset_adjustment'

export type TransactionDirection = 'in' | 'out' | 'adjustment' | 'internal'
export type TransactionStatus = 'COMPLETED' | 'FAILED'

export interface TransactionListItem {
  id: string
  type: TransactionType
  type_label: string
  asset_id: string
  asset_symbol: string
  amount: string
  display_amount: string
  direction: TransactionDirection
  wallet_id: string
  wallet_name: string
  status: TransactionStatus
  occurred_at: string
  usd_value?: string
}

export interface LedgerEntry {
  id: string
  account_code: string
  account_label: string
  debit_credit: 'DEBIT' | 'CREDIT'
  entry_type: string
  amount: string
  display_amount: string
  asset_id: string
  asset_symbol: string
  usd_value?: string
}

export interface TransactionDetail extends TransactionListItem {
  source: string
  external_id?: string
  recorded_at: string
  notes?: string
  raw_data?: Record<string, unknown>
  entries: LedgerEntry[]
}

export interface TransactionListResponse {
  transactions: TransactionListItem[]
  total: number
  page: number
  page_size: number
}

// CreateTransactionRequest - only for asset_adjustment (transfers come from sync)
export interface CreateTransactionRequest {
  type: 'asset_adjustment'
  wallet_id: string
  asset_id: string
  coingecko_id?: string
  amount: string
  new_balance: string
  usd_rate?: string
  occurred_at?: string
  notes?: string
}

export interface TransactionFilters {
  wallet_id?: string
  asset_id?: string
  type?: TransactionType
  start_date?: string
  end_date?: string
  page?: number
  page_size?: number
}
