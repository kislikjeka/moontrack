import api from './api';

export interface Transaction {
  id: string;
  type: 'manual_income' | 'manual_outcome' | 'asset_adjustment';
  wallet_id: string;
  wallet_name?: string;
  asset_id: string;
  amount: string; // Big number as string
  usd_rate?: string; // Big number as string (scaled by 10^8)
  usd_value: string; // Big number as string
  occurred_at: string; // ISO timestamp
  recorded_at: string; // ISO timestamp
  notes?: string;
  price_source?: 'manual' | 'coingecko';
  status: 'COMPLETED' | 'FAILED';
}

export interface TransactionListResponse {
  transactions: Transaction[];
  total: number;
  page: number;
  page_size: number;
}

export interface CreateTransactionRequest {
  type: 'manual_income' | 'manual_outcome' | 'asset_adjustment';
  wallet_id: string;
  asset_id: string;
  amount: string;
  usd_rate?: string; // Optional manual price override
  occurred_at?: string; // ISO timestamp, defaults to now
  notes?: string;
  new_balance?: string; // For asset_adjustment type only
}

export const transactionService = {
  /**
   * Create a new transaction (income, outcome, or asset adjustment)
   */
  async create(data: CreateTransactionRequest): Promise<Transaction> {
    const response = await api.post<Transaction>('/transactions', data);
    return response.data;
  },

  /**
   * List transactions with optional filters and pagination
   */
  async list(params?: {
    wallet_id?: string;
    asset_id?: string;
    type?: string;
    start_date?: string;
    end_date?: string;
    page?: number;
    page_size?: number;
  }): Promise<TransactionListResponse> {
    const response = await api.get<TransactionListResponse>('/transactions', {
      params,
    });
    return response.data;
  },

  /**
   * Get a single transaction by ID
   */
  async getById(id: string): Promise<Transaction> {
    const response = await api.get<Transaction>(`/transactions/${id}`);
    return response.data;
  },
};

export default transactionService;
