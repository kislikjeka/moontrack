import api from './api';

/**
 * Wallet API client
 * Handles all wallet-related API calls
 */

/**
 * Create a new wallet
 * @param {Object} walletData - Wallet creation data
 * @param {string} walletData.name - Wallet name
 * @param {string} walletData.chain_id - Blockchain chain ID
 * @param {string} [walletData.address] - Optional blockchain address
 * @returns {Promise<Object>} Created wallet
 */
export const createWallet = async (walletData) => {
  const response = await api.post('/wallets', walletData);
  return response.data;
};

/**
 * Get all wallets for the current user
 * @returns {Promise<Array>} List of wallets
 */
export const getWallets = async () => {
  const response = await api.get('/wallets');
  return response.data;
};

/**
 * Get a specific wallet by ID
 * @param {string} walletId - Wallet ID
 * @returns {Promise<Object>} Wallet details
 */
export const getWallet = async (walletId) => {
  const response = await api.get(`/wallets/${walletId}`);
  return response.data;
};

/**
 * Update an existing wallet
 * @param {string} walletId - Wallet ID
 * @param {Object} walletData - Updated wallet data
 * @param {string} walletData.name - Wallet name
 * @param {string} walletData.chain_id - Blockchain chain ID
 * @param {string} [walletData.address] - Optional blockchain address
 * @returns {Promise<Object>} Updated wallet
 */
export const updateWallet = async (walletId, walletData) => {
  const response = await api.put(`/wallets/${walletId}`, walletData);
  return response.data;
};

/**
 * Delete a wallet
 * @param {string} walletId - Wallet ID
 * @returns {Promise<void>}
 */
export const deleteWallet = async (walletId) => {
  await api.delete(`/wallets/${walletId}`);
};

/**
 * Supported blockchain chains
 */
export const SUPPORTED_CHAINS = [
  { id: 'ethereum', name: 'Ethereum', symbol: 'ETH' },
  { id: 'bitcoin', name: 'Bitcoin', symbol: 'BTC' },
  { id: 'solana', name: 'Solana', symbol: 'SOL' },
  { id: 'polygon', name: 'Polygon', symbol: 'MATIC' },
  { id: 'binance-smart-chain', name: 'Binance Smart Chain', symbol: 'BNB' },
  { id: 'arbitrum', name: 'Arbitrum', symbol: 'ARB' },
  { id: 'optimism', name: 'Optimism', symbol: 'OP' },
  { id: 'avalanche', name: 'Avalanche', symbol: 'AVAX' },
];
