import api from './api';
import type { PortfolioSummary, WalletBalance } from '@/types/portfolio';

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
