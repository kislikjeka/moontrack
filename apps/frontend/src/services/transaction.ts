import api from './api'
import type {
  TransactionDetail,
  TransactionListResponse,
  CreateTransactionRequest,
} from '@/types/transaction'

// Re-export types for convenience
export type {
  TransactionType,
  TransactionDirection,
  TransactionListItem,
  TransactionDetail,
  TransactionListResponse,
  CreateTransactionRequest,
  LedgerEntry,
} from '@/types/transaction'

// Legacy interface for backwards compatibility
export interface Transaction {
  id: string
  type: string
  wallet_id: string
  wallet_name?: string
  asset_id: string
  amount: string
  usd_rate?: string
  usd_value: string
  occurred_at: string
  recorded_at: string
  notes?: string
  price_source?: 'manual' | 'coingecko'
  status: 'COMPLETED' | 'FAILED'
}

export const transactionService = {
  /**
   * Create a new transaction (asset_adjustment only for manual creation)
   */
  async create(data: CreateTransactionRequest): Promise<Transaction> {
    const response = await api.post<Transaction>('/transactions', data)
    return response.data
  },

  /**
   * List transactions with optional filters and pagination
   */
  async list(params?: {
    wallet_id?: string
    asset_id?: string
    type?: string
    start_date?: string
    end_date?: string
    page?: number
    page_size?: number
  }): Promise<TransactionListResponse> {
    const response = await api.get<TransactionListResponse>('/transactions', {
      params,
    })
    return response.data
  },

  /**
   * Get a single transaction by ID with full details and ledger entries
   */
  async getById(id: string): Promise<TransactionDetail> {
    const response = await api.get<TransactionDetail>(`/transactions/${id}`)
    return response.data
  },
}

export default transactionService
