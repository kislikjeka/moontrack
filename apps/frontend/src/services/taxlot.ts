import api from './api'
import type { TaxLot, PositionWAC, OverrideCostBasisRequest } from '@/types/taxlot'

export const taxlotService = {
  async getLots(walletId: string, asset: string): Promise<TaxLot[]> {
    const response = await api.get<{ lots: TaxLot[] }>('/lots', {
      params: { wallet_id: walletId, asset },
    })
    return response.data.lots || []
  },

  async overrideCostBasis(lotId: string, data: OverrideCostBasisRequest): Promise<void> {
    await api.put(`/lots/${lotId}/override`, data)
  },

  async getWAC(walletId?: string): Promise<PositionWAC[]> {
    const response = await api.get<{ positions: PositionWAC[] }>('/positions/wac', {
      params: walletId ? { wallet_id: walletId } : undefined,
    })
    return response.data.positions || []
  },
}

export default taxlotService
