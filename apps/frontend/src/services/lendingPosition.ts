import api from './api'
import type { LendingPosition } from '@/types/lendingPosition'

export const listLendingPositions = async (
  walletId: string,
  status?: string
): Promise<LendingPosition[]> => {
  const params: Record<string, string> = { wallet_id: walletId }
  if (status) params.status = status
  const response = await api.get<LendingPosition[]>('/lending/positions', { params })
  return response.data
}

export const getLendingPosition = async (id: string): Promise<LendingPosition> => {
  const response = await api.get<LendingPosition>(`/lending/positions/${id}`)
  return response.data
}
