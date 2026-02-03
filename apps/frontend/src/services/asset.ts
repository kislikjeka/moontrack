import api from './api';

/**
 * PriceResponse represents the response from the asset price endpoint
 */
export interface PriceResponse {
  asset_id: string; // Asset UUID
  price_usd: string; // USD price scaled by 10^8
  source: string; // Price data source (e.g., "coingecko")
  timestamp: string; // ISO 8601 timestamp
  is_stale?: boolean; // True if price is from stale cache
}

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

/**
 * PriceHistoryPoint represents a single price point in history
 */
export interface PriceHistoryPoint {
  timestamp: string;
  price_usd: string;
}

/**
 * PriceHistoryResponse represents the response from the price history endpoint
 */
export interface PriceHistoryResponse {
  asset_id: string;
  from: string;
  to: string;
  interval: '1h' | '1d' | '1w';
  prices: PriceHistoryPoint[];
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

  /**
   * Get an asset by its UUID
   * @param id Asset UUID
   * @returns Full asset details
   */
  async getAsset(id: string): Promise<RegistryAsset> {
    const response = await api.get<RegistryAsset>(`/assets/${id}`);
    return response.data;
  },

  /**
   * List assets with optional filters
   * @param params Optional filter parameters
   * @returns List of assets
   */
  async listAssets(params?: { symbol?: string; chain?: string }): Promise<{ assets: RegistryAsset[] }> {
    const response = await api.get<{ assets: RegistryAsset[] }>('/assets', { params });
    return response.data;
  },

  /**
   * Get the current USD price for an asset
   * @param assetId Asset UUID
   * @returns Price response with metadata
   */
  async getPrice(assetId: string): Promise<PriceResponse> {
    const response = await api.get<PriceResponse>(`/assets/${assetId}/price`);
    return response.data;
  },

  /**
   * Get batch prices for multiple assets
   * @param assetIds Array of asset UUIDs
   * @returns Array of price responses
   */
  async getBatchPrices(assetIds: string[]): Promise<{ prices: PriceResponse[] }> {
    const response = await api.post<{ prices: PriceResponse[] }>('/assets/prices', {
      asset_ids: assetIds,
    });
    return response.data;
  },

  /**
   * Get price history for an asset
   * @param assetId Asset UUID
   * @param from Start date (RFC3339 or YYYY-MM-DD)
   * @param to End date (RFC3339 or YYYY-MM-DD)
   * @param interval Price interval (1h, 1d, 1w)
   * @returns Price history
   */
  async getPriceHistory(
    assetId: string,
    from: string,
    to: string,
    interval: '1h' | '1d' | '1w' = '1d'
  ): Promise<PriceHistoryResponse> {
    const response = await api.get<PriceHistoryResponse>(`/assets/${assetId}/history`, {
      params: { from, to, interval },
    });
    return response.data;
  },
};

export default assetService;
