// Sync status for wallet blockchain synchronization
export type WalletSyncStatus = 'pending' | 'syncing' | 'synced' | 'error'

export interface ChainConfig {
  id: string
  name: string
  shortName: string
  color: string
  explorerUrl: string
}

export const CHAIN_CONFIG: Record<string, ChainConfig> = {
  ethereum: { id: 'ethereum', name: 'Ethereum', shortName: 'ETH', color: '#627EEA', explorerUrl: 'https://etherscan.io' },
  polygon: { id: 'polygon', name: 'Polygon', shortName: 'MATIC', color: '#8247E5', explorerUrl: 'https://polygonscan.com' },
  arbitrum: { id: 'arbitrum', name: 'Arbitrum One', shortName: 'ARB', color: '#28A0F0', explorerUrl: 'https://arbiscan.io' },
  optimism: { id: 'optimism', name: 'Optimism', shortName: 'OP', color: '#FF0420', explorerUrl: 'https://optimistic.etherscan.io' },
  base: { id: 'base', name: 'Base', shortName: 'BASE', color: '#0052FF', explorerUrl: 'https://basescan.org' },
  avalanche: { id: 'avalanche', name: 'Avalanche', shortName: 'AVAX', color: '#E84142', explorerUrl: 'https://snowtrace.io' },
  'binance-smart-chain': { id: 'binance-smart-chain', name: 'BNB Smart Chain', shortName: 'BSC', color: '#F0B90B', explorerUrl: 'https://bscscan.com' },
}

export interface Wallet {
  id: string
  name: string
  address: string
  supported_chains?: string[]
  sync_status?: WalletSyncStatus
  last_sync_at?: string
  sync_error?: string
  created_at: string
  updated_at: string
}

export interface CreateWalletRequest {
  name: string
  address: string
}

export interface UpdateWalletRequest {
  name?: string
}

// Helpers
export function getChainConfig(chainId: string): ChainConfig | undefined {
  return CHAIN_CONFIG[chainId]
}

export function getChainName(chainId: string): string {
  return CHAIN_CONFIG[chainId]?.name ?? chainId
}

export function getChainShortName(chainId: string): string {
  return CHAIN_CONFIG[chainId]?.shortName ?? chainId.slice(0, 4).toUpperCase()
}

export function isValidEVMAddress(address: string): boolean {
  return /^0x[a-fA-F0-9]{40}$/.test(address)
}
