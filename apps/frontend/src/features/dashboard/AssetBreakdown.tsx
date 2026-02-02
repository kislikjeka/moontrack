import React from 'react';
import { AssetHolding } from '../../services/portfolio';
import './AssetBreakdown.css';

interface AssetBreakdownProps {
  assetHoldings: AssetHolding[];
}

/**
 * AssetBreakdown component - Displays individual asset holdings with USD values
 * Shows each cryptocurrency asset with its current balance and value
 */
const AssetBreakdown: React.FC<AssetBreakdownProps> = ({ assetHoldings }) => {
  const formatUSD = (value: string): string => {
    try {
      const bigIntValue = BigInt(value);
      const dollars = Number(bigIntValue) / 100000000;
      return new Intl.NumberFormat('en-US', {
        style: 'currency',
        currency: 'USD',
        minimumFractionDigits: 2,
        maximumFractionDigits: 2,
      }).format(dollars);
    } catch (e) {
      return '$0.00';
    }
  };

  const formatAmount = (amount: string, assetId: string): string => {
    try {
      const bigIntValue = BigInt(amount);

      // Different decimals for different assets
      const decimals: Record<string, number> = {
        'BTC': 8,   // Bitcoin: satoshis
        'ETH': 18,  // Ethereum: wei
        'USDC': 6,  // USDC: 6 decimals
        'USDT': 6,  // USDT: 6 decimals
        'SOL': 9,   // Solana: lamports
      };

      const decimal = decimals[assetId] || 18; // Default to 18
      const divisor = BigInt(10 ** decimal);
      const integerPart = bigIntValue / divisor;
      const fractionalPart = bigIntValue % divisor;

      const fractionalStr = fractionalPart.toString().padStart(decimal, '0');
      const trimmedFractional = fractionalStr.replace(/0+$/, '').slice(0, 8); // Show up to 8 decimals

      if (trimmedFractional === '') {
        return `${integerPart.toString()}`;
      }

      return `${integerPart.toString()}.${trimmedFractional}`;
    } catch (e) {
      return '0';
    }
  };

  const formatPrice = (price: string): string => {
    try {
      const bigIntValue = BigInt(price);
      const dollars = Number(bigIntValue) / 100000000;
      return new Intl.NumberFormat('en-US', {
        style: 'currency',
        currency: 'USD',
        minimumFractionDigits: 2,
        maximumFractionDigits: 2,
      }).format(dollars);
    } catch (e) {
      return '$0.00';
    }
  };

  const getAssetIcon = (assetId: string): string => {
    const icons: Record<string, string> = {
      'BTC': '₿',
      'ETH': 'Ξ',
      'USDC': '💵',
      'USDT': '💵',
      'SOL': '◎',
      'BNB': '🔶',
      'ADA': '🔷',
      'DOT': '⚫',
    };
    return icons[assetId] || '🪙';
  };

  if (assetHoldings.length === 0) {
    return null;
  }

  return (
    <div className="asset-breakdown">
      <h2>Asset Holdings</h2>
      <div className="asset-list">
        {assetHoldings.map((holding) => (
          <div key={holding.asset_id} className="asset-card">
            <div className="asset-header">
              <div className="asset-icon">{getAssetIcon(holding.asset_id)}</div>
              <div className="asset-info">
                <h3 className="asset-name">{holding.asset_id}</h3>
                <div className="asset-amount">
                  {formatAmount(holding.total_amount, holding.asset_id)} {holding.asset_id}
                </div>
              </div>
            </div>
            <div className="asset-values">
              <div className="value-row">
                <span className="value-label">Value:</span>
                <span className="value-amount">{formatUSD(holding.usd_value)}</span>
              </div>
              <div className="value-row">
                <span className="value-label">Price:</span>
                <span className="value-amount">{formatPrice(holding.current_price)}</span>
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
};

export default AssetBreakdown;
