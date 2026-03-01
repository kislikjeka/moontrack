import api from './api';
import type { LPPosition } from '@/types/lpPosition';

/**
 * List LP positions for a wallet, optionally filtered by status
 */
export const listLPPositions = async (
  walletId: string,
  status?: string
): Promise<LPPosition[]> => {
  const params: Record<string, string> = { wallet_id: walletId };
  if (status) params.status = status;
  const response = await api.get<LPPosition[]>('/lp/positions', { params });
  return response.data;
};

/**
 * Get a single LP position by ID
 */
export const getLPPosition = async (id: string): Promise<LPPosition> => {
  const response = await api.get<LPPosition>(`/lp/positions/${id}`);
  return response.data;
};
