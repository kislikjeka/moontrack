import api from './api'
import type { Wallet, CreateWalletRequest, UpdateWalletRequest } from '@/types/wallet'

interface WalletsResponse {
  wallets: Wallet[]
}

export async function createWallet(data: CreateWalletRequest): Promise<Wallet> {
  const response = await api.post<Wallet>('/wallets', data)
  return response.data
}

export async function getWallets(): Promise<Wallet[]> {
  const response = await api.get<WalletsResponse>('/wallets')
  return response.data.wallets || []
}

export async function getWallet(id: string): Promise<Wallet> {
  const response = await api.get<Wallet>(`/wallets/${id}`)
  return response.data
}

export async function updateWallet(id: string, data: UpdateWalletRequest): Promise<Wallet> {
  const response = await api.put<Wallet>(`/wallets/${id}`, data)
  return response.data
}

export async function deleteWallet(id: string): Promise<void> {
  await api.delete(`/wallets/${id}`)
}

export async function triggerWalletSync(walletId: string): Promise<void> {
  await api.post(`/wallets/${walletId}/sync`)
}

export { SUPPORTED_CHAINS } from '@/types/wallet'
