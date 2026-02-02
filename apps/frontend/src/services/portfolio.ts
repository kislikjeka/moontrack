import api from './api';

export interface AssetHolding {
  asset_id: string;
  total_amount: string;
  usd_value: string;
  current_price: string;
}

export interface AssetBalance {
  asset_id: string;
  amount: string;
  usd_value: string;
  price: string;
}

export interface WalletBalance {
  wallet_id: string;
  wallet_name: string;
  chain_id: string;
  assets: AssetBalance[];
  total_usd: string;
}

export interface PortfolioSummary {
  total_usd_value: string;
  total_assets: number;
  asset_holdings: AssetHolding[];
  wallet_balances: WalletBalance[];
  last_updated: string;
}

/**
 * Get portfolio summary for the authenticated user
 */
export const getPortfolioSummary = async (): Promise<PortfolioSummary> => {
  const response = await api.get<PortfolioSummary>('/portfolio');
  return response.data;
};

/**
 * Get detailed breakdown of a specific asset across all wallets
 */
export const getAssetBreakdown = async (assetId: string): Promise<WalletBalance[]> => {
  const response = await api.get<WalletBalance[]>(`/portfolio/assets`, {
    params: { asset_id: assetId }
  });
  return response.data;
};

export default {
  getPortfolioSummary,
  getAssetBreakdown,
};
