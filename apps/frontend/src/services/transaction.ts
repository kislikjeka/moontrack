import api from './api';

export type TransactionType = 'manual_income' | 'manual_outcome' | 'asset_adjustment';
export type TransactionDirection = 'in' | 'out' | 'adjustment';

// List view item with enriched fields
export interface TransactionListItem {
  id: string;
  type: TransactionType;
  type_label: string; // "Income", "Outcome", "Adjustment"
  asset_id: string;
  asset_symbol: string; // "BTC", "ETH"
  amount: string; // Base units as string
  display_amount: string; // "0.5 BTC"
  direction: TransactionDirection;
  wallet_id: string;
  wallet_name: string;
  status: 'COMPLETED' | 'FAILED';
  occurred_at: string; // ISO timestamp
  usd_value?: string;
}

// Ledger entry for detail view
export interface LedgerEntry {
  id: string;
  account_code: string;
  account_label: string; // "My Wallet - BTC"
  debit_credit: 'DEBIT' | 'CREDIT';
  entry_type: string; // "asset_increase", "income", etc.
  amount: string;
  display_amount: string;
  asset_id: string;
  asset_symbol: string;
  usd_value?: string;
}

// Detail view with full information
export interface TransactionDetail extends TransactionListItem {
  source: string;
  external_id?: string;
  recorded_at: string;
  notes?: string;
  raw_data?: Record<string, unknown>;
  entries: LedgerEntry[];
}

// Legacy interface for backwards compatibility
export interface Transaction {
  id: string;
  type: TransactionType;
  wallet_id: string;
  wallet_name?: string;
  asset_id: string;
  amount: string;
  usd_rate?: string;
  usd_value: string;
  occurred_at: string;
  recorded_at: string;
  notes?: string;
  price_source?: 'manual' | 'coingecko';
  status: 'COMPLETED' | 'FAILED';
}

export interface TransactionListResponse {
  transactions: TransactionListItem[];
  total: number;
  page: number;
  page_size: number;
}

export interface CreateTransactionRequest {
  type: 'manual_income' | 'manual_outcome' | 'asset_adjustment';
  wallet_id: string;
  asset_id: string;
  coingecko_id?: string; // CoinGecko ID for price lookup (e.g., "bitcoin" for BTC)
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
   * Get a single transaction by ID with full details and ledger entries
   */
  async getById(id: string): Promise<TransactionDetail> {
    const response = await api.get<TransactionDetail>(`/transactions/${id}`);
    return response.data;
  },
};

export default transactionService;
