export interface Wallet {
  id: string
  name: string
  chain_id: string
  address?: string
  created_at: string
  updated_at: string
}

export interface CreateWalletRequest {
  name: string
  chain_id: string
  address?: string
}

export interface UpdateWalletRequest {
  name?: string
  chain_id?: string
  address?: string
}

export const SUPPORTED_CHAINS = [
  { id: 'ethereum', name: 'Ethereum', symbol: 'ETH' },
  { id: 'bitcoin', name: 'Bitcoin', symbol: 'BTC' },
  { id: 'solana', name: 'Solana', symbol: 'SOL' },
  { id: 'polygon', name: 'Polygon', symbol: 'MATIC' },
  { id: 'binance-smart-chain', name: 'Binance Smart Chain', symbol: 'BNB' },
  { id: 'arbitrum', name: 'Arbitrum', symbol: 'ARB' },
  { id: 'optimism', name: 'Optimism', symbol: 'OP' },
  { id: 'avalanche', name: 'Avalanche', symbol: 'AVAX' },
] as const

export type ChainId = (typeof SUPPORTED_CHAINS)[number]['id']
