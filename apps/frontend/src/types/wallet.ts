// Sync status for wallet blockchain synchronization
export type WalletSyncStatus = 'pending' | 'syncing' | 'synced' | 'error'

export interface Wallet {
  id: string
  name: string
  chain_id: number | string // Backend may return string until migrated
  address?: string // Optional until backend requires it
  sync_status?: WalletSyncStatus // Optional until backend supports sync
  last_sync_block?: number
  last_sync_at?: string
  sync_error?: string
  created_at: string
  updated_at: string
}

export interface CreateWalletRequest {
  name: string
  chain_id: number
  address: string
}

export interface UpdateWalletRequest {
  name?: string
  // chain_id and address are NOT editable after creation
}

// EVM-only chains with numeric chain IDs
export const SUPPORTED_CHAINS = [
  { id: 1, name: 'Ethereum', symbol: 'ETH' },
  { id: 137, name: 'Polygon', symbol: 'MATIC' },
  { id: 42161, name: 'Arbitrum', symbol: 'ETH' },
  { id: 10, name: 'Optimism', symbol: 'ETH' },
  { id: 8453, name: 'Base', symbol: 'ETH' },
] as const

export type ChainId = (typeof SUPPORTED_CHAINS)[number]['id']

// Helpers
export function getChainById(chainId: number | string) {
  const numericId = typeof chainId === 'string' ? parseInt(chainId, 10) : chainId
  return SUPPORTED_CHAINS.find((c) => c.id === numericId)
}

// Legacy chain name mapping for backwards compatibility
const LEGACY_CHAIN_NAMES: Record<string, string> = {
  ethereum: 'Ethereum',
  polygon: 'Polygon',
  arbitrum: 'Arbitrum',
  optimism: 'Optimism',
  base: 'Base',
  bitcoin: 'Bitcoin',
  solana: 'Solana',
  'binance-smart-chain': 'BSC',
  avalanche: 'Avalanche',
}

export function getChainName(chainId: number | string): string {
  // Try numeric lookup first
  const chain = getChainById(chainId)
  if (chain) return chain.name

  // Try legacy string lookup
  if (typeof chainId === 'string' && LEGACY_CHAIN_NAMES[chainId]) {
    return LEGACY_CHAIN_NAMES[chainId]
  }

  return String(chainId)
}

export function getChainSymbol(chainId: number | string): string {
  const chain = getChainById(chainId)
  if (chain) return chain.symbol

  // For legacy string IDs, return uppercase first 3-4 chars
  if (typeof chainId === 'string') {
    return chainId.slice(0, 4).toUpperCase()
  }

  return String(chainId)
}

export function isValidEVMAddress(address: string): boolean {
  return /^0x[a-fA-F0-9]{40}$/.test(address)
}
