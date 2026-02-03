import React from 'react';
import { useNavigate } from 'react-router-dom';
import { TransactionListItem } from '../../services/transaction';

interface TransactionItemProps {
  transaction: TransactionListItem;
}

export const TransactionItem: React.FC<TransactionItemProps> = ({ transaction }) => {
  const navigate = useNavigate();

  // Handle row click - navigate to transaction detail
  const handleClick = () => {
    navigate(`/transactions/${transaction.id}`);
  };

  // Handle keyboard navigation
  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      handleClick();
    }
  };

  // Format date for display
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

  // Get CSS class for direction styling
  const getDirectionClass = () => {
    switch (transaction.direction) {
      case 'in':
        return 'income';
      case 'out':
        return 'outcome';
      case 'adjustment':
        return 'adjustment';
      default:
        return '';
    }
  };

  // Get direction indicator
  const getDirectionIndicator = () => {
    switch (transaction.direction) {
      case 'in':
        return { icon: '↓', prefix: '+' };
      case 'out':
        return { icon: '↑', prefix: '-' };
      default:
        return { icon: '⚙', prefix: '' };
    }
  };

  const directionInfo = getDirectionIndicator();
  const formattedDate = formatDate(transaction.occurred_at);

  return (
    <div
      className={`transaction-item ${getDirectionClass()}`}
      onClick={handleClick}
      onKeyDown={handleKeyDown}
      role="button"
      tabIndex={0}
      aria-label={`${transaction.type_label}: ${transaction.display_amount}`}
    >
      <div className="transaction-icon">
        <span className="icon">{directionInfo.icon}</span>
      </div>

      <div className="transaction-details">
        <div className="transaction-header">
          <h3 className="transaction-type">{transaction.type_label}</h3>
          {transaction.status === 'FAILED' && (
            <span className="status-badge failed">Failed</span>
          )}
        </div>

        <div className="transaction-amount">
          <span className={`amount amount-${transaction.direction}`}>
            {directionInfo.prefix}{transaction.display_amount}
          </span>
          {transaction.usd_value && (
            <span className="usd-value">
              ${parseFloat(transaction.usd_value).toLocaleString('en-US', {
                minimumFractionDigits: 2,
                maximumFractionDigits: 2,
              })}
            </span>
          )}
        </div>

        <div className="transaction-meta">
          <span className="wallet-name">{transaction.wallet_name}</span>
          <span className="date">{formattedDate}</span>
        </div>
      </div>

      <div className="transaction-arrow">
        <span className="chevron">›</span>
      </div>
    </div>
  );
};

export default TransactionItem;
