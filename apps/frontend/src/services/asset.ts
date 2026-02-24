import api from './api';

/**
 * RegistryAsset represents a full asset from the registry
 */
export interface RegistryAsset {
  id: string; // UUID
  symbol: string;
  name: string;
  coingecko_id: string;
  decimals: number;
  asset_type: 'crypto' | 'fiat' | 'custom';
  chain_id?: string;
  contract_address?: string;
  market_cap_rank?: number;
  is_active: boolean;
}

export const assetService = {
  /**
   * Search for cryptocurrency assets by name or symbol
   * @param query Search query (min 2, max 50 characters)
   * @returns List of matching assets sorted by market cap rank
   */
  async search(query: string): Promise<{ assets: RegistryAsset[] }> {
    const response = await api.get<{ assets: RegistryAsset[] }>('/assets/search', {
      params: { q: query },
    });
    return response.data;
  },
};

export default assetService;
