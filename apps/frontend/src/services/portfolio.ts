import api from './api';
import type { PortfolioSummary } from '@/types/portfolio';

/**
 * Get portfolio summary for the authenticated user
 */
export const getPortfolioSummary = async (): Promise<PortfolioSummary> => {
  const response = await api.get<PortfolioSummary>('/portfolio');
  return response.data;
};

// getAssetBreakdown and GET /portfolio/assets are gone (#42). The call had no
// caller anywhere in src/ and there is no asset page for one to live on, and the
// per-wallet breakdown it returned is already carried by the summary's
// wallet_balances. The WalletBalance TYPE stays — it is what that field is.

export default {
  getPortfolioSummary,
};
