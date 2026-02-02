import React from 'react';
import { Transaction } from '../../services/transaction';

interface TransactionItemProps {
  transaction: Transaction;
}

export const TransactionItem: React.FC<TransactionItemProps> = ({ transaction }) => {
  // Format amount (remove trailing zeros and format with proper decimals)
  const formatAmount = (amountStr: string, assetId: string): string => {
    // Convert from base units to human-readable format
    // This is a simplified version - real implementation should handle different decimals per asset
    const amount = parseFloat(amountStr);
    const decimals = getAssetDecimals(assetId);
    const formatted = (amount / Math.pow(10, decimals)).toFixed(decimals);

    // Remove trailing zeros
    return parseFloat(formatted).toString();
  };

  // Get decimals for an asset (simplified - should come from config)
  const getAssetDecimals = (assetId: string): number => {
    const decimalsMap: Record<string, number> = {
      BTC: 8,
      ETH: 18,
      USDC: 6,
      USDT: 6,
      BNB: 18,
      SOL: 9,
      ADA: 6,
      DOT: 10,
      MATIC: 18,
      AVAX: 18,
    };
    return decimalsMap[assetId] || 18; // Default to 18 decimals
  };

  // Format USD value (scaled by 10^8)
  const formatUSD = (usdValueStr: string): string => {
    const value = parseFloat(usdValueStr) / Math.pow(10, 8);
    return new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency: 'USD',
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format(value);
  };

  // Format date
  const formatDate = (isoDateStr: string): string => {
    const date = new Date(isoDateStr);
    return new Intl.DateTimeFormat('en-US', {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    }).format(date);
  };

  // Get transaction type display info
  const getTypeInfo = (type: string) => {
    switch (type) {
      case 'manual_income':
        return { label: 'Income', icon: '↓', className: 'income' };
      case 'manual_outcome':
        return { label: 'Outcome', icon: '↑', className: 'outcome' };
      case 'asset_adjustment':
        return { label: 'Adjustment', icon: '⚙', className: 'adjustment' };
      default:
        return { label: type, icon: '•', className: 'unknown' };
    }
  };

  const typeInfo = getTypeInfo(transaction.type);
  const formattedAmount = formatAmount(transaction.amount, transaction.asset_id);
  const formattedUSD = formatUSD(transaction.usd_value);
  const formattedDate = formatDate(transaction.occurred_at);

  return (
    <div className={`transaction-item ${typeInfo.className}`}>
      <div className="transaction-icon">
        <span className="icon">{typeInfo.icon}</span>
      </div>

      <div className="transaction-details">
        <div className="transaction-header">
          <h3 className="transaction-type">{typeInfo.label}</h3>
          {transaction.status === 'FAILED' && (
            <span className="status-badge failed">Failed</span>
          )}
        </div>

        <div className="transaction-amount">
          <span className="amount">
            {transaction.type === 'manual_income' ? '+' : transaction.type === 'manual_outcome' ? '-' : ''}
            {formattedAmount} {transaction.asset_id}
          </span>
          <span className="usd-value">{formattedUSD}</span>
        </div>

        <div className="transaction-meta">
          {transaction.wallet_name && (
            <span className="wallet-name">
              <strong>Wallet:</strong> {transaction.wallet_name}
            </span>
          )}
          <span className="date">{formattedDate}</span>
        </div>

        {transaction.price_source && (
          <div className="price-source">
            <span className="label">Price source:</span>
            <span className={`source ${transaction.price_source}`}>
              {transaction.price_source === 'manual' ? '✏️ Manual' : '🌐 CoinGecko'}
            </span>
          </div>
        )}

        {transaction.notes && (
          <div className="transaction-notes">
            <span className="label">Notes:</span>
            <p>{transaction.notes}</p>
          </div>
        )}
      </div>
    </div>
  );
};

export default TransactionItem;
