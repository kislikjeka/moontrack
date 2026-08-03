import api from './api';
import type { Asset } from '../types/asset';

/**
 * RegistryAsset is the wire shape of a registry asset — an alias of `Asset`,
 * not a second declaration of it.
 *
 * The two used to be separate interfaces that happened to match, and they drifted:
 * after #59 this file still described `asset_type`, `market_cap_rank` and
 * `is_active`, fields the API had already stopped sending. Nothing caught it,
 * because two structurally identical types are assignable to each other. One
 * name per wire shape makes that drift impossible.
 */
export type RegistryAsset = Asset;

export const assetService = {
  /**
   * Search registry assets by symbol or name.
   *
   * The registry only — the CoinGecko fallback is gone, retired with the
   * `assets` table it used to insert into (#59). Exact symbol matches rank
   * first, since there is no market cap rank left to sort by.
   *
   * @param query Search query (min 2, max 50 characters)
   */
  async search(query: string): Promise<{ assets: RegistryAsset[] }> {
    const response = await api.get<{ assets: RegistryAsset[] }>('/assets/search', {
      params: { q: query },
    });
    return response.data;
  },
};

export default assetService;
