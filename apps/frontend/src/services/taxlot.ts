import api from './api'
import type { TaxLot, PositionWAC, OverrideCostBasisRequest, TransactionLotImpact } from '@/types/taxlot'

export const taxlotService = {
  async getLots(walletId: string, asset: string, chainId?: string): Promise<TaxLot[]> {
    const params: Record<string, string> = { wallet_id: walletId, asset }
    if (chainId) {
      params.chain_id = chainId
    }
    const response = await api.get<{ lots: TaxLot[] }>('/lots', { params })
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

  async getTransactionLots(transactionId: string): Promise<TransactionLotImpact> {
    const response = await api.get<TransactionLotImpact>(`/transactions/${transactionId}/lots`)
    return response.data
  },
}

export default taxlotService
